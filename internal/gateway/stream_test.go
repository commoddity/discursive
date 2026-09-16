package gateway

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSSEUsageScanner_DeepSeekHitsAndMisses(t *testing.T) {
	sse := `data: {"usage":{"prompt_tokens":1000,"completion_tokens":500,"prompt_cache_hit_tokens":300,"prompt_cache_miss_tokens":700}}

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if !sc.found {
		t.Fatal("expected usage found")
	}
	if sc.usage.PromptTokens != 1000 {
		t.Fatalf("prompt_tokens=%d", sc.usage.PromptTokens)
	}
	if sc.usage.CompletionTokens != 500 {
		t.Fatalf("completion_tokens=%d", sc.usage.CompletionTokens)
	}
	if sc.usage.CacheHitTokens != 300 {
		t.Fatalf("cache_hit_tokens=%d", sc.usage.CacheHitTokens)
	}
	if sc.usage.CacheMissTokens != 700 {
		t.Fatalf("cache_miss_tokens=%d", sc.usage.CacheMissTokens)
	}
}

func TestSSEUsageScanner_DeepSeekAltFields(t *testing.T) {
	sse := `data: {"usage":{"prompt_tokens":100,"completion_tokens":50,"cache_hit_tokens":20,"cache_miss_tokens":80}}

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if !sc.found {
		t.Fatal("expected usage found")
	}
	if sc.usage.CacheHitTokens != 20 {
		t.Fatalf("cache_hit_tokens=%d", sc.usage.CacheHitTokens)
	}
	if sc.usage.CacheMissTokens != 80 {
		t.Fatalf("cache_miss_tokens=%d", sc.usage.CacheMissTokens)
	}
}

func TestSSEUsageScanner_OpenRouterHost(t *testing.T) {
	sse := `data: {"provider":"Wafer","choices":[{"delta":{"content":"hi"}}]}

data: {"usage":{"prompt_tokens":10,"completion_tokens":1}}

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if sc.orHost != "Wafer" {
		t.Fatalf("orHost=%q want Wafer", sc.orHost)
	}
	if !sc.found {
		t.Fatal("expected usage found")
	}
}

func TestSSEUsageScanner_NoUsageInStream(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"hello"}}]}

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if sc.found {
		t.Fatal("should not find usage in stream without usage field")
	}
}

func TestSSEUsageScanner_InvalidJSON(t *testing.T) {
	sse := `data: not-json

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if sc.found {
		t.Fatal("should not find usage in invalid JSON")
	}
}

func TestSSEUsageScanner_EmptyData(t *testing.T) {
	sse := `data: 

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if sc.found {
		t.Fatal("should not find usage in empty data")
	}
}

func TestSSEUsageScanner_ChunkedFeeding(t *testing.T) {
	var sc sseUsageScanner
	sc.feed([]byte(`data: {"usage":{"prompt`))
	if sc.found {
		t.Fatal("should not find usage mid-line")
	}
	sc.feed([]byte(`_tokens": 42, "completion_tokens": 10}}` + "\n"))
	sc.feed([]byte("\n"))
	if !sc.found {
		t.Fatal("expected usage found after completing line")
	}
	if sc.usage.PromptTokens != 42 {
		t.Fatalf("prompt_tokens=%d", sc.usage.PromptTokens)
	}
	if sc.usage.CompletionTokens != 10 {
		t.Fatalf("completion_tokens=%d", sc.usage.CompletionTokens)
	}
}

func TestSSEUsageScanner_NonDataLines(t *testing.T) {
	sse := `event: ping

: comment line

data: {"usage":{"prompt_tokens":1,"completion_tokens":2}}

data: [DONE]
`
	var sc sseUsageScanner
	sc.feed([]byte(sse))
	if !sc.found {
		t.Fatal("expected usage found after non-data lines")
	}
}

func TestSynthesizeSSE(t *testing.T) {
	completion := map[string]any{
		"id":    "chatcmpl-123",
		"model": "kimi-k3",
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": "Hello from Kimi"},
			},
		},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	}
	result := synthesizeSSE(completion)
	s := string(result)

	if !strings.Contains(s, "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", s)
	}
	if !strings.Contains(s, "Hello from Kimi") {
		t.Fatalf("missing content: %s", s)
	}
	if !strings.Contains(s, "chatcmpl-123") {
		t.Fatalf("missing id: %s", s)
	}
	if !strings.Contains(s, "prompt_tokens") {
		t.Fatalf("missing usage: %s", s)
	}
	if !strings.Contains(s, `"object":"chat.completion.chunk"`) {
		t.Fatalf("missing chunk object: %s", s)
	}
}

