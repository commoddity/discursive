// Package openrouter implements OpenRouter-specific helpers shared by the
// gateway and usage dashboard.
//
// Contract: may depend on net/http and encoding/json only; must not import
// gateway, usageui, or config.
package openrouter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const creditsURL = "https://openrouter.ai/api/v1/credits"

// Credits holds the remaining OpenRouter prepaid balance.
type Credits struct {
	Remaining    float64
	TotalCredits float64
	TotalUsage   float64
}

// FetchCredits queries OpenRouter for prepaid credit balance.
func FetchCredits(client *http.Client, apiKey string) (Credits, error) {
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, creditsURL, nil)
	if err != nil {
		return Credits{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return Credits{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Credits{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return Credits{}, fmt.Errorf("unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return Credits{}, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var parsed struct {
		Data struct {
			TotalCredits float64 `json:"total_credits"`
			TotalUsage   float64 `json:"total_usage"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Credits{}, fmt.Errorf("invalid response")
	}
	remaining := parsed.Data.TotalCredits - parsed.Data.TotalUsage
	return Credits{
		Remaining:    remaining,
		TotalCredits: parsed.Data.TotalCredits,
		TotalUsage:   parsed.Data.TotalUsage,
	}, nil
}

// PeakRerouteUsable reports whether peak-hour traffic may route through
// OpenRouter: key configured, fetch succeeded, and remaining balance > 0.
func PeakRerouteUsable(configured bool, remaining float64, errMsg string) bool {
	return configured && errMsg == "" && remaining > 0
}
