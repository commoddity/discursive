package gateway

import (
	"encoding/json"
	"fmt"

	"github.com/commoddity/discursive/internal/config"
)

// SanitizeConfig controls optional sanitizer behavior.
type SanitizeConfig struct {
	ForceNonStreaming          bool
	SanitizeTools              bool
	InjectReasoningPlaceholder bool // overridden false for K3 unless explicitly set
	MaxTokensDefault           int
	MaxTokensCap               int
	ThinkingDisabled           bool              // retained for tests/compat; K2 uses EffortByModel off|on
	EffortByModel              map[string]string // real model id → effort (from app settings)
}

// DefaultSanitizeConfig returns product defaults.
func DefaultSanitizeConfig() SanitizeConfig {
	return SanitizeConfig{
		SanitizeTools:              true,
		InjectReasoningPlaceholder: true,
		MaxTokensDefault:           defaultMaxTokens,
		MaxTokensCap:               maxTokensCap,
		ThinkingDisabled:           true,
		EffortByModel:              config.DefaultReasoningEffort(),
	}
}

// SanitizeResult is the sanitized body plus resolved provider/model for T05 usage.
type SanitizeResult struct {
	Body     map[string]any
	Provider config.Provider
	Model    string
	Effort   string // effective reasoning effort sent upstream (or "n/a" / "off")
}

// SanitizeRequest applies the full sanitizer pipeline per gateway.mdc order.
func SanitizeRequest(body map[string]any, cfg SanitizeConfig) (SanitizeResult, error) {
	if body == nil {
		return SanitizeResult{}, fmt.Errorf("sanitize: nil body")
	}
	if cfg.MaxTokensDefault <= 0 {
		cfg.MaxTokensDefault = defaultMaxTokens
	}
	if cfg.MaxTokensCap <= 0 {
		cfg.MaxTokensCap = maxTokensCap
	}

	requested := stringField(body, "model")
	if requested == "" {
		return SanitizeResult{}, fmt.Errorf("sanitize: missing model")
	}
	route, err := ResolveModel(requested)
	if err != nil {
		return SanitizeResult{}, err
	}
	body["model"] = route.RealModel

	injectPlaceholder := cfg.InjectReasoningPlaceholder
	if route.Policy == PolicyK3 {
		injectPlaceholder = false
	}

	stripImages := route.Provider == config.ProviderDeepSeek

	applyThinkingPolicy(body, route, cfg)

	if cfg.ForceNonStreaming {
		body["stream"] = false
		delete(body, "stream_options")
	}

	if maxCompletion, ok := body["max_completion_tokens"]; ok {
		if _, has := body["max_tokens"]; !has {
			body["max_tokens"] = maxCompletion
		}
		delete(body, "max_completion_tokens")
	}
	normalizeMaxTokens(body, cfg.MaxTokensDefault, cfg.MaxTokensCap)
	stripUnsupportedParams(body, route)

	adaptCursorResponsesRequest(body, stripImages)
	RepairToolCallIDs(body)

	toolNameMap := map[string]string{}
	if cfg.SanitizeTools {
		normalizeAndSanitizeTools(body, toolNameMap)
		sanitizeToolChoice(body, toolNameMap)
	}

	seedProbeMessageIfNeeded(body)

	if msgs, ok := body["messages"].([]any); ok {
		repairToolCallPairingInPlace(&msgs)
		for i := range msgs {
			if msg, ok := msgs[i].(map[string]any); ok {
				sanitizeMessage(msg, injectPlaceholder, toolNameMap, stripImages)
				msgs[i] = msg
			}
		}
		if stripImages && messagesContainPlaceholder(msgs) {
			msgs = prependSystemNote(msgs, imageStrippedWarning)
		}
		body["messages"] = msgs
	}

	return SanitizeResult{
		Body:     body,
		Provider: route.Provider,
		Model:    route.RealModel,
		Effort:   effectiveEffort(body, route),
	}, nil
}

