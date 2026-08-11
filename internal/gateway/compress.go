// Package gateway implements compression of verbose tool results to reduce
// token cost. It uses a cheap summarizer model (deepseek-v4-flash) with a
// content-hash cache, singleflight deduplication, and semaphore-bounded
// concurrency.
//
// The compressor is fail-open: on any error (flash model timeout, upstream
// rejection, cancellation), it returns the original body unchanged.
// Compression is opt-in via the usage dashboard toggle.
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

	// compressCacheTTL is the cache lifetime for tool-result summaries.
	compressCacheTTL = 30 * time.Minute

	// compressMaxConcurrent is the semaphore limit for concurrent summarizer
	// calls to deepseek-v4-flash.
	compressMaxConcurrent = 4

	// compressRequestPath is the chat completions path appended to the flash
	// model's base URL.
	compressRequestPath = "/chat/completions"
)

// toolResultSummarizePrompt is the instruction sent to the cheap summarizer
// model when compressing a verbose tool result.
const toolResultSummarizePrompt = `Summarize the key information in this tool output for a coding assistant.
Preserve: file paths, error messages, line numbers, test results, and
critical values. Remove: verbose logs, formatting noise, and repetition.
Keep it concise but complete — do not drop any actionable detail.`

// verbatimToolNames is the set of tool names whose output must never be
// compressed — the model needs the full verbatim content to reason and edit
// precisely. List is case-insensitive and matches after stripping common
// prefixes (mcp., tools., custom.) and trailing .json suffix.
//
// File readers, search tools, version-control diffs, web fetches, and patch
// tools are excluded. Shell/terminal output, lint logs, test runners, DB
// queries, and similar verbose-but-disposable output remains compressible.
var verbatimToolNames = map[string]bool{
	// File read tools
	"read": true, "read_file": true, "readfile": true,
	"get_file": true, "getfile": true, "read_text_file": true,
	"open_file": true, "openfile": true,
	// Code search / find tools
	"search": true, "grep": true, "find": true, "find_files": true,
	"search_code": true, "search_files": true, "search_content": true,
	"search_file": true, "searchfile": true,
	"search_files_in_path": true, "find_files_in_path": true,
	// Web fetch tools
	"fetch_url": true, "fetchurl": true, "fetch": true,
	"web_fetch": true, "webfetch": true, "get_url": true,
	"read_url": true, "readurl": true,
	// Version control tools (diffs / blame — model needs exact context)
	"diff": true, "blame": true, "git_diff": true, "git_blame": true,
	"git_diff_staged": true,
	// Patch / edit content tools (the diff/patch content is what the model produced)
	"apply_patch": true, "applypatch": true,
}

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
}

// Compressor compresses verbose tool results and long conversation histories.
type Compressor struct {
	cfg    CompressorConfig
	client *http.Client

	cache sync.Map           // sha256(content) → cacheEntry
	sfg   singleflight.Group // deduplicate concurrent calls for the same hash
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
}

// NewCompressor creates a Compressor. If cfg.Enabled is false, subsequent
// calls to Compress are no-ops.
func NewCompressor(cfg CompressorConfig, client *http.Client) *Compressor {
	if !cfg.Enabled {
		return nil
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	return &Compressor{
		cfg:    cfg,
		client: client,
	}
}

// isVerbatimTool reports whether a tool message's output must NOT be
// compressed because it carries file/source/search/diff content the model
// needs verbatim to reason and edit precisely.
//
// It resolves the tool name from two sources (in priority order):
//  1. The message's own "name" field (present when the sanitizer preserves it).
//  2. Correlating the message's "tool_call_id" against the preceding assistant
//     message's tool_calls[].function.name via nameMap.
//
// Returns false when no name can be resolved, defaulting to "compress" (safe
// for unknown tools since shell/test output is the common case).
func isVerbatimTool(msg map[string]any, nameMap map[string]string) bool {
	// Priority 1: direct name field on the tool message.
	if name := stringField(msg, "name"); name != "" {
		return isVerbatimToolName(name)
	}
	// Priority 2: correlate tool_call_id → assistant tool_calls name.
	if nameMap == nil {
		return false
	}
	callID := stringField(msg, "tool_call_id")
	if callID == "" {
		return false
	}
	name := nameMap[callID]
	if name == "" {
		return false
	}
	return isVerbatimToolName(name)
}

// isVerbatimToolName checks whether the resolved tool name belongs to the
// verbatim set. Matching is case-insensitive. Common prefixes (mcp., tools.,
// custom.) and trailing .json are stripped before lookup.
func isVerbatimToolName(name string) bool {
	n := strings.TrimSuffix(strings.ToLower(name), ".json")
	// Strip common tool-name prefixes.
	for _, prefix := range []string{"mcp.", "mcp__", "tools.", "custom."} {
		if strings.HasPrefix(n, prefix) {
			n = n[len(prefix):]
			break
		}
	}
	// Fast path: exact match.
	if verbatimToolNames[n] {
		return true
	}
	// Substring match: a qualified name like "mcp__read_file" → "read_file".
	for _, prefix := range []string{"read_", "read", "get_", "get", "search_", "search",
		"grep", "find_", "find", "fetch_", "fetch", "diff", "blame", "apply_", "applypatch"} {
		if strings.Contains(n, prefix) && verbatimToolNames[prefix] {
			return true
		}
	}
	return false
}

// buildToolNameMap scans all messages and builds a map from tool_call_id to
// the resolved function name from assistant tool_calls. Used by isVerbatimTool
// for tool messages whose "name" field was stripped by the sanitizer.
func buildToolNameMap(msgs []any) map[string]string {
	out := make(map[string]string)
	for _, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}
		calls, _ := msg["tool_calls"].([]any)
		for _, rawCall := range calls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				continue
			}
			id := stringField(call, "id")
			if id == "" {
				continue
			}
			if fn, ok := call["function"].(map[string]any); ok {
				if fnName, ok := fn["name"].(string); ok && fnName != "" {
					out[id] = fnName
				}
			}
		}
	}
	return out
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

	// Fast scan: check if there's any tool message worth compressing.
	// We check two things with a cheap heuristic that doesn't need
	// buildToolNameMap:
	//  1. Content exceeds the length threshold.
	//  2. The tool's "name" field (if present) is NOT verbatim.
	//
	// This catches the overwhelming majority of cases: all Cursor tool
	// messages carry a "name" field with well-known values like "read",
	// "grep", "search", etc. If a tool message lacks a name we cannot
	// check cheaply, but we'll catch it in the second pass below after
	// building the name map.
	//
	// For requests where every tool result is either short or a named
	// verbatim tool (file read, search, diff), we return immediately
	// without paying the cost of buildToolNameMap.
	hasCandidate := false
	for _, raw := range msgs {
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
		// Cheap check: if the tool has a direct "name", check verbatim.
		if name := stringField(msg, "name"); name != "" && isVerbatimToolName(name) {
			continue
		}
		hasCandidate = true
		break
	}
	if !hasCandidate {
		return CompressionStats{}, nil
	}

	// We have at least one candidate. Build the full name map so the
	// second pass can correlate tool_call_id → function name for tools
	// whose "name" field was stripped.
	nameMap := buildToolNameMap(msgs)

	// Second pass: collect jobs for the compressible tool messages,
	// using the full name map for precise verbatim checking.
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
		if isVerbatimTool(msg, nameMap) {
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
