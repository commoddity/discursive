package gateway

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// bearerScheme is the OpenAI-compatible Authorization scheme used by Cursor.
// It is matched case-insensitively so operators can send "Bearer", "bearer",
// or any mixed-case variant and still authenticate.
const bearerScheme = "Bearer"

// apiKeyHeaders lists the fallback headers (after Authorization) that may
// carry the gateway key, in priority order.
var apiKeyHeaders = []string{"api-key", "x-api-key", "x-openai-api-key"}

// ExtractAPIKey reads the gateway API key from common OpenAI-style headers.
// The Authorization scheme is matched case-insensitively. If Authorization is
// present with a non-Bearer scheme (or empty), control falls through to the
// api-key fallback headers.
func ExtractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if scheme, token, ok := strings.Cut(auth, " "); ok && strings.EqualFold(scheme, bearerScheme) {
			return strings.TrimSpace(token)
		}
	}
	for _, name := range apiKeyHeaders {
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
