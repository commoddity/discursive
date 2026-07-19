// Package config holds app settings defaults and filesystem path resolution.
//
// Contract: CGO-free; no Cobra imports; never logs secrets.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	// AppName is the OS app-data directory name.
	AppName = "Discursive"

	// DefaultPort is the local gateway listen port.
	DefaultPort = 4001

	// DefaultRealModel is the Moonshot model id used by default.
	DefaultRealModel = "kimi-k3"

	// DefaultAliasModel is the Cursor-facing alias for DefaultRealModel.
	DefaultAliasModel = "gpt-5-high"

	portableDataDirName = "DiscursiveData"
	portableMarkerName  = "portable"
)

// ResolveOpts controls data-root resolution (injectable for tests).
type ResolveOpts struct {
	GOOS           string // default: runtime.GOOS
	Home           string // default: os.UserHomeDir
	XDGDataHome    string // Linux: $XDG_DATA_HOME
	ExeDir         string // directory containing the executable (portable)
	PortableFlag   bool   // --portable
	PortableMarker bool   // marker file present next to exe
}

// DataRoot returns the application data directory root without creating it.
//
// macOS: ~/Library/Application Support/Discursive
// Linux: $XDG_DATA_HOME/Discursive or ~/.local/share/Discursive
// Portable: <exeDir>/DiscursiveData
func DataRoot(opts ResolveOpts) (string, error) {
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.PortableFlag || opts.PortableMarker {
		if opts.ExeDir == "" {
			return "", fmt.Errorf("portable mode requires executable directory")
		}
		return filepath.Join(opts.ExeDir, portableDataDirName), nil
	}

	home := opts.Home
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
	}

	base, err := platformDataBase(opts.GOOS, home, opts.XDGDataHome)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, AppName), nil
}

// EnsureDataRoot resolves and creates the data root (and a data/ subdirectory).
func EnsureDataRoot(opts ResolveOpts) (string, error) {
	root, err := DataRoot(opts)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		return "", fmt.Errorf("create data root: %w", err)
	}
	return root, nil
}

// DefaultResolveOpts builds options from the live environment.
func DefaultResolveOpts(portableFlag bool) (ResolveOpts, error) {
	opts := ResolveOpts{
		GOOS:         runtime.GOOS,
		XDGDataHome:  os.Getenv("XDG_DATA_HOME"),
		PortableFlag: portableFlag,
	}
	exe, err := os.Executable()
	if err == nil {
		opts.ExeDir = filepath.Dir(exe)
		if _, err := os.Stat(filepath.Join(opts.ExeDir, portableMarkerName)); err == nil {
			opts.PortableMarker = true
		}
	}
	return opts, nil
}

func platformDataBase(goos, home, xdgDataHome string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support"), nil
	case "linux":
		if xdgDataHome != "" {
			return xdgDataHome, nil
		}
		return filepath.Join(home, ".local", "share"), nil
	default:
		// Unsupported for product MVP, but keep a predictable fallback for tests.
		if xdgDataHome != "" {
			return xdgDataHome, nil
		}
		return filepath.Join(home, ".local", "share"), nil
	}
}