func TestSynthesizeSSE_DefaultID(t *testing.T) {
	completion := map[string]any{
		"model": "test-model",
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": ""},
			},
		},
	}
	result := synthesizeSSE(completion)
	if !strings.Contains(string(result), "chatcmpl-synth") {
		t.Fatal("expected default synthetic id")
	}
}

func TestTeeScanReader(t *testing.T) {
	input := strings.NewReader("hello world")
	var sc sseUsageScanner
	tr := &teeScanReader{r: input, scan: &sc}

	buf := make([]byte, 5)
	n, err := tr.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || string(buf[:n]) != "hello" {
		t.Fatalf("got %q", string(buf[:n]))
	}

	n, err = tr.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || string(buf[:n]) != " worl" {
		t.Fatalf("got %q", string(buf[:n]))
	}

	// Last byte: may return (1, nil) or (1, io.EOF) depending on reader impl.
	buf2 := make([]byte, 5)
	n, err = tr.Read(buf2)
	if n != 1 || string(buf2[:n]) != "d" {
		t.Fatalf("got %q", string(buf2[:n]))
	}
	// EOF check: either returned with final byte or on next read.
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected err: %v", err)
	}
	if err == nil {
		_, err = tr.Read(buf2)
		if err != io.EOF {
			t.Fatalf("expected EOF after full read, got %v", err)
		}
	}
}

func TestTeeScanReader_NilScanner(t *testing.T) {
	input := strings.NewReader("data")
	tr := &teeScanReader{r: input, scan: nil}
	buf := make([]byte, 4)
	n, err := tr.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || string(buf[:n]) != "data" {
		t.Fatalf("got %q", string(buf[:n]))
	}
}

func TestCopySSE_Basic(t *testing.T) {
	upstream := strings.NewReader("data: hello\n\ndata: [DONE]\n\n")
	var sc sseUsageScanner
	w := httptest.NewRecorder()

	_, err := copySSE(w, upstream, &sc)
	if err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "data: hello") {
		t.Fatalf("missing stream data: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing [DONE]: %s", body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestCopySSE_UsageScanning(t *testing.T) {
	upstream := strings.NewReader(`data: {"choices":[{"delta":{"content":"hi"}}]}

data: {"usage":{"prompt_tokens":5,"completion_tokens":1}}

data: [DONE]
`)
	var sc sseUsageScanner
	w := httptest.NewRecorder()

	_, err := copySSE(w, upstream, &sc)
	if err != nil {
		t.Fatal(err)
	}
	if !sc.found {
		t.Fatal("expected usage found via copySSE")
	}
	if sc.usage.PromptTokens != 5 {
		t.Fatalf("prompt_tokens=%d", sc.usage.PromptTokens)
	}
	if sc.usage.CompletionTokens != 1 {
		t.Fatalf("completion_tokens=%d", sc.usage.CompletionTokens)
	}
}

func TestIsSSEContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"text/event-stream", true},
		{"TEXT/EVENT-STREAM", true},
		{"text/event-stream; charset=utf-8", true},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isSSEContentType(tt.ct); got != tt.want {
			t.Fatalf("isSSEContentType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}

func TestIsToolCallIDError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"not 400", 500, "tool_call_id not found", false},
		{"400 not found", 400, "tool_call_id 'x' not found", true},
		{"400 not match", 400, "tool_call_id does not match", true},
		{"400 no keyword", 400, "invalid request", false},
		{"400 tool_call_id only", 400, "tool_call_id", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isToolCallIDError(tt.status, tt.body); got != tt.want {
				t.Fatalf("isToolCallIDError(%d, %q) = %v, want %v", tt.status, tt.body, got, tt.want)
			}
		})
	}
}

