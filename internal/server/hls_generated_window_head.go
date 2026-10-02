package server

import (
	"context"

	"github.com/moooyo/goby/internal/transcode"
)

// HEAD checks existing identity and ownership without producing bytes or
// acquiring a cache reader. Its accepted names mirror the corresponding GET.
func (h *hlsRuntime) checkGeneratedWindowHead(ctx context.Context, session *hlsSession, name, producerID, graphID string, start int64) error {
	if producerID != "" && graphID != "" {
		return errHLSRequestInvalid
	}
	if graphID != "" {
		session.mu.Lock()
		graph := session.windowGraph
		owned := !session.closed && session.ctx.Err() == nil && graphID == session.id && graph != nil && !graph.fallback && graph.published
		if owned {
			if _, playlist := hlsGeneratedWindowPlaylistVariant(session.key.plan, name); !playlist {
				owned = false
			}
		}
		nativeDuration := int64(0)
		if owned {
			nativeDuration = graph.endpoint.DurationTicks
		}
		session.mu.Unlock()
		if !owned {
			return transcode.ErrJobNotFound
		}
		if start < 0 || start >= nativeDuration {
			return errHLSRequestInvalid
		}
		return ctx.Err()
	}
	if producerID != "" {
		session.mu.Lock()
		_, private := h.generatedWindowBindingLocked(session, producerID, "")
		_, exact := h.generatedWindowBindingLocked(session, producerID, name)
		session.mu.Unlock()
		if private && !exact {
			if _, playlist := hlsGeneratedWindowPlaylistVariant(session.key.plan, name); playlist {
				return errHLSRequestInvalid
			}
			return transcode.ErrJobNotFound
		}
		return h.generatedProducer(ctx, session, producerID)
	}
	session.mu.Lock()
	graph := session.windowGraph
	_, playlist := hlsGeneratedWindowPlaylistVariant(session.key.plan, name)
	nativeDuration := int64(0)
	if !session.closed && session.ctx.Err() == nil && graph != nil && !graph.fallback && graph.published && playlist {
		nativeDuration = graph.endpoint.DurationTicks
	}
	session.mu.Unlock()
	if nativeDuration > 0 && (start < 0 || start >= nativeDuration) {
		return errHLSRequestInvalid
	}
	return ctx.Err()
}
