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
	return out
}
