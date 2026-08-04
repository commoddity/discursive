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
	PolicyThaura
	PolicyZai
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
	Experimental bool // reserved for future use
}

// ListAdvertisedModels returns the canonical advertise list (aliases only).
// Must stay aligned with ResolveModel cases.
func ListAdvertisedModels() []AdvertisedModel {
	return []AdvertisedModel{
		{ID: "gpt-4o", Provider: config.ProviderMoonshot},
		{ID: "gpt-4o-mini", Provider: config.ProviderMoonshot},
		{ID: "o1", Provider: config.ProviderDeepSeek},
		{ID: "o3-mini", Provider: config.ProviderDeepSeek},
		{ID: "gpt-5-nano", Provider: config.ProviderThaura},
		{ID: "gpt-4.1-turbo", Provider: config.ProviderZai},
		{ID: "gpt-4.1", Provider: config.ProviderZai},
	}
}

// ResolveModel maps a Cursor alias or known real model id to a Route.
// Unknown models return an error (T05 maps to 400).
func ResolveModel(requested string) (Route, error) {
	switch requested {
	case "gpt-4o":
		return Route{config.ProviderMoonshot, "kimi-k3", PolicyK3}, nil
	case "gpt-4o-mini":
		return Route{config.ProviderMoonshot, "kimi-k2.6", PolicyK2}, nil
	case "o1":
		return Route{config.ProviderDeepSeek, "deepseek-v4-pro", PolicyDeepSeek}, nil
	case "o3-mini":
		return Route{config.ProviderDeepSeek, "deepseek-v4-flash", PolicyDeepSeek}, nil
	case "kimi-k3":
		return Route{config.ProviderMoonshot, "kimi-k3", PolicyK3}, nil
	case "kimi-k2.6":
		return Route{config.ProviderMoonshot, "kimi-k2.6", PolicyK2}, nil
	case "deepseek-v4-pro", "deepseek-v4-flash":
		return Route{config.ProviderDeepSeek, requested, PolicyDeepSeek}, nil
	case "gpt-5-nano":
		return Route{config.ProviderThaura, "thaura", PolicyThaura}, nil
	case "thaura":
		return Route{config.ProviderThaura, "thaura", PolicyThaura}, nil
	case "gpt-4.1-turbo":
		return Route{config.ProviderZai, "glm-5.2", PolicyZai}, nil
	case "gpt-4.1":
		return Route{config.ProviderZai, "glm-4.7", PolicyZai}, nil
	case "glm-5.2":
		return Route{config.ProviderZai, "glm-5.2", PolicyZai}, nil
	case "glm-4.7":
		return Route{config.ProviderZai, "glm-4.7", PolicyZai}, nil
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
	// Official API values are high|max; low|medium|xhigh are compatibility aliases
	// (mapped upstream by DeepSeek / by our normalizer before send).
	switch s {
	case "high", "max", "low", "medium", "xhigh":
		return true
	default:
		return false
	}
}

func isZaiValidReasoningEffort(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	s = strings.TrimSpace(s)
	// Z.AI accepts none|minimal|low|medium|high|xhigh|max.
	// low|medium → high and xhigh → max in normalizer.
	switch s {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
	}
}
