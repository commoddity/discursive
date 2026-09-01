package gateway

import (
	"testing"

	"github.com/commoddity/discursive/internal/config"
)

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name     string
		request  string
		provider config.Provider
		model    string
		policy   ThinkingPolicy
		wantErr  bool
	}{
		{name: "deprecated moonshot k3 alias gpt-4o", request: DeprecatedAliasMoonshotK3, provider: config.ProviderMoonshot, model: "kimi-k3", policy: PolicyK3},
		{name: "deprecated moonshot k2.7 alias gpt-4o-mini", request: DeprecatedAliasMoonshotK27, provider: config.ProviderMoonshot, model: "kimi-k2.7-code", policy: PolicyK27},
		{name: "deepseek pro", request: "deepseek-v4-pro", provider: config.ProviderDeepSeek, model: "deepseek-v4-pro", policy: PolicyDeepSeek},
		{name: "deepseek flash vision", request: "deepseek-v4-flash-vision-exp", provider: config.ProviderDeepSeek, model: config.ModelDeepSeekV4FlashVisionExp, policy: PolicyDeepSeek},
		{name: "deprecated deepseek pro alias o1", request: DeprecatedAliasDeepSeekPro, provider: config.ProviderDeepSeek, model: "deepseek-v4-pro", policy: PolicyDeepSeek},
		{name: "deprecated deepseek flash alias o3-mini", request: DeprecatedAliasDeepSeekFlash, provider: config.ProviderDeepSeek, model: config.ModelDeepSeekV4FlashVisionExp, policy: PolicyDeepSeek},
		{name: "real kimi-k3", request: "kimi-k3", provider: config.ProviderMoonshot, model: "kimi-k3", policy: PolicyK3},
		{name: "real kimi-k2.7", request: "kimi-k2.7-code", provider: config.ProviderMoonshot, model: "kimi-k2.7-code", policy: PolicyK27},
		{name: "legacy deepseek flash", request: "deepseek-v4-flash", provider: config.ProviderDeepSeek, model: config.ModelDeepSeekV4FlashVisionExp, policy: PolicyDeepSeek},
		{name: "thaura alias", request: "gpt-5-nano", provider: config.ProviderThaura, model: "thaura", policy: PolicyThaura},
		{name: "real thaura", request: "thaura", provider: config.ProviderThaura, model: "thaura", policy: PolicyThaura},
		{name: "zai glm-5.3", request: "glm-5.3", provider: config.ProviderZai, model: "glm-5.3", policy: PolicyZai},
		{name: "zai glm-5.3-flash", request: "glm-5.3-flash", provider: config.ProviderZai, model: config.ModelZaiGLM53Flash, policy: PolicyZai},
		{name: "deprecated zai glm-5.3 alias gpt-4.1-turbo", request: DeprecatedAliasZaiGLM53, provider: config.ProviderZai, model: "glm-5.3", policy: PolicyZai},
		{name: "compat zai gpt-4-turbo alias", request: CompatAliasZaiGLM53GPT4Turbo, provider: config.ProviderZai, model: "glm-5.3", policy: PolicyZai},
		{name: "deprecated zai glm-5.3-flash alias gpt-4.1", request: DeprecatedAliasZaiGLM53Flash, provider: config.ProviderZai, model: config.ModelZaiGLM53Flash, policy: PolicyZai},
		{name: "legacy glm-4.7", request: "glm-4.7", provider: config.ProviderZai, model: config.ModelZaiGLM53Flash, policy: PolicyZai},
		{name: "openrouter zai glm-5.3 upstream", request: config.ModelOpenRouterZaiGLM53, provider: config.ProviderOpenRouter, model: config.ModelOpenRouterZaiGLM53, policy: PolicyZai},
		{name: "openrouter zai glm-5.3-flash upstream", request: config.ModelOpenRouterZaiGLM53Flash, provider: config.ProviderOpenRouter, model: config.ModelOpenRouterZaiGLM53Flash, policy: PolicyZai},
		{name: "openrouter flash upstream", request: "deepseek/deepseek-v4-flash-0731", provider: config.ProviderOpenRouter, model: "deepseek/deepseek-v4-flash-0731", policy: PolicyDeepSeek},
		{name: "openrouter pro upstream", request: "deepseek/deepseek-v4-pro-0813", provider: config.ProviderOpenRouter, model: "deepseek/deepseek-v4-pro-0813", policy: PolicyDeepSeek},
		{name: "unknown", request: "gpt-3.5-turbo", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, err := ResolveModel(tt.request)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveModel: %v", err)
			}
			if route.Provider != tt.provider || route.RealModel != tt.model || route.Policy != tt.policy {
				t.Fatalf("got %+v want provider=%s model=%s policy=%d", route, tt.provider, tt.model, tt.policy)
			}
		})
	}
}

func TestListAdvertisedModels(t *testing.T) {
	list := ListAdvertisedModels()
	if len(list) != 15 {
		t.Fatalf("len=%d want 13", len(list))
	}
	var sawMoonshot, sawDeepSeek, sawThaura, sawZai, sawOpenRouter bool
	for i, m := range list {
		if _, err := ResolveModel(m.ID); err != nil {
			t.Fatalf("list[%d] id %q not resolvable: %v", i, m.ID, err)
		}
		switch m.Provider {
		case config.ProviderMoonshot:
			sawMoonshot = true
		case config.ProviderDeepSeek:
			sawDeepSeek = true
		case config.ProviderThaura:
			sawThaura = true
		case config.ProviderZai:
			sawZai = true
		case config.ProviderOpenRouter:
			sawOpenRouter = true
		}
	}
	if !sawMoonshot || !sawDeepSeek || !sawThaura || !sawZai {
		t.Fatal("expected all four direct providers in advertise list")
	}
	// OpenRouter is intentionally internal-only: used for peak fallback and
	// subagent downgrades, not advertised as a user-selectable provider.
	if sawOpenRouter {
		t.Fatal("openrouter should not be advertised")
	}
}
