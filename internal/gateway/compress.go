// Package gateway implements compression of verbose tool results and long
// conversation histories to reduce token cost. Both strategies use a cheap
// summarizer model (deepseek-v4-flash) and share a content-hash cache with
// singleflight deduplication and semaphore-bounded concurrency.
//
// The compressor is fail-open: on any error (flash model timeout, upstream
// rejection, cancellation), it returns the original body unchanged.
// Compression is opt-in via --compress CLI flag.
package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// toolResultMaxLen is the threshold (in chars) at which a tool message is
	// compressed. Messages shorter than this are left verbatim.
	toolResultMaxLen = 4000

	// compressCacheTTL is the cache lifetime for tool-result and history
	// summaries. Longer than vision's 10 min because conversations persist
	// across many turns.
	compressCacheTTL = 30 * time.Minute

	// compressMaxConcurrent is the semaphore limit for concurrent summarizer
	// calls to deepseek-v4-flash.
	compressMaxConcurrent = 4

	// compressRequestPath is the chat completions path appended to the flash
	// model's base URL.
	compressRequestPath = "/chat/completions"

	// historyMsgThreshold is the message count at which history compression
	// triggers. When the message count exceeds this, the middle range is
	// summarized.
	historyMsgThreshold = 30

	// recentMsgsToKeep is the number of most recent messages preserved
	// verbatim during history compression.
	recentMsgsToKeep = 10

	// systemMsgsToKeep is the number of leading system/developer messages
	// kept verbatim during history compression.
	systemMsgsToKeep = 2
)

// toolResultSummarizePrompt is the instruction sent to the cheap summarizer
// model when compressing a verbose tool result.
const toolResultSummarizePrompt = `Summarize the key information in this tool output for a coding assistant.
Preserve: file paths, error messages, line numbers, test results, and
critical values. Remove: verbose logs, formatting noise, and repetition.
Keep it concise but complete — do not drop any actionable detail.`

// historySummarizePrompt is the instruction sent to deepseek-v4-flash when
// compressing the middle range of a long conversation history.
const historySummarizePrompt = `Summarize this conversation history for a coding assistant.
Preserve: decisions made, files modified, errors encountered, architecture
decisions, and open questions. Include the reasoning behind key decisions.
Do NOT drop any actionable detail. Be concise but complete.`

// CompressorConfig configures the Compressor.
type CompressorConfig struct {
	Enabled bool
	// ChatURL is the deepseek-v4-flash endpoint base (not including /chat/completions).
	// Defaults to DeepSeek base URL when empty.
	ChatURL string
	// GetAPIKey is a callback that returns the DeepSeek API key (or "", false).
	GetAPIKey func() (string, bool)
	// ChatURLOverride is for test use only.
	ChatURLOverride string

	// ShadowMode (3a only): when true, CompressHistory computes what it would
	// compress, logs the stats, but does NOT mutate the body.
	ShadowMode bool
}

// Compressor compresses verbose tool results and long conversation histories.
type Compressor struct {
	cfg    CompressorConfig
	client *http.Client

	cache sync.Map           // sha256(content) → cacheEntry
	sfg   singleflight.Group // deduplicate concurrent calls for the same hash

	mu      sync.Mutex
	counter int // per-request tool-result counter for logging
}

type compressCacheEntry struct {
	summary   string
	expiresAt time.Time
}

// CompressionStats records what was compressed in a single request cycle.
type CompressionStats struct {
	ToolResultsCompressed int
	CharsBefore           int
	CharsAfter            int

	// History compression stats (3a).
	HistoryMsgsCompressed int
	HistoryCharsBefore    int
	HistoryCharsAfter     int
}

