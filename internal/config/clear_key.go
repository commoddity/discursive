package config

import (
	"fmt"
	"strings"
)

// ClearProviderKey removes the stored API key for provider.
func (s *AppSettings) ClearProviderKey(p Provider) {
	switch p {
	case ProviderMoonshot:
		s.ClearMoonshotKey()
	case ProviderDeepSeek:
		s.ClearDeepSeekKey()
	case ProviderZai:
		s.ClearZaiKey()
	case ProviderThaura:
		s.ClearThauraKey()
	case ProviderOpenRouter:
		s.ClearOpenRouterKey()
	}
}

// ParseClearProvider normalizes a --clear flag value to a Provider.
func ParseClearProvider(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "moonshot", "kimi":
		return ProviderMoonshot, nil
	case "deepseek":
		return ProviderDeepSeek, nil
	case "zai", "z.ai":
		return ProviderZai, nil
	case "thaura":
		return ProviderThaura, nil
	case "openrouter", "or":
		return ProviderOpenRouter, nil
	default:
		return "", fmt.Errorf("unknown provider %q for --clear (use moonshot, deepseek, zai, thaura, or openrouter)", name)
	}
}

// IsChatProvider reports whether provider is a user-selectable chat provider
// (not OpenRouter, which is peak fallback only).
func IsChatProvider(p Provider) bool {
	switch p {
	case ProviderMoonshot, ProviderDeepSeek, ProviderZai, ProviderThaura:
		return true
	default:
		return false
	}
}
