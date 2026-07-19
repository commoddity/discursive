package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSaveRoundTripBothKeys(t *testing.T) {
	tests := []struct {
		name         string
		moonshot     string
		deepseek     string
		wantMoonshot bool
		wantDeepseek bool
	}{
		{name: "both_keys", moonshot: "sk-moon-aaa", deepseek: "sk-deep-bbb", wantMoonshot: true, wantDeepseek: true},
		{name: "moonshot_only", moonshot: "sk-moon-only", wantMoonshot: true},
		{name: "deepseek_only", deepseek: "sk-deep-only", wantDeepseek: true},
		{name: "neither"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataRoot := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dataRoot, "data"), 0o755); err != nil {
				t.Fatal(err)
			}

			s := DefaultSettings()
			if err := s.EnsureGatewayKey(); err != nil {
				t.Fatal(err)
			}
			gwBefore := s.GatewayKey
			if tt.moonshot != "" {
				if err := s.SetMoonshotKey(dataRoot, tt.moonshot); err != nil {
					t.Fatal(err)
				}
			}
			if tt.deepseek != "" {
				if err := s.SetDeepSeekKey(dataRoot, tt.deepseek); err != nil {
					t.Fatal(err)
				}
			}
			if err := Save(dataRoot, s); err != nil {
				t.Fatal(err)
			}

			loaded, err := Load(dataRoot)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.GatewayKey != gwBefore {
				t.Fatalf("gateway key changed: %q vs %q", loaded.GatewayKey, gwBefore)
			}
			if loaded.HasMoonshotKey() != tt.wantMoonshot {
				t.Fatalf("has moonshot=%v want %v", loaded.HasMoonshotKey(), tt.wantMoonshot)
			}
			if loaded.HasDeepSeekKey() != tt.wantDeepseek {
				t.Fatalf("has deepseek=%v want %v", loaded.HasDeepSeekKey(), tt.wantDeepseek)
			}

			gotM, err := loaded.GetMoonshotKey(dataRoot)
			if err != nil {
				t.Fatal(err)
			}
			gotD, err := loaded.GetDeepSeekKey(dataRoot)
			if err != nil {
				t.Fatal(err)
			}
			assertOptional(t, gotM, tt.moonshot, tt.wantMoonshot)
			assertOptional(t, gotD, tt.deepseek, tt.wantDeepseek)

			if tt.moonshot != "" && loaded.MoonshotKeyEncrypted != nil {
				if *loaded.MoonshotKeyEncrypted == base64.StdEncoding.EncodeToString([]byte(tt.moonshot)) {
					t.Fatal("moonshot ciphertext is plaintext base64")
				}
			}
			if tt.deepseek != "" && loaded.DeepSeekKeyEncrypted != nil {
				if *loaded.DeepSeekKeyEncrypted == base64.StdEncoding.EncodeToString([]byte(tt.deepseek)) {
					t.Fatal("deepseek ciphertext is plaintext base64")
				}
			}

			info, err := os.Stat(ConfigPath(dataRoot))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("config.json perms too open: %v", info.Mode())
			}
		})
	}
}

func TestRotateGatewayKey(t *testing.T) {
	s := DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	before := s.GatewayKey
	if err := s.RotateGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if s.GatewayKey == before {
		t.Fatal("gateway key did not change")
	}
	if len(s.GatewayKey) < 20 {
		t.Fatalf("rotated key too short: %q", s.GatewayKey)
	}
}

func TestLoadMissingCreatesGatewayKey(t *testing.T) {
	root := t.TempDir()
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.GatewayKey == "" {
		t.Fatal("expected gateway key")
	}
}

func assertOptional(t *testing.T, got *string, want string, expect bool) {
	t.Helper()
	if !expect {
		if got != nil {
			t.Fatalf("expected nil, got %q", *got)
		}
		return
	}
	if got == nil || *got != want {
		t.Fatalf("got %v want %q", got, want)
	}
}
