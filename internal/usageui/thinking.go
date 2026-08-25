package usageui

import (
	"encoding/json"
	"net/http"

	"github.com/commoddity/discursive/internal/config"
)

// ThinkingModelDTO is one model row for the dashboard thinking toggles.
type ThinkingModelDTO struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

// ThinkingResponse is GET /api/thinking-enabled.
type ThinkingResponse struct {
	Models []ThinkingModelDTO `json:"models"`
}

func (s *Server) handleThinkingEnabled(w http.ResponseWriter, r *http.Request) {
	if s.live == nil {
		http.Error(w, "settings not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeThinkingEnabled(w)
	case http.MethodPut, http.MethodPost:
		s.saveThinkingEnabled(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeThinkingEnabled(w http.ResponseWriter) {
	thinking := s.live.ThinkingEnabledMap()
	out := ThinkingResponse{Models: make([]ThinkingModelDTO, 0, len(config.ThinkingEnabledCatalog()))}
	for _, spec := range config.ThinkingEnabledCatalog() {
		if !s.providerActive(spec.Provider) {
			continue
		}
		out.Models = append(out.Models, ThinkingModelDTO{
			ID:       spec.Model,
			Label:    spec.Label,
			Provider: string(spec.Provider),
			Enabled:  thinking[spec.Model],
		})
	}
	writeJSON(w, out)
}

func (s *Server) saveThinkingEnabled(w http.ResponseWriter, r *http.Request) {
	var body map[string]bool
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	for model, enabled := range body {
		if err := s.live.SetThinkingEnabled(model, enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.writeThinkingEnabled(w)
}
