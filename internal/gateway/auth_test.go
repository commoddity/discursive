package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractAPIKey_Bearer(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer sk-test-key-12345")
	if got := ExtractAPIKey(req); got != "sk-test-key-12345" {
		t.Fatalf("got %q want %q", got, "sk-test-key-12345")
	}
}

func TestExtractAPIKey_BearerLowercase(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "bearer sk-test-key-12345")
	if got := ExtractAPIKey(req); got != "sk-test-key-12345" {
		t.Fatalf("got %q want %q", got, "sk-test-key-12345")
	}
}

func TestExtractAPIKey_BearerWithSpaces(t *testing.T) {
	// Authorization header is checked as-is (no leading-space trim on the header value itself).
	// The key value IS trimmed after extraction.
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer   sk-test-key-12345  ")
	if got := ExtractAPIKey(req); got != "sk-test-key-12345" {
		t.Fatalf("got %q want %q", got, "sk-test-key-12345")
	}
}

func TestExtractAPIKey_XApiKey(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("x-api-key", "sk-x-api-key")
	if got := ExtractAPIKey(req); got != "sk-x-api-key" {
		t.Fatalf("got %q want %q", got, "sk-x-api-key")
	}
}

func TestExtractAPIKey_ApiKeyHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("api-key", "sk-api-key")
	if got := ExtractAPIKey(req); got != "sk-api-key" {
		t.Fatalf("got %q want %q", got, "sk-api-key")
	}
}

func TestExtractAPIKey_OpenAIKeyHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("x-openai-api-key", "sk-openai-key")
	if got := ExtractAPIKey(req); got != "sk-openai-key" {
		t.Fatalf("got %q want %q", got, "sk-openai-key")
	}
}

func TestExtractAPIKey_NoHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if got := ExtractAPIKey(req); got != "" {
		t.Fatalf("got %q want empty", got)
	}
}

func TestExtractAPIKey_BearerTakesPriority(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer sk-bearer-key")
	req.Header.Set("x-api-key", "sk-x-key")
	if got := ExtractAPIKey(req); got != "sk-bearer-key" {
		t.Fatalf("got %q want %q", got, "sk-bearer-key")
	}
}

func TestExtractAPIKey_NoBearerPrefix(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if got := ExtractAPIKey(req); got != "" {
		t.Fatalf("got %q want empty", got)
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

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "invalid_api_key") {
		t.Fatalf("missing invalid_api_key in body: %s", body)
	}
	if !strings.Contains(body, "invalid_request_error") {
		t.Fatalf("missing invalid_request_error in body: %s", body)
	}
}
