package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubFlashServer returns an httptest server that responds with a summary and
// a call counter for verifying cache hit/miss behavior.
func stubFlashServer(t *testing.T, summary string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var callCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": summary,
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &callCount
}

// stubErrorFlashServer returns an httptest server that always returns 500.
func stubErrorFlashServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "internal error"},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func stubDeepSeekKey() func() (string, bool) {
	return func() (string, bool) { return "ds-test-key", true }
}

func shortToolResult(content string) map[string]any {
	role, toolCallID, name := "tool", "call_123", "shell"
	return map[string]any{
		"role":         role,
		"content":      content,
		"tool_call_id": toolCallID,
		"name":         name,
	}
}

func userMessage(content string) map[string]any {
	return map[string]any{
		"role":    "user",
		"content": content,
	}
}

func bodyWithMessages(msgs ...map[string]any) map[string]any {
	raw := make([]any, len(msgs))
	for i, m := range msgs {
		raw[i] = m
	}
	return map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": raw,
	}
}

func longString(approxChars int) string {
	// Generate a repeatable long string for testing.
	base := "abcdefghijklmnopqrstuvwxyz0123456789\n"
	var sb strings.Builder
	for sb.Len() < approxChars {
		sb.WriteString(base)
	}
	return sb.String()
}

func TestCompress_NilCompressor(t *testing.T) {
	var c *Compressor
	body := bodyWithMessages(shortToolResult("hello"))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("nil compressor should not error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("nil compressor should compress nothing")
	}
}

func TestCompress_NoToolMessages(t *testing.T) {
	c := &Compressor{}
	body := bodyWithMessages(userMessage("hello"), userMessage("world"))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("expected 0 tool results compressed, got %d", stats.ToolResultsCompressed)
	}
}

func TestCompress_UnderThreshold(t *testing.T) {
	c := &Compressor{}
	short := longString(500) // well under 4000
	body := bodyWithMessages(shortToolResult(short))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("messages under threshold should not be compressed, got %d", stats.ToolResultsCompressed)
	}
	// Body should be unchanged.
	msgs := body["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(string)
	if content != short {
		t.Fatalf("content should be unchanged for under-threshold message")
	}
}

