package util

import (
	"github.com/commoddity/discursive/internal/crypto"
)

// MaskGatewayKey masks a gateway key for display / logs.
func MaskGatewayKey(key string) string {
	if len(key) <= 6 {
		return "••••••"
	}
	return key[:3] + "••••••" + key[len(key)-4:]
}

// GatewayKeyLogAttrs returns slog attrs for the gateway key.
// By default the key is masked; show=true logs the full value for Cursor setup.
func GatewayKeyLogAttrs(key string, show bool) []any {
	if show {
		return []any{"gateway_key", key}
	}
	return []any{"gateway_key_masked", crypto.MaskSecret(key)}
}
