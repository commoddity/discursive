package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
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

func (s *Server) recordUsage(provider config.Provider, model, requestID string, lat time.Duration, u tokenUsage) {
	if u.CacheHitTokens == 0 && u.CacheMissTokens == 0 && u.PromptTokens > 1024 {
		slog.Debug("usage: no cache tokens reported by upstream",
			"request_id", requestID,
			"provider", string(provider),
			"model", model,
			"prompt_tokens", u.PromptTokens,
		)
	}
	ev := usage.Event{
		SessionID:        s.sessionID,
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
		logRequest(requestID, "usage_record_error", err.Error())
	}
}

// sseUsageScanner extracts usage from streamed SSE chunks.
type sseUsageScanner struct {
	buf   strings.Builder
	usage *tokenUsage
	found bool
	err   *modelNotAvailableError // set when SSE chunk contains an error
}

type modelNotAvailableError struct {
	message string
}

func (sc *sseUsageScanner) feed(p []byte) {
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

// copySSE copies upstream SSE to the client while scanning usage.
func copySSE(w http.ResponseWriter, upstream io.Reader, scan *sseUsageScanner) error {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	reader := bufio.NewReader(teeReader(upstream, scan))
	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
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
