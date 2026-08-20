package config

import (
	"fmt"
	"sync"
)

// LiveSettings is a mutex-guarded AppSettings shared by the gateway and usage UI
// so effort changes apply without restarting the process.
type LiveSettings struct {
	mu       sync.RWMutex
	settings AppSettings
	dataRoot string
}

// NewLiveSettings wraps settings loaded for a data root.
func NewLiveSettings(dataRoot string, s AppSettings) *LiveSettings {
	s.ReasoningEffort = NormalizeReasoningEffortMap(s.ReasoningEffort)
	s.Verbosity = NormalizeVerbosityMap(s.Verbosity)
	s.ThinkingEnabled = NormalizeThinkingEnabledMap(s.ThinkingEnabled)
	return &LiveSettings{settings: s, dataRoot: dataRoot}
}

// DataRoot returns the app data directory.
func (l *LiveSettings) DataRoot() string {
	return l.dataRoot
}

// Snapshot returns a copy of current settings (including encrypted key fields).
func (l *LiveSettings) Snapshot() AppSettings {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return cloneSettings(l.settings)
}

// EffortMap returns a copy of the normalized per-model effort map.
func (l *LiveSettings) EffortMap() map[string]string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return NormalizeReasoningEffortMap(l.settings.ReasoningEffort)
}

// EffortFor returns configured effort for a real model id ("" if unsupported).
func (l *LiveSettings) EffortFor(model string) string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return EffortForModel(l.settings.ReasoningEffort, model)
}

// SetReasoningEffort validates, applies, and persists the full effort map.
// Only catalog models are accepted; missing keys keep current/default values.
func (l *LiveSettings) SetReasoningEffort(updates map[string]string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	merged := NormalizeReasoningEffortMap(l.settings.ReasoningEffort)
	for model, effort := range updates {
		norm, err := NormalizeReasoningEffort(model, effort)
		if err != nil {
			return err
		}
		merged[model] = norm
	}
	l.settings.ReasoningEffort = merged
	if err := Save(l.dataRoot, l.settings); err != nil {
		return fmt.Errorf("save reasoning effort: %w", err)
	}
	return nil
}

// GatewayKey returns the current gateway API key.
func (l *LiveSettings) GatewayKey() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GatewayKey
}

// LocalPort returns the configured local gateway port.
func (l *LiveSettings) LocalPort() uint16 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.LocalPort
}

// ToolCompressionEnabled returns the live tool-result compression toggle state.
func (l *LiveSettings) ToolCompressionEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.ToolCompressionEnabled
}

// SetToolCompressionEnabled updates the live tool-result compression toggle and
// persists it. "on" is safe only when a DeepSeek key is present so summaries
// can be generated fail-open; callers (start CLI) pre-check that.
func (l *LiveSettings) SetToolCompressionEnabled(v bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.settings.ToolCompressionEnabled = v
	if err := Save(l.dataRoot, l.settings); err != nil {
		return fmt.Errorf("save tool compression enabled: %w", err)
	}
	return nil
}

// VerbosityMap returns a copy of the normalized per-model verbosity map.
func (l *LiveSettings) VerbosityMap() map[string]bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return NormalizeVerbosityMap(l.settings.Verbosity)
}

// VerbosityFor returns configured verbosity for a real model id.
func (l *LiveSettings) VerbosityFor(model string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return VerbosityFor(l.settings.Verbosity, model)
}

// SetVerbosity updates the live verbosity toggle for model and persists it.
func (l *LiveSettings) SetVerbosity(model string, enabled bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	norm := NormalizeVerbosityMap(l.settings.Verbosity)
	if _, ok := norm[model]; !ok {
		return fmt.Errorf("model %q does not support verbosity control", model)
	}
	norm[model] = enabled
	l.settings.Verbosity = norm
	if err := Save(l.dataRoot, l.settings); err != nil {
		return fmt.Errorf("save verbosity: %w", err)
	}
	return nil
}

