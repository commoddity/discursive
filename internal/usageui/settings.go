package usageui

import (
	"encoding/json"
	"net/http"
)

// GatewaySettingsDTO is the GET/PUT shape for /api/settings.
type GatewaySettingsDTO struct {
	ToolCompressionEnabled bool `json:"toolCompressionEnabled"`
}

// handleSettings exposes the live gateway toggles (compression/verbosity) so
// operators can switch them on/off at runtime without restarting the binary.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if s.live == nil {
		http.Error(w, "settings not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.writeSettings(w)
	case http.MethodPut, http.MethodPost:
		s.saveSettings(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeSettings(w http.ResponseWriter) {
	writeJSON(w, GatewaySettingsDTO{
		ToolCompressionEnabled: s.live.ToolCompressionEnabled(),
	})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	var dto GatewaySettingsDTO
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&dto); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := s.live.SetToolCompressionEnabled(dto.ToolCompressionEnabled); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.writeSettings(w)
}