func applyThinkingPolicy(body map[string]any, route Route, cfg SanitizeConfig) {
	effort := config.EffortForModel(cfg.EffortByModel, route.RealModel)
	switch route.Policy {
	case PolicyK3:
		delete(body, "thinking")
		if effort == "" {
			effort = "low"
		}
		body["reasoning_effort"] = effort
	case PolicyK2:
		delete(body, "reasoning_effort")
		// K2.6 uses thinking on/off only (not reasoning_effort). Config values: off|on.
		if effort == "" || effort == config.EffortOff {
			body["thinking"] = map[string]any{"type": "disabled"}
		} else {
			body["thinking"] = map[string]any{"type": "enabled"}
		}
	case PolicyDeepSeek:
		if effort == "" || effort == config.EffortOff {
			body["thinking"] = map[string]any{"type": "disabled"}
			delete(body, "reasoning_effort")
		} else {
			// Docs: thinking enabled + reasoning_effort high|max only.
			// low/medium → high, xhigh → max (compatibility aliases).
			norm, err := config.NormalizeReasoningEffort(route.RealModel, effort)
			if err != nil || norm == config.EffortOff {
				body["thinking"] = map[string]any{"type": "disabled"}
				delete(body, "reasoning_effort")
			} else {
				body["thinking"] = map[string]any{"type": "enabled"}
				body["reasoning_effort"] = norm
			}
		}
	case PolicyThaura:
		delete(body, "thinking")
		delete(body, "reasoning_effort")
	}
}

// effectiveEffort reports the effort label for logs after policy application.
func effectiveEffort(body map[string]any, route Route) string {
	switch route.Policy {
	case PolicyK3:
		if s, ok := body["reasoning_effort"].(string); ok && s != "" {
			return s
		}
		return "low"
	case PolicyK2:
		if thinking, ok := body["thinking"].(map[string]any); ok {
			if thinking["type"] == "enabled" {
				return config.EffortOn
			}
		}
		return config.EffortOff
	case PolicyDeepSeek:
		if thinking, ok := body["thinking"].(map[string]any); ok {
			if thinking["type"] == "disabled" {
				return config.EffortOff
			}
		}
		if s, ok := body["reasoning_effort"].(string); ok && s != "" {
			return s
		}
		return config.EffortOff
	default:
		return "n/a"
	}
}

func stripUnsupportedParams(body map[string]any, route Route) {
	keys := []string{
		"temperature", "top_p", "n", "presence_penalty", "frequency_penalty",
		"logprobs", "top_logprobs", "seed", "reasoning", "parallel_tool_calls",
		"store", "metadata", "service_tier", "user", "prediction",
		"modalities", "audio", "web_search_options",
	}
	for _, k := range keys {
		delete(body, k)
	}
	switch route.Policy {
	case PolicyK2:
		delete(body, "reasoning_effort")
	case PolicyDeepSeek:
		if !isDeepSeekValidReasoningEffort(body["reasoning_effort"]) {
			delete(body, "reasoning_effort")
		}
	case PolicyThaura:
		delete(body, "reasoning_effort")
	}

	if rf, ok := body["response_format"].(map[string]any); ok {
		typ := stringField(rf, "type")
		if typ != "json_object" && typ != "text" {
			delete(body, "response_format")
		}
	} else if rf != nil {
		if m, ok := body["response_format"].(map[string]any); ok && stringField(m, "type") == "" {
			delete(body, "response_format")
		}
	}
}

func normalizeMaxTokens(body map[string]any, defaultVal, cap int) {
	requested := defaultVal
	if v, ok := body["max_tokens"]; ok {
		if n, ok := jsonNumberInt(v); ok {
			requested = n
		}
	}
	if requested < 1 {
		requested = 1
	}
	if requested > cap {
		requested = cap
	}
	body["max_tokens"] = requested
}

func jsonNumberInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			f, err := n.Float64()
			if err != nil {
				return 0, false
			}
			return int(f), true
		}
		return int(i), true
	default:
		return 0, false
	}
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func mapField(m map[string]any, key string) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil, false
	}
	obj, ok := v.(map[string]any)
	return obj, ok
}

func arrayField(m map[string]any, key string) ([]any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return nil, false
	}
	arr, ok := v.([]any)
	return arr, ok
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
