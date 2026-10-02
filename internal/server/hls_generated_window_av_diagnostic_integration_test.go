//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsGeneratedAVDiagnosticWriteJSON(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return transcode.ErrTimelineLimit
	}
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(data, '\n'))
	return errors.Join(writeErr, file.Close())
}

// Each raw shard has its own fixed budget. The manifest indexes complete typed
// facts without duplicating access-unit or frame arrays in an aggregate object.
func hlsGeneratedAVDiagnosticWriteFacts(directory string, result transcode.GeneratedAVWindowDiagnostic) error {
	var shards []string
	write := func(name string, value any) error {
		if err := hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, name), value); err != nil {
			return err
		}
		shards = append(shards, name)
		return nil
	}
	for _, part := range []struct {
		name  string
		value any
	}{{"source-certificate.json", result.SourceCertificate}, {"source-effective.json", result.SourceEffective}, {"source-pcm.json", result.SourcePCM}} {
		if err := write(part.name, part.value); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		name string
		run  transcode.GeneratedAVDiagnosticRun
	}{{"baseline", result.Baseline}, {"observer", result.Observer}} {
		common := item.run
		common.Renditions = nil
		if err := write(item.name+"-receipts.json", common); err != nil {
			return err
		}
		if len(item.run.Renditions) > transcode.MaxHLSRenditions {
			return transcode.ErrTimelineLimit
		}
		for index, rendition := range item.run.Renditions {
			if len(rendition.Cuts) > 32 {
				return transcode.ErrTimelineLimit
			}
			commonRendition := rendition
			commonRendition.Cuts = nil
			if err := write(fmt.Sprintf("%s-rendition-%02d.json", item.name, index), commonRendition); err != nil {
				return err
			}
			for cutIndex, cut := range rendition.Cuts {
				if err := write(fmt.Sprintf("%s-rendition-%02d-cut-%03d.json", item.name, index, cutIndex), cut); err != nil {
					return err
				}
			}
		}
	}
	return hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, "actual-av-cut-facts-manifest.json"), map[string]any{
		"Marker": "goby-generated-av-a1-cut-facts-v1", "Qualified": result.Qualified, "Complete": result.Complete,
		"NativeClockComplete": result.NativeClockComplete,
		"NegotiatedPlan":      result.NegotiatedPlan, "WindowPlan": result.WindowPlan,
		"ObserverDelta": result.ObserverDelta, "Comparison": result.Comparison, "Shards": shards,
		"MaxJSONBytesPerShard": 4 << 20,
	})
}