func TestClientWantsStream(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want bool
	}{
		{"stream true", map[string]any{"stream": true}, true},
		{"stream false", map[string]any{"stream": false}, false},
		{"no stream", map[string]any{}, false},
		{"string not bool", map[string]any{"stream": "true"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clientWantsStream(tt.body); got != tt.want {
				t.Fatalf("clientWantsStream(%v) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestRestoreClientStream(t *testing.T) {
	body := map[string]any{"stream": false}
	restoreClientStream(body, true)
	if v, ok := body["stream"].(bool); !ok || !v {
		t.Fatalf("expected stream=true, got %v", body["stream"])
	}
	if _, ok := body["stream_options"]; !ok {
		t.Fatal("expected stream_options to be set")
	}
}

func TestRestoreClientStream_False(t *testing.T) {
	body := map[string]any{"stream": true, "stream_options": map[string]any{"include_usage": true}}
	restoreClientStream(body, false)
	if v, ok := body["stream"].(bool); !ok || v {
		t.Fatalf("expected stream=false, got %v", body["stream"])
	}
	if _, ok := body["stream_options"]; ok {
		t.Fatal("expected stream_options to be deleted")
	}
}

func TestCloneMapDeep(t *testing.T) {
	orig := map[string]any{"key": "value", "nested": map[string]any{"a": float64(1)}}
	cloned := cloneMapDeep(orig)
	if cloned["key"] != "value" {
		t.Fatalf("clone mismatch: %v", cloned)
	}
	if nested, ok := cloned["nested"].(map[string]any); !ok || nested["a"] != float64(1) {
		t.Fatalf("nested clone mismatch: %v", cloned["nested"])
	}
	// Mutate clone, ensure original is untouched.
	cloned["key"] = "modified"
	if orig["key"] != "value" {
		t.Fatal("cloneDeep mutated original")
	}
}

func TestCloneMapDeep_NonJSON(t *testing.T) {
	// Functions are not JSON-serializable — should fall back to shallow clone.
	orig := map[string]any{"fn": func() {}}
	cloned := cloneMapDeep(orig)
	// The clone should have the same function reference (shallow).
	if cloned["fn"] == nil {
		t.Fatal("clone lost the fn key")
	}
	// Since cloneMapDeep can't deep-copy a func, the shallow clone retains the pointer.
	_ = cloned
}

func TestWriteSynthesizedSSE(t *testing.T) {
	w := httptest.NewRecorder()
	completion := map[string]any{
		"id":    "chatcmpl-test",
		"model": "kimi-k3",
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": "test content"},
			},
		},
	}
	writeSynthesizedSSE(w, completion)

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(w.Body.String(), "test content") {
		t.Fatal("body missing content")
	}
	if !strings.Contains(w.Body.String(), "data: [DONE]") {
		t.Fatal("body missing [DONE]")
	}
}

func TestParseUsageObject_KimiCachedTokens(t *testing.T) {
	// Kimi returns a single "cached_tokens" field (top-level or nested).
	tests := []struct {
		name           string
		usage          map[string]any
		wantPrompt     uint64
		wantCompletion uint64
		wantCacheHit   uint64
		wantCacheMiss  uint64
	}{
		{
			name:           "kimi cold request (zero cached_tokens)",
			usage:          map[string]any{"prompt_tokens": 1000, "completion_tokens": 500, "cached_tokens": 0},
			wantPrompt:     1000,
			wantCompletion: 500,
			wantCacheHit:   0,
			wantCacheMiss:  0,
		},
		{
			name:           "kimi cache hit (top-level cached_tokens)",
			usage:          map[string]any{"prompt_tokens": 1000, "completion_tokens": 500, "cached_tokens": 800},
			wantPrompt:     1000,
			wantCompletion: 500,
			wantCacheHit:   800,
			wantCacheMiss:  200, // 1000 - 800
		},
		{
			name: "kimi cache hit (nested prompt_tokens_details.cached_tokens)",
			usage: map[string]any{
				"prompt_tokens":     1000,
				"completion_tokens": 500,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 800,
				},
			},
			wantPrompt:     1000,
			wantCompletion: 500,
			wantCacheHit:   800,
			wantCacheMiss:  200,
		},
		{
			name:           "kimi full cache hit (all tokens cached)",
			usage:          map[string]any{"prompt_tokens": 1000, "completion_tokens": 500, "cached_tokens": 1000},
			wantPrompt:     1000,
			wantCompletion: 500,
			wantCacheHit:   1000,
			wantCacheMiss:  0, // all cached, none uncached
		},
		{
			name:           "kimi no cached_tokens field at all",
			usage:          map[string]any{"prompt_tokens": 500, "completion_tokens": 100},
			wantPrompt:     500,
			wantCompletion: 100,
			wantCacheHit:   0,
			wantCacheMiss:  0,
		},
		{
			name: "kimi partial cache with prompt_tokens_details",
			usage: map[string]any{
				"prompt_tokens":     1000,
				"completion_tokens": 500,
				"prompt_tokens_details": map[string]any{
					"cached_tokens": 300,
				},
			},
			wantPrompt:     1000,
			wantCompletion: 500,
			wantCacheHit:   300,
			wantCacheMiss:  700,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUsageObject(tt.usage)
			if got.PromptTokens != tt.wantPrompt {
				t.Fatalf("PromptTokens = %d, want %d", got.PromptTokens, tt.wantPrompt)
			}
			if got.CompletionTokens != tt.wantCompletion {
				t.Fatalf("CompletionTokens = %d, want %d", got.CompletionTokens, tt.wantCompletion)
			}
			if got.CacheHitTokens != tt.wantCacheHit {
				t.Fatalf("CacheHitTokens = %d, want %d", got.CacheHitTokens, tt.wantCacheHit)
			}
			if got.CacheMissTokens != tt.wantCacheMiss {
				t.Fatalf("CacheMissTokens = %d, want %d", got.CacheMissTokens, tt.wantCacheMiss)
			}
		})
	}
}

