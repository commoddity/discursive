package config

import "strings"

// ModelDeepSeekV4FlashVisionExp is the DeepSeek vision model used by the
// gateway image-description worker for DeepSeek-routed requests.
const ModelDeepSeekV4FlashVisionExp = "deepseek-v4-flash-vision-exp"

// ModelZaiGLM46v is the Z.AI vision model used by the gateway image worker.
const ModelZaiGLM46v = "glm-4.6v"

// ModelOpenRouterZaiGLM53 is the OpenRouter upstream id for glm-5.3 peak reroute.
const ModelOpenRouterZaiGLM53 = "z-ai/glm-5.3"

// ModelOpenRouterZaiGLM47 is the OpenRouter upstream id for glm-4.7 peak reroute.
const ModelOpenRouterZaiGLM47 = "z-ai/glm-4.7"

// ProviderSpec is the canonical model catalog row for one chat provider.
type ProviderSpec struct {
	BigModel    string
	SmallModel  string
	VisionModel string
	HasPeak     bool
}

var providerCatalog = map[Provider]ProviderSpec{
	ProviderMoonshot: {
		BigModel:    ModelKimiK3,
		SmallModel:  ModelKimiK27,
		VisionModel: ModelKimiK27,
		HasPeak:     false,
	},
	ProviderDeepSeek: {
		BigModel:    ModelDeepSeekV4Pro,
		SmallModel:  ModelDeepSeekV4Flash,
		VisionModel: ModelDeepSeekV4FlashVisionExp,
		HasPeak:     true,
	},
	ProviderZai: {
		BigModel:    ModelZaiGLM53,
		SmallModel:  ModelZaiGLM47,
		VisionModel: ModelZaiGLM46v,
		HasPeak:     true,
	},
	ProviderThaura: {
		BigModel:    "thaura",
		SmallModel:  "thaura",
		VisionModel: "thaura",
		HasPeak:     false,
	},
}

// openRouterTwins maps real model ids to OpenRouter upstream ids for peak reroute.
var openRouterTwins = map[string]string{
	ModelDeepSeekV4Pro:   ModelOpenRouterDeepSeekV4Pro,
	ModelDeepSeekV4Flash: ModelOpenRouterDeepSeekV4Flash,
	ModelZaiGLM53:        ModelOpenRouterZaiGLM53,
	ModelZaiGLM47:        ModelOpenRouterZaiGLM47,
}

// modelToProvider maps every known real or OpenRouter model id to its chat provider.
var modelToProvider map[string]Provider

func init() {
	modelToProvider = make(map[string]Provider)
	for p, spec := range providerCatalog {
		for _, m := range []string{spec.BigModel, spec.SmallModel, spec.VisionModel} {
			if m != "" {
				modelToProvider[m] = p
			}
		}
	}
	for real, orID := range openRouterTwins {
		modelToProvider[orID] = modelToProvider[real]
	}
}

// ProviderSpecFor returns the catalog row for provider.
func ProviderSpecFor(p Provider) (ProviderSpec, bool) {
	spec, ok := providerCatalog[p]
	return spec, ok
}

// ProviderForModel maps a real model id or OpenRouter twin back to its provider.
func ProviderForModel(model string) (Provider, bool) {
	model = strings.TrimSpace(model)
	if base, ok := strings.CutSuffix(model, "[1m]"); ok {
		model = base
	}
	p, ok := modelToProvider[model]
	return p, ok
}

// IsBigModel reports whether model is the big-model slot for provider.
func IsBigModel(provider Provider, model string) bool {
	spec, ok := ProviderSpecFor(provider)
	if !ok {
		return false
	}
	if real, _, ok := OpenRouterRealFor(model); ok {
		model = real
	}
	if base, ok := strings.CutSuffix(model, "[1m]"); ok {
		model = base
	}
	return model == spec.BigModel
}

// SmallModelFor returns the catalog small model for provider ("" if unknown).
func SmallModelFor(provider Provider) string {
	spec, ok := ProviderSpecFor(provider)
	if !ok {
		return ""
	}
	return spec.SmallModel
}

// VisionModelFor returns the catalog vision model for provider ("" if unknown).
func VisionModelFor(provider Provider) string {
	spec, ok := ProviderSpecFor(provider)
	if !ok {
		return ""
	}
	return spec.VisionModel
}

// OpenRouterTwinFor returns the OpenRouter upstream id for a real model id.
func OpenRouterTwinFor(model string) (string, bool) {
	if base, ok := strings.CutSuffix(model, "[1m]"); ok {
		model = base
	}
	twin, ok := openRouterTwins[model]
	return twin, ok
}

// OpenRouterRealFor maps an OpenRouter id back to the real model and provider.
func OpenRouterRealFor(orID string) (string, Provider, bool) {
	for real, twin := range openRouterTwins {
		if twin == orID {
			p, ok := modelToProvider[real]
			return real, p, ok
		}
	}
	return "", "", false
}

// ChatProviders returns chat providers in default-snap priority order.
func ChatProviders() []Provider {
	return []Provider{
		ProviderDeepSeek,
		ProviderZai,
		ProviderMoonshot,
		ProviderThaura,
	}
}

// HasChatProviderKey reports whether at least one chat provider key is configured.
func (s AppSettings) HasChatProviderKey() bool {
	return s.HasMoonshotKey() || s.HasDeepSeekKey() || s.HasZaiKey() || s.HasThauraKey()
}

// IsProviderActive reports whether provider has a configured API key.
func (s AppSettings) IsProviderActive(p Provider) bool {
	switch p {
	case ProviderMoonshot:
		return s.HasMoonshotKey()
	case ProviderDeepSeek:
		return s.HasDeepSeekKey()
	case ProviderZai:
		return s.HasZaiKey()
	case ProviderThaura:
		return s.HasThauraKey()
	case ProviderOpenRouter:
		return s.HasOpenRouterKey()
	default:
		return false
	}
}

// SnapDefaultModelIfNeeded moves RealModel to the first active provider's big
// model when the current default provider has no key.
func (s *AppSettings) SnapDefaultModelIfNeeded() {
	if p, ok := ProviderForModel(s.RealModel); ok && s.IsProviderActive(p) {
		return
	}
	for _, p := range ChatProviders() {
		if !s.IsProviderActive(p) {
			continue
		}
		spec, ok := ProviderSpecFor(p)
		if !ok {
			continue
		}
		s.RealModel = spec.BigModel
		return
	}
}
