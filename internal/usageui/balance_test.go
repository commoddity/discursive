package usageui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/usage"
)

func TestFetchMoonshotBalance(t *testing.T) {
	tests := []struct {
		name       string
		getKey     func() (string, bool)
		status     int
		body       string
		wantConf   bool
		wantUSD    *float64
		wantErrSub string
	}{
		{
			name:     "no key source",
			getKey:   nil,
			wantConf: false,
		},
		{
			name:     "key unset",
			getKey:   func() (string, bool) { return "", false },
			wantConf: false,
		},
		{
			name:     "success",
			getKey:   func() (string, bool) { return "sk-test", true },
			status:   200,
			body:     `{"code":0,"data":{"available_balance":49.58894,"voucher_balance":46.5,"cash_balance":3.0},"scode":"0x0","status":true}`,
			wantConf: true,
			wantUSD:  floatPtr(49.58894),
		},
		{
			name:       "unauthorized",
			getKey:     func() (string, bool) { return "sk-bad", true },
			status:     401,
			body:       `{"error":{"message":"bad key"}}`,
			wantConf:   true,
			wantErrSub: "unauthorized",
		},
		{
			name:       "nonzero code",
			getKey:     func() (string, bool) { return "sk-test", true },
			status:     200,
			body:       `{"code":1,"data":{"available_balance":0},"scode":"x","status":false}`,
			wantConf:   true,
			wantErrSub: "code 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *http.Client
			if tt.getKey != nil {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/users/me/balance" && !strings.HasSuffix(r.URL.Path, "/users/me/balance") {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					auth := r.Header.Get("Authorization")
					if !strings.HasPrefix(auth, "Bearer ") {
						t.Errorf("missing bearer auth")
					}
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer ts.Close()
				client = &http.Client{Transport: rewriteHost(ts.URL)}
			}

			got := fetchMoonshotBalance(client, tt.getKey)
			if got.Configured != tt.wantConf {
				t.Fatalf("configured=%v want %v (err=%q)", got.Configured, tt.wantConf, got.Error)
			}
			if tt.wantErrSub != "" && !strings.Contains(got.Error, tt.wantErrSub) {
				t.Fatalf("error %q want substring %q", got.Error, tt.wantErrSub)
			}
			if tt.wantUSD == nil {
				if got.AvailableUSD != nil {
					t.Fatalf("available_usd=%v want nil", *got.AvailableUSD)
				}
			} else if got.AvailableUSD == nil || *got.AvailableUSD != *tt.wantUSD {
				t.Fatalf("available_usd=%v want %v", got.AvailableUSD, *tt.wantUSD)
			}
		})
	}
}

