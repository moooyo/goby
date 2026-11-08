package transcode

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// PlaybackBufferHighTicks and PlaybackBufferLowTicks are Goby's internal HLS
// scheduling limits. They are not native FFmpeg or Emby throttle parameters.
const (
	PlaybackBufferHighTicks int64 = 30 * ticksPerSecond
	PlaybackBufferLowTicks  int64 = 20 * ticksPerSecond
)

// PlaybackBufferDemand contains an authorized committed playback position.
// A request for an artifact is separate production demand, never a position.
type PlaybackBufferDemand struct {
	Known         bool
	Revision      int64
	PositionTicks int64
}

type playbackBufferAdmission struct {
	enabled bool
	demand  PlaybackBufferDemand
}

type playbackBufferAdmissionKey struct{}
type playbackBufferControllerKey struct{}

// WithPlaybackBufferThrottling captures scheduling policy independently of a
// job's immutable output plan. Missing policy retains historical execution.
func WithPlaybackBufferThrottling(ctx context.Context, enabled bool, demand PlaybackBufferDemand) context.Context {
	return context.WithValue(ctx, playbackBufferAdmissionKey{}, playbackBufferAdmission{enabled: enabled, demand: demand})
}

func playbackBufferAdmissionFromContext(ctx context.Context) playbackBufferAdmission {
	value, _ := ctx.Value(playbackBufferAdmissionKey{}).(playbackBufferAdmission)
	return value
}

func validPlaybackBufferDemand(value PlaybackBufferDemand) bool {
	return value.Revision >= 0 && value.PositionTicks >= 0 && value.PositionTicks <= maxDurationTicks
}

// PlaybackBufferThrottlingEligible confines the scheduling policy to finite
// file HLS video encoding. A closed proof window completes before publication;
// its next-job admission is controlled by the server rather than stopped here.
func PlaybackBufferThrottlingEligible(plan Plan) bool {
	return plan.OutputMode == "" && plan.SourceMode == "" && plan.DurationTicks > 0 &&
		plan.VideoStreamIndex >= 0 && VideoEncodingSupported(plan.VideoCodec) && !plan.HLS.Window.RequireInputEvidence
}

type playbackBufferController struct {
	mu             sync.Mutex
	plan           Plan
	demand         PlaybackBufferDemand
	outputTicks    int64
	outputEnded    bool
	ready          bool
	pendingOutput  int
	desiredPaused  bool
	actuallyPaused bool
	stopped        bool
	changed        chan struct{}
}

func newPlaybackBufferController(plan Plan, value playbackBufferAdmission) *playbackBufferController {
	if !value.enabled || !PlaybackBufferThrottlingEligible(plan) {
		return nil
	}
	return &playbackBufferController{plan: plan, demand: value.demand, changed: make(chan struct{})}
}

func (c *playbackBufferController) notifyLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *playbackBufferController) updateDemand(value PlaybackBufferDemand) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || value.Revision < c.demand.Revision ||
		value.Revision == c.demand.Revision && (c.demand.Known || !value.Known) {
		return
	}
	c.demand = value
	c.notifyLocked()
}

func (c *playbackBufferController) progress(value Progress) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	changed := false
	if value.OutputTicks > c.outputTicks {
		c.outputTicks = value.OutputTicks
		changed = true
	}
	if value.Ended && !c.outputEnded {
		c.outputEnded = true
		changed = true
	}
	if changed {
		c.notifyLocked()
	}
}

func (c *playbackBufferController) setReady(ready bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopped && c.ready != ready {
		c.ready = ready
		c.notifyLocked()
	}
}

func (c *playbackBufferController) addOutputDemand(delta int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingOutput += delta
	if !c.stopped {
		c.notifyLocked()
	}
}

func (c *playbackBufferController) next() (bool, <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || !c.ready || !c.demand.Known || c.pendingOutput != 0 || c.outputEnded {
		c.desiredPaused = false
	} else {
		end := c.plan.DurationTicks
		if c.plan.EndTicks != 0 {
			end = c.plan.EndTicks
		}
		produced := c.plan.StartTicks + min(c.outputTicks, end-c.plan.StartTicks)
		threshold := PlaybackBufferHighTicks
		if c.desiredPaused {
			threshold = PlaybackBufferLowTicks
		}
		c.desiredPaused = produced-c.demand.PositionTicks > threshold
	}
	return c.desiredPaused, c.changed
}

func (c *playbackBufferController) isPaused() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.actuallyPaused
}

func (c *playbackBufferController) setPaused(paused bool) {
	c.mu.Lock()
	c.actuallyPaused = paused
	c.mu.Unlock()
}

type playbackBufferRuntime struct {
	controller *playbackBufferController
	onResumed  func()
}

