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
		{name: "kimi k3 alias", request: "gpt-4o", provider: config.ProviderMoonshot, model: "kimi-k3", policy: PolicyK3},
		{name: "kimi k2.6 alias", request: "gpt-4o-mini", provider: config.ProviderMoonshot, model: "kimi-k2.6", policy: PolicyK2},
		{name: "deepseek pro alias", request: "o1", provider: config.ProviderDeepSeek, model: "deepseek-v4-pro", policy: PolicyDeepSeek},
		{name: "deepseek flash alias", request: "o3-mini", provider: config.ProviderDeepSeek, model: "deepseek-v4-flash", policy: PolicyDeepSeek},
		{name: "real kimi-k3", request: "kimi-k3", provider: config.ProviderMoonshot, model: "kimi-k3", policy: PolicyK3},
		{name: "real kimi-k2.6", request: "kimi-k2.6", provider: config.ProviderMoonshot, model: "kimi-k2.6", policy: PolicyK2},
		{name: "real deepseek pro", request: "deepseek-v4-pro", provider: config.ProviderDeepSeek, model: "deepseek-v4-pro", policy: PolicyDeepSeek},
		{name: "real deepseek flash", request: "deepseek-v4-flash", provider: config.ProviderDeepSeek, model: "deepseek-v4-flash", policy: PolicyDeepSeek},
		{name: "thaura alias", request: "gpt-5-nano", provider: config.ProviderThaura, model: "thaura", policy: PolicyThaura},
		{name: "real thaura", request: "thaura", provider: config.ProviderThaura, model: "thaura", policy: PolicyThaura},
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
	if len(list) != 5 {
		t.Fatalf("len=%d want 5", len(list))
	}
	var sawMoonshot, sawDeepSeek, sawThaura bool
	for i, m := range list {
		if _, err := ResolveModel(m.ID); err != nil {
			t.Fatalf("list[%d] id %q not resolvable: %v", i, m.ID, err)
		}
		if m.Provider == config.ProviderMoonshot {
			sawMoonshot = true
		}
		if m.Provider == config.ProviderDeepSeek {
			sawDeepSeek = true
		}
		if m.Provider == config.ProviderThaura {
			sawThaura = true
		}
	}
	if !sawMoonshot || !sawDeepSeek || !sawThaura {
		t.Fatal("expected all three providers in advertise list")
	}
}
