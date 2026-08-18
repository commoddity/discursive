package usageui

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

// KeySource supplies decrypted upstream API keys for balance checks.
// Never log or return these to clients.
type KeySource struct {
	Moonshot func() (string, bool) // key, ok
	DeepSeek func() (string, bool)
	Zai      func() (string, bool)
}

// ProviderBalance is one provider's balance for the dashboard.
// Prefer AvailableUSD for threshold coloring. When only a non-USD amount is
// known (DeepSeek CNY without FX, Z.AI credits), Amount+Currency are set and
// AvailableUSD is nil. UsagePercent is the consumed quota percentage when known
// (Z.AI coding-plan credits).
type ProviderBalance struct {
	Configured   bool     `json:"configured"`
	AvailableUSD *float64 `json:"available_usd"` // nil when unavailable / needs client FX
	Amount       *float64 `json:"amount,omitempty"`
	Currency     string   `json:"currency,omitempty"`
	UsagePercent *float64 `json:"usage_percent,omitempty"`
	// Credits holds a per-bucket breakdown when the provider is credit-based
	// (Z.AI GLM Coding Plan). Each entry is {label, remaining, percentage}.
	Credits     []CreditBucket `json:"credits,omitempty"`
	IsAvailable *bool          `json:"is_available,omitempty"` // DeepSeek only
	// ToppedUp is the native-currency portion of the balance that came from a
	// manual top-up rather than granted/earned balance (DeepSeek only). It is
	// used to net out recharge increases when computing confirmed spend.
	ToppedUp float64 `json:"topped_up,omitempty"`
	Error    string  `json:"error,omitempty"`
}

// CreditBucket is one quota window (e.g. 5-hour vs weekly) for credit-based providers.
// NextResetMs is the epoch-millisecond reset time when the provider exposes one
// (Z.AI weekly bucket); zero when the bucket does not carry a reset time.
type CreditBucket struct {
	Label       string  `json:"label"`
	Remaining   float64 `json:"remaining"`
	Percentage  float64 `json:"percentage"`
	NextResetMs int64   `json:"next_reset_ms,omitempty"`
}

// BalancesResponse is the /api/balances payload.
type BalancesResponse struct {
	Moonshot ProviderBalance `json:"moonshot"`
	DeepSeek ProviderBalance `json:"deepseek"`
	Zai      ProviderBalance `json:"zai"`
}

type deepSeekBalanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// SetKeySource wires upstream key getters used by /api/balances.
func (s *Server) SetKeySource(ks KeySource) {
	s.keySource = ks
}

func (s *Server) handleBalances(w http.ResponseWriter, r *http.Request) {
	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}

	var moonshot, deepseek, zai ProviderBalance
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		moonshot = fetchMoonshotBalance(client, s.keySource.Moonshot)
	}()
	go func() {
		defer wg.Done()
		deepseek = fetchDeepSeekBalance(client, s.keySource.DeepSeek)
	}()
	go func() {
		defer wg.Done()
		zai = fetchZaiBalance(client, s.keySource.Zai)
	}()
	wg.Wait()

	if err := s.annotateZaiHourlyReset(&zai); err != nil {
		slog.Debug("zai hourly reset estimate failed", "error", err.Error())
	}

	writeJSON(w, BalancesResponse{Moonshot: moonshot, DeepSeek: deepseek, Zai: zai})
}

// zaiHourlyWindow is the rolling 5-hour Z.AI coding-plan credit window.
const zaiHourlyWindow = 5 * time.Hour

// annotateZaiHourlyReset fills NextResetMs on the Z.AI 5-Hour bucket from the
// local usage store: the rolling window opens at the first spend inside the
// window, so reset ≈ that timestamp + 5h. The provider API exposes no reset
// time for this bucket. Best-effort: silent on any store/lookup failure.
func (s *Server) annotateZaiHourlyReset(zai *ProviderBalance) error {
	if s.store == nil {
		return nil
	}
	first, ok, err := s.store.QueryFirstEventSince(string(config.ProviderZai), time.Now().UTC().Add(-zaiHourlyWindow))
	if err != nil {
		return err
	}
	if !ok {
		return nil // no Z.AI spend in the window; bucket is full, nothing to count down
	}
	reset := first.Add(zaiHourlyWindow)
	for i := range zai.Credits {
		if zai.Credits[i].Label == "5-Hour" && zai.Credits[i].NextResetMs == 0 {
			zai.Credits[i].NextResetMs = reset.UnixMilli()
		}
	}
	return nil
}

