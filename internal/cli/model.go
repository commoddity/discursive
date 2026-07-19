package cli

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"

	"discursive/internal/config"
	"discursive/internal/gateway"
)

func newSetModelCmd() *cobra.Command {
	var modelFlag string
	cmd := &cobra.Command{
		Use:   "set-model [alias-or-real-id]",
		Short: "🤖 Choose your default model alias (persists to config)",
		Long:  "🤖  Pick a model alias (kimi-k3, deepseek-v4-flash, etc.).  Resolves to the matching\nupstream provider + real model id.  Cursor Agent still sends the model per request,\nbut this sets the default for status / doctor.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requested := strings.TrimSpace(modelFlag)
			if requested == "" && len(args) == 1 {
				requested = strings.TrimSpace(args[0])
			}
			if requested == "" {
				return fmt.Errorf("model required (arg or --model)")
			}
			return runSetModel(requested)
		},
	}
	cmd.Flags().StringVar(&modelFlag, "model", "", "alias or real model id")
	return cmd
}

func runSetModel(requested string) error {
	setupLogger()
	route, err := gateway.ResolveModel(requested)
	if err != nil {
		return err
	}
	dataRoot, err := resolveDataRoot()
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}
	s.AliasModel = requested
	s.RealModel = route.RealModel
	if err := config.Save(dataRoot, s); err != nil {
		return err
	}
	slog.Info("set model",
		"alias_model", s.AliasModel,
		"real_model", s.RealModel,
		"provider", string(route.Provider),
	)
	return nil
}
