//go:build linux

package server

import (
	"context"
	"crypto/sha256"
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

type hlsGeneratedAVPCMContentHeld struct {
	file     *os.File
	before   os.FileInfo
	identity string
	offset   int64
}

// This captures the original PCM streams once each and compares sparse content
// candidates with a fixed oracle. Native clock/origin/trim facts remain separate.
func TestHTTPGeneratedAVPCMSourceContentAndSeam(t *testing.T) {
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
	directory, err := os.MkdirTemp(parent, "generated-av-pcm-content-")
	if err != nil || os.Chmod(directory, 0700) != nil {
		t.Fatal("create private association evidence directory")
	}
	driver := map[string]any{
		"Marker": "goby-generated-av-pcm-content-association-v1", "RunId": runID,
		"Complete": false, "Qualified": false, "SchemaParsed": false,
		"PhysicalPacketBytesBound": false, "DecodedFrameOriginComplete": false, "ContentBound": false,
		"DefaultEnabled": false, "NativeClockVersion": 0, "RequireInputEvidence": false,
		"SourceSeconds": 48, "ProducerStartTicks": 0, "ProducerEndTicks": 24 * media.TicksPerSecond,
		"PhysicalSegmentSeconds": 6, "ObservedVariant": 0, "ObservedCuts": []int{0, 1},
		"ProbeInvocationsPerObservedCut": 1, "RunsOldOutputFrameProbe": false, "RunsDefaultFillProbe": false,
		"RunsStreamingPCM": true, "PlannedPCMCaptureInvocations": 4, "BaselineBudget": "existing_scoped_transcode_manager",
		"ObservationBudget":                  "existing_media_process_and_hls_probe_lanes",
		"ManagerRunConsumesMediaHelperLease": false, "ManagerTerminalProvesAVClosure": false,
		"CredentialsWrittenToEvidence": false,
		"ContentCandidateObserved":     false, "CapturedPCMIsNativeClock": false,
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
	var held []hlsGeneratedAVPCMContentHeld
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
		held = append(held, hlsGeneratedAVPCMContentHeld{file: file, before: info, identity: identity, offset: offset})
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
	failureStage := "packet_frame_measurement"
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
	var captures []transcode.GeneratedAVPCMCaptureDiagnostic
	var pcmFiles []*os.File
	defer func() {
		for _, file := range pcmFiles {
			_ = file.Close()
		}
	}()
	if calibrationErr == nil {
		group := make([]*os.File, 0, 4)
		for _, handle := range baseline.MediaHandles[0] {
			group = append(group, handle.File)
		}
		jobs := []struct {
			name  string
			role  transcode.GeneratedAVPCMCaptureRole
			files []*os.File
		}{
			{"source", transcode.GeneratedAVPCMCaptureSource, []*os.File{source}},
			{"cut-000", transcode.GeneratedAVPCMCaptureCut, []*os.File{group[0]}},
			{"cut-001", transcode.GeneratedAVPCMCaptureCut, []*os.File{group[1]}},
			{"continuous", transcode.GeneratedAVPCMCaptureGroup, group},
		}
		for index, job := range jobs {
			failureStage = "pcm_capture_" + job.name
			path := filepath.Join(directory, job.name+".pcm")
			capture, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				calibrationErr = err
				break
			}
			receipt, measurementErr := transcode.MeasureGeneratedAVPCMCaptureDiagnostic(ctx, source, job.files, transcode.GeneratedAVPCMCaptureOptions{
				FFmpegPath: h.ffmpeg, SourceCertificate: certificate, ExpectedSourceSHA256: sourceSHA,
				Channels: reference.Audio.Channels, Role: job.role, AcquireProbe: acquire, Capture: capture,
			})
			captures = append(captures, receipt)
			_, captureStatErr := capture.Stat()
			if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(directory, job.name+"-pcm-receipt.json"), receipt) != nil {
				t.Error("preserve bounded PCM capture receipt")
			}
			limit := int64(8 << 20)
			if index == 0 {
				limit = 24 << 20
			}
			if measurementErr != nil || !errors.Is(captureStatErr, os.ErrClosed) || !receipt.Complete || !receipt.CaptureWritten ||
				receipt.Qualified || receipt.NativeClockComplete || receipt.DecodedOriginComplete || receipt.ContentBound ||
				!receipt.PCM.Complete || receipt.PCM.Qualified || receipt.PCM.SampleRate != 48000 || receipt.PCM.Channels != 2 ||
				receipt.PCM.Bytes < 1 || receipt.PCM.Bytes > limit || receipt.LimitBytes != limit ||
				receipt.SourceSHA256 != sourceSHA || !receipt.SourceIdentityUnchanged || !receipt.InputIdentitiesUnchanged || len(receipt.InputSHA256) != len(job.files) {
				calibrationErr = errors.Join(measurementErr, transcode.ErrTimelineProbe)
				break
			}
			if index == 0 && (receipt.InputSHA256[0] != sourceSHA || receipt.PCM.Samples != reference.Audio.Samples) {
				calibrationErr = transcode.ErrInvalidInput
				break
			}
			if index == 1 || index == 2 {
				projection := receipts[index-1]
				if receipt.InputSHA256[0] != projection.Transport.SHA256 || receipt.PCM.Samples != projection.Projection.Observation.Audio.Samples {
					calibrationErr = transcode.ErrInvalidInput
					break
				}
			}
			file, err := os.Open(path)
			if err != nil {
				calibrationErr = err
				break
			}
			pcmFiles = append(pcmFiles, file)
			if err := hold(file); err != nil {
				calibrationErr = err
				break
			}
		}
	}
	var candidates []map[string]any
	var contentFailureStages []string
	if calibrationErr == nil && len(captures) == 4 && len(pcmFiles) == 4 {
		checks := []struct {
			name              string
			ref, query        int
			first, queryFirst int64
			wantUnique        bool
		}{
			{"source_cut0_interior", 0, 1, 0, 4096, true},
			{"source_cut1_interior", 0, 2, 288000, 4096, true},
			{"source_continuous_initial", 0, 3, 0, 4096, true},
			{"source_continuous_seam", 0, 3, 283904, 292096, true},
			{"continuous_cut0_interior", 3, 1, 0, 4096, true},
			{"continuous_cut1_interior", 3, 2, 288000, 4096, true},
			{"continuous_cut1_startup", 3, 2, 284000, 0, false},
			{"wrong_source_position", 0, 1, 1152000, 4096, false},
		}
		for _, check := range checks {
			candidateStage := "pcm_candidate_" + check.name
			options := transcode.GeneratedAVPCMContentOptions{
				Channels: 2, EncodedSourceSHA256: sourceSHA,
				ReferenceSHA256: captures[check.ref].PCM.SHA256, QuerySHA256: captures[check.query].PCM.SHA256,
				ReferenceFirstCandidateSample: check.first, QueryFirstSample: check.queryFirst, CandidateCount: 8193, WindowSamples: 4096,
			}
			candidate, err := transcode.CompareGeneratedAVPCMContentDiagnostic(ctx, pcmFiles[check.ref], pcmFiles[check.query], options)
			accepted := err == nil && candidate.Complete && candidate.CapturedBytesVerified && !candidate.Qualified && !candidate.NativeClockKnown && !candidate.DecodedOriginComplete && !candidate.ContentBound &&
				(!check.wantUnique || candidate.Status == transcode.GeneratedAVAssociationUnique) && (check.name != "wrong_source_position" || candidate.Status != transcode.GeneratedAVAssociationUnique)
			candidates = append(candidates, map[string]any{"Name": check.name, "RequiredUnique": check.wantUnique, "Accepted": accepted, "Candidate": candidate, "ErrorType": fmt.Sprintf("%T", err)})
			if !accepted {
				contentFailureStages = append(contentFailureStages, candidateStage)
			}
			if err != nil || !candidate.Complete || !candidate.CapturedBytesVerified || candidate.Qualified || candidate.NativeClockKnown || candidate.DecodedOriginComplete || candidate.ContentBound {
				calibrationErr = errors.Join(calibrationErr, err, transcode.ErrTimelineProbe)
			}
			if check.wantUnique && candidate.Status != transcode.GeneratedAVAssociationUnique {
				calibrationErr = errors.Join(calibrationErr, transcode.ErrTimelineProbe)
			}
			if check.name == "wrong_source_position" && candidate.Status == transcode.GeneratedAVAssociationUnique {
				calibrationErr = errors.Join(calibrationErr, transcode.ErrTimelineProbe)
			}
		}
		// This artifact is an explicit derived negative, never a producer capture
		// or an encoded-source receipt. Only the channel-order guard is tested.
		swappedPath := filepath.Join(directory, "derived-swapped-cut0.pcm")
		failureStage = "pcm_candidate_swapped_channels"
		swappedHash, err := hlsGeneratedAVPCMContentSwapChannels(ctx, pcmFiles[1], swappedPath, captures[1].PCM.Bytes)
		if err == nil {
			swapped, openErr := os.Open(swappedPath)
			err = openErr
			if openErr == nil {
				pcmFiles = append(pcmFiles, swapped)
				if holdErr := hold(swapped); holdErr != nil {
					err = holdErr
				} else {
					candidate, compareErr := transcode.CompareGeneratedAVPCMContentDiagnostic(ctx, pcmFiles[0], swapped, transcode.GeneratedAVPCMContentOptions{
						Channels: 2, EncodedSourceSHA256: sourceSHA, ReferenceSHA256: captures[0].PCM.SHA256, QuerySHA256: swappedHash,
						ReferenceFirstCandidateSample: 0, QueryFirstSample: 4096, CandidateCount: 8193, WindowSamples: 4096,
					})
					candidates = append(candidates, map[string]any{"Name": "swapped_channels", "DerivedNegative": true, "Candidate": candidate, "ErrorType": fmt.Sprintf("%T", compareErr)})
					if compareErr != nil || !candidate.Complete || candidate.Status == transcode.GeneratedAVAssociationUnique {
						err = errors.Join(compareErr, transcode.ErrTimelineProbe)
					}
				}
			}
		}
		calibrationErr = errors.Join(calibrationErr, err)
		if err != nil {
			contentFailureStages = append(contentFailureStages, "pcm_candidate_swapped_channels")
		}
	}
	driver["PCMCaptures"], driver["PCMContentCandidates"] = captures, candidates
	driver["ContentFailureStages"] = contentFailureStages
	driver["ActualPCMCaptureReceipts"] = len(captures)
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
	var pcmCloseErr error
	pcmReadersClosed := true
	for _, file := range pcmFiles {
		pcmCloseErr = errors.Join(pcmCloseErr, file.Close())
		_, statErr := file.Stat()
		pcmReadersClosed = pcmReadersClosed && errors.Is(statErr, os.ErrClosed)
	}
	pcmFiles = nil
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
	// Detailed packet/frame arrays remain in their bounded per-cut shards.
	// The driver indexes them without another full aggregate allocation.
	driver["ProjectionReceiptShards"] = []string{"cut-000-projection-receipt.json", "cut-001-projection-receipt.json"}
	driver["AllHeldIdentitiesAndOffsetsUnchanged"] = allUnchanged
	driver["CalibrationErrorType"] = "none"
	if calibrationErr != nil {
		driver["CalibrationErrorType"] = fmt.Sprintf("%T", calibrationErr)
		if len(contentFailureStages) > 0 {
			failureStage = contentFailureStages[0]
		}
		driver["FailureStage"] = failureStage
	}
	driver["PCMCaptureReadersClosed"] = pcmReadersClosed && pcmCloseErr == nil
	driver["MediaHelpersBefore"], driver["MediaHelpersAfterClose"] = processBefore, processAfter
	driver["ManagerScopeAfterClose"] = usage
	driver["BaselineReadersClosed"], driver["SourceDescriptorClosed"] = closeBaselineErr == nil, closeSourceErr == nil && errors.Is(closedSourceErr, os.ErrClosed)
	driver["RuntimeCloseSucceeded"], driver["ActualBaselineCacheRemoved"] = closeRuntimeErr == nil, errors.Is(cacheErr, os.ErrNotExist)
	if !allUnchanged || !pcmReadersClosed || pcmCloseErr != nil || closeBaselineErr != nil || closeSourceErr != nil || !errors.Is(closedSourceErr, os.ErrClosed) || closeRuntimeErr != nil || usageErr != nil ||
		usage != (transcode.ResourceUsage{}) || !errors.Is(cacheErr, os.ErrNotExist) || processAfter.Active != 0 || processAfter.Background != 0 || processAfter.Queued != 0 ||
		processAfter.RetirementUnknown != processBefore.RetirementUnknown || h.f.app.hls.generatedWindowsEnabled {
		t.Fatal("association calibration did not drain its real owned resources or preserve the default feature")
	}
	if calibrationErr != nil || len(receipts) != 2 || len(captures) != 4 || len(candidates) != 9 {
		t.Fatalf("PCM content diagnostic failed after owned cleanup: error_type=%T", calibrationErr)
	}
	driver["SchemaParsed"], driver["PhysicalPacketBytesBound"] = true, true
	driver["ContentCandidateObserved"], driver["PCMCaptureReadersClosed"] = true, pcmCloseErr == nil
	driver["Complete"] = true
	t.Log("actual_av_pcm_content_complete=true sparse_candidate_observed=true native_clock_complete=false decoded_origin_complete=false content_bound=false qualified=false")
}

