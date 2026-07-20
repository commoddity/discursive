package tunnel

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"testing"
	"time"
)

func TestHealthURLFromBase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "with /v1 suffix", in: "https://ai.example.com/v1", want: "https://ai.example.com/health"},
		{name: "with trailing slash", in: "https://ai.example.com/v1/", want: "https://ai.example.com/health"},
		{name: "no v1 suffix", in: "https://ai.example.com/", want: "https://ai.example.com/health"},
		{name: "bare host", in: "https://ai.example.com", want: "https://ai.example.com/health"},
		{name: "trycloudflare URL", in: "https://foo.trycloudflare.com/v1", want: "https://foo.trycloudflare.com/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := healthURLFromBase(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestSleepContext(t *testing.T) {
	t.Run("completes before cancel", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now()
		err := sleepContext(ctx, 10*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(start) < 10*time.Millisecond {
			t.Fatal("returned too early")
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := sleepContext(ctx, 10*time.Second)
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
	})
}

func TestApplyDefaults(t *testing.T) {
	c := &Config{
		Mode:      ModeNone,
		Port:      4001,
		PublicURL: "https://ai.example.com/v1",
	}
	c.applyDefaults()
	if c.ExecCommand == nil {
		t.Fatal("ExecCommand nil after applyDefaults")
	}
	if c.EnsureBinary == nil {
		t.Fatal("EnsureBinary nil after applyDefaults")
	}
	if c.KillStale == nil {
		t.Fatal("KillStale nil after applyDefaults")
	}
	if c.HealthCheck == nil {
		t.Fatal("HealthCheck nil after applyDefaults")
	}
	if c.Sleep == nil {
		t.Fatal("Sleep nil after applyDefaults")
	}
	t.Run("does not override custom", func(t *testing.T) {
		custom := func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			return nil
		}
		c := &Config{ExecCommand: custom}
		c.applyDefaults()
		if c.ExecCommand == nil || c.ExecCommand(nil, "") != nil {
			t.Fatal("custom ExecCommand was overridden")
		}
	})
}

func TestEmitURL(t *testing.T) {
	t.Run("calls OnURLChange", func(t *testing.T) {
		var got string
		c := &Config{
			Mode:        ModeNone,
			Logger:      slog.Default(),
			OnURLChange: func(u string) { got = u },
		}
		c.emitURL("https://example.com/v1")
		if got != "https://example.com/v1" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("nil OnURLChange is safe", func(t *testing.T) {
		c := &Config{Mode: ModeNone, Logger: slog.Default()}
		c.emitURL("https://example.com/v1") // must not panic
	})
}

func TestRunNamedMissingToken(t *testing.T) {
	cfg := &Config{
		Mode:      ModeNamed,
		PublicURL: "https://example.com/v1",
	}
	err := cfg.Run(context.Background())
	if err == nil || err.Error() != "named mode requires tunnel token in config (run set --tunnel-token)" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunNamedMissingURL(t *testing.T) {
	cfg := &Config{
		Mode:  ModeNamed,
		Token: "some-token",
	}
	err := cfg.Run(context.Background())
	if err == nil || err.Error() != "named mode requires publicBaseUrl" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunQuickNoBinary(t *testing.T) {
	cfg := &Config{
		Mode: ModeQuick,
		Port: 4001,
		EnsureBinary: func(ctx context.Context, dataRoot string) (string, error) {
			return "", errors.New("no binary")
		},
		KillStale: func(port uint16) int { return 0 },
		Sleep: func(ctx context.Context, d time.Duration) error {
			return context.Canceled // exit the loop
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := cfg.Run(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunQuickCancel(t *testing.T) {
	cfg := &Config{
		Mode: ModeQuick,
		Port: 4001,
		EnsureBinary: func(ctx context.Context, dataRoot string) (string, error) {
			return "/fake/cloudflared", nil
		},
		KillStale: func(port uint16) int { return 0 },
		ExecCommand: func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			// Return a command that will block
			cmd := exec.CommandContext(ctx, "sleep", "10")
			return cmd
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			return nil // don't sleep during test
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = cfg.Run(ctx) // should exit via ctx cancellation
}

func TestRunNoneMissingURL(t *testing.T) {
	cfg := &Config{Mode: ModeNone}
	err := cfg.Run(context.Background())
	if err == nil || err.Error() != "BYO mode requires public URL" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUnknownMode(t *testing.T) {
	cfg := &Config{Mode: "bogus"}
	err := cfg.Run(context.Background())
	if err == nil || err.Error() != `unknown tunnel mode "bogus"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitHealthy(t *testing.T) {
	t.Run("immediate healthy", func(t *testing.T) {
		ctx := context.Background()
		procCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		waitDone := make(chan error, 1)
		c := &Config{
			Logger: slog.Default(),
			HealthCheck: func(ctx context.Context, healthURL string) (bool, error) {
				return true, nil
			},
		}
		healthy := c.waitHealthy(ctx, procCtx, "http://127.0.0.1/health", waitDone, cancel)
		if !healthy {
			t.Fatal("expected healthy")
		}
	})
	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		procCtx, pcancel := context.WithCancel(context.Background())
		defer pcancel()
		waitDone := make(chan error, 1)
		c := &Config{
			Logger:      slog.Default(),
			HealthCheck: func(ctx context.Context, healthURL string) (bool, error) { return false, nil },
		}
		healthy := c.waitHealthy(ctx, procCtx, "http://127.0.0.1/health", waitDone, pcancel)
		if healthy {
			t.Fatal("expected not healthy on cancelled context")
		}
	})
	t.Run("process exits before healthy", func(t *testing.T) {
		ctx := context.Background()
		procCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		waitDone := make(chan error, 1)
		waitDone <- errors.New("exit 1")
		c := &Config{
			Logger: slog.Default(),
		}
		healthy := c.waitHealthy(ctx, procCtx, "http://127.0.0.1/health", waitDone, cancel)
		if healthy {
			t.Fatal("expected not healthy on early exit")
		}
	})
}

func TestSuperviseUntilUnhealthy(t *testing.T) {
	t.Run("process exits", func(t *testing.T) {
		ctx := context.Background()
		cancel := func() {}
		waitDone := make(chan error, 1)
		waitDone <- errors.New("exit 1")
		c := &Config{
			Logger:      slog.Default(),
			HealthCheck: func(ctx context.Context, healthURL string) (bool, error) { return true, nil },
		}
		healthy := c.superviseUntilUnhealthy(ctx, "http://127.0.0.1/health", waitDone, cancel)
		if !healthy {
			t.Fatal("expected true on process exit")
		}
	})
	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		pcancel := func() {}
		waitDone := make(chan error, 1)
		c := &Config{
			Logger:      slog.Default(),
			HealthCheck: func(ctx context.Context, healthURL string) (bool, error) { return true, nil },
		}
		healthy := c.superviseUntilUnhealthy(ctx, "http://127.0.0.1/health", waitDone, pcancel)
		if healthy {
			t.Fatal("expected false on cancelled context")
		}
	})
}
