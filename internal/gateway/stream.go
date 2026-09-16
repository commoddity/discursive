package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

type tokenUsage struct {
	PromptTokens     uint64
	CompletionTokens uint64
	CacheHitTokens   uint64
	CacheMissTokens  uint64
}

func parseUsageObject(u map[string]any) tokenUsage {
	var out tokenUsage
	out.PromptTokens = uint64Field(u, "prompt_tokens")
	out.CompletionTokens = uint64Field(u, "completion_tokens")

	// DeepSeek returns separate hit/miss fields.
	out.CacheHitTokens = uint64Field(u, "prompt_cache_hit_tokens")
	if out.CacheHitTokens == 0 {
		out.CacheHitTokens = uint64Field(u, "cache_hit_tokens")
	}
	out.CacheMissTokens = uint64Field(u, "prompt_cache_miss_tokens")
	if out.CacheMissTokens == 0 {
		out.CacheMissTokens = uint64Field(u, "cache_miss_tokens")
	}

	// Kimi/Moonshot returns a single "cached_tokens" (top-level + nested).
	if out.CacheHitTokens == 0 && out.CacheMissTokens == 0 {
		cached := uint64Field(u, "cached_tokens")
		if cached == 0 {
			if d, ok := u["prompt_tokens_details"].(map[string]any); ok {
				cached = uint64Field(d, "cached_tokens")
			}
		}
		if cached > 0 {
			out.CacheHitTokens = cached
			// Derive miss from total prompt minus cached.
			if cached < out.PromptTokens {
				out.CacheMissTokens = out.PromptTokens - cached
			}
		}
	}

	// OpenRouter reports cache write/hit inside prompt_tokens_details when it
	// exposes them at all. cache_write_tokens indicates newly-cached (miss) work.
	if details, ok := u["prompt_tokens_details"].(map[string]any); ok {
		if writeTokens := uint64Field(details, "cache_write_tokens"); writeTokens > 0 {
			out.CacheMissTokens = writeTokens
		}
		if cached := uint64Field(details, "cached_tokens"); cached > 0 {
			out.CacheHitTokens = cached
		}
	}

	return out
}

func uint64Field(m map[string]any, key string) uint64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	n, ok := jsonNumberInt(v)
	if !ok || n < 0 {
		return 0
	}
	return uint64(n)
}

func (s *Server) recordUsage(provider config.Provider, model, effort, requestID string, lat time.Duration, u tokenUsage) {
	if u.CacheHitTokens == 0 && u.CacheMissTokens == 0 && u.PromptTokens > 1024 {
		slog.Debug("usage: no cache tokens reported by upstream",
			"request_id", requestID,
			"provider", string(provider),
			"model", model,
			"effort", effort,
			"prompt_tokens", u.PromptTokens,
		)
	}
	ev := usage.Event{
		SessionID:        s.sessionID,
		Provider:         provider,
		Model:            model,
		Effort:           effort,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CacheHitTokens:   u.CacheHitTokens,
		CacheMissTokens:  u.CacheMissTokens,
		RequestID:        requestID,
		LatencyMS:        uint64(lat.Milliseconds()),
	}
	if err := s.store.RecordAndObserve(s.agg, ev); err != nil {
		logRequest(requestID, "usage_record_error", err.Error(), "effort", effort)
	}
}

// recordAuxUsage records usage for auxiliary worker calls (vision description,
// tool-result compression). These use their own fixed sentinel session ids so
// they meter into the per-day/provider/model aggregates but form their own
// session row in the UI (never the active chat session). Best-effort: errors
// are logged, never propagated to the primary request.
func (s *Server) recordAuxUsage(sessionID string, provider config.Provider, model, requestID string, lat time.Duration, u tokenUsage) {
	ev := usage.Event{
		SessionID:        sessionID,
		Provider:         provider,
		Model:            model,
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CacheHitTokens:   u.CacheHitTokens,
		CacheMissTokens:  u.CacheMissTokens,
		RequestID:        requestID,
		LatencyMS:        uint64(lat.Milliseconds()),
	}
	if err := s.store.RecordAndObserve(s.agg, ev); err != nil {
		slog.Warn("aux usage record failed", "request_id", requestID, "provider", string(provider), "model", model, "err", err)
	}
}

