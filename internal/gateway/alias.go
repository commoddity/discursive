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
	PolicyK27
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

// Deprecated Moonshot Cursor aliases. Still resolved for backwards compat, but
// Cursor applies ~128k context assumptions for gpt-4o/gpt-4o-mini. Prefer
// kimi-k3 and kimi-k2.7-code (not in Cursor's catalog → 1M default context budget).
const (
	DeprecatedAliasMoonshotK3  = "gpt-4o"
	DeprecatedAliasMoonshotK27 = "gpt-4o-mini"
)

// Deprecated DeepSeek Cursor aliases. Still resolved for backwards compat, but
// Cursor applies ~200k context assumptions for o1/o3-mini and compresses long
// chats too early. Prefer deepseek-v4-pro and deepseek-v4-flash-vision-exp
// (not in Cursor's catalog → 1M default context budget).
const (
	DeprecatedAliasDeepSeekPro   = "o1"
	DeprecatedAliasDeepSeekFlash = "o3-mini"
)

// Deprecated Z.AI (GLM) Cursor aliases. Same rationale as the DeepSeek ones:
// Cursor applies ~200k context assumptions for gpt-4.1-turbo/gpt-4.1, so
// prefer the real IDs glm-5.3 / glm-5.3-flash (1M context). gpt-4-turbo stays
// because Cursor sometimes rewrites gpt-4.1-turbo → gpt-4-turbo client-side.
const (
	DeprecatedAliasZaiGLM53      = "gpt-4.1-turbo"
	DeprecatedAliasZaiGLM53Flash = "gpt-4.1"
	CompatAliasZaiGLM53GPT4Turbo = "gpt-4-turbo" // Cursor rewrites gpt-4.1-turbo → this
)

// AdvertisedModel is one entry for GET /v1/models (and ListAdvertisedModels).
type AdvertisedModel struct {
	ID           string
	Provider     config.Provider
	Experimental bool // reserved for future use
}

// ListAdvertisedModels returns the canonical advertise list (aliases + real ids).
// Must stay aligned with ResolveModel cases.
func ListAdvertisedModels() []AdvertisedModel {
	return []AdvertisedModel{
		// Moonshot — prefer real IDs (Cursor 1M context default; see DeprecatedAliasMoonshot*).
		{ID: "kimi-k3", Provider: config.ProviderMoonshot},
		{ID: "kimi-k2.7-code", Provider: config.ProviderMoonshot},
		// DeepSeek — prefer real IDs (Cursor 1M context default; see DeprecatedAlias*).
		{ID: "deepseek-v4-pro", Provider: config.ProviderDeepSeek},
		{ID: "deepseek-v4-flash-vision-exp", Provider: config.ProviderDeepSeek},
		// Z.AI — prefer real IDs (Cursor 1M context default; see DeprecatedAlias*).
		{ID: "glm-5.3", Provider: config.ProviderZai},
		{ID: "glm-5.3-flash", Provider: config.ProviderZai},
		{ID: "thaura", Provider: config.ProviderThaura},
		{ID: "gpt-5-nano", Provider: config.ProviderThaura},
		// Deprecated Moonshot aliases (backwards compat).
		{ID: DeprecatedAliasMoonshotK3, Provider: config.ProviderMoonshot},
		{ID: DeprecatedAliasMoonshotK27, Provider: config.ProviderMoonshot},
		// Deprecated DeepSeek aliases (backwards compat).
		{ID: DeprecatedAliasDeepSeekPro, Provider: config.ProviderDeepSeek},
		{ID: DeprecatedAliasDeepSeekFlash, Provider: config.ProviderDeepSeek},
		// Deprecated Z.AI aliases (backwards compat).
		{ID: DeprecatedAliasZaiGLM53, Provider: config.ProviderZai},
		{ID: DeprecatedAliasZaiGLM53Flash, Provider: config.ProviderZai},
		{ID: CompatAliasZaiGLM53GPT4Turbo, Provider: config.ProviderZai}, // Cursor rewrites gpt-4.1-turbo → this
	}
}

