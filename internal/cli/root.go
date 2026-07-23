package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/doctor"
	"github.com/commoddity/discursive/internal/cli/initcmd"
	"github.com/commoddity/discursive/internal/cli/loglevel"
	"github.com/commoddity/discursive/internal/cli/logs"
	"github.com/commoddity/discursive/internal/cli/setcmd"
	"github.com/commoddity/discursive/internal/cli/start"
	"github.com/commoddity/discursive/internal/cli/status"
	"github.com/commoddity/discursive/internal/cli/stop"
	"github.com/commoddity/discursive/internal/cli/usage"
	"github.com/commoddity/discursive/internal/cli/util"
	"github.com/commoddity/discursive/internal/config"
)

var portableFlag bool

func portable() bool { return portableFlag }

// NewRoot builds the discursive command tree.
func NewRoot() *cobra.Command {
	var showKey bool
	cmd := &cobra.Command{
		Use:           "discursive",
		Short:         "🌉 Local OpenAI-compatible gateway → Moonshot Kimi & DeepSeek for Cursor",
		Long:          "🌉  Discursive — a local sanitizing proxy that routes Cursor requests to Moonshot Kimi or DeepSeek via a public HTTPS tunnel.\n\n  Pick a provider by changing the model alias in Cursor.  Secrets stay on your machine.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd, showKey)
		},
	}

	cmd.PersistentFlags().BoolVar(&portableFlag, "portable", false, "store data next to the executable")
	cmd.Flags().BoolVar(&showKey, "show-key", false, "print the full gateway API key (default: masked)")

	cmd.AddCommand(NewVersionCmd())
	cmd.AddCommand(initcmd.NewCmd(portable))
	cmd.AddCommand(setcmd.NewCmd(portable))
	cmd.AddCommand(start.NewCmd(start.Options{
		Version:  Version,
		Portable: portable,
		RunSetup: func(cmd *cobra.Command, fromStart bool) error {
			return initcmd.RunSetup(cmd, portable, initcmd.Flags{}, initcmd.Opts{FromStart: fromStart})
		},
	}))
	cmd.AddCommand(stop.NewCmd(portable))
	cmd.AddCommand(status.NewCmd(portable, Version))
	cmd.AddCommand(logs.NewCmd(portable))
	cmd.AddCommand(doctor.NewCmd(portable))
	cmd.AddCommand(usage.NewCmd(portable))
	cmd.AddCommand(loglevel.NewCmd())

	return cmd
}

// Execute runs the root command and returns an exit code.
func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runRoot(cmd *cobra.Command, showKey bool) error {
	_ = cmd
	util.SetupLogger()

	root, err := util.ResolveDataRoot(portable())
	if err != nil {
		return err
	}

	settings, err := config.Load(root)
	if err != nil {
		return err
	}
	if err := config.Save(root, settings); err != nil {
		return err
	}

	keyField := "gateway_key_masked"
	keyValue := util.MaskGatewayKey(settings.GatewayKey)
	if showKey {
		keyField = "gateway_key"
		keyValue = settings.GatewayKey
	}

	out := map[string]any{
		"data_root":        root,
		"local_port":       settings.LocalPort,
		"real_model":       settings.RealModel,
		"alias_model":      settings.AliasModel,
		"has_moonshot_key": settings.HasMoonshotKey(),
		"has_deepseek_key": settings.HasDeepSeekKey(),
		"has_thaura_key":   settings.HasThauraKey(),
		"version":          Version,
		keyField:           keyValue,
	}
	return util.EmitPretty(out)
}