// NewCompressor creates a Compressor. If cfg.Enabled is false, subsequent
// calls to Compress and CompressHistory are no-ops.
func NewCompressor(cfg CompressorConfig, client *http.Client) *Compressor {
	if !cfg.Enabled {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Compressor{
		cfg:    cfg,
		client: client,
	}
}

// Compress walks body["messages"], finds role:tool messages exceeding the
// length threshold, and replaces their content in-place with a compressed
// summary. Returns compression stats. On any error, returns the error and
// zero stats — the caller logs the error and sends the original body.
func (c *Compressor) Compress(ctx context.Context, body map[string]any) (CompressionStats, error) {
	if c == nil {
		return CompressionStats{}, nil
	}

	msgs, ok := body["messages"].([]any)
	if !ok {
		return CompressionStats{}, nil
	}

	c.mu.Lock()
	c.counter = 0
	c.mu.Unlock()

	// Collect jobs for tool messages exceeding the threshold.
	type compressJob struct {
		idx     int
		content string
		hash    string
	}
	var jobs []compressJob
	for i, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}
		content, ok := msg["content"].(string)
		if !ok || len(content) <= toolResultMaxLen {
			continue
		}

		h := hashString(content)
		jobs = append(jobs, compressJob{idx: i, content: content, hash: h})
	}

	if len(jobs) == 0 {
		return CompressionStats{}, nil
	}

	var (
		stats    CompressionStats
		results  = make([]string, len(jobs))
		sem      = make(chan struct{}, compressMaxConcurrent)
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)

	for i, job := range jobs {
		// Check cache first (fast path outside semaphore).
		if v, ok := c.cache.Load(job.hash); ok {
			entry := v.(compressCacheEntry)
			if time.Now().Before(entry.expiresAt) {
				results[i] = entry.summary
				slog.Debug("compress: tool_result_cache_hit",
					"original_len", len(job.content),
					"compressed_len", len(entry.summary),
				)
				continue
			}
			c.cache.Delete(job.hash)
		}

		wg.Add(1)
		go func(idx int, j compressJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			val, err, _ := c.sfg.Do(j.hash, func() (any, error) {
				summary, err := c.summarizeContent(ctx, j.content)
				if err != nil {
					return nil, err
				}
				c.cache.Store(j.hash, compressCacheEntry{
					summary:   summary,
					expiresAt: time.Now().Add(compressCacheTTL),
				})
				return summary, nil
			})
			if err != nil {
				errOnce.Do(func() { firstErr = err })
				return
			}
			results[idx] = val.(string)
		}(i, job)
	}
	wg.Wait()

	if firstErr != nil {
		return CompressionStats{}, firstErr
	}

	// Replace tool-result contents in-place.
	for i, job := range jobs {
		prefix := fmt.Sprintf("[Compressed tool result — %d chars original]\n", len(job.content))
		summary := results[i]
		msgs[job.idx].(map[string]any)["content"] = prefix + summary
		stats.CharsBefore += len(job.content)
		stats.CharsAfter += len(prefix) + len(summary)
		stats.ToolResultsCompressed++
	}

	return stats, nil
}

// CompressHistory compresses the middle range of a long conversation when the
// message count exceeds historyMsgThreshold. It preserves leading system
// messages and the most recent messages verbatim, replacing the middle range
// with a single summarized system message.
//
// When ShadowMode is enabled, it computes what it would compress and logs the
// stats, but does not mutate the body — this lets callers validate compression
// quality without affecting requests.
//
// Fail-open: on any error, returns the original body unchanged.
func (c *Compressor) CompressHistory(ctx context.Context, body map[string]any) (CompressionStats, error) {
	if c == nil {
		return CompressionStats{}, nil
	}

	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) <= historyMsgThreshold {
		return CompressionStats{}, nil
	}

	msgCount := len(msgs)

	// Partition: keep leading system/developer messages + trailing recent
	// messages. The middle range is the compression window.
	sysEnd := 0
	for i, raw := range msgs {
		if sysEnd >= systemMsgsToKeep {
			break
		}
		msg, ok := raw.(map[string]any)
		if !ok {
			break
		}
		role, _ := msg["role"].(string)
		if role == "system" || role == "developer" {
			sysEnd = i + 1
		} else {
			break
		}
	}

	recentStart := msgCount - recentMsgsToKeep
	// Ensure we don't overlap: middle range must be non-empty.
	if sysEnd >= recentStart {
		return CompressionStats{}, nil
	}

	middle := msgs[sysEnd:recentStart]
	if len(middle) == 0 {
		return CompressionStats{}, nil
	}

	// Build a hash of the middle range for caching. Use the JSON serialization
	// of the middle range, excluding any volatile fields.
	middleHash, middleChars := middleHash(middle)

	var (
		stats   CompressionStats
		summary string
	)

	if v, ok := c.cache.Load(middleHash); ok {
		entry := v.(compressCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			summary = entry.summary
			slog.Debug("compress: history_cache_hit",
				"msg_count", msgCount,
				"middle_count", len(middle),
				"middle_chars", middleChars,
			)
		} else {
			c.cache.Delete(middleHash)
		}
	}

	if summary == "" {
		// Cache miss — call flash summarizer.
		var err error
		summary, err = c.summarizeHistory(ctx, middle)
		if err != nil {
			slog.Warn("compress: history_summarize_failed", "err", err)
			return CompressionStats{}, err
		}
		c.cache.Store(middleHash, compressCacheEntry{
			summary:   summary,
			expiresAt: time.Now().Add(compressCacheTTL),
		})
	}

	prefix := fmt.Sprintf("[Compressed conversation history — %d messages, %d chars original]\n", len(middle), middleChars)
	replacement := prefix + summary

	stats.HistoryMsgsCompressed = len(middle)
	stats.HistoryCharsBefore = middleChars
	stats.HistoryCharsAfter = len(replacement)

	if c.cfg.ShadowMode {
		slog.Info("compress: history_shadow",
			"msg_count", msgCount,
			"middle_count", len(middle),
			"middle_chars", middleChars,
			"compressed_chars", len(replacement),
		)
		return stats, nil
	}

	// Rebuild messages: [system msgs] + [compressed summary] + [recent msgs]
	compressedMsg := map[string]any{
		"role":    "system",
		"content": replacement,
	}

	recent := msgs[recentStart:]
	system := msgs[:sysEnd]

	newMsgs := make([]any, 0, 1+len(system)+len(recent))
	newMsgs = append(newMsgs, system...)
	newMsgs = append(newMsgs, compressedMsg)
	newMsgs = append(newMsgs, recent...)
	body["messages"] = newMsgs

	slog.Info("compress: history",
		"msg_count", msgCount,
		"middle_count", len(middle),
		"middle_chars", middleChars,
		"compressed_chars", len(replacement),
	)

	return stats, nil
}