// sseUsageScanner extracts usage from streamed SSE chunks.
type sseUsageScanner struct {
	mu     sync.Mutex
	buf    strings.Builder
	usage  *tokenUsage
	found  bool
	orHost string
	err    *modelNotAvailableError // set when SSE chunk contains an error
}

type modelNotAvailableError struct {
	message string
}

func (sc *sseUsageScanner) feed(p []byte) {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.buf.Write(p)
	data := sc.buf.String()
	for {
		idx := strings.Index(data, "\n")
		if idx < 0 {
			sc.buf.Reset()
			sc.buf.WriteString(data)
			return
		}
		line := strings.TrimSpace(data[:idx])
		data = data[idx+1:]
		sc.consumeLine(line)
	}
}

func (sc *sseUsageScanner) snapshot() (found bool, usage *tokenUsage, orHost string, err *modelNotAvailableError) {
	if sc == nil {
		return
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.found, sc.usage, sc.orHost, sc.err
}

func (sc *sseUsageScanner) consumeLine(line string) {
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(payload), &obj); err != nil {
		return
	}
	if u, ok := obj["usage"].(map[string]any); ok {
		parsed := parseUsageObject(u)
		sc.usage = &parsed
		sc.found = true
	}
	if sc.orHost == "" {
		sc.orHost = openRouterHostFromObject(obj)
	}
	if sc.err == nil {
		sc.err = extractModelNotAvailableError(obj)
	}
}

func extractModelNotAvailableError(obj map[string]any) *modelNotAvailableError {
	e, ok := obj["error"].(map[string]any)
	if !ok {
		return nil
	}
	msg, ok := e["message"].(string)
	if !ok {
		return nil
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not available") ||
		(strings.Contains(lower, "model") && (strings.Contains(lower, "not found") || strings.Contains(lower, "not available"))) {
		return &modelNotAvailableError{message: msg}
	}
	return nil
}

func synthesizeSSE(completion map[string]any) []byte {
	id, _ := completion["id"].(string)
	if id == "" {
		id = "chatcmpl-synth"
	}
	model, _ := completion["model"].(string)
	content := ""
	if choices, ok := completion["choices"].([]any); ok && len(choices) > 0 {
		if ch, ok := choices[0].(map[string]any); ok {
			if msg, ok := ch["message"].(map[string]any); ok {
				if c, ok := msg["content"].(string); ok {
					content = c
				}
			}
		}
	}

	var buf bytes.Buffer
	writeChunk := func(delta map[string]any, finish any) {
		chunk := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{
				map[string]any{
					"index":         0,
					"delta":         delta,
					"finish_reason": finish,
				},
			},
		}
		if u, ok := completion["usage"]; ok && finish != nil {
			chunk["usage"] = u
		}
		raw, _ := json.Marshal(chunk)
		buf.WriteString("data: ")
		buf.Write(raw)
		buf.WriteString("\n\n")
	}
	writeChunk(map[string]any{"role": "assistant", "content": ""}, nil)
	if content != "" {
		writeChunk(map[string]any{"content": content}, nil)
	}
	writeChunk(map[string]any{}, "stop")
	buf.WriteString("data: [DONE]\n\n")
	return buf.Bytes()
}

func isSSEContentType(ct string) bool {
	return strings.Contains(strings.ToLower(ct), "text/event-stream")
}

func isToolCallIDError(status int, body string) bool {
	if status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(body, "tool_call_id") &&
		(strings.Contains(body, "not found") || strings.Contains(body, "not match"))
}

func teeReader(r io.Reader, scan *sseUsageScanner) io.Reader {
	return &teeScanReader{r: r, scan: scan}
}

type teeScanReader struct {
	r    io.Reader
	scan *sseUsageScanner
}

func (t *teeScanReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 && t.scan != nil {
		t.scan.feed(p[:n])
	}
	return n, err
}

