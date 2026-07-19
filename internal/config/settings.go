package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"discursive/internal/crypto"
)

const configFileName = "config.json"

// AppSettings is the persisted settings (secrets encrypted at rest).
type AppSettings struct {
	LocalPort            uint16  `json:"localPort"`
	RealModel            string  `json:"realModel"`
	AliasModel           string  `json:"aliasModel"`
	TunnelMode           string  `json:"tunnelMode,omitempty"`
	PublicBaseURL        string  `json:"publicBaseUrl,omitempty"`
	TunnelTokenEncrypted *string `json:"tunnelTokenEncrypted,omitempty"`
	MoonshotKeyEncrypted *string `json:"moonshotKeyEncrypted,omitempty"`
	DeepSeekKeyEncrypted *string `json:"deepseekKeyEncrypted,omitempty"`
	GatewayKey           string  `json:"gatewayKey"`
}

// DefaultSettings returns product defaults (no upstream secrets; empty gateway until Ensure).
func DefaultSettings() AppSettings {
	return AppSettings{
		LocalPort:  DefaultPort,
		RealModel:  DefaultRealModel,
		AliasModel: DefaultAliasModel,
		TunnelMode: DefaultTunnelMode,
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

// HasDeepSeekKey reports whether an encrypted DeepSeek key is present.
func (s AppSettings) HasDeepSeekKey() bool {
	return s.DeepSeekKeyEncrypted != nil && *s.DeepSeekKeyEncrypted != ""
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
