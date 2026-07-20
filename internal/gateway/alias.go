package gateway

import (
	"fmt"
	"strings"

	"github.com/commoddity/discursive/internal/config"
)

// ThinkingPolicy selects provider/model-specific thinking parameter shape.
type ThinkingPolicy int

const (
	PolicyK3 ThinkingPolicy = iota
	PolicyK2
	PolicyDeepSeek
)

// Route is the resolved alias → provider + real model + thinking policy.
type Route struct {
	Provider  config.Provider
	RealModel string
	Policy    ThinkingPolicy
}

// AdvertisedModel is one entry for GET /v1/models (and ListAdvertisedModels).
type AdvertisedModel struct {
	ID           string
	Provider     config.Provider
	Experimental bool // real-id extras / highspeed — not Agent-safe until T10
}

// ListAdvertisedModels returns the canonical advertise list (aliases first, then real ids).
// Must stay aligned with ResolveModel cases.
func ListAdvertisedModels() []AdvertisedModel {
	return []AdvertisedModel{
		{ID: "gpt-5-high", Provider: config.ProviderMoonshot},
		{ID: "gpt-5-codex", Provider: config.ProviderMoonshot},
		{ID: "gpt-4o", Provider: config.ProviderDeepSeek},
		{ID: "gpt-4o-mini", Provider: config.ProviderDeepSeek},
		{ID: "kimi-k3", Provider: config.ProviderMoonshot, Experimental: true},
		{ID: "kimi-k2.7-code", Provider: config.ProviderMoonshot, Experimental: true},
		{ID: "kimi-k2.7-code-highspeed", Provider: config.ProviderMoonshot, Experimental: true},
		{ID: "deepseek-v4-pro", Provider: config.ProviderDeepSeek, Experimental: true},
		{ID: "deepseek-v4-flash", Provider: config.ProviderDeepSeek, Experimental: true},
	}
}

// ResolveModel maps a Cursor alias or known real model id to a Route.
// Unknown models return an error (T05 maps to 400).
func ResolveModel(requested string) (Route, error) {
	switch requested {
	case "gpt-5-high":
		return Route{config.ProviderMoonshot, "kimi-k3", PolicyK3}, nil
	case "gpt-5-codex":
		return Route{config.ProviderMoonshot, "kimi-k2.7-code", PolicyK2}, nil
	case "gpt-4o":
		return Route{config.ProviderDeepSeek, "deepseek-v4-pro", PolicyDeepSeek}, nil
	case "gpt-4o-mini":
		return Route{config.ProviderDeepSeek, "deepseek-v4-flash", PolicyDeepSeek}, nil
	case "kimi-k3":
		return Route{config.ProviderMoonshot, "kimi-k3", PolicyK3}, nil
	case "kimi-k2.7-code", "kimi-k2.7-code-highspeed":
		return Route{config.ProviderMoonshot, requested, PolicyK2}, nil
	case "deepseek-v4-pro", "deepseek-v4-flash":
		return Route{config.ProviderDeepSeek, requested, PolicyDeepSeek}, nil
	default:
		return Route{}, fmt.Errorf("unknown model alias %q", requested)
	}
}

func isDeepSeekValidReasoningEffort(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	s = strings.TrimSpace(s)
	switch s {
	case "low", "medium", "high", "max":
		return true
	default:
		return false
	}
}
