package config

import (
	"fmt"
	"strings"
)

// Real model IDs that expose configurable thinking / reasoning_effort.
const (
	ModelKimiK3          = "kimi-k3"
	ModelKimiK26         = "kimi-k2.6"
	ModelDeepSeekV4Pro   = "deepseek-v4-pro"
	ModelDeepSeekV4Flash = "deepseek-v4-flash"
)

// EffortOff disables thinking (K2.6 / DeepSeek). Not used for K3.
const EffortOff = "off"

// EffortOn enables K2.6 thinking (maps to thinking.type=enabled).
const EffortOn = "on"

// ReasoningEffortSpec describes one model row for the usage UI / validation.
type ReasoningEffortSpec struct {
	Model    string
	Provider Provider
	Label    string
	Options  []string // allowed effort values (order = UI order)
	Default  string
}

// ReasoningEffortCatalog is the canonical list of models with configurable effort.
//
// DeepSeek options follow https://api-docs.deepseek.com/guides/thinking_mode :
// thinking on/off plus reasoning_effort high|max only (low/medium/xhigh are
// compatibility aliases normalized in NormalizeReasoningEffort).
//
// Kimi K3 options follow https://platform.kimi.ai/docs/guide/use-reasoning-effort :
// low|high|max (API default is max; product default is low for cost).
func ReasoningEffortCatalog() []ReasoningEffortSpec {
	return []ReasoningEffortSpec{
		{
			Model:    ModelKimiK3,
			Provider: ProviderMoonshot,
			Label:    "Kimi K3",
			Options:  []string{"low", "high", "max"},
			Default:  "low",
		},
		{
			Model:    ModelKimiK26,
			Provider: ProviderMoonshot,
			Label:    "Kimi K2.6",
			Options:  []string{EffortOff, EffortOn},
			Default:  EffortOff,
		},
		{
			Model:    ModelDeepSeekV4Pro,
			Provider: ProviderDeepSeek,
			Label:    "DeepSeek V4 Pro",
			Options:  []string{EffortOff, "high", "max"},
			Default:  EffortOff,
		},
		{
			Model:    ModelDeepSeekV4Flash,
			Provider: ProviderDeepSeek,
			Label:    "DeepSeek V4 Flash",
			Options:  []string{EffortOff, "high", "max"},
			Default:  EffortOff,
		},
	}
}

// DefaultReasoningEffort returns a fresh map of model → default effort.
func DefaultReasoningEffort() map[string]string {
	out := make(map[string]string, len(ReasoningEffortCatalog()))
	for _, spec := range ReasoningEffortCatalog() {
		out[spec.Model] = spec.Default
	}
	return out
}

// NormalizeReasoningEffortMap fills missing keys with defaults and validates known values.
// Unknown model keys are dropped. Invalid values for known models fall back to default.
func NormalizeReasoningEffortMap(in map[string]string) map[string]string {
	out := DefaultReasoningEffort()
	if in == nil {
		return out
	}
	for _, spec := range ReasoningEffortCatalog() {
		if v, ok := in[spec.Model]; ok {
			if norm, err := NormalizeReasoningEffort(spec.Model, v); err == nil {
				out[spec.Model] = norm
			}
		}
	}
	return out
}

// NormalizeReasoningEffort validates (and for DeepSeek, remaps) effort for a known model.
func NormalizeReasoningEffort(model, effort string) (string, error) {
	effort = strings.TrimSpace(strings.ToLower(effort))
	for _, spec := range ReasoningEffortCatalog() {
		if spec.Model != model {
			continue
		}
		if isDeepSeekModel(model) {
			return normalizeDeepSeekEffort(effort)
		}
		for _, opt := range spec.Options {
			if effort == opt {
				return opt, nil
			}
		}
		return "", fmt.Errorf("invalid reasoning effort %q for %s (want %s)", effort, model, strings.Join(spec.Options, "|"))
	}
	return "", fmt.Errorf("model %q does not support configurable reasoning effort", model)
}

func isDeepSeekModel(model string) bool {
	return model == ModelDeepSeekV4Pro || model == ModelDeepSeekV4Flash
}

// normalizeDeepSeekEffort maps DeepSeek effort values per official docs:
// high|max are real; low|medium → high; xhigh → max; off disables thinking.
func normalizeDeepSeekEffort(effort string) (string, error) {
	switch effort {
	case EffortOff, "high", "max":
		return effort, nil
	case "low", "medium":
		return "high", nil
	case "xhigh":
		return "max", nil
	default:
		return "", fmt.Errorf("invalid reasoning effort %q for DeepSeek (want off|high|max)", effort)
	}
}

// EffortForModel returns the configured effort for model, or the catalog default,
// or "" if the model is not in the catalog.
func EffortForModel(m map[string]string, model string) string {
	norm := NormalizeReasoningEffortMap(m)
	if v, ok := norm[model]; ok {
		return v
	}
	return ""
}
