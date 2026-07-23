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

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/gateway"
	"github.com/commoddity/discursive/internal/usageui"
)

func serveGateway(version, dataRoot string, settings config.AppSettings) error {
	live := config.NewLiveSettings(dataRoot, settings)
	snap := live.Snapshot()
	listen := fmt.Sprintf("127.0.0.1:%d", snap.LocalPort)
	srv, err := gateway.NewServer(gateway.ServerConfig{
		ListenAddr: listen,
		GatewayKey: snap.GatewayKey,
		DataRoot:   dataRoot,
		Settings:   &snap,
		Live:       live,
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
		"gateway_key", snap.GatewayKey,
		"session_id", srv.SessionID(),
		"usage_ui_url", "http://127.0.0.1:4002",
		"reasoning_effort", live.EffortMap(),
	)

	uiSrv := startUsageUI(version, srv, live, publicURL)
	defer func() { _ = uiSrv.Shutdown() }()

	pidPath, err := writePIDFile(dataRoot)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(pidPath) }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
