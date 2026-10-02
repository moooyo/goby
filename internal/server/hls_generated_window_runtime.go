package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	hlsGeneratedWindowProducer = -2
	hlsGeneratedWindowGrace    = 5 * time.Second
)

type hlsGeneratedWindowGraph struct {
	endpoint       transcode.GeneratedSourceEndpointCertificate
	timeline       transcode.Timeline
	basePlan       transcode.Plan
	fallback       bool
	published      bool
	slots          map[int]hlsGeneratedWindowBinding
	initialization *hlsGeneratedWindowBudgetHistory
}

// A binding contains proof for one closed immutable job, independently of its
// logical admission URI. Its artifact identity fences later cached opens; the
// source certificate and the private worker's outer source fence are separate.
type hlsGeneratedWindowBinding struct {
	producer           hlsProducer
	plan               transcode.Plan
	closure            transcode.GeneratedWindowClosure
	artifactName       string
	artifactIdentity   string
	artifactNames      [transcode.MaxHLSRenditions]string
	artifactIdentities [transcode.MaxHLSRenditions]string
	artifacts          hlsGeneratedWindowArtifactSet
	nativeEmission     transcode.GeneratedFMP4NativeEmission
	redirectedUntil    time.Time
	redirectPin        *transcode.ReadHandle
	redirectPins       [transcode.MaxHLSRenditions]*transcode.ReadHandle
	initializationPins [transcode.MaxHLSRenditions]*transcode.ReadHandle
}

type hlsGeneratedWindowJobs interface {
	GeneratedWindowInputEvidence(context.Context, transcode.Scope, string) ([transcode.MaxHLSRenditions]transcode.GeneratedInputEvidence, error)
	HLSClock(context.Context, transcode.Scope, string, int) (transcode.HLSMuxClock, error)
}

