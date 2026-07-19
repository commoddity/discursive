package cli

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"discursive/internal/config"
	"discursive/internal/crypto"
)

func newSetMoonshotKeyCmd() *cobra.Command {
	var keyFlag string
	cmd := &cobra.Command{
		Use:   "set-moonshot-key",
		Short: "🌙 Save Moonshot / Kimi API key (encrypted at rest)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setUpstreamKey(cmd, "moonshot", keyFlag)
		},
	}
	cmd.Flags().StringVar(&keyFlag, "key", "", "API key (omit for interactive prompt; or pipe via stdin)")
	return cmd
}

func newSetDeepSeekKeyCmd() *cobra.Command {
	var keyFlag string
	cmd := &cobra.Command{
		Use:   "set-deepseek-key",
		Short: "🌊 Save DeepSeek API key (encrypted at rest)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return setUpstreamKey(cmd, "deepseek", keyFlag)
		},
	}
	cmd.Flags().StringVar(&keyFlag, "key", "", "API key (omit for interactive prompt; or pipe via stdin)")
	return cmd
}

func newRotateGatewayKeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate-gateway-key",
		Short: "🔄 Generate a new gateway API key (sk-…)",
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogger()
			dataRoot, err := resolveDataRoot()
			if err != nil {
				return err
			}
			s, err := config.Load(dataRoot)
			if err != nil {
				return err
			}
			if err := s.RotateGatewayKey(); err != nil {
				return err
			}
			if err := config.Save(dataRoot, s); err != nil {
				return err
			}
			slog.Info("rotated gateway key",
				"gateway_key_masked", crypto.MaskSecret(s.GatewayKey),
				"has_moonshot_key", s.HasMoonshotKey(),
				"has_deepseek_key", s.HasDeepSeekKey(),
			)
			return nil
		},
	}
}

func setUpstreamKey(cmd *cobra.Command, provider, keyFlag string) error {
	setupLogger()
	plain, err := readUpstreamKeyPlain(cmd, provider, keyFlag)
	if err != nil {
		return err
	}
	if plain == "" {
		return fmt.Errorf("empty API key")
	}

	dataRoot, err := resolveDataRoot()
	if err != nil {
		return err
	}
	s, err := config.Load(dataRoot)
	if err != nil {
		return err
	}

	switch provider {
	case "moonshot":
		if err := s.SetMoonshotKey(dataRoot, plain); err != nil {
			return err
		}
	case "deepseek":
		if err := s.SetDeepSeekKey(dataRoot, plain); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}

	if err := config.Save(dataRoot, s); err != nil {
		return err
	}

	slog.Info("saved upstream key",
		"provider", provider,
		"key_masked", crypto.MaskSecret(plain),
		"has_moonshot_key", s.HasMoonshotKey(),
		"has_deepseek_key", s.HasDeepSeekKey(),
	)
	return nil
}

// readUpstreamKeyPlain returns the key from --key, an interactive TTY prompt, or stdin (pipe).
func readUpstreamKeyPlain(cmd *cobra.Command, provider, keyFlag string) (string, error) {
	var label string
	switch provider {
	case "moonshot":
		label = "Moonshot"
	case "deepseek":
		label = "DeepSeek"
	default:
		label = provider
	}
	return readSecretPlain(cmd, label, keyFlag)
}

// readSecretPlain reads a secret from flag, TTY (hidden), or stdin.
func readSecretPlain(cmd *cobra.Command, label, keyFlag string) (string, error) {
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

// readLinePlain reads a non-secret value from flag, TTY (echoed), or stdin.
func readLinePlain(cmd *cobra.Command, label, flagValue string) (string, error) {
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

// readLineFromCmd reads one line from cmd's stdin, preserving a shared bufio.Reader
// so multi-prompt commands (init) can consume successive lines from a pipe.
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

func resolveDataRoot() (string, error) {
	opts, err := config.DefaultResolveOpts(portableFlag)
	if err != nil {
		return "", err
	}
	return config.EnsureDataRoot(opts)
}
