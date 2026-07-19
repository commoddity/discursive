// Package usage holds pricing tables, cost estimates, and per-session usage storage.
//
// Contract: prices real model ids post-alias; never logs secrets; CGO-free.
package usage

import (
	"errors"
	"fmt"

	"discursive/internal/config"
)

// Pricing verified: 2026-07 (see usage.mdc / README).
// Sources: https://platform.kimi.ai/docs/pricing/chat
//
//	https://api-docs.deepseek.com/quick_start/pricing

var ErrUnknownModel = errors.New("unknown model for pricing")

// UsageTokens is token counts for a single request (real model id, post-alias).
type UsageTokens struct {
	PromptTokens     uint64
	CompletionTokens uint64
	CacheHitTokens   uint64 // optional; DeepSeek prompt_cache_hit_tokens
	CacheMissTokens  uint64 // optional; DeepSeek prompt_cache_miss_tokens
}

// moonshotRates USD per 1M tokens (cache hit, input, output).
// Source: https://platform.kimi.ai/docs/pricing/chat
type moonshotRates struct {
	cacheHit, input, output float64
}

var moonshotPricing = map[string]moonshotRates{
	"kimi-k3":                  {0.30, 3.00, 15.00},
	"kimi-k2.7-code":           {0.19, 0.95, 4.00},
	"kimi-k2.7-code-highspeed": {0.38, 1.90, 8.00},
}

// deepseekRates USD per 1M tokens (cache hit, cache miss input, output).
// Source: pricingDeepSeekSource
type deepseekRates struct {
	cacheHit, cacheMiss, output float64
}

var deepseekPricing = map[string]deepseekRates{
	"deepseek-v4-flash": {0.0028, 0.14, 0.28},
	"deepseek-v4-pro":   {0.003625, 0.435, 0.87},
}

// cursorComparisonUSD is REFERENCE ONLY — never used by EstimateUSD.
// Peer reading for CLI/docs (T09); source: usage.mdc Cursor comparison table.
var cursorComparisonUSD = struct {
	composer25Input, composer25Cache, composer25Output float64
}{
	composer25Input: 0.50, composer25Cache: 0.20, composer25Output: 2.50,
}

// CursorComparisonReference returns reference-only Cursor pricing (not billing).
func CursorComparisonReference() (input, cache, output float64) {
	return cursorComparisonUSD.composer25Input, cursorComparisonUSD.composer25Cache, cursorComparisonUSD.composer25Output
}

// EstimateUSD computes estimated cost for provider + real model id.
func EstimateUSD(provider config.Provider, model string, u UsageTokens) (float64, error) {
	switch provider {
	case config.ProviderMoonshot:
		r, ok := moonshotPricing[model]
		if !ok {
			return 0, fmt.Errorf("%w: moonshot %q", ErrUnknownModel, model)
		}
		hit, miss := splitPrompt(u)
		return perMillion(hit, r.cacheHit) +
			perMillion(miss, r.input) +
			perMillion(u.CompletionTokens, r.output), nil
	case config.ProviderDeepSeek:
		r, ok := deepseekPricing[model]
		if !ok {
			return 0, fmt.Errorf("%w: deepseek %q", ErrUnknownModel, model)
		}
		hit, miss := splitPrompt(u)
		return perMillion(hit, r.cacheHit) +
			perMillion(miss, r.cacheMiss) +
			perMillion(u.CompletionTokens, r.output), nil
	default:
		return 0, fmt.Errorf("%w: provider %q", ErrUnknownModel, provider)
	}
}

func splitPrompt(u UsageTokens) (hit, miss uint64) {
	if u.CacheHitTokens > 0 || u.CacheMissTokens > 0 {
		return u.CacheHitTokens, u.CacheMissTokens
	}
	// No cache split: treat full prompt as billable input (cache-miss / input tier).
	return 0, u.PromptTokens
}

func perMillion(tokens uint64, usdPerMTok float64) float64 {
	return float64(tokens) / 1_000_000 * usdPerMTok
}
