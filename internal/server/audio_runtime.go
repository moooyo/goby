package server

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

type progressiveJobs interface {
	OpenProgressive(context.Context, transcode.Scope, string) (*transcode.ProgressiveReader, error)
}

// progressive consumes input even when opening fails. Its request lease is
// distinct from the manager's file lease: the last departing HTTP consumer
// cancels unfinished production while a completed result remains cacheable.
func (h *hlsRuntime) progressive(ctx context.Context, session *hlsSession, input *os.File) (*transcode.ProgressiveReader, func(), error) {
	jobs, ok := h.manager.(progressiveJobs)
	if !ok || session.key.plan.OutputMode != "progressive" {
		_ = input.Close()
		return nil, nil, transcode.ErrOutputUnavailable
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		_ = input.Close()
		return nil, nil, transcode.ErrJobNotFound
	}
	// Fresh source authority and its retained I/O capability can consult the
	// runtime registry. Do not reverse its runtime -> session lock order while
	// Ensure prepares an input or waits for repository admission.
	session.mu.Unlock()
	record, err := h.manager.Ensure(ctx, transcode.Spec{Scope: session.key.scope,
		SourceStamp: session.key.stamp, Plan: session.key.plan}, input)
	session.mu.Lock()
	if err != nil {
		session.mu.Unlock()
		return nil, nil, err
	}
	if session.closed || ctx.Err() != nil {
		session.mu.Unlock()
		h.cancelUnattachedProducer(session.key.scope, record.ID)
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		return nil, nil, transcode.ErrJobNotFound
	}
	producer, err := h.ownProducer(session.key.scope, record.ID, 0, 0)
	if err != nil {
		session.mu.Unlock()
		h.cancelUnattachedProducer(session.key.scope, record.ID)
		if errors.Is(err, errHLSAdmissionStale) {
			err = transcode.ErrJobNotFound
		}
		return nil, nil, err
	}
	// A progressive revision has one complete output rather than HLS windows.
	// The manager deduplicates concurrent requests for this immutable plan.
	for _, prior := range session.producers {
		h.releaseProducer(session.key.scope, prior)
	}
	session.producers = []hlsProducer{producer}
	session.progressiveReaders++
	session.accessed = time.Now()
	session.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			retire := false
			session.mu.Lock()
			session.progressiveReaders--
			session.accessed = time.Now()
			if session.progressiveReaders == 0 && !session.closed {
				state, err := h.manager.Snapshot(session.key.scope, record.ID)
				if err != nil || state.State != "completed" {
					for _, producer := range session.producers {
						h.releaseProducer(session.key.scope, producer)
					}
					// Claim retirement while holding the same lock used by a
					// joining request. Manager deduplication is already fenced
					// before a replacement registry entry becomes possible.
					session.closed = true
					session.cancel()
					retire = true
				}
			}
			session.mu.Unlock()
			if retire {
				h.retire(session)
			}
		})
	}
	reader, err := jobs.OpenProgressive(ctx, session.key.scope, record.ID)
	if err != nil {
		release()
		return nil, nil, err
	}
	return reader, release, nil
}
