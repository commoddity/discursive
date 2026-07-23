// Package start implements the `discursive start` command.
//
// logwriter.go — rotating log file writer with size-based rotation.
// Max file size: 2 MB. Keeps 1 current + up to 2 rotated files (~4 MB total).
//
// Docker uses a similar approach but with a JSON-file driver; we adopt the
// same principles in Go native code.
package start

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// maxLogSize is the maximum size in bytes before rotation (2 MB).
	maxLogSize = 2 * 1024 * 1024
	// maxRotatedFiles is the number of rotated backup files to keep.
	maxRotatedFiles = 2
	// rotationCheckInterval is how often to check size without a write.
	rotationCheckInterval = 10 * time.Second
)

// rotatingWriter is an io.Writer that writes to a log file and rotates it
// when it exceeds maxLogSize. Rotates to .1, .2, etc. It also reopens the
// file if it detects an external rename (e.g., from logrotate or manual cleanup).
type rotatingWriter struct {
	mu     sync.Mutex
	dir    string
	base   string
	file   *os.File
	size   int64
	stopCh chan struct{}
}

// newRotatingWriter creates and opens the current log file.
func newRotatingWriter(logPath string) (*rotatingWriter, error) {
	dir := filepath.Dir(logPath)
	base := filepath.Base(logPath)

	w := &rotatingWriter{
		dir:    dir,
		base:   base,
		stopCh: make(chan struct{}),
	}

	if err := w.open(); err != nil {
		return nil, err
	}

	// Background goroutine to detect external file removal (e.g., logrotate)
	// and reopen the file when needed. Also trims old rotated files.
	go w.maintenanceLoop()

	return w, nil
}

func (w *rotatingWriter) open() error {
	path := filepath.Join(w.dir, w.base)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file %s: %w", path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log file %s: %w", path, err)
	}
	// fsync the directory so renames are durable.
	_ = w.syncDir()

	if w.file != nil {
		_ = w.file.Close()
	}
	w.file = f
	w.size = fi.Size()
	return nil
}

func (w *rotatingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}

	path := filepath.Join(w.dir, w.base)

	// Shift existing rotated files: .2 → remove, .1 → .2
	rotatedFiles := w.listRotated()
	// Remove older files beyond maxRotatedFiles
	sort.Sort(sort.Reverse(sort.StringSlice(rotatedFiles)))
	for _, rf := range rotatedFiles {
		suffix := strings.TrimPrefix(rf, w.base+".")
		num := 0
		if _, err := fmt.Sscanf(suffix, "%d", &num); err != nil {
			continue
		}
		if num >= maxRotatedFiles {
			_ = os.Remove(filepath.Join(w.dir, rf))
		} else {
			newName := fmt.Sprintf("%s.%d", w.base, num+1)
			_ = os.Rename(filepath.Join(w.dir, rf), filepath.Join(w.dir, newName))
		}
	}

	// Rotate current file to .1
	_ = os.Rename(path, filepath.Join(w.dir, fmt.Sprintf("%s.1", w.base)))

	_ = w.syncDir()

	// Reopen
	return w.open()
}

func (w *rotatingWriter) listRotated() []string {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return nil
	}
	var rotated []string
	prefix := w.base + "."
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) && !e.IsDir() {
			rotated = append(rotated, e.Name())
		}
	}
	return rotated
}

func (w *rotatingWriter) syncDir() error {
	dir, err := os.Open(w.dir)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}

	if w.size >= maxLogSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = w.file.Write(p)
	w.size += int64(n)

	return n, err
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	close(w.stopCh)
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// maintenanceLoop periodically checks if the current file was removed
// (e.g. by logrotate) and reopens it. Also trims excess rotated files.
func (w *rotatingWriter) maintenanceLoop() {
	ticker := time.NewTicker(rotationCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			path := filepath.Join(w.dir, w.base)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				_ = w.open()
			}
			// Trim excess rotated files
			rotated := w.listRotated()
			if len(rotated) > maxRotatedFiles {
				sort.Strings(rotated)
				for _, rf := range rotated[:len(rotated)-maxRotatedFiles] {
					_ = os.Remove(filepath.Join(w.dir, rf))
				}
			}
			w.mu.Unlock()
		}
	}
}
