// Package usage holds pricing tables, cost estimates, and per-session usage storage.
//
// Contract: prices real model ids post-alias; never logs secrets; CGO-free.
package usage

import (
	"errors"
	"fmt"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

// Pricing verified: 2026-08 (see usage.mdc / README).
// Sources: https://platform.kimi.ai/docs/pricing/chat
//
//	https://api-docs.deepseek.com/quick_start/pricing
//	https://thaura.ai/api-platform
//	https://docs.z.ai/guides/overview/pricing
//	https://openrouter.ai/docs
//
// DeepSeek switched to peak/off-peak billing at 2026-08-16 16:00 UTC; peak
// hours (01:00–04:00 and 06:00–10:00 UTC) bill at 2x the off-peak rates on
// Beijing weekdays. From 2026-08-23 00:00 Beijing, weekends (Beijing time)
// are off-peak all day.

var ErrUnknownModel = errors.New("unknown model for pricing")

// ZaiFlatFeeUSD is the fixed monthly subscription cost for Z.AI GLM Coding Plan
// (not token-based) — used for month projections only. The user is on the Pro
// tier: $64/mo effective (3 months prepaid on an $80/mo list plan), 6x Lite
// usage (5-hour credits 12,000 / weekly 60,000).
const ZaiFlatFeeUSD = 64.00

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
	"kimi-k3":        {0.30, 3.00, 15.00},
	"kimi-k2.7-code": {0.19, 0.95, 4.00},
}

// deepseekRates USD per 1M tokens (cache hit, cache miss input, output).
// Source: pricingDeepSeekSource
type deepseekRates struct {
	cacheHit, cacheMiss, output float64
}

// deepseekPricing is the legacy FLAT card, valid until 2026-08-16 16:00 UTC.
// Retained because pricing is per-request: events recorded before the cutover
// must keep estimating at legacy rates, not the new card.
var deepseekPricing = map[string]deepseekRates{
	"deepseek-v4-flash":            {0.0028, 0.14, 0.28},
	"deepseek-v4-flash-vision-exp": {0.0028, 0.14, 0.28}, // vision worker; same card as flash until published separately
	"deepseek-v4-pro":              {0.003625, 0.435, 0.87},
}

// deepseekPricingOffPeak is the new off-peak card (2026-08-16 16:00 UTC onward).
// Peak = 2x off-peak on every billing item.
var deepseekPricingOffPeak = map[string]deepseekRates{
	"deepseek-v4-flash":            {0.007, 0.22, 0.66},
	"deepseek-v4-flash-vision-exp": {0.007, 0.22, 0.66},
	"deepseek-v4-pro":              {0.022, 0.66, 1.98},
}

// DeepSeekPeakCutover is when the new peak/off-peak card takes effect.
// Source: https://api-docs.deepseek.com/quick_start/pricing
var DeepSeekPeakCutover = time.Date(2026, 8, 16, 16, 0, 0, 0, time.UTC)

// DeepSeekWeekendOffPeakCutover is when Beijing weekend days stop having peak
// billing windows (effective 2026-08-23 00:00 Beijing = 2026-08-22 16:00 UTC).
var DeepSeekWeekendOffPeakCutover = time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)

// beijingTZ is China Standard Time (UTC+8). DeepSeek defines weekday/weekend
// billing in Beijing time.
var beijingTZ = time.FixedZone("Asia/Shanghai", 8*60*60)

// DeepSeekPeakHours reports whether at falls in a DeepSeek peak billing window.
// Peak windows are half-open [01:00,04:00) and [06:00,10:00) UTC (hours
// 1,2,3 and 6,7,8,9) on Beijing weekdays. From DeepSeekWeekendOffPeakCutover
// onward, Beijing Saturday and Sunday are off-peak all day.
func DeepSeekPeakHours(at time.Time) bool {
	utc := at.UTC()
	if !utc.Before(DeepSeekWeekendOffPeakCutover) {
		wd := utc.In(beijingTZ).Weekday()
		if wd == time.Saturday || wd == time.Sunday {
			return false
		}
	}
	h := utc.Hour()
	return (h >= 1 && h < 4) || (h >= 6 && h < 10)
}

// deepseekRateFor selects the rate card for a given billing instant: the legacy
// flat card before the cutover, otherwise the new card with peak hours at 2x.
func deepseekRateFor(model string, at time.Time) (deepseekRates, error) {
	if at.Before(DeepSeekPeakCutover) {
		r, ok := deepseekPricing[model]
		if !ok {
			return deepseekRates{}, fmt.Errorf("%w: deepseek %q", ErrUnknownModel, model)
		}
		return r, nil
	}
	r, ok := deepseekPricingOffPeak[model]
	if !ok {
		return deepseekRates{}, fmt.Errorf("%w: deepseek %q", ErrUnknownModel, model)
	}
	if DeepSeekPeakHours(at) {
		return deepseekRates{cacheHit: r.cacheHit * 2, cacheMiss: r.cacheMiss * 2, output: r.output * 2}, nil
	}
	return r, nil
}

