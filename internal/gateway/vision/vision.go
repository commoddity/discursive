// Package vision describes image content parts for text models without native
// vision by routing them through a vision-capable model (Z.AI glm-4.6v).
// Contract: depends on config (Z.AI key + chat URL) + injectable http.Client.
package vision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	cacheTTL          = 10 * time.Minute
	maxConcurrent     = 4
	visionRequestPath = "/chat/completions"
)

// visionPrompt is the instruction sent to glm-4.6v for each image.
const visionPrompt = "Describe this image in detail for a coding assistant. Capture any text, UI, error messages, diagrams, and layout. Be precise and concise."

// ErrNoVisionKey indicates the caller tried to describe an image but the Z.AI
// vision key is not configured. Callers fail fast (never silently strip).
var ErrNoVisionKey = fmt.Errorf("image attached but the vision model (Z.AI glm-4.6v) requires a Z.AI API key which is not configured; run `discursive set --zai-key <key>` and restart the gateway")

// Describer describes images via glm-4.6v and replaces image_url parts with
// text descriptions inline.
type Describer struct {
	client    *http.Client
	chatURL   string
	getZaiKey func() (string, bool)

	cache   sync.Map // sha256hash -> cacheEntry
	sfg     singleflight.Group
	descMu  sync.Mutex // guards descriptionCounter
	counter int        // per-replace cycle image counter
}

type cacheEntry struct {
	desc      string
	expiresAt time.Time
}

// NewDescriber creates a Describer. getZaiKey reuses the existing
// AppSettings.GetZaiKey / LiveSettings.GetZaiKey pattern.
func NewDescriber(client *http.Client, chatURL string, getZaiKey func() (string, bool)) *Describer {
	return &Describer{
		client:    client,
		chatURL:   chatURL,
		getZaiKey: getZaiKey,
	}
}

// ReplaceImages scans messages for image parts and replaces each with a text
// description from glm-4.6v. Applies to ALL providers (Moonshot/DeepSeek/Z.AI/
// Thaura). Caches by content hash (10 min TTL). Returns the count of images
// described and, on hard failure, an error that the caller must surface to the
// user (fail-fast): a missing Z.AI key, or any upstream rejection / network
// error. On error the body is left untouched.
func (d *Describer) ReplaceImages(ctx context.Context, body map[string]any) (int, error) {
	if d == nil {
		return 0, ErrNoVisionKey
	}

	msgs, ok := body["messages"].([]any)
	if !ok {
		return 0, nil
	}

	d.descMu.Lock()
	d.counter = 0
	d.descMu.Unlock()

	// Collect all image hashes across messages first.
	type imageJob struct {
		msgIdx  int
		partIdx int
		hash    string
		imgURL  string
	}
	var jobs []imageJob
	for mi, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
			// Single content object (e.g. map with type == image_url in
			// rare non-array content). Skip for now.
			continue
		}
		for pi, part := range parts {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			url := extractImageURL(p)
			if url == "" {
				continue
			}
			h := hashImageURL(url)
			jobs = append(jobs, imageJob{msgIdx: mi, partIdx: pi, hash: h, imgURL: url})
		}
	}
	if len(jobs) == 0 {
		return 0, nil
	}

	// Fail fast if the vision key is missing — never silently strip images.
	if d.getZaiKey == nil {
		return 0, ErrNoVisionKey
	}
	if key, ok := d.getZaiKey(); !ok || key == "" {
		return 0, ErrNoVisionKey
	}

	// Deduplicate concurrent descriptions by hash.
	type result struct {
		desc string
		err  error
	}
	results := make([]result, len(jobs))

	// Limit concurrent vision calls.
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, job := range jobs {
		// Check cache first (fast path outside semaphore).
		if v, ok := d.cache.Load(job.hash); ok {
			entry := v.(cacheEntry)
			if time.Now().Before(entry.expiresAt) {
				results[i] = result{desc: entry.desc}
				continue
			}
			d.cache.Delete(job.hash)
		}

		wg.Add(1)
		go func(idx int, j imageJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Deduplicate concurrent calls for the same hash.
			val, err, _ := d.sfg.Do(j.hash, func() (any, error) {
				desc, err := d.describeImage(ctx, j.imgURL)
				if err != nil {
					return nil, err
				}
				d.cache.Store(j.hash, cacheEntry{desc: desc, expiresAt: time.Now().Add(cacheTTL)})
				return desc, nil
			})
			if err != nil {
				results[idx] = result{err: err}
			} else {
				results[idx] = result{desc: val.(string)}
			}
		}(i, job)
	}
	wg.Wait()

	// On ANY hard failure, fail fast: surface the error and leave body untouched.
	for i := range jobs {
		if results[i].err != nil {
			return 0, fmt.Errorf("vision describe failed for image %d: %w", i+1, results[i].err)
		}
	}

	// Apply results: replace image parts inline.
	count := 0
	for i, job := range jobs {
		msg := msgs[job.msgIdx].(map[string]any)
		parts := msg["content"].([]any)
		count++
		d.descMu.Lock()
		d.counter++
		n := d.counter
		d.descMu.Unlock()
		parts[job.partIdx] = map[string]any{"type": "text", "text": fmt.Sprintf("[Image %d: %s]", n, results[i].desc)}
	}

	return count, nil
}

