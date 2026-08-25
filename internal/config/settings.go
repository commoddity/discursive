package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/commoddity/discursive/internal/crypto"
)

const configFileName = "config.json"

const (
	// EnvOpenRouterSort is the env var that controls the OpenRouter provider
	// routing sort preference. Default is "throughput". Set to empty to disable.
	EnvOpenRouterSort = "DISCURSIVE_OPENROUTER_SORT"
	// DefaultOpenRouterSort is the default OpenRouter sort hint.
	DefaultOpenRouterSort = "throughput"
)

// OpenRouterSort returns the configured OpenRouter provider sort preference.
// Empty disables the sort hint entirely.
func OpenRouterSort() string {
	if v := os.Getenv(EnvOpenRouterSort); v != "" {
		return v
	}
	return DefaultOpenRouterSort
}

// AppSettings is the persisted settings (secrets encrypted at rest).
type AppSettings struct {
	LocalPort              uint16            `json:"localPort"`
	RealModel              string            `json:"realModel"`
	AliasModel             string            `json:"aliasModel"`
	TunnelMode             string            `json:"tunnelMode,omitempty"`
	PublicBaseURL          string            `json:"publicBaseUrl,omitempty"`
	TunnelTokenEncrypted   *string           `json:"tunnelTokenEncrypted,omitempty"`
	MoonshotKeyEncrypted   *string           `json:"moonshotKeyEncrypted,omitempty"`
	DeepSeekKeyEncrypted   *string           `json:"deepseekKeyEncrypted,omitempty"`
	ThauraKeyEncrypted     *string           `json:"thauraKeyEncrypted,omitempty"`
	ZaiKeyEncrypted        *string           `json:"zaiKeyEncrypted,omitempty"`
	OpenRouterKeyEncrypted *string           `json:"openRouterKeyEncrypted,omitempty"`
	GatewayKey             string            `json:"gatewayKey"`
	ReasoningEffort        map[string]string `json:"reasoningEffort,omitempty"`    // real model id → effort
	CompressionEnabled     bool              `json:"compressionEnabled,omitempty"` // legacy: maps to ToolCompressionEnabled
	ToolCompressionEnabled bool              `json:"toolCompressionEnabled,omitempty"`
	Verbosity              map[string]bool   `json:"verbosity,omitempty"`       // real model id → enabled
	ThinkingEnabled        map[string]bool   `json:"thinkingEnabled,omitempty"` // real model id → thinking on/off (GLM-4.7 family)
}

// DefaultSettings returns product defaults (no upstream secrets; empty gateway until Ensure).
func DefaultSettings() AppSettings {
	return AppSettings{
		LocalPort:       DefaultPort,
		RealModel:       DefaultRealModel,
		AliasModel:      DefaultAliasModel,
		TunnelMode:      DefaultTunnelMode,
		ReasoningEffort: DefaultReasoningEffort(),
		ThinkingEnabled: DefaultThinkingEnabled(),
	}
}

// ConfigPath returns {dataRoot}/config.json.
func ConfigPath(dataRoot string) string {
	return filepath.Join(dataRoot, configFileName)
}

// Load reads config.json or returns defaults if missing. Ensures a gateway key exists.
func Load(dataRoot string) (AppSettings, error) {
	path := ConfigPath(dataRoot)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			s := DefaultSettings()
			if err := s.EnsureGatewayKey(); err != nil {
				return AppSettings{}, err
			}
			return s, nil
		}
		return AppSettings{}, fmt.Errorf("read config: %w", err)
	}
	var s AppSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		return AppSettings{}, fmt.Errorf("parse config: %w", err)
	}
	if s.LocalPort == 0 {
		s.LocalPort = DefaultPort
	}
	if s.RealModel == "" {
		s.RealModel = DefaultRealModel
	}
	if s.AliasModel == "" {
		s.AliasModel = DefaultAliasModel
	}
	if s.TunnelMode == "" {
		s.TunnelMode = DefaultTunnelMode
	}
	// Migrate legacy single compression toggle → tool compression.
	s.normalizeCompressionFlags()
	s.ReasoningEffort = NormalizeReasoningEffortMap(s.ReasoningEffort)
	s.ThinkingEnabled = NormalizeThinkingEnabledMap(s.ThinkingEnabled)
	s.SnapDefaultModelIfNeeded()
	if err := s.EnsureGatewayKey(); err != nil {
		return AppSettings{}, err
	}
	return s, nil
}