// thauraRates USD per 1M tokens (input, output).
// Source: https://thaura.ai/api-platform
// TODO: confirm whether Thaura exposes cached-token pricing; not documented as of 2026-07.
type thauraRates struct {
	input, output float64
}

var thauraPricing = map[string]thauraRates{
	"thaura": {0.50, 2.00},
}

// zaiRates USD per 1M tokens (cache hit, input, output).
// Source: https://docs.z.ai/guides/overview/pricing
type zaiRates struct {
	cacheHit, input, output float64
}

var zaiPricing = map[string]zaiRates{
	// GLM USD/MTok from https://docs.z.ai/guides/overview/pricing (2026-08).
	// glm-5.3-flash promo (until 2026-09-09 UTC+8): $0.015 cache / $0.075 in / $0.25 out.
	// List rates: $0.03 / $0.15 / $0.50. glm-5.3: $0.26 / $1.40 / $4.40.
	"glm-5.3":       {0.26, 1.40, 4.40},   // cache / input / output
	"glm-5.2":       {0.26, 1.40, 4.40},   // deprecated, same as glm-5.3
	"glm-5.3-flash": {0.015, 0.075, 0.25}, // promo until 2026-09-09 UTC+8; list $0.03/$0.15/$0.50
	"glm-4.7":       {0.015, 0.075, 0.25}, // legacy log id; same as glm-5.3-flash
	"glm-4.6v":      {0.03, 0.12, 0.27},   // vision worker, from 1.2/0.3/2.7 per 10k credits
}

// openrouterRates USD per 1M tokens (cache hit, input, output).
// Source: https://openrouter.ai/deepseek/deepseek-v4-flash-0731
//
//	https://openrouter.ai/deepseek/deepseek-v4-pro-0813
//
// OpenRouter charges one flat list rate year-round — it does NOT have
// peak/off-peak pricing (https://openrouter.ai/blog/insights/why-openrouter-for-deepseek/).
// These are the catalog list rates: flash $0.014/$0.065/$0.14;
// pro $0.022/$0.66/$1.98. Weighted-average "typical blended" provider rates
// (informational): flash ≈ $0.0476 in / $0.384 out; pro ≈ $0.2365 in / $3.174 out.
type openrouterRates struct {
	cacheHit, input, output float64
}

var openrouterPricing = map[string]openrouterRates{
	"deepseek/deepseek-v4-flash-0731":   {0.014, 0.065, 0.14},
	"deepseek/deepseek-v4-pro-0813":     {0.022, 0.66, 1.98},
	config.ModelOpenRouterZaiGLM53:      {0.26, 1.40, 4.40},   // verified 2026-08 openrouter.ai/z-ai/glm-5.3
	config.ModelOpenRouterZaiGLM53Flash: {0.015, 0.075, 0.25}, // Z.AI flash promo USD; list $0.03/$0.15/$0.50
}

// zaiCreditsPerMTok holds official Coding Plan credit multipliers per 10k
// tokens, expressed as credits per 1M tokens (multiplier × 100).
// Source: https://docs.z.ai/devpack/overview (Credit Calculation table).
var zaiCreditsPerMTok = map[string][3]float64{ // {cache_hit, input, output}
	"glm-5.3":       {170, 690, 2400}, // 1.7 / 6.9 / 24
	"glm-5.2":       {170, 690, 2400}, // routed to 5.3 upstream
	"glm-5.3-flash": {56, 230, 800},   // 0.56 / 2.3 / 8 per 10k credits
	"glm-4.7":       {56, 230, 800},   // legacy log id; same as glm-5.3-flash
	"glm-5-turbo":   {150, 570, 2100}, // 1.5 / 5.7 / 21
	"glm-4.6v":      {30, 120, 270},   // 0.3 / 1.2 / 2.7 (vision worker)
}

// ZaiCreditsAt computes coding-plan credits consumed by one usage event.
// Off-peak hours (everything except Mon–Fri 14:00–18:00 SGT = 06:00–10:00 UTC)
// bill at 50% of the standard credit rate. Unknown models return an error.
func ZaiCreditsAt(model string, u UsageTokens, at time.Time) (float64, error) {
	r, ok := zaiCreditsPerMTok[model]
	if !ok {
		return 0, fmt.Errorf("%w: zai credits %q", ErrUnknownModel, model)
	}
	rate := 1.0
	if !ZaiPeakHours(at) {
		rate = 0.5
	}
	hit, miss := splitPrompt(u)
	return rate * (perMillion(hit, r[0]) +
		perMillion(miss, r[1]) +
		perMillion(u.CompletionTokens, r[2])), nil
}

