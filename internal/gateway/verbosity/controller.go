// Package verbosity provides toggle-able output-verbosity control for upstream
// models. It coerces models like deepseek-v4-flash to be less verbose via two
// request-side mechanisms — it never edits response content:
//
//  1. System-message directive injection — appends an authority-marked,
//     numbered-constraint terseness directive to the last system message.
//  2. max_tokens capping — sets a hard output-token ceiling per model.
//
// Contract: pure value transform over JSON-decoded request bodies; must not
// depend on internal/gateway to avoid import cycles, must not log secrets.
package verbosity

// ModelConfig holds verbosity-control settings for a single real model ID.
type ModelConfig struct {
	// SystemMessageDirective is appended to the last system message.
	// Empty means no prompt injection for this model.
	SystemMessageDirective string

	// MaxTokens sets the output-token ceiling for this model. It only ever
	// lowers an existing higher value; it never increases a request's tokens.
	// 0 means no override (use gateway default).
	MaxTokens int
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
	return m.SystemMessageDirective != "" || m.MaxTokens > 0
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
