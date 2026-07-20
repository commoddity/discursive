package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/commoddity/discursive/internal/cli/wizard"
	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/crypto"
)

func newInitCmd() *cobra.Command {
	var (
		moonshotFlag  string
		deepseekFlag  string
		tunnelFlag    string
		publicURLFlag string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "✨ Interactive setup wizard (keys, tunnel, public URL)",
		Long: `✨  First-time setup — prompts for Moonshot + DeepSeek API keys,
a Cloudflare tunnel token, and your public HTTPS URL.

Secrets are encrypted at rest and never sent to Cursor.
Run this once, or let 'discursive start' trigger it automatically.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, initFlags{
				moonshot:  moonshotFlag,
				deepseek:  deepseekFlag,
				tunnel:    tunnelFlag,
				publicURL: publicURLFlag,
			}, setupOpts{forceAll: true})
		},
	}
	cmd.Flags().StringVar(&moonshotFlag, "moonshot-key", "", "Moonshot/Kimi API key (omit to prompt)")
	cmd.Flags().StringVar(&deepseekFlag, "deepseek-key", "", "DeepSeek API key (omit to prompt)")
	cmd.Flags().StringVar(&tunnelFlag, "tunnel-token", "", "Cloudflare tunnel token (omit to prompt)")
	cmd.Flags().StringVar(&publicURLFlag, "public-url", "", "public HTTPS base URL ending in /v1 (omit to prompt)")
	return cmd
}

type initFlags struct {
	moonshot  string
	deepseek  string
	tunnel    string
	publicURL string
}

type setupOpts struct {
	// forceAll prompts for every field (explicit `init`).
	forceAll bool
	// fromStart adjusts messaging when invoked by `start`.
	fromStart bool
}

func runSetup(cmd *cobra.Command, flags initFlags, opts setupOpts) error {
	setupLogger()

	dataRoot, err := resolveDataRoot()
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}

	needMoonshot := opts.forceAll || flags.moonshot != "" || !s.HasMoonshotKey()
	needDeepseek := opts.forceAll || flags.deepseek != "" || !s.HasDeepSeekKey()
	mode := config.NormalizeTunnelMode(s.TunnelMode)
	needTunnel := opts.forceAll || flags.tunnel != "" ||
		(mode == config.TunnelModeNamed && !s.HasTunnelToken())
	needPublicURL := opts.forceAll || flags.publicURL != "" ||
		((mode == config.TunnelModeNamed || mode == config.TunnelModeNone) &&
			strings.TrimSpace(s.PublicBaseURL) == "")

	if !needMoonshot && !needDeepseek && !needTunnel && !needPublicURL {
		slog.Info("setup already complete",
			"data_root", dataRoot,
			"public_url", s.PublicBaseURL,
			"has_moonshot_key", s.HasMoonshotKey(),
			"has_deepseek_key", s.HasDeepSeekKey(),
			"has_tunnel_token", s.HasTunnelToken(),
		)
		return nil
	}

	total := 0
	if needMoonshot {
		total++
	}
	if needDeepseek {
		total++
	}
	if needTunnel {
		total++
	}
	if needPublicURL {
		total++
	}

	// Suppress JSON logs on stdout while wizard is drawing on stderr.
	old := slog.Default()
	if wizInteractive := stdinIsInteractive(cmd); wizInteractive {
		slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{})))
	}
	defer slog.SetDefault(old)

	w := wizard.New(wizard.Options{
		Out:         cmd.ErrOrStderr(),
		In:          cmd.InOrStdin(),
		Interactive: stdinIsInteractive(cmd),
	})
	w.Intro(opts.fromStart)
	step := 0

	if needMoonshot {
		step++
		moonshot, err := w.AskSecret(step, total, "🌙", "Moonshot / Kimi API key",
			"Get your key at https://platform.kimi.ai  →  API Keys.  Your key stays on this machine & is never sent to Cursor.",
			flags.moonshot)
		if err != nil {
			return err
		}
		if err := s.SetMoonshotKey(dataRoot, moonshot); err != nil {
			return err
		}
	}

	if needDeepseek {
		step++
		deepseek, err := w.AskSecret(step, total, "🌊", "DeepSeek API key",
			"Get your key at https://platform.deepseek.com  →  API Keys.  Your key stays on this machine & is never sent to Cursor.",
			flags.deepseek)
		if err != nil {
			return err
		}
		if err := s.SetDeepSeekKey(dataRoot, deepseek); err != nil {
			return err
		}
	}

	if needTunnel {
		step++
		tunnelTok, err := w.AskSecret(step, total, "☁️", "Cloudflare tunnel token",
			"Get started at https://one.dash.cloudflare.com  →  Zero Trust  →  Networks  →  Tunnels.  Create or pick a tunnel, then copy its token.",
			flags.tunnel)
		if err != nil {
			return err
		}
		if err := s.SetTunnelToken(dataRoot, tunnelTok); err != nil {
			return err
		}
		s.TunnelMode = config.TunnelModeNamed
	}

	if needPublicURL {
		step++
		publicRaw, err := w.AskLine(step, total, "🔗", "Public HTTPS base URL",
			"https://<your-hostname>/v1  —  the hostname Cloudflare will route to this machine.  See https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/ for setup.\n     💡 If you're using a Cloudflare Quick Tunnel, skip this — Discursive will auto-detect the ephemeral URL.",
			flags.publicURL)
		if err != nil {
			return err
		}
		publicURL, err := config.NormalizePublicBaseURL(publicRaw)
		if err != nil {
			return fmt.Errorf("invalid public base URL: %w", err)
		}
		s.PublicBaseURL = publicURL
	}

	if err := config.ValidateTunnelSettings(s); err != nil {
		return err
	}
	if !s.HasMoonshotKey() || !s.HasDeepSeekKey() {
		return fmt.Errorf("both Moonshot and DeepSeek API keys are required")
	}
	if err := config.Save(dataRoot, s); err != nil {
		return err
	}

	slog.Info("init complete",
		"data_root", dataRoot,
		"tunnel_mode", config.NormalizeTunnelMode(s.TunnelMode),
		"public_url", s.PublicBaseURL,
		"has_moonshot_key", s.HasMoonshotKey(),
		"has_deepseek_key", s.HasDeepSeekKey(),
		"has_tunnel_token", s.HasTunnelToken(),
		"gateway_key", s.GatewayKey,
		"gateway_key_masked", crypto.MaskSecret(s.GatewayKey),
	)

	w.Finish(opts.fromStart, s.PublicBaseURL, s.LocalPort, s.GatewayKey)
	return nil
}

func stdinIsInteractive(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
