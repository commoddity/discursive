package config

import "testing"

func TestParseClearProvider(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Provider
		wantErr bool
	}{
		{name: "moonshot", in: "moonshot", want: ProviderMoonshot},
		{name: "kimi alias", in: "kimi", want: ProviderMoonshot},
		{name: "deepseek", in: "deepseek", want: ProviderDeepSeek},
		{name: "zai", in: "zai", want: ProviderZai},
		{name: "z.ai alias", in: "z.ai", want: ProviderZai},
		{name: "thaura", in: "thaura", want: ProviderThaura},
		{name: "openrouter", in: "openrouter", want: ProviderOpenRouter},
		{name: "or alias", in: "or", want: ProviderOpenRouter},
		{name: "unknown", in: "anthropic", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseClearProvider(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestClearProviderKey(t *testing.T) {
	enc := "encrypted"
	s := AppSettings{
		MoonshotKeyEncrypted:   &enc,
		DeepSeekKeyEncrypted:   &enc,
		ZaiKeyEncrypted:        &enc,
		ThauraKeyEncrypted:     &enc,
		OpenRouterKeyEncrypted: &enc,
	}
	s.ClearProviderKey(ProviderMoonshot)
	if s.HasMoonshotKey() {
		t.Fatal("moonshot key should be cleared")
	}
	if !s.HasDeepSeekKey() || !s.HasZaiKey() || !s.HasThauraKey() || !s.HasOpenRouterKey() {
		t.Fatal("other keys should remain")
	}
}
