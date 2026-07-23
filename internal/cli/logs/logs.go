package logs

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/commoddity/discursive/internal/cli/util"
)

// NewCmd returns the logs subcommand.
func NewCmd(portable func() bool) *cobra.Command {
	var followFlag bool
	var linesFlag int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "📄 Watch or print gateway logs (nicely formatted)",
		Long: `📄  Read gateway.log and pretty-print each JSON line.

  discursive logs            # print all log entries
  discursive logs --follow   # tail -f with jq-style formatting
  discursive logs -n 50      # show last 50 entries only

Logs are stored at {dataRoot}/gateway.log (rotating, max ~2 MB per file,
keeps 2 rotated backups).  The --background flag on 'start' redirects
all output to this file.`,

		RunE: func(cmd *cobra.Command, args []string) error {
			dataRoot, err := util.ResolveDataRoot(portable())
			if err != nil {
				return err
			}
			logPath := filepath.Join(dataRoot, "gateway.log")

			if followFlag {
				return followLogs(cmd.OutOrStdout(), logPath)
			}
			return printLogs(cmd.OutOrStdout(), logPath, linesFlag)
		},
	}
	cmd.Flags().BoolVarP(&followFlag, "follow", "f", false, "tail the log file (like tail -f)")
	cmd.Flags().IntVarP(&linesFlag, "lines", "n", 0, "show last N lines (0 = all)")
	return cmd
}

// followLogs tails the log file using fsnotify for reliable real-time updates.
// It handles file rotation: when the log file is rotated (renamed to .1 etc.),
// it drains the old file and switches to the new gateway.log.
func followLogs(w io.Writer, logPath string) error {
	dir := filepath.Dir(logPath)
	base := filepath.Base(logPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create file watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(dir); err != nil {
		return fmt.Errorf("watch directory: %w", err)
	}

	_, _ = fmt.Fprintf(w, "📄  Following %s  (Ctrl-C to stop)\n\n", logPath)

	// Open current log file and seek to end.
	f, err := openAndSeekEnd(logPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	reader := bufio.NewReader(f)

	nameMatches := func(ev fsnotify.Event) bool {
		return strings.HasPrefix(filepath.Base(ev.Name), base)
	}

	// readAndPrint reads all available lines from reader and writes them.
	readAndPrint := func() error {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return fmt.Errorf("read log: %w", err)
			}
			if line != "" && line != "\n" {
				writePrettyLine(w, line)
			}
		}
	}

	// Initial drain of any content already in the file.
	if err := readAndPrint(); err != nil {
		return err
	}

	for {
		select {
		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !nameMatches(ev) {
				continue
			}
			switch {
			case ev.Has(fsnotify.Write):
				// Content was written — drain it.
				if err := readAndPrint(); err != nil {
					return err
				}
			case ev.Has(fsnotify.Create):
				// New log file created (e.g., after rotation or fresh start).
				_ = f.Close()
				f, err = openAndSeekEnd(logPath)
				if err != nil {
					return err
				}
				reader = bufio.NewReader(f)
			case ev.Has(fsnotify.Rename), ev.Has(fsnotify.Remove):
				// The current log file was rotated (renamed to .1) or removed.
				// Drain whatever is left in the old file handle, then close and
				// wait for the new file to appear.
				_ = readAndPrint()
				_ = f.Close()
				// Wait for the new gateway.log to be created.
				timeout := time.After(5 * time.Second)
			waitCreate:
				for {
					select {
					case ev2, ok := <-watcher.Events:
						if !ok {
							return nil
						}
						if nameMatches(ev2) && ev2.Has(fsnotify.Create) {
							f, err = openAndSeekEnd(logPath)
							if err != nil {
								return err
							}
							reader = bufio.NewReader(f)
							break waitCreate
						}
					case err2, ok := <-watcher.Errors:
						if !ok {
							return nil
						}
						return fmt.Errorf("watch error: %w", err2)
					case <-timeout:
						// File didn't reappear — try opening anyway.
						if nf, err := os.Open(logPath); err == nil {
							f = nf
							reader = bufio.NewReader(f)
						}
						break waitCreate
					}
				}
			}
		case err2, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			if err2 != nil {
				// Log errors but continue — don't fail on transient issues.
				_, _ = fmt.Fprintf(w, "⚠️  watch error: %v\n", err2)
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

// openAndSeekEnd opens a file and seeks to the end for tailing.
func openAndSeekEnd(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create an empty file so the watcher can pick it up.
			f, err = os.Create(path)
			if err != nil {
				return nil, err
			}
			return f, nil
		}
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func printLogs(w io.Writer, logPath string, lastN int) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			_, _ = fmt.Fprintf(w, "📄  No log file yet at %s\n", logPath)
			_, _ = fmt.Fprintf(w, "💡  Start the gateway with:  discursive start --background\n")
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	if lastN <= 0 {
		return formatLogLines(w, f)
	}

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > lastN {
			lines = lines[1:]
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read log: %w", err)
	}
	for _, line := range lines {
		writePrettyLine(w, line)
	}
	return nil
}

func formatLogLines(w io.Writer, r io.Reader) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if sc.Text() == "" {
			continue
		}
		writePrettyLine(w, sc.Text())
	}
	return sc.Err()
}

func writePrettyLine(w io.Writer, raw string) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		_, _ = fmt.Fprintln(w, raw)
		return
	}

	level, _ := obj["level"].(string)

	prefix := ""
	switch level {
	case "DEBUG":
		prefix = "\033[90mDEBU\033[0m"
	case "INFO":
		prefix = "\033[36mINFO\033[0m"
	case "WARN":
		prefix = "\033[33mWARN\033[0m"
	case "ERROR":
		prefix = "\033[31mERRO\033[0m"
	default:
		prefix = "     "
	}

	pretty, err := json.MarshalIndent(obj, "  ", "  ")
	if err != nil {
		_, _ = fmt.Fprintln(w, raw)
		return
	}
	_, _ = fmt.Fprintf(w, "%s  %s\n", prefix, string(pretty))
}
