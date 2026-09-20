package server

import (
	"context"
	"errors"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

// Registry insertion precedes final source validation. Publication can then
// either retire the registered session or make the validation reject its stale
// source. No producer may use a source that slipped between open and retirement.
func (h *hlsRuntime) registerVerified(ctx context.Context, principal identity.Principal, source library.MediaFile, playID string, decision playback.ConversionDecision, start int64) (*hlsSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h == nil || h.verify == nil {
		return nil, library.ErrUnavailable
	}
	session, created, err := h.registerWithStatus(principal, source, playID, decision, start)
	if err != nil {
		return nil, err
	}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	stop := context.AfterFunc(session.ctx, cancel)
	defer stop()
	defer cancel()
	if session.ctx.Err() != nil {
		cancel()
	}
	file, current, err := h.verify(check, principal, session.key.scope, session.key.stamp, session.key.plan)
	if file != nil {
		_ = file.Close()
	}
	if err == nil {
		err = check.Err()
	}
	if err == nil {
		err = session.ctx.Err()
	}
	if err == nil && (file == nil || current.Item.ID != source.Item.ID || current.SourceID != source.SourceID || current.ETag != source.ETag) {
		err = library.ErrSourceChanged
	}
	if err != nil {
		// A transient observation failure rejects this request without erasing
		// a previously accepted session. A new unused registration is disposable.
		if created || permanentHLSError(err) || session.ctx.Err() != nil {
			h.retire(session)
		}
		return nil, err
	}
	return session, nil
}

// The catalog commit has already invalidated old playback rows. Its publication
// barrier stays in place until exact-source readers and producers have retired.
// Other items, credentials and output jobs remain unaffected.
func (s *Server) retireReplacedMediaSource(ctx context.Context, itemID, sourceID string) error {
	if itemID == "" || sourceID == "" {
		return library.ErrInvalidInput
	}
	pending := s.originals.retireSource(itemID, sourceID)
	scopes := make(map[transcode.Scope]struct{})
	var sessions []*hlsSession
	var manager hlsJobs
	if s.hls != nil {
		s.hls.mu.Lock()
		manager = s.hls.manager
		for _, session := range s.hls.sessions {
			if session.key.scope.ItemID == itemID && session.key.scope.SourceID == sourceID {
				sessions = append(sessions, session)
				scopes[session.key.scope] = struct{}{}
			}
		}
		s.hls.mu.Unlock()
	}
	gate := s.playbackPolicyGate()
	gate.mu.Lock()
	var retired []transcode.Scope
	for _, lease := range gate.leases {
		if lease.key.item == itemID && lease.key.source == sourceID {
			scope := gate.retireLocked(lease)
			retired = append(retired, scope)
			if scope.PlaySessionID != "" {
				scopes[scope] = struct{}{}
			}
		}
	}
	gate.mu.Unlock()
	for _, session := range sessions {
		s.hls.retire(session)
	}
	gate.notify(retired)
	var result error
	if joined, ok := manager.(interface {
		CancelSource(context.Context, string, string) error
	}); ok {
		result = joined.CancelSource(ctx, itemID, sourceID)
	} else if len(scopes) != 0 {
		joined, ok := manager.(interface {
			Cancel(context.Context, transcode.Scope) error
		})
		if !ok {
			result = library.ErrUnavailable
		} else {
			for scope := range scopes {
				result = errors.Join(result, joined.Cancel(ctx, scope))
			}
		}
	}
	for _, done := range pending {
		select {
		case <-done:
		case <-ctx.Done():
			return errors.Join(result, ctx.Err())
		}
	}
	return errors.Join(result, ctx.Err())
}
