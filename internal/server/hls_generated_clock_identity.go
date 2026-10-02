package server

import (
	"context"
	"io"
	"os"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsProducerClockEvidence struct {
	producerID string
	origin     int64
	delta      int64
}

type hlsProducerSubtitleClock struct {
	evidence hlsProducerClockEvidence
	known    bool
	pending  chan struct{}
	windows  hlsSubtitleTimeline
}

// Each retained producer keeps its own measured clock and immutable subtitle
// intervals. Looking up an older URL cannot borrow a newer producer's map or
// overwrite the newer subtitle epoch while its response is being prepared.
func (h *hlsRuntime) producerSubtitleClock(ctx context.Context, session *hlsSession, input *os.File, producerIDs ...string) (hlsProducerClockEvidence, error) {
	clocks, ok := h.manager.(hlsClockJobs)
	if !ok || !transcode.HasHLSSubtitles(session.key.plan) {
		return hlsProducerClockEvidence{}, transcode.ErrOutputUnavailable
	}
	first, err := h.generatedArtifact(ctx, session, input, transcode.HLSPlaylistName(0, session.key.plan.HLS.RenditionCount), producerIDs...)
	if err != nil {
		return hlsProducerClockEvidence{}, err
	}
	defer first.Close()
	id := first.EncodingID()
	if id == "" {
		return hlsProducerClockEvidence{}, transcode.ErrOutputUnavailable
	}
	for {
		session.mu.Lock()
		if !h.generatedProducerOwnedLocked(session, id) {
			session.mu.Unlock()
			return hlsProducerClockEvidence{}, transcode.ErrJobNotFound
		}
		if session.subtitleProducerClocks == nil {
			session.subtitleProducerClocks = make(map[string]*hlsProducerSubtitleClock)
		}
		state := session.subtitleProducerClocks[id]
		if state == nil {
			state = &hlsProducerSubtitleClock{}
			session.subtitleProducerClocks[id] = state
		}
		if state.known {
			evidence := state.evidence
			session.mu.Unlock()
			return evidence, nil
		}
		if pending := state.pending; pending != nil {
			session.mu.Unlock()
			select {
			case <-ctx.Done():
				return hlsProducerClockEvidence{}, ctx.Err()
			case <-session.ctx.Done():
				return hlsProducerClockEvidence{}, transcode.ErrJobCancelled
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		state.pending = pending
		session.mu.Unlock()
		evidence, err := h.measureProducerSubtitleClock(ctx, session, clocks, id, first)
		session.mu.Lock()
		if err == nil && (!h.generatedProducerOwnedLocked(session, id) || session.subtitleProducerClocks[id] != state) {
			err = transcode.ErrJobNotFound
		}
		if err == nil {
			state.evidence, state.known = evidence, true
		}
		state.pending = nil
		close(pending)
		session.mu.Unlock()
		return evidence, err
	}
}

func (h *hlsRuntime) measureProducerSubtitleClock(ctx context.Context, session *hlsSession, clocks hlsClockJobs, id string, first *transcode.ReadHandle) (hlsProducerClockEvidence, error) {
	evidence := hlsProducerClockEvidence{producerID: id}
	for index := 0; index < max(1, session.key.plan.HLS.RenditionCount); index++ {
		playlist := first
		if index != 0 {
			var err error
			playlist, err = h.manager.Open(ctx, session.key.scope, id, transcode.HLSPlaylistName(index, session.key.plan.HLS.RenditionCount))
			if err != nil {
				return evidence, err
			}
		}
		data, err := io.ReadAll(io.LimitReader(playlist, transcode.MaxPlaylistBytes+1))
		if index != 0 {
			_ = playlist.Close()
		}
		if err != nil {
			return evidence, err
		}
		list, err := transcode.ParseMediaPlaylist(data)
		if err != nil || list.Sequence != 0 || len(list.Segments) == 0 || list.Segments[0].Number != 0 {
			return evidence, transcode.ErrInvalidTimeline
		}
		before, err := clocks.HLSClock(ctx, session.key.scope, id, index)
		if err != nil {
			return evidence, err
		}
		preTicks, err := before.Ticks()
		if err != nil {
			return evidence, err
		}
		segment, err := h.manager.Open(ctx, session.key.scope, id, list.Segments[0].Name)
		if err != nil {
			return evidence, err
		}
		var initialization *transcode.ReadHandle
		if list.InitName != "" {
			initialization, err = h.manager.Open(ctx, session.key.scope, id, list.InitName)
			if err != nil {
				_ = segment.Close()
				return evidence, err
			}
		}
		var initFile *os.File
		if initialization != nil {
			initFile = initialization.File
		}
		select {
		case h.probes <- struct{}{}:
			var postTicks int64
			postTicks, err = transcode.MeasureHLSMuxClock(ctx, h.server.cfg.FFprobePath, initFile, segment.File, session.key.plan.VideoStreamIndex >= 0)
			<-h.probes
			if err == nil {
				delta := postTicks - preTicks
				if delta < -24*60*60*media.TicksPerSecond || delta > 24*60*60*media.TicksPerSecond {
					err = transcode.ErrInvalidTimeline
				} else if index == 0 {
					evidence.origin, evidence.delta = preTicks, delta
				} else if delta-evidence.delta > 112 || evidence.delta-delta > 112 {
					err = transcode.ErrInvalidTimeline
				}
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
		_ = segment.Close()
		if initialization != nil {
			_ = initialization.Close()
		}
		if err != nil {
			return evidence, err
		}
	}
	return evidence, nil
}
