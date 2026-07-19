package doctor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"discursive/internal/config"
	"discursive/internal/crypto"
)

func TestRunAll_MoonshotKeyMissing(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	// Stub HTTP + filesystem to avoid real I/O.
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "moonshot_key_present")
	if check.OK {
		t.Fatal("expected moonshot_key_present to fail without key")
	}
}

func TestRunAll_DeepSeekKeyMissing(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "deepseek_key_present")
	if check.OK {
		t.Fatal("expected deepseek_key_present to fail without key")
	}
}

func TestRunAll_KeysPresentPass(t *testing.T) {
	dataRoot := t.TempDir()
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMoonshotKey(dataRoot, "sk-test-moonshot-key-1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeepSeekKey(dataRoot, "sk-test-deepseek-key-1234567890"); err != nil {
		t.Fatal(err)
	}
	_ = config.Save(dataRoot, s) // ensure encrypted fields written

	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	for _, tc := range []struct{ name string }{
		{"moonshot_key_present"},
		{"deepseek_key_present"},
		{"gateway_key_valid"},
	} {
		check := findCheck(report, tc.name)
		if !check.OK {
			t.Fatalf("expected %s to pass, got %q", tc.name, check.Detail)
		}
	}
}

func TestRunAll_GatewayKeyInvalid(t *testing.T) {
	s := config.DefaultSettings()
	s.GatewayKey = "bad-key"
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "gateway_key_valid")
	if check.OK {
		t.Fatal("expected gateway_key_valid to fail with bad key")
	}
}

func TestRunAll_PortAvailable(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.LocalPort = 55590 // unlikely to be in use
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return http.StatusOK, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "/usr/local/bin/cloudflared", nil }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "port_available")
	if !check.OK {
		t.Fatalf("expected port_available to pass, got %q", check.Detail)
	}
}

func TestRunAll_LocalHealthPasses(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.LocalPort = 55556
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return http.StatusOK, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "/usr/local/bin/cloudflared", nil }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "local_health")
	if !check.OK {
		t.Fatalf("expected local_health to pass, got %q", check.Detail)
	}
}

func TestRunAll_LocalHealthFails(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.LocalPort = 55557
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "local_health")
	if check.OK {
		t.Fatal("expected local_health to fail with 503")
	}
}

func TestRunAll_TunnelModeValid(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.TunnelMode = config.TunnelModeQuick
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return http.StatusOK, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "/usr/local/bin/cloudflared", nil }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "tunnel_mode_valid")
	if !check.OK {
		t.Fatalf("expected tunnel_mode_valid to pass for quick mode, got %q", check.Detail)
	}
}

func TestRunAll_TunnelModeInvalidNamed(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.TunnelMode = config.TunnelModeNamed
	// No tunnel token and no public URL → invalid
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "tunnel_mode_valid")
	if check.OK {
		t.Fatal("expected tunnel_mode_valid to fail for named mode without token")
	}
}

func TestRunAll_PublicHealthSkipped(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	s.PublicBaseURL = "" // no public URL → skipped
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 200, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "/usr/local/bin/cloudflared", nil }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "public_health")
	if !check.OK {
		t.Fatalf("expected public_health to pass (skipped), got %q", check.Detail)
	}
}

func TestRunAll_CloudflaredFound(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "/usr/local/bin/cloudflared", nil }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "cloudflared_present")
	if !check.OK {
		t.Fatalf("expected cloudflared_present to pass, got %q", check.Detail)
	}
}

func TestRunAll_CloudflaredMissing(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, t.TempDir())
	check := findCheck(report, "cloudflared_present")
	if check.OK {
		t.Fatal("expected cloudflared_present to fail")
	}
}

