package gateway

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_ = r
	body := map[string]any{
		"ok": true,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if !requireGatewayKey(w, r, s.cfg.GatewayKey) {
		return
	}
	listed := ListAdvertisedModels()
	data := make([]map[string]any, 0, len(listed))
	for _, m := range listed {
		data = append(data, map[string]any{
			"id":       m.ID,
			"object":   "model",
			"owned_by": "openai",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}
