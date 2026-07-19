//go:build live

package gateway_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// findCloudflared locates the cloudflared binary. Returns (path, true) or ("", false).
func findCloudflared() (string, bool) {
	// Check project-local .bin/cloudflared
	if p, err := filepath.Abs(".bin/cloudflared"); err == nil {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	// Check user's .kimi-cursor directory
	home, err := os.UserHomeDir()
	if err == nil {
		p := filepath.Join(home, ".kimi-cursor", "cloudflared")
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	// Check PATH
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, true
	}
	return "", false
}

// tunnelURLRegex matches cloudflared Quick Tunnel URLs.
var tunnelURLRegex = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// waitForTunnelURL reads lines from a reader (cloudflared stderr) until a trycloudflare.com URL
// is found or the deadline is exceeded.
func waitForTunnelURL(r io.Reader, deadline time.Duration) (string, error) {
	scanner := bufio.NewScanner(r)
	timeout := time.After(deadline)
	urlCh := make(chan string, 1)

	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if m := tunnelURLRegex.FindString(line); m != "" {
				urlCh <- m
				return
			}
		}
	}()

	select {
	case url := <-urlCh:
		return url, nil
	case <-timeout:
		return "", fmt.Errorf("timed out waiting for tunnel URL after %v", deadline)
	}
}

func TestLive_Tunnel(t *testing.T) {
	cloudflaredPath, ok := findCloudflared()
	if !ok {
		t.Skip("cloudflared not found in .bin/, ~/.kimi-cursor/, or $PATH")
	}
	t.Logf("cloudflared: %s", cloudflaredPath)

	const port uint16 = 18432
	srv, _, gatewayKey := startLiveGateway(t, port)
	defer func() { _ = srv.Shutdown(t.Context()) }()

	hasMoonshot := os.Getenv("MOONSHOT_API_KEY") != ""
	hasDeepSeek := os.Getenv("DEEPSEEK_API_KEY") != ""
	hasAnyKey := hasMoonshot || hasDeepSeek

	// Phase A: Start cloudflared Quick Tunnel
	cmd := exec.Command(cloudflaredPath,
		"tunnel",
		"--protocol", "http2",
		"--url", fmt.Sprintf("http://127.0.0.1:%d", port),
	)
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("cloudflared stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start cloudflared: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	tunnelURL, err := waitForTunnelURL(stderr, 45*time.Second)
	if err != nil {
		t.Fatalf("tunnel URL: %v", err)
	}
	t.Logf("public tunnel URL: %s", tunnelURL)

	// Quick Tunnel DNS can take a moment to propagate. Give it a head start.
	t.Logf("waiting for DNS propagation...")
	time.Sleep(5 * time.Second)

	// Health check through the public URL (retry up to 20x, 3s apart)
	healthOk := false
	for i := 0; i < 20; i++ {
		resp, err := http.Get(tunnelURL + "/health")
		if err != nil {
			t.Logf("health attempt %d: %v", i+1, err)
			time.Sleep(3 * time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == 200 {
			var health map[string]any
			if err := json.Unmarshal(body, &health); err == nil {
				if ok, _ := health["ok"].(bool); ok {
					healthOk = true
					t.Logf("public /health: OK (attempt %d)", i+1)
					break
				}
			}
		}
		t.Logf("health attempt %d: status %d body %s", i+1, resp.StatusCode, body)
		time.Sleep(2 * time.Second)
	}
	if !healthOk {
		t.Fatal("public /health did not respond through tunnel after 20 attempts")
	}

	// Phase B: Model probe through public URL (if any key present)
	if hasAnyKey {
		modelToUse := "gpt-5-high"
		if !hasMoonshot {
			modelToUse = "gpt-4o-mini"
		}

		probeBody := fmt.Sprintf(`{"model":"%s","temperature":0,"messages":[],"tools":[{"type":"function","name":"mcp.fs.read_file","parameters":{"type":"object","properties":{}}},{"type":"custom","name":"apply_patch","description":"patch"}]}`, modelToUse)
		req, _ := http.NewRequest(http.MethodPost, tunnelURL+"/v1/chat/completions", strings.NewReader(probeBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+gatewayKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("public probe request: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("public probe failed: status %d body %s", resp.StatusCode, body)
		}
		t.Logf("public probe (empty msgs + MCP tools): OK")
	}

	// Phase C: Streaming chat through public URL (only if Moonshot key set)
	if hasMoonshot {
		streamBody := `{"model":"gpt-5-high","stream":true,"messages":[{"role":"user","content":"Reply with exactly: TUNNEL_OK"}],"max_tokens":50}`
		req, _ := http.NewRequest(http.MethodPost, tunnelURL+"/v1/chat/completions", strings.NewReader(streamBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+gatewayKey)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("public stream request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("public stream failed: status %d body %s", resp.StatusCode, body)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/event-stream") {
			t.Fatalf("public stream content-type: %q", ct)
		}

		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodyStr := string(raw)

		if !strings.Contains(bodyStr, "[DONE]") {
			t.Fatalf("public stream missing [DONE]: %s", bodyStr)
		}
		if !strings.Contains(strings.ToUpper(bodyStr), "TUNNEL") {
			t.Fatalf("public stream missing TUNNEL: %s", bodyStr)
		}
		t.Logf("public streaming chat through tunnel: OK")
	}
}