func TestRunAll_LogsDirWritable(t *testing.T) {
	s := config.DefaultSettings()
	if err := s.EnsureGatewayKey(); err != nil {
		t.Fatal(err)
	}
	dataRoot := t.TempDir()
	origHTTP := httpGET
	origStat := fileStat
	origLookPath := lookPath
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		httpGET = origHTTP
		fileStat = origStat
		lookPath = origLookPath
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	httpGET = func(url string, timeout time.Duration) (int, error) { return 503, nil }
	fileStat = func(name string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	lookPath = func(name string) (string, error) { return "", os.ErrNotExist }
	mkdirAll = func(path string, perm os.FileMode) error { return nil }
	createTemp = func(dir, pattern string) (*os.File, error) {
		return os.CreateTemp(t.TempDir(), pattern)
	}

	report := RunAll(s, dataRoot)
	check := findCheck(report, "logs_dir_writable")
	if !check.OK {
		t.Fatalf("expected logs_dir_writable to pass, got %q", check.Detail)
	}
}

func TestCheck_Fields(t *testing.T) {
	c := Check{Name: "test", OK: true, Detail: "all good"}
	if c.Name != "test" || !c.OK || c.Detail != "all good" {
		t.Fatal("check fields mismatch")
	}
}

func TestReport_AllOk(t *testing.T) {
	r := Report{OK: true, Checks: []Check{{Name: "a", OK: true}}}
	if !r.OK {
		t.Fatal("expected OK")
	}
}

func TestReport_NotAllOk(t *testing.T) {
	r := Report{OK: false, Checks: []Check{{Name: "a", OK: true}, {Name: "b", OK: false}}}
	if r.OK {
		t.Fatal("expected not OK")
	}
}

func TestMaskSecretNotLeaked(t *testing.T) {
	key, err := crypto.GenerateGatewayKey()
	if err != nil {
		t.Fatal(err)
	}
	masked := crypto.MaskSecret(key)
	if masked == key {
		t.Fatal("mask should not return plaintext key")
	}
}

func TestPortAvailableActuallyUnavailable(t *testing.T) {
	// Occupy a port, then check it.
	l, err := listenTCP("127.0.0.1", 55580)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	ok, detail := checkPortAvailable(55580)
	if ok {
		t.Fatalf("expected port 55580 unavailable, got %q", detail)
	}
}

func TestCheckCloudflaredLocalBinary(t *testing.T) {
	dataRoot := t.TempDir()
	localPath := filepath.Join(dataRoot, "cloudflared")
	origStat := fileStat
	t.Cleanup(func() { fileStat = origStat })
	fileStat = func(name string) (os.FileInfo, error) {
		if name == localPath {
			return &fakeFileInfo{}, nil
		}
		return nil, os.ErrNotExist
	}
	ok, detail := checkCloudflared(dataRoot)
	if !ok {
		t.Fatalf("expected cloudflared found locally, got %q", detail)
	}
}

func TestCheckLogsDirWritableFailure(t *testing.T) {
	origMkdirAll := mkdirAll
	origCreateTemp := createTemp
	t.Cleanup(func() {
		mkdirAll = origMkdirAll
		createTemp = origCreateTemp
	})
	mkdirAll = func(path string, perm os.FileMode) error {
		return os.ErrPermission
	}
	createTemp = func(dir, pattern string) (*os.File, error) {
		return nil, os.ErrPermission
	}
	ok, detail := checkLogsDirWritable(t.TempDir())
	if ok {
		t.Fatalf("expected logs_dir_writable to fail, got %q", detail)
	}
}

// --- helpers ---

func findCheck(r Report, name string) Check {
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	return Check{Name: name, OK: false, Detail: "check not found"}
}

type fakeFileInfo struct{}

func (f *fakeFileInfo) Name() string       { return "cloudflared" }
func (f *fakeFileInfo) Size() int64        { return 100 }
func (f *fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (f *fakeFileInfo) ModTime() time.Time { return time.Now() }
func (f *fakeFileInfo) IsDir() bool        { return false }
func (f *fakeFileInfo) Sys() any           { return nil }

// listenTCP is a helper to occupy a port for TestPortAvailableActuallyUnavailable.
func listenTCP(host string, port uint16) (net.Listener, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	return (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
}
