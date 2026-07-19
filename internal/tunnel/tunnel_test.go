package tunnel

import (
	"context"
	"testing"
	"time"
)

func TestParseTryCloudflareURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "match", in: "Visit https://foo-bar.trycloudflare.com now", want: "https://foo-bar.trycloudflare.com"},
		{name: "no_match", in: "no url here", want: ""},
		{name: "multiple", in: "https://a.trycloudflare.com and https://b.trycloudflare.com", want: "https://a.trycloudflare.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTryCloudflareURL(tt.in)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want time.Duration
	}{
		{in: 3 * time.Second, want: 6 * time.Second},
		{in: 16 * time.Second, want: 30 * time.Second},
		{in: 30 * time.Second, want: 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.in.String(), func(t *testing.T) {
			if got := NextBackoff(tt.in); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestPIDsFromPSLines(t *testing.T) {
	lines := []string{
		"  123 cloudflared tunnel --url http://127.0.0.1:4001",
		"  456 other process",
		"789 cloudflared tunnel run --token x",
	}
	got := PIDsFromPSLines(lines, 4001)
	if len(got) != 1 || got[0] != 123 {
		t.Fatalf("got %v want [123]", got)
	}
}

func TestCloudflaredArtifact(t *testing.T) {
	tests := []struct {
		goos, goarch string
		want         string
		wantErr      bool
	}{
		{goos: "darwin", goarch: "arm64", want: "cloudflared-darwin-arm64.tgz"},
		{goos: "linux", goarch: "amd64", want: "cloudflared-linux-amd64"},
		{goos: "windows", goarch: "amd64", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"_"+tt.goarch, func(t *testing.T) {
			got, err := CloudflaredArtifact(tt.goos, tt.goarch)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestRunNoneEmitsURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	var got string
	cfg := &Config{
		Mode:      ModeNone,
		PublicURL: "https://ai.example.com/v1",
		OnURLChange: func(u string) {
			got = u
		},
	}
	_ = cfg.Run(ctx)
	if got != "https://ai.example.com/v1" {
		t.Fatalf("got %q", got)
	}
}
