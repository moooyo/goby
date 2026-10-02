package server

import (
	"context"
	"net/url"

	"github.com/moooyo/goby/internal/transcode"
)

const hlsProducerQuery = "GobyHlsProducerId"

// The initialization, media and subtitle URLs in a published graph retain the
// exact producer that established their bytes and timing. A late old URL may
// read retained output, but cannot create a replacement producer on cache miss.
func hlsProducerArtifactURL(session *hlsSession, resource, name, token string, start int64, producerID string) string {
	value := hlsArtifactURL(session, resource, name, token, start)
	if producerID != "" {
		value += "&" + hlsProducerQuery + "=" + url.QueryEscape(producerID)
	}
	return value
}

func (h *hlsRuntime) generatedProducer(ctx context.Context, session *hlsSession, id string) error {
	if id == "" || ctx.Err() != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return transcode.ErrJobNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.key.plan.SourceMode != "" || !transcode.GeneratedHLS(session.key.plan) {
		return transcode.ErrJobNotFound
	}
	if binding, owned := h.generatedWindowBindingLocked(session, id, ""); owned {
		state, err := h.manager.Snapshot(session.key.scope, id)
		if err != nil {
			return err
		}
		if state.State != "completed" || state.Spec.Plan != binding.plan {
			return transcode.ErrOutputUnavailable
		}
		return nil
	}
	for _, producer := range session.producers {
		if producer.id != id || producer.first != -1 || h.producerReleased(producer) {
			continue
		}
		state, err := h.manager.Snapshot(session.key.scope, id)
		if err != nil {
			return err
		}
		if state.State != "queued" && state.State != "running" && state.State != "completed" {
			return transcode.ErrJobFailed
		}
		return nil
	}
	return transcode.ErrJobNotFound
}

// generatedProducerOwnedLocked requires session.mu. A retained reader does not
// authorize inserting new metadata after its registry ownership was evicted.
func (h *hlsRuntime) generatedProducerOwnedLocked(session *hlsSession, id string) bool {
	if session.closed {
		return false
	}
	for _, producer := range session.producers {
		if producer.id == id && producer.first == -1 && !h.producerReleased(producer) {
			return true
		}
	}
	return false
}

func (h *hlsRuntime) generatedProducerArtifact(ctx context.Context, session *hlsSession, id, name string) (*transcode.ReadHandle, error) {
	session.mu.Lock()
	_, private := h.generatedWindowBindingLocked(session, id, "")
	session.mu.Unlock()
	if private {
		return h.generatedWindowExactArtifact(ctx, session, id, name)
	}
	if !hlsPlanArtifact(session.key.plan, name) {
		return nil, transcode.ErrJobNotFound
	}
	if err := h.generatedProducer(ctx, session, id); err != nil {
		return nil, err
	}
	// Every URI advertised by a media playlist already names published bytes.
	// Do not turn a forged, evicted or obsolete URI into future production work.
	handle, err := h.manager.TryOpen(session.key.scope, id, name)
	if err == nil {
		session.mu.Lock()
		active := false
		if !session.closed {
			for _, producer := range session.producers {
				active = active || producer.id == id && !h.producerReleased(producer)
			}
		}
		session.mu.Unlock()
		if !active || ctx.Err() != nil {
			_ = handle.Close()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, transcode.ErrJobNotFound
		}
	}
	return handle, err
}
