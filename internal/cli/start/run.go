package start

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/daemon"
	"github.com/commoddity/discursive/internal/cli/util"
	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

func runStart(cmd *cobra.Command, opts Options, tunnelFlag, publicURLFlag, logLevelFlag string, background, bgChild bool) error {
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

	// --- logger setup (after possible fork) ---
	if bgChild {
		// Background child: write logs to a rotating log file, detach from terminal.
		logPath := filepath.Join(dataRoot, "gateway.log")
		lw, err := newRotatingWriter(logPath)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		daemon.Detach(lw.file, lw.file)
		level := usage.ParseLogLevel(logLevelFlag)
		util.SetupLoggerToWriter(lw, level)
	} else {
		// Foreground: write logs to stdout.
		util.SetupLogger()
		if logLevelFlag != "" {
			util.SetupLoggerWithLevel(logLevelFlag)
		}
	}

	return serveGateway(opts.Version, dataRoot, settings)
}