// ---- /api/balance-spend ----

// PeriodSpend holds confirmed spend values for one provider across multiple
// period lengths, derived from balance snapshots.
type PeriodSpend struct {
	Day     *float64 `json:"day"`
	Week    *float64 `json:"week"`
	Month   *float64 `json:"month"`
	Covered bool     `json:"covered"` // true when we have a boundary snapshot near period start
}

// BalanceSpendResponse maps provider name to its period-level confirmed spend.
// Only providers with USD-denominated balances are included; credit-based
// providers (Z.AI) are omitted because credit spend is not convertible to USD.
type BalanceSpendResponse struct {
	Moonshot PeriodSpend `json:"moonshot"`
	DeepSeek PeriodSpend `json:"deepseek"`
}

func (s *Server) handleBalanceSpend(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()

	var resp BalanceSpendResponse
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		resp.Moonshot = s.confirmedPeriodSpend(config.ProviderMoonshot, now)
	}()
	go func() {
		defer wg.Done()
		resp.DeepSeek = s.confirmedPeriodSpend(config.ProviderDeepSeek, now)
	}()
	wg.Wait()

	writeJSON(w, resp)
}

func (s *Server) confirmedPeriodSpend(prov config.Provider, now time.Time) PeriodSpend {
	var ps PeriodSpend
	for _, b := range basesForTime(now) {
		rng := periodRange(b.basis, b.periodStart)
		spend, err := s.store.ConfirmedSpend(prov, b.basis, rng.after, rng.before)
		if err != nil {
			slog.Warn("confirmed spend query failed", "provider", prov, "basis", b.basis, "err", err)
			continue
		}
		if spend < 0 {
			continue
		}
		v := spend
		switch b.basis {
		case "day":
			ps.Day = &v
		case "week":
			ps.Week = &v
		case "month":
			ps.Month = &v
		}
	}
	// Check if the month period is fully covered (boundary snapshot exists near
	// the 1st of the month). Use a 1-hour tolerance — the controller captures
	// every 15 minutes, so the first boundary snapshot should appear within
	// the first hour of a new period.
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	covered, err := s.store.PeriodCovered(prov, "month", monthStart, time.Hour)
	if err != nil {
		slog.Warn("period covered check failed", "provider", prov, "err", err)
	}
	ps.Covered = covered
	return ps
}

type periodBounds struct{ after, before time.Time }

// periodRange returns the snapshot search window for a period basis starting at
// periodStart. after is the first moment of the period, before the last.
// Disambiguation relies on the basis label because day/week/month starts can
// coincide (e.g. the 1st landing on a Monday).
func periodRange(basis string, periodStart time.Time) periodBounds {
	y, m, _ := periodStart.Date()
	var end time.Time
	switch basis {
	case "week":
		end = periodStart.AddDate(0, 0, 7)
	case "month":
		if m == time.December {
			end = time.Date(y+1, 1, 1, 0, 0, 0, 0, time.UTC)
		} else {
			end = time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
		}
	case "day":
		end = periodStart.AddDate(0, 0, 1)
	default:
		end = periodStart.Add(24 * time.Hour) // sample
	}
	return periodBounds{after: periodStart, before: end.Add(-1 * time.Second)}
}

