package gateway

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestApplyOpenRouterRouting(t *testing.T) {
	orRoute := Route{Provider: config.ProviderOpenRouter, RealModel: config.ModelOpenRouterZaiGLM53Flash}
	dsRoute := Route{Provider: config.ProviderDeepSeek, RealModel: config.ModelDeepSeekV4Flash}

	tests := []struct {
		name  string
		cfg   SanitizeConfig
		route Route
		body  map[string]any
		want  map[string]any
	}{
		{
			name:  "sort ignore and latency",
			cfg:   SanitizeConfig{OpenRouterSort: "throughput", OpenRouterIgnore: []string{"wafer", "morph"}, OpenRouterMaxLatencyP90: 2.5},
			route: orRoute,
			body:  map[string]any{"model": config.ModelOpenRouterZaiGLM53Flash},
			want: map[string]any{
				"sort":                  "throughput",
				"ignore":                []string{"wafer", "morph"},
				"preferred_max_latency": map[string]any{"p90": 2.5},
			},
		},
		{
			name:  "latency only",
			cfg:   SanitizeConfig{OpenRouterMaxLatencyP90: 3},
			route: orRoute,
			body:  map[string]any{"model": config.ModelOpenRouterZaiGLM53Flash},
			want: map[string]any{
				"preferred_max_latency": map[string]any{"p90": float64(3)},
			},
		},
		{
			name:  "all knobs off drops provider",
			cfg:   SanitizeConfig{},
			route: orRoute,
			body:  map[string]any{"provider": map[string]any{"sort": "stale"}},
			want:  nil,
		},
		{
			name:  "non-openrouter clears leftover provider",
			cfg:   SanitizeConfig{OpenRouterSort: "throughput", OpenRouterIgnore: []string{"wafer"}, OpenRouterMaxLatencyP90: 2.5},
			route: dsRoute,
			body:  map[string]any{"provider": map[string]any{"sort": "throughput"}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyOpenRouterRouting(tt.body, tt.route, tt.cfg)
			got, ok := tt.body["provider"].(map[string]any)
			if tt.want == nil {
				if ok {
					t.Fatalf("expected no provider, got %v", got)
				}
				return
			}
			if !ok {
				t.Fatal("expected provider object")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("provider: got %#v want %#v", got, tt.want)
			}
		})
	}
}

func TestApplyOpenRouterSession(t *testing.T) {
	tests := []struct {
		name      string
		provider  config.Provider
		sessionID string
		body      map[string]any
		wantID    string
		wantSet   bool
	}{
		{
			name:      "sets sticky id",
			provider:  config.ProviderOpenRouter,
			sessionID: "sess_abc",
			body:      map[string]any{},
			wantID:    "sess_abc",
			wantSet:   true,
		},
		{
			name:      "empty session skipped",
			provider:  config.ProviderOpenRouter,
			sessionID: "",
			body:      map[string]any{"session_id": "stale"},
			wantSet:   false,
		},
		{
			name:      "non-openrouter clears leftover",
			provider:  config.ProviderZai,
			sessionID: "sess_abc",
			body:      map[string]any{"session_id": "stale"},
			wantSet:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyOpenRouterSession(tt.body, tt.provider, tt.sessionID)
			got, ok := tt.body["session_id"].(string)
			if tt.wantSet != ok {
				t.Fatalf("session_id present=%v want %v (got %q)", ok, tt.wantSet, got)
			}
			if tt.wantSet && got != tt.wantID {
				t.Fatalf("session_id=%q want %q", got, tt.wantID)
			}
		})
	}
}

func TestOpenRouterHostFromHeaders(t *testing.T) {
	canonical := make(http.Header)
	canonical.Set("X-OpenRouter-Provider", "Wafer")
	alt := make(http.Header)
	alt.Set("OpenRouter-Provider", "Morph")
	empty := make(http.Header)
	empty.Set("X-OpenRouter-Provider", "  ")

	tests := []struct {
		name string
		h    http.Header
		want string
	}{
		{name: "nil", h: nil, want: ""},
		{name: "canonical", h: canonical, want: "Wafer"},
		{name: "alt", h: alt, want: "Morph"},
		{name: "empty ignored", h: empty, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openRouterHostFromHeaders(tt.h); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestOpenRouterHostFromObject(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		want string
	}{
		{name: "nil", obj: nil, want: ""},
		{name: "top-level provider", obj: map[string]any{"provider": "Wafer"}, want: "Wafer"},
		{
			name: "selected metadata endpoint",
			obj: map[string]any{
				"openrouter_metadata": map[string]any{
					"endpoints": map[string]any{
						"available": []any{
							map[string]any{"provider": "Venice", "selected": false},
							map[string]any{"provider": "Wafer", "selected": true},
						},
					},
				},
			},
			want: "Wafer",
		},
		{
			name: "no selected endpoint",
			obj: map[string]any{
				"openrouter_metadata": map[string]any{
					"endpoints": map[string]any{
						"available": []any{
							map[string]any{"provider": "Venice", "selected": false},
						},
					},
				},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openRouterHostFromObject(tt.obj); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestApplyOpenRouterRequestHeaders(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		body        map[string]any
		wantSession string
		wantMeta    bool
	}{
		{name: "no session non-or url", url: "https://api.z.ai/chat", body: map[string]any{}, wantSession: "", wantMeta: false},
		{name: "openrouter url without session", url: "https://openrouter.ai/api/v1/chat/completions", body: nil, wantSession: "", wantMeta: true},
		{name: "session_id sets both", url: "http://127.0.0.1/chat", body: map[string]any{"session_id": "sess_abc"}, wantSession: "sess_abc", wantMeta: true},
		{name: "non-string session_id ignored", url: "http://127.0.0.1/chat", body: map[string]any{"session_id": 1}, wantSession: "", wantMeta: false},
		{name: "blank session_id ignored", url: "http://127.0.0.1/chat", body: map[string]any{"session_id": "  "}, wantSession: "", wantMeta: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := make(http.Header)
			applyOpenRouterRequestHeaders(h, tt.url, tt.body)
			if got := h.Get("X-Session-Id"); got != tt.wantSession {
				t.Fatalf("X-Session-Id=%q want %q", got, tt.wantSession)
			}
			gotMeta := h.Get("X-OpenRouter-Metadata") == "enabled"
			if gotMeta != tt.wantMeta {
				t.Fatalf("metadata=%v want %v", gotMeta, tt.wantMeta)
			}
		})
	}
}
