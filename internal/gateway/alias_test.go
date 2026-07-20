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
		{name: "kimi k3 alias", request: "gpt-5-high", provider: config.ProviderMoonshot, model: "kimi-k3", policy: PolicyK3},
		{name: "kimi k2.7 alias", request: "gpt-5-codex", provider: config.ProviderMoonshot, model: "kimi-k2.7-code", policy: PolicyK2},
		{name: "deepseek pro alias", request: "gpt-4o", provider: config.ProviderDeepSeek, model: "deepseek-v4-pro", policy: PolicyDeepSeek},
		{name: "deepseek flash alias", request: "gpt-4o-mini", provider: config.ProviderDeepSeek, model: "deepseek-v4-flash", policy: PolicyDeepSeek},
		{name: "real kimi-k3", request: "kimi-k3", provider: config.ProviderMoonshot, model: "kimi-k3", policy: PolicyK3},
		{name: "real kimi-k2.7-code", request: "kimi-k2.7-code", provider: config.ProviderMoonshot, model: "kimi-k2.7-code", policy: PolicyK2},
		{name: "real kimi-k2.7-code-highspeed", request: "kimi-k2.7-code-highspeed", provider: config.ProviderMoonshot, model: "kimi-k2.7-code-highspeed", policy: PolicyK2},
		{name: "real deepseek pro", request: "deepseek-v4-pro", provider: config.ProviderDeepSeek, model: "deepseek-v4-pro", policy: PolicyDeepSeek},
		{name: "real deepseek flash", request: "deepseek-v4-flash", provider: config.ProviderDeepSeek, model: "deepseek-v4-flash", policy: PolicyDeepSeek},
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
	if len(list) != 9 {
		t.Fatalf("len=%d want 9", len(list))
	}
	var sawMoonshot, sawDeepSeek bool
	var sawPrimary, sawExperimental bool
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
		if !m.Experimental {
			sawPrimary = true
		} else {
			sawExperimental = true
		}
	}
	if !sawMoonshot || !sawDeepSeek {
		t.Fatal("expected both providers in advertise list")
	}
	if !sawPrimary || !sawExperimental {
		t.Fatal("expected both Agent-safe aliases and experimental real ids")
	}
	// Primary aliases first
	for i := 0; i < 4; i++ {
		if list[i].Experimental {
			t.Fatalf("primary slot %d marked experimental: %s", i, list[i].ID)
		}
	}
}