// ZaiPeakHours reports whether the instant falls in the GLM Coding Plan peak
// window: Monday–Friday 14:00–18:00 Singapore time (UTC+8), i.e. weekdays
// 06:00–10:00 UTC. Off-peak usage bills at 50% of the standard credit rate.
func ZaiPeakHours(at time.Time) bool {
	utc := at.UTC()
	wd := utc.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	h := utc.Hour()
	return h >= 6 && h < 10
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

// EstimateUSDAt computes estimated cost for provider + real model id at the
// billing instant (per-request timestamp). For DeepSeek the rate card depends
// on the instant: legacy flat card before 2026-08-16 16:00 UTC, then
// off-peak/peak (Beijing weekdays: peak hours 01:00–04:00 and 06:00–10:00 UTC
// bill at 2x; Beijing weekends off-peak all day from Aug 23 2026).
func EstimateUSDAt(provider config.Provider, model string, u UsageTokens, at time.Time) (float64, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
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
		r, err := deepseekRateFor(model, at)
		if err != nil {
			return 0, err
		}
		hit, miss := splitPrompt(u)
		return perMillion(hit, r.cacheHit) +
			perMillion(miss, r.cacheMiss) +
			perMillion(u.CompletionTokens, r.output), nil
	case config.ProviderThaura:
		r, ok := thauraPricing[model]
		if !ok {
			return 0, fmt.Errorf("%w: thaura %q", ErrUnknownModel, model)
		}
		// No cache split: full prompt billed at input rate.
		return perMillion(u.PromptTokens, r.input) +
			perMillion(u.CompletionTokens, r.output), nil
	case config.ProviderZai:
		r, ok := zaiPricing[model]
		if !ok {
			return 0, fmt.Errorf("%w: zai %q", ErrUnknownModel, model)
		}
		hit, miss := splitPrompt(u)
		return perMillion(hit, r.cacheHit) +
			perMillion(miss, r.input) +
			perMillion(u.CompletionTokens, r.output), nil
	case config.ProviderOpenRouter:
		r, ok := openrouterPricing[model]
		if !ok {
			return 0, fmt.Errorf("%w: openrouter %q", ErrUnknownModel, model)
		}
		hit, miss := splitPrompt(u)
		return perMillion(hit, r.cacheHit) +
			perMillion(miss, r.input) +
			perMillion(u.CompletionTokens, r.output), nil
	default:
		return 0, fmt.Errorf("%w: provider %q", ErrUnknownModel, provider)
	}
}

// EstimateUSD computes estimated cost at the current instant.
func EstimateUSD(provider config.Provider, model string, u UsageTokens) (float64, error) {
	return EstimateUSDAt(provider, model, u, time.Now().UTC())
}

func splitPrompt(u UsageTokens) (hit, miss uint64) {
	if u.CacheHitTokens > 0 || u.CacheMissTokens > 0 {
		hit = u.CacheHitTokens
		miss = u.CacheMissTokens
		// If cache fields don't cover total prompt, remainder is uncached input.
		if covered := hit + miss; u.PromptTokens > covered {
			miss += u.PromptTokens - covered
		}
		return hit, miss
	}
	// No cache split: treat full prompt as billable input (cache-miss / input tier).
	return 0, u.PromptTokens
}

func perMillion(tokens uint64, usdPerMTok float64) float64 {
	return float64(tokens) / 1_000_000 * usdPerMTok
}

// ModelWeight holds the estimated cost for one model — used as a proportional
// weight to distribute confirmed provider spend across models.
type ModelWeight struct {
	Model    string
	Provider config.Provider
	Weight   float64 // estimated USD via EstimateUSD (unrounded, purely proportional)
}

// AllocateByModel distributes a confirmed total provider spend (confirmedUSD)
// across models proportionally to their estimated per-model cost.
//
// If no models are supplied or every model has zero weight, the total is
// returned unallocated and allocatedUSD will be zero.
//
// The returned per-model values are rounded to 6 decimal places.
// unallocatedUSD is the residual due to rounding; caller may add it to the
// highest-weight model or discard it.
func AllocateByModel(confirmedUSD float64, models []ModelWeight) (allocated []ModelWeight, unallocatedUSD float64) {
	var totalWeight float64
	for _, m := range models {
		totalWeight += m.Weight
	}
	if totalWeight <= 0 || confirmedUSD <= 0 {
		return append([]ModelWeight(nil), models...), confirmedUSD
	}

	allocated = make([]ModelWeight, len(models))
	var sum float64
	for i, m := range models {
		share := (m.Weight / totalWeight) * confirmedUSD
		share = RoundUSD(share)
		allocated[i] = ModelWeight{
			Model:    m.Model,
			Provider: m.Provider,
			Weight:   share,
		}
		sum += share
	}
	unallocatedUSD = RoundUSD(confirmedUSD - sum)
	if unallocatedUSD < 0 {
		unallocatedUSD = 0
	}
	return allocated, unallocatedUSD
}
