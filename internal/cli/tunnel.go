package cli

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"discursive/internal/config"
	"discursive/internal/crypto"
)

func newSetTunnelTokenCmd() *cobra.Command {
	var tokenFlag, publicURLFlag string
	cmd := &cobra.Command{
		Use:   "set-tunnel-token",
		Short: "☁️  Save Cloudflare tunnel token + public base URL",
		Long:  "☁️  Named tunnel mode: save your cloudflared token and public hostname (https://<host>/v1).\nOmitting flags prompts interactively.  Get tokens at https://one.dash.cloudflare.com",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setTunnelToken(cmd, tokenFlag, publicURLFlag)
		},
	}
	cmd.Flags().StringVar(&tokenFlag, "token", "", "tunnel token (omit for interactive prompt; or pipe via stdin)")
	cmd.Flags().StringVar(&publicURLFlag, "public-url", "", "public HTTPS base URL ending in /v1")
	return cmd
}

func newSetPublicURLCmd() *cobra.Command {
	var urlFlag string
	cmd := &cobra.Command{
		Use:   "set-public-url",
		Short: "🔗 Save public HTTPS base URL (https://<host>/v1)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setPublicURL(cmd, urlFlag)
		},
	}
	cmd.Flags().StringVar(&urlFlag, "url", "", "public HTTPS base URL ending in /v1 (omit for interactive prompt)")
	return cmd
}

func setTunnelToken(cmd *cobra.Command, tokenFlag, publicURLFlag string) error {
	setupLogger()
	plain, err := readSecretPlain(cmd, "Cloudflare tunnel", tokenFlag)
	if err != nil {
		return err
	}
	if plain == "" {
		return fmt.Errorf("empty tunnel token")
	}

	dataRoot, err := resolveDataRoot()
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}
	if err := s.SetTunnelToken(dataRoot, plain); err != nil {
		return err
	}

	urlIn := strings.TrimSpace(publicURLFlag)
	if urlIn == "" {
		urlIn = strings.TrimSpace(s.PublicBaseURL)
	}
	if urlIn == "" {
		urlIn, err = readLinePlain(cmd, "Public HTTPS base URL (https://<host>/v1)", "")
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(urlIn) == "" {
		return fmt.Errorf("public base URL required for named tunnels (https://<host>/v1); re-run with --public-url or set-public-url")
	}
	norm, err := config.NormalizePublicBaseURL(urlIn)
	if err != nil {
		return fmt.Errorf("invalid public base URL: %w", err)
	}
	s.PublicBaseURL = norm
	s.TunnelMode = config.TunnelModeNamed

	if err := config.Save(dataRoot, s); err != nil {
		return err
	}

	slog.Info("saved tunnel token",
		"token_masked", crypto.MaskSecret(plain),
		"has_tunnel_token", s.HasTunnelToken(),
		"public_url", s.PublicBaseURL,
		"tunnel_mode", config.NormalizeTunnelMode(s.TunnelMode),
	)
	return nil
}

func setPublicURL(cmd *cobra.Command, urlFlag string) error {
	setupLogger()
	raw, err := readLinePlain(cmd, "Public HTTPS base URL (https://<host>/v1)", urlFlag)
	if err != nil {
		return err
	}
	if raw == "" {
		return fmt.Errorf("empty public base URL")
	}
	norm, err := config.NormalizePublicBaseURL(raw)
	if err != nil {
		return fmt.Errorf("invalid public base URL: %w", err)
	}

	dataRoot, err := resolveDataRoot()
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}
	s.PublicBaseURL = norm
	if err := config.Save(dataRoot, s); err != nil {
		return err
	}

	slog.Info("saved public base URL",
		"public_url", s.PublicBaseURL,
		"tunnel_mode", config.NormalizeTunnelMode(s.TunnelMode),
		"has_tunnel_token", s.HasTunnelToken(),
	)
	return nil
}
