package start

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/daemon"
	"github.com/commoddity/discursive/internal/cli/util"
	"github.com/commoddity/discursive/internal/config"
)

func runStart(cmd *cobra.Command, opts Options, tunnelFlag, publicURLFlag, logLevelFlag string, background, bgChild bool) error {
	util.SetupLogger()

	if logLevelFlag != "" {
		util.SetupLoggerWithLevel(logLevelFlag)
	}

	// --- resolve data root + load settings ---
	dataRoot, err := util.ResolveDataRoot(opts.Portable())
	if err != nil {
		return err
	}
	settings, err := config.Load(dataRoot)
	if err != nil {
		return err
	}

	if err := ApplyStartFlags(&settings, tunnelFlag, publicURLFlag); err != nil {
		return err
	}

	// --- auto-setup when config is incomplete ---
	if config.NeedsSetup(settings) {
		if !util.StdinIsInteractive(cmd) {
			return fmt.Errorf("setup required: run discursive init (or discursive start in a terminal)")
		}
		if opts.RunSetup == nil {
			return fmt.Errorf("setup required: run discursive init")
		}
		if err := opts.RunSetup(cmd, true); err != nil {
			return err
		}
		settings, err = config.Load(dataRoot)
		if err != nil {
			return err
		}
		if err := ApplyStartFlags(&settings, tunnelFlag, publicURLFlag); err != nil {
			return err
		}
	}

	// --- validate + persist ---
	if err := config.ValidateTunnelSettings(settings); err != nil {
		return err
	}
	if !settings.HasMoonshotKey() || !settings.HasDeepSeekKey() {
		return fmt.Errorf("setup required: both Moonshot and DeepSeek API keys must be saved (run discursive init)")
	}
	if err := config.Save(dataRoot, settings); err != nil {
		return err
	}

	// --- background fork or foreground serve ---
	if background && !bgChild {
		return forkBackground(dataRoot)
	}

	if bgChild {
		daemon.Detach(dataRoot)
	}

	return serveGateway(opts.Version, dataRoot, settings)
}
