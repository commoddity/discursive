package vision

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestReplaceImagesUpstreamErrorFailsFast(t *testing.T) {
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
	if n != 0 {
		t.Fatalf("expected 0 images described on error, got %d", n)
	}
	if err == nil {
		t.Fatal("expected fail-fast error on upstream rejection")
	}
	if !strings.Contains(err.Error(), "Insufficient balance") {
		t.Fatalf("expected upstream message surfaced in error, got: %v", err)
	}

	// Body must be left untouched (fail fast, no silent placeholder).
	msgs := body["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	imgPart := parts[1].(map[string]any)
	if imgPart["type"] != "image_url" {
		t.Fatalf("expected original image_url part preserved on error, got type=%v", imgPart["type"])
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
	if err == nil {
		t.Fatal("expected error for nil describer with images")
	}
	// A nil describer means vision is unavailable → fail fast.
	if !strings.Contains(err.Error(), "vision") {
		t.Fatalf("expected vision error, got: %v", err)
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
	if n != 0 {
		t.Fatalf("expected 0 images described without key, got %d", n)
	}
	if err != ErrNoVisionKey {
		t.Fatalf("expected ErrNoVisionKey, got: %v", err)
	}

	// Body must be left untouched (fail fast, no silent placeholder).
	msgs := body["messages"].([]any)
	msg := msgs[0].(map[string]any)
	parts := msg["content"].([]any)
	imgPart := parts[1].(map[string]any)
	if imgPart["type"] != "image_url" {
		t.Fatalf("expected original image_url part preserved, got type=%v", imgPart["type"])
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
