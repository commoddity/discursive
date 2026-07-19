package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

const cloudflaredReleaseBase = "https://github.com/cloudflare/cloudflared/releases/latest/download/"

// CloudflaredArtifact returns the GitHub release artifact name for goos/goarch.
func CloudflaredArtifact(goos, goarch string) (string, error) {
	switch goos {
	case "linux":
		switch goarch {
		case "amd64":
			return "cloudflared-linux-amd64", nil
		case "arm64":
			return "cloudflared-linux-arm64", nil
		}
	case "darwin":
		switch goarch {
		case "amd64":
			return "cloudflared-darwin-amd64.tgz", nil
		case "arm64":
			return "cloudflared-darwin-arm64.tgz", nil
		}
	}
	return "", fmt.Errorf("unsupported platform for cloudflared download: %s/%s", goos, goarch)
}

// CloudflaredBinPath returns the cached binary path under dataRoot.
func CloudflaredBinPath(dataRoot string) string {
	return filepath.Join(dataRoot, "bin", "cloudflared")
}

// EnsureCloudflared downloads and caches cloudflared when missing.
func EnsureCloudflared(ctx context.Context, dataRoot string) (string, error) {
	return ensureCloudflaredWith(ctx, dataRoot, runtime.GOOS, runtime.GOARCH, http.DefaultClient)
}

func ensureCloudflaredWith(ctx context.Context, dataRoot, goos, goarch string, client *http.Client) (string, error) {
	dest := CloudflaredBinPath(dataRoot)
	if st, err := os.Stat(dest); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
		return dest, nil
	}
	artifact, err := CloudflaredArtifact(goos, goarch)
	if err != nil {
		return "", err
	}
	url := cloudflaredReleaseBase + artifact
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download cloudflared: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download cloudflared: HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".tmp"
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		return "", err
	}

	if goos == "darwin" {
		if err := extractDarwinTGZ(resp.Body, tmp); err != nil {
			return "", err
		}
	} else {
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, resp.Body); err != nil {
			_ = out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
	}

	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return "", err
	}
	return dest, nil
}

func extractDarwinTGZ(r io.Reader, dest string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gzr.Close() }()
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || hdr.Name != "cloudflared" {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("cloudflared binary not found in tgz")
}
