package config

import "strings"

// VerbositySpec describes one model row for the usage-UI verbosity toggles.
type VerbositySpec struct {
	Model    string
	Provider Provider
	Label    string
	Default  bool
}

// VerbosityCatalog is the canonical list of models with toggle-able output
// verbosity control. Applies the terseness directive + token cap to models
// that tend to emit verbose prose in agentic coding loops (DeepSeek and
// Z.AI GLM alike — the directive is model-agnostic prompt injection).
func VerbosityCatalog() []VerbositySpec {
	return []VerbositySpec{
		{
			Model:    ModelDeepSeekV4Flash,
			Provider: ProviderDeepSeek,
			Label:    "DeepSeek V4 Flash",
			Default:  true,
		},
		{
			Model:    ModelDeepSeekV4Pro,
			Provider: ProviderDeepSeek,
			Label:    "DeepSeek V4 Pro",
			Default:  false,
		},
		{
			Model:    ModelZaiGLM47,
			Provider: ProviderZai,
			Label:    "GLM-4.7",
			Default:  true,
		},
		{
			Model:    ModelZaiGLM53,
			Provider: ProviderZai,
			Label:    "GLM-5.3",
			Default:  false,
		},
	}
}

// DefaultVerbosity returns a fresh map of model → default verbosity enabled.
func DefaultVerbosity() map[string]bool {
	out := make(map[string]bool, len(VerbosityCatalog()))
	for _, spec := range VerbosityCatalog() {
		out[spec.Model] = spec.Default
	}
	return out
}

// NormalizeVerbosityMap fills missing keys with defaults and drops unknown
// model keys.
func NormalizeVerbosityMap(in map[string]bool) map[string]bool {
	out := DefaultVerbosity()
	if in == nil {
		return out
	}
	for _, spec := range VerbosityCatalog() {
		if v, ok := in[spec.Model]; ok {
			out[spec.Model] = v
		}
	}
	return out
}

// VerbosityFor returns whether verbosity control is enabled for a model in the
// map, falling back to the catalog default. Unknown models return false.
func VerbosityFor(m map[string]bool, model string) bool {
	norm := NormalizeVerbosityMap(m)
	if v, ok := norm[model]; ok {
		return v
	}
	// [1m] suffix variants inherit their base model's setting (glm-5.3[1m] →
	// glm-5.3) — the suffix is a Cursor-facing context variant, not a
	// distinct verbosity target.
	if base, ok := strings.CutSuffix(model, "[1m]"); ok {
		return norm[base]
	}
	return false
}
