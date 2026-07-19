package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCloudflaredBinPath(t *testing.T) {
	got := CloudflaredBinPath("/tmp/foo")
	if got != filepath.Join("/tmp/foo", "bin", "cloudflared") {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDarwinTGZ(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	content := []byte("#!/bin/sh\necho fake-cloudflared\n")
	err := tw.WriteHeader(&tar.Header{
		Name:     "cloudflared",
		Size:     int64(len(content)),
		Mode:     0o755,
		Typeflag: tar.TypeReg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "cloudflared")
	if err := extractDarwinTGZ(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("got %q", string(got))
	}

	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatal("binary should be executable")
	}
}

func TestExtractDarwinTGZ_SkipsNonCloudflared(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := tw.WriteHeader(&tar.Header{
		Name:     "README",
		Size:     5,
		Mode:     0o644,
		Typeflag: tar.TypeReg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "cloudflared")
	err = extractDarwinTGZ(bytes.NewReader(buf.Bytes()), dest)
	if err == nil {
		t.Fatal("expected error for missing cloudflared in tgz")
	}
}

func TestExtractDarwinTGZ_NotGzip(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cloudflared")
	err := extractDarwinTGZ(bytes.NewReader([]byte("not a gzip")), dest)
	if err == nil {
		t.Fatal("expected gzip error")
	}
}

func TestDefaultHealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	ok, err := DefaultHealthCheck(t.Context(), srv.URL+"/health")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected healthy")
	}
}

func TestDefaultHealthCheck_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// healthOKMaxStatus = 530; status >= 530 is unhealthy.
		w.WriteHeader(530)
	}))
	t.Cleanup(srv.Close)

	ok, err := DefaultHealthCheck(t.Context(), srv.URL+"/health")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected unhealthy for 530")
	}
}

func TestDefaultHealthCheck_Refused(t *testing.T) {
	_, err := DefaultHealthCheck(t.Context(), "http://127.0.0.1:1/health")
	if err == nil {
		t.Fatal("expected error for refused connection")
	}
}

func TestEnsureCloudflared_AlreadyCached(t *testing.T) {
	dataRoot := t.TempDir()
	binDir := filepath.Join(dataRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(binDir, "cloudflared")
	if err := os.WriteFile(dest, []byte("fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ensureCloudflaredWith(t.Context(), dataRoot, "darwin", "arm64", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("got %q want %q", got, dest)
	}
}

func TestEnsureCloudflared_UnsupportedPlatform(t *testing.T) {
	_, err := ensureCloudflaredWith(t.Context(), t.TempDir(), "freebsd", "amd64", nil)
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
}
