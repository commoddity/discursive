package initcmd

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
	"github.com/commoddity/discursive/internal/cli/wizard"
	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/crypto"
)

// Flags holds non-interactive init/set values from CLI flags.
type Flags struct {
	Moonshot  string
	Deepseek  string
	Thaura    string
	Tunnel    string
	PublicURL string
}

// Opts controls setup behavior when invoked from init vs start.
type Opts struct {
	ForceAll  bool
	FromStart bool
}

// NewCmd returns the init subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	var (
		moonshotFlag  string
		deepseekFlag  string
		thauraFlag    string
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
			return RunSetup(cmd, portable, Flags{
				Moonshot:  moonshotFlag,
				Deepseek:  deepseekFlag,
				Thaura:    thauraFlag,
				Tunnel:    tunnelFlag,
				PublicURL: publicURLFlag,
			}, Opts{ForceAll: true})
		},
	}
	cmd.Flags().StringVar(&moonshotFlag, "moonshot-key", "", "Moonshot/Kimi API key (omit to prompt)")
	cmd.Flags().StringVar(&deepseekFlag, "deepseek-key", "", "DeepSeek API key (omit to prompt)")
	cmd.Flags().StringVar(&thauraFlag, "thaura-key", "", "Thaura AI API key (optional, omit to skip)")
	cmd.Flags().StringVar(&tunnelFlag, "tunnel-token", "", "Cloudflare tunnel token (omit to prompt)")
	cmd.Flags().StringVar(&publicURLFlag, "public-url", "", "public HTTPS base URL ending in /v1 (omit to prompt)")
	return cmd
}

// RunSetup runs the interactive or flag-driven setup wizard.
func RunSetup(cmd *cobra.Command, portable func() bool, flags Flags, opts Opts) error {
	util.SetupLogger()

	dataRoot, err := util.ResolveDataRoot(portable())
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}

	needMoonshot := opts.ForceAll || flags.Moonshot != "" || !s.HasMoonshotKey()
	needDeepseek := opts.ForceAll || flags.Deepseek != "" || !s.HasDeepSeekKey()
	mode := config.NormalizeTunnelMode(s.TunnelMode)
	needTunnel := opts.ForceAll || flags.Tunnel != "" ||
		(mode == config.TunnelModeNamed && !s.HasTunnelToken())
	needPublicURL := opts.ForceAll || flags.PublicURL != "" ||
		((mode == config.TunnelModeNamed || mode == config.TunnelModeNone) &&
			strings.TrimSpace(s.PublicBaseURL) == "")

	if !needMoonshot && !needDeepseek && !needTunnel && !needPublicURL {
		slog.Info("setup already complete",
			"data_root", dataRoot,
			"public_url", s.PublicBaseURL,
			"has_moonshot_key", s.HasMoonshotKey(),
			"has_deepseek_key", s.HasDeepSeekKey(),
			"has_thaura_key", s.HasThauraKey(),
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
	if wizInteractive := util.StdinIsInteractive(cmd); wizInteractive {
		slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{})))
	}
	defer slog.SetDefault(old)

	w := wizard.New(wizard.Options{
		Out:         cmd.ErrOrStderr(),
		In:          cmd.InOrStdin(),
		Interactive: util.StdinIsInteractive(cmd),
	})
	w.Intro(opts.FromStart)
	step := 0

	if needMoonshot {
		step++
		moonshot, err := w.AskSecret(step, total, "🌙", "Moonshot / Kimi API key",
			"Get your key at https://platform.kimi.ai  →  API Keys.  Your key stays on this machine & is never sent to Cursor.",
			flags.Moonshot)
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
			flags.Deepseek)
		if err != nil {
			return err
		}
		if err := s.SetDeepSeekKey(dataRoot, deepseek); err != nil {
			return err
		}
	}

	// Thaura is optional — only save if a flag was explicitly set.
	if flags.Thaura != "" {
		if err := s.SetThauraKey(dataRoot, flags.Thaura); err != nil {
			return err
		}
	}

	if needTunnel {
		step++
		tunnelTok, err := w.AskSecret(step, total, "☁️", "Cloudflare tunnel token",
			"Get started at https://one.dash.cloudflare.com  →  Zero Trust  →  Networks  →  Tunnels.  Create or pick a tunnel, then copy its token.",
			flags.Tunnel)
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
			flags.PublicURL)
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
		"has_thaura_key", s.HasThauraKey(),
		"has_tunnel_token", s.HasTunnelToken(),
		"gateway_key", s.GatewayKey,
		"gateway_key_masked", crypto.MaskSecret(s.GatewayKey),
	)

	w.Finish(opts.FromStart, s.PublicBaseURL, s.LocalPort, s.GatewayKey)
	return nil
}