// ThinkingEnabledMap returns a copy of the normalized per-model thinking
// (on/off) map for the GLM-4.7 family.
func (l *LiveSettings) ThinkingEnabledMap() map[string]bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return NormalizeThinkingEnabledMap(l.settings.ThinkingEnabled)
}

// ThinkingEnabledFor returns the live thinking toggle for a real model id in
// the thinking-enabled catalog (GLM-4.7 family). Unknown models return false.
func (l *LiveSettings) ThinkingEnabledFor(model string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	norm := NormalizeThinkingEnabledMap(l.settings.ThinkingEnabled)
	v, ok := norm[model]
	return ok && v
}

// SetThinkingEnabled updates the live thinking toggle for model and persists.
func (l *LiveSettings) SetThinkingEnabled(model string, enabled bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	norm := NormalizeThinkingEnabledMap(l.settings.ThinkingEnabled)
	if _, ok := norm[model]; !ok {
		return fmt.Errorf("model %q does not support a thinking toggle", model)
	}
	norm[model] = enabled
	l.settings.ThinkingEnabled = norm
	if err := Save(l.dataRoot, l.settings); err != nil {
		return fmt.Errorf("save thinking-enabled: %w", err)
	}
	return nil
}

// HasMoonshotKey reports whether a Moonshot key is configured.
func (l *LiveSettings) HasMoonshotKey() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.HasMoonshotKey()
}

// HasDeepSeekKey reports whether a DeepSeek key is configured.
func (l *LiveSettings) HasDeepSeekKey() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.HasDeepSeekKey()
}

// HasThauraKey reports whether a Thaura key is configured.
func (l *LiveSettings) HasThauraKey() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.HasThauraKey()
}

// HasZaiKey reports whether a Z.AI key is configured.
func (l *LiveSettings) HasZaiKey() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.HasZaiKey()
}

// HasOpenRouterKey reports whether an OpenRouter key is configured.
func (l *LiveSettings) HasOpenRouterKey() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.HasOpenRouterKey()
}

// HasTunnelToken reports whether a tunnel token is configured.
func (l *LiveSettings) HasTunnelToken() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.HasTunnelToken()
}

// GetMoonshotKey decrypts the stored Moonshot key.
func (l *LiveSettings) GetMoonshotKey() (*string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GetMoonshotKey(l.dataRoot)
}

// GetDeepSeekKey decrypts the stored DeepSeek key.
func (l *LiveSettings) GetDeepSeekKey() (*string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GetDeepSeekKey(l.dataRoot)
}

// GetThauraKey decrypts the stored Thaura key.
func (l *LiveSettings) GetThauraKey() (*string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GetThauraKey(l.dataRoot)
}

// GetZaiKey decrypts the stored Z.AI key.
func (l *LiveSettings) GetZaiKey() (*string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GetZaiKey(l.dataRoot)
}

// GetOpenRouterKey decrypts the stored OpenRouter key.
func (l *LiveSettings) GetOpenRouterKey() (*string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GetOpenRouterKey(l.dataRoot)
}

// GetTunnelToken decrypts the stored tunnel token.
func (l *LiveSettings) GetTunnelToken() (*string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.settings.GetTunnelToken(l.dataRoot)
}

func cloneSettings(s AppSettings) AppSettings {
	out := s
	if s.ReasoningEffort != nil {
		out.ReasoningEffort = make(map[string]string, len(s.ReasoningEffort))
		for k, v := range s.ReasoningEffort {
			out.ReasoningEffort[k] = v
		}
	}
	if s.Verbosity != nil {
		out.Verbosity = make(map[string]bool, len(s.Verbosity))
		for k, v := range s.Verbosity {
			out.Verbosity[k] = v
		}
	}
	if s.ThinkingEnabled != nil {
		out.ThinkingEnabled = make(map[string]bool, len(s.ThinkingEnabled))
		for k, v := range s.ThinkingEnabled {
			out.ThinkingEnabled[k] = v
		}
	}
	return out
}
