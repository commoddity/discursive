package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// trimBufferedSSE consumes an upstream SSE stream, trims trailing verbose
// prose from pure-text completions, and writes the result to w in the client's
// requested stream mode. Tool-call streams are always passed through verbatim
// (never trimmed) to avoid corrupting the tool-call protocol.
//
// scan (may be nil) is fed the untrimmed upstream bytes so usage accounting is
// unaffected by trimming. Returns an error only on a write failure.
func (s *Server) trimBufferedSSE(w http.ResponseWriter, body io.Reader, model string, scan *sseUsageScanner) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if scan != nil {
		scan.feed(raw)
	}

	// Parse the accumulated stream. If it contains tool calls or no content,
	// emit the original bytes unchanged (tool-call streams must never be
	// synthesized away).
	parsed, hasToolCalls, ok := sseToCompletion(raw)
	if !ok || hasToolCalls {
		return writeRawSSE(w, raw)
	}

	content, _ := parsed["content"].(string)
	if content == "" {
		return writeRawSSE(w, raw)
	}
	trimmedContent := s.verbosity.TrimStreaming(content, model)
	if trimmedContent == content {
		// No trim warranted — pass through the original stream exactly.
		return writeRawSSE(w, raw)
	}

	// Build the full completion shape consumed by synthesizeSSE (which reads
	// choices[0].message.content) and hand it off for re-emission.
	completion := map[string]any{
		"id":      parsed["id"],
		"object":  "chat.completion",
		"model":   parsed["model"],
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": trimmedContent}, "finish_reason": "stop"}},
	}
	if u, ok := parsed["usage"]; ok && u != nil {
		completion["usage"] = u
	}
	writeSynthesizedSSE(w, completion)
	return nil
}

// sseToCompletion parses a buffered OpenAI SSE stream and returns a (cut-down)
// completion object containing content + usage, plus a bool indicating whether
// the stream carried any tool calls (which must never be synthesized away).
func sseToCompletion(raw []byte) (completion map[string]any, hasToolCalls bool, ok bool) {
	var (
		id, model string
		content   strings.Builder
		usage     map[string]any
		toolCalls bool
	)
	lines := strings.Split(string(raw), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if cid, _ := chunk["id"].(string); cid != "" {
			id = cid
		}
		if m, _ := chunk["model"].(string); m != "" {
			model = m
		}
		if u, is := chunk["usage"].(map[string]any); is {
			usage = u
		}
		choices, is := chunk["choices"].([]any)
		if !is || len(choices) == 0 {
			continue
		}
		sel, is := choices[0].(map[string]any)
		if !is {
			continue
		}
		delta, is := sel["delta"].(map[string]any)
		if !is {
			continue
		}
		if tc, is := delta["tool_calls"].([]any); is && len(tc) > 0 {
			toolCalls = true
		}
		if c, is := delta["content"].(string); is {
			content.WriteString(c)
		}
	}
	completion = map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"model":   model,
		"content": content.String(),
	}
	if usage != nil {
		completion["usage"] = usage
	}
	return completion, toolCalls, true
}

// writeRawSSE writes already-serialized SSE bytes to w with the standard SSE
// headers (used for passthrough when no trim applies).
func writeRawSSE(w http.ResponseWriter, raw []byte) error {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// trimBufferedJSON trims trailing verbose prose from a complete JSON chat
// response (non-streaming) before it is written to the client, via the
// verbosity controller. Returns the (possibly unchanged) trimmed body.
func (s *Server) trimBufferedJSON(respBody []byte, model string) []byte {
	if s.verbosity == nil {
		return respBody
	}
	trimmed, changed := s.verbosity.TrimResponse(respBody, model)
	if changed {
		return trimmed
	}
	return respBody
}
