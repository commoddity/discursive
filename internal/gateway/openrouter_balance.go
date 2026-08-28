package gateway

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/commoddity/discursive/internal/config"
	"github.com/commoddity/discursive/internal/openrouter"
)

const openRouterBalanceTTL = 60 * time.Second

type orBalanceCache struct {
	checkedAt time.Time
	usable    bool
}

// openRouterPeakUsable, when non-nil, overrides the live balance check (tests).
func (s *Server) openRouterPeakRerouteOK() bool {
	if s.openRouterPeakUsable != nil {
		return s.openRouterPeakUsable()
	}
	if s.settings == nil || !s.settings.HasOpenRouterKey() {
		return false
	}

	s.orBalanceMu.Lock()
	if !s.orBalance.checkedAt.IsZero() && time.Since(s.orBalance.checkedAt) < openRouterBalanceTTL {
		usable := s.orBalance.usable
		s.orBalanceMu.Unlock()
		return usable
	}
	s.orBalanceMu.Unlock()

	key, err := s.upstreamKey(config.ProviderOpenRouter)
	if err != nil || key == "" {
		s.setORBalanceCache(false)
		return false
	}

	client := s.openRouterBalanceClient()
	credits, err := openrouter.FetchCredits(client, key)
	usable := err == nil && credits.Remaining > 0
	s.setORBalanceCache(usable)
	if !usable {
		if err != nil {
			slog.Info("openrouter_peak_skip", "reason", "balance_check_failed", "error", err.Error())
		} else {
			slog.Info("openrouter_peak_skip", "reason", "zero_balance")
		}
	}
	return usable
}

func (s *Server) setORBalanceCache(usable bool) {
	s.orBalanceMu.Lock()
	s.orBalance = orBalanceCache{checkedAt: time.Now(), usable: usable}
	s.orBalanceMu.Unlock()
}

func (s *Server) openRouterBalanceClient() *http.Client {
	if s.client != nil && s.client.Transport != nil {
		return &http.Client{Transport: s.client.Transport, Timeout: 8 * time.Second}
	}
	return &http.Client{Timeout: 8 * time.Second}
}
