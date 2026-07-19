package config

import (
	"testing"
)

func TestUpstreamBaseURLDefaultsAndEnv(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		env      map[string]string
		want     string
		wantErr  bool
	}{
		{
			name:     "moonshot_default",
			provider: ProviderMoonshot,
			want:     DefaultMoonshotBaseURL,
		},
		{
			name:     "deepseek_default",
			provider: ProviderDeepSeek,
			want:     DefaultDeepSeekBaseURL,
		},
		{
			name:     "moonshot_env_override",
			provider: ProviderMoonshot,
			env:      map[string]string{EnvMoonshotBaseURL: "https://moonshot.example/v1/"},
			want:     "https://moonshot.example/v1",
		},
		{
			name:     "deepseek_env_override",
			provider: ProviderDeepSeek,
			env:      map[string]string{EnvDeepSeekBaseURL: "https://deepseek.example/"},
			want:     "https://deepseek.example",
		},
		{
			name:     "empty_env_uses_default",
			provider: ProviderDeepSeek,
			env:      map[string]string{EnvDeepSeekBaseURL: "  "},
			want:     DefaultDeepSeekBaseURL,
		},
		{
			name:     "unknown_provider",
			provider: Provider("openai"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := getenv
			t.Cleanup(func() { getenv = prev })
			getenv = func(k string) string {
				if tt.env != nil {
					return tt.env[k]
				}
				return ""
			}

			got, err := UpstreamBaseURL(tt.provider)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestChatCompletionsURLNoDoubleV1(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		env      map[string]string
		want     string
	}{
		{
			name:     "moonshot_default",
			provider: ProviderMoonshot,
			want:     "https://api.moonshot.ai/v1/chat/completions",
		},
		{
			name:     "deepseek_default",
			provider: ProviderDeepSeek,
			want:     "https://api.deepseek.com/chat/completions",
		},
		{
			name:     "deepseek_with_v1_override",
			provider: ProviderDeepSeek,
			env:      map[string]string{EnvDeepSeekBaseURL: "https://api.deepseek.com/v1"},
			want:     "https://api.deepseek.com/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := getenv
			t.Cleanup(func() { getenv = prev })
			getenv = func(k string) string {
				if tt.env != nil {
					return tt.env[k]
				}
				return ""
			}
			got, err := ChatCompletionsURL(tt.provider)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestModelsURL(t *testing.T) {
	prev := getenv
	t.Cleanup(func() { getenv = prev })
	getenv = func(string) string { return "" }

	got, err := ModelsURL(ProviderMoonshot)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.moonshot.ai/v1/models" {
		t.Fatalf("got %q", got)
	}
}