// closedGeneratedWindow owns the descriptor passed to Ensure separately from
// the source descriptor retained across all source and output observations.
// The caller owns the pending-admission gate and orphan cancellation. This
// helper never publishes a producer lease or converts partial output to success.
func (h *hlsRuntime) closedGeneratedWindow(ctx context.Context, session *hlsSession, source *os.File, endpoint transcode.GeneratedSourceEndpointCertificate, plan transcode.Plan, sourceInfo media.Info) (transcode.Record, hlsGeneratedWindowBinding, error) {
	var record transcode.Record
	var empty hlsGeneratedWindowBinding
	jobs, supported := h.manager.(hlsGeneratedWindowJobs)
	if !supported {
		return record, empty, transcode.ErrUnsupportedTimeline
	}
	if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, endpoint); err != nil {
		return record, empty, err
	}
	record, err := h.ensureGeneratedWindowInput(ctx, session, source, plan)
	if err != nil {
		return record, empty, err
	}
	actualPlan := record.Spec.Plan
	semanticPlan := actualPlan
	if plan.ExecutionVersion == 0 && plan.Execution == (transcode.ExecutionOptions{}) {
		semanticPlan.ExecutionVersion, semanticPlan.Execution = 0, transcode.ExecutionOptions{}
	}
	if semanticPlan != plan || record.Spec.Scope != session.key.scope || record.Spec.SourceStamp != session.key.stamp {
		return record, empty, transcode.ErrInvalidPlan
	}
	plan = actualPlan
	input, err := jobs.GeneratedWindowInputEvidence(ctx, session.key.scope, record.ID)
	if err != nil {
		return record, empty, err
	}
	terminal, err := h.manager.Snapshot(session.key.scope, record.ID)
	if err != nil {
		return record, empty, err
	}
	if terminal.State != "completed" || terminal.Spec != record.Spec || terminal.Spec.Plan != plan {
		return record, empty, transcode.ErrOutputUnavailable
	}
	if plan.HLS.SegmentType == "fmp4" {
		return h.closedGeneratedFMP4Window(ctx, session, source, endpoint, plan, sourceInfo, terminal, input, jobs)
	}
	count, valid := hlsGeneratedWindowVariantCount(plan)
	if !valid {
		return record, empty, transcode.ErrInvalidPlan
	}
	var clocks [transcode.MaxHLSRenditions]transcode.HLSMuxClock
	var outputs [transcode.MaxHLSRenditions]transcode.GeneratedSegmentBounds
	var lists [transcode.MaxHLSRenditions]transcode.MediaPlaylist
	var segments [transcode.MaxHLSRenditions]*transcode.ReadHandle
	binding := hlsGeneratedWindowBinding{plan: plan}
	// Every rendition remains pinned through the shared source observation,
	// every output proof and the final identity fence. A closed primary output
	// cannot stand in for a negotiated variant that is absent or still changing.
	defer func() {
		for _, segment := range segments {
			if segment != nil {
				_ = segment.Close()
			}
		}
	}()
	for index := 0; index < count; index++ {
		clocks[index], err = jobs.HLSClock(ctx, session.key.scope, record.ID, index)
		if err != nil {
			return record, empty, err
		}
		playlist, err := h.manager.Open(ctx, session.key.scope, record.ID, transcode.HLSPlaylistName(index, plan.HLS.RenditionCount))
		if err != nil {
			return record, empty, err
		}
		data, readErr := io.ReadAll(io.LimitReader(playlist, transcode.MaxPlaylistBytes+1))
		closeErr := playlist.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return record, empty, err
		}
		list, err := transcode.ParseMediaPlaylist(data)
		if err != nil || !list.Ended || !hlsGeneratedWindowVariantArtifact(plan, list, index) {
			return record, empty, transcode.ErrInvalidTimeline
		}
		lists[index] = list
		segments[index], err = h.manager.Open(ctx, session.key.scope, record.ID, list.Segments[0].Name)
		if err != nil {
			return record, empty, err
		}
		stat, err := segments[index].Stat()
		if err != nil {
			return record, empty, err
		}
		identity, err := media.VideoSeekSourceIdentity(stat)
		if err != nil {
			return record, empty, transcode.ErrInvalidInput
		}
		binding.artifactNames[index], binding.artifactIdentities[index] = list.Segments[0].Name, identity
	}
	// Publication probes remain bounded independently of HTTP request slots.
	// Their process permits retain the trusted foreground/background lane.
	select {
	case h.probes <- struct{}{}:
	case <-ctx.Done():
		return record, empty, ctx.Err()
	case <-session.ctx.Done():
		return record, empty, transcode.ErrJobNotFound
	}
	defer func() { <-h.probes }()
	coverage, err := transcode.MeasureGeneratedSourceRange(ctx, h.server.cfg.FFprobePath, source, plan, sourceInfo.FormatStartTicks)
	if err != nil {
		return record, empty, err
	}
	for index := 0; index < count; index++ {
		outputs[index], err = transcode.MeasureGeneratedWindowSegment(ctx, h.server.cfg.FFprobePath, plan, nil, segments[index].File)
		if err != nil {
			return record, empty, err
		}
	}
	closure, err := transcode.ValidateGeneratedWindowClosure(plan, coverage, input, clocks, outputs, lists)
	if err != nil {
		return record, empty, err
	}
	for index := 0; index < count; index++ {
		after, err := segments[index].Stat()
		if err != nil {
			return record, empty, err
		}
		afterIdentity, err := media.VideoSeekSourceIdentity(after)
		if err != nil || afterIdentity != binding.artifactIdentities[index] {
			return record, empty, transcode.ErrInvalidInput
		}
	}
	if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, endpoint); err != nil {
		return record, empty, err
	}
	if err := ctx.Err(); err != nil {
		return record, empty, err
	}
	binding.closure = closure
	binding.artifactName, binding.artifactIdentity = binding.artifactNames[0], binding.artifactIdentities[0]
	return terminal, binding, nil
}

func hlsGeneratedWindowPlaylistArtifact(plan transcode.Plan, list transcode.MediaPlaylist) bool {
	return hlsGeneratedWindowVariantArtifact(plan, list, 0)
}

func hlsGeneratedWindowVariantCount(plan transcode.Plan) (int, bool) {
	count := plan.HLS.RenditionCount
	if count < 0 || count == 1 || count > transcode.MaxHLSRenditions ||
		plan.HLS.SegmentType != "mpegts" && (plan.HLS.SegmentType != "fmp4" || !transcode.GeneratedWindowFMP4NativeClockEligible(plan)) {
		return 0, false
	}
	return max(1, count), true
}

