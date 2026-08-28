package config

// ThinkingSpec describes one model row that uses the thinking
// {type: enabled|disabled} on/off shape. This is distinct from the
// reasoning_effort catalog (glm-5.3 / glm-5.3-flash / DeepSeek / Kimi K3).
// Models here expose a boolean thinking toggle rather than an effort selector.
type ThinkingSpec struct {
	Model    string
	Provider Provider
	Label    string
	Default  bool
}

// ThinkingEnabledCatalog is the canonical list of models with a thinking
// on/off toggle. Empty — Z.AI glm-5.3 and glm-5.3-flash always think
// (reasoning_effort only); see ReasoningEffortCatalog.
func ThinkingEnabledCatalog() []ThinkingSpec {
	return nil
}

// DefaultThinkingEnabled returns a fresh map of model → thinking-enabled default.
func DefaultThinkingEnabled() map[string]bool {
	out := make(map[string]bool, len(ThinkingEnabledCatalog()))
	for _, spec := range ThinkingEnabledCatalog() {
		out[spec.Model] = spec.Default
	}
	return out
}

// NormalizeThinkingEnabledMap fills missing keys with defaults and drops
// unknown model keys.
func NormalizeThinkingEnabledMap(in map[string]bool) map[string]bool {
	out := DefaultThinkingEnabled()
	if in == nil {
		return out
	}
	for _, spec := range ThinkingEnabledCatalog() {
		if v, ok := in[spec.Model]; ok {
			out[spec.Model] = v
		}
	}
	return out
}