func fetchMoonshotBalance(client *http.Client, getKey func() (string, bool)) ProviderBalance {
	if getKey == nil {
		return ProviderBalance{Configured: false}
	}
	key, ok := getKey()
	if !ok || key == "" {
		return ProviderBalance{Configured: false}
	}
	base, err := config.UpstreamBaseURL(config.ProviderMoonshot)
	if err != nil {
		return ProviderBalance{Configured: true, Error: "base url unavailable"}
	}
	url := strings.TrimRight(base, "/") + "/users/me/balance"

	body, status, err := getJSON(client, url, key)
	if err != nil {
		return ProviderBalance{Configured: true, Error: err.Error()}
	}
	if status == http.StatusUnauthorized {
		return ProviderBalance{Configured: true, Error: "unauthorized"}
	}
	if status != http.StatusOK {
		return ProviderBalance{Configured: true, Error: fmt.Sprintf("upstream status %d", status)}
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			AvailableBalance float64 `json:"available_balance"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ProviderBalance{Configured: true, Error: "invalid response"}
	}
	if resp.Code != 0 {
		return ProviderBalance{Configured: true, Error: fmt.Sprintf("code %d", resp.Code)}
	}
	v := resp.Data.AvailableBalance
	return ProviderBalance{Configured: true, AvailableUSD: &v, Amount: &v, Currency: "USD"}
}

func fetchDeepSeekBalance(client *http.Client, getKey func() (string, bool)) ProviderBalance {
	if getKey == nil {
		return ProviderBalance{Configured: false}
	}
	key, ok := getKey()
	if !ok || key == "" {
		return ProviderBalance{Configured: false}
	}
	base, err := config.UpstreamBaseURL(config.ProviderDeepSeek)
	if err != nil {
		return ProviderBalance{Configured: true, Error: "base url unavailable"}
	}
	url := strings.TrimRight(base, "/") + "/user/balance"

	body, status, err := getJSON(client, url, key)
	if err != nil {
		return ProviderBalance{Configured: true, Error: err.Error()}
	}
	if status == http.StatusUnauthorized {
		return ProviderBalance{Configured: true, Error: "unauthorized"}
	}
	if status != http.StatusOK {
		return ProviderBalance{Configured: true, Error: fmt.Sprintf("upstream status %d", status)}
	}

	var resp struct {
		IsAvailable  bool                  `json:"is_available"`
		BalanceInfos []deepSeekBalanceInfo `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ProviderBalance{Configured: true, Error: "invalid response"}
	}

	avail := resp.IsAvailable
	if usdAmt, usdTopped, ok := pickDeepSeekInfo(resp.BalanceInfos, "USD"); ok {
		return ProviderBalance{
			Configured:   true,
			AvailableUSD: &usdAmt,
			Amount:       &usdAmt,
			Currency:     "USD",
			IsAvailable:  &avail,
			ToppedUp:     usdTopped,
		}
	}
	cnyAmt, cnyTopped, ok := pickDeepSeekInfo(resp.BalanceInfos, "CNY")
	if !ok {
		return ProviderBalance{Configured: true, IsAvailable: &avail, Error: "no balance info"}
	}
	rate, err := fetchUSDtoCNY(client)
	if err != nil || rate <= 0 {
		// Client can convert with its cached FX rate.
		return ProviderBalance{
			Configured:  true,
			Amount:      &cnyAmt,
			Currency:    "CNY",
			IsAvailable: &avail,
			ToppedUp:    cnyTopped,
		}
	}
	usd := cnyAmt / rate
	return ProviderBalance{
		Configured:   true,
		AvailableUSD: &usd,
		Amount:       &cnyAmt,
		Currency:     "CNY",
		IsAvailable:  &avail,
		ToppedUp:     cnyTopped,
	}
}

// pickDeepSeekInfo parses the total balance and topped-up balance for the given
// currency from the DeepSeek balance_infos list.
func pickDeepSeekInfo(infos []deepSeekBalanceInfo, currency string) (amount, toppedUp float64, ok bool) {
	for _, info := range infos {
		if !strings.EqualFold(info.Currency, currency) {
			continue
		}
		v, err := strconv.ParseFloat(info.TotalBalance, 64)
		if err != nil {
			return 0, 0, false
		}
		tp, _ := strconv.ParseFloat(info.ToppedUpBalance, 64)
		return v, tp, true
	}
	return 0, 0, false
}

