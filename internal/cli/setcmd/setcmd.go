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

// NewCmd returns the set subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	var (
		moonshotKey string
		deepseekKey string
		thauraKey   string
		tunnelToken string
		publicURL   string
		gatewayKey  bool
		showKey     bool
		model       string
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "⚙️  Configure Discursive settings",
		Long: `⚙️  Configure Discursive settings with one or more flags.

  # Set upstream API keys
  discursive set --moonshot-key sk-xxx --deepseek-key sk-yyy

  # Tunnel configuration
  discursive set --tunnel-token <token> --public-url https://my-host/v1

  # Rotate gateway key + model alias
  discursive set --rotate-gateway-key --model gpt-4o

  # Show the gateway key while setting
  discursive set --rotate-gateway-key --show-key

Omitting flags leaves the corresponding setting unchanged.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			return runSet(portable, moonshotKey, deepseekKey, thauraKey, tunnelToken, publicURL, gatewayKey, showKey, model)
		},
	}

	cmd.Flags().StringVar(&moonshotKey, "moonshot-key", "", "Moonshot/Kimi API key")
	cmd.Flags().StringVar(&deepseekKey, "deepseek-key", "", "DeepSeek API key")
	cmd.Flags().StringVar(&thauraKey, "thaura-key", "", "Thaura AI API key")
	cmd.Flags().StringVar(&tunnelToken, "tunnel-token", "", "Cloudflare tunnel token")
	cmd.Flags().StringVar(&publicURL, "public-url", "", "public HTTPS base URL (https://<host>/v1)")
	cmd.Flags().BoolVar(&gatewayKey, "rotate-gateway-key", false, "generate a new gateway API key")
	cmd.Flags().BoolVar(&showKey, "show-key", false, "print the full gateway API key (default: masked)")
	cmd.Flags().StringVar(&model, "model", "", "alias or real model id")

	_ = cmd.RegisterFlagCompletionFunc("model", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return util.CompleteModelIDs(toComplete), cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

func runSet(portable func() bool, moonshotKey, deepseekKey, thauraKey, tunnelToken, publicURL string, rotateGateway, showKey bool, modelID string) error {
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

	if moonshotKey != "" {
		plain := strings.TrimSpace(moonshotKey)
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

	if deepseekKey != "" {
		plain := strings.TrimSpace(deepseekKey)
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

	if thauraKey != "" {
		plain := strings.TrimSpace(thauraKey)
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

	if tunnelToken != "" {
		plain := strings.TrimSpace(tunnelToken)
		if plain == "" {
			return fmt.Errorf("empty tunnel token")
		}
		if err := s.SetTunnelToken(dataRoot, plain); err != nil {
			return err
		}
		s.TunnelMode = config.TunnelModeNamed
		anySet = true
	}

	if publicURL != "" {
		norm, err := config.NormalizePublicBaseURL(publicURL)
		if err != nil {
			return fmt.Errorf("invalid public base URL: %w", err)
		}
		s.PublicBaseURL = norm
		anySet = true
	} else if tunnelToken != "" && s.PublicBaseURL == "" {
		return fmt.Errorf("--public-url required when setting --tunnel-token")
	}

	if modelID != "" {
		requested := strings.TrimSpace(modelID)
		route, err := gateway.ResolveModel(requested)
		if err != nil {
			return err
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

	if rotateGateway {
		if err := s.RotateGatewayKey(); err != nil {
			return fmt.Errorf("rotate gateway key: %w", err)
		}
		attrs := []any{
			"has_moonshot_key", s.HasMoonshotKey(),
			"has_deepseek_key", s.HasDeepSeekKey(),
			"has_thaura_key", s.HasThauraKey(),
		}
		attrs = append(attrs, util.GatewayKeyLogAttrs(s.GatewayKey, showKey)...)
		slog.Info("rotated gateway key", attrs...)
		anySet = true
	}

	if !anySet {
		return fmt.Errorf("no flags provided; use --moonshot-key, --deepseek-key, --thaura-key, --tunnel-token, --public-url, --rotate-gateway-key, or --model")
	}

	if err := config.Save(dataRoot, s); err != nil {
		return err
	}

	if tunnelToken != "" || publicURL != "" {
		slog.Info("saved tunnel config",
			"has_tunnel_token", s.HasTunnelToken(),
			"public_url", s.PublicBaseURL,
			"tunnel_mode", config.NormalizeTunnelMode(s.TunnelMode),
		)
	}

	return nil
}
