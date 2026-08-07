package start

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
	"github.com/commoddity/discursive/internal/usageui"
)

func serveGateway(version, dataRoot string, settings config.AppSettings, smartRouter, diagnosticDump bool) error {
	live := config.NewLiveSettings(dataRoot, settings)
	snap := live.Snapshot()
	listen := fmt.Sprintf("127.0.0.1:%d", snap.LocalPort)
	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr:            listen,
		GatewayKey:            snap.GatewayKey,
		DataRoot:              dataRoot,
		Settings:              &snap,
		Live:                  live,
		SmartRouterEnabled:    smartRouter,
		DiagnosticDumpEnabled: diagnosticDump,
	})
	if err != nil {
		return err
	}

	publicURL := normalizePublicURL(snap.PublicBaseURL)

	slog.Info("gateway starting",
		"listen", listen,
		"data_root", dataRoot,
		"tunnel_mode", config.NormalizeTunnelMode(snap.TunnelMode),
		"public_url", publicURL,
		"has_tunnel_token", live.HasTunnelToken(),
		"has_moonshot_key", live.HasMoonshotKey(),
		"has_deepseek_key", live.HasDeepSeekKey(),
		"has_thaura_key", live.HasThauraKey(),
		"has_zai_key", live.HasZaiKey(),
		"gateway_key", snap.GatewayKey,
		"session_id", srv.SessionID(),
		"usage_ui_url", "http://127.0.0.1:4002",
		"reasoning_effort", live.EffortMap(),
		"smart_router", smartRouter,
		"diagnostic_dump", diagnosticDump,
	)

	uiSrv := startUsageUI(version, srv, live, publicURL)
	defer func() { _ = uiSrv.Shutdown() }()

	pidPath, err := writePIDFile(dataRoot)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(pidPath) }()

	stopFile := filepath.Join(dataRoot, "gateway.stop")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Background child: also watch for SIGTERM from discursive stop, but log
	// it at debug level since spurious SIGTERMs are expected in some environments.
	// The primary stop mechanism for background is the stop file.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			slog.Debug("ignoring signal in background", "signal", sig.String(), "pid", os.Getpid())
		}
	}()
	defer signal.Stop(sigCh)

	tunCfg, err := BuildTunnelConfig(snap, dataRoot, publicURL)
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

	// Poll for stop file — used by `discursive stop` to request clean shutdown
	// without relying on SIGTERM (which may be sent spuriously by the environment).
	go pollStopFile(ctx, stopFile, stop)

	serveErr := srv.ListenAndServe(ctx)
	if serveErr != nil {
		return serveErr
	}
	slog.Info("gateway stopped")
	return nil
}

func normalizePublicURL(raw string) string {
	if raw == "" {
		return ""
	}
	if norm, err := config.NormalizePublicBaseURL(raw); err == nil {
		return norm
	}
	return raw
}

func startUsageUI(version string, srv *gateway.Server, live *config.LiveSettings, publicURL string) *usageui.Server {
	snap := live.Snapshot()
	uiSrv := usageui.NewServer("127.0.0.1:4002", srv.Store())
	uiSrv.SetLive(live)
	uiSrv.SetHealth(usageui.HealthInfo{
		Version:        version,
		PID:            os.Getpid(),
		HasMoonshotKey: live.HasMoonshotKey(),
		HasDeepSeekKey: live.HasDeepSeekKey(),
		HasThauraKey:   live.HasThauraKey(),
		HasZaiKey:      live.HasZaiKey(),
		TunnelMode:     config.NormalizeTunnelMode(snap.TunnelMode),
		PublicURL:      publicURL,
		LocalPort:      int(snap.LocalPort),
		GatewayKey:     snap.GatewayKey,
	})
	uiSrv.SetKeySource(usageui.KeySource{
		Moonshot: func() (string, bool) {
			k, err := live.GetMoonshotKey()
			if err != nil || k == nil || *k == "" {
				return "", false
			}
			return *k, true
		},
		DeepSeek: func() (string, bool) {
			k, err := live.GetDeepSeekKey()
			if err != nil || k == nil || *k == "" {
				return "", false
			}
			return *k, true
		},
		Zai: func() (string, bool) {
			k, err := live.GetZaiKey()
			if err != nil || k == nil || *k == "" {
				return "", false
			}
			return *k, true
		},
	})
	if err := uiSrv.Start(); err != nil {
		slog.Warn("usage_ui_start_failed", "err", err)
	}
	return uiSrv
}

func writePIDFile(dataRoot string) (string, error) {
	pidPath := filepath.Join(dataRoot, "gateway.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return "", fmt.Errorf("write pid file: %w", err)
	}
	return pidPath, nil
}

// serveWithWatchdog runs serveGateway in a loop for background child processes.
// Clean shutdown (via `discursive stop` stop-file) exits the loop. Crashes and
// listen errors restart after a short delay.
func serveWithWatchdog(version, dataRoot string, settings config.AppSettings, smartRouter, diagnosticDump bool) error {
	const restartDelay = 2 * time.Second

	for {
		err := serveGateway(version, dataRoot, settings, smartRouter, diagnosticDump)
		if err == nil {
			slog.Info("gateway stopped cleanly")
			return nil
		}

		slog.Warn("gateway exited with error, restarting",
			"err", err,
			"delay_seconds", restartDelay.Seconds(),
		)

		// Reload settings in case the user has changed something.
		loaded, loadErr := config.Load(dataRoot)
		if loadErr == nil {
			settings = loaded
		}

		time.Sleep(restartDelay)
	}
}

// pollStopFile watches for a stop file (written by `discursive stop`).
// When found, it logs and cancels the server context for clean shutdown.
func pollStopFile(ctx context.Context, path string, cancel context.CancelFunc) {
	// Check immediately, then poll — stop must not wait on a slow ticker.
	if _, err := os.Stat(path); err == nil {
		slog.Info("stop file detected, shutting down", "path", path)
		_ = os.Remove(path)
		cancel()
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				slog.Info("stop file detected, shutting down", "path", path)
				_ = os.Remove(path)
				cancel()
				return
			}
		}
	}
}
