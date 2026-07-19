// Package wizard renders the interactive Discursive setup UI
// (banner, colors, spinners, cleared prompts).
//
// Contract: presentation only — no config/crypto/Cobra imports.
// Callers supply io.Reader/Writer and interactive mode.
package wizard

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiCyan    = "\033[36m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiMagenta = "\033[35m"
	ansiBlue    = "\033[34m"
	ansiWhite   = "\033[97m"
	ansiClearLn = "\033[2K"
	ansiUp1     = "\033[1A"
	ansiHideCur = "\033[?25l"
	ansiShowCur = "\033[?25h"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var bannerLines = []string{
	`   ██████╗ ██╗███████╗ ██████╗██╗   ██╗██████╗ ███████╗██╗██╗   ██╗███████╗`,
	`   ██╔══██╗██║██╔════╝██╔════╝██║   ██║██╔══██╗██╔════╝██║██║   ██║██╔════╝`,
	`   ██║  ██║██║███████╗██║     ██║   ██║██████╔╝███████╗██║██║   ██║█████╗  `,
	`   ██║  ██║██║╚════██║██║     ██║   ██║██╔══██╗╚════██║██║╚██╗ ██╔╝██╔══╝  `,
	`   ██████╔╝██║███████║╚██████╗╚██████╔╝██║  ██║███████║██║ ╚████╔╝ ███████╗`,
	`   ╚═════╝ ╚═╝╚══════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═══╝  ╚══════╝`,
}

// UI is the setup wizard presenter.
type UI struct {
	out         io.Writer
	in          io.Reader
	br          *bufio.Reader
	color       bool
	interactive bool
	sleep       func(time.Duration)
}

// Options configures a new UI.
type Options struct {
	Out         io.Writer
	In          io.Reader
	Interactive bool
	// Sleep overrides time.Sleep (tests).
	Sleep func(time.Duration)
}

// New builds a wizard UI. Out should be stderr so stdout stays JSON-clean.
func New(opts Options) *UI {
	out := opts.Out
	if out == nil {
		out = os.Stderr
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	color := opts.Interactive && supportsColor(out)
	return &UI{
		out:         out,
		in:          in,
		color:       color,
		interactive: opts.Interactive,
		sleep:       sleep,
	}
}

func supportsColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

func (w *UI) paint(code, s string) string {
	if !w.color {
		return s
	}
	return code + s + ansiReset
}

func (w *UI) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(w.out, format, args...)
}

func (w *UI) println(args ...any) {
	_, _ = fmt.Fprintln(w.out, args...)
}

func (w *UI) clearLines(n int) {
	if !w.interactive || n <= 0 {
		return
	}
	for i := 0; i < n; i++ {
		w.printf("%s%s\r", ansiUp1, ansiClearLn)
	}
}

func (w *UI) lineReader() *bufio.Reader {
	if w.br == nil {
		w.br = bufio.NewReader(w.in)
	}
	return w.br
}

// Intro prints the banner / plain setup header.
func (w *UI) Intro(fromStart bool) {
	if !w.interactive {
		w.println("Discursive setup — paste each value when prompted.")
		w.println("Secrets are encrypted at rest and never logged in full.")
		w.println()
		return
	}

	w.printf("%s", ansiHideCur)
	defer w.printf("%s", ansiShowCur)

	w.println()
	accent := ansiCyan
	for i, line := range bannerLines {
		if i == len(bannerLines)-1 {
			accent = ansiMagenta
		}
		w.println(w.paint(ansiBold+accent, line))
		w.sleep(28 * time.Millisecond)
	}
	w.println()
	w.println(w.paint(ansiDim, "  local gateway  ·  Moonshot Kimi  ·  DeepSeek  ·  Cloudflare"))
	w.println()

	msg := "First-time setup"
	if fromStart {
		msg = "Setup needed before start"
	}
	w.Spin(msg+" — secrets stay on this machine", 900*time.Millisecond)
	w.println()
	w.println(w.paint(ansiDim, "  Paste each value when prompted. Input is hidden for secrets."))
	w.println()
}

// Spin shows a short braille spinner (or a plain line when non-interactive).
func (w *UI) Spin(label string, d time.Duration) {
	if !w.interactive {
		w.println("  " + label)
		return
	}
	w.printf("%s", ansiHideCur)
	deadline := time.Now().Add(d)
	i := 0
	for time.Now().Before(deadline) {
		frame := spinnerFrames[i%len(spinnerFrames)]
		w.printf("\r%s  %s  %s", ansiClearLn, w.paint(ansiCyan, frame), w.paint(ansiDim, label))
		w.sleep(55 * time.Millisecond)
		i++
	}
	w.printf("\r%s  %s  %s\n", ansiClearLn, w.paint(ansiGreen, "✓"), label)
	w.printf("%s", ansiShowCur)
}

// AskSecret prompts for a hidden value (or uses flag / piped stdin).
func (w *UI) AskSecret(step, total int, emoji, title, hint, flag string) (string, error) {
	if plain := strings.TrimSpace(flag); plain != "" {
		w.stepDone(step, total, emoji, title, "from flag")
		return plain, nil
	}
	if !w.interactive {
		plain, err := w.readLine()
		if err != nil {
			return "", err
		}
		if plain == "" {
			return "", fmt.Errorf("%s is required", title)
		}
		w.stepDone(step, total, emoji, title, "saved · encrypted")
		return plain, nil
	}

	lines := w.printPrompt(step, total, emoji, title, hint, true)
	f, ok := w.in.(*os.File)
	if !ok {
		return "", fmt.Errorf("interactive secret prompt requires a TTY")
	}
	b, err := term.ReadPassword(int(f.Fd()))
	w.println()
	if err != nil {
		return "", fmt.Errorf("read secret: %w", err)
	}
	plain := strings.TrimSpace(string(b))
	w.clearLines(lines + 1)
	if plain == "" {
		return "", fmt.Errorf("%s is required", title)
	}
	w.stepDone(step, total, emoji, title, "saved · encrypted")
	return plain, nil
}

// AskLine prompts for a visible value (or uses flag / piped stdin).
func (w *UI) AskLine(step, total int, emoji, title, hint, flag string) (string, error) {
	if plain := strings.TrimSpace(flag); plain != "" {
		w.stepDone(step, total, emoji, title, "from flag")
		return plain, nil
	}
	if !w.interactive {
		plain, err := w.readLine()
		if err != nil {
			return "", err
		}
		if plain == "" {
			return "", fmt.Errorf("%s is required", title)
		}
		w.stepDone(step, total, emoji, title, "saved")
		return plain, nil
	}

	lines := w.printPrompt(step, total, emoji, title, hint, false)
	f, ok := w.in.(*os.File)
	if !ok {
		return "", fmt.Errorf("interactive prompt requires a TTY")
	}
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}
		return "", fmt.Errorf("%s is required", title)
	}
	plain := strings.TrimSpace(sc.Text())
	w.clearLines(lines + 1)
	if plain == "" {
		return "", fmt.Errorf("%s is required", title)
	}
	w.stepDone(step, total, emoji, title, "saved")
	return plain, nil
}

