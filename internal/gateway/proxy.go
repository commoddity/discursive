package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/commoddity/discursive/internal/config"
)

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if !requireGatewayKey(w, r, s.cfg.GatewayKey) {
		return
	}
	started := time.Now()
	requestID := newRequestID()

	var body map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "invalid_request_error")
		return
	}

	wantsStream := clientWantsStream(body)

	scfg := s.sanitizeConfig()
	sanitized, err := SanitizeRequest(body, scfg)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	restoreClientStream(sanitized.Body, wantsStream)

	// Apply cache-optimization pass after sanitization.
	OptimizeRequest(sanitized, OptimizeConfig{PromptCacheKey: s.sessionID})

	upstreamKey, err := s.upstreamKey(sanitized.Provider)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	chatURL, err := s.chatURL(sanitized.Provider)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}

	effort := sanitized.Effort
	slog.Debug("proxy: sending upstream",
		"request_id", requestID,
		"provider", string(sanitized.Provider),
		"model", sanitized.Model,
		"effort", effort,
		"stream", wantsStream,
		"url", chatURL,
	)

	resp, err := s.doUpstream(r, chatURL, upstreamKey, sanitized.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err), "upstream_error")
		return
	}

	// Buffer error / non-SSE responses; stream SSE success without buffering.
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && wantsStream && isSSEContentType(resp.Header.Get("Content-Type")) {
		scan := &sseUsageScanner{}
		cerr := copySSE(w, resp.Body, scan)
		_ = resp.Body.Close()
		lat := time.Since(started)
		if scan.found && scan.usage != nil {
			s.recordUsage(sanitized.Provider, sanitized.Model, effort, requestID, lat, *scan.usage)
		}
		if cerr != nil {
			logRequest(requestID, "sse_copy_error", cerr.Error(), "effort", effort)
		}
		if scan.err != nil {
			slog.Error("upstream_error",
				"request_id", requestID,
				"provider", string(sanitized.Provider),
				"model", sanitized.Model,
				"effort", effort,
				"body", scan.err.message,
			)
		}
		logRequest(requestID, "status", resp.StatusCode, "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort, "stream", "passthrough")
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "failed reading upstream body", "upstream_error")
		return
	}

	if isToolCallIDError(resp.StatusCode, string(respBody)) {
		retryBody := cloneMapDeep(sanitized.Body)
		_ = RepairToolCallIDs(retryBody)
		logRequest(requestID, "retry", "tool_call_id", "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort)
		resp2, err := s.doUpstream(r, chatURL, upstreamKey, retryBody)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("upstream retry failed: %v", err), "upstream_error")
			return
		}
		s.finishUpstream(w, resp2, wantsStream, sanitized.Provider, sanitized.Model, effort, requestID, started)
		return
	}

	s.writeBufferedResponse(w, resp.StatusCode, respBody, wantsStream, sanitized.Provider, sanitized.Model, effort, requestID, started)
}

func (s *Server) finishUpstream(w http.ResponseWriter, resp *http.Response, wantsStream bool, provider config.Provider, model, effort, requestID string, started time.Time) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && wantsStream && isSSEContentType(resp.Header.Get("Content-Type")) {
		scan := &sseUsageScanner{}
		_ = copySSE(w, resp.Body, scan)
		lat := time.Since(started)
		if scan.found && scan.usage != nil {
			s.recordUsage(provider, model, effort, requestID, lat, *scan.usage)
		}
		if scan.err != nil {
			slog.Error("upstream_error",
				"request_id", requestID,
				"provider", string(provider),
				"model", model,
				"effort", effort,
				"body", scan.err.message,
			)
		}
		logRequest(requestID, "status", resp.StatusCode, "provider", string(provider), "model", model, "effort", effort, "stream", "passthrough", "retry", true)
		return
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "failed reading upstream body", "upstream_error")
		return
	}
	s.writeBufferedResponse(w, resp.StatusCode, respBody, wantsStream, provider, model, effort, requestID, started)
}

func (s *Server) writeBufferedResponse(w http.ResponseWriter, status int, respBody []byte, wantsStream bool, provider config.Provider, model, effort, requestID string, started time.Time) {
	lat := time.Since(started)
	if status >= 200 && status < 300 {
		var completion map[string]any
		if err := json.Unmarshal(respBody, &completion); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(respBody)
			return
		}
		if u, ok := completion["usage"].(map[string]any); ok {
			s.recordUsage(provider, model, effort, requestID, lat, parseUsageObject(u))
		}
		if wantsStream {
			writeSynthesizedSSE(w, completion)
			logRequest(requestID, "status", status, "provider", string(provider), "model", model, "effort", effort, "stream", "synthesize")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(completion)
		logRequest(requestID, "status", status, "provider", string(provider), "model", model, "effort", effort, "stream", false)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var errObj map[string]any
	if json.Unmarshal(respBody, &errObj) == nil {
		_ = json.NewEncoder(w).Encode(errObj)

		// Always log the full upstream error body at ERROR level.
		slog.Error("upstream_error",
			"request_id", requestID,
			"status", status,
			"provider", string(provider),
			"model", model,
			"effort", effort,
			"body", string(respBody),
		)
	} else {
		slog.Error("upstream_error",
			"request_id", requestID,
			"status", status,
			"provider", string(provider),
			"model", model,
			"effort", effort,
			"body", string(respBody),
		)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": fmt.Sprintf("upstream status %d", status),
				"type":    "upstream_error",
			},
		})
	}
	logRequest(requestID, "status", status, "provider", string(provider), "model", model, "effort", effort, "upstream_error", true)
}

func (s *Server) doUpstream(r *http.Request, url, apiKey string, body map[string]any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return s.client.Do(req)
}