func TestUint64Field(t *testing.T) {
	m := map[string]any{
		"int":    int64(42),
		"float":  float64(42.0),
		"nil":    nil,
		"neg":    float64(-1),
		"string": "not a number",
	}
	if got := uint64Field(m, "int"); got != 42 {
		t.Fatalf("int: got %d", got)
	}
	if got := uint64Field(m, "float"); got != 42 {
		t.Fatalf("float: got %d", got)
	}
	if got := uint64Field(m, "nil"); got != 0 {
		t.Fatalf("nil: got %d", got)
	}
	if got := uint64Field(m, "neg"); got != 0 {
		t.Fatalf("neg: got %d", got)
	}
	if got := uint64Field(m, "string"); got != 0 {
		t.Fatalf("string: got %d", got)
	}
	if got := uint64Field(m, "missing"); got != 0 {
		t.Fatalf("missing: got %d", got)
	}
}

func TestSynthesizeSSE_UsageOnlyOnFinish(t *testing.T) {
	// Usage should only appear on the finish chunk (finish_reason = "stop").
	completion := map[string]any{
		"id":    "chatcmpl-usg",
		"model": "kimi-k3",
		"choices": []any{
			map[string]any{
				"message": map[string]any{"content": "hi"},
			},
		},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 2},
	}
	result := synthesizeSSE(completion)
	s := string(result)
	// The finish reason "stop" chunk should have usage.
	count := 0
	for _, line := range bytes.Split([]byte(s), []byte("\n")) {
		if bytes.Contains(line, []byte(`"prompt_tokens"`)) {
			count++
			if !bytes.Contains(line, []byte(`"finish_reason":"stop"`)) {
				t.Fatalf("usage on non-finish chunk: %s", line)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 usage occurrence, got %d", count)
	}
}

func TestAtSSEEventBoundary(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "empty is boundary", in: "", want: true},
		{name: "lf event", in: "data: x\n\n", want: true},
		{name: "crlf event", in: "data: x\r\n\r\n", want: true},
		{name: "comment", in: sseHeartbeatComment, want: true},
		{name: "partial json", in: `data: {"x":`, want: false},
		{name: "single lf", in: "data: x\n", want: false},
		{name: "single crlf", in: "data: x\r\n", want: false},
		{name: "suffix only last 4", in: "abc\n\n", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := atSSEEventBoundary([]byte(tt.in)); got != tt.want {
				t.Fatalf("atSSEEventBoundary(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSSETail(t *testing.T) {
	tests := []struct {
		name string
		tail string
		p    string
		want string
	}{
		{name: "short from empty", tail: "", p: "ab", want: "ab"},
		{name: "keeps last 4 of long p", tail: "zz", p: "abcdefgh", want: "efgh"},
		{name: "append under 4", tail: "ab", p: "c", want: "abc"},
		{name: "append overflow", tail: "abc", p: "de", want: "bcde"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(sseTail([]byte(tt.tail), []byte(tt.p)))
			if got != tt.want {
				t.Fatalf("sseTail(%q, %q) = %q, want %q", tt.tail, tt.p, got, tt.want)
			}
		})
	}
}

type stallReader struct {
	chunks [][]byte
	delays []time.Duration
	i      int
}

func (s *stallReader) Read(p []byte) (int, error) {
	if s.i >= len(s.chunks) {
		return 0, io.EOF
	}
	if s.i < len(s.delays) && s.delays[s.i] > 0 {
		time.Sleep(s.delays[s.i])
	}
	n := copy(p, s.chunks[s.i])
	s.i++
	return n, nil
}

func TestCopySSEHeartbeat(t *testing.T) {
	const interval = 20 * time.Millisecond
	const stall = 150 * time.Millisecond

	tests := []struct {
		name           string
		chunks         [][]byte
		delays         []time.Duration
		wantMinBeats   int
		wantContains   []string
		wantContiguous string
	}{
		{
			name:         "ttfb silence gets comment",
			chunks:       [][]byte{[]byte("data: {\"a\":1}\n\ndata: [DONE]\n\n")},
			delays:       []time.Duration{stall},
			wantMinBeats: 1,
			wantContains: []string{sseHeartbeatComment, `data: {"a":1}`, "data: [DONE]"},
		},
		{
			name:           "mid-line stall does not splice",
			chunks:         [][]byte{[]byte(`data: {"x":`), []byte("1}\n\ndata: [DONE]\n\n")},
			delays:         []time.Duration{0, stall},
			wantMinBeats:   0,
			wantContains:   []string{"data: [DONE]"},
			wantContiguous: `data: {"x":1}`,
		},
		{
			name:         "silence after complete event gets comment",
			chunks:       [][]byte{[]byte("data: {\"a\":1}\n\n"), []byte("data: [DONE]\n\n")},
			delays:       []time.Duration{0, stall},
			wantMinBeats: 1,
			wantContains: []string{sseHeartbeatComment, `data: {"a":1}`, "data: [DONE]"},
		},
		{
			name:         "immediate stream no comment",
			chunks:       [][]byte{[]byte("data: hello\n\ndata: [DONE]\n\n")},
			delays:       nil,
			wantMinBeats: 0,
			wantContains: []string{"data: hello", "data: [DONE]"},
		},
		{
			name:         "lf event split across reads then silence",
			chunks:       [][]byte{[]byte("data: {\"a\":1}\n"), []byte("\n"), []byte("data: [DONE]\n\n")},
			delays:       []time.Duration{0, 0, stall},
			wantMinBeats: 1,
			wantContains: []string{sseHeartbeatComment, `data: {"a":1}`, "data: [DONE]"},
		},
		{
			name:         "crlf event split across reads then silence",
			chunks:       [][]byte{[]byte("data: {\"a\":1}\r\n"), []byte("\r\n"), []byte("data: [DONE]\r\n\r\n")},
			delays:       []time.Duration{0, 0, stall},
			wantMinBeats: 1,
			wantContains: []string{sseHeartbeatComment, "data: [DONE]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sc sseUsageScanner
			w := httptest.NewRecorder()
			src := &stallReader{chunks: tt.chunks, delays: tt.delays}
			stats, err := copySSEWithHeartbeat(w, src, &sc, interval)
			if err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			if tt.wantMinBeats == 0 {
				if stats.Heartbeats != 0 {
					t.Fatalf("heartbeats=%d, want 0; body=%q", stats.Heartbeats, body)
				}
			} else if stats.Heartbeats < tt.wantMinBeats {
				t.Fatalf("heartbeats=%d, want >= %d; body=%q", stats.Heartbeats, tt.wantMinBeats, body)
			}
			for _, s := range tt.wantContains {
				if !strings.Contains(body, s) {
					t.Fatalf("missing %q in %q", s, body)
				}
			}
			if tt.wantContiguous != "" && !strings.Contains(body, tt.wantContiguous) {
				t.Fatalf("spliced JSON, missing contiguous %q in %q", tt.wantContiguous, body)
			}
			if tt.name == "mid-line stall does not splice" {
				bad := ": ping\n\n1}"
				if strings.Contains(body, bad) {
					t.Fatalf("ping spliced into JSON: %q", body)
				}
			}
		})
	}
}

func TestSSETailCrossChunkBoundary(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   bool
	}{
		{name: "lf split short second", chunks: []string{"data: x\n", "\n"}, want: true},
		{name: "crlf split short second", chunks: []string{"data: x\r\n", "\r\n"}, want: true},
		{name: "lf then long next event complete", chunks: []string{"data: x\n", "\ndata: [DONE]\n\n"}, want: true},
		{name: "lf then next event incomplete", chunks: []string{"data: x\n", "\ndata: {\"a\":"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tail []byte
			for _, c := range tt.chunks {
				tail = sseTail(tail, []byte(c))
			}
			if got := atSSEEventBoundary(tail); got != tt.want {
				t.Fatalf("atSSEEventBoundary after chunks %q = %v, want %v (tail=%q)", tt.chunks, got, tt.want, tail)
			}
		})
	}
}
