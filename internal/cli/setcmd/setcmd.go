package setcmd

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/crypto"
	"github.com/commoddity/discursive/internal/gateway"
)

type setOptions struct {
	moonshotKey   string
	deepseekKey   string
	thauraKey     string
	zaiKey        string
	openRouterKey string
	tunnelToken   string
	publicURL     string
	rotateGateway bool
	showKey       bool
	model         string
	clear         []string
}

// NewCmd returns the set subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	var opts setOptions

	cmd := &cobra.Command{
		Use:   "set",
		Short: "⚙️  Configure Discursive settings",
		Long: `⚙️  Configure Discursive settings with one or more flags.

  # Set upstream API keys
  discursive set --moonshot-key sk-xxx --deepseek-key sk-yyy

  # Remove a stored API key (provider becomes inactive)
  discursive set --clear moonshot
  discursive set --clear deepseek --clear zai

  # Optional OpenRouter key (peak-hour fallback)
  discursive set --openrouter-key sk-or-xxx
  discursive set --clear openrouter

  # Tunnel configuration
  discursive set --tunnel-token <token> --public-url https://my-host/v1

  # Rotate gateway key + model alias
  discursive set --rotate-gateway-key --model gpt-4o

  # Show the gateway key while setting
  discursive set --rotate-gateway-key --show-key

Omitting flags leaves the corresponding setting unchanged.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runSet(portable, opts)
		},
	}

	cmd.Flags().StringVar(&opts.moonshotKey, "moonshot-key", "", "Moonshot/Kimi API key")
	cmd.Flags().StringVar(&opts.deepseekKey, "deepseek-key", "", "DeepSeek API key")
	cmd.Flags().StringVar(&opts.thauraKey, "thaura-key", "", "Thaura AI API key")
	cmd.Flags().StringVar(&opts.zaiKey, "zai-key", "", "Z.AI API key (coding plan)")
	cmd.Flags().StringVar(&opts.openRouterKey, "openrouter-key", "", "OpenRouter API key (optional peak-hour fallback)")
	cmd.Flags().StringVar(&opts.tunnelToken, "tunnel-token", "", "Cloudflare tunnel token")
	cmd.Flags().StringVar(&opts.publicURL, "public-url", "", "public HTTPS base URL (https://<host>/v1)")
	cmd.Flags().BoolVar(&opts.rotateGateway, "rotate-gateway-key", false, "generate a new gateway API key")
	cmd.Flags().BoolVar(&opts.showKey, "show-key", false, "print the full gateway API key (default: masked)")
	cmd.Flags().StringVar(&opts.model, "model", "", "alias or real model id")
	cmd.Flags().StringArrayVar(&opts.clear, "clear", nil, "remove stored API key for provider (moonshot, deepseek, zai, thaura, openrouter); repeat flag for multiple")

	_ = cmd.RegisterFlagCompletionFunc("model", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return util.CompleteModelIDs(toComplete), cobra.ShellCompDirectiveNoFileComp
	})
	_ = cmd.RegisterFlagCompletionFunc("clear", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"moonshot", "deepseek", "zai", "thaura", "openrouter"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func runSet(portable func() bool, opts setOptions) error {
	util.SetupLogger()

	dataRoot, err := util.ResolveDataRoot(portable())
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}

	anySet := false

	if opts.moonshotKey != "" {
		plain := strings.TrimSpace(opts.moonshotKey)
		if plain == "" {
			return fmt.Errorf("empty moonshot key")
		}
		if err := s.SetMoonshotKey(dataRoot, plain); err != nil {
			return err
		}
		slog.Info("saved upstream key",
			"provider", "moonshot",
			"key_masked", crypto.MaskSecret(plain),
		)
		anySet = true
	}

	if opts.deepseekKey != "" {
		plain := strings.TrimSpace(opts.deepseekKey)
		if plain == "" {
			return fmt.Errorf("empty deepseek key")
		}
		if err := s.SetDeepSeekKey(dataRoot, plain); err != nil {
			return err
		}
		slog.Info("saved upstream key",
			"provider", "deepseek",
			"key_masked", crypto.MaskSecret(plain),
		)
		anySet = true
	}

	if opts.thauraKey != "" {
		plain := strings.TrimSpace(opts.thauraKey)
		if plain == "" {
			return fmt.Errorf("empty thaura key")
		}
		if err := s.SetThauraKey(dataRoot, plain); err != nil {
			return err
		}
		slog.Info("saved upstream key",
			"provider", "thaura",
			"key_masked", crypto.MaskSecret(plain),
		)
		anySet = true
	}

	if opts.zaiKey != "" {
		plain := strings.TrimSpace(opts.zaiKey)
		if plain == "" {
			return fmt.Errorf("empty zai key")
		}
		if err := s.SetZaiKey(dataRoot, plain); err != nil {
			return err
		}
		slog.Info("saved upstream key",
			"provider", "zai",
			"key_masked", crypto.MaskSecret(plain),
		)
		anySet = true
	}

	if opts.openRouterKey != "" {
		plain := strings.TrimSpace(opts.openRouterKey)
		if plain == "" {
			return fmt.Errorf("empty openrouter key")
		}
		if err := s.SetOpenRouterKey(dataRoot, plain); err != nil {
			return err
		}
		slog.Info("saved upstream key",
			"provider", "openrouter",
			"key_masked", crypto.MaskSecret(plain),
		)
		anySet = true
	}

	clearProviders, err := normalizeClearFlags(opts.clear)
	if err != nil {
		return err
	}
	keyMutation := opts.moonshotKey != "" || opts.deepseekKey != "" || opts.thauraKey != "" ||
		opts.zaiKey != "" || opts.openRouterKey != "" || len(clearProviders) > 0
	if err := applyClearFlags(&s, clearProviders, opts); err != nil {
		return err
	}
	if len(clearProviders) > 0 {
		anySet = true
	}

	if opts.tunnelToken != "" {
		plain := strings.TrimSpace(opts.tunnelToken)
		if plain == "" {
			return fmt.Errorf("empty tunnel token")
		}
		if err := s.SetTunnelToken(dataRoot, plain); err != nil {
			return err
		}
		s.TunnelMode = config.TunnelModeNamed
		anySet = true
	}

	if opts.publicURL != "" {
		norm, err := config.NormalizePublicBaseURL(opts.publicURL)
		if err != nil {
			return fmt.Errorf("invalid public base URL: %w", err)
		}
		s.PublicBaseURL = norm
		anySet = true
	} else if opts.tunnelToken != "" && s.PublicBaseURL == "" {
		return fmt.Errorf("--public-url required when setting --tunnel-token")
	}

	if opts.model != "" {
		requested := strings.TrimSpace(opts.model)
		route, err := gateway.ResolveModel(requested)
		if err != nil {
			return err
		}
		if !s.IsProviderActive(route.Provider) {
			return fmt.Errorf("cannot set model %q: provider %q has no API key configured", requested, route.Provider)
		}
		s.AliasModel = requested
		s.RealModel = route.RealModel
		slog.Info("set model",
			"alias_model", s.AliasModel,
			"real_model", s.RealModel,
			"provider", string(route.Provider),
		)
		anySet = true
	}

	if opts.rotateGateway {
		if err := s.RotateGatewayKey(); err != nil {
			return fmt.Errorf("rotate gateway key: %w", err)
		}
		attrs := []any{
			"has_moonshot_key", s.HasMoonshotKey(),
			"has_deepseek_key", s.HasDeepSeekKey(),
			"has_thaura_key", s.HasThauraKey(),
			"has_zai_key", s.HasZaiKey(),
			"has_openrouter_key", s.HasOpenRouterKey(),
		}
		attrs = append(attrs, util.GatewayKeyLogAttrs(s.GatewayKey, opts.showKey)...)
		slog.Info("rotated gateway key", attrs...)
		anySet = true
	}

	if !anySet {
		return fmt.Errorf("no flags provided; use --moonshot-key, --deepseek-key, --thaura-key, --zai-key, --openrouter-key, --clear, --tunnel-token, --public-url, --rotate-gateway-key, or --model")
	}

	if keyMutation && !s.HasChatProviderKey() {
		return fmt.Errorf("at least one chat provider key is required (moonshot, deepseek, zai, or thaura)")
	}

	s.SnapDefaultModelIfNeeded()

	if err := config.Save(dataRoot, s); err != nil {
		return err
	}

	if opts.tunnelToken != "" || opts.publicURL != "" {
		slog.Info("saved tunnel config",
			"has_tunnel_token", s.HasTunnelToken(),
			"public_url", s.PublicBaseURL,
			"tunnel_mode", config.NormalizeTunnelMode(s.TunnelMode),
		)
	}

	return nil
}

func normalizeClearFlags(raw []string) ([]config.Provider, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[config.Provider]struct{})
	var out []config.Provider
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			p, err := config.ParseClearProvider(part)
			if err != nil {
				return nil, err
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out, nil
}

func applyClearFlags(s *config.AppSettings, providers []config.Provider, opts setOptions) error {
	for _, p := range providers {
		if conflictsWithSet(p, opts) {
			return fmt.Errorf("cannot --clear %s in the same command as setting its key", p)
		}
		s.ClearProviderKey(p)
		slog.Info("cleared upstream key", "provider", string(p))
	}
	return nil
}

func conflictsWithSet(p config.Provider, opts setOptions) bool {
	switch p {
	case config.ProviderMoonshot:
		return opts.moonshotKey != ""
	case config.ProviderDeepSeek:
		return opts.deepseekKey != ""
	case config.ProviderZai:
		return opts.zaiKey != ""
	case config.ProviderThaura:
		return opts.thauraKey != ""
	case config.ProviderOpenRouter:
		return opts.openRouterKey != ""
	default:
		return false
	}
}
