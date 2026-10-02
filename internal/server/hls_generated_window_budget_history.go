package server

import (
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// The presentation owns one full-timeline reservation independently of producer
// and cache lifetimes. The arena is the only retained digest backing storage.
// The graph owner holds session.mu for every history operation.
type hlsGeneratedWindowBudgetHistory struct {
	graphID            string
	sourceIdentity     string
	basePlan           transcode.Plan
	nativeClockVersion uint8
	lease              *hlsInitializationReservation
}

func newHLSGeneratedWindowBudgetHistory(budget *hlsInitializationBudget, graphID, sourceIdentity string, basePlan transcode.Plan,
	nativeClockVersion uint8, entries int) (*hlsGeneratedWindowBudgetHistory, error) {
	validated, err := newHLSGeneratedWindowInitializationHistory(graphID, sourceIdentity, basePlan, nativeClockVersion)
	if err != nil {
		return nil, err
	}
	period := int64(basePlan.SegmentSeconds) * media.TicksPerSecond
	if nativeClockVersion != transcode.GeneratedWindowNativeClockV2 || entries < 1 || entries > transcode.MaxTimelineSegments ||
		int64(entries) != (basePlan.DurationTicks-1)/period+1 {
		return nil, transcode.ErrInvalidTimeline
	}
	lease, err := budget.reserve(entries)
	if err != nil {
		return nil, err
	}
	return &hlsGeneratedWindowBudgetHistory{graphID: validated.graphID, sourceIdentity: validated.sourceIdentity,
		basePlan: validated.basePlan, nativeClockVersion: validated.nativeClockVersion, lease: lease}, nil
}

// Remember follows independent closure, actual byte measurement and the final
// identity fence. Structural artifacts cannot establish any of those facts.
func (history *hlsGeneratedWindowBudgetHistory) validateAndRemember(graphID, sourceIdentity string, artifacts hlsGeneratedWindowArtifactSet,
	digests [transcode.MaxHLSRenditions][32]byte) error {
	if history == nil || history.lease == nil {
		return transcode.ErrJobNotFound
	}
	if graphID == "" || graphID != history.graphID || sourceIdentity != history.sourceIdentity ||
		!artifacts.valid(artifacts.producerID, artifacts.plan) || artifacts.plan.HLS.SegmentType != "fmp4" ||
		artifacts.plan.HLS.Window.NativeClockVersion != history.nativeClockVersion {
		return transcode.ErrInvalidTimeline
	}
	plan := artifacts.plan
	base := plan
	base.StartTicks, base.HLS.Window = 0, transcode.HLSWindow{}
	// An unspecified execution profile is captured by the manager. A supplied
	// profile remains part of the immutable presentation contract.
	if history.basePlan.ExecutionVersion == 0 && history.basePlan.Execution == (transcode.ExecutionOptions{}) {
		base.ExecutionVersion, base.Execution = 0, transcode.ExecutionOptions{}
	}
	if base != history.basePlan || plan.StartTicks < 0 || plan.HLS.Window.StartNumber < 0 ||
		plan.HLS.Window.StartNumber >= history.lease.entries || plan.HLS.Window.StartNumber >= transcode.MaxTimelineSegments {
		return transcode.ErrInvalidTimeline
	}
	period := int64(base.SegmentSeconds) * media.TicksPerSecond
	start := int64(plan.HLS.Window.StartNumber) * period
	if start >= base.DurationTicks || plan.StartTicks != start || plan.HLS.Window.EndTicks != start+min(period, base.DurationTicks-start) {
		return transcode.ErrInvalidTimeline
	}
	count := max(1, base.HLS.RenditionCount)
	for variant, digest := range digests {
		if variant < count && digest == ([32]byte{}) || variant >= count && digest != ([32]byte{}) {
			return transcode.ErrInvalidTimeline
		}
	}
	return history.lease.remember(plan.HLS.Window.StartNumber, count, digests)
}

func (history *hlsGeneratedWindowBudgetHistory) release() {
	if history != nil {
		history.lease.release()
	}
}

// rememberBinding requires session.mu and precedes producer ownership and graph
// publication. Compact proof consistency never substitutes for the publisher's
// independent source, input, native fragment or final file identity checks.
func (graph *hlsGeneratedWindowGraph) rememberBinding(graphID string, binding hlsGeneratedWindowBinding) error {
	if binding.plan.HLS.SegmentType == "mpegts" {
		return nil
	}
	if graph == nil || graph.initialization == nil {
		return transcode.ErrJobNotFound
	}
	plan, closure, emission := binding.plan, binding.closure, binding.nativeEmission
	count := max(1, plan.HLS.RenditionCount)
	if plan.HLS.SegmentType != "fmp4" || plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
		!transcode.GeneratedWindowFMP4NativeClockEligible(plan) ||
		!plan.HLS.Window.RequireInputEvidence || closure.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
		closure.StartTicks != plan.StartTicks || closure.EndTicks != plan.HLS.Window.EndTicks ||
		closure.Number != plan.HLS.Window.StartNumber || closure.RenditionCount != count ||
		emission.StartTicks != plan.StartTicks || emission.EndTicks != plan.HLS.Window.EndTicks || emission.RenditionCount != count ||
		!binding.artifacts.valid(binding.artifacts.producerID, plan) {
		return transcode.ErrInvalidTimeline
	}
	for variant := 0; variant < transcode.MaxHLSRenditions; variant++ {
		initSHA, mediaSHA := emission.InitializationSHA256[variant], emission.SegmentSHA256[variant]
		if variant >= count {
			if initSHA != ([32]byte{}) || mediaSHA != ([32]byte{}) || emission.TrackIDs[variant] != 0 || emission.MediaTimeScales[variant] != 0 ||
				closure.Input[variant] != (transcode.GeneratedInputEvidence{}) || closure.Output[variant] != (transcode.GeneratedSegmentBounds{}) {
				return transcode.ErrInvalidTimeline
			}
			continue
		}
		if emission.TrackIDs[variant] != 1 || emission.MediaTimeScales[variant] <= 0 || initSHA == ([32]byte{}) || mediaSHA == ([32]byte{}) ||
			closure.Output[variant].InitializationSHA256 != initSHA || closure.Output[variant].SegmentSHA256 != mediaSHA {
			return transcode.ErrInvalidTimeline
		}
	}
	return graph.initialization.validateAndRemember(graphID, graph.endpoint.SourceIdentity, binding.artifacts, emission.InitializationSHA256)
}