func TestFetchDeepSeekBalance(t *testing.T) {
	tests := []struct {
		name     string
		getKey   func() (string, bool)
		status   int
		body     string
		wantConf bool
		wantUSD  *float64
		wantCur  string
	}{
		{
			name:     "no key",
			getKey:   func() (string, bool) { return "", false },
			wantConf: false,
		},
		{
			name:     "usd balance",
			getKey:   func() (string, bool) { return "sk-ds", true },
			status:   200,
			body:     `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"25.50","granted_balance":"0","topped_up_balance":"25.50"}]}`,
			wantConf: true,
			wantUSD:  floatPtr(25.50),
			wantCur:  "USD",
		},
		{
			name:     "cny with fx",
			getKey:   func() (string, bool) { return "sk-ds", true },
			status:   200,
			body:     `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00","granted_balance":"10.00","topped_up_balance":"100.00"}]}`,
			wantConf: true,
			wantUSD:  floatPtr(15.714285714285714), // 110/7
			wantCur:  "CNY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.getKey == nil || !func() bool { _, ok := tt.getKey(); return ok }() {
				got := fetchDeepSeekBalance(nil, tt.getKey)
				if got.Configured != tt.wantConf {
					t.Fatalf("configured=%v want %v", got.Configured, tt.wantConf)
				}
				return
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/user/balance", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})
			mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"rates":{"CNY":7.0}}`))
			})
			ts := httptest.NewServer(mux)
			defer ts.Close()

			client := &http.Client{Transport: rewriteHostMulti(map[string]string{
				"api.deepseek.com":    ts.URL,
				"api.frankfurter.app": ts.URL,
			})}

			got := fetchDeepSeekBalance(client, tt.getKey)
			if got.Configured != tt.wantConf {
				t.Fatalf("configured=%v want %v err=%q", got.Configured, tt.wantConf, got.Error)
			}
			if tt.wantUSD != nil {
				if got.AvailableUSD == nil {
					t.Fatalf("available_usd=nil want %v", *tt.wantUSD)
				}
				if diff := *got.AvailableUSD - *tt.wantUSD; diff > 0.001 || diff < -0.001 {
					t.Fatalf("available_usd=%v want %v", *got.AvailableUSD, *tt.wantUSD)
				}
			}
			if tt.wantCur != "" && got.Currency != tt.wantCur {
				t.Fatalf("currency=%q want %q", got.Currency, tt.wantCur)
			}
		})
	}
}

func TestHandleBalancesNoKeys(t *testing.T) {
	srv := &Server{addr: "", store: testStore(t)}
	req := httptest.NewRequest("GET", "/api/balances", nil)
	w := httptest.NewRecorder()
	srv.handleBalances(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var resp BalancesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Moonshot.Configured || resp.DeepSeek.Configured || resp.Zai.Configured {
		t.Fatalf("expected unconfigured: %+v", resp)
	}
}

func TestAnnotateZaiHourlyReset(t *testing.T) {
	store := testStore(t)
	now := time.Now().UTC()
	// Z.AI event 1h ago: window opened then, reset in ~4h.
	if _, err := store.Record(usage.Event{
		SessionID: "s1", Provider: config.ProviderZai, Model: "glm-4.7",
		Timestamp: now.Add(-1 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		buckets []CreditBucket
		check   func(t *testing.T, b []CreditBucket)
	}{
		{
			name: "5-hour bucket gets estimated reset",
			buckets: []CreditBucket{
				{Label: "Weekly", Remaining: 50000, NextResetMs: 1786438400971},
				{Label: "5-Hour", Remaining: 9000},
			},
			check: func(t *testing.T, b []CreditBucket) {
				var five *CreditBucket
				for i := range b {
					if b[i].Label == "5-Hour" {
						five = &b[i]
					}
				}
				if five == nil || five.NextResetMs == 0 {
					t.Fatalf("5-Hour bucket missing reset: %+v", b)
				}
				want := now.Add(-1 * time.Hour).Add(5 * time.Hour)
				got := time.UnixMilli(five.NextResetMs)
				if diff := got.Sub(want); diff < -time.Minute || diff > time.Minute {
					t.Fatalf("reset = %v, want ~%v", got, want)
				}
			},
		},
		{
			name:    "provider-supplied reset not overwritten",
			buckets: []CreditBucket{{Label: "5-Hour", Remaining: 9000, NextResetMs: 1234567890}},
			check: func(t *testing.T, b []CreditBucket) {
				if b[0].NextResetMs != 1234567890 {
					t.Fatalf("reset overwritten: %d", b[0].NextResetMs)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &Server{store: store}
			zai := ProviderBalance{Configured: true, Credits: tt.buckets}
			if err := srv.annotateZaiHourlyReset(&zai); err != nil {
				t.Fatal(err)
			}
			tt.check(t, zai.Credits)
		})
	}

	// No Z.AI spend in the window: nothing annotated.
	empty := testStore(t)
	srv := &Server{store: empty}
	zai := ProviderBalance{Credits: []CreditBucket{{Label: "5-Hour"}}}
	if err := srv.annotateZaiHourlyReset(&zai); err != nil {
		t.Fatal(err)
	}
	if zai.Credits[0].NextResetMs != 0 {
		t.Fatalf("expected no reset annotation, got %d", zai.Credits[0].NextResetMs)
	}
}

func TestFetchZaiBalance(t *testing.T) {
	zaiBody := `{"code":200,"msg":"Operation successful","success":true,"data":{"level":"lite","limits":[{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":2000,"remaining":1800,"percentage":10},{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":10000,"remaining":7500,"percentage":25,"nextResetTime":1786438400971}]}}`

	tests := []struct {
		name        string
		getKey      func() (string, bool)
		status      int
		body        string
		wantConf    bool
		wantAmt     *float64
		wantPct     *float64
		wantResetMs int64
		wantErrSub  string
	}{
		{
			name:     "no key",
			getKey:   func() (string, bool) { return "", false },
			wantConf: false,
		},
		{
			name:     "nil getKey",
			getKey:   nil,
			wantConf: false,
		},
		{
			name:        "success weekly credits",
			getKey:      func() (string, bool) { return "sk-zai-test", true },
			status:      200,
			body:        zaiBody,
			wantConf:    true,
			wantAmt:     floatPtr(7500),
			wantPct:     floatPtr(25),
			wantResetMs: 1786438400971,
		},
		{
			name:       "unauthorized",
			getKey:     func() (string, bool) { return "sk-bad", true },
			status:     401,
			body:       `{"code":401}`,
			wantConf:   true,
			wantErrSub: "unauthorized",
		},
		{
			name:       "api error code",
			getKey:     func() (string, bool) { return "sk-zai-test", true },
			status:     200,
			body:       `{"code":500,"msg":"fail","success":false,"data":{}}`,
			wantConf:   true,
			wantErrSub: "code 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *http.Client
			if tt.getKey != nil && tt.status != 0 {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.HasSuffix(r.URL.Path, "/api/monitor/usage/quota/limit") {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					auth := r.Header.Get("Authorization")
					if !strings.HasPrefix(auth, "Bearer ") {
						t.Errorf("missing bearer auth")
					}
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer ts.Close()
				client = &http.Client{Transport: rewriteHost(ts.URL)}
			}

			got := fetchZaiBalance(client, tt.getKey)
			if got.Configured != tt.wantConf {
				t.Fatalf("configured=%v want %v (err=%q)", got.Configured, tt.wantConf, got.Error)
			}
			if tt.wantErrSub != "" {
				if !strings.Contains(got.Error, tt.wantErrSub) {
					t.Fatalf("error=%q want substring %q", got.Error, tt.wantErrSub)
				}
				return
			}
			if got.Error != "" {
				t.Fatalf("unexpected error: %q", got.Error)
			}
			if tt.wantAmt != nil {
				if got.Amount == nil || *got.Amount != *tt.wantAmt {
					t.Fatalf("amount=%v want %v", got.Amount, tt.wantAmt)
				}
				if got.Currency != "credits" {
					t.Fatalf("currency=%q want credits", got.Currency)
				}
			}
			if tt.wantPct != nil {
				if got.UsagePercent == nil || *got.UsagePercent != *tt.wantPct {
					t.Fatalf("usage_percent=%v want %v", got.UsagePercent, tt.wantPct)
				}
			}
			if tt.wantResetMs != 0 {
				// Weekly bucket carries the nextReseTime; verify it propagates.
				found := false
				for _, c := range got.Credits {
					if c.Label == "Weekly" && c.NextResetMs == tt.wantResetMs {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("weekly bucket reset not propagated: %+v", got.Credits)
				}
			}
		})
	}
}

func TestCollectZaiCreditBuckets(t *testing.T) {
	now := time.Now().UTC()
	weeklyReset := now.Add(6 * 24 * time.Hour).UnixMilli()
	fiveHourReset := now.Add(90 * time.Minute).UnixMilli()
	// Pro-plan shape: both buckets exceed 2000 usage; reset distance
	// discriminates (5-hour resets within 5h, weekly within 7d).
	limits := []zaiCreditLimit{
		{Type: "CREDIT_LIMIT", Unit: 3, Number: 5, Remaining: 11655, Percentage: 2, Usage: 345, NextResetTime: fiveHourReset},
		{Type: "CREDIT_LIMIT", Unit: 6, Number: 1, Remaining: 51745, Percentage: 13, Usage: 8255, NextResetTime: weeklyReset},
	}
	got, ok := collectZaiCreditBuckets(limits, now)
	if !ok {
		t.Fatal("expected buckets")
	}
	if len(got) != 2 {
		t.Fatalf("got %d buckets: %+v", len(got), got)
	}
	// Weekly is first by the ordering pass.
	if got[0].Label != "Weekly" || got[0].Remaining != 51745 || got[0].Percentage != 13 {
		t.Fatalf("weekly bucket wrong: %+v", got[0])
	}
	if got[0].NextResetMs != weeklyReset {
		t.Fatalf("weekly reset=%d want %d", got[0].NextResetMs, weeklyReset)
	}
	if got[1].Label != "5-Hour" || got[1].Remaining != 11655 {
		t.Fatalf("5-hour bucket wrong: %+v", got[1])
	}
	if got[1].NextResetMs != fiveHourReset {
		t.Fatalf("5-hour reset=%d want %d", got[1].NextResetMs, fiveHourReset)
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestFetchOpenRouterBalance(t *testing.T) {
	tests := []struct {
		name       string
		getKey     func() (string, bool)
		body       string
		status     int
		wantConf   bool
		wantPeak   *bool
		wantAmt    *float64
		wantErrSub string
	}{
		{
			name:     "no key",
			getKey:   func() (string, bool) { return "", false },
			wantConf: false,
			wantPeak: openRouterBoolPtr(false),
		},
		{
			name:     "positive balance",
			getKey:   func() (string, bool) { return "sk-or", true },
			status:   200,
			body:     `{"data":{"total_credits":10,"total_usage":3}}`,
			wantConf: true,
			wantPeak: openRouterBoolPtr(true),
			wantAmt:  floatPtr(7),
		},
		{
			name:     "zero balance",
			getKey:   func() (string, bool) { return "sk-or", true },
			status:   200,
			body:     `{"data":{"total_credits":5,"total_usage":5}}`,
			wantConf: true,
			wantPeak: openRouterBoolPtr(false),
			wantAmt:  floatPtr(0),
		},
		{
			name:       "unauthorized",
			getKey:     func() (string, bool) { return "sk-bad", true },
			status:     401,
			body:       `{}`,
			wantConf:   true,
			wantPeak:   openRouterBoolPtr(false),
			wantErrSub: "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *http.Client
			if tt.getKey != nil && tt.status != 0 {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.HasSuffix(r.URL.Path, "/api/v1/credits") {
						t.Errorf("unexpected path %s", r.URL.Path)
					}
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer ts.Close()
				client = &http.Client{Transport: rewriteHost(ts.URL)}
			}

			got := fetchOpenRouterBalance(client, tt.getKey)
			if got.Configured != tt.wantConf {
				t.Fatalf("configured=%v want %v", got.Configured, tt.wantConf)
			}
			if tt.wantPeak != nil {
				if got.PeakUsable == nil || *got.PeakUsable != *tt.wantPeak {
					t.Fatalf("peak_usable=%v want %v", got.PeakUsable, *tt.wantPeak)
				}
			}
			if tt.wantErrSub != "" && !strings.Contains(got.Error, tt.wantErrSub) {
				t.Fatalf("error=%q want substring %q", got.Error, tt.wantErrSub)
			}
			if tt.wantAmt != nil {
				if got.Amount == nil || *got.Amount != *tt.wantAmt {
					t.Fatalf("amount=%v want %v", got.Amount, *tt.wantAmt)
				}
			}
		})
	}
}

func openRouterBoolPtr(v bool) *bool { return &v }

// rewriteHost redirects all outbound requests to the given base URL (httptest).
type rewriteHost string

func (h rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	u := string(h)
	nr := req.Clone(req.Context())
	nr.URL.Scheme = "http"
	if strings.HasPrefix(u, "https://") {
		nr.URL.Scheme = "https"
		u = strings.TrimPrefix(u, "https://")
	} else {
		u = strings.TrimPrefix(u, "http://")
	}
	hostPath := strings.SplitN(u, "/", 2)
	nr.URL.Host = hostPath[0]
	// Preserve original path (Moonshot uses /v1/users/me/balance).
	nr.RequestURI = ""
	return http.DefaultTransport.RoundTrip(nr)
}

type rewriteHostMulti map[string]string

func (m rewriteHostMulti) RoundTrip(req *http.Request) (*http.Response, error) {
	nr := req.Clone(req.Context())
	nr.RequestURI = ""
	if base, ok := m[req.URL.Host]; ok {
		u := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
		nr.URL.Scheme = "http"
		nr.URL.Host = strings.SplitN(u, "/", 2)[0]
	}
	return http.DefaultTransport.RoundTrip(nr)
}
