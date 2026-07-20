package gateway

import (
	"sort"

	"github.com/commoddity/discursive/internal/config"
)

// OptimizeConfig carries optional cache/performance tuning.
type OptimizeConfig struct {
	// PromptCacheKey hints the upstream cache partition.
	// Injected as prompt_cache_key for Moonshot providers only.
	PromptCacheKey string
}

// OptimizeRequest applies cache-friendly transformations to an already-sanitized
// request body, tuned per provider. It mutates body.
func OptimizeRequest(sanitized SanitizeResult, cfg OptimizeConfig) {
	// Inject prompt_cache_key for Moonshot providers (improves KV-cache hit rates).
	if cfg.PromptCacheKey != "" && sanitized.Provider == config.ProviderMoonshot {
		sanitized.Body["prompt_cache_key"] = cfg.PromptCacheKey
	}

	// Sort tools by function.name for deterministic JSON serialization
	// (improves provider-side prompt cache hit rates).
	sortToolsByNameInBody(sanitized.Body)
}

// sortToolsByNameInBody sorts the tools array in place by function.name.
func sortToolsByNameInBody(body map[string]any) {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) <= 1 {
		return
	}
	sort.SliceStable(tools, func(i, j int) bool {
		ti, oki := tools[i].(map[string]any)
		tj, okj := tools[j].(map[string]any)
		if !oki || !okj {
			return false
		}
		fni, _ := mapField(ti, "function")
		fnj, _ := mapField(tj, "function")
		return stringField(fni, "name") < stringField(fnj, "name")
	})
}