// extractImageURL pulls the URL string from an image_url content part.
// Returns "" if the part is not an image_url or missing url.
func extractImageURL(part map[string]any) string {
	typ := stringField(part, "type")
	if typ != "image_url" {
		// Also check the image_url field directly (some shapes omit type).
		if _, ok := part["image_url"]; !ok {
			return ""
		}
	}
	img, ok := part["image_url"].(map[string]any)
	if !ok {
		return ""
	}
	return stringField(img, "url")
}

// hashImageURL returns a SHA-256 hash of the image content.
// For data URLs the decoded bytes are hashed; for https URLs the URL string
// is hashed (fetch happens upstream-side by glm-4.6v).
func hashImageURL(url string) string {
	b := sha256.Sum256(imageContentForHash(url))
	return fmt.Sprintf("%x", b[:])
}

func imageContentForHash(url string) []byte {
	if strings.HasPrefix(url, "data:") {
		// data:image/png;base64,<base64>
		idx := strings.Index(url, ";base64,")
		if idx >= 0 {
			raw := url[idx+len(";base64,"):]
			dec, err := base64.StdEncoding.DecodeString(raw)
			if err != nil {
				return []byte(raw)
			}
			return dec
		}
	}
	return []byte(url)
}

func (d *Describer) describeImage(ctx context.Context, imgURL string) (string, error) {
	key, ok := d.getZaiKey()
	if !ok || key == "" {
		return "", ErrNoVisionKey
	}

	reqBody := map[string]any{
		"model":  "glm-4.6v",
		"stream": false,
		"thinking": map[string]any{
			"type": "disabled",
		},
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": imgURL},
					},
					map[string]any{
						"type": "text",
						"text": visionPrompt,
					},
				},
			},
		},
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("vision: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.chatURL, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("vision: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision: upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("vision: read body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := extractUpstreamError(respBody)
		slog.Warn("vision_call_failed",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		if msg != "" {
			return "", fmt.Errorf("vision model rejected the image: %s (HTTP %d)", msg, resp.StatusCode)
		}
		return "", fmt.Errorf("vision model rejected the image (HTTP %d)", resp.StatusCode)
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return "", fmt.Errorf("vision: unmarshal completion: %w", err)
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("vision: no choices in response")
	}

	desc := strings.TrimSpace(completion.Choices[0].Message.Content)
	if desc == "" {
		return "", fmt.Errorf("vision: empty description")
	}
	slog.Info("vision_call_succeeded",
		"model", "glm-4.6v",
		"desc_len", len(desc),
	)
	return desc, nil
}

// extractUpstreamError pulls an OpenAI-shaped error.message from an upstream
// error body. Returns "" if none is present.
func extractUpstreamError(body []byte) string {
	var out struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return ""
	}
	return strings.TrimSpace(out.Error.Message)
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case json.Number:
		return s.String()
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	default:
		return ""
	}
}