// SSE comments keep Cursor/cloudflare from idle-dropping a live stream while
// the upstream model is thinking and emitting no bytes. Only injected at event
// boundaries (before first byte, or after \n\n / \r\n\r\n) so a ping can never
// splice into a partial data: JSON line.
const (
	sseHeartbeatInterval = 10 * time.Second
	sseHeartbeatComment  = ": ping\n\n"
)

type sseCopyStats struct {
	TTFB       time.Duration
	Heartbeats int
}

func sseCopyLogAttrs(stats sseCopyStats) []any {
	attrs := []any{"ttfb_ms", stats.TTFB.Milliseconds()}
	if stats.Heartbeats > 0 {
		attrs = append(attrs, "sse_heartbeats", stats.Heartbeats)
	}
	return attrs
}

func atSSEEventBoundary(tail []byte) bool {
	if len(tail) == 0 {
		return true
	}
	return bytes.HasSuffix(tail, []byte("\n\n")) || bytes.HasSuffix(tail, []byte("\r\n\r\n"))
}

func sseTail(tail, p []byte) []byte {
	const keep = 4
	if len(p) >= keep {
		out := make([]byte, keep)
		copy(out, p[len(p)-keep:])
		return out
	}
	tail = append(append([]byte(nil), tail...), p...)
	if len(tail) > keep {
		tail = tail[len(tail)-keep:]
	}
	return tail
}

// copySSE copies upstream SSE to the client while scanning usage and injecting
// keep-alive comments during silent gaps at event boundaries.
func copySSE(w http.ResponseWriter, upstream io.Reader, scan *sseUsageScanner) (sseCopyStats, error) {
	return copySSEWithHeartbeat(w, upstream, scan, sseHeartbeatInterval)
}

func copySSEWithHeartbeat(w http.ResponseWriter, upstream io.Reader, scan *sseUsageScanner, interval time.Duration) (sseCopyStats, error) {
	var stats sseCopyStats
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	reader := bufio.NewReader(teeReader(upstream, scan))
	type readResult struct {
		b   []byte
		err error
	}
	ch := make(chan readResult)
	done := make(chan struct{})
	defer close(done)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := reader.Read(buf)
			var b []byte
			if n > 0 {
				b = bytes.Clone(buf[:n])
			}
			select {
			case ch <- readResult{b, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	started := time.Now()
	var tail []byte
	var timerC <-chan time.Time
	var timer *time.Timer
	if interval > 0 {
		timer = time.NewTimer(interval)
		defer timer.Stop()
		timerC = timer.C
	}

	for {
		select {
		case r := <-ch:
			if len(r.b) > 0 {
				if stats.TTFB == 0 {
					stats.TTFB = time.Since(started)
				}
				if _, werr := w.Write(r.b); werr != nil {
					return stats, werr
				}
				if flusher != nil {
					flusher.Flush()
				}
				tail = sseTail(tail, r.b)
			}
			if r.err == io.EOF {
				return stats, nil
			}
			if r.err != nil {
				return stats, r.err
			}
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(interval)
			}
		case <-timerC:
			if atSSEEventBoundary(tail) {
				if _, werr := w.Write([]byte(sseHeartbeatComment)); werr != nil {
					return stats, werr
				}
				if flusher != nil {
					flusher.Flush()
				}
				tail = sseTail(tail, []byte(sseHeartbeatComment))
				stats.Heartbeats++
			}
			if timer != nil {
				timer.Reset(interval)
			}
		}
	}
}

func writeSynthesizedSSE(w http.ResponseWriter, completion map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(synthesizeSSE(completion))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func restoreClientStream(body map[string]any, clientWantsStream bool) {
	if clientWantsStream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	} else {
		body["stream"] = false
		delete(body, "stream_options")
	}
}

func clientWantsStream(body map[string]any) bool {
	v, ok := body["stream"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func cloneMapDeep(m map[string]any) map[string]any {
	raw, err := json.Marshal(m)
	if err != nil {
		return cloneMap(m)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return cloneMap(m)
	}
	return out
}
