package server

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func (h *hlsRuntime) generatedPlaylistFromCurrentSource(ctx context.Context, session *hlsSession, input *os.File, sourceInfo media.Info, name, resource, token string, start int64, producerID, graphID string, view playback.HLSSubtitleView) ([]byte, error) {
	variant, valid := hlsGeneratedWindowPlaylistVariant(session.key.plan, name)
	if !valid {
		return nil, errHLSRequestInvalid
	}
	if producerID != "" {
		if graphID != "" {
			return nil, errHLSRequestInvalid
		}
		session.mu.Lock()
		_, private := h.generatedWindowBindingLocked(session, producerID, "")
		session.mu.Unlock()
		if private {
			// A private window playlist ends one producer, never the whole
			// presentation. It is not an admitted public playlist resource.
			return nil, errHLSRequestInvalid
		}
		return h.generatedPlaylistFromProducer(ctx, session, input, name, resource, token, start, producerID, view)
	}
	if graphID != "" {
		session.mu.Lock()
		graph := session.windowGraph
		owned := graphID == session.id && graph != nil && !graph.fallback && graph.published && !session.closed
		session.mu.Unlock()
		if !owned {
			return nil, transcode.ErrJobNotFound
		}
		if start < 0 || start >= graph.endpoint.DurationTicks {
			return nil, errHLSRequestInvalid
		}
		if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(input, graph.endpoint); err != nil {
			return nil, err
		}
		return hlsGeneratedWindowManifest(session, graph, resource, token, start, variant)
	}
	if h.generatedWindowsEnabled && session.key.plan.SourceMode == "" {
		graph, err := h.prepareGeneratedWindowGraph(ctx, session, input, sourceInfo, start)
		if err != nil {
			return nil, err
		}
		if graph != nil {
			if start < 0 || start >= graph.endpoint.DurationTicks {
				return nil, errHLSRequestInvalid
			}
			body, err := hlsGeneratedWindowManifest(session, graph, resource, token, start, variant)
			if err != nil {
				return nil, err
			}
			if ctx.Err() != nil || !h.publishGeneratedWindowGraph(session, graph) {
				return nil, transcode.ErrJobNotFound
			}
			return body, nil
		}
	}
	return h.generatedPlaylistFromProducer(ctx, session, input, name, resource, token, start, "", view)
}

func hlsGeneratedWindowPlaylistVariant(plan transcode.Plan, name string) (int, bool) {
	for variant := 0; variant < max(1, plan.HLS.RenditionCount); variant++ {
		if name == transcode.HLSPlaylistName(variant, plan.HLS.RenditionCount) {
			return variant, true
		}
	}
	return 0, false
}

// Direct unbound artifact requests select the existing presentation before a
// logical graph is published. They never turn a ready window graph into a new
// complete-source producer or reuse its preparation as a raw-file admission.
func (h *hlsRuntime) selectManualGeneratedMode(session *hlsSession) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return transcode.ErrJobNotFound
	}
	if graph := session.windowGraph; graph != nil && !graph.fallback {
		return errHLSRequestInvalid
	}
	if session.admission != nil && (session.admission.first == hlsGeneratedGraphPreparation || session.admission.spec.Plan.HLS.Window.RequireInputEvidence) {
		return transcode.ErrBusy
	}
	if h.generatedWindowsEnabled && session.windowGraph == nil {
		session.windowGraph = &hlsGeneratedWindowGraph{fallback: true}
	}
	return nil
}

func (s *Server) serveGeneratedWindowAdmission(w http.ResponseWriter, r *http.Request, session *hlsSession, source *os.File, sourceInfo media.Info, graphID string, number, variant int, resource, token string, start int64) {
	s.serveGeneratedWindowResourceAdmission(w, r, session, source, sourceInfo, graphID, hlsGeneratedWindowResource{number: number, variant: variant}, resource, token, start)
}

func (s *Server) serveGeneratedWindowResourceAdmission(w http.ResponseWriter, r *http.Request, session *hlsSession, source *os.File, sourceInfo media.Info, graphID string, windowResource hlsGeneratedWindowResource, resource, token string, start int64) {
	if graphID == "" {
		s.hlsError(w, r, errHLSRequestInvalid)
		return
	}
	session.mu.Lock()
	graph := session.windowGraph
	owned := !session.closed && session.ctx.Err() == nil && graphID == session.id && graph != nil && !graph.fallback && graph.published &&
		windowResource.number >= 0 && windowResource.number < len(graph.timeline.Segments) &&
		hlsGeneratedWindowResourceName(graph.basePlan, windowResource) != ""
	nativeDuration := int64(0)
	if owned {
		nativeDuration = graph.endpoint.DurationTicks
	}
	session.mu.Unlock()
	if !owned {
		s.hlsError(w, r, transcode.ErrJobNotFound)
		return
	}
	if start < 0 || start >= nativeDuration {
		s.hlsError(w, r, errHLSRequestInvalid)
		return
	}
	if r.Method == http.MethodHead {
		contentType := "video/mp2t"
		if graph.basePlan.HLS.SegmentType == "fmp4" {
			contentType = "video/mp4"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	work, release, err := s.acquireMediaPolicy(r.Context(), principal, session.key.scope)
	if err != nil {
		s.hlsError(w, r, err)
		return
	}
	defer release()
	committed := false
	defer func() {
		if !committed {
			s.failMediaPolicy(work, session.key.scope)
		}
	}()
	binding, pin, err := s.hls.generatedWindowSlotResource(work, session, source, sourceInfo, graphID, windowResource)
	if err != nil {
		s.hlsError(w, r, err)
		return
	}
	defer func() {
		if pin != nil {
			_ = pin.Close()
		}
	}()
	location := hlsProducerArtifactURL(session, resource, binding.artifactName, token, start, binding.producer.id)
	if !validHLSManifestURL(location) {
		s.hlsError(w, r, errInvalidHLSManifest)
		return
	}
	if !s.revalidateGeneratedHLS(work, w, r, session) {
		return
	}
	if err := s.hls.commitGeneratedWindowRedirect(work, session, graphID, binding, pin); err != nil {
		pin = nil // commit consumes ownership even when it rejects the binding.
		s.hlsError(w, r, err)
		return
	}
	pin = nil
	if work.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	session.mu.Lock()
	session.accessed = time.Now()
	session.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Location", location)
	w.WriteHeader(http.StatusTemporaryRedirect)
	committed = true
}