// middleHash computes a stable SHA-256 hash for the middle range of messages,
// excluding volatile fields. Returns the hex hash and the total char count.
func middleHash(msgs []any) (hash string, chars int) {
	// Serialize each message as JSON, stripping volatile fields.
	var sb strings.Builder
	for _, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Shallow copy excluding volatile fields (any field whose key contains
		// "timestamp" or "request_id").
		cleaned := make(map[string]any, len(msg))
		for k, v := range msg {
			kl := strings.ToLower(k)
			if strings.Contains(kl, "timestamp") || strings.Contains(kl, "request_id") {
				continue
			}
			cleaned[k] = v
			if s, ok := v.(string); k == "content" && ok {
				chars += len(s)
			}
		}
		b, _ := json.Marshal(cleaned)
		sb.Write(b)
	}
	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:]), chars
}

// summarizeHistory calls deepseek-v4-flash to summarize the middle range of
// a conversation history.
func (c *Compressor) summarizeHistory(ctx context.Context, middle []any) (string, error) {
	key, ok := c.cfg.GetAPIKey()
	if !ok || key == "" {
		return "", fmt.Errorf("compress: no DeepSeek API key configured")
	}

	// Format the middle messages as a user-readable transcript.
	var transcript strings.Builder
	for _, raw := range middle {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if content == "" {
			// Tool calls are JSON arrays; serialize them.
			if tc, ok := msg["tool_calls"]; ok {
				b, _ := json.Marshal(tc)
				content = string(b)
			}
		}
		fmt.Fprintf(&transcript, "[%s]: %s\n", role, content)
	}

	reqBody := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []map[string]any{
			{"role": "system", "content": historySummarizePrompt},
			{"role": "user", "content": transcript.String()},
		},
		"max_tokens":  2000,
		"temperature": 0,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("compress: marshal history request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.compressChatURL(), bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("compress: create history request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("compress: send history request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("compress: read history response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("compress: history upstream returned %d: %s", resp.StatusCode, trimTo(body, 500))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("compress: unmarshal history response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("compress: empty history response from flash model")
	}

	return strings.TrimSpace(chatResp.Choices[0].Message.Content), nil
}

// compressChatURL returns the flash model's chat completions URL.
func (c *Compressor) compressChatURL() string {
	if c.cfg.ChatURLOverride != "" {
		return c.cfg.ChatURLOverride
	}
	if c.cfg.ChatURL != "" {
		return c.cfg.ChatURL + compressRequestPath
	}
	// Default: DeepSeek flash endpoint. The caller should set ChatURL to the
	// DeepSeek base URL via config.DefaultDeepSeekBaseURL.
	return "https://api.deepseek.com/chat/completions"
}

// summarizeContent calls deepseek-v4-flash to summarize the given content.
// Uses a brief system prompt for compression.
func (c *Compressor) summarizeContent(ctx context.Context, content string) (string, error) {
	key, ok := c.cfg.GetAPIKey()
	if !ok || key == "" {
		return "", fmt.Errorf("compress: no DeepSeek API key configured")
	}

	reqBody := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []map[string]any{
			{"role": "system", "content": toolResultSummarizePrompt},
			{"role": "user", "content": content},
		},
		"max_tokens":  1000,
		"temperature": 0,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("compress: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.compressChatURL(), bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("compress: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("compress: send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("compress: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("compress: upstream returned %d: %s", resp.StatusCode, trimTo(body, 500))
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("compress: unmarshal response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("compress: empty response from flash model")
	}

	summary := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("compress: flash model returned empty summary (content %d chars)", len(content))
	}
	if len(summary) >= len(content) {
		slog.Warn("compress: summary is not shorter than original — skipping", "content_len", len(content), "summary_len", len(summary))
		return "", fmt.Errorf("compress: summary not shorter than original (%d >= %d)", len(summary), len(content))
	}
	return summary, nil
}

func trimTo(b []byte, maxLen int) string {
	s := string(b)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
