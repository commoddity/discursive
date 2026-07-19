package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	InitialBackoff    = 3 * time.Second
	MaxBackoff        = 30 * time.Second
	BackoffReset      = 120 * time.Second
	URLWaitTimeout    = 45 * time.Second
	HealthInterval    = 60 * time.Second
	HealthMaxFails    = 2
	healthOKMaxStatus = 530
)

var tryCloudflareRE = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// Mode identifies how the public URL is provided.
type Mode string

const (
	ModeNamed Mode = "named"
	ModeNone  Mode = "none"
	ModeQuick Mode = "quick"
)

// Config drives tunnel supervision.
type Config struct {
	Mode        Mode
	Port        uint16
	DataRoot    string
	PublicURL   string // normalized https://host/v1 for named/none
	Token       string // decrypted; named only
	OnURLChange func(publicURL string)
	Logger      *slog.Logger

	ExecCommand  func(ctx context.Context, name string, arg ...string) *exec.Cmd
	EnsureBinary func(ctx context.Context, dataRoot string) (string, error)
	KillStale    func(port uint16) int
	HealthCheck  func(ctx context.Context, healthURL string) (bool, error)
	Sleep        func(context.Context, time.Duration) error
}

// Run supervises the tunnel until ctx is cancelled.
func (c *Config) Run(ctx context.Context) error {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	c.applyDefaults()

	switch c.Mode {
	case ModeNone:
		return c.runNone(ctx)
	case ModeNamed:
		return c.runNamed(ctx)
	case ModeQuick:
		return c.runQuick(ctx)
	default:
		return fmt.Errorf("unknown tunnel mode %q", c.Mode)
	}
}

func (c *Config) applyDefaults() {
	if c.ExecCommand == nil {
		c.ExecCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
			return exec.CommandContext(ctx, name, arg...)
		}
	}
	if c.EnsureBinary == nil {
		c.EnsureBinary = EnsureCloudflared
	}
	if c.KillStale == nil {
		c.KillStale = KillStaleUnix
	}
	if c.HealthCheck == nil {
		c.HealthCheck = DefaultHealthCheck
	}
	if c.Sleep == nil {
		c.Sleep = sleepContext
	}
}

func (c *Config) runNone(ctx context.Context) error {
	if c.PublicURL == "" {
		return errors.New("BYO mode requires public URL")
	}
	c.emitURL(c.PublicURL)
	<-ctx.Done()
	return ctx.Err()
}

func (c *Config) runNamed(ctx context.Context) error {
	if c.Token == "" {
		return errors.New("named mode requires tunnel token in config (run set-tunnel-token)")
	}
	if c.PublicURL == "" {
		return errors.New("named mode requires publicBaseUrl")
	}
	healthURL, err := healthURLFromBase(c.PublicURL)
	if err != nil {
		return err
	}

	backoff := InitialBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		c.KillStale(c.Port)
		bin, err := c.EnsureBinary(ctx, c.DataRoot)
		if err != nil {
			c.Logger.Warn("cloudflared ensure failed", "err", err)
			if err := c.Sleep(ctx, 5*time.Second); err != nil {
				return err
			}
			continue
		}

		procCtx, cancel := context.WithCancel(ctx)
		cmd := c.ExecCommand(procCtx, bin, "tunnel", "--no-autoupdate", "run")
		cmd.Env = append(os.Environ(), "TUNNEL_TOKEN="+c.Token)
		if err := cmd.Start(); err != nil {
			cancel()
			c.Logger.Warn("cloudflared start failed", "err", err)
			if err := c.Sleep(ctx, backoff); err != nil {
				return err
			}
			backoff = NextBackoff(backoff)
			continue
		}

		waitDone := make(chan error, 1)
		go func() { waitDone <- cmd.Wait() }()

		healthy := c.waitHealthy(ctx, procCtx, healthURL, waitDone, cancel)
		if healthy {
			c.emitURL(c.PublicURL)
			backoff = InitialBackoff
			if c.superviseUntilUnhealthy(ctx, healthURL, waitDone, cancel) {
				cancel()
				<-waitDone
				continue
			}
			cancel()
			<-waitDone
			return ctx.Err()
		}

		cancel()
		<-waitDone
		c.KillStale(c.Port)
		if err := c.Sleep(ctx, backoff); err != nil {
			return err
		}
		backoff = NextBackoff(backoff)
	}
}

