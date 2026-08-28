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

	"github.com/commoddity/discursive/internal/config"
	"golang.org/x/sync/singleflight"
)

const (
	// toolResultMaxLen is the threshold (in chars) at which a tool message is
	// toolResultMaxLen is the threshold (in chars) at which a tool message is
	// compressed. Messages shorter than this are left verbatim. Tuned to 24,000:
	// a summarizer round-trip (1–3s + flash cost) only pays for itself on
	// substantial results; below ~3k tokens of content the compressed form
	// saves little, especially when the conversation prefix is mostly
	// cache-hit input billed at a discount.
	toolResultMaxLen = 24_000

	// compressCacheTTL is the cache lifetime for tool-result summaries.
	compressCacheTTL = 30 * time.Minute

	// compressMaxConcurrent is the semaphore limit for concurrent summarizer
	// calls to deepseek-v4-flash.
	compressMaxConcurrent = 4

	// compressMinTotalChars is the minimum total characters across all
	// compressible tool messages required to justify the summarizer
	// round-trip. Below this threshold, the token savings (especially when
	// the conversation prefix is mostly cache-hit input billed at a discount)
	// do not offset the 1–3s latency and flash-model cost.
	compressMinTotalChars = 20000
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
	// RecordUsage is an optional best-effort usage meter called after each
	// successful summarizer call with provider, real model id, prompt/completion
	// token counts, and round-trip latency. May be nil (disabled).
	RecordUsage func(provider config.Provider, model string, promptTokens, completionTokens uint64, latency time.Duration)
}

// CompressContext selects the summarizer endpoint for one request.
type CompressContext struct {
	Provider  config.Provider
	Model     string
	ChatURL   string
	APIKey    string
	SessionID string
}

// Compressor compresses verbose tool results and long conversation histories.
type Compressor struct {
	cfg         CompressorConfig
	client      *http.Client
	recordUsage func(provider config.Provider, model string, promptTokens, completionTokens uint64, latency time.Duration)

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
	SummarizerCalls       int
	CacheHits             int
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
		cfg:         cfg,
		client:      client,
		recordUsage: cfg.RecordUsage,
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
func (c *Compressor) Compress(ctx context.Context, body map[string]any, cctx CompressContext) (CompressionStats, error) {
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
	//
	// We also track the total compressible chars so we can skip marginal
	// gains entirely — a 1–3s summarizer round-trip only pays for itself
	// when there's substantial content to compress.
	hasCandidate := false
	var totalCompressibleChars int
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
		totalCompressibleChars += len(content)
	}
	if !hasCandidate {
		return CompressionStats{}, nil
	}
	// Skip compression entirely when the total compressible content is below
	// the minimum — the summarizer round-trip doesn't pay for itself on small
	// gains, especially when most conversation input is cache-hit (discounted).
	if totalCompressibleChars < compressMinTotalChars {
		slog.Debug("compress: skipped — total compressible below threshold",
			"total_compressible", totalCompressibleChars,
			"threshold", compressMinTotalChars)
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
				stats.CacheHits++
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

			val, err, shared := c.sfg.Do(j.hash, func() (any, error) {
				summary, err := c.summarizeContent(ctx, j.content, cctx)
				if err != nil {
					return nil, err
				}
				c.cache.Store(j.hash, compressCacheEntry{
					summary:   summary,
					expiresAt: time.Now().Add(compressCacheTTL),
				})
				return summary, nil
			})
			if err == nil && !shared {
				stats.SummarizerCalls++
			}
			if err != nil {
				// Empty/unshorter summaries are soft failures: leave a zero
				// result so the caller truncates instead of failing open.
				// Hard errors (network, auth, upstream) set firstErr to abort.
				if softCompressFailure(err) {
					results[idx] = ""
					return
				}
				errOnce.Do(func() { firstErr = err })
				return
			}
			results[idx] = val.(string)
		}(i, job)
	}
	wg.Wait()

	// Replace tool-result contents in-place. Summarizer failures (empty
	// result) don't abort the batch — we truncate that message to a safe
	// prefix instead of failing open, since we already paid the round-trip
	// latency/cost. Hard errors (firstErr) still abort below.
	if firstErr != nil {
		return CompressionStats{}, firstErr
	}

	// Replace tool-result contents in-place. For summarizer failures
	// (empty results), fall back to truncation rather than failing open —
	// we already paid the latency/cost of the round-trip, so truncate
	// preserves some of the benefit without sending the full original.
	for i, job := range jobs {
		summary := results[i]
		if summary == "" {
			// Summarizer returned empty — truncate to a safe prefix instead
			// of failing open and sending the full original content.
			trunc := job.content
			if len(trunc) > toolResultMaxLen {
				trunc = trunc[:toolResultMaxLen] + "\n... [truncated due to summarizer failure]"
			}
			msgs[job.idx].(map[string]any)["content"] = trunc
			stats.CharsBefore += len(job.content)
			stats.CharsAfter += len(trunc)
			slog.Debug("compress: summarizer failed, truncated original",
				"original_len", len(job.content),
				"truncated_len", len(trunc))
			continue
		}
		prefix := fmt.Sprintf("[Compressed tool result — %d chars original]\n", len(job.content))
		msgs[job.idx].(map[string]any)["content"] = prefix + summary
		stats.CharsBefore += len(job.content)
		stats.CharsAfter += len(prefix) + len(summary)
		stats.ToolResultsCompressed++
	}

	return stats, nil
}

// summarizeContent calls the provider's small model to summarize content.
func (c *Compressor) summarizeContent(ctx context.Context, content string, cctx CompressContext) (string, error) {
	if cctx.APIKey == "" {
		return "", fmt.Errorf("compress: no API key configured")
	}

	reqBody := map[string]any{
		"model": cctx.Model,
		"messages": []map[string]any{
			{"role": "system", "content": toolResultSummarizePrompt},
			{"role": "user", "content": content},
		},
		"max_tokens":  1000,
		"temperature": 0,
	}
	applyOpenRouterRouting(reqBody, Route{Provider: cctx.Provider}, DefaultSanitizeConfig())
	applyOpenRouterSession(reqBody, cctx.Provider, cctx.SessionID)

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("compress: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cctx.ChatURL, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("compress: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cctx.APIKey)
	applyOpenRouterRequestHeaders(req.Header, cctx.ChatURL, reqBody)

	start := time.Now()
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
		Usage map[string]any `json:"usage"`
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

	// Best-effort usage metering; never fail the primary request on a record error.
	if c.recordUsage != nil && chatResp.Usage != nil {
		tu := parseUsageObject(chatResp.Usage)
		c.recordUsage(cctx.Provider, cctx.Model, tu.PromptTokens, tu.CompletionTokens, time.Since(start))
	}
	return summary, nil
}

// softCompressFailure reports whether a summarizer error is a "soft" failure
// (empty summary, or summary not shorter than original) that should fall back
// to truncating the original tool result rather than aborting the whole
// compression batch. Hard errors (network, auth, upstream status) are not soft
// and abort the batch. Returns false for nil.
func softCompressFailure(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "empty summary") ||
		strings.Contains(err.Error(), "summary not shorter")
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
