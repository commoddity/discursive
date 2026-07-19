package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var followFlag bool
	var linesFlag int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "📄 Watch or print gateway logs (nicely formatted)",
		Long: `📄  Read gateway.log and pretty-print each JSON line.

  discursive logs            # print all log entries
  discursive logs --follow   # tail -f with jq-style formatting
  discursive logs -n 50      # show last 50 entries only

Logs are stored at {dataRoot}/gateway.log.  The --background flag on
'start' redirects all output to this file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dataRoot, err := resolveDataRoot()
			if err != nil {
				return err
			}
			logPath := filepath.Join(dataRoot, "gateway.log")

			if followFlag {
				return tailLogs(cmd.OutOrStdout(), logPath)
			}
			return printLogs(cmd.OutOrStdout(), logPath, linesFlag)
		},
	}
	cmd.Flags().BoolVarP(&followFlag, "follow", "f", false, "tail the log file (like tail -f)")
	cmd.Flags().IntVarP(&linesFlag, "lines", "n", 0, "show last N lines (0 = all)")
	return cmd
}

func printLogs(w io.Writer, logPath string, lastN int) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(w, "📄  No log file yet at %s\n", logPath)
			fmt.Fprintf(w, "💡  Start the gateway with:  discursive start --background\n")
			return nil
		}
		return err
	}
	defer f.Close()

	if lastN <= 0 {
		return formatLogLines(w, f)
	}

	// Read all lines, keep the last N.
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

func tailLogs(w io.Writer, logPath string) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			f, err = os.Create(logPath)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	defer f.Close()

	// Seek to end.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	fmt.Fprintf(w, "📄  Following %s  (Ctrl-C to stop)\n\n", logPath)

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			return fmt.Errorf("read log: %w", err)
		}
		if line == "" || line == "\n" {
			continue
		}
		writePrettyLine(w, line)
	}
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
		// Not valid JSON — print raw.
		fmt.Fprintln(w, raw)
		return
	}

	// Extract level for a color prefix (if present).
	level, _ := obj["level"].(string)

	prefix := ""
	switch level {
	case "DEBUG":
		prefix = "\033[90mDEBU\033[0m" // gray
	case "INFO":
		prefix = "\033[36mINFO\033[0m" // cyan
	case "WARN":
		prefix = "\033[33mWARN\033[0m" // yellow
	case "ERROR":
		prefix = "\033[31mERRO\033[0m" // red
	default:
		prefix = "     "
	}

	// Pretty-print with indent, prefixed by level.
	pretty, err := json.MarshalIndent(obj, "  ", "  ")
	if err != nil {
		fmt.Fprintln(w, raw)
		return
	}
	fmt.Fprintf(w, "%s  %s\n", prefix, string(pretty))
}
