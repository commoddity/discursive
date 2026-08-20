package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Provider identifies an upstream LLM API host.
type Provider string

const (
	// ProviderMoonshot identifies the Moonshot/Kimi API host.
	ProviderMoonshot Provider = "moonshot"
	// ProviderDeepSeek identifies the DeepSeek API host.
	ProviderDeepSeek Provider = "deepseek"
	// ProviderThaura identifies the Thaura AI API host.
	ProviderThaura Provider = "thaura"
	// ProviderZai identifies the Z.AI API host.
	ProviderZai Provider = "zai"
	// ProviderOpenRouter identifies the OpenRouter API host.
	ProviderOpenRouter Provider = "openrouter"
)

// Hardcoded upstream OpenAI-compatible API roots (no trailing slash).
//
// Moonshot / Kimi
// ---------------
// Docs: https://platform.kimi.ai/
// API index: https://platform.kimi.ai/docs/llms.txt
// Chat completions live under the OpenAI-style /v1 prefix, e.g.
//
//	POST https://api.moonshot.ai/v1/chat/completions
//
// DeepSeek
// --------
// Docs: https://api-docs.deepseek.com/
// Pricing: https://api-docs.deepseek.com/quick_start/pricing
// Official OpenAI-format base_url is the host root (not …/v1), e.g.
//
//	POST https://api.deepseek.com/chat/completions
//
// (Anthropic-compat URL https://api.deepseek.com/anthropic is out of MVP —
// Cursor override path is OpenAI chat completions only.)
//
// Thaura AI
// ---------
// Docs: https://thaura.ai/api-platform
// OpenAI-compat base URL:
//
//	POST https://backend.thaura.ai/v1/chat/completions
//
// Z.AI
// ----
// Docs: https://docs.z.ai/
// GLM Coding Plan: https://docs.z.ai/devpack/overview  (subscription, credits quota)
// OpenAI-compat base URL:
//
//	POST https://api.z.ai/api/coding/paas/v4/chat/completions
//
// Z.AI uses thinking: {type: enabled|disabled} for glm-4.7 and
// glm-5.3 always thinks (disabled is not supported): it supports
// reasoning_effort (low|high|max) with normalization: off/none/minimal → low,
// medium → high, xhigh → max.
//
// OpenRouter
// ---------
// Docs: https://openrouter.ai/docs
// API reference: https://openrouter.ai/docs/api_reference/overview
// OpenAI-compat base URL:
//
//	POST https://openrouter.ai/api/v1/chat/completions
//
// Local OpenAI schema reference (do not vendor into the binary):
// examples/openai-openapi/
//
// Join chat/completions via ChatCompletionsURL so Moonshot’s /v1 and DeepSeek’s
// host root never become /v1/v1/….
const (
	DefaultMoonshotBaseURL   = "https://api.moonshot.ai/v1"
	DefaultDeepSeekBaseURL   = "https://api.deepseek.com"
	DefaultThauraBaseURL     = "https://backend.thaura.ai/v1"
	DefaultZaiBaseURL        = "https://api.z.ai/api/coding/paas/v4"
	DefaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
)

// UpstreamBaseURL returns the OpenAI-compatible API root for provider.
func UpstreamBaseURL(provider Provider) (string, error) {
	switch provider {
	case ProviderMoonshot:
		return DefaultMoonshotBaseURL, nil
	case ProviderDeepSeek:
		return DefaultDeepSeekBaseURL, nil
	case ProviderThaura:
		return DefaultThauraBaseURL, nil
	case ProviderZai:
		return DefaultZaiBaseURL, nil
	case ProviderOpenRouter:
		return DefaultOpenRouterBaseURL, nil
	default:
		return "", fmt.Errorf("unknown provider %q", provider)
	}
}

// ChatCompletionsURL returns {base}/chat/completions for provider.
func ChatCompletionsURL(provider Provider) (string, error) {
	base, err := UpstreamBaseURL(provider)
	if err != nil {
		return "", err
	}
	return joinURLPath(base, "chat", "completions"), nil
}

// ModelsURL returns {base}/models for provider (OpenAI list-models shape).
func ModelsURL(provider Provider) (string, error) {
	base, err := UpstreamBaseURL(provider)
	if err != nil {
		return "", err
	}
	return joinURLPath(base, "models"), nil
}

func joinURLPath(base string, parts ...string) string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// Fallback for odd but still usable absolute strings.
		out := strings.TrimRight(base, "/")
		for _, p := range parts {
			out += "/" + strings.Trim(p, "/")
		}
		return out
	}
	seg := make([]string, 0, 1+len(parts))
	if u.Path != "" && u.Path != "/" {
		seg = append(seg, strings.Trim(u.Path, "/"))
	}
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p != "" {
			seg = append(seg, p)
		}
	}
	u.Path = "/" + strings.Join(seg, "/")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
