package config

import (
	"testing"
)

func TestUpstreamBaseURLDefaultsAndEnv(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
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
			name:     "thaura_default",
			provider: ProviderThaura,
			want:     DefaultThauraBaseURL,
		},
		{
			name:     "unknown_provider",
			provider: Provider("openai"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			name:     "thaura_default",
			provider: ProviderThaura,
			want:     "https://backend.thaura.ai/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
	got, err := ModelsURL(ProviderMoonshot)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.moonshot.ai/v1/models" {
		t.Fatalf("got %q", got)
	}
}
