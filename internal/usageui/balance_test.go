package usageui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestFetchZaiBalance(t *testing.T) {
	zaiBody := `{"code":200,"msg":"Operation successful","success":true,"data":{"level":"lite","limits":[{"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":2000,"remaining":1800,"percentage":10},{"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":10000,"remaining":7500,"percentage":25,"nextResetTime":1786438400971}]}}`

	tests := []struct {
		name       string
		getKey     func() (string, bool)
		status     int
		body       string
		wantConf   bool
		wantAmt    *float64
		wantPct    *float64
		wantErrSub string
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
			name:     "success weekly credits",
			getKey:   func() (string, bool) { return "sk-zai-test", true },
			status:   200,
			body:     zaiBody,
			wantConf: true,
			wantAmt:  floatPtr(7500),
			wantPct:  floatPtr(25),
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
		})
	}
}

func TestCollectZaiCreditBuckets(t *testing.T) {
	limits := []zaiCreditLimit{
		{Type: "CREDIT_LIMIT", Unit: 3, Number: 5, Remaining: 1800, Percentage: 10, Usage: 2000},
		{Type: "CREDIT_LIMIT", Unit: 6, Number: 1, Remaining: 7500, Percentage: 25, Usage: 10000},
	}
	got, ok := collectZaiCreditBuckets(limits)
	if !ok {
		t.Fatal("expected buckets")
	}
	if len(got) != 2 {
		t.Fatalf("got %d buckets: %+v", len(got), got)
	}
	// Weekly is first because it has the larger allowance.
	if got[0].Label != "Weekly" || got[0].Remaining != 7500 || got[0].Percentage != 25 {
		t.Fatalf("weekly bucket wrong: %+v", got[0])
	}
	if got[1].Label != "5-Hour" || got[1].Remaining != 1800 {
		t.Fatalf("5-hour bucket wrong: %+v", got[1])
	}
}

func floatPtr(v float64) *float64 { return &v }

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
