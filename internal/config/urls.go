package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Provider identifies an upstream LLM API host.
type Provider string

const (
	// ProviderMoonshot identifies the Moonshot/Kimi API host.
	ProviderMoonshot Provider = "moonshot"
	// ProviderDeepSeek identifies the DeepSeek API host.
	ProviderDeepSeek Provider = "deepseek"
)

// Environment variable overrides for upstream OpenAI-compatible base URLs.
// Defaults below are correct for production; overrides are for staging / debugging only.
const (
	// EnvMoonshotBaseURL overrides DefaultMoonshotBaseURL when non-empty.
	EnvMoonshotBaseURL = "DISCURSIVE_MOONSHOT_BASE_URL"

	// EnvDeepSeekBaseURL overrides DefaultDeepSeekBaseURL when non-empty.
	EnvDeepSeekBaseURL = "DISCURSIVE_DEEPSEEK_BASE_URL"
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
// Local OpenAI schema reference (do not vendor into the binary):
// examples/openai-openapi/
//
// Join chat/completions via ChatCompletionsURL so Moonshot’s /v1 and DeepSeek’s
// host root never become /v1/v1/….
const (
	DefaultMoonshotBaseURL = "https://api.moonshot.ai/v1"
	DefaultDeepSeekBaseURL = "https://api.deepseek.com"
)

// getenv is os.Getenv; tests may swap it.
var getenv = os.Getenv

// UpstreamBaseURL returns the OpenAI-compatible API root for provider.
// Empty env override → hardcoded default. Trims trailing slashes.
func UpstreamBaseURL(provider Provider) (string, error) {
	switch provider {
	case ProviderMoonshot:
		return pickURL(getenv(EnvMoonshotBaseURL), DefaultMoonshotBaseURL), nil
	case ProviderDeepSeek:
		return pickURL(getenv(EnvDeepSeekBaseURL), DefaultDeepSeekBaseURL), nil
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

func pickURL(override, fallback string) string {
	u := strings.TrimSpace(override)
	if u == "" {
		u = fallback
	}
	return strings.TrimRight(u, "/")
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