func TestCompress_OverThreshold(t *testing.T) {
	srv, count := stubFlashServer(t, "compressed summary of the long output")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	body := bodyWithMessages(shortToolResult(long))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 1 {
		t.Fatalf("expected 1 tool result compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 1 {
		t.Fatalf("expected 1 flash model call, got %d", count.Load())
	}
	msgs := body["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(content, "[Compressed tool result") {
		t.Fatalf("expected compressed prefix in content, got: %q", content)
	}
	if !strings.Contains(content, "compressed summary of the long output") {
		t.Fatalf("expected summary in content, got: %q", content)
	}
	if stats.CharsBefore <= stats.CharsAfter {
		t.Fatalf("expected chars_before (%d) > chars_after (%d)", stats.CharsBefore, stats.CharsAfter)
	}
}

func TestCompress_CacheHit(t *testing.T) {
	srv, count := stubFlashServer(t, "cached summary")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)

	// First call — should hit flash.
	body1 := bodyWithMessages(shortToolResult(long))
	stats1, err := c.Compress(t.Context(), body1)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if stats1.ToolResultsCompressed != 1 {
		t.Fatalf("first call: expected 1 compressed, got %d", stats1.ToolResultsCompressed)
	}
	if count.Load() != 1 {
		t.Fatalf("first call: expected 1 flash call, got %d", count.Load())
	}

	// Second call with same content — should be cache hit.
	body2 := bodyWithMessages(shortToolResult(long))
	stats2, err := c.Compress(t.Context(), body2)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if stats2.ToolResultsCompressed != 1 {
		t.Fatalf("second call: expected 1 compressed, got %d", stats2.ToolResultsCompressed)
	}
	if count.Load() != 1 {
		t.Fatalf("second call should be cache hit (flash not called again), got %d calls", count.Load())
	}
}

func TestCompress_CacheKeyStability(t *testing.T) {
	// Same content should produce same hash.
	s1 := "hello world"
	h1 := hashString(s1)
	h1a := hashString(s1)
	if h1 != h1a {
		t.Fatalf("same content should produce same hash")
	}
	// Different content should produce different hashes.
	h2 := hashString("goodbye world")
	if h1 == h2 {
		t.Fatalf("different content should produce different hashes")
	}
}

func TestCompress_CacheExpiry(t *testing.T) {
	srv, count := stubFlashServer(t, "expired summary")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	hash := hashString(long)

	// Store an expired entry in the cache.
	c.cache.Store(hash, compressCacheEntry{
		summary:   "old summary",
		expiresAt: time.Now().Add(-time.Second),
	})

	// Should call flash because the entry is expired.
	body := bodyWithMessages(shortToolResult(long))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 1 {
		t.Fatalf("expected 1 compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 1 {
		t.Fatalf("expired cache entry should trigger flash call, got %d", count.Load())
	}
}

func TestCompress_NoDeepSeekKey(t *testing.T) {
	c := &Compressor{
		cfg: CompressorConfig{
			GetAPIKey: func() (string, bool) { return "", false },
		},
		client: &http.Client{},
	}
	long := longString(30000)
	body := bodyWithMessages(shortToolResult(long))
	_, err := c.Compress(t.Context(), body)
	if err == nil {
		t.Fatal("expected error when DeepSeek key is missing")
	}
}

func TestCompress_FailOpen(t *testing.T) {
	// Fail-open: when the flash model returns an error, Compress returns the
	// error but the body should be left untouched (the caller sends original).
	srv := stubErrorFlashServer(t)
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	body := bodyWithMessages(shortToolResult(long))
	_, err := c.Compress(t.Context(), body)
	if err == nil {
		t.Fatal("expected error from flash model")
	}
	// Body should be unchanged — fail-open.
	msgs := body["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].(string)
	if content != long {
		t.Fatalf("fail-open: body should be unchanged, got different content")
	}
}

func TestCompress_MultipleToolResults(t *testing.T) {
	srv, count := stubFlashServer(t, "summary")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	// Use content with a distinct suffix to produce different hashes.
	long2 := long + "\nUNIQUE_APPENDIX_FOR_TEST"
	body := bodyWithMessages(shortToolResult(long), shortToolResult(long2))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 2 {
		t.Fatalf("expected 2 compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 2 {
		t.Fatalf("expected 2 flash calls (different content), got %d", count.Load())
	}
}

func TestCompress_ExactThreshold(t *testing.T) {
	c := &Compressor{}
	// Build a string of exactly toolResultMaxLen chars.
	var sb strings.Builder
	for sb.Len() < toolResultMaxLen {
		sb.WriteString("x")
	}
	exact := sb.String()
	if len(exact) != toolResultMaxLen {
		t.Fatalf("test setup: expected len=%d, got %d", toolResultMaxLen, len(exact))
	}
	body := bodyWithMessages(shortToolResult(exact))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exactly at threshold should NOT be compressed (we use > not >=).
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("exactly at threshold should not be compressed, got %d", stats.ToolResultsCompressed)
	}
}

func TestCompress_JustOverThreshold(t *testing.T) {
	srv, _ := stubFlashServer(t, "summary")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	// Build a tool result of exactly toolResultMaxLen+1 chars, plus a second
	// long message so the aggregate is above compressMinTotalChars (a single
	// just-over-threshold result would be skipped by the min-total gate).
	var sb strings.Builder
	for sb.Len() <= toolResultMaxLen {
		sb.WriteString("y")
	}
	justOver := sb.String()
	if len(justOver) != toolResultMaxLen+1 {
		t.Fatalf("test setup: expected len=%d, got %d", toolResultMaxLen+1, len(justOver))
	}
	seed := longString(compressMinTotalChars + 1) // guarantees we pass the min-total gate
	body := bodyWithMessages(shortToolResult(justOver), shortToolResult(seed))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// justOver exceeds the per-message threshold and the batch exceeds the
	// min-total gate, so it should compress (along with the seed tool result).
	if stats.ToolResultsCompressed < 1 {
		t.Fatalf("just over threshold should be compressed, got %d", stats.ToolResultsCompressed)
	}
}

// --- Verbatim-tool skip tests ---

// assistantWithToolCall creates a minimal assistant message with a tool_call
// that links to the given tool_call_id and name.
func assistantWithToolCall(id, name string) map[string]any {
	return map[string]any{
		"role":    "assistant",
		"content": "I'll read that file.",
		"tool_calls": []any{
			map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": "{}",
				},
			},
		},
	}
}

// toolMsg creates a tool message with role=tool, content, tool_call_id, and
// optional name. The name is omitted when empty (simulating the sanitizer's
// behavior of stripping the name field from messages after parsing).
func toolMsg(toolCallID, name, content string) map[string]any {
	m := map[string]any{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"content":      content,
	}
	if name != "" {
		m["name"] = name
	}
	return m
}

func TestCompress_SkipsFileReadToolResult(t *testing.T) {
	srv, count := stubFlashServer(t, "should not be called")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)

	// Simulate post-sanitizer body: assistant + tool, where tool name is
	// missing (sanitizer strips it). Detection via tool_call_id → assistant
	// tool_calls correlation.
	body := bodyWithMessages(
		assistantWithToolCall("call_abc", "read_file"),
		toolMsg("call_abc", "", long),
	)
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("file read tool result should not be compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 0 {
		t.Fatalf("flash summarizer should not be called for verbatim tool, got %d calls", count.Load())
	}
	// Content must be unchanged.
	msgs := body["messages"].([]any)
	content := msgs[1].(map[string]any)["content"].(string)
	if content != long {
		t.Fatalf("read_file content should be unchanged")
	}
}

