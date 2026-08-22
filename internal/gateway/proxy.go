package gateway

import (
	"bytes"
	"context"
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
	routerResult := s.router.ClassifyAndOverride(sanitized.Body, requestID)
	if routerResult.ReleaseLaneSlot != nil {
		defer routerResult.ReleaseLaneSlot()
	}
	if routerResult.OverrideApplied {
		sanitized.Model = routerResult.OverrideModel
		// The override may map to a different provider. Re-resolve the
		// provider from the overridden model so the request is sent to the
		// correct upstream. If the override model is unknown, fall back to a
		// best-effort client error — we must not send a model to the wrong
		// provider's endpoint.
		if route, err := ResolveModel(routerResult.OverrideModel); err != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("override model %q not resolvable: %v", routerResult.OverrideModel, err), "provider", string(sanitized.Provider), "model", routerResult.OverrideModel)
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("override model %q not resolvable: %v", routerResult.OverrideModel, err), "upstream_error")
			return
		} else {
			sanitized.Provider = route.Provider
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
	if s.verbosity != nil && s.verbosityEnabledFor(sanitized.Model) {
		s.verbosity.ApplyRequest(sanitized.Body, sanitized.Model)
	}

	// Pin the output language to English on every Z.AI-bound request (plan):
	// GLM models intermittently drift to Chinese without an explicit pin. Runs
	// after routing so it keys on the FINAL provider.
	if sanitized.Provider == config.ProviderZai {
		s.injectZaiLanguageDirective(sanitized.Body)
	}

	// Reserve a Z.AI plan slot (or overflow to a fast lane) BEFORE resolving
	// the key/URL, because the overflow decision can change the provider and
	// endpoint. Doing this after key/URL resolution would send the overflowed
	// model to the wrong upstream.
	var releaseZaiSlot func()
	if sanitized.Provider == config.ProviderZai {
		chosen, release, ok := s.acquireZaiLaneOrOverflow(sanitized.Model, requestID, nil)
		if !ok {
			logRequest(requestID, "status", http.StatusBadGateway, "error", "zai lane: request cancelled while waiting for slot", "model", sanitized.Model)
			writeJSONError(w, http.StatusBadGateway, "request cancelled while waiting for zai slot", "upstream_error")
			return
		}
		releaseZaiSlot = release
		defer releaseZaiSlot()
		if chosen != sanitized.Model {
			slog.Info("zai_lane: direct request overflow", "request_id", requestID, "from", sanitized.Model, "to", chosen)
			sanitized.Model = chosen
			sanitized.Body["model"] = chosen
			if route, rerr := ResolveModel(chosen); rerr == nil {
				sanitized.Provider = route.Provider
				sanitized.Policy = route.Policy
				cfg := s.sanitizeConfig()
				applyThinkingPolicy(sanitized.Body, route, cfg)
				applyOpenRouterSort(sanitized.Body, route, cfg.OpenRouterSort)
			}
		}
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

	// Z.AI quota-exhaustion fallback: when the plan rejects the request
	// (429 code 1305/1113), retry once on the fallback lane — OpenRouter
	// DeepSeek flash if an OpenRouter key is configured, otherwise direct
	// DeepSeek flash.
	if sanitized.Provider == config.ProviderZai && isZaiQuotaError(resp.StatusCode, respBody) {
		fbModel := s.applyFallback(requestID)
		fbBody := cloneMapDeep(sanitized.Body)
		fbBody["model"] = fbModel
		fbRoute, rerr := ResolveModel(fbModel)
		if rerr != nil {
			logRequest(requestID, "status", resp.StatusCode, "error", fmt.Sprintf("fallback model not resolvable: %v", rerr), "provider", string(sanitized.Provider), "model", fbModel, "effort", effort)
			s.writeBufferedResponse(w, resp.StatusCode, respBody, wantsStream, sanitized.Provider, sanitized.Model, effort, requestID, started)
			return
		}
		cfg := s.sanitizeConfig()
		applyThinkingPolicy(fbBody, fbRoute, cfg)
		applyOpenRouterSort(fbBody, fbRoute, cfg.OpenRouterSort)
		u, uerr := s.chatURL(fbRoute.Provider, fbModel)
		if uerr != nil {
			logRequest(requestID, "status", resp.StatusCode, "error", fmt.Sprintf("fallback url: %v", uerr), "provider", string(fbRoute.Provider), "model", fbModel, "effort", effort)
			s.writeBufferedResponse(w, resp.StatusCode, respBody, wantsStream, sanitized.Provider, sanitized.Model, effort, requestID, started)
			return
		}
		k, kerr := s.upstreamKey(fbRoute.Provider)
		if kerr != nil {
			logRequest(requestID, "status", resp.StatusCode, "error", fmt.Sprintf("fallback key: %v", kerr), "model", fbModel, "effort", effort)
			s.writeBufferedResponse(w, resp.StatusCode, respBody, wantsStream, sanitized.Provider, sanitized.Model, effort, requestID, started)
			return
		}
		logRequest(requestID, "fallback", fbModel, "from", sanitized.Model, "provider", string(sanitized.Provider))
		resp2, ferr := s.doUpstream(r, u, k, fbBody)
		if ferr != nil {
			logRequest(requestID, "status", http.StatusBadGateway, "error", fmt.Sprintf("fallback request failed: %v", ferr), "model", fbModel, "effort", effort)
			writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", ferr), "upstream_error")
			return
		}
		s.finishUpstream(w, resp2, wantsStream, fbRoute.Provider, fbModel, effort, requestID, started)
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

// doUpstreamWithRetry wraps doUpstream with retry logic for Z.AI 429 (code 1305
// "service may be temporarily overloaded") and 1113 ("insufficient balance or
// no resource package") responses. These are concurrency/plan errors that often
// clear on retry with backoff. We retry once with a 1s delay; if the second try
// still fails, we surface the error to the caller (which logs at ERROR and
// returns a 502 to Cursor). Non-Z.AI providers bypass retries and call
// doUpstream directly.
func (s *Server) doUpstreamWithRetry(r *http.Request, url, apiKey string, body map[string]any, requestID, model string, provider config.Provider, effort string) (*http.Response, error) {
	const maxRetries = 1
	// Short backoff: with the concurrency semaphore correctly sized to the
	// plan limit, a 429 here means a transient blip — 1s just compounds
	// latency on every affected turn.
	const retryDelay = 250 * time.Millisecond

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := s.doUpstream(r, url, apiKey, body)
		if err != nil {
			return nil, err
		}
		// Only retry Z.AI 429/1113 errors; everything else returns immediately.
		if provider != config.ProviderZai || attempt == maxRetries {
			return resp, nil
		}
		if resp.StatusCode != 429 {
			return resp, nil
		}
		// Check if this is a retryable Z.AI error (code 1305 or 1113).
		retryable := false
		if bodyBytes, readErr := io.ReadAll(resp.Body); readErr == nil {
			_ = resp.Body.Close()
			var errObj map[string]any
			if json.Unmarshal(bodyBytes, &errObj) == nil {
				if code, ok := errObj["error"].(map[string]any)["code"]; ok {
					if code == float64(1305) || code == float64(1113) {
						retryable = true
						slog.Info("zai_retry: backing off",
							"request_id", requestID,
							"model", model,
							"code", code,
							"attempt", attempt+1,
							"max_attempts", maxRetries+1,
						)
					}
				}
			}
		}
		if !retryable {
			return resp, nil
		}
		// Wait before retry.
		select {
		case <-time.After(retryDelay):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	return nil, fmt.Errorf("retry exhausted for %s", model)
}

// acquireZaiLaneOrOverflow reserves a Z.AI plan slot for a direct (non-
// downgraded) request, or overflows to the fallback lane when both slots are
// busy. Unlike the router downgrade path (which never blocks), direct requests
// get a short grace wait — the user explicitly picked this model, so we give a
// slot a chance to free before rerouting.
//
// Sticky fallback: once a model overflows, subsequent requests for that model
// go straight to the fallback (no grace wait) until the lane has been free for
// zaiStickyFreeStreak consecutive requests. This avoids flip-flopping between
// upstreams (cold prompt cache on both) and keeps quality consistent within a
// turn sequence.
// Returns (chosenModel, release, ok); ok=false means the wait context expired.
func (s *Server) acquireZaiLaneOrOverflow(model, requestID string, nowUTC func() time.Time) (string, func(), bool) {
	if s.zaiSem == nil {
		return model, func() {}, true
	}
	// Sticky: stay on the fallback unless the lane probe lifts stickiness.
	if s.stickyFallbacks.sticky(model) {
		if !s.stickyLaneProbe(model, requestID) {
			fbModel := s.applyFallbackWithModel(model, requestID)
			return fbModel, func() {}, true
		}
		// Stickiness lifted: fall through and take a slot normally.
	}
	// Non-blocking first try.
	select {
	case s.zaiSem <- struct{}{}:
		return model, func() { <-s.zaiSem }, true
	default:
	}
	// Short grace wait — slot may free momentarily.
	ctx, cancel := context.WithTimeout(context.Background(), zaiSlotGraceWait)
	defer cancel()
	select {
	case s.zaiSem <- struct{}{}:
		slog.Info("zai_lane: slot freed within grace wait", "request_id", requestID, "model", model)
		return model, func() { <-s.zaiSem }, true
	case <-ctx.Done():
	}
	// Overflow: preserve "big" model choice for direct requests; pin sticky.
	s.stickyFallbacks.markOverflowed(model)
	fbModel := s.applyFallbackWithModel(model, requestID)
	return fbModel, func() {}, true
}

// zaiSlotGraceWait is how long a direct Z.AI request waits for a plan slot
// before overflowing to the fallback lane. Short enough that a queued request
// is rerouted before the user notices a stall; long enough to absorb bursty
// double-submits (main chat + one subagent finishing).
const zaiSlotGraceWait = 2 * time.Second
