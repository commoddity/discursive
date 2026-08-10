// Package verbosity provides toggle-able output-verbosity control for upstream
// models. It reduces verbose prose from models like deepseek-v4-flash via
// three independent mechanisms:
//
//  1. System-message directive injection — appends an authority-marked,
//     numbered-constraint terseness directive to the last system message.
//  2. max_tokens capping — sets a hard output-token ceiling per model.
//  3. Response trimming — removes trailing prose fluff after the last
//     substantive boundary (code block, tool call, bullet list, diff).
//
// It only mutates substantive content in the trailing prose region and never
// truncates code, tool calls, diffs, or bullet lists.
//
// Contract: pure value transform over JSON-decoded request/response bodies;
// must not depend on internal/gateway to avoid import cycles, must not log
// secrets.
package verbosity

import "encoding/json"

// ModelConfig holds verbosity-control settings for a single real model ID.
type ModelConfig struct {
	// SystemMessageDirective is appended to the last system message.
	// Empty means no prompt injection for this model.
	SystemMessageDirective string

	// MaxTokens sets the output-token ceiling for this model. It only ever
	// lowers an existing higher value; it never increases a request's tokens.
	// 0 means no override (use gateway default).
	MaxTokens int

	// TrimEnabled gates response-side trimming for this model.
	TrimEnabled bool
}

// VerbosityConfig maps real model IDs to their per-model settings.
type VerbosityConfig struct {
	Models map[string]ModelConfig
}

// Controller applies verbosity-control transformations per model.
type Controller struct {
	cfg VerbosityConfig
}

// NewController creates a Controller with the given config.
func NewController(cfg VerbosityConfig) *Controller {
	return &Controller{cfg: cfg}
}

// modelConfig returns the ModelConfig for a real model ID, or the zero value
// if the model is not configured.
func (c *Controller) modelConfig(model string) ModelConfig {
	if c == nil || c.cfg.Models == nil {
		return ModelConfig{}
	}
	return c.cfg.Models[model]
}

// configured reports whether any verbosity behavior is enabled for a model.
func (c *Controller) configured(m ModelConfig) bool {
	return m.SystemMessageDirective != "" || m.MaxTokens > 0 || m.TrimEnabled
}

// ApplyRequest mutates the request body with all request-side verbosity
// transformations for the given real model. Returns the matched ModelConfig
// (zero value when the model is not configured). It is a no-op when the model
// is not configured. body may be nil.
func (c *Controller) ApplyRequest(body map[string]any, model string) ModelConfig {
	m := c.modelConfig(model)
	if !c.configured(m) {
		return ModelConfig{}
	}
	if body == nil {
		return m
	}
	if m.SystemMessageDirective != "" {
		injectDirective(body, m.SystemMessageDirective)
	}
	if m.MaxTokens > 0 {
		capTokens(body, m.MaxTokens)
	}
	return m
}

// TrimResponse trims trailing verbose prose from a complete chat-completions
// response body. Returns the mutated body and whether trimming occurred.
// It returns the input unchanged when the model has no configured trim policy
// or when the body does not represent a trimmable assistant answer.
func (c *Controller) TrimResponse(body []byte, model string) ([]byte, bool) {
	m := c.modelConfig(model)
	if !m.TrimEnabled {
		return body, false
	}

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return body, false
	}
	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		return body, false
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return body, false
	}
	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return body, false
	}
	content, ok := msg["content"].(string)
	if !ok {
		// Tool-call-only or non-string content — never trim.
		return body, false
	}
	trimmed := trimProse(content)
	if trimmed == content {
		return body, false
	}
	msg["content"] = trimmed
	out, err := json.Marshal(resp)
	if err != nil {
		return body, false
	}
	return out, true
}

// TrimStreaming trims trailing verbose prose from a buffered streaming content
// string (the full content delta text accumulated for a streamed turn).
// Returns the trimmed string. When the model has no trim policy or no trim is
// warranted, it returns content unchanged.
func (c *Controller) TrimStreaming(content, model string) string {
	m := c.modelConfig(model)
	if !m.TrimEnabled {
		return content
	}
	return trimProse(content)
}
