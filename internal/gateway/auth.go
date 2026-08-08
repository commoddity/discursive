package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// ExtractAPIKey reads the gateway API key from common OpenAI-style headers.
func ExtractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if rest, ok := strings.CutPrefix(auth, "Bearer "); ok {
			return strings.TrimSpace(rest)
		}
		if rest, ok := strings.CutPrefix(auth, "bearer "); ok {
			return strings.TrimSpace(rest)
		}
	}
	for _, name := range []string{"api-key", "x-api-key", "x-openai-api-key"} {
		if v := strings.TrimSpace(r.Header.Get(name)); v != "" {
			return v
		}
	}
	return ""
}

// GatewayKeyMatches reports whether provided equals expected using a
// constant-time comparison.
func GatewayKeyMatches(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// WriteUnauthorized writes an OpenAI-shaped 401 JSON body.
func WriteUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": "Incorrect API key provided: invalid_api_key.",
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    "invalid_api_key",
		},
	})
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    typ,
		},
	})
}

// AuthMiddleware returns an http.Handler that wraps next with gateway key
// authentication. Requests that fail auth receive an OpenAI-shaped 401.
// If expected is empty, auth is bypassed.
func AuthMiddleware(next http.Handler, expected string) http.Handler {
	if expected == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !GatewayKeyMatches(ExtractAPIKey(r), expected) {
			WriteUnauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
