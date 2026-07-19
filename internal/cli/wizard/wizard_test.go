package wizard

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPaintNoColor(t *testing.T) {
	w := New(Options{Out: &bytes.Buffer{}, Interactive: false, Sleep: func(time.Duration) {}})
	if got := w.paint(ansiCyan, "hi"); got != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestPaintColor(t *testing.T) {
	w := &UI{out: &bytes.Buffer{}, color: true, interactive: true, sleep: func(time.Duration) {}}
	got := w.paint(ansiCyan, "hi")
	if !strings.Contains(got, ansiCyan) || !strings.HasSuffix(got, ansiReset) {
		t.Fatalf("got %q", got)
	}
}

func TestIntroPlain(t *testing.T) {
	var buf bytes.Buffer
	w := New(Options{Out: &buf, Interactive: false, Sleep: func(time.Duration) {}})
	w.Intro(false)
	if !strings.Contains(buf.String(), "Discursive setup") {
		t.Fatalf("missing setup text: %q", buf.String())
	}
}

func TestStepDoneAndFinishPlain(t *testing.T) {
	var buf bytes.Buffer
	w := New(Options{Out: &buf, Interactive: false, Sleep: func(time.Duration) {}})
	w.stepDone(1, 2, "🌙", "Moonshot", "saved")
	w.Finish(false, "https://ai.example.com/v1", 4001, "sk-test")
	out := buf.String()
	if !strings.Contains(out, "Moonshot") {
		t.Fatalf("missing step: %q", out)
	}
	if !strings.Contains(out, "discursive start") {
		t.Fatalf("missing next step: %q", out)
	}
}

func TestIntroAnimatedNoSleep(t *testing.T) {
	var buf bytes.Buffer
	w := New(Options{Out: &buf, Interactive: true, Sleep: func(time.Duration) {}})
	w.color = true
	w.Intro(true)
	out := buf.String()
	if !strings.Contains(out, "██") {
		t.Fatalf("missing banner: %q", out)
	}
	if !strings.Contains(out, "Setup needed") {
		t.Fatalf("missing fromStart copy: %q", out)
	}
}

func TestAskSecretFromFlag(t *testing.T) {
	var buf bytes.Buffer
	w := New(Options{Out: &buf, Interactive: false, Sleep: func(time.Duration) {}})
	got, err := w.AskSecret(1, 1, "🌙", "Moonshot", "", "sk-flag")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sk-flag" {
		t.Fatalf("got %q", got)
	}
}

func TestAskLinePipe(t *testing.T) {
	var out bytes.Buffer
	w := New(Options{
		Out:         &out,
		In:          strings.NewReader("https://x.example.com/v1\n"),
		Interactive: false,
		Sleep:       func(time.Duration) {},
	})
	got, err := w.AskLine(1, 1, "🔗", "URL", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://x.example.com/v1" {
		t.Fatalf("got %q", got)
	}
}
