// Package doctor runs gateway health checks for the `kimi-cursor doctor` CLI.
//
// Contract: slog JSON only; never logs upstream secrets; CGO-free.
package doctor

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/crypto"
)

// Check is one health check result.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Report aggregates all Check results.
type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// Overridable for tests.
var (
	// httpGET makes an HTTP GET request with a timeout.
	httpGET func(url string, timeout time.Duration) (int, error)

	// lookPath looks up a binary in PATH.
	lookPath func(name string) (string, error)

	// fileStat reports file info.
	fileStat func(name string) (os.FileInfo, error)

	// mkdirAll creates a directory tree.
	mkdirAll func(path string, perm os.FileMode) error

	// createTemp creates a temp file in dir.
	createTemp func(dir, pattern string) (*os.File, error)
)

func init() {
	httpGET = defaultHTTPGet
	lookPath = exec.LookPath
	fileStat = os.Stat
	mkdirAll = os.MkdirAll
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(dir, pattern)
	}
}

func defaultHTTPGet(url string, timeout time.Duration) (int, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// RunAll executes every health check against settings and returns a Report.
func RunAll(settings config.AppSettings, dataRoot string) Report {
	var checks []Check

	// 1. moonshot_key_present
	checks = append(checks, Check{
		Name: "moonshot_key_present",
		OK:   settings.HasMoonshotKey(),
		Detail: func() string {
			if settings.HasMoonshotKey() {
				return ""
			}
			return "Moonshot/Kimi API key is not saved (run set --moonshot-key)"
		}(),
	})

	// 2. deepseek_key_present
	checks = append(checks, Check{
		Name: "deepseek_key_present",
		OK:   settings.HasDeepSeekKey(),
		Detail: func() string {
			if settings.HasDeepSeekKey() {
				return ""
			}
			return "DeepSeek API key is not saved (run set --deepseek-key)"
		}(),
	})

	// 3. thaura_key_present
	checks = append(checks, Check{
		Name: "thaura_key_present",
		OK:   settings.HasThauraKey(),
		Detail: func() string {
			if settings.HasThauraKey() {
				return ""
			}
			return "optional: Thaura AI API key not saved (run set --thaura-key to enable the gpt-5-nano alias)"
		}(),
	})

	// 4. gateway_key_valid
	gkOk := crypto.IsOpenAIStyleGatewayKey(settings.GatewayKey)
	checks = append(checks, Check{
		Name: "gateway_key_valid",
		OK:   gkOk,
		Detail: func() string {
			if gkOk {
				return ""
			}
			return fmt.Sprintf("gateway key is malformed: %s", crypto.MaskSecret(settings.GatewayKey))
		}(),
	})

	// 4. port_available
	portOk, portDetail := checkPortAvailable(settings.LocalPort)
	checks = append(checks, Check{Name: "port_available", OK: portOk, Detail: portDetail})

	// 5. local_health
	lhOk, lhDetail := checkHTTPHealth(fmt.Sprintf("http://127.0.0.1:%d/health", settings.LocalPort), 2*time.Second)
	checks = append(checks, Check{Name: "local_health", OK: lhOk, Detail: lhDetail})

	// 6. tunnel_mode_valid
	tunOk, tunDetail := checkTunnelMode(settings)
	checks = append(checks, Check{Name: "tunnel_mode_valid", OK: tunOk, Detail: tunDetail})

	// 7. public_health
	if settings.PublicBaseURL != "" {
		healthURL, err := config.HealthURLFromPublicBase(settings.PublicBaseURL)
		if err != nil {
			checks = append(checks, Check{
				Name:   "public_health",
				OK:     false,
				Detail: fmt.Sprintf("bad public URL: %v", err),
			})
		} else {
			phOK, phDetail := checkHTTPHealth(healthURL, 5*time.Second)
			checks = append(checks, Check{Name: "public_health", OK: phOK, Detail: phDetail})
		}
	} else {
		checks = append(checks, Check{
			Name:   "public_health",
			OK:     true,
			Detail: "skipped: no public URL",
		})
	}

	// 8. cloudflared_present
	cfOk, cfDetail := checkCloudflared(dataRoot)
	checks = append(checks, Check{Name: "cloudflared_present", OK: cfOk, Detail: cfDetail})

	// 9. logs_dir_writable
	logsOk, logsDetail := checkLogsDirWritable(dataRoot)
	checks = append(checks, Check{Name: "logs_dir_writable", OK: logsOk, Detail: logsDetail})

	allOK := true
	for _, c := range checks {
		if !c.OK {
			allOK = false
			break
		}
	}
	return Report{OK: allOK, Checks: checks}
}

func checkPortAvailable(port uint16) (bool, string) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return false, fmt.Sprintf("port %d is in use", port)
	}
	_ = l.Close()
	return true, ""
}

func checkHTTPHealth(url string, timeout time.Duration) (bool, string) {
	code, err := httpGET(url, timeout)
	if err != nil {
		return false, fmt.Sprintf("HTTP GET %s: %v", url, err)
	}
	if code == http.StatusOK {
		return true, fmt.Sprintf("HTTP %d", code)
	}
	return false, fmt.Sprintf("HTTP %d (expected 200)", code)
}

func checkTunnelMode(settings config.AppSettings) (bool, string) {
	if err := config.ValidateTunnelSettings(settings); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func checkCloudflared(dataRoot string) (bool, string) {
	// Check in dataRoot first.
	localPath := filepath.Join(dataRoot, "cloudflared")
	if info, err := fileStat(localPath); err == nil && !info.IsDir() {
		return true, fmt.Sprintf("found at %s", localPath)
	}
	if path, err := lookPath("cloudflared"); err == nil && path != "" {
		return true, fmt.Sprintf("found at %s", path)
	}
	return false, "cloudflared not found (install cloudflared or run set --tunnel-token)"
}

func checkLogsDirWritable(dataRoot string) (bool, string) {
	logsDir := filepath.Join(dataRoot, "logs")
	if err := mkdirAll(logsDir, 0o755); err != nil {
		return false, fmt.Sprintf("cannot create logs dir: %v", err)
	}
	tmp, err := createTemp(logsDir, "doctor-*")
	if err != nil {
		return false, fmt.Sprintf("cannot write to logs dir: %v", err)
	}
	_ = tmp.Close()
	_ = os.Remove(tmp.Name())
	return true, ""
}
