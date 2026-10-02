package server

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// closedGeneratedFMP4Window proves one normally completed version-two job.
// Every media and initialization reader stays pinned until the shared source
// observation, independent output proofs and final file identities all agree.
// The caller owns admission, producer attachment and initialization history.
func (h *hlsRuntime) closedGeneratedFMP4Window(ctx context.Context, session *hlsSession, source *os.File,
	endpoint transcode.GeneratedSourceEndpointCertificate, plan transcode.Plan, sourceInfo media.Info,
	terminal transcode.Record, input [transcode.MaxHLSRenditions]transcode.GeneratedInputEvidence,
	jobs hlsGeneratedWindowJobs) (transcode.Record, hlsGeneratedWindowBinding, error) {
	var empty hlsGeneratedWindowBinding
	if transcode.ValidatePlan(plan) != nil || plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
		!transcode.GeneratedWindowFMP4NativeClockEligible(plan) {
		return terminal, empty, transcode.ErrInvalidPlan
	}
	if terminal.ID == "" || terminal.State != "completed" || terminal.Spec.Plan != plan ||
		terminal.Spec.Scope != session.key.scope || terminal.Spec.SourceStamp != session.key.stamp {
		return terminal, empty, transcode.ErrOutputUnavailable
	}
	if !sourceInfo.FormatStartKnown || !endpoint.DurationTicksExact || endpoint.DurationTicks != plan.DurationTicks {
		return terminal, empty, transcode.ErrInvalidTimeline
	}
	if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, endpoint); err != nil {
		return terminal, empty, err
	}
	sourceStat, err := source.Stat()
	if err != nil {
		return terminal, empty, err
	}
	count := max(1, plan.HLS.RenditionCount)
	var clocks [transcode.MaxHLSRenditions]transcode.HLSMuxClock
	var outputs [transcode.MaxHLSRenditions]transcode.GeneratedSegmentBounds
	var native [transcode.MaxHLSRenditions]transcode.GeneratedFMP4NativeClock
	var lists [transcode.MaxHLSRenditions]transcode.MediaPlaylist
	var held [transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]*transcode.ReadHandle
	var stats [transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]os.FileInfo
	var identities [transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string
	binding := hlsGeneratedWindowBinding{plan: plan}
	defer func() {
		for _, resources := range held {
			for _, handle := range resources {
				if handle != nil {
					_ = handle.Close()
				}
			}
		}
	}()
	for variant := 0; variant < count; variant++ {
		clocks[variant], err = jobs.HLSClock(ctx, session.key.scope, terminal.ID, variant)
		if err != nil {
			return terminal, empty, err
		}
		playlist, err := h.manager.Open(ctx, session.key.scope, terminal.ID, transcode.HLSPlaylistName(variant, plan.HLS.RenditionCount))
		if err != nil {
			return terminal, empty, err
		}
		data, readErr := io.ReadAll(io.LimitReader(playlist, transcode.MaxPlaylistBytes+1))
		closeErr := playlist.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return terminal, empty, err
		}
		if len(data) > transcode.MaxPlaylistBytes {
			return terminal, empty, transcode.ErrTimelineLimit
		}
		list, err := transcode.ParseMediaPlaylist(data)
		if err != nil {
			return terminal, empty, err
		}
		resource := hlsGeneratedWindowResource{number: plan.HLS.Window.StartNumber, variant: variant}
		mediaName := hlsGeneratedWindowExactResourceName(plan, resource)
		resource.initialization = true
		initName := hlsGeneratedWindowExactResourceName(plan, resource)
		if !list.Ended || len(list.Segments) != 1 || mediaName == "" || initName == "" ||
			list.Segments[0].Name != mediaName || list.InitName != initName {
			return terminal, empty, transcode.ErrInvalidTimeline
		}
		lists[variant] = list
		for index, name := range [hlsGeneratedWindowResourcesPerRendition]string{mediaName, initName} {
			held[variant][index], err = h.manager.Open(ctx, session.key.scope, terminal.ID, name)
			if err != nil {
				return terminal, empty, err
			}
			stat, err := held[variant][index].Stat()
			if err != nil {
				return terminal, empty, err
			}
			identity, err := media.VideoSeekSourceIdentity(stat)
			if err != nil || os.SameFile(stat, sourceStat) {
				return terminal, empty, transcode.ErrInvalidInput
			}
			// Identity strings include change times. Also compare physical file
			// identity so one object cannot fill two roles between observations.
			for _, resources := range stats {
				for _, prior := range resources {
					if prior != nil && os.SameFile(stat, prior) {
						return terminal, empty, transcode.ErrInvalidInput
					}
				}
			}
			stats[variant][index], identities[variant][index] = stat, identity
		}
		binding.artifactNames[variant], binding.artifactIdentities[variant] = mediaName, identities[variant][0]
	}
	artifacts, err := hlsGeneratedWindowArtifactSetFromPlaylists(terminal.ID, plan, lists, identities)
	if err != nil {
		return terminal, empty, err
	}
	// Reuse the bounded publication lane and the caller's trusted foreground or
	// background context for every process-backed observation in this proof.
	select {
	case h.probes <- struct{}{}:
	case <-ctx.Done():
		return terminal, empty, ctx.Err()
	case <-session.ctx.Done():
		return terminal, empty, transcode.ErrJobNotFound
	}
	defer func() { <-h.probes }()
	coverage, err := transcode.MeasureGeneratedSourceRange(ctx, h.server.cfg.FFprobePath, source, plan, sourceInfo.FormatStartTicks)
	if err != nil {
		return terminal, empty, err
	}
	if plan.HLS.Window.EndTicks == endpoint.DurationTicks {
		// An init header's unknown duration and successful bounded probing are
		// not endpoint evidence. Only the actual decoded tail can join the
		// independent finite sample-set certificate at the final source slot.
		tail, err := transcode.MeasureGeneratedMP4SourceTail(ctx, h.server.cfg.FFprobePath, source, plan, endpoint)
		if err != nil {
			return terminal, empty, err
		}
		if tail != coverage {
			return terminal, empty, transcode.ErrInvalidTimeline
		}
	}
	for variant := 0; variant < count; variant++ {
		initialization, segment := held[variant][1].File, held[variant][0].File
		outputs[variant], err = transcode.MeasureGeneratedWindowSegment(ctx, h.server.cfg.FFprobePath, plan, initialization, segment)
		if err != nil {
			return terminal, empty, err
		}
		native[variant], err = transcode.MeasureGeneratedFMP4NativeClock(ctx, plan, initialization, segment)
		if err != nil {
			return terminal, empty, err
		}
	}
	closure, err := transcode.ValidateGeneratedWindowClosure(plan, coverage, input, clocks, outputs, lists)
	if err != nil {
		return terminal, empty, err
	}
	emission, err := transcode.ValidateGeneratedFMP4NativeEmission(plan, clocks, outputs, native)
	if err != nil {
		return terminal, empty, err
	}
	for variant := 0; variant < count; variant++ {
		for index, handle := range held[variant] {
			after, err := handle.Stat()
			if err != nil {
				return terminal, empty, err
			}
			afterIdentity, err := media.VideoSeekSourceIdentity(after)
			if err != nil || afterIdentity != identities[variant][index] || !os.SameFile(stats[variant][index], after) {
				return terminal, empty, transcode.ErrInvalidInput
			}
		}
	}
	if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, endpoint); err != nil {
		return terminal, empty, err
	}
	if err := ctx.Err(); err != nil {
		return terminal, empty, err
	}
	if err := session.ctx.Err(); err != nil {
		return terminal, empty, transcode.ErrJobNotFound
	}
	binding.closure, binding.artifacts, binding.nativeEmission = closure, artifacts, emission
	binding.artifactName, binding.artifactIdentity = binding.artifactNames[0], binding.artifactIdentities[0]
	return terminal, binding, nil
}