func fetchUSDtoCNY(client *http.Client) (float64, error) {
	resp, err := client.Get("https://api.frankfurter.app/latest?from=USD&to=CNY")
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fx status %d", resp.StatusCode)
	}
	var data struct {
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	rate, ok := data.Rates["CNY"]
	if !ok {
		return 0, fmt.Errorf("no CNY rate")
	}
	return rate, nil
}

func fetchZaiBalance(client *http.Client, getKey func() (string, bool)) ProviderBalance {
	if getKey == nil {
		return ProviderBalance{Configured: false}
	}
	key, ok := getKey()
	if !ok || key == "" {
		return ProviderBalance{Configured: false}
	}

	// GLM Coding Plan quota API (credits-based, not USD balance).
	// Docs: https://docs.z.ai/devpack/overview
	url := "https://api.z.ai/api/monitor/usage/quota/limit"
	body, status, err := getJSON(client, url, key)
	if err != nil {
		return ProviderBalance{Configured: true, Error: err.Error()}
	}
	if status == http.StatusUnauthorized {
		return ProviderBalance{Configured: true, Error: "unauthorized"}
	}
	if status != http.StatusOK {
		return ProviderBalance{Configured: true, Error: fmt.Sprintf("upstream status %d", status)}
	}

	var resp struct {
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
		Success bool   `json:"success"`
		Data    struct {
			Level  string           `json:"level"`
			Limits []zaiCreditLimit `json:"limits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ProviderBalance{Configured: true, Error: "invalid response"}
	}
	if resp.Code != 200 || !resp.Success {
		return ProviderBalance{Configured: true, Error: fmt.Sprintf("code %d: %s", resp.Code, resp.Msg)}
	}

	limits, ok := collectZaiCreditBuckets(resp.Data.Limits, time.Now().UTC())
	if !ok {
		return ProviderBalance{Configured: true, Error: "no credit quota"}
	}

	// Primary (display headline) bucket: prefer weekly, else 5-hour.
	primary := limits[0]
	for _, l := range limits {
		if l.Label == "Weekly" {
			primary = l
			break
		}
	}
	return ProviderBalance{
		Configured:   true,
		Amount:       &primary.Remaining,
		Currency:     "credits",
		UsagePercent: &primary.Percentage,
		Credits:      limits,
	}
}

type zaiCreditLimit struct {
	Type          string `json:"type"`
	Unit          int    `json:"unit"`
	Number        int    `json:"number"`
	Usage         int    `json:"usage"`
	CurrentValue  int    `json:"currentValue"`
	Remaining     int    `json:"remaining"`
	Percentage    int    `json:"percentage"`
	NextResetTime int64  `json:"nextResetTime"`
}

// collectZaiCreditBuckets maps the Z.AI credit limits to labeled buckets and
// returns them ordered weekly-first when both present (primary headline bucket
// is the weekly allowance). The API carries no explicit window-type field; both
// buckets carry nextResetTime, and the 5-hour bucket's reset always lands within
// 5 hours while the weekly's lands within 7 days, so the reset distance is the
// discriminator (allowance-size heuristics break when the 5-hour usage exceeds
// 2000 on Pro plans).
func collectZaiCreditBuckets(limits []zaiCreditLimit, now time.Time) ([]CreditBucket, bool) {
	var buckets []CreditBucket
	for _, l := range limits {
		if l.Type != "CREDIT_LIMIT" {
			continue
		}
		label := "Weekly"
		if l.NextResetTime > 0 {
			reset := time.UnixMilli(l.NextResetTime)
			if reset.After(now) && reset.Sub(now) <= 5*time.Hour+time.Minute {
				label = "5-Hour"
			}
		}
		buckets = append(buckets, CreditBucket{
			Label:       label,
			Remaining:   float64(l.Remaining),
			Percentage:  float64(l.Percentage),
			NextResetMs: l.NextResetTime,
		})
	}
	if len(buckets) == 0 {
		return nil, false
	}
	// Weekly first when present.
	for i := 1; i < len(buckets); i++ {
		if buckets[i].Label == "Weekly" {
			buckets[0], buckets[i] = buckets[i], buckets[0]
			break
		}
	}
	return buckets, true
}

func getJSON(client *http.Client, url, bearer string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
