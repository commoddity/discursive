package openrouter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchCredits(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		wantRem   float64
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "success",
			status:  200,
			body:    `{"data":{"total_credits":10,"total_usage":3}}`,
			wantRem: 7,
		},
		{
			name:    "zero remaining",
			status:  200,
			body:    `{"data":{"total_credits":5,"total_usage":5}}`,
			wantRem: 0,
		},
		{
			name:      "unauthorized",
			status:    401,
			body:      `{}`,
			wantErr:   true,
			errSubstr: "unauthorized",
		},
		{
			name:      "invalid json",
			status:    200,
			body:      `{`,
			wantErr:   true,
			errSubstr: "invalid response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/credits" {
					t.Fatalf("path %s", r.URL.Path)
				}
				if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sk-or") {
					t.Fatalf("auth %q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer ts.Close()

			client := &http.Client{Transport: rewriteHost(ts.URL)}
			got, err := FetchCredits(client, "sk-or-test")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Remaining != tt.wantRem {
				t.Fatalf("remaining=%v want %v", got.Remaining, tt.wantRem)
			}
		})
	}
}

func TestPeakRerouteUsable(t *testing.T) {
	tests := []struct {
		name       string
		configured bool
		remaining  float64
		errMsg     string
		want       bool
	}{
		{"configured with balance", true, 1.5, "", true},
		{"zero balance", true, 0, "", false},
		{"not configured", false, 10, "", false},
		{"fetch error", true, 10, "unauthorized", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PeakRerouteUsable(tt.configured, tt.remaining, tt.errMsg); got != tt.want {
				t.Fatalf("PeakRerouteUsable() = %v, want %v", got, tt.want)
			}
		})
	}
}

type rewriteHost string

func (h rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	nr := req.Clone(req.Context())
	nr.URL.Scheme = "http"
	u := strings.TrimPrefix(string(h), "http://")
	nr.URL.Host = strings.SplitN(u, "/", 2)[0]
	nr.RequestURI = ""
	return http.DefaultTransport.RoundTrip(nr)
}
