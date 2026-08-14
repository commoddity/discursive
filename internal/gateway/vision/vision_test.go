package vision

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func stubGLM46vServer(t *testing.T, body map[string]any) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var callCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &callCount
}

func stubZaiKey() func() (string, bool) {
	return func() (string, bool) { return "zai-test-key", true }
}

func stubNoZaiKey() func() (string, bool) {
	return func() (string, bool) { return "", false }
}

func sampleChatBodyWithImages(images ...map[string]any) map[string]any {
	var parts []any
	parts = append(parts, map[string]any{"type": "text", "text": "describe these"})
	for _, img := range images {
		parts = append(parts, img)
	}
	return map[string]any{
		"model": "o3-mini",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": parts,
			},
		},
	}
}

func standardImageURLPart(url string) map[string]any {
	return map[string]any{
		"type":      "image_url",
		"image_url": map[string]any{"url": url},
	}
}

func TestReplaceImagesDescriptions(t *testing.T) {
	srv, count := stubGLM46vServer(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "A blue triangle on a white background",
				},
			},
		},
	})
	d := &Describer{
		client:    srv.Client(),
		chatURL:   srv.URL + visionRequestPath,
		getZaiKey: stubZaiKey(),
	}

	body := sampleChatBodyWithImages(
		standardImageURLPart("data:image/png;base64,iVBORw0KGgo="),
	)
	n, err := d.ReplaceImages(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 image described, got %d", n)
	}
	if count.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", count.Load())
	}

	msgs := body["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	// parts: 0=text, 1=description
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	descPart := parts[1].(map[string]any)
	if descPart["type"] != "text" {
		t.Fatalf("expected text type for image desc, got %v", descPart["type"])
	}
	text := descPart["text"].(string)
	if text != "[Image 1: A blue triangle on a white background]" {
		t.Fatalf("unexpected description text: %q", text)
	}
}

func TestReplaceImagesCacheHit(t *testing.T) {
	srv, count := stubGLM46vServer(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "Cached image description",
				},
			},
		},
	})
	d := &Describer{
		client:    srv.Client(),
		chatURL:   srv.URL + visionRequestPath,
		getZaiKey: stubZaiKey(),
	}

	// Two identical images — second should hit cache.
	body := sampleChatBodyWithImages(
		standardImageURLPart("https://example.com/img.png"),
		standardImageURLPart("https://example.com/img.png"),
	)
	n, err := d.ReplaceImages(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 images described, got %d", n)
	}
	if count.Load() != 1 {
		t.Fatalf("expected 1 upstream call (cache hit), got %d", count.Load())
	}
}

func TestReplaceImagesUpstreamErrorFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient balance. Please recharge."}}`))
	}))
	t.Cleanup(srv.Close)
	d := &Describer{
		client:    srv.Client(),
		chatURL:   srv.URL + visionRequestPath,
		getZaiKey: stubZaiKey(),
	}

	body := sampleChatBodyWithImages(
		standardImageURLPart("data:image/png;base64,abc123"),
	)
	n, err := d.ReplaceImages(t.Context(), body)
	if err != nil {
		t.Fatalf("expected graceful fallback (no error), got: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 image part replaced, got %d", n)
	}

	// The image part must now be a text placeholder, not the raw image_url.
	msgs := body["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	replaced := parts[1].(map[string]any)
	if replaced["type"] != "text" {
		t.Fatalf("expected image replaced with text placeholder, got type=%v", replaced["type"])
	}
	if !strings.Contains(replaced["text"].(string), "Image 1") {
		t.Fatalf("unexpected replacement text: %q", replaced["text"])
	}
}

func TestReplaceImagesNoImages(t *testing.T) {
	d := &Describer{
		client:    &http.Client{},
		chatURL:   "http://unused",
		getZaiKey: stubZaiKey(),
	}

	body := map[string]any{
		"model": "o3-mini",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "hello, no images here",
			},
		},
	}
	n, err := d.ReplaceImages(t.Context(), body)
	if err != nil {
		t.Fatalf("unexpected error for no images: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 images described, got %d", n)
	}
}