// Save writes config.json with mode 0600.
func Save(dataRoot string, s AppSettings) error {
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return fmt.Errorf("create data root: %w", err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	path := ConfigPath(dataRoot)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return fmt.Errorf("chmod config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

// normalizeCompressionFlags ports the legacy single compression toggle onto the
// per-feature toggles. The old flag enabled tool-result compression.
func (s *AppSettings) normalizeCompressionFlags() {
	if s.CompressionEnabled {
		if !s.ToolCompressionEnabled {
			s.ToolCompressionEnabled = true
		}
		s.CompressionEnabled = false
	}
}

// EnsureGatewayKey sets a valid gateway key when missing or malformed.
func (s *AppSettings) EnsureGatewayKey() error {
	if crypto.IsOpenAIStyleGatewayKey(s.GatewayKey) {
		return nil
	}
	key, err := crypto.GenerateGatewayKey()
	if err != nil {
		return err
	}
	s.GatewayKey = key
	return nil
}

// RotateGatewayKey replaces the gateway key.
func (s *AppSettings) RotateGatewayKey() error {
	key, err := crypto.GenerateGatewayKey()
	if err != nil {
		return err
	}
	s.GatewayKey = key
	return nil
}

// SetMoonshotKey encrypts and stores the Moonshot API key.
func (s *AppSettings) SetMoonshotKey(dataRoot, plaintext string) error {
	enc, err := crypto.Protect(dataRoot, plaintext)
	if err != nil {
		return err
	}
	s.MoonshotKeyEncrypted = &enc
	return nil
}

// GetMoonshotKey decrypts the stored Moonshot key, or nil if unset.
func (s *AppSettings) GetMoonshotKey(dataRoot string) (*string, error) {
	if s.MoonshotKeyEncrypted == nil || *s.MoonshotKeyEncrypted == "" {
		return nil, nil
	}
	plain, err := crypto.Unprotect(dataRoot, *s.MoonshotKeyEncrypted)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

// SetDeepSeekKey encrypts and stores the DeepSeek API key.
func (s *AppSettings) SetDeepSeekKey(dataRoot, plaintext string) error {
	enc, err := crypto.Protect(dataRoot, plaintext)
	if err != nil {
		return err
	}
	s.DeepSeekKeyEncrypted = &enc
	return nil
}

// GetDeepSeekKey decrypts the stored DeepSeek key, or nil if unset.
func (s *AppSettings) GetDeepSeekKey(dataRoot string) (*string, error) {
	if s.DeepSeekKeyEncrypted == nil || *s.DeepSeekKeyEncrypted == "" {
		return nil, nil
	}
	plain, err := crypto.Unprotect(dataRoot, *s.DeepSeekKeyEncrypted)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

// HasMoonshotKey reports whether an encrypted Moonshot key is present.
func (s AppSettings) HasMoonshotKey() bool {
	return s.MoonshotKeyEncrypted != nil && *s.MoonshotKeyEncrypted != ""
}

// ClearMoonshotKey removes the stored Moonshot API key.
func (s *AppSettings) ClearMoonshotKey() {
	s.MoonshotKeyEncrypted = nil
}

// HasDeepSeekKey reports whether an encrypted DeepSeek key is present.
func (s AppSettings) HasDeepSeekKey() bool {
	return s.DeepSeekKeyEncrypted != nil && *s.DeepSeekKeyEncrypted != ""
}

// ClearDeepSeekKey removes the stored DeepSeek API key.
func (s *AppSettings) ClearDeepSeekKey() {
	s.DeepSeekKeyEncrypted = nil
}

// SetThauraKey encrypts and stores the Thaura AI API key.
func (s *AppSettings) SetThauraKey(dataRoot, plaintext string) error {
	enc, err := crypto.Protect(dataRoot, plaintext)
	if err != nil {
		return err
	}
	s.ThauraKeyEncrypted = &enc
	return nil
}

// GetThauraKey decrypts the stored Thaura key, or nil if unset.
func (s *AppSettings) GetThauraKey(dataRoot string) (*string, error) {
	if s.ThauraKeyEncrypted == nil || *s.ThauraKeyEncrypted == "" {
		return nil, nil
	}
	plain, err := crypto.Unprotect(dataRoot, *s.ThauraKeyEncrypted)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

// HasThauraKey reports whether an encrypted Thaura key is present.
func (s AppSettings) HasThauraKey() bool {
	return s.ThauraKeyEncrypted != nil && *s.ThauraKeyEncrypted != ""
}

// ClearThauraKey removes the stored Thaura API key.
func (s *AppSettings) ClearThauraKey() {
	s.ThauraKeyEncrypted = nil
}

// SetZaiKey encrypts and stores the Z.AI API key.
func (s *AppSettings) SetZaiKey(dataRoot, plaintext string) error {
	enc, err := crypto.Protect(dataRoot, plaintext)
	if err != nil {
		return err
	}
	s.ZaiKeyEncrypted = &enc
	return nil
}

// GetZaiKey decrypts the stored Z.AI key, or nil if unset.
func (s *AppSettings) GetZaiKey(dataRoot string) (*string, error) {
	if s.ZaiKeyEncrypted == nil || *s.ZaiKeyEncrypted == "" {
		return nil, nil
	}
	plain, err := crypto.Unprotect(dataRoot, *s.ZaiKeyEncrypted)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

// HasZaiKey reports whether an encrypted Z.AI key is present.
func (s AppSettings) HasZaiKey() bool {
	return s.ZaiKeyEncrypted != nil && *s.ZaiKeyEncrypted != ""
}

// ClearZaiKey removes the stored Z.AI API key.
func (s *AppSettings) ClearZaiKey() {
	s.ZaiKeyEncrypted = nil
}

// SetOpenRouterKey encrypts and stores the OpenRouter API key.
func (s *AppSettings) SetOpenRouterKey(dataRoot, plaintext string) error {
	enc, err := crypto.Protect(dataRoot, plaintext)
	if err != nil {
		return err
	}
	s.OpenRouterKeyEncrypted = &enc
	return nil
}

// GetOpenRouterKey decrypts the stored OpenRouter key, or nil if unset.
func (s *AppSettings) GetOpenRouterKey(dataRoot string) (*string, error) {
	if s.OpenRouterKeyEncrypted == nil || *s.OpenRouterKeyEncrypted == "" {
		return nil, nil
	}
	plain, err := crypto.Unprotect(dataRoot, *s.OpenRouterKeyEncrypted)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

// HasOpenRouterKey reports whether an encrypted OpenRouter key is present.
func (s AppSettings) HasOpenRouterKey() bool {
	return s.OpenRouterKeyEncrypted != nil && *s.OpenRouterKeyEncrypted != ""
}

// ClearOpenRouterKey removes the stored OpenRouter API key.
func (s *AppSettings) ClearOpenRouterKey() {
	s.OpenRouterKeyEncrypted = nil
}

// SetTunnelToken encrypts and stores the Cloudflare tunnel token.
func (s *AppSettings) SetTunnelToken(dataRoot, plaintext string) error {
	enc, err := crypto.Protect(dataRoot, plaintext)
	if err != nil {
		return err
	}
	s.TunnelTokenEncrypted = &enc
	return nil
}

// GetTunnelToken decrypts the stored tunnel token, or nil if unset.
func (s AppSettings) GetTunnelToken(dataRoot string) (*string, error) {
	if s.TunnelTokenEncrypted == nil || *s.TunnelTokenEncrypted == "" {
		return nil, nil
	}
	plain, err := crypto.Unprotect(dataRoot, *s.TunnelTokenEncrypted)
	if err != nil {
		return nil, err
	}
	return &plain, nil
}

// HasTunnelToken reports whether an encrypted tunnel token is present.
func (s AppSettings) HasTunnelToken() bool {
	return s.TunnelTokenEncrypted != nil && *s.TunnelTokenEncrypted != ""
}
