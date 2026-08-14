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

// visionModel is the real Z.AI model id used for image description.
const visionModel = "glm-4.6v"

// UsageRecorder meters one successful vision call. model is the real model id,
// promptTokens/completionTokens come from the upstream usage block, latency is
// the round-trip duration. Best-effort: implementers must not fail the vision
// path on a recording error.
type UsageRecorder func(model string, promptTokens, completionTokens uint64, latency time.Duration)

// Describer describes images via glm-4.6v and replaces image_url parts with
// text descriptions inline.
type Describer struct {
	client    *http.Client
	chatURL   string
	getZaiKey func() (string, bool)
	record    UsageRecorder // optional best-effort usage meter; may be nil

	cache   sync.Map  // sha256hash -> cacheEntry (short-lived in-memory)
	persist descCache // durable hash -> description store; nil in tests
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

// SetPersistentCache installs a durable content-hash description store. When
// set, an image that was described on a previous turn (or a previous process)
// is resolved from the store without an upstream vision call, so a vision model
// that is rate-limited no longer breaks later turns that only carry already
// described images. Closing is the caller's responsibility.
func (d *Describer) SetPersistentCache(c descCache) {
	if d != nil {
		d.persist = c
	}
}

// SetUsageRecorder installs a best-effort usage meter called after each
// successful vision call. Passing nil disables metering (default). The
// recorder must not fail the vision path.
func (d *Describer) SetUsageRecorder(r UsageRecorder) {
	if d != nil {
		d.record = r
	}
}

// imageJob describes one image_url part found in the message history.
type imageJob struct {
	msgIdx  int
	partIdx int
	hash    string
	imgURL  string
}

// imageResult is the per-image replacement outcome: a description on success
// or an error on a failed vision call (which falls back to a placeholder).
type imageResult struct {
	desc string
	err  error
}

// imageUnavailableNote is substituted for an image part when the vision model
// cannot describe it (rate-limited, missing key, network, or upstream
// rejection). The text model still receives a clear, honest placeholder instead
// of the raw image, and the turn proceeds.
const imageUnavailableNote = "[An image could not be described because the vision model is unavailable (e.g. rate-limited). It has been replaced with this note so the conversation can continue.]"

// ReplaceImages scans messages for image parts and replaces each with a text
// description from glm-4.6v. Applies to ALL providers (Moonshot/DeepSeek/Z.AI/
// Thaura). Caches by content hash in memory (10 min TTL) and, when a durable
// store is installed, persists descriptions so historical images are reused
// without a vision call. Returns the count of image parts replaced.
//
// Contract: ReplaceImages is graceful. An image already described (historical)
// is reused from cache with no vision call. A new image that cannot be
// described (missing Z.AI key, rate limit, network, upstream rejection) is
// replaced with a placeholder note rather than failing the whole request — a
// rate-limited vision model never blocks subsequent prompts.
func (d *Describer) ReplaceImages(ctx context.Context, body map[string]any) (int, error) {
	if d == nil {
		return 0, nil
	}

	msgs, ok := body["messages"].([]any)
	if !ok {
		return 0, nil
	}

	d.descMu.Lock()
	d.counter = 0
	d.descMu.Unlock()

	// Collect all image hashes across messages first.
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

	// Resolve historical images from the durable/in-memory cache; only images
	// with no cached description need a vision call (and only those hit the
	// upstream provider). A rate-limited vision model therefore cannot fault
	// already-described images.
	keyMissing := false
	results := make([]imageResult, len(jobs))
	needsVision := false

	if d.getZaiKey != nil {
		if k, ok := d.getZaiKey(); !ok || k == "" {
			keyMissing = true
		}
	} else {
		keyMissing = true
	}

	for i, job := range jobs {
		if d.persist != nil {
			if desc, ok := d.persist.Get(job.hash); ok {
				d.cache.Store(job.hash, cacheEntry{desc: desc, expiresAt: time.Now().Add(cacheTTL)})
				results[i] = imageResult{desc: desc}
				continue
			}
		}
		if v, ok := d.cache.Load(job.hash); ok {
			entry := v.(cacheEntry)
			if time.Now().Before(entry.expiresAt) {
				results[i] = imageResult{desc: entry.desc}
				continue
			}
			d.cache.Delete(job.hash)
		}
		// No cached description: this image needs a vision call. If the key is
		// missing, we will fall back to a placeholder below (never fail).
		needsVision = true
		if keyMissing {
			results[i] = imageResult{err: ErrNoVisionKey}
		}
	}

	if !needsVision {
		return d.applyDescriptions(msgs, jobs, results), nil
	}

	// If the vision key is missing, no upstream call is possible; fall back to
	// placeholder notes for every undescribed image and continue.
	if keyMissing {
		logDescriptionsSkipped(jobs, results)
		return d.applyDescriptions(msgs, jobs, results), nil
	}

	// Limit concurrent vision calls.
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, job := range jobs {
		if results[i].desc != "" {
			continue // already served from cache
		}

		wg.Add(1)
		go func(idx int, j imageJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			val, err, _ := d.sfg.Do(j.hash, func() (any, error) {
				desc, err := d.describeImage(ctx, j.imgURL, j.hash)
				if err != nil {
					return nil, err
				}
				d.cache.Store(j.hash, cacheEntry{desc: desc, expiresAt: time.Now().Add(cacheTTL)})
				if d.persist != nil {
					_ = d.persist.Put(j.hash, desc)
				}
				return desc, nil
			})
			if err != nil {
				results[idx] = imageResult{err: err}
			} else {
				results[idx] = imageResult{desc: val.(string)}
			}
		}(i, job)
	}
	wg.Wait()

	// Graceful fallback: any image that failed to describe is replaced with a
	// placeholder note. The request always proceeds.
	logDescriptionsSkipped(jobs, results)
	return d.applyDescriptions(msgs, jobs, results), nil
}

// applyDescriptions replaces image parts inline with their text descriptions
// (or a placeholder note on failure). It returns the number of image parts
// replaced.
func (d *Describer) applyDescriptions(msgs []any, jobs []imageJob, results []imageResult) int {
	count := 0
	for i, job := range jobs {
		msg := msgs[job.msgIdx].(map[string]any)
		parts := msg["content"].([]any)
		count++
		d.descMu.Lock()
		d.counter++
		n := d.counter
		d.descMu.Unlock()
		text := results[i].desc
		if text == "" {
			text = imageUnavailableNote
		}
		parts[job.partIdx] = map[string]any{"type": "text", "text": fmt.Sprintf("[Image %d: %s]", n, text)}
	}
	return count
}

// logDescriptionsSkipped logs (at WARN) one line per image that could not be
// described, including its truncated content hash so the operator can tell
// whether Cursor is resending the same image bytes across turns.
func logDescriptionsSkipped(jobs []imageJob, results []imageResult) {
	for i, job := range jobs {
		if i < len(results) && results[i].desc != "" {
			continue
		}
		short := job.hash
		if len(short) > 12 {
			short = short[:12]
		}
		slog.Warn("vision_image_unavailable",
			"image_hash", short,
			"msg_idx", job.msgIdx,
			"reason", "vision model could not describe the image (rate-limited, missing key, or upstream error)",
		)
	}
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

func (d *Describer) describeImage(ctx context.Context, imgURL, hash string) (string, error) {
	key, ok := d.getZaiKey()
	if !ok || key == "" {
		return "", ErrNoVisionKey
	}

	reqBody := map[string]any{
		"model":  visionModel,
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

	start := time.Now()
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
		// Truncate the hash so repeated diagnostics stay compact but still let
		// us distinguish "same image resent" (same hash) from "new image" (new
		// hash). This is the signal used to debug Cursor's image-history replay.
		shortHash := hash
		if len(shortHash) > 12 {
			shortHash = shortHash[:12]
		}
		slog.Warn("vision_call_failed",
			"status", resp.StatusCode,
			"image_hash", shortHash,
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
		Usage struct {
			PromptTokens     uint64 `json:"prompt_tokens"`
			CompletionTokens uint64 `json:"completion_tokens"`
		} `json:"usage"`
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
		"model", visionModel,
		"desc_len", len(desc),
	)
	// Best-effort usage metering; never fail the vision path on a record error.
	if d.record != nil {
		d.record(visionModel, completion.Usage.PromptTokens, completion.Usage.CompletionTokens, time.Since(start))
	}
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
