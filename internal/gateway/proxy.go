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
	started := time.Now()
	requestID := newRequestID()

	var body map[string]any
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(&body); err != nil {
		logRequest(requestID, "status", http.StatusBadRequest, "error", "invalid JSON body")
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body", "invalid_request_error")
		return
	}

	wantsStream := clientWantsStream(body)

	scfg := s.sanitizeConfig()
	sanitized, err := SanitizeRequest(body, scfg)
	if err != nil {
		logRequest(requestID, "status", http.StatusBadRequest, "error", err.Error())
		writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	restoreClientStream(sanitized.Body, wantsStream)

	// Describe images via glm-4.6v for ALL providers before the text model
	// sees them. Graceful: historical images are reused from the durable
	// cache, and any image the vision model cannot describe (rate-limited,
	// missing key, network, rejection) is replaced with a placeholder note so
	// the turn proceeds. ReplaceImages no longer returns a fatal error for an
	// image it cannot describe; the error branch below is defensive only.
	if _, err := s.vision.ReplaceImages(r.Context(), sanitized.Body); err != nil {
		logRequest(requestID, "status", http.StatusBadRequest, "error", "vision", err.Error(), "provider", string(sanitized.Provider), "model", sanitized.Model)
		writeJSONError(w, http.StatusBadRequest, err.Error(), "vision_error")
		return
	}

	// Tool-result compression.
	if s.compressor != nil && s.toolCompressionEnabled() {
		stats, err := s.compressor.Compress(r.Context(), sanitized.Body)
		if err != nil {
			slog.Warn("compress: failed, sending original", "request_id", requestID, "err", err)
		} else if stats.ToolResultsCompressed > 0 {
			slog.Info("compress: tool_results", "request_id", requestID,
				"compressed", stats.ToolResultsCompressed,
				"chars_before", stats.CharsBefore, "chars_after", stats.CharsAfter)
		}
	}

	// Apply cache-optimization pass after sanitization.
	OptimizeRequest(sanitized, OptimizeConfig{PromptCacheKey: s.sessionID})

	// Classify request for subagent-like cheap work and optionally downgrade
	// to a cheaper model (e.g. simple lookups, code search → flash).
	if result := s.router.ClassifyAndOverride(sanitized.Body, requestID); result.OverrideApplied {
		sanitized.Model = result.OverrideModel
		// The override may map to a different provider (e.g. kimi-k3 →
		// deepseek-v4-flash via the default fallback). Re-resolve the
		// provider from the overridden model so the request is sent to the
		// correct upstream. If the override model is unknown, fall back to a
		// best-effort client error — we must not send a model to the wrong
		// provider's endpoint.
		if route, err := ResolveModel(result.OverrideModel); err != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("override model %q not resolvable: %v", result.OverrideModel, err), "provider", string(sanitized.Provider), "model", result.OverrideModel)
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("override model %q not resolvable: %v", result.OverrideModel, err), "upstream_error")
			return
		} else {
			sanitized.Provider = route.Provider
		}
	}

	// Apply verbosity controls (gated per-model on the live map so each model's
	// toggle can be set independently at runtime). Runs AFTER routing/downgrade
	// so the directive + token cap key on the FINAL model actually served (e.g.
	// deepseek-v4-pro downgraded to deepseek-v4-flash for subagent-like work
	// still gets flash's verbosity controls). Configuration only exists for
	// models we target.
	if s.verbosity != nil && s.verbosityEnabledFor(sanitized.Model) {
		s.verbosity.ApplyRequest(sanitized.Body, sanitized.Model)
	}

	upstreamKey, err := s.upstreamKey(sanitized.Provider)
	if err != nil {
		logRequest(requestID, "status", http.StatusBadGateway, "error", err.Error(), "provider", string(sanitized.Provider), "model", sanitized.Model)
		writeJSONError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	chatURL, err := s.chatURL(sanitized.Provider)
	if err != nil {
		logRequest(requestID, "status", http.StatusBadGateway, "error", err.Error(), "provider", string(sanitized.Provider), "model", sanitized.Model)
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
		logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("upstream request failed: %v", err), "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort)
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
		logRequest(requestID, "status", http.StatusBadGateway, "error", "failed reading upstream body", "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort)
		writeJSONError(w, http.StatusBadGateway, "failed reading upstream body", "upstream_error")
		return
	}

	if isToolCallIDError(resp.StatusCode, string(respBody)) {
		retryBody := cloneMapDeep(sanitized.Body)
		_ = RepairToolCallIDs(retryBody)
		logRequest(requestID, "retry", "tool_call_id", "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort)
		resp2, err := s.doUpstream(r, chatURL, upstreamKey, retryBody)
		if err != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("upstream retry failed: %v", err), "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort)
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
		cerr := copySSE(w, resp.Body, scan)
		if cerr != nil {
			logRequest(requestID, "sse_copy_error", cerr.Error(), "effort", effort, "retry", true)
		}
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
		logRequest(requestID, "status", http.StatusBadGateway, "error", "failed reading retry upstream body", "provider", string(provider), "model", model, "effort", effort)
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

	// Always log the full upstream error body at ERROR level.
	slog.Error("upstream_error",
		"request_id", requestID,
		"status", status,
		"provider", string(provider),
		"model", model,
		"effort", effort,
		"body", string(respBody),
	)

	// Surface provider errors verbatim. If the upstream body is valid JSON,
	// pass it through untouched (preserves the provider's error shape). If
	// it's not JSON, wrap the raw body in an OpenAI-shaped envelope so the
	// actual provider message reaches Cursor instead of a generic placeholder.
	var errObj map[string]any
	if json.Unmarshal(respBody, &errObj) == nil {
		_ = json.NewEncoder(w).Encode(errObj)
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": fmt.Sprintf("upstream status %d: %s", status, string(respBody)),
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