func hlsGeneratedAVPCMContentSwapChannels(ctx context.Context, input *os.File, path string, bytes int64) (result [32]byte, failure error) {
	if ctx == nil || input == nil || bytes < 4 || bytes > 8<<20 || bytes%4 != 0 {
		return result, transcode.ErrInvalidOptions
	}
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	defer func() { failure = errors.Join(failure, output.Close()) }()
	digest := sha256.New()
	var buffer [64 << 10]byte
	for offset := int64(0); offset < bytes; {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		n := int(min(int64(len(buffer)), bytes-offset))
		if actual, err := input.ReadAt(buffer[:n], offset); err != nil || actual != n {
			return result, errors.Join(transcode.ErrInvalidInput, err)
		}
		for frame := 0; frame < n; frame += 4 {
			buffer[frame], buffer[frame+2] = buffer[frame+2], buffer[frame]
			buffer[frame+1], buffer[frame+3] = buffer[frame+3], buffer[frame+1]
		}
		if actual, err := output.Write(buffer[:n]); err != nil || actual != n {
			return result, errors.Join(io.ErrShortWrite, err)
		}
		_, _ = digest.Write(buffer[:n])
		offset += int64(n)
	}
	if err := output.Sync(); err != nil {
		return result, err
	}
	copy(result[:], digest.Sum(nil))
	return result, ctx.Err()
}
