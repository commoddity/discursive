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
	"github.com/commoddity/discursive/internal/gateway/vision"
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

	if s.settings != nil && !s.settings.IsProviderActive(sanitized.Provider) {
		logRequest(requestID, "status", http.StatusBadRequest, "error", "provider inactive", "provider", string(sanitized.Provider), "model", sanitized.Model)
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("provider %q is not configured (no API key)", sanitized.Provider), "invalid_request_error")
		return
	}

	visionModel := config.VisionModelFor(sanitized.Provider)
	var visionURL string
	if s.cfg.VisionChatURLOverride != "" {
		visionURL = s.cfg.VisionChatURLOverride
	} else {
		var vErr error
		visionURL, vErr = s.chatURL(sanitized.Provider, visionModel)
		if vErr != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", vErr.Error(), "provider", string(sanitized.Provider), "model", visionModel)
			writeJSONError(w, http.StatusBadGateway, vErr.Error(), "upstream_error")
			return
		}
	}
	if _, err := s.vision.ReplaceImages(r.Context(), sanitized.Body, vision.Request{
		Provider: sanitized.Provider,
		Model:    visionModel,
		ChatURL:  visionURL,
		GetKey: func() (string, bool) {
			k, kerr := s.upstreamKey(sanitized.Provider)
			if kerr != nil || k == "" {
				return "", false
			}
			return k, true
		},
	}); err != nil {
		logRequest(requestID, "status", http.StatusBadRequest, "error", "vision", err.Error(), "provider", string(sanitized.Provider), "model", sanitized.Model)
		writeJSONError(w, http.StatusBadRequest, err.Error(), "vision_error")
		return
	}

	// Tool-result compression.
	if s.compressor != nil && s.toolCompressionEnabled() {
		cctx, cerr := s.compressContext(sanitized.Provider, time.Now())
		if cerr != nil {
			slog.Warn("compress: skipped, no context", "request_id", requestID, "err", cerr)
		} else {
			stats, err := s.compressor.Compress(r.Context(), sanitized.Body, cctx)
			if err != nil {
				slog.Warn("compress: failed, sending original", "request_id", requestID, "err", err)
			} else if stats.ToolResultsCompressed > 0 {
				slog.Info("compress: tool_results", "request_id", requestID,
					"compressed", stats.ToolResultsCompressed,
					"chars_before", stats.CharsBefore, "chars_after", stats.CharsAfter)
			}
		}
	}

	// Apply cache-optimization pass after sanitization.
	OptimizeRequest(sanitized, OptimizeConfig{PromptCacheKey: s.sessionID})

	// Classify request for subagent-like cheap work and optionally downgrade
	// to a cheaper model (e.g. simple lookups, code search → flash).
	routerResult := s.router.ClassifyAndOverride(sanitized.Body, requestID)
	if routerResult.OverrideApplied {
		sanitized.Model = routerResult.OverrideModel
		sanitized.Body["model"] = routerResult.OverrideModel
		if route, err := ResolveModel(routerResult.OverrideModel); err != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("override model %q not resolvable: %v", routerResult.OverrideModel, err), "provider", string(sanitized.Provider), "model", routerResult.OverrideModel)
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("override model %q not resolvable: %v", routerResult.OverrideModel, err), "upstream_error")
			return
		} else {
			sanitized.Provider = route.Provider
			sanitized.Policy = route.Policy
		}
	}

	// Peak reroute: when the requested model's provider is in peak hours and
	// an OpenRouter key is configured, route to the appropriate OpenRouter
	// DeepSeek model. Runs after downgrade so cheap-class traffic already has
	// its final model; peak reroute only applies to non-downgraded traffic.
	if newModel, redirected := s.applyPeakReroute(sanitized.Model, requestID, time.Now()); redirected {
		sanitized.Model = newModel
		sanitized.Body["model"] = newModel
		route, err := ResolveModel(newModel)
		if err != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("peak redirect model %q not resolvable: %v", newModel, err), "model", newModel)
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("peak redirect model %q not resolvable: %v", newModel, err), "upstream_error")
			return
		}
		sanitized.Provider = route.Provider
		sanitized.Policy = route.Policy
		// Re-apply the thinking/sampling policy for the new model so the
		// OpenRouter DeepSeek shape is emitted correctly.
		cfg := s.sanitizeConfig()
		applyThinkingPolicy(sanitized.Body, route, cfg)
		applyOpenRouterSort(sanitized.Body, route, cfg.OpenRouterSort)
	}

	// Thinking-effort coupling: for GLM-4.7-family models (thinking on/off),
	// force thinking OFF on downgrade-safe (mechanical) classes even when the
	// per-model toggle is on, so cheap turns stay fast/cheap while hard turns
	// (editing / complex_reasoning) keep quality. The per-model live toggle is
	// the baseline; this only narrows it downward on mechanical classes.
	if route, err := ResolveModel(sanitized.Model); err == nil && zaiThinkingToggle(route.RealModel) {
		if !thinkingClass(routerResult.RequestClass) {
			sanitized.Body["thinking"] = map[string]any{"type": "disabled"}
		}
	}

	// Apply verbosity controls (gated per-model on the live map so each model's
	// toggle can be set independently at runtime). Runs AFTER routing/downgrade
	// so the directive + token cap key on the FINAL model actually served (e.g.
	// deepseek-v4-pro downgraded to deepseek-v4-flash for subagent-like work
	// still gets flash's verbosity controls). Configuration only exists for
	// models we target.
	if s.verbosity != nil {
		s.verbosity.ApplyRequest(sanitized.Body, sanitized.Model)
	}

	// Pin the output language to English on every Z.AI-bound request (plan):
	// GLM models intermittently drift to Chinese without an explicit pin. Runs
	// after routing so it keys on the FINAL provider.
	if sanitized.Provider == config.ProviderZai {
		s.injectZaiLanguageDirective(sanitized.Body)
	}

	// HARD RULE guard at send time: OpenRouter may only be used while the
	// equivalent real provider is in peak. If any upstream path produced an
	// OpenRouter model off-peak, correct it back to the real model.
	if corrected := s.isPeakAllowedOpenRouter(sanitized.Model, requestID); corrected != sanitized.Model {
		sanitized.Model = corrected
		sanitized.Body["model"] = corrected
		if route, rerr := ResolveModel(corrected); rerr == nil {
			sanitized.Provider = route.Provider
			sanitized.Policy = route.Policy
			cfg := s.sanitizeConfig()
			applyThinkingPolicy(sanitized.Body, route, cfg)
			applyOpenRouterSort(sanitized.Body, route, cfg.OpenRouterSort)
		}
	}

	upstreamKey, err := s.upstreamKey(sanitized.Provider)
	if err != nil {
		logRequest(requestID, "status", http.StatusBadGateway, "error", err.Error(), "provider", string(sanitized.Provider), "model", sanitized.Model)
		writeJSONError(w, http.StatusBadGateway, err.Error(), "upstream_error")
		return
	}
	chatURL, err := s.chatURL(sanitized.Provider, sanitized.Model)
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

	resp, err := s.doUpstreamWithRetry(r, chatURL, upstreamKey, sanitized.Body, requestID, sanitized.Model, sanitized.Provider, effort)
	if err != nil {
		logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("upstream request failed: %v", err), "provider", string(sanitized.Provider), "model", sanitized.Model, "effort", effort)
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err), "upstream_error")
		return
	}
	slog.Debug("proxy: upstream response received",
		"request_id", requestID,
		"provider", string(sanitized.Provider),
		"model", sanitized.Model,
		"status", resp.StatusCode,
	)

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

	// Log the outbound request for debugging flash model issues.
	model := ""
	if m, ok := body["model"]; ok {
		model = fmt.Sprintf("%v", m)
	}
	slog.Debug("doUpstream: sending request",
		"model", model,
		"url", url,
		"body_bytes", len(raw),
	)

	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("doUpstream: request failed",
			"model", model,
			"url", url,
			"error", err,
		)
	}
	return resp, err
}

// doUpstreamWithRetry wraps doUpstream with retry logic for Z.AI HTTP 429
// responses. Transient plan/concurrency limits often clear quickly; we retry up
// to three times with short exponential backoff before returning the 429 to
// Cursor. Non-Z.AI providers bypass retries and call doUpstream directly.
func (s *Server) doUpstreamWithRetry(r *http.Request, url, apiKey string, body map[string]any, requestID, model string, provider config.Provider, effort string) (*http.Response, error) {
	const maxRetries = 3
	const baseDelay = 250 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := s.doUpstream(r, url, apiKey, body)
		if err != nil {
			return nil, err
		}
		if provider != config.ProviderZai || resp.StatusCode != http.StatusTooManyRequests || attempt == maxRetries {
			return resp, nil
		}
		_ = resp.Body.Close()
		delay := baseDelay * time.Duration(1<<attempt)
		slog.Info("zai_retry: backing off",
			"request_id", requestID,
			"model", model,
			"attempt", attempt+1,
			"max_retries", maxRetries,
			"delay", delay,
		)
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	return nil, fmt.Errorf("retry exhausted for %s", model)
}