func withPlaybackBufferController(ctx context.Context, controller *playbackBufferController, onResumed func()) context.Context {
	if controller == nil {
		return ctx
	}
	return context.WithValue(ctx, playbackBufferControllerKey{}, playbackBufferRuntime{controller: controller, onResumed: onResumed})
}

func playbackBufferRuntimeFromContext(ctx context.Context) playbackBufferRuntime {
	value, _ := ctx.Value(playbackBufferControllerKey{}).(playbackBufferRuntime)
	return value
}

// run applies process signals outside controller and manager locks. Only the
// runner's process-group owner can authorize a signal while its PID is pinned.
func (r playbackBufferRuntime) run(ctx context.Context, signal func(bool) error) error {
	c := r.controller
	paused := false
	defer func() {
		if paused && r.onResumed != nil {
			r.onResumed()
		}
		c.setPaused(false)
		c.mu.Lock()
		c.stopped = true
		c.notifyLocked()
		c.mu.Unlock()
	}()
	for {
		if ctx.Err() != nil {
			return nil
		}
		wanted, changed := c.next()
		if wanted != paused {
			if err := signal(wanted); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, errPlaybackBufferProcessRetired) {
					return nil
				}
				return err
			}
			// Refresh the timeout baseline before exposing an actual resume.
			if !wanted && r.onResumed != nil {
				r.onResumed()
			}
			paused = wanted
			c.setPaused(paused)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-changed:
		}
	}
}

var errPlaybackBufferProcessRetired = errors.New("playback buffer process retired")

// UpdatePlaybackBufferDemand accepts only a complete ownership scope and a
// committed source position. It performs bounded memory updates without I/O.
func (m *Manager) UpdatePlaybackBufferDemand(scope Scope, demand PlaybackBufferDemand) error {
	if !validScope(scope) || !validPlaybackBufferDemand(demand) {
		return ErrInvalidScope
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return ErrManagerClosed
	}
	for _, job := range m.jobs {
		if job.record.Spec.Scope == scope && !job.finished && !job.reclaiming && job.stopCode == "" && !job.productionSealed {
			job.bufferThrottle.updateDemand(demand)
		}
	}
	return nil
}

// beginPlaybackBufferOutputDemand keeps an authorized pending artifact moving.
// The bounded lease ends when the file opens, not when a slow reader closes it.
func (m *Manager) beginPlaybackBufferOutputDemand(scope Scope, id, name string, readiness bool) (func(), error) {
	m.mu.Lock()
	job, err := m.lookupLocked(scope, id)
	if err == nil {
		err = jobError(job)
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if job.bufferThrottle == nil || job.finished || job.productionSealed || readiness && job.ready {
		m.mu.Unlock()
		return nil, nil
	}
	if !readiness && !playbackBufferArtifactAllowed(job.record.Spec.Plan, name) {
		m.mu.Unlock()
		return nil, ErrOutputUnavailable
	}
	if m.bufferWaiters >= m.options.MaxReaders || job.bufferWaiters >= m.options.MaxJobReaders {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.bufferWaiters++
	job.bufferWaiters++
	job.bufferThrottle.addOutputDemand(1)
	m.touchLocked(job)
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.bufferWaiters--
			job.bufferWaiters--
			job.bufferThrottle.addOutputDemand(-1)
			m.mu.Unlock()
		})
	}, nil
}

func playbackBufferArtifactAllowed(plan Plan, name string) bool {
	count := max(1, plan.HLS.RenditionCount)
	for variant := 0; variant < count; variant++ {
		if name == HLSPlaylistName(variant, plan.HLS.RenditionCount) {
			return true
		}
		if plan.HLS.SegmentType == "fmp4" && name == hlsPrefix(variant, plan.HLS.RenditionCount)+"init.mp4" {
			return true
		}
	}
	number, _, extension, valid := generatedHLSSegment(name)
	if !valid {
		return false
	}
	expectedExtension := "ts"
	if plan.HLS.SegmentType == "fmp4" {
		expectedExtension = "m4s"
	}
	if extension != expectedExtension {
		return false
	}
	variantMatched := false
	for variant := 0; variant < count; variant++ {
		variantMatched = variantMatched || name == hlsPrefix(variant, plan.HLS.RenditionCount)+fmt.Sprintf("segment-%06d.%s", number, expectedExtension)
	}
	if !variantMatched {
		return false
	}
	first, last := int64(generatedHLSStartNumber(plan)), int64(0)
	if plan.SegmentMode == "vod" {
		cuts, err := planSegmentTimes(plan)
		if err != nil {
			return false
		}
		last = first + int64(len(cuts))
	} else {
		length := int64(plan.SegmentSeconds) * ticksPerSecond
		if length <= 0 {
			return false
		}
		// A published terminal rounding artifact remains a cache hit. It
		// cannot authorize speculative production outside the nominal range.
		last = first + (generatedHLSEndTicks(plan)-plan.StartTicks+length-1)/length - 1
	}
	return number >= first && number <= last
}
