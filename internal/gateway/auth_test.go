package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{
			name:    "Bearer",
			headers: map[string]string{"Authorization": "Bearer sk-test-key-12345"},
			want:    "sk-test-key-12345",
		},
		{
			name:    "bearer lowercase",
			headers: map[string]string{"Authorization": "bearer sk-test-key-12345"},
			want:    "sk-test-key-12345",
		},
		{
			name:    "Bearer with extra whitespace",
			headers: map[string]string{"Authorization": "Bearer   sk-test-key-12345  "},
			want:    "sk-test-key-12345",
		},
		{
			name:    "x-api-key header",
			headers: map[string]string{"x-api-key": "sk-x-api-key"},
			want:    "sk-x-api-key",
		},
		{
			name:    "api-key header",
			headers: map[string]string{"api-key": "sk-api-key"},
			want:    "sk-api-key",
		},
		{
			name:    "x-openai-api-key header",
			headers: map[string]string{"x-openai-api-key": "sk-openai-key"},
			want:    "sk-openai-key",
		},
		{
			name:    "no header",
			headers: map[string]string{},
			want:    "",
		},
		{
			name:    "Bearer takes priority over x-api-key",
			headers: map[string]string{"Authorization": "Bearer sk-bearer-key", "x-api-key": "sk-x-key"},
			want:    "sk-bearer-key",
		},
		{
			name:    "Authorization without Bearer prefix",
			headers: map[string]string{"Authorization": "Basic dXNlcjpwYXNz"},
			want:    "",
		},
		{
			name:    "empty Authorization falls through to x-api-key",
			headers: map[string]string{"Authorization": "", "x-api-key": "sk-fallback"},
			want:    "sk-fallback",
		},
		{
			name:    "Bearer with empty value returns empty",
			headers: map[string]string{"Authorization": "Bearer "},
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if got := ExtractAPIKey(req); got != tt.want {
				t.Fatalf("ExtractAPIKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGatewayKeyMatches(t *testing.T) {
	tests := []struct {
		name     string
		provided string
		expected string
		want     bool
	}{
		{"exact match", "sk-abc123", "sk-abc123", true},
		{"mismatch", "sk-abc123", "sk-xyz789", false},
		{"mismatch same length", "sk-aaaaaaaaaa", "sk-bbbbbbbbbb", false},
		{"empty provided", "", "sk-abc123", false},
		{"empty expected", "sk-abc123", "", false},
		{"both empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GatewayKeyMatches(tt.provided, tt.expected); got != tt.want {
				t.Fatalf("GatewayKeyMatches(%q, %q) = %v, want %v", tt.provided, tt.expected, got, tt.want)
			}
		})
	}
}

func TestWriteUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	WriteUnauthorized(w)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var resp struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Param   any    `json:"param"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if resp.Error.Code != "invalid_api_key" {
		t.Fatalf("error.code = %q, want invalid_api_key", resp.Error.Code)
	}
	if resp.Error.Type != "invalid_request_error" {
		t.Fatalf("error.type = %q, want invalid_request_error", resp.Error.Type)
	}
	if resp.Error.Param != nil {
		t.Fatalf("error.param = %v, want nil", resp.Error.Param)
	}
	if !strings.Contains(resp.Error.Message, "invalid_api_key") {
		t.Fatalf("error.message missing invalid_api_key: %q", resp.Error.Message)
	}
}

func TestWriteJSONError(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
		typ     string
	}{
		{"not_found", http.StatusNotFound, "resource not found", "not_found_error"},
		{"bad_request", http.StatusBadRequest, "invalid input", "invalid_request_error"},
		{"internal", http.StatusInternalServerError, "something went wrong", "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSONError(w, tt.status, tt.message, tt.typ)

			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d", w.Code, tt.status)
			}
			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", ct)
			}

			var resp struct {
				Error struct {
					Message string `json:"message"`
					Type    string `json:"type"`
				} `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("invalid JSON body: %v", err)
			}
			if resp.Error.Message != tt.message {
				t.Fatalf("error.message = %q, want %q", resp.Error.Message, tt.message)
			}
			if resp.Error.Type != tt.typ {
				t.Fatalf("error.type = %q, want %q", resp.Error.Type, tt.typ)
			}
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("valid key passes through", func(t *testing.T) {
		mw := AuthMiddleware(okHandler, "sk-correct")
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer sk-correct")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("invalid key returns 401", func(t *testing.T) {
		mw := AuthMiddleware(okHandler, "sk-correct")
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer sk-wrong")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("missing key returns 401", func(t *testing.T) {
		mw := AuthMiddleware(okHandler, "sk-correct")
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("empty expected bypasses auth", func(t *testing.T) {
		mw := AuthMiddleware(okHandler, "")
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d (bypass when key is empty)", w.Code, http.StatusOK)
		}
	})

	t.Run("wrong key format returns 401", func(t *testing.T) {
		mw := AuthMiddleware(okHandler, "sk-correct")
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("key via x-api-key header passes through", func(t *testing.T) {
		mw := AuthMiddleware(okHandler, "sk-correct")
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("x-api-key", "sk-correct")
		w := httptest.NewRecorder()
		mw.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestAuthMiddleware_IntegrationWithServer(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
		GatewayKey: "sk-test-gateway",
		Settings:   &config.AppSettings{},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })

	t.Run("GET /health bypasses auth", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/health")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("health status = %d, want %d", res.StatusCode, http.StatusOK)
		}
	})

	t.Run("GET /v1/models without auth returns 401", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/v1/models")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
		}
	})

	t.Run("POST /v1/chat/completions without auth returns 401", func(t *testing.T) {
		res, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
		}
	})
}
