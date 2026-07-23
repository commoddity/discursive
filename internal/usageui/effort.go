package usageui

import (
	"encoding/json"
	"net/http"

	"github.com/commoddity/discursive/internal/config"
)

// ReasoningEffortModelDTO is one model row for the dashboard.
type ReasoningEffortModelDTO struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Options []string `json:"options"`
	Effort  string   `json:"effort"`
}

// ReasoningEffortProviderDTO groups models under a provider.
type ReasoningEffortProviderDTO struct {
	ID     string                    `json:"id"`
	Label  string                    `json:"label"`
	Models []ReasoningEffortModelDTO `json:"models"`
}

// ReasoningEffortResponse is GET /api/reasoning-effort.
type ReasoningEffortResponse struct {
	Providers []ReasoningEffortProviderDTO `json:"providers"`
}

func (s *Server) handleReasoningEffort(w http.ResponseWriter, r *http.Request) {
	if s.live == nil {
		http.Error(w, "settings not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeReasoningEffort(w)
	case http.MethodPut, http.MethodPost:
		s.saveReasoningEffort(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeReasoningEffort(w http.ResponseWriter) {
	efforts := s.live.EffortMap()
	byProvider := map[config.Provider]*ReasoningEffortProviderDTO{}
	var order []config.Provider
	for _, spec := range config.ReasoningEffortCatalog() {
		p, ok := byProvider[spec.Provider]
		if !ok {
			p = &ReasoningEffortProviderDTO{
				ID:    string(spec.Provider),
				Label: providerLabel(spec.Provider),
			}
			byProvider[spec.Provider] = p
			order = append(order, spec.Provider)
		}
		effort := efforts[spec.Model]
		if effort == "" {
			effort = spec.Default
		}
		p.Models = append(p.Models, ReasoningEffortModelDTO{
			ID:      spec.Model,
			Label:   spec.Label,
			Options: append([]string(nil), spec.Options...),
			Effort:  effort,
		})
	}
	out := ReasoningEffortResponse{Providers: make([]ReasoningEffortProviderDTO, 0, len(order))}
	for _, id := range order {
		out.Providers = append(out.Providers, *byProvider[id])
	}
	writeJSON(w, out)
}

func (s *Server) saveReasoningEffort(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := s.live.SetReasoningEffort(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeReasoningEffort(w)
}

func providerLabel(p config.Provider) string {
	switch p {
	case config.ProviderMoonshot:
		return "Moonshot (Kimi)"
	case config.ProviderDeepSeek:
		return "DeepSeek"
	case config.ProviderThaura:
		return "Thaura"
	default:
		return string(p)
	}
}
