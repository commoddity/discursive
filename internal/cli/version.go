package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is injected by GoReleaser via ldflags:
//
//	-X github.com/commoddity/discursive/internal/cli.Version={{.Version}}
var Version = "0.0.0-dev"

func init() {
	if Version != "0.0.0-dev" {
		return // ldflags already injected a real version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			Version = info.Main.Version
		}
	}
}

// NewVersionCmd returns the version subcommand.
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "🏷️  Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Version)
			return err
		},
	}
}