func hlsGeneratedWindowVariantArtifact(plan transcode.Plan, list transcode.MediaPlaylist, variant int) bool {
	count, valid := hlsGeneratedWindowVariantCount(plan)
	if !valid || variant < 0 || variant >= count || plan.HLS.Window.StartNumber < 0 || plan.HLS.Window.StartNumber >= transcode.MaxPlaylistSegments ||
		len(list.Segments) != 1 || list.InitName != "" {
		return false
	}
	prefix := ""
	if plan.HLS.RenditionCount > 0 {
		prefix = fmt.Sprintf("v%d-", variant)
	}
	return list.Segments[0].Name == fmt.Sprintf("%ssegment-%06d.ts", prefix, plan.HLS.Window.StartNumber)
}

// hlsGeneratedWindowBindingVariant retains the entire closure while selecting
// the one exact artifact identity authorized for a requested rendition. The
// selected copy never lends a redirect pin owned by the registry.
func hlsGeneratedWindowBindingVariant(binding hlsGeneratedWindowBinding, variant int) (hlsGeneratedWindowBinding, bool) {
	if binding.plan.HLS.SegmentType == "fmp4" {
		return hlsGeneratedWindowBindingResource(binding, hlsGeneratedWindowResource{number: planNumber(binding.plan), variant: variant})
	}
	count, valid := hlsGeneratedWindowVariantCount(binding.plan)
	if !valid || variant < 0 || variant >= count {
		return hlsGeneratedWindowBinding{}, false
	}
	name, identity := binding.artifactNames[variant], binding.artifactIdentities[variant]
	if identity == "" || !hlsGeneratedWindowVariantArtifact(binding.plan, transcode.MediaPlaylist{Segments: []transcode.MediaSegment{{Name: name}}}, variant) {
		return hlsGeneratedWindowBinding{}, false
	}
	binding.artifactName, binding.artifactIdentity = name, identity
	binding.redirectPin = nil
	binding.redirectPins = [transcode.MaxHLSRenditions]*transcode.ReadHandle{}
	binding.initializationPins = [transcode.MaxHLSRenditions]*transcode.ReadHandle{}
	return binding, true
}

// MAP and media share the already proved producer, but each role owns its
// separate canonical artifact identity. Neither selection starts production.
func hlsGeneratedWindowBindingResource(binding hlsGeneratedWindowBinding, resource hlsGeneratedWindowResource) (hlsGeneratedWindowBinding, bool) {
	if !resource.initialization && binding.plan.HLS.SegmentType == "mpegts" {
		if resource.number != planNumber(binding.plan) {
			return hlsGeneratedWindowBinding{}, false
		}
		return hlsGeneratedWindowBindingVariant(binding, resource.variant)
	}
	if binding.plan.HLS.SegmentType != "fmp4" || binding.closure.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
		binding.plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV2 ||
		!transcode.GeneratedWindowFMP4NativeClockEligible(binding.plan) ||
		binding.closure.StartTicks != binding.plan.StartTicks || binding.closure.EndTicks != binding.plan.HLS.Window.EndTicks ||
		binding.closure.Number != planNumber(binding.plan) || binding.closure.RenditionCount != max(1, binding.plan.HLS.RenditionCount) ||
		binding.nativeEmission.StartTicks != binding.plan.StartTicks || binding.nativeEmission.EndTicks != binding.plan.HLS.Window.EndTicks ||
		binding.nativeEmission.RenditionCount != max(1, binding.plan.HLS.RenditionCount) {
		return hlsGeneratedWindowBinding{}, false
	}
	for index := 0; index < transcode.MaxHLSRenditions; index++ {
		initSHA, mediaSHA := binding.nativeEmission.InitializationSHA256[index], binding.nativeEmission.SegmentSHA256[index]
		if index >= binding.nativeEmission.RenditionCount {
			if initSHA != ([32]byte{}) || mediaSHA != ([32]byte{}) || binding.nativeEmission.TrackIDs[index] != 0 ||
				binding.nativeEmission.MediaTimeScales[index] != 0 || binding.closure.Output[index] != (transcode.GeneratedSegmentBounds{}) ||
				binding.closure.Input[index] != (transcode.GeneratedInputEvidence{}) {
				return hlsGeneratedWindowBinding{}, false
			}
		} else if binding.nativeEmission.TrackIDs[index] != 1 || binding.nativeEmission.MediaTimeScales[index] <= 0 ||
			initSHA == ([32]byte{}) || mediaSHA == ([32]byte{}) || binding.closure.Output[index].InitializationSHA256 != initSHA ||
			binding.closure.Output[index].SegmentSHA256 != mediaSHA {
			return hlsGeneratedWindowBinding{}, false
		}
	}
	artifact, valid := binding.artifacts.selectResource(binding.producer.id, binding.plan, resource)
	if !valid {
		return hlsGeneratedWindowBinding{}, false
	}
	binding.artifactName, binding.artifactIdentity = artifact.name, artifact.identity
	binding.redirectPin = nil
	binding.redirectPins = [transcode.MaxHLSRenditions]*transcode.ReadHandle{}
	binding.initializationPins = [transcode.MaxHLSRenditions]*transcode.ReadHandle{}
	return binding, true
}