// ResolveModel maps a Cursor alias or known real model id to a Route.
// Unknown models return an error (T05 maps to 400).
func ResolveModel(requested string) (Route, error) {
	switch requested {
	case DeprecatedAliasMoonshotK3:
		// Deprecated: use kimi-k3 (Cursor 1M context default).
		return Route{config.ProviderMoonshot, "kimi-k3", PolicyK3}, nil
	case DeprecatedAliasMoonshotK27:
		// Deprecated: use kimi-k2.7-code (Cursor 1M context default).
		return Route{config.ProviderMoonshot, "kimi-k2.7-code", PolicyK27}, nil
	case DeprecatedAliasDeepSeekPro:
		// Deprecated: use deepseek-v4-pro (Cursor 1M context default).
		return Route{config.ProviderDeepSeek, "deepseek-v4-pro", PolicyDeepSeek}, nil
	case DeprecatedAliasDeepSeekFlash:
		// Deprecated: use deepseek-v4-flash-vision-exp (Cursor 1M context default).
		return Route{config.ProviderDeepSeek, config.ModelDeepSeekV4FlashVisionExp, PolicyDeepSeek}, nil
	case "kimi-k3":
		return Route{config.ProviderMoonshot, "kimi-k3", PolicyK3}, nil
	case "kimi-k2.7-code":
		return Route{config.ProviderMoonshot, "kimi-k2.7-code", PolicyK27}, nil
	case "deepseek-v4-pro":
		return Route{config.ProviderDeepSeek, requested, PolicyDeepSeek}, nil
	case "deepseek-v4-flash-vision-exp":
		return Route{config.ProviderDeepSeek, config.ModelDeepSeekV4FlashVisionExp, PolicyDeepSeek}, nil
	case "deepseek-v4-flash":
		// Legacy compat — upstream is the vision-capable flash model.
		return Route{config.ProviderDeepSeek, config.ModelDeepSeekV4FlashVisionExp, PolicyDeepSeek}, nil
	case "gpt-5-nano":
		return Route{config.ProviderThaura, "thaura", PolicyThaura}, nil
	case "thaura":
		return Route{config.ProviderThaura, "thaura", PolicyThaura}, nil
	case DeprecatedAliasZaiGLM53:
		// Deprecated: use glm-5.3 (Cursor 1M context default).
		return Route{config.ProviderZai, "glm-5.3", PolicyZai}, nil
	case CompatAliasZaiGLM53GPT4Turbo:
		// Cursor sometimes rewrites gpt-4.1-turbo → gpt-4-turbo.
		return Route{config.ProviderZai, "glm-5.3", PolicyZai}, nil
	case DeprecatedAliasZaiGLM53Flash:
		// Deprecated: use glm-5.3-flash (Cursor 1M context default).
		return Route{config.ProviderZai, config.ModelZaiGLM53Flash, PolicyZai}, nil
	case "glm-5.3":
		return Route{config.ProviderZai, config.ModelZaiGLM53, PolicyZai}, nil
	case "glm-5.3-flash":
		return Route{config.ProviderZai, config.ModelZaiGLM53Flash, PolicyZai}, nil
	case "glm-4.7":
		// Legacy compat — upstream is glm-5.3-flash (replaced glm-4.7).
		return Route{config.ProviderZai, config.ModelZaiGLM53Flash, PolicyZai}, nil
	case config.ModelOpenRouterDeepSeekV4Flash, config.ModelOpenRouterDeepSeekV4Pro:
		return Route{config.ProviderOpenRouter, requested, PolicyDeepSeek}, nil
	case config.ModelOpenRouterZaiGLM53, config.ModelOpenRouterZaiGLM53Flash:
		return Route{config.ProviderOpenRouter, requested, PolicyZai}, nil
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