// A1 observes genuine six-second physical cuts and codec delay. A completed
// diagnostic is not finite A/V publication, source EOF or native playback.
func TestHTTPGeneratedAVWindowA1ActualDiagnostic(t *testing.T) {
	runID := os.Getenv("GOBY_GENERATED_AV_DIAGNOSTIC_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_GENERATED_AV_DIAGNOSTIC_RUN_ID explicitly admits the owned A1 diagnostic")
	}
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" ||
		!regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("A1 requires root, an owned integration database and a bounded run identity")
	}
	parent := os.Getenv("GOBY_GENERATED_AV_DIAGNOSTIC_ARTIFACTS_DIR")
	if !filepath.IsAbs(parent) || filepath.Clean(parent) != parent {
		t.Fatal("A1 evidence requires an absolute canonical private parent")
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0700 {
		t.Fatal("A1 evidence parent must be an owned private directory")
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		t.Fatal("A1 evidence parent cannot redirect through symbolic links")
	}
	directory, err := os.MkdirTemp(parent, "generated-av-a1-")
	if err != nil || os.Chmod(directory, 0700) != nil {
		t.Fatal("create private A1 evidence directory")
	}
	driver := map[string]any{
		"Marker": "goby-generated-av-a1-driver-v1", "RunId": runID, "Complete": false, "Qualified": false,
		"NativeClockComplete": false,
		"DefaultEnabled":      false, "SourceSeconds": 48, "WindowStartTicks": 0, "WindowEndTicks": 24 * media.TicksPerSecond,
		"PhysicalSegmentSeconds": 6, "NativeClockVersion": 0, "RequireInputEvidence": false,
		"BaselineBudget": "existing_scoped_transcode_manager", "ObservationBudget": "existing_media_process_and_hls_probe_lanes",
		"ManagerRunConsumesMediaHelperLease": false, "ManagerTerminalProvesAVClosure": false,
		"CredentialsWrittenToEvidence": false,
	}
	defer func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, "driver-result.json"), driver) != nil {
			t.Error("preserve bounded A1 driver evidence")
		}
	}()
	h := newHLSGeneratedAVWindowDiagnosticFixture(t)
	graph := hlsGeneratedAVWindowDiagnosticPrepare(t, h)
	negotiated := graph.session.key.plan
	ctx, cancel := context.WithTimeout(h.f.ctx, 4*time.Minute)
	defer cancel()
	ctx = transcode.WithGeneratedAVEffectiveJSONCapture(ctx, func(request transcode.GeneratedAVEffectiveJSONCaptureRequest) (*os.File, error) {
		// Probe one observes the source. Probe two is the first baseline
		// physical cut; preserve its original projection before strict parsing.
		if request.Ordinal != 2 {
			return nil, nil
		}
		if request.Bytes <= 0 || request.Bytes > 4<<20 || request.LimitBytes != 4<<20 {
			return nil, transcode.ErrTimelineLimit
		}
		file, err := os.OpenFile(filepath.Join(directory, "baseline-first-effective-raw.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		driver["EffectiveRawCapture"] = map[string]any{
			"Ordinal": request.Ordinal, "Bytes": request.Bytes, "LimitBytes": request.LimitBytes,
			"SHA256": fmt.Sprintf("%x", request.SHA256), "PrivateArtifact": "baseline-first-effective-raw.json",
			"SameJoinedProbe": true, "AddsProcess": false, "CapturesStderrOrArguments": false,
		}
		return file, nil
	})
	source, _, err := h.f.app.hls.verify(ctx, graph.session.principal, graph.session.key.scope, graph.session.key.stamp, negotiated)
	if err != nil || source == nil {
		t.Fatal("A1 could not retain its freshly authorized genuine source")
	}
	var sourceClosed bool
	defer func() {
		if !sourceClosed {
			_ = source.Close()
		}
	}()
	before, err := source.Stat()
	if err != nil {
		t.Fatal("A1 source identity could not be captured")
	}
	identity, err := media.VideoSeekSourceIdentity(before)
	if err != nil {
		t.Fatal("A1 source lacks a stable native identity")
	}
	processBefore := media.GetProcessCapacityStats()
	result, diagnosticErr := transcode.RunGeneratedAVWindowDiagnostic(ctx, source, transcode.GeneratedAVWindowDiagnosticOptions{
		FFmpegPath: h.ffmpeg, FFprobePath: h.ffprobe, Directory: directory, NegotiatedPlan: negotiated,
		StartTicks: 0, EndTicks: 24 * media.TicksPerSecond, StartNumber: 0, Threads: 1,
		AcquireProbe: hlsGeneratedAVDiagnosticAcquireProbe(h.f.app.hls), Baseline: hlsGeneratedAVDiagnosticBaseline(h, graph),
	})
	if hlsGeneratedAVDiagnosticWriteFacts(directory, result) != nil {
		t.Error("preserve bounded actual A/V source, coded and effective cut facts")
	}
	after, statErr := source.Stat()
	afterIdentity, identityErr := media.VideoSeekSourceIdentity(after)
	sourceUnchanged := statErr == nil && identityErr == nil && afterIdentity == identity
	closeSourceErr := source.Close()
	sourceClosed = true
	_, closedStatErr := source.Stat()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
	closeErr := h.f.app.hls.Close(closeCtx)
	closeCancel()
	processAfter := media.GetProcessCapacityStats()
	resources, supported := h.f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	var usage transcode.ResourceUsage
	var usageErr error
	if supported {
		usage, usageErr = resources.ResourceUsage(h.f.ctx, graph.session.key.scope)
	} else {
		usageErr = errors.New("scoped production resource observer unavailable")
	}
	driver["NegotiatedPlan"] = negotiated
	driver["NativeClockComplete"] = result.NativeClockComplete
	driver["FullNegotiatedRenditionsPreserved"] = graph.session.key.plan == negotiated && result.NegotiatedPlan == negotiated
	driver["SourceIdentityUnchanged"] = sourceUnchanged
	driver["SourceDescriptorClosed"] = closeSourceErr == nil && errors.Is(closedStatErr, os.ErrClosed)
	driver["RuntimeCloseSucceeded"], driver["ManagerScopeAfterClose"] = closeErr == nil, usage
	driver["MediaHelpersBefore"], driver["MediaHelpersAfterClose"] = processBefore, processAfter
	baselineCacheRemoved := false
	if id := result.Baseline.Record.ID; id != "" && filepath.Base(id) == id {
		_, statErr := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id))
		baselineCacheRemoved = errors.Is(statErr, os.ErrNotExist)
	}
	driver["ActualBaselineCacheRemovedAfterClose"] = baselineCacheRemoved
	driver["DiagnosticErrorType"] = "none"
	if diagnosticErr != nil {
		driver["DiagnosticErrorType"] = "diagnostic_failure"
		var stage *transcode.GeneratedAVDiagnosticStageError
		if errors.As(diagnosticErr, &stage) {
			driver["DiagnosticStage"], driver["DiagnosticVariant"], driver["DiagnosticNumber"], driver["DiagnosticCause"] = stage.Stage, stage.Variant, stage.Number, stage.Cause
		}
	}
	if diagnosticErr != nil || closeErr != nil || usageErr != nil || closeSourceErr != nil || !sourceUnchanged || !errors.Is(closedStatErr, os.ErrClosed) {
		t.Fatalf("actual A1 failed: diagnostic=%T close=%T resources=%T; inspect private cut facts", diagnosticErr, closeErr, usageErr)
	}
	if !result.Complete || result.Qualified || result.NativeClockComplete || result.NegotiatedPlan != negotiated || graph.session.key.plan != negotiated ||
		!result.Baseline.BaselineManagerCompleted || result.Baseline.Plan.HLS.Window.RequireInputEvidence || result.Baseline.Plan.HLS.Window.NativeClockVersion != 0 ||
		!result.Observer.ObserversJoined || !result.Baseline.SourceIdentityUnchanged || !result.Observer.SourceIdentityUnchanged ||
		!result.Baseline.OutputIdentitiesUnchanged || !result.Observer.OutputIdentitiesUnchanged ||
		!result.Comparison.MediaBytesEqual || !result.Comparison.PacketClocksEqual || !result.Comparison.PhysicalCutsEqual || !result.Comparison.NegotiatedLadderPreserved {
		t.Fatal("A1 observations are incomplete, alter the genuine baseline, or falsely grant A/V qualification")
	}
	for _, run := range []transcode.GeneratedAVDiagnosticRun{result.Baseline, result.Observer} {
		if len(run.Renditions) != negotiated.HLS.RenditionCount {
			t.Fatal("A1 omitted a naturally negotiated output from the actual observations")
		}
		for _, rendition := range run.Renditions {
			if len(rendition.Cuts) == 0 || !rendition.ContinuousObservation.Complete ||
				!rendition.ContinuousObservation.FramesParsed || rendition.ContinuousObservation.Qualified ||
				rendition.ContinuousObservation.NativeClockComplete || rendition.ContinuousEffective.Complete || rendition.ContinuousEffective.FramesParsed {
				t.Fatal("A1 continuous observations omitted unknown clocks or forged strict source evidence")
			}
			for _, cut := range rendition.Cuts {
				if !cut.Observation.Complete || !cut.Observation.FramesParsed || cut.Observation.Qualified ||
					cut.Observation.NativeClockComplete || cut.Effective.Complete || cut.Effective.FramesParsed {
					t.Fatal("A1 physical cut observations omitted unknown clocks or forged strict source evidence")
				}
			}
		}
	}
	// The original same-probe capture established these returned fields for the
	// pinned first physical cut. Missing values must survive the new observation
	// domain, including the actual samples beyond the nominal six-second count.
	first := result.Baseline.Renditions[0].Cuts[0].Observation
	if first.Video.Frames != 144 || first.Video.NativePTSFrames != 144 || first.Video.NativeDTSFrames != 144 || first.Video.NativeDurationFrames != 0 ||
		first.Audio.Frames != 283 || first.Audio.NativePTSFrames != 49 || first.Audio.NativeDTSFrames != 49 || first.Audio.NativeDurationFrames != 0 || first.Audio.Samples != 289792 ||
		first.Video.AggregateEndKnown || first.Audio.AggregateEndKnown || first.Video.GapFactsKnown || first.Audio.GapFactsKnown {
		t.Fatal("A1 first cut no longer retains the captured original missing clocks and actual decoded sample count")
	}
	if result.SourceCertificate.SourceIdentity != identity || result.SourceCertificate.DurationTicks != negotiated.DurationTicks ||
		result.SourceCertificate.DemuxOrigin != (transcode.GeneratedRational{Num: 0, Den: 1}) ||
		result.SourceCertificate.PresentationEpoch != (transcode.GeneratedRational{Num: 0, Den: 1}) ||
		result.SourceCertificate.Audio.SampleRate != 48000 || result.SourceCertificate.Audio.Channels != 2 {
		t.Fatal("A1 metadata candidate changed its actual held source or aliased format and presentation origins")
	}
	if !baselineCacheRemoved || usage != (transcode.ResourceUsage{}) || processAfter.Active != 0 || processAfter.Background != 0 || processAfter.Queued != 0 ||
		processAfter.RetirementUnknown != processBefore.RetirementUnknown || h.f.app.hls.generatedWindowsEnabled {
		t.Fatal("A1 did not return its actual production/observation resources or changed the default feature")
	}
	driver["Complete"] = true
	t.Logf("actual_av_a1_diagnostic_complete=true qualified=false native_clock_complete=false natural_renditions=%d physical_segment_seconds=6 baseline_manager_budget_separate=true", negotiated.HLS.RenditionCount)
}
