package server

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"net/http"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) dynamicSessionForSpec(spec transcode.Spec, jobID string) (*dynamicStreamSession, uint64, error) {
	if s.dynamicStreams == nil || s.dynamicStreams.store == nil {
		return nil, 0, dynamicsource.ErrUnavailable
	}
	s.dynamicStreams.mu.Lock()
	if s.dynamicStreams.closing {
		s.dynamicStreams.mu.Unlock()
		return nil, 0, dynamicsource.ErrClosed
	}
	var candidates []*dynamicStreamSession
	for _, candidate := range s.dynamicStreams.sessions {
		if candidate.scope == spec.Scope {
			candidates = append(candidates, candidate)
		}
	}
	s.dynamicStreams.mu.Unlock()
	for _, session := range candidates {
		session.mu.Lock()
		_, planMatches := dynamicEncodingRevision(&spec.Plan, session.key.plan)
		matched := !session.closed && planMatches && session.jobID == jobID && session.input != nil && session.input.Stamp == spec.SourceStamp
		generation := session.generation
		session.mu.Unlock()
		if matched {
			return session, generation, nil
		}
	}
	return nil, 0, dynamicsource.ErrNotFound
}

// The engine lends complete files until this call returns. Publishing copies
// them into charged, independently owned storage before scratch can be reused.
func (s *Server) publishDynamicSegment(ctx context.Context, spec transcode.Spec, jobID string, segment transcode.LiveSegment) error {
	session, generation, err := s.dynamicSessionForSpec(spec, jobID)
	if err != nil {
		return err
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(session.ctx, cancel)
	defer func() { stop(); cancel() }()
	session.mu.Lock()
	principal := session.principal
	first := session.clockGeneration != generation
	expected := session.lastSequence + 1
	if first {
		expected = 0
	}
	delta := session.clockDeltaTicks
	session.mu.Unlock()
	principal, err = s.freshDynamicPrincipal(work, principal)
	if err != nil {
		return err
	}
	if err := s.checkMediaPolicy(principal, session.scope); err != nil {
		return err
	}
	lease, err := s.dynamicSources.Info(work, dynamicSourceOwner(principal), session.key.liveID)
	if err != nil {
		return err
	}
	if lease.Generation != generation || lease.Stamp != spec.SourceStamp {
		return dynamicsource.ErrStaleGeneration
	}
	if !principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &lease.Info}}, spec.Plan) {
		return library.ErrForbidden
	}
	request := (&http.Request{Method: http.MethodGet}).WithContext(context.WithValue(work, principalKey, principal))
	limits, err := s.dynamicLimits(work, principal, session.request, request)
	if err != nil {
		return err
	}
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), session.request, limits)
	if _, matches := dynamicEncodingRevision(conversion.Plan, session.key.plan); err != nil || !matches {
		return library.ErrForbidden
	}
	if segment.Sequence != expected || segment.RenditionCount != max(1, spec.Plan.HLS.RenditionCount) ||
		segment.DurationTicks <= 0 || segment.StartTicks > math.MaxInt64-segment.DurationTicks {
		return transcode.ErrInvalidTimeline
	}
	// A connection generation has one measured clock and subtitle journal.
	// An unannounced timestamp jump must reopen as a new explicit generation;
	// it cannot be silently spliced into a continuous advertised timeline.
	if !first && segment.Discontinuity {
		return transcode.ErrInvalidTimeline
	}
	if first {
		for index := 0; index < segment.RenditionCount; index++ {
			rendition := segment.Renditions[index]
			select {
			case s.hls.probes <- struct{}{}:
			case <-work.Done():
				return work.Err()
			}
			post, probeErr := transcode.MeasureHLSMuxClock(work, s.cfg.FFprobePath, rendition.Init, rendition.Media, spec.Plan.VideoStreamIndex >= 0)
			<-s.hls.probes
			if probeErr != nil {
				return probeErr
			}
			measured := post - rendition.PreMuxClockTicks
			if measured < -24*60*60*media.TicksPerSecond || measured > 24*60*60*media.TicksPerSecond {
				return transcode.ErrInvalidTimeline
			}
			if index == 0 {
				delta = measured
			} else if measured-delta > 112 || delta-measured > 112 {
				return transcode.ErrInvalidTimeline
			}
		}
	}
	if spec.Plan.VideoStreamIndex >= 0 && spec.Plan.VideoCodec == "copy" {
		for index := 0; index < segment.RenditionCount; index++ {
			rendition := segment.Renditions[index]
			select {
			case s.hls.probes <- struct{}{}:
			case <-work.Done():
				return work.Err()
			}
			err := transcode.ValidateLiveVideoRestart(work, s.cfg.FFprobePath, rendition.Init, rendition.Media, transcode.VideoOutputCodec(spec.Plan))
			<-s.hls.probes
			if err != nil {
				return err
			}
		}
	}
	publication := timeshift.Publication{Generation: generation, DurationTicks: segment.DurationTicks}
	for index := 0; index < segment.RenditionCount; index++ {
		rendition := segment.Renditions[index]
		variant := fmt.Sprintf("r%d", index)
		publication.Segments = append(publication.Segments, timeshift.ArtifactInput{VariantID: variant, File: rendition.Media})
		if first && rendition.Init != nil {
			publication.Initializations = append(publication.Initializations, timeshift.ArtifactInput{VariantID: variant, File: rendition.Init})
		}
	}
	snapshot, err := s.dynamicStreams.store.Publish(work, timeshiftScope(session.scope), session.windowID, publication)
	if err != nil {
		return err
	}
	if len(snapshot.Segments) == 0 {
		return transcode.ErrOutputUnavailable
	}
	published := snapshot.Segments[len(snapshot.Segments)-1]
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.generation != generation {
		return dynamicsource.ErrStaleGeneration
	}
	session.clockGeneration, session.clockDeltaTicks, session.lastSequence = generation, delta, segment.Sequence
	if session.subtitles != nil {
		if segment.StartTicks >= 0 && (!spec.Plan.SourceFormatStartKnown || segment.StartTicks >= media.TicksPerSecond) {
			anchor, err := dynamicSourceClockAnchor(generation, segment.StartTicks, spec.Plan)
			if err != nil {
				return err
			}
			if err := session.subtitles.BindSourceAnchor(generation, anchor); err != nil {
				return err
			}
		}
		if err := session.subtitles.PublishMedia(generation, published.Sequence, segment.StartTicks, segment.StartTicks+segment.DurationTicks, delta); err != nil {
			return err
		}
		if err := s.pruneDynamicSubtitles(work, session, snapshot); err != nil {
			return err
		}
	}
	signalDynamicSessionLocked(session)
	return nil
}

