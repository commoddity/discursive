package config

// VerbositySpec describes one model row for the usage-UI verbosity toggles.
type VerbositySpec struct {
	Model    string
	Provider Provider
	Label    string
	Default  bool
}

// VerbosityCatalog is the canonical list of models with toggle-able output
// verbosity control. Today only DeepSeek models are supported — they tend to
// emit verbose reasoning prose, so the terseness directive + token cap apply
// to them.
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
	return false
}
