package util

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// StdinIsInteractive reports whether cmd's stdin is a TTY.
func StdinIsInteractive(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// ReadSecretPlain reads a secret from flag, TTY (hidden), or stdin.
func ReadSecretPlain(cmd *cobra.Command, label, keyFlag string) (string, error) {
	if plain := strings.TrimSpace(keyFlag); plain != "" {
		return plain, nil
	}

	stdin := cmd.InOrStdin()
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		// Prompts go to stderr so `discursive start | jq` keeps stdout JSON-clean.
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s (input hidden): ", label); err != nil {
			return "", err
		}
		b, err := term.ReadPassword(int(f.Fd()))
		if _, werr := fmt.Fprintln(cmd.ErrOrStderr()); werr != nil {
			return "", werr
		}
		if err != nil {
			return "", fmt.Errorf("read secret: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}

	return readLineFromCmd(cmd)
}

// ReadLinePlain reads a non-secret value from flag, TTY (echoed), or stdin.
func ReadLinePlain(cmd *cobra.Command, label, flagValue string) (string, error) {
	if plain := strings.TrimSpace(flagValue); plain != "" {
		return plain, nil
	}

	stdin := cmd.InOrStdin()
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "%s: ", label); err != nil {
			return "", err
		}
		// Read directly from the TTY fd so we do not wrap stdin in bufio
		// (wrapping would break later term.IsTerminal checks for secrets).
		sc := bufio.NewScanner(f)
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return "", fmt.Errorf("read input: %w", err)
			}
			return "", nil
		}
		return strings.TrimSpace(sc.Text()), nil
	}

	return readLineFromCmd(cmd)
}

func readLineFromCmd(cmd *cobra.Command) (string, error) {
	br := ensureBufioReader(cmd)
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read input: %w", err)
	}
	if err == io.EOF && line == "" {
		return "", nil
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), nil
}

func ensureBufioReader(cmd *cobra.Command) *bufio.Reader {
	in := cmd.InOrStdin()
	if br, ok := in.(*bufio.Reader); ok {
		return br
	}
	br := bufio.NewReader(in)
	cmd.SetIn(br)
	return br
}