func (w *UI) readLine() (string, error) {
	line, err := w.lineReader().ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read input: %w", err)
	}
	if err == io.EOF && line == "" {
		return "", nil
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), nil
}

func (w *UI) printPrompt(step, total int, emoji, title, hint string, secret bool) int {
	badge := fmt.Sprintf(" %d/%d ", step, total)
	w.printf("  %s %s  %s\n",
		w.paint(ansiBold+ansiBlue, badge),
		emoji,
		w.paint(ansiBold+ansiWhite, title),
	)
	lines := 1
	if hint != "" {
		w.printf("     %s\n", w.paint(ansiDim, hint))
		lines++
	}
	suffix := ":"
	if secret {
		suffix = " " + w.paint(ansiDim, "(hidden)") + ":"
	}
	w.printf("  %s%s ", w.paint(ansiCyan, "›"), suffix)
	lines++
	return lines
}

func (w *UI) stepDone(step, total int, emoji, title, detail string) {
	badge := fmt.Sprintf("%d/%d", step, total)
	w.printf("  %s %s  %s  %s\n",
		w.paint(ansiGreen, "✓"),
		w.paint(ansiDim, badge),
		emoji+" "+w.paint(ansiBold, title),
		w.paint(ansiDim, "· "+detail),
	)
}

// Finish prints the post-setup summary card with numbered Cursor setup steps.
func (w *UI) Finish(fromStart bool, publicURL string, port uint16, gatewayKey string) {
	if !w.interactive {
		w.println()
		w.printf("Ready. Ensure Cloudflare routes %s → http://127.0.0.1:%d\n",
			strings.TrimSuffix(publicURL, "/v1"), port)
		if fromStart {
			w.println("Starting gateway…")
		} else {
			w.println("Then: discursive start | jq .")
		}
		return
	}

	w.println()
	w.Spin("Locking secrets & writing config", 650*time.Millisecond)
	w.println()

	bar := w.paint(ansiGreen, "══════════════════════════════════════════════════════════")

	w.println("  " + bar)
	w.printf("  %s  %s\n",
		w.paint(ansiGreen+ansiBold, "✦"),
		w.paint(ansiBold+ansiWhite, "You're all set — Discursive is ready"))
	w.println("  " + bar)
	w.println()

	// ── Cursor setup instructions ────────────────────────────
	w.println("  " + w.paint(ansiBold+ansiWhite, "📋  Configure Cursor"))
	w.println("  " + w.paint(ansiDim, "   Settings  →  Models  →  OpenAI API Key & Override Base URL"))
	w.println()

	// 1 API key
	w.printf("  %s  %s\n",
		w.paint(ansiBold+ansiWhite, "1️⃣   Override OpenAI API Key"),
		w.paint(ansiDim, "→ paste your gateway key:"))
	w.printf("     %s\n", w.paint(ansiYellow+ansiBold, gatewayKey))
	w.println()

	// 2 Base URL
	w.printf("  %s  %s\n",
		w.paint(ansiBold+ansiWhite, "2️⃣   Override Base URL"),
		w.paint(ansiDim, "→ paste your public URL:"))
	w.printf("     %s\n", w.paint(ansiCyan+ansiBold, publicURL))
	w.println()

	// 3 Save
	w.printf("  %s  %s\n",
		w.paint(ansiBold+ansiWhite, "3️⃣   Click Save."),
		w.paint(ansiDim, "That's it — Cursor now routes through Discursive."))
	w.println()

	w.println("  " + bar)
	w.println()

	// ── Provider summary ─────────────────────────────────────
	w.printf("  %s  %s\n",
		w.paint(ansiDim, "⚙️   Tunnel:"),
		w.paint(ansiDim, "Cloudflare ")+w.paint(ansiDim, fmt.Sprintf("→ http://127.0.0.1:%d", port)))
	w.printf("  %s  %s\n",
		w.paint(ansiDim, "🌙  Moonshot / Kimi:"),
		w.paint(ansiDim, "https://platform.kimi.ai"))
	w.printf("  %s  %s\n",
		w.paint(ansiDim, "🌊  DeepSeek:"),
		w.paint(ansiDim, "https://platform.deepseek.com"))
	w.printf("  %s  %s\n",
		w.paint(ansiDim, "💡  Switch providers:"),
		w.paint(ansiDim, "change the Cursor model alias  —  see `discursive start | jq .`"))
	w.println()

	// ── Next action ──────────────────────────────────────────
	if fromStart {
		w.printf("  %s  %s\n",
			w.paint(ansiMagenta, "▶"),
			w.paint(ansiBold, "Starting gateway…"))
	} else {
		w.printf("  %s  %s\n",
			w.paint(ansiMagenta, "▶"),
			"Next: "+w.paint(ansiBold, "discursive start | jq ."))
	}
	w.println()
}
