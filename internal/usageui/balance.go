package usageui

import (
	"encoding/json"
	"fmt"
	"io"
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
}

// ProviderBalance is one provider's balance for the dashboard.
// Prefer AvailableUSD for threshold coloring. When only a non-USD amount is
// known (DeepSeek CNY without FX), Amount+Currency are set and AvailableUSD is nil.
type ProviderBalance struct {
	Configured   bool     `json:"configured"`
	AvailableUSD *float64 `json:"available_usd"` // nil when unavailable / needs client FX
	Amount       *float64 `json:"amount,omitempty"`
	Currency     string   `json:"currency,omitempty"`
	IsAvailable  *bool    `json:"is_available,omitempty"` // DeepSeek only
	Error        string   `json:"error,omitempty"`
}

// BalancesResponse is the /api/balances payload.
type BalancesResponse struct {
	Moonshot ProviderBalance `json:"moonshot"`
	DeepSeek ProviderBalance `json:"deepseek"`
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

	var moonshot, deepseek ProviderBalance
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		moonshot = fetchMoonshotBalance(client, s.keySource.Moonshot)
	}()
	go func() {
		defer wg.Done()
		deepseek = fetchDeepSeekBalance(client, s.keySource.DeepSeek)
	}()
	wg.Wait()

	writeJSON(w, BalancesResponse{Moonshot: moonshot, DeepSeek: deepseek})
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
	if usdAmt, ok := pickDeepSeekAmount(resp.BalanceInfos, "USD"); ok {
		return ProviderBalance{
			Configured:   true,
			AvailableUSD: &usdAmt,
			Amount:       &usdAmt,
			Currency:     "USD",
			IsAvailable:  &avail,
		}
	}
	cnyAmt, ok := pickDeepSeekAmount(resp.BalanceInfos, "CNY")
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
		}
	}
	usd := cnyAmt / rate
	return ProviderBalance{
		Configured:   true,
		AvailableUSD: &usd,
		Amount:       &cnyAmt,
		Currency:     "CNY",
		IsAvailable:  &avail,
	}
}

func pickDeepSeekAmount(infos []deepSeekBalanceInfo, currency string) (float64, bool) {
	for _, info := range infos {
		if strings.EqualFold(info.Currency, currency) {
			v, err := strconv.ParseFloat(info.TotalBalance, 64)
			if err != nil {
				return 0, false
			}
			return v, true
		}
	}
	return 0, false
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
