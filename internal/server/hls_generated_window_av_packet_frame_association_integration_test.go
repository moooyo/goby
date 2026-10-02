//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedAVPacketFrameAssociationHeld struct {
	file     *os.File
	before   os.FileInfo
	identity string
	offset   int64
}

// This measured association retains the original joined projection and binds
// encoded units to the same held input. Missing decoded origins remain unknown.
func TestHTTPGeneratedAVPacketFrameTransportAssociation(t *testing.T) {
	runID := os.Getenv("GOBY_GENERATED_AV_ASSOCIATION_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_GENERATED_AV_ASSOCIATION_RUN_ID explicitly admits the owned raw calibration")
	}
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" ||
		!regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("association calibration requires root, an owned integration database and a bounded run identity")
	}
	parent := os.Getenv("GOBY_GENERATED_AV_ASSOCIATION_ARTIFACTS_DIR")
	if !filepath.IsAbs(parent) || filepath.Clean(parent) != parent {
		t.Fatal("association evidence requires an absolute canonical private parent")
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0700 {
		t.Fatal("association evidence parent must be a private directory")
	}
	resolved, err := filepath.EvalSymlinks(parent)
	if err != nil || resolved != parent {
		t.Fatal("association evidence parent cannot redirect through symbolic links")
	}
	directory, err := os.MkdirTemp(parent, "generated-av-packet-frame-")
	if err != nil || os.Chmod(directory, 0700) != nil {
		t.Fatal("create private association evidence directory")
	}
	driver := map[string]any{
		"Marker": "goby-generated-av-packet-frame-association-v1", "RunId": runID,
		"Complete": false, "Qualified": false, "SchemaParsed": false,
		"PhysicalPacketBytesBound": false, "DecodedFrameOriginComplete": false, "ContentBound": false,
		"DefaultEnabled": false, "NativeClockVersion": 0, "RequireInputEvidence": false,
		"SourceSeconds": 48, "ProducerStartTicks": 0, "ProducerEndTicks": 24 * media.TicksPerSecond,
		"PhysicalSegmentSeconds": 6, "ObservedVariant": 0, "ObservedCuts": []int{0, 1},
		"ProbeInvocationsPerObservedCut": 1, "RunsOldOutputFrameProbe": false, "RunsDefaultFillProbe": false,
		"RunsStreamingPCM": false, "BaselineBudget": "existing_scoped_transcode_manager",
		"ObservationBudget":                  "existing_media_process_and_hls_probe_lanes",
		"ManagerRunConsumesMediaHelperLease": false, "ManagerTerminalProvesAVClosure": false,
		"CredentialsWrittenToEvidence": false,
	}
	defer func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, "driver-result.json"), driver) != nil {
			t.Error("preserve bounded association driver evidence")
		}
	}()
	h := newHLSGeneratedAVWindowDiagnosticFixture(t)
	graph := hlsGeneratedAVWindowDiagnosticPrepare(t, h)
	negotiated := graph.session.key.plan
	ctx, cancel := context.WithTimeout(h.f.ctx, 4*time.Minute)
	defer cancel()
	source, _, err := h.f.app.hls.verify(ctx, graph.session.principal, graph.session.key.scope, graph.session.key.stamp, negotiated)
	if err != nil || source == nil {
		t.Fatal("association calibration could not retain its freshly authorized source")
	}
	defer source.Close()
	var held []hlsGeneratedAVPacketFrameAssociationHeld
	hold := func(file *os.File) error {
		if file == nil {
			return transcode.ErrInvalidInput
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 {
			return transcode.ErrInvalidInput
		}
		identity, err := media.VideoSeekSourceIdentity(info)
		if err != nil {
			return err
		}
		for _, prior := range held {
			if os.SameFile(prior.before, info) {
				return transcode.ErrInvalidInput
			}
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}
		held = append(held, hlsGeneratedAVPacketFrameAssociationHeld{file: file, before: info, identity: identity, offset: offset})
		return nil
	}
	if err := hold(source); err != nil {
		t.Fatal("association source lacks an independently held stable identity")
	}
	processBefore := media.GetProcessCapacityStats()
	acquire := hlsGeneratedAVDiagnosticAcquireProbe(h.f.app.hls)
	probe := func(run func() error) error {
		release, err := acquire(ctx)
		if err != nil {
			return err
		}
		if release == nil {
			return transcode.ErrInvalidOptions
		}
		defer release()
		return run()
	}
	var certificate transcode.GeneratedAVSourceCertificate
	err = probe(func() error {
		var err error
		certificate, err = transcode.MeasureGeneratedMP4AVSourceEndpoint(ctx, h.ffprobe, source, negotiated.VideoStreamIndex, negotiated.AudioStreamIndex)
		return err
	})
	if err != nil || certificate.SourceIdentity != held[0].identity || certificate.DurationTicks != negotiated.DurationTicks ||
		certificate.DemuxOrigin != (transcode.GeneratedRational{Den: 1}) || certificate.PresentationEpoch != (transcode.GeneratedRational{Den: 1}) {
		t.Fatal("association source certificate changed its actual selected source")
	}
	var reference transcode.GeneratedAVEffectiveDecodeDiagnostic
	err = probe(func() error {
		var err error
		reference, err = transcode.MeasureGeneratedAVEffectiveDecodeDiagnostic(ctx, h.ffprobe, []*os.File{source})
		return err
	})
	if err != nil || !reference.Complete || len(reference.InputSHA256) != 1 || reference.Qualified {
		t.Fatal("association calibration lacks its strict whole encoded-source reference")
	}
	sourceSHA := reference.InputSHA256[0]
	driver["EncodedSourceSHA256"] = fmt.Sprintf("%x", sourceSHA)
	if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, "source-certificate.json"), certificate) != nil ||
		hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, "source-effective.json"), reference) != nil {
		t.Fatal("preserve bounded same-source calibration reference")
	}
	plan := negotiated
	plan.HLS.Window = transcode.HLSWindow{EndTicks: 24 * media.TicksPerSecond, StartNumber: 0}
	if transcode.ValidatePlan(plan) != nil || plan.HLS.Window.NativeClockVersion != 0 || plan.HLS.Window.RequireInputEvidence {
		t.Fatal("association calibration altered the genuine generic producer contract")
	}
	baseline, err := hlsGeneratedAVDiagnosticBaseline(h, graph)(ctx, source, plan)
	if err != nil {
		t.Fatal("association calibration could not retain its real scoped baseline")
	}
	closeBaseline := func() error {
		var failures []error
		for variant := range baseline.PlaylistHandles {
			if handle := baseline.PlaylistHandles[variant]; handle != nil {
				failures = append(failures, handle.Close())
			}
			for _, handle := range baseline.MediaHandles[variant] {
				if handle != nil {
					failures = append(failures, handle.Close())
				}
			}
		}
		return errors.Join(failures...)
	}
	defer closeBaseline()
	semantic := baseline.Record.Spec.Plan
	if plan.ExecutionVersion == 0 && plan.Execution == (transcode.ExecutionOptions{}) {
		semantic.ExecutionVersion, semantic.Execution = 0, transcode.ExecutionOptions{}
	}
	if baseline.Record.ID == "" || baseline.Record.State != "completed" || semantic != plan {
		t.Fatal("association baseline completion changed its actual negotiated plan")
	}
	for variant := 0; variant < negotiated.HLS.RenditionCount; variant++ {
		if baseline.PlaylistHandles[variant] == nil || baseline.PlaylistHandles[variant].EncodingID() != baseline.Record.ID ||
			len(baseline.MediaHandles[variant]) != len(baseline.Playlists[variant].Segments) || len(baseline.MediaHandles[variant]) != 4 {
			t.Fatal("association calibration omitted the original full natural ladder or physical cuts")
		}
		if err := hold(baseline.PlaylistHandles[variant].File); err != nil {
			t.Fatal("retain actual distinct baseline playlist")
		}
		for _, handle := range baseline.MediaHandles[variant] {
			if handle == nil || handle.EncodingID() != baseline.Record.ID || hold(handle.File) != nil {
				t.Fatal("retain actual distinct baseline media")
			}
		}
	}
	var receipts []transcode.GeneratedAVPacketFrameAssociationMeasurement
	var calibrationErr error
	for number := 0; number < 2; number++ {
		segment := baseline.MediaHandles[0][number].File
		name := fmt.Sprintf("cut-%03d-packet-frame-raw.json", number)
		capture, err := os.OpenFile(filepath.Join(directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			calibrationErr = err
			break
		}
		// The measurement consumes capture on every outcome. Source and media
		// are borrowed and remain held with every natural sibling until fences.
		receipt, measurementErr := transcode.MeasureGeneratedAVPacketFrameTransportAssociation(ctx, source, segment, transcode.GeneratedAVAssociationProjectionOptions{
			FFprobePath: h.ffprobe, SourceCertificate: certificate, ExpectedSourceSHA256: sourceSHA,
			AcquireProbe: acquire, Capture: capture,
		})
		_, captureStatErr := capture.Stat()
		if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, fmt.Sprintf("cut-%03d-transport.json", number)), receipt.Transport) != nil ||
			hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, fmt.Sprintf("cut-%03d-projection-receipt.json", number)), receipt) != nil {
			t.Error("preserve bounded physical transport and projection receipt")
		}
		receipts = append(receipts, receipt)
		raw := receipt.RawProjection
		if measurementErr != nil || !errors.Is(captureStatErr, os.ErrClosed) || !receipt.Complete || receipt.Stage != "complete" || receipt.Qualified || receipt.NativeClockComplete ||
			!raw.Complete || !raw.EnvelopeParsed || !raw.CaptureWritten || raw.Qualified || raw.SchemaCalibrated || raw.PayloadRepresentationKnown || raw.PacketFrameBound || raw.ContentBound ||
			raw.JSONBytes < 1 || raw.JSONBytes > 4<<20 || raw.JSONLimitBytes != 4<<20 || raw.SourceSHA256 != sourceSHA ||
			raw.InputSHA256 != receipt.Transport.SHA256 || !raw.SourceIdentityUnchanged || !raw.InputIdentityUnchanged ||
			!receipt.Projection.SchemaParsed || !receipt.Projection.Complete || !receipt.Projection.MeasuredInputKnown || receipt.Projection.Qualified ||
			!receipt.Projection.Observation.Complete || receipt.Projection.Observation.NativeClockComplete || receipt.Projection.Observation.Qualified ||
			!receipt.Association.SameHeldInput || !receipt.Association.CandidateByteSequenceEqual || !receipt.Association.PacketUnitBindingComplete ||
			receipt.Association.ReturnedPositionCandidatesComplete || receipt.Association.DecodedFrameOriginComplete || receipt.Association.ContentBound || receipt.Association.Qualified {
			calibrationErr = errors.Join(measurementErr, transcode.ErrTimelineProbe)
			break
		}
		uniqueFrames, missingFrames := 0, 0
		for _, candidate := range receipt.Association.PacketUnits {
			if candidate.Status != transcode.GeneratedAVAssociationUnique || candidate.CandidateCount != 1 || candidate.UnitReused {
				calibrationErr = transcode.ErrTimelineProbe
			}
		}
		for _, candidate := range receipt.Association.FramePackets {
			switch candidate.Status {
			case transcode.GeneratedAVAssociationUnique:
				uniqueFrames++
			case transcode.GeneratedAVAssociationMissing:
				missingFrames++
				if candidate.CandidateCount != 0 || candidate.PacketOrdinal != -1 {
					calibrationErr = transcode.ErrTimelineProbe
				}
			default:
				calibrationErr = transcode.ErrTimelineProbe
			}
		}
		// These counts are from the original returned positions, not from
		// packet/frame adjacency or a synthesized clock for the missing frames.
		if uniqueFrames != []int{193, 190}[number] || missingFrames != []int{234, 235}[number] ||
			len(receipt.Projection.Packets) != []int{427, 425}[number] || receipt.Projection.Observation.Audio.Samples != []int64{289792, 287744}[number] {
			calibrationErr = transcode.ErrTimelineProbe
		}
		if calibrationErr != nil {
			break
		}
	}
	allUnchanged := true
	for _, value := range held {
		info, err := value.file.Stat()
		offset, offsetErr := value.file.Seek(0, io.SeekCurrent)
		identity, identityErr := media.VideoSeekSourceIdentity(info)
		allUnchanged = allUnchanged && err == nil && offsetErr == nil && offset == value.offset && identityErr == nil && identity == value.identity
	}
	if transcode.ValidateGeneratedMP4AVSourceEndpointIdentity(source, certificate) != nil {
		allUnchanged = false
	}
	closeBaselineErr := closeBaseline()
	closeSourceErr := source.Close()
	_, closedSourceErr := source.Stat()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 20*time.Second)
	closeRuntimeErr := h.f.app.hls.Close(closeCtx)
	closeCancel()
	processAfter := media.GetProcessCapacityStats()
	resourceObserver, supported := h.f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	var usage transcode.ResourceUsage
	var usageErr error
	if supported {
		usage, usageErr = resourceObserver.ResourceUsage(h.f.ctx, graph.session.key.scope)
	} else {
		usageErr = errors.New("scoped production resource observer unavailable")
	}
	_, cacheErr := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, baseline.Record.ID))
	driver["NegotiatedPlan"], driver["BaselineRecord"] = negotiated, baseline.Record
	driver["NaturalRenditions"], driver["PhysicalCutsPerRendition"] = negotiated.HLS.RenditionCount, 4
	driver["ProjectionReceipts"], driver["AllHeldIdentitiesAndOffsetsUnchanged"] = receipts, allUnchanged
	driver["CalibrationErrorType"] = "none"
	if calibrationErr != nil {
		driver["CalibrationErrorType"] = fmt.Sprintf("%T", calibrationErr)
	}
	driver["MediaHelpersBefore"], driver["MediaHelpersAfterClose"] = processBefore, processAfter
	driver["ManagerScopeAfterClose"] = usage
	driver["BaselineReadersClosed"], driver["SourceDescriptorClosed"] = closeBaselineErr == nil, closeSourceErr == nil && errors.Is(closedSourceErr, os.ErrClosed)
	driver["RuntimeCloseSucceeded"], driver["ActualBaselineCacheRemoved"] = closeRuntimeErr == nil, errors.Is(cacheErr, os.ErrNotExist)
	if !allUnchanged || closeBaselineErr != nil || closeSourceErr != nil || !errors.Is(closedSourceErr, os.ErrClosed) || closeRuntimeErr != nil || usageErr != nil ||
		usage != (transcode.ResourceUsage{}) || !errors.Is(cacheErr, os.ErrNotExist) || processAfter.Active != 0 || processAfter.Background != 0 || processAfter.Queued != 0 ||
		processAfter.RetirementUnknown != processBefore.RetirementUnknown || h.f.app.hls.generatedWindowsEnabled {
		t.Fatal("association calibration did not drain its real owned resources or preserve the default feature")
	}
	if calibrationErr != nil || len(receipts) != 2 {
		t.Fatalf("association raw calibration failed after owned cleanup: error=%T", calibrationErr)
	}
	driver["SchemaParsed"], driver["PhysicalPacketBytesBound"] = true, true
	driver["Complete"] = true
	t.Log("actual_av_packet_frame_association_complete=true physical_packet_bytes_bound=true decoded_frame_origin_complete=false native_clock_complete=false content_bound=false qualified=false")
}