func TestReplaceImagesAppliesToAllProviders(t *testing.T) {
	// Every provider (including moonshot / thaura) must describe images now.
	for _, providerFixture := range []string{"moonshot", "thaura"} {
		t.Run(providerFixture, func(t *testing.T) {
			srv, count := stubGLM46vServer(t, map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": "a described image",
						},
					},
				},
			})
			d := &Describer{
				client:    srv.Client(),
				chatURL:   srv.URL + visionRequestPath,
				getZaiKey: stubZaiKey(),
			}

			body := sampleChatBodyWithImages(
				standardImageURLPart("https://example.com/img.png"),
			)
			// provider param removed — the describer no longer gates on provider.
			n, err := d.ReplaceImages(t.Context(), body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != 1 {
				t.Fatalf("expected 1 image described for %s, got %d", providerFixture, n)
			}
			if count.Load() != 1 {
				t.Fatalf("expected 1 upstream call, got %d", count.Load())
			}

			msgs := body["messages"].([]any)
			msg := msgs[0].(map[string]any)
			parts := msg["content"].([]any)
			descPart := parts[1].(map[string]any)
			if descPart["type"] != "text" {
				t.Fatalf("expected image replaced with text for %s, got type=%v", providerFixture, descPart["type"])
			}
		})
	}
}

func TestReplaceImagesNilDescriber(t *testing.T) {
	var d *Describer = nil
	body := sampleChatBodyWithImages(
		standardImageURLPart("https://example.com/img.png"),
	)
	n, err := d.ReplaceImages(t.Context(), body)
	if n != 0 {
		t.Fatalf("expected 0 images described for nil describer, got %d", n)
	}
	if err != nil {
		t.Fatalf("expected no error for nil describer (graceful no-op), got: %v", err)
	}
}

func TestReplaceImagesNoKey(t *testing.T) {
	d := &Describer{
		client:    &http.Client{},
		chatURL:   "http://unused",
		getZaiKey: stubNoZaiKey(),
	}

	body := sampleChatBodyWithImages(
		standardImageURLPart("https://example.com/img.png"),
	)
	n, err := d.ReplaceImages(t.Context(), body)
	if err != nil {
		t.Fatalf("expected graceful fallback (no error) without key, got: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 image replaced with placeholder, got %d", n)
	}

	// Missing key → graceful placeholder, not the raw image_url.
	msgs := body["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	replaced := parts[1].(map[string]any)
	if replaced["type"] != "text" {
		t.Fatalf("expected image replaced with text placeholder, got type=%v", replaced["type"])
	}
}

func TestReplaceImagesReusesPersistentCache(t *testing.T) {
	// First turn: describe the image once (populates the durable cache).
	// Subsequent turns with the same image must NOT re-call the vision model,
	// even after the in-memory cache has been cleared.
	srv, count := stubGLM46vServer(t, map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": "A cached, persisted image description",
				},
			},
		},
	})
	dir := t.TempDir()
	persist, err := NewPersistentCache(dir)
	if err != nil {
		t.Fatalf("NewPersistentCache: %v", err)
	}
	t.Cleanup(func() { _ = persist.Close() })

	d := &Describer{
		client:    srv.Client(),
		chatURL:   srv.URL + visionRequestPath,
		getZaiKey: stubZaiKey(),
		persist:   persist,
	}

	first := sampleChatBodyWithImages(standardImageURLPart("https://example.com/historical.png"))
	if n, err := d.ReplaceImages(t.Context(), first); err != nil || n != 1 {
		t.Fatalf("first turn: n=%d err=%v", n, err)
	}
	if count.Load() != 1 {
		t.Fatalf("first turn should make 1 upstream call, got %d", count.Load())
	}

	// Simulate a stale in-memory cache so only the durable store can serve it.
	d.cache = sync.Map{}

	second := sampleChatBodyWithImages(standardImageURLPart("https://example.com/historical.png"))
	if n, err := d.ReplaceImages(t.Context(), second); err != nil || n != 1 {
		t.Fatalf("second turn: n=%d err=%v", n, err)
	}
	if count.Load() != 1 {
		t.Fatalf("historical image must be reused from durable cache (no new upstream call), got %d calls", count.Load())
	}

	// The description text must be present in the second turn's body.
	msgs := second["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	descPart := parts[1].(map[string]any)
	if descPart["type"] != "text" {
		t.Fatalf("expected text replacement, got %v", descPart["type"])
	}
	if !strings.Contains(descPart["text"].(string), "persisted image description") {
		t.Fatalf("unexpected reused description: %q", descPart["text"])
	}
}