func dynamicSourceClockAnchor(generation uint64, sourceTicks int64, plan transcode.Plan) (subtitle.LiveClockAnchor, error) {
	raw := big.NewInt(sourceTicks)
	raw.Sub(raw, big.NewInt(transcode.LiveSourceClockBiasTicks(plan)))
	raw.Mul(raw, big.NewInt(90_000))
	if raw.Sign() >= 0 {
		raw.Add(raw, big.NewInt(media.TicksPerSecond/2))
	} else {
		raw.Sub(raw, big.NewInt(media.TicksPerSecond/2))
	}
	raw.Quo(raw, big.NewInt(media.TicksPerSecond))
	if !raw.IsInt64() || sourceTicks < 0 {
		return subtitle.LiveClockAnchor{}, transcode.ErrInvalidTimeline
	}
	// Use the same additional transport epoch for every observation, including
	// negative decoder priming immediately before source zero.
	raw.Add(raw, big.NewInt(1<<33))
	if !raw.IsInt64() || raw.Sign() < 0 {
		return subtitle.LiveClockAnchor{}, transcode.ErrInvalidTimeline
	}
	return subtitle.LiveClockAnchor{Known: true, Generation: generation, MPEGTS: raw.Int64(), SourceTicks: sourceTicks, MaxDistance90k: 6 * 60 * 60 * 90_000}, nil
}
