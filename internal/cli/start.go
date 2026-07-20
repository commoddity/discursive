package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
	"github.com/commoddity/discursive/internal/tunnel"
	"github.com/commoddity/discursive/internal/usageui"
)

func newStartCmd() *cobra.Command {
	var tunnelFlag, publicURLFlag, logLevelFlag string
	var backgroundFlag, bgChildFlag bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "🚀 Start the gateway on 127.0.0.1 + launch the Cloudflare tunnel",
		Long: `🚀  Listen on 127.0.0.1 (default port 4001) and supervise the Cloudflare tunnel.

On first run the interactive setup wizard runs automatically if config is
incomplete.  Logs are JSON on stdout — pipe through | jq .
Use Ctrl-C to stop cleanly.

  --background    Detach and run in the background.  Logs go to
                  {dataRoot}/gateway.log — use 'discursive logs' to watch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, tunnelFlag, publicURLFlag, logLevelFlag, backgroundFlag, bgChildFlag)
		},
	}
	cmd.Flags().StringVar(&tunnelFlag, "tunnel", "", "tunnel mode: named, none, or quick (persists to config)")
	cmd.Flags().StringVar(&publicURLFlag, "public-url", "", "public HTTPS base URL ending in /v1 (persists to config)")
	cmd.Flags().StringVar(&logLevelFlag, "log-level", "", "log verbosity: debug, info, warn, error (overrides DISCURSIVE_LOG_LEVEL)")
	cmd.Flags().BoolVar(&backgroundFlag, "background", false, "detach and run in the background")
	cmd.Flags().BoolVar(&bgChildFlag, "_bg", false, "")
	_ = cmd.Flags().MarkHidden("_bg")
	_ = cmd.RegisterFlagCompletionFunc("tunnel", cobra.FixedCompletions(
		[]string{"named", "none", "quick"}, cobra.ShellCompDirectiveNoFileComp,
	))
	_ = cmd.RegisterFlagCompletionFunc("log-level", cobra.FixedCompletions(
		[]string{"debug", "info", "warn", "error"}, cobra.ShellCompDirectiveNoFileComp,
	))
	return cmd
}

func runStart(cmd *cobra.Command, tunnelFlag, publicURLFlag, logLevelFlag string, background, bgChild bool) error {
	setupLogger()

	if logLevelFlag != "" {
		setupLoggerWithLevel(logLevelFlag)
	}

	opts, err := config.DefaultResolveOpts(portableFlag)
	if err != nil {
		return err
	}
	dataRoot, err := config.EnsureDataRoot(opts)
	if err != nil {
		return err
	}
	settings, err := config.Load(dataRoot)
	if err != nil {
		return err
	}

	if err := applyStartFlags(&settings, tunnelFlag, publicURLFlag); err != nil {
		return err
	}

	if config.NeedsSetup(settings) {
		if !stdinIsInteractive(cmd) {
			return fmt.Errorf("setup required: run discursive init (or discursive start in a terminal)")
		}
		if err := runSetup(cmd, initFlags{}, setupOpts{fromStart: true}); err != nil {
			return err
		}
		settings, err = config.Load(dataRoot)
		if err != nil {
			return err
		}
		if err := applyStartFlags(&settings, tunnelFlag, publicURLFlag); err != nil {
			return err
		}
	}

	if err := config.ValidateTunnelSettings(settings); err != nil {
		return err
	}
	if !settings.HasMoonshotKey() || !settings.HasDeepSeekKey() {
		return fmt.Errorf("setup required: both Moonshot and DeepSeek API keys must be saved (run discursive init)")
	}
	if err := config.Save(dataRoot, settings); err != nil {
		return err
	}

	if background && !bgChild {
		return forkBackground(dataRoot)
	}

	if bgChild {
		daemonize(dataRoot)
	}

	return serveGateway(dataRoot, settings)
}

func serveGateway(dataRoot string, settings config.AppSettings) error {
	listen := fmt.Sprintf("127.0.0.1:%d", settings.LocalPort)
	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: listen,
		GatewayKey: settings.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &settings,
	})
	if err != nil {
		return err
	}

	publicURL := settings.PublicBaseURL
	if publicURL != "" {
		if norm, err := config.NormalizePublicBaseURL(publicURL); err == nil {
			publicURL = norm
		}
	}

	slog.Info("gateway starting",
		"listen", listen,
		"data_root", dataRoot,
		"tunnel_mode", config.NormalizeTunnelMode(settings.TunnelMode),
		"public_url", publicURL,
		"has_tunnel_token", settings.HasTunnelToken(),
		"has_moonshot_key", settings.HasMoonshotKey(),
		"has_deepseek_key", settings.HasDeepSeekKey(),
		"gateway_key", settings.GatewayKey,
		"session_id", srv.SessionID(),
		"usage_ui_url", "http://127.0.0.1:4002",
	)

	// Start the usage UI (always-on, loopback only).
	uiSrv := usageui.NewServer("127.0.0.1:4002", srv.Store())
	if err := uiSrv.Start(); err != nil {
		slog.Warn("usage_ui_start_failed", "err", err)
	}
	defer func() { _ = uiSrv.Shutdown() }()

	// Write PID file for stop command.
	pidPath := filepath.Join(dataRoot, "gateway.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer func() { _ = os.Remove(pidPath) }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tunCfg, err := buildTunnelConfig(settings, dataRoot, publicURL)
	if err != nil {
		return err
	}

	tunCtx, tunCancel := context.WithCancel(ctx)
	defer tunCancel()
	go func() {
		if err := tunCfg.Run(tunCtx); err != nil && tunCtx.Err() == nil {
			slog.Warn("tunnel supervisor stopped", "err", err)
		}
	}()

	serveErr := srv.ListenAndServe(ctx)
	if serveErr != nil {
		return serveErr
	}
	slog.Info("gateway stopped")
	return nil
}

// forkBackground re-execs this binary in the background.
func forkBackground(dataRoot string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("can't resolve executable: %w", err)
	}

	// Build args: same as current but replace --background with --_bg.
	args := make([]string, 0, len(os.Args))
	skipNext := false
	for _, a := range os.Args[1:] {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--background" || a == "-background" {
			continue
		}
		if a == "--tunnel" || a == "-tunnel" ||
			a == "--public-url" || a == "-public-url" {
			skipNext = true // skip flag arg — already persisted to config
			continue
		}
		args = append(args, a)
	}
	args = append(args, "--_bg")

	logPath := filepath.Join(dataRoot, "gateway.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	// We'll use the same file for the child's stdout/stderr.  The parent
	// needs a reference so the child inherits the fd; we pass a dup via
	// ExtraFiles below, but since the child's daemonize() re-opens the
	// log path, we don't need to hand it here — just verify writable.
	_ = logFile.Close()

	// Re-exec with --_bg, detaching from the terminal.
	procAttr := &os.ProcAttr{
		Files: []*os.File{nil, nil, nil}, // /dev/null for stdin/stdout/stderr in parent's view
		Env:   os.Environ(),
		Dir:   "",
		Sys:   daemonSysProcAttr(), // setsid() + no new controlling terminal
	}

	proc, err := os.StartProcess(exe, append([]string{exe}, args...), procAttr)
	if err != nil {
		return fmt.Errorf("start background process: %w", err)
	}

	// Write PID before releasing (child will overwrite once it daemonizes).
	pidPath := filepath.Join(dataRoot, "gateway.pid")
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(proc.Pid)), 0o600)

	_, _ = fmt.Fprintf(os.Stderr, "🚀  Gateway started in background (PID: %d)\n", proc.Pid)
	_, _ = fmt.Fprintf(os.Stderr, "📄  Logs:  %s\n", logPath)
	_, _ = fmt.Fprintf(os.Stderr, "💡  Watch logs:  discursive logs --follow\n")
	_, _ = fmt.Fprintf(os.Stderr, "💡  Stop:        discursive stop\n")

	// Release the process — we don't wait for it.
	_ = proc.Release()
	return nil
}

func applyStartFlags(settings *config.AppSettings, tunnelFlag, publicURLFlag string) error {
	if strings.TrimSpace(tunnelFlag) != "" {
		settings.TunnelMode = config.NormalizeTunnelMode(tunnelFlag)
	}
	if strings.TrimSpace(publicURLFlag) != "" {
		norm, err := config.NormalizePublicBaseURL(publicURLFlag)
		if err != nil {
			return err
		}
		settings.PublicBaseURL = norm
	}
	return nil
}

func buildTunnelConfig(settings config.AppSettings, dataRoot, publicURL string) (*tunnel.Config, error) {
	mode := config.NormalizeTunnelMode(settings.TunnelMode)
	cfg := &tunnel.Config{
		Port:      settings.LocalPort,
		DataRoot:  dataRoot,
		PublicURL: publicURL,
		OnURLChange: func(u string) {
			slog.Info("tunnel ready",
				"public_url", u,
				"tunnel_mode", mode,
				"gateway_key", settings.GatewayKey,
				"msg", "Set this URL and key in Cursor Settings → Models → OpenAI API Key / Override Base URL",
			)
		},
	}
	switch mode {
	case config.TunnelModeNamed:
		cfg.Mode = tunnel.ModeNamed
		tok, err := settings.GetTunnelToken(dataRoot)
		if err != nil {
			return nil, err
		}
		if tok == nil || *tok == "" {
			return nil, fmt.Errorf("tunnel mode named requires tunnel token in config (run set --tunnel-token)")
		}
		cfg.Token = *tok
	case config.TunnelModeNone:
		cfg.Mode = tunnel.ModeNone
	case config.TunnelModeQuick:
		cfg.Mode = tunnel.ModeQuick
	default:
		return nil, fmt.Errorf("unknown tunnel mode %q", mode)
	}
	return cfg, nil
}
