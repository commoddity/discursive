package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func TestOpenRouterPeakRerouteOK(t *testing.T) {
	t.Run("no key", func(t *testing.T) {
		s := &Server{settings: &config.AppSettings{}}
		if s.openRouterPeakRerouteOK() {
			t.Fatal("expected false without key")
		}
	})

	t.Run("test override", func(t *testing.T) {
		s := &Server{openRouterPeakUsable: func() bool { return true }}
		if !s.openRouterPeakRerouteOK() {
			t.Fatal("expected override true")
		}
	})

	t.Run("zero balance from API", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"total_credits":5,"total_usage":5}}`))
		}))
		defer ts.Close()

		dataRoot := t.TempDir()
		s := &Server{
			cfg:      ServerConfig{DataRoot: dataRoot},
			settings: &config.AppSettings{},
			client:   &http.Client{Transport: rewriteORHost(ts.URL)},
		}
		_ = s.settings.SetOpenRouterKey(dataRoot, "sk-or")
		if s.openRouterPeakRerouteOK() {
			t.Fatal("expected false for zero balance")
		}
	})

	t.Run("positive balance cached", func(t *testing.T) {
		var calls int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"total_credits":10,"total_usage":3}}`))
		}))
		defer ts.Close()

		dataRoot := t.TempDir()
		s := &Server{
			cfg:      ServerConfig{DataRoot: dataRoot},
			settings: &config.AppSettings{},
			client:   &http.Client{Transport: rewriteORHost(ts.URL)},
		}
		_ = s.settings.SetOpenRouterKey(dataRoot, "sk-or")
		if !s.openRouterPeakRerouteOK() {
			t.Fatal("expected true with positive balance")
		}
		if !s.openRouterPeakRerouteOK() {
			t.Fatal("expected cached true")
		}
		if calls != 1 {
			t.Fatalf("expected one API call due to cache, got %d", calls)
		}
	})
}

type rewriteORHost string

func (h rewriteORHost) RoundTrip(req *http.Request) (*http.Response, error) {
	nr := req.Clone(req.Context())
	nr.URL.Scheme = "http"
	u := strings.TrimPrefix(string(h), "http://")
	nr.URL.Host = strings.SplitN(u, "/", 2)[0]
	nr.RequestURI = ""
	return http.DefaultTransport.RoundTrip(nr)
}

func TestOpenRouterPeakRerouteOKCacheExpiry(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"total_credits":10,"total_usage":0}}`))
	}))
	defer ts.Close()

	dataRoot := t.TempDir()
	s := &Server{
		cfg:      ServerConfig{DataRoot: dataRoot},
		settings: &config.AppSettings{},
		client:   &http.Client{Transport: rewriteORHost(ts.URL)},
	}
	_ = s.settings.SetOpenRouterKey(dataRoot, "sk-or")
	s.orBalanceMu.Lock()
	s.orBalance = orBalanceCache{checkedAt: time.Now().Add(-2 * openRouterBalanceTTL), usable: false}
	s.orBalanceMu.Unlock()

	if !s.openRouterPeakRerouteOK() {
		t.Fatal("expected refresh after TTL expiry")
	}
}
