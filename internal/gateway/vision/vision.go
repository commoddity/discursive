// Package vision describes image content parts for text models without native
// vision by routing them through a provider-specific vision-capable model.
// Contract: depends on config (provider catalog) + injectable http.Client.
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

	"github.com/commoddity/discursive/internal/config"
	"golang.org/x/sync/singleflight"
)

const cacheTTL = 10 * time.Minute

// visionPrompt is the instruction sent to the vision model for each image.
const visionPrompt = "Describe this image in detail for a coding assistant. Capture any text, UI, error messages, diagrams, and layout. Be precise and concise."

// ErrNoVisionKey indicates the caller tried to describe an image but no API key
// is configured for the selected vision provider.
var ErrNoVisionKey = fmt.Errorf("image attached but the vision model requires an API key which is not configured for this provider")

// UsageRecorder meters one successful vision call.
type UsageRecorder func(provider config.Provider, model string, promptTokens, completionTokens uint64, latency time.Duration)

// Request carries per-call vision upstream settings from the gateway proxy.
type Request struct {
	Provider config.Provider
	Model    string
	ChatURL  string
	GetKey   func() (string, bool)
}

// Describer describes images and replaces image_url parts with text descriptions.
type Describer struct {
	client  *http.Client
	record  UsageRecorder
	cache   sync.Map
	persist descCache
	sfg     singleflight.Group
	descMu  sync.Mutex
	counter int
}

type cacheEntry struct {
	desc      string
	expiresAt time.Time
}

// NewDescriber creates a Describer.
func NewDescriber(client *http.Client) *Describer {
	return &Describer{client: client}
}

// SetPersistentCache installs a durable content-hash description store.
func (d *Describer) SetPersistentCache(c descCache) {
	if d != nil {
		d.persist = c
	}
}

// SetUsageRecorder installs a best-effort usage meter called after each
// successful vision call.
func (d *Describer) SetUsageRecorder(r UsageRecorder) {
	if d != nil {
		d.record = r
	}
}

type imageJob struct {
	msgIdx  int
	partIdx int
	hash    string
	imgURL  string
}

type imageResult struct {
	desc string
	err  error
}

const imageUnavailableNote = "[An image could not be described because the vision model is unavailable (e.g. rate-limited). It has been replaced with this note so the conversation can continue.]"

// ReplaceImages scans messages for image parts and replaces each with a text
// description from the configured vision model for the request provider.
func (d *Describer) ReplaceImages(ctx context.Context, body map[string]any, req Request) (int, error) {
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

	var jobs []imageJob
	for mi, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
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

	keyMissing := false
	results := make([]imageResult, len(jobs))
	needsVision := false

	if req.GetKey != nil {
		if k, ok := req.GetKey(); !ok || k == "" {
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
		needsVision = true
		if keyMissing {
			results[i] = imageResult{err: ErrNoVisionKey}
		}
	}

	if !needsVision {
		return d.applyDescriptions(msgs, jobs, results), nil
	}
	if keyMissing {
		logDescriptionsSkipped(jobs, results)
		return d.applyDescriptions(msgs, jobs, results), nil
	}

	var wg sync.WaitGroup

	for i, job := range jobs {
		if results[i].desc != "" {
			continue
		}

		wg.Add(1)
		go func(idx int, j imageJob) {
			defer wg.Done()

			val, err, _ := d.sfg.Do(j.hash, func() (any, error) {
				desc, err := d.describeImage(ctx, j.imgURL, j.hash, req)
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

	logDescriptionsSkipped(jobs, results)
	return d.applyDescriptions(msgs, jobs, results), nil
}

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

// CountImages returns how many image_url parts are present in body messages.
func CountImages(body map[string]any) int {
	if body == nil {
		return 0
	}
	msgs, ok := body["messages"].([]any)
	if !ok {
		return 0
	}
	n := 0
	for _, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if extractImageURL(p) != "" {
				n++
			}
		}
	}
	return n
}

func extractImageURL(part map[string]any) string {
	typ := stringField(part, "type")
	if typ != "image_url" {
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

func hashImageURL(url string) string {
	b := sha256.Sum256(imageContentForHash(url))
	return fmt.Sprintf("%x", b[:])
}

func imageContentForHash(url string) []byte {
	if strings.HasPrefix(url, "data:") {
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

func (d *Describer) describeImage(ctx context.Context, imgURL, hash string, req Request) (string, error) {
	var key string
	if req.GetKey != nil {
		k, ok := req.GetKey()
		if !ok || k == "" {
			return "", ErrNoVisionKey
		}
		key = k
	} else {
		return "", ErrNoVisionKey
	}

	reqBody := map[string]any{
		"model":  req.Model,
		"stream": false,
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
	if req.Provider == config.ProviderZai {
		reqBody["thinking"] = map[string]any{"type": "disabled"}
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("vision: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.ChatURL, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("vision: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	start := time.Now()
	resp, err := d.client.Do(httpReq)
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
		"model", req.Model,
		"provider", req.Provider,
		"desc_len", len(desc),
	)
	if d.record != nil {
		d.record(req.Provider, req.Model, completion.Usage.PromptTokens, completion.Usage.CompletionTokens, time.Since(start))
	}
	return desc, nil
}

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
