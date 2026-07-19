package crypto

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestGenerateGatewayKey(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "format_and_openai_style"},
		{name: "unique_across_calls"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := GenerateGatewayKey()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(key, gatewayKeyPrefix) {
				t.Fatalf("prefix: got %q", key)
			}
			if len(key) != len(gatewayKeyPrefix)+gatewayKeySuffixLen {
				t.Fatalf("len=%d want %d", len(key), len(gatewayKeyPrefix)+gatewayKeySuffixLen)
			}
			if !IsOpenAIStyleGatewayKey(key) {
				t.Fatalf("not openai-style: %q", key)
			}
			if tt.name == "unique_across_calls" {
				key2, err := GenerateGatewayKey()
				if err != nil {
					t.Fatal(err)
				}
				if key == key2 {
					t.Fatal("expected distinct keys")
				}
			}
		})
	}
}

func TestIsOpenAIStyleGatewayKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "valid_gateway", key: "sk-kimi-" + strings.Repeat("a", 40), want: true},
		{name: "sk_short_ok", key: "sk-abcdefghijklmnopqr", want: true},
		{name: "too_short", key: "sk-abc", want: false},
		{name: "no_sk", key: "kimi-abcdefghijklmnop", want: false},
		{name: "empty", key: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOpenAIStyleGatewayKey(tt.key); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   string
		forbid string
	}{
		{name: "short", secret: "abc", want: maskedPlaceholder},
		{name: "exactly_8", secret: "12345678", want: maskedPlaceholder},
		{name: "hides_middle", secret: "sk-abcdefghijklmnop", want: "sk-a" + maskedPlaceholder + "mnop", forbid: "efgh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskSecret(tt.secret)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
			if tt.forbid != "" && strings.Contains(got, tt.forbid) {
				t.Fatalf("masked still contains %q: %q", tt.forbid, got)
			}
		})
	}
}

func TestProtectUnprotectRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{name: "moonshot_shaped", plaintext: "sk-test-moonshot-key-12345"},
		{name: "deepseek_shaped", plaintext: "sk-deepseek-test-key-abcdef"},
		{name: "empty", plaintext: ""},
		{name: "unicode", plaintext: "密钥-secret-🔑"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			enc, err := Protect(root, tt.plaintext)
			if err != nil {
				t.Fatal(err)
			}
			if plainB64 := base64.StdEncoding.EncodeToString([]byte(tt.plaintext)); enc == plainB64 && tt.plaintext != "" {
				t.Fatal("ciphertext equals plaintext base64")
			}
			got, err := Unprotect(root, enc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.plaintext {
				t.Fatalf("got %q want %q", got, tt.plaintext)
			}
			info, err := os.Stat(MasterKeyPath(root))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("master key perms too open: %v", info.Mode())
			}
		})
	}
}

func TestUnprotectBadPayload(t *testing.T) {
	root := t.TempDir()
	if _, err := EnsureMasterKey(root); err != nil {
		t.Fatal(err)
	}
	good, err := Protect(root, "ok")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(good)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(raw)

	tests := []struct {
		name string
		enc  string
	}{
		{name: "not_base64", enc: "%%%"},
		{name: "too_short", enc: base64.StdEncoding.EncodeToString([]byte("short"))},
		{name: "tampered", enc: tampered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Unprotect(root, tt.enc); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestProtectIsolatedPerDataRoot(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	enc, err := Protect(a, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unprotect(b, enc); err == nil {
		t.Fatal("expected decrypt fail with different master key")
	}
}
