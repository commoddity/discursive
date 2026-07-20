package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Tunnel modes.
const (
	TunnelModeNamed = "named"
	TunnelModeNone  = "none"
	TunnelModeQuick = "quick"
)

// DefaultTunnelMode is the Agent-capable named Cloudflare tunnel.
const DefaultTunnelMode = TunnelModeNamed

// NormalizeTunnelMode returns a valid mode or default.
func NormalizeTunnelMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case TunnelModeNamed, TunnelModeNone, TunnelModeQuick:
		return strings.TrimSpace(strings.ToLower(mode))
	default:
		return DefaultTunnelMode
	}
}

// NormalizePublicBaseURL ensures https://host/... ends with /v1 (no trailing slash after).
func NormalizePublicBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("empty public base URL")
	}
	if !strings.HasPrefix(s, "https://") {
		return "", fmt.Errorf("public base URL must use https://")
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("parse public base URL: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("public base URL must use https://")
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("public base URL missing host")
	}
	if err := rejectPrivateHost(host); err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path == "" || path == "/" {
		path = "/v1"
	} else if !strings.HasSuffix(path, "/v1") {
		if strings.HasSuffix(path, "/v") {
			return "", fmt.Errorf("public base URL path must end with /v1")
		}
		path = strings.TrimSuffix(path, "/") + "/v1"
	}
	return "https://" + u.Host + path, nil
}

func rejectPrivateHost(host string) error {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("public base URL cannot use localhost")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("public base URL cannot use private or loopback IP")
		}
	}
	return nil
}

// HealthURLFromPublicBase returns the origin + /health for a normalized /v1 base URL.
func HealthURLFromPublicBase(publicBase string) (string, error) {
	norm, err := NormalizePublicBaseURL(publicBase)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(norm)
	if err != nil {
		return "", err
	}
	u.Path = "/health"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// NeedsSetup reports whether start should run interactive setup before serving.
// Requires both upstream API keys plus tunnel settings valid for the current mode.
func NeedsSetup(s AppSettings) bool {
	if !s.HasMoonshotKey() || !s.HasDeepSeekKey() {
		return true
	}
	return ValidateTunnelSettings(s) != nil
}

// ValidateTunnelSettings checks mode-specific requirements.
func ValidateTunnelSettings(s AppSettings) error {
	mode := NormalizeTunnelMode(s.TunnelMode)
	switch mode {
	case TunnelModeNamed:
		if !s.HasTunnelToken() {
			return fmt.Errorf("tunnel mode %q requires a tunnel token (run set --tunnel-token)", mode)
		}
		if strings.TrimSpace(s.PublicBaseURL) == "" {
			return fmt.Errorf("tunnel mode %q requires publicBaseUrl (https://<host>/v1); run discursive init or set --public-url / start --public-url", mode)
		}
		if _, err := NormalizePublicBaseURL(s.PublicBaseURL); err != nil {
			return fmt.Errorf("invalid publicBaseUrl: %w", err)
		}
	case TunnelModeNone:
		if strings.TrimSpace(s.PublicBaseURL) == "" {
			return fmt.Errorf("tunnel mode %q requires publicBaseUrl", mode)
		}
		if _, err := NormalizePublicBaseURL(s.PublicBaseURL); err != nil {
			return fmt.Errorf("invalid publicBaseUrl: %w", err)
		}
	case TunnelModeQuick:
		// no token or public URL required
	default:
		return fmt.Errorf("unknown tunnel mode %q", s.TunnelMode)
	}
	return nil
}