func TestReplaceImagesNewImageFallsBackGracefully(t *testing.T) {
	// A rate-limited vision model must NOT fail the request; a NEW (undescribed)
	// image is replaced with a placeholder, and its hash is logged so the
	// operator can tell whether Cursor resends the same bytes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Usage limit reached for 5 hour."}}`))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	persist, err := NewPersistentCache(dir)
	if err != nil {
		t.Fatalf("NewPersistentCache: %v", err)
	}
	t.Cleanup(func() { _ = persist.Close() })

	d := &Describer{
		client:    srv.Client(),
		chatURL:   srv.URL + visionRequestPath,
		getZaiKey: stubZaiKey(),
		persist:   persist,
	}

	// A new, undescribed image against a rate-limited model falls back to a
	// placeholder instead of failing.
	newBody := sampleChatBodyWithImages(standardImageURLPart("https://example.com/new.png"))
	if n, err := d.ReplaceImages(t.Context(), newBody); n != 1 || err != nil {
		t.Fatalf("expected graceful fallback for new image, got n=%d err=%v", n, err)
	}
	msgs := newBody["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	if parts[1].(map[string]any)["type"] != "text" {
		t.Fatalf("expected text placeholder, got %v", parts[1].(map[string]any)["type"])
	}

	// Pre-populate the durable cache, then a turn carrying only that historical
	// image must reuse the description (no upstream call, no failure).
	if err := persist.Put(hashImageURL("https://example.com/old.png"), "known old image"); err != nil {
		t.Fatalf("persist.Put: %v", err)
	}
	histBody := sampleChatBodyWithImages(standardImageURLPart("https://example.com/old.png"))
	if n, err := d.ReplaceImages(t.Context(), histBody); n != 1 || err != nil {
		t.Fatalf("historical image must reuse cache, got n=%d err=%v", n, err)
	}
	histParts := histBody["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if !strings.Contains(histParts[1].(map[string]any)["text"].(string), "known old image") {
		t.Fatalf("expected cached description, got %q", histParts[1].(map[string]any)["text"])
	}
}

func TestHashImageURLDataURI(t *testing.T) {
	u1 := "data:image/png;base64,iVBORw0KGgo="
	u2 := "data:image/png;base64,iVBORw0KGgo="
	u3 := "data:image/png;base64,different"
	if hashImageURL(u1) != hashImageURL(u2) {
		t.Fatal("identical data URIs should hash identically")
	}
	if hashImageURL(u1) == hashImageURL(u3) {
		t.Fatal("different data URIs should hash differently")
	}
}

func TestHashImageURLHTTPS(t *testing.T) {
	u1 := "https://example.com/img.png"
	u2 := "https://example.com/img.png"
	u3 := "https://example.com/other.png"
	if hashImageURL(u1) != hashImageURL(u2) {
		t.Fatal("identical HTTPS URLs should hash identically")
	}
	if hashImageURL(u1) == hashImageURL(u3) {
		t.Fatal("different HTTPS URLs should hash differently")
	}
}

func TestExtractImageURL(t *testing.T) {
	tests := []struct {
		name string
		part map[string]any
		want string
	}{
		{
			name: "standard image_url",
			part: standardImageURLPart("https://example.com/a.png"),
			want: "https://example.com/a.png",
		},
		{
			name: "image_url without type field",
			part: map[string]any{
				"image_url": map[string]any{"url": "https://example.com/b.jpg"},
			},
			want: "https://example.com/b.jpg",
		},
		{
			name: "text part",
			part: map[string]any{"type": "text", "text": "hello"},
			want: "",
		},
		{
			name: "image_url with detail field",
			part: map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "https://example.com/d.png", "detail": "high"},
			},
			want: "https://example.com/d.png",
		},
		{
			name: "nil image_url field",
			part: map[string]any{"type": "image_url"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractImageURL(tt.part)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
