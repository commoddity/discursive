package config

import "testing"

func TestHasNativeVision(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{model: ModelZaiGLM53Flash, want: true},
		{model: ModelOpenRouterZaiGLM53Flash, want: true},
		{model: "glm-4.7", want: true},
		{model: ModelDeepSeekV4FlashVisionExp, want: true},
		{model: ModelZaiGLM53, want: false},
		{model: ModelOpenRouterZaiGLM53, want: false},
		{model: ModelDeepSeekV4Pro, want: false},
		{model: ModelDeepSeekV4Flash, want: false},
		{model: ModelOpenRouterDeepSeekV4Flash, want: false},
		{model: ModelKimiK3, want: false},
		{model: "", want: false},
	}
	for _, tt := range tests {
		name := tt.model
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got := HasNativeVision(tt.model); got != tt.want {
				t.Fatalf("HasNativeVision(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestOpenRouterRealForPrefersCatalogSmallModel(t *testing.T) {
	real, p, ok := OpenRouterRealFor(ModelOpenRouterDeepSeekV4Flash)
	if !ok || p != ProviderDeepSeek || real != ModelDeepSeekV4FlashVisionExp {
		t.Fatalf("OpenRouterRealFor flash twin = %q %s %v, want %s deepseek true", real, p, ok, ModelDeepSeekV4FlashVisionExp)
	}
}
