package usageui

import (
	"encoding/json"
	"net/http"

	"github.com/commoddity/discursive/internal/config"
)

// VerbosityModelDTO is one model row for the dashboard verbosity toggles.
type VerbosityModelDTO struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

// VerbosityResponse is GET /api/verbosity.
type VerbosityResponse struct {
	Models []VerbosityModelDTO `json:"models"`
}

func (s *Server) handleVerbosity(w http.ResponseWriter, r *http.Request) {
	if s.live == nil {
		http.Error(w, "settings not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeVerbosity(w)
	case http.MethodPut, http.MethodPost:
		s.saveVerbosity(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeVerbosity(w http.ResponseWriter) {
	verbosity := s.live.VerbosityMap()
	out := VerbosityResponse{Models: make([]VerbosityModelDTO, 0, len(config.VerbosityCatalog()))}
	for _, spec := range config.VerbosityCatalog() {
		out.Models = append(out.Models, VerbosityModelDTO{
			ID:       spec.Model,
			Label:    spec.Label,
			Provider: providerLabel(spec.Provider),
			Enabled:  verbosity[spec.Model],
		})
	}
	writeJSON(w, out)
}

func (s *Server) saveVerbosity(w http.ResponseWriter, r *http.Request) {
	var body map[string]bool
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	for model, enabled := range body {
		if err := s.live.SetVerbosity(model, enabled); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	s.writeVerbosity(w)
}