// generatedWindowBindingLocked requires session.mu. Only a retained complete
// closure and its exact ownership lease authorize the corresponding raw bytes.
func (h *hlsRuntime) generatedWindowBindingLocked(session *hlsSession, id, name string) (hlsGeneratedWindowBinding, bool) {
	graph := session.windowGraph
	if session.closed || graph == nil || graph.fallback || !graph.published {
		return hlsGeneratedWindowBinding{}, false
	}
	for _, binding := range graph.slots {
		if binding.producer.id != id || binding.producer.first != hlsGeneratedWindowProducer || h.producerReleased(binding.producer) {
			continue
		}
		count, valid := hlsGeneratedWindowVariantCount(binding.plan)
		if !valid {
			continue
		}
		if binding.plan.HLS.SegmentType == "fmp4" {
			resource, found := binding.artifacts.resourceForExactName(id, binding.plan, name)
			if name == "" {
				resource, found = hlsGeneratedWindowResource{number: planNumber(binding.plan)}, true
			}
			if found {
				if selected, valid := hlsGeneratedWindowBindingResource(binding, resource); valid {
					return selected, true
				}
			}
			continue
		}
		for variant := 0; variant < count; variant++ {
			selected, valid := hlsGeneratedWindowBindingVariant(binding, variant)
			if valid && (name == "" && variant == 0 || name == selected.artifactName) {
				return selected, true
			}
		}
	}
	return hlsGeneratedWindowBinding{}, false
}

func (h *hlsRuntime) generatedWindowExactArtifact(ctx context.Context, session *hlsSession, id, name string) (*transcode.ReadHandle, error) {
	session.mu.Lock()
	binding, owned := h.generatedWindowBindingLocked(session, id, name)
	session.mu.Unlock()
	if !owned {
		return nil, transcode.ErrJobNotFound
	}
	state, err := h.manager.Snapshot(session.key.scope, id)
	if err != nil {
		return nil, err
	}
	if state.State != "completed" || state.Spec.Plan != binding.plan {
		return nil, transcode.ErrOutputUnavailable
	}
	handle, err := h.manager.TryOpen(session.key.scope, id, name)
	if err != nil {
		return nil, err
	}
	stat, err := handle.Stat()
	if err == nil {
		var identity string
		identity, err = media.VideoSeekSourceIdentity(stat)
		if err == nil && identity != binding.artifactIdentity {
			err = transcode.ErrInvalidInput
		}
	}
	session.mu.Lock()
	current, owned := h.generatedWindowBindingLocked(session, id, name)
	owned = owned && current.artifactIdentity == binding.artifactIdentity && current.closure == binding.closure &&
		current.artifacts == binding.artifacts && current.nativeEmission == binding.nativeEmission
	session.mu.Unlock()
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && !owned {
		err = transcode.ErrJobNotFound
	}
	if err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

// removeGeneratedWindowBindingsLocked runs with session.mu after producer
// eviction. Proof metadata shares the bounded artifact-lease lifetime.
func (h *hlsRuntime) removeGeneratedWindowBindingsLocked(session *hlsSession) {
	if session.windowGraph == nil {
		return
	}
	for number, binding := range session.windowGraph.slots {
		if h.producerReleased(binding.producer) {
			for _, pin := range binding.initializationPins {
				if pin != nil {
					h.releaseGeneratedWindowPinLocked(session, pin)
				}
			}
			for _, pin := range binding.redirectPins {
				if pin != nil {
					h.releaseGeneratedWindowPinLocked(session, pin)
				}
			}
			if binding.redirectPin != nil {
				h.releaseGeneratedWindowPinLocked(session, binding.redirectPin)
			}
			delete(session.windowGraph.slots, number)
		}
	}
}