func TestCompress_SkipsFileReadByNameField(t *testing.T) {
	srv, count := stubFlashServer(t, "should not be called")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)

	// Tool message with explicit name field (no assistant needed).
	body := bodyWithMessages(toolMsg("call_x", "read_file", long))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("read_file by name should not be compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 0 {
		t.Fatalf("flash summarizer should not be called, got %d", count.Load())
	}
}

func TestCompress_CompressesShellToolResult(t *testing.T) {
	srv, count := stubFlashServer(t, "shell output summary")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)

	// Shell tool results should still be compressed.
	body := bodyWithMessages(
		assistantWithToolCall("call_sh", "shell"),
		toolMsg("call_sh", "", long),
	)
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 1 {
		t.Fatalf("shell tool result should be compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 1 {
		t.Fatalf("flash summarizer should be called for shell, got %d", count.Load())
	}
}

func TestCompress_CompressesUnknownToolName(t *testing.T) {
	srv, count := stubFlashServer(t, "unknown tool summary")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)

	// A tool with no name and no matching assistant tool_calls → fall through
	// to compress (safe default — unknown tools are more likely
	// shell/test/log output than verbatim file content).
	body := bodyWithMessages(toolMsg("orphan_call", "", long))
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 1 {
		t.Fatalf("unknown tool without name should be compressed (safe default), got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 1 {
		t.Fatalf("flash summarizer should be called, got %d", count.Load())
	}
}

func TestCompress_SkipsGrepToolResult(t *testing.T) {
	srv, count := stubFlashServer(t, "should not be called")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	body := bodyWithMessages(
		assistantWithToolCall("call_g", "Grep"),
		toolMsg("call_g", "", long),
	)
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("grep tool result should not be compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 0 {
		t.Fatalf("flash summarizer should not be called for grep, got %d", count.Load())
	}
}

func TestCompress_SkipsMcpPrefixedToolName(t *testing.T) {
	srv, count := stubFlashServer(t, "should not be called")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	body := bodyWithMessages(
		assistantWithToolCall("call_mcp", "mcp.fs.read_file"),
		toolMsg("call_mcp", "", long),
	)
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("mcp.__read_file tool result should not be compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 0 {
		t.Fatalf("flash summarizer should not be called, got %d", count.Load())
	}
}

func TestCompress_SkipsWebFetchToolResult(t *testing.T) {
	srv, count := stubFlashServer(t, "should not be called")
	c := &Compressor{
		cfg: CompressorConfig{
			ChatURLOverride: srv.URL + "/chat/completions",
			GetAPIKey:       stubDeepSeekKey(),
		},
		client: srv.Client(),
	}
	long := longString(30000)
	body := bodyWithMessages(
		assistantWithToolCall("call_wf", "WebFetch"),
		toolMsg("call_wf", "", long),
	)
	stats, err := c.Compress(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.ToolResultsCompressed != 0 {
		t.Fatalf("WebFetch tool result should not be compressed, got %d", stats.ToolResultsCompressed)
	}
	if count.Load() != 0 {
		t.Fatalf("flash summarizer should not be called for WebFetch, got %d", count.Load())
	}
}