func (c *Config) waitHealthy(ctx context.Context, procCtx context.Context, healthURL string, waitDone <-chan error, cancel context.CancelFunc) bool {
	deadline := time.Now().Add(URLWaitTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case err := <-waitDone:
			c.Logger.Warn("cloudflared exited before healthy", "err", err)
			return false
		case <-ticker.C:
			ok, _ := c.HealthCheck(procCtx, healthURL)
			if ok {
				return true
			}
		}
	}
	return false
}

func (c *Config) superviseUntilUnhealthy(ctx context.Context, healthURL string, waitDone <-chan error, cancel context.CancelFunc) bool {
	failures := 0
	ticker := time.NewTicker(HealthInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case err := <-waitDone:
			c.Logger.Warn("cloudflared exited", "err", err)
			return true
		case <-ticker.C:
			ok, _ := c.HealthCheck(ctx, healthURL)
			if ok {
				failures = 0
				continue
			}
			failures++
			if failures >= HealthMaxFails {
				c.Logger.Warn("tunnel health failed; restarting")
				cancel()
				return true
			}
		}
	}
}

func (c *Config) runQuick(ctx context.Context) error {
	backoff := InitialBackoff
	var lastURL string

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.KillStale(c.Port)
		bin, err := c.EnsureBinary(ctx, c.DataRoot)
		if err != nil {
			if err := c.Sleep(ctx, 5*time.Second); err != nil {
				return err
			}
			continue
		}

		procCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		origin := fmt.Sprintf("http://127.0.0.1:%d", c.Port)
		cmd := c.ExecCommand(procCtx, bin, "tunnel", "--no-autoupdate", "--protocol", "quic", "--url", origin)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			cancel()
			if err := c.Sleep(ctx, backoff); err != nil {
				return err
			}
			backoff = NextBackoff(backoff)
			continue
		}

		urlCh := make(chan string, 1)
		var wg sync.WaitGroup
		scan := func(r io.Reader) {
			defer wg.Done()
			buf := make([]byte, 4096)
			acc := strings.Builder{}
			for {
				n, rerr := r.Read(buf)
				if n > 0 {
					acc.Write(buf[:n])
					if u := ParseTryCloudflareURL(acc.String()); u != "" {
						select {
						case urlCh <- u:
						default:
						}
					}
				}
				if rerr != nil {
					return
				}
			}
		}
		wg.Add(2)
		go scan(stdout)
		go scan(stderr)

		waitDone := make(chan error, 1)
		go func() {
			wg.Wait()
			waitDone <- cmd.Wait()
		}()

		var publicURL string
		urlTimer := time.NewTimer(URLWaitTimeout)
		select {
		case publicURL = <-urlCh:
		case <-urlTimer.C:
		case <-ctx.Done():
			urlTimer.Stop()
			<-waitDone
			return ctx.Err()
		}
		urlTimer.Stop()

		if publicURL == "" {
			<-waitDone
			if err := c.Sleep(ctx, backoff); err != nil {
				return err
			}
			backoff = NextBackoff(backoff)
			continue
		}

		norm := publicURL + "/v1"
		if norm != lastURL {
			c.emitURL(norm)
			lastURL = norm
			backoff = InitialBackoff
		}

		select {
		case <-ctx.Done():
			<-waitDone
			return ctx.Err()
		case err := <-waitDone:
			if err != nil {
				c.Logger.Warn("quick tunnel exited", "err", err)
			}
		}

		if err := c.Sleep(ctx, backoff); err != nil {
			return err
		}
		backoff = NextBackoff(backoff)
	}
}

func (c *Config) emitURL(u string) {
	if c.OnURLChange != nil {
		c.OnURLChange(u)
	}
	c.Logger.Info("tunnel_url_changed", "public_url", u, "tunnel_mode", string(c.Mode))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func healthURLFromBase(publicBase string) (string, error) {
	u := strings.TrimSuffix(publicBase, "/")
	u = strings.TrimSuffix(u, "/v1")
	return u + "/health", nil
}

// DefaultHealthCheck GETs healthURL; status < 530 is healthy.
func DefaultHealthCheck(ctx context.Context, healthURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode < healthOKMaxStatus, nil
}

// ParseTryCloudflareURL extracts the first trycloudflare.com URL from a log buffer.
func ParseTryCloudflareURL(line string) string {
	return tryCloudflareRE.FindString(line)
}

// NextBackoff doubles backoff up to MaxBackoff.
func NextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > MaxBackoff {
		return MaxBackoff
	}
	return next
}
