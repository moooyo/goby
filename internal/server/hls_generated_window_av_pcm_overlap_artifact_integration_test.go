//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsGeneratedAVPCMOverlapOwned struct {
	file     *os.File
	before   os.FileInfo
	identity string
	name     string
}

func (value hlsGeneratedAVPCMOverlapOwned) fence() error {
	info, err := value.file.Stat()
	offset, offsetErr := value.file.Seek(0, io.SeekCurrent)
	identity, identityErr := media.VideoSeekSourceIdentity(info)
	if err != nil || offsetErr != nil || offset != 0 || identityErr != nil || identity != value.identity || !os.SameFile(info, value.before) {
		return transcode.ErrInvalidInput
	}
	return nil
}

func hlsGeneratedAVPCMOverlapPrivateDirectory(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	resolved, resolveErr := filepath.EvalSymlinks(path)
	return ok && stat.Uid == uint32(os.Geteuid()) && resolveErr == nil && resolved == path
}

// Fixed basenames beneath a caller-owned private directory are opened without
// following symlinks. JSON metadata is diagnostic evidence, never authorization.
func hlsGeneratedAVPCMOverlapOpen(directory, name string, limit int64) (hlsGeneratedAVPCMOverlapOwned, error) {
	var empty hlsGeneratedAVPCMOverlapOwned
	fd, err := syscall.Open(filepath.Join(directory, name), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return empty, transcode.ErrInvalidInput
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() < 1 || info.Size() > limit {
		_ = file.Close()
		return empty, transcode.ErrInvalidInput
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	identity, identityErr := media.VideoSeekSourceIdentity(info)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || identityErr != nil {
		_ = file.Close()
		return empty, transcode.ErrInvalidInput
	}
	return hlsGeneratedAVPCMOverlapOwned{file: file, before: info, identity: identity, name: name}, nil
}

func hlsGeneratedAVPCMOverlapUnicode(data []byte) bool {
	quoted := false
	for index := 0; index < len(data); index++ {
		if data[index] == '"' {
			quoted = !quoted
			continue
		}
		if !quoted || data[index] != '\\' {
			continue
		}
		index++
		if index >= len(data) {
			return false
		}
		if data[index] != 'u' {
			continue
		}
		if index+4 >= len(data) {
			return false
		}
		code, err := strconv.ParseUint(string(data[index+1:index+5]), 16, 16)
		if err != nil {
			return false
		}
		index += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[index+3:index+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}

func hlsGeneratedAVPCMOverlapDecode(data []byte, value any) error {
	if len(data) < 1 || len(data) > 4<<20 || !utf8.Valid(data) || !hlsGeneratedAVPCMOverlapUnicode(data) || !metadataUniqueJSON(data) {
		return transcode.ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return transcode.ErrInvalidInput
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return transcode.ErrInvalidInput
	}
	// A typed round trip rejects omitted/null scalar fields and case aliases
	// that encoding/json would otherwise normalize into an accepted field.
	canonical, err := json.Marshal(value)
	if err != nil || len(canonical) > 4<<20 {
		return transcode.ErrInvalidInput
	}
	var original, decoded any
	if json.Unmarshal(data, &original) != nil || json.Unmarshal(canonical, &decoded) != nil || !reflect.DeepEqual(original, decoded) {
		return transcode.ErrInvalidInput
	}
	return nil
}

func hlsGeneratedAVPCMOverlapHash(ctx context.Context, value hlsGeneratedAVPCMOverlapOwned, expected [32]byte) error {
	if expected == ([32]byte{}) || value.fence() != nil {
		return transcode.ErrInvalidInput
	}
	digest := sha256.New()
	var buffer [64 << 10]byte
	for offset := int64(0); offset < value.before.Size(); {
		if err := ctx.Err(); err != nil {
			return err
		}
		length := int(min(int64(len(buffer)), value.before.Size()-offset))
		if n, err := value.file.ReadAt(buffer[:length], offset); err != nil || n != length {
			return transcode.ErrInvalidInput
		}
		_, _ = digest.Write(buffer[:length])
		offset += int64(length)
	}
	var actual [32]byte
	copy(actual[:], digest.Sum(nil))
	if actual != expected || value.fence() != nil {
		return transcode.ErrInvalidInput
	}
	return ctx.Err()
}

type hlsGeneratedAVPCMOverlapCandidateRow struct {
	Name            string
	Accepted        *bool `json:",omitempty"`
	RequiredUnique  *bool `json:",omitempty"`
	DerivedNegative *bool `json:",omitempty"`
	ErrorType       string
	Candidate       transcode.GeneratedAVPCMContentCandidate
}

type hlsGeneratedAVPCMOverlapHistoricalDriver struct {
	Marker, RunId, EncodedSourceSHA256, CalibrationErrorType                                                                  string
	Complete, GoTestFailed, Qualified, ContentBound, DecodedFrameOriginComplete, DefaultEnabled                               bool
	SourceDescriptorClosed, PCMCaptureReadersClosed, BaselineReadersClosed, RuntimeCloseSucceeded, ActualBaselineCacheRemoved bool
	AllHeldIdentitiesAndOffsetsUnchanged, SchemaParsed, PhysicalPacketBytesBound, ContentCandidateObserved                    bool
	NativeClockVersion, SourceSeconds, PhysicalSegmentSeconds, NaturalRenditions, PhysicalCutsPerRendition, ObservedVariant   int
	ProducerStartTicks, ProducerEndTicks                                                                                      int64
	ActualPCMCaptureReceipts, PlannedPCMCaptureInvocations, ProbeInvocationsPerObservedCut                                    int
	RequireInputEvidence, ManagerTerminalProvesAVClosure, ManagerRunConsumesMediaHelperLease                                  bool
	RunsStreamingPCM, RunsOldOutputFrameProbe, RunsDefaultFillProbe, CapturedPCMIsNativeClock                                 bool
	PCMCaptures                                                                                                               []transcode.GeneratedAVPCMCaptureDiagnostic
	PCMContentCandidates                                                                                                      []hlsGeneratedAVPCMOverlapCandidateRow
	ContentFailureStages                                                                                                      []string
	MediaHelpersBefore, MediaHelpersAfterClose                                                                                media.ProcessCapacitySnapshot
	ManagerScopeAfterClose                                                                                                    transcode.ResourceUsage
}

func hlsGeneratedAVPCMOverlapHistorical(data []byte) (hlsGeneratedAVPCMOverlapHistoricalDriver, error) {
	var empty hlsGeneratedAVPCMOverlapHistoricalDriver
	var fields map[string]json.RawMessage
	if hlsGeneratedAVPCMOverlapDecode(data, &fields) != nil {
		return empty, transcode.ErrInvalidInput
	}
	keys := strings.Fields("ActualBaselineCacheRemoved ActualPCMCaptureReceipts AllHeldIdentitiesAndOffsetsUnchanged BaselineBudget BaselineReadersClosed BaselineRecord CalibrationErrorType CapturedPCMIsNativeClock Complete ContentBound ContentCandidateObserved ContentFailureStages CredentialsWrittenToEvidence DecodedFrameOriginComplete DefaultEnabled EncodedSourceSHA256 GoTestFailed ManagerRunConsumesMediaHelperLease ManagerScopeAfterClose ManagerTerminalProvesAVClosure Marker MediaHelpersAfterClose MediaHelpersBefore NativeClockVersion NaturalRenditions NegotiatedPlan ObservationBudget ObservedCuts ObservedVariant PCMCaptureReadersClosed PCMCaptures PCMContentCandidates PhysicalCutsPerRendition PhysicalPacketBytesBound PhysicalSegmentSeconds PlannedPCMCaptureInvocations ProbeInvocationsPerObservedCut ProducerEndTicks ProducerStartTicks ProjectionReceiptShards Qualified RequireInputEvidence RunId RunsDefaultFillProbe RunsOldOutputFrameProbe RunsStreamingPCM RuntimeCloseSucceeded SchemaParsed SourceDescriptorClosed SourceSeconds")
	if len(fields) != len(keys) {
		return empty, transcode.ErrInvalidInput
	}
	for _, key := range keys {
		if _, present := fields[key]; !present {
			return empty, transcode.ErrInvalidInput
		}
	}
	var observed []int
	if hlsGeneratedAVPCMOverlapDecode(fields["ObservedCuts"], &observed) != nil || !reflect.DeepEqual(observed, []int{0, 1}) {
		return empty, transcode.ErrInvalidInput
	}
	var shardNames []string
	if hlsGeneratedAVPCMOverlapDecode(fields["ProjectionReceiptShards"], &shardNames) != nil || !reflect.DeepEqual(shardNames, []string{"cut-000-projection-receipt.json", "cut-001-projection-receipt.json"}) {
		return empty, transcode.ErrInvalidInput
	}
	for _, key := range strings.Fields("Qualified ContentBound DecodedFrameOriginComplete DefaultEnabled GoTestFailed RequireInputEvidence ManagerTerminalProvesAVClosure ManagerRunConsumesMediaHelperLease RunsOldOutputFrameProbe RunsDefaultFillProbe CapturedPCMIsNativeClock CredentialsWrittenToEvidence") {
		if !bytes.Equal(bytes.TrimSpace(fields[key]), []byte("false")) {
			return empty, transcode.ErrInvalidInput
		}
	}
	for _, key := range strings.Fields("Complete SourceDescriptorClosed PCMCaptureReadersClosed BaselineReadersClosed RuntimeCloseSucceeded ActualBaselineCacheRemoved AllHeldIdentitiesAndOffsetsUnchanged SchemaParsed PhysicalPacketBytesBound ContentCandidateObserved RunsStreamingPCM") {
		if !bytes.Equal(bytes.TrimSpace(fields[key]), []byte("true")) {
			return empty, transcode.ErrInvalidInput
		}
	}
	// Original record/plan/budget/index fields are retained only in the old
	// bounded JSON; they are not copied into the new driver or treated as AUTH.
	for _, key := range strings.Fields("BaselineBudget BaselineRecord NegotiatedPlan ObservationBudget ObservedCuts ProjectionReceiptShards CredentialsWrittenToEvidence") {
		delete(fields, key)
	}
	encoded, err := json.Marshal(fields)
	if err != nil || hlsGeneratedAVPCMOverlapDecode(encoded, &empty) != nil {
		return empty, transcode.ErrInvalidInput
	}
	return empty, nil
}

// TestGeneratedAVPCMArtifactOverlapDiagnostic reopens only the actual completed
// PCM run artifacts. It launches no child, opens no database, creates no manager
// job and reads no encoded source. Historical public JSON grants no authorization.
func TestGeneratedAVPCMArtifactOverlapDiagnostic(t *testing.T) {
	runID := os.Getenv("GOBY_GENERATED_AV_PCM_OVERLAP_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_GENERATED_AV_PCM_OVERLAP_RUN_ID explicitly admits artifact overlap diagnostics")
	}
	if os.Geteuid() != 0 || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("overlap artifact diagnostics require root and a bounded owned run identity")
	}
	input := os.Getenv("GOBY_GENERATED_AV_PCM_OVERLAP_INPUT_EVIDENCE_DIR")
	outputParent := os.Getenv("GOBY_GENERATED_AV_PCM_OVERLAP_ARTIFACTS_DIR")
	if !hlsGeneratedAVPCMOverlapPrivateDirectory(input) || !hlsGeneratedAVPCMOverlapPrivateDirectory(outputParent) || outputParent == input || strings.HasPrefix(outputParent, input+string(os.PathSeparator)) {
		t.Fatal("overlap evidence requires distinct canonical private directories")
	}
	output, err := os.MkdirTemp(outputParent, "generated-av-pcm-overlap-")
	if err != nil || os.Chmod(output, 0700) != nil {
		t.Fatal("create private overlap result directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	report := map[string]any{"Marker": "goby-generated-av-pcm-artifact-overlap-v1", "RunId": runID,
		"Complete": false, "Qualified": false, "NativeClockKnown": false, "DecodedOriginComplete": false, "ContentBound": false,
		"HistoricalEncodedSourceSHAOnly": true, "LiveEncodedSourceHeld": false, "EvidenceIsPublicJSON": true, "EvidenceGrantsAuthorization": false,
		"StartsChild": false, "OpensDatabase": false, "CreatesProductionJob": false, "TrimsOrRewritesCapturedPCM": false}
	var owned []hlsGeneratedAVPCMOverlapOwned
	closed := false
	closeOwned := func() error {
		if closed {
			return nil
		}
		closed = true
		allClosed := true
		var failures []error
		for _, value := range owned {
			failures = append(failures, value.file.Close())
			_, err := value.file.Stat()
			allClosed = allClosed && errors.Is(err, os.ErrClosed)
		}
		report["AllOwnedReadersClosed"] = allClosed
		return errors.Join(failures...)
	}
	defer func() {
		if err := closeOwned(); err != nil {
			t.Error("close owned artifact readers")
		}
		report["GoTestFailed"] = t.Failed()
		if t.Failed() {
			report["Complete"] = false
		}
		if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(output, "driver-result.json"), report) != nil {
			t.Error("preserve bounded artifact overlap result")
		}
	}()
	readJSON := func(name string, target any) {
		t.Helper()
		report["Stage"] = "input_json_" + name
		value, err := hlsGeneratedAVPCMOverlapOpen(input, name, 4<<20)
		if err != nil {
			t.Fatal("open fixed private bounded JSON evidence")
		}
		owned = append(owned, value)
		data := make([]byte, int(value.before.Size()))
		if n, err := value.file.ReadAt(data, 0); err != nil || n != len(data) || hlsGeneratedAVPCMOverlapDecode(data, target) != nil || value.fence() != nil {
			t.Fatal("read unique complete bounded JSON evidence without changing identity")
		}
		report[name+"SHA256"] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	var rawDriver map[string]json.RawMessage
	readJSON("driver-result.json", &rawDriver)
	driverBytes, err := json.Marshal(rawDriver)
	if err != nil {
		t.Fatal("preserve bounded historical driver object")
	}
	driver, err := hlsGeneratedAVPCMOverlapHistorical(driverBytes)
	report["Stage"] = "historical_driver"
	if err != nil || driver.Marker != "goby-generated-av-pcm-content-association-v1" || driver.RunId != "av-pcm-content-01" || driver.CalibrationErrorType != "none" ||
		driver.NativeClockVersion != 0 || driver.SourceSeconds != 48 || driver.ProducerStartTicks != 0 || driver.ProducerEndTicks != 24*media.TicksPerSecond || driver.PhysicalSegmentSeconds != 6 ||
		driver.NaturalRenditions != 4 || driver.PhysicalCutsPerRendition != 4 || driver.ObservedVariant != 0 || driver.ActualPCMCaptureReceipts != 4 || driver.PlannedPCMCaptureInvocations != 4 ||
		driver.ProbeInvocationsPerObservedCut != 1 || len(driver.ContentFailureStages) != 0 || driver.ManagerScopeAfterClose != (transcode.ResourceUsage{}) || driver.MediaHelpersAfterClose != driver.MediaHelpersBefore ||
		driver.MediaHelpersAfterClose.Active != 0 || driver.MediaHelpersAfterClose.Background != 0 || driver.MediaHelpersAfterClose.Queued != 0 {
		t.Fatal("historical run marker, fixed oracle or completed ownership receipts disagree")
	}
	sourceHashBytes, err := hex.DecodeString(driver.EncodedSourceSHA256)
	var sourceHash [32]byte
	if err != nil || len(sourceHashBytes) != 32 || driver.EncodedSourceSHA256 != strings.ToLower(driver.EncodedSourceSHA256) {
		t.Fatal("historical encoded-source context is invalid")
	}
	copy(sourceHash[:], sourceHashBytes)
	if sourceHash == ([32]byte{}) {
		t.Fatal("historical encoded-source context cannot be unknown")
	}
	var sourceEffective transcode.GeneratedAVEffectiveDecodeDiagnostic
	var sourceCertificate transcode.GeneratedAVSourceCertificate
	readJSON("source-effective.json", &sourceEffective)
	readJSON("source-certificate.json", &sourceCertificate)
	if !sourceEffective.Complete || sourceEffective.Qualified || len(sourceEffective.InputSHA256) != 1 || sourceEffective.InputSHA256[0] != sourceHash ||
		sourceCertificate.SourceIdentity == "" || sourceCertificate.DurationTicks != 48*media.TicksPerSecond || sourceCertificate.Audio.SampleRate != 48000 || sourceCertificate.Audio.Channels != 2 {
		t.Fatal("historical source reference changed its encoded-byte context")
	}
	names := []string{"source", "cut-000", "cut-001", "continuous"}
	expectedSamples := []int64{2304000, 289792, 287744, 1153024}
	roles := []transcode.GeneratedAVPCMCaptureRole{transcode.GeneratedAVPCMCaptureSource, transcode.GeneratedAVPCMCaptureCut, transcode.GeneratedAVPCMCaptureCut, transcode.GeneratedAVPCMCaptureGroup}
	var captures [4]transcode.GeneratedAVPCMCaptureDiagnostic
	var pcm [4]*os.File
	if len(driver.PCMCaptures) != 4 || len(driver.PCMContentCandidates) != 9 {
		t.Fatal("historical run omitted fixed captures or candidate records")
	}
	for index, name := range names {
		readJSON(name+"-pcm-receipt.json", &captures[index])
		capture := captures[index]
		limit, parts := int64(8<<20), 1
		if index == 0 {
			limit = 24 << 20
		}
		if index == 3 {
			parts = 4
		}
		if !reflect.DeepEqual(capture, driver.PCMCaptures[index]) || !capture.Complete || !capture.PCM.Complete || !capture.CaptureWritten || capture.Stage != "complete" ||
			capture.Qualified || capture.NativeClockComplete || capture.DecodedOriginComplete || capture.ContentBound || capture.PCM.Qualified || capture.Role != roles[index] ||
			capture.LimitBytes != limit || capture.SourceSHA256 != sourceHash || capture.SourceIdentity != sourceCertificate.SourceIdentity || capture.SourceBytes != sourceEffective.InputBytes ||
			!capture.SourceIdentityUnchanged || !capture.InputIdentitiesUnchanged || len(capture.InputSHA256) != parts || len(capture.InputIdentities) != parts ||
			capture.PCM.Channels != 2 || capture.PCM.SampleRate != 48000 || capture.PCM.Samples != expectedSamples[index] || capture.PCM.Bytes != expectedSamples[index]*4 ||
			index == 0 && (capture.InputSHA256[0] != sourceHash || capture.PCM.Samples != sourceEffective.Audio.Samples) {
			t.Fatal("historical capture receipts disagree with fixed actual complete PCM extents")
		}
		value, err := hlsGeneratedAVPCMOverlapOpen(input, name+".pcm", limit)
		if err != nil {
			t.Fatal("reopen fixed private completed PCM capture")
		}
		owned, pcm[index] = append(owned, value), value.file
		if value.before.Size() != capture.PCM.Bytes || hlsGeneratedAVPCMOverlapHash(ctx, value, capture.PCM.SHA256) != nil {
			t.Fatal("actual full private PCM capture does not match its historical receipt")
		}
	}
	if captures[3].InputSHA256[0] != captures[1].InputSHA256[0] || captures[3].InputSHA256[1] != captures[2].InputSHA256[0] {
		t.Fatal("ordered continuous input receipts changed the first or adjacent encoded part")
	}
	checks := []struct {
		name                            string
		ref, query                      int
		first, queryFirst, displacement int64
		unique                          bool
	}{
		{"source_cut0_interior", 0, 1, 0, 4096, -1024, true}, {"source_cut1_interior", 0, 2, 288000, 4096, 288768, true},
		{"source_continuous_initial", 0, 3, 0, 4096, -1024, true}, {"source_continuous_seam", 0, 3, 283904, 292096, -1024, true},
		{"continuous_cut0_interior", 3, 1, 0, 4096, 0, true}, {"continuous_cut1_interior", 3, 2, 288000, 4096, 289792, true},
		{"continuous_cut1_startup", 3, 2, 284000, 0, 0, false}, {"wrong_source_position", 0, 1, 1152000, 4096, 0, false},
		{"swapped_channels", 0, 1, 0, 4096, 0, false},
	}
	report["Stage"] = "historical_candidates"
	for index, check := range checks {
		row, candidate := driver.PCMContentCandidates[index], driver.PCMContentCandidates[index].Candidate
		if row.Name != check.name || row.ErrorType != "<nil>" || !candidate.Complete || !candidate.CapturedBytesVerified || candidate.Qualified || candidate.NativeClockKnown || candidate.DecodedOriginComplete || candidate.ContentBound ||
			candidate.EncodedSourceSHA256 != sourceHash || candidate.ReferenceSHA256 != captures[check.ref].PCM.SHA256 || index < 8 && candidate.QuerySHA256 != captures[check.query].PCM.SHA256 ||
			candidate.ReferenceBytes != captures[check.ref].PCM.Bytes || candidate.QueryBytes != captures[check.query].PCM.Bytes || candidate.ReferenceFirstCandidateSample != check.first || candidate.QueryFirstSample != check.queryFirst ||
			candidate.Channels != 2 || candidate.CandidateCount != 8193 || candidate.WindowSamples != 4096 || candidate.SampleStride != 4 || candidate.ExclusionRadiusSamples != 32 || candidate.ComparedValuesPerChannel != 1024 ||
			candidate.MinimumCorrelation != 0.90 || candidate.MinimumMargin != 0.01 || !candidate.SecondCandidateKnown || math.IsNaN(candidate.BestMinimumCorrelation) || math.IsInf(candidate.BestMinimumCorrelation, 0) {
			t.Fatal("historical candidate names, hashes, ranges or fixed oracle changed")
		}
		if candidate.ReferenceSampleHypothesis < check.first || candidate.ReferenceSampleHypothesis > check.first+8192 || candidate.DisplacementSamples != candidate.ReferenceSampleHypothesis-candidate.QueryFirstSample ||
			candidate.BestChannelCorrelation[0] < -1 || candidate.BestChannelCorrelation[0] > 1 || candidate.BestChannelCorrelation[1] < -1 || candidate.BestChannelCorrelation[1] > 1 ||
			candidate.SecondMinimumCorrelation < -1 || candidate.SecondMinimumCorrelation > 1 || candidate.BestMinimumCorrelation != min(candidate.BestChannelCorrelation[0], candidate.BestChannelCorrelation[1]) || candidate.Margin != candidate.BestMinimumCorrelation-candidate.SecondMinimumCorrelation {
			t.Fatal("historical scores invented a competitor, range position or margin")
		}
		if index < 8 && (row.Accepted == nil || !*row.Accepted || row.RequiredUnique == nil || *row.RequiredUnique != check.unique || row.DerivedNegative != nil) ||
			index == 8 && (row.Accepted != nil || row.RequiredUnique != nil || row.DerivedNegative == nil || !*row.DerivedNegative) {
			t.Fatal("historical positive/negative candidate roles changed")
		}
		if check.unique {
			if candidate.Status != transcode.GeneratedAVAssociationUnique || candidate.DisplacementSamples != check.displacement || candidate.ReferenceSampleHypothesis-candidate.QueryFirstSample != check.displacement || candidate.Margin < 0.01 || candidate.BestMinimumCorrelation < 0.90 ||
				candidate.BestMinimumCorrelation != min(candidate.BestChannelCorrelation[0], candidate.BestChannelCorrelation[1]) || candidate.Margin != candidate.BestMinimumCorrelation-candidate.SecondMinimumCorrelation {
				t.Fatal("a required interior hypothesis lost actual unique candidate evidence")
			}
		} else if candidate.Status != transcode.GeneratedAVAssociationMissing || candidate.BestMinimumCorrelation >= 0.90 {
			t.Fatal("startup or wrong-content candidate was upgraded into a source origin")
		}
	}
	derived, err := hlsGeneratedAVPCMOverlapOpen(input, "derived-swapped-cut0.pcm", 8<<20)
	if err != nil {
		t.Fatal("reopen explicit derived negative PCM evidence")
	}
	owned = append(owned, derived)
	if derived.before.Size() != captures[1].PCM.Bytes || hlsGeneratedAVPCMOverlapHash(ctx, derived, driver.PCMContentCandidates[8].Candidate.QuerySHA256) != nil {
		t.Fatal("derived negative candidate hash changed its actual PCM bytes")
	}
	before := media.GetProcessCapacityStats()
	jobs := []struct {
		name                          string
		ref, query, candidate         int
		refFirst, queryFirst, samples int64
	}{
		{"continuous_cut0_full", 3, 1, 4, 0, 0, captures[1].PCM.Samples},
		{"continuous_cut1_full", 3, 2, 5, 289792, 0, captures[2].PCM.Samples},
		{"source_cut0_mapped", 0, 1, 0, 0, 1024, captures[1].PCM.Samples - 1024},
		{"source_cut1_full", 0, 2, 1, 288768, 0, captures[2].PCM.Samples},
		{"source_continuous_mapped", 0, 3, 2, 0, 1024, captures[3].PCM.Samples - 1024},
	}
	var shards []map[string]any
	var diagnosticErr error
	for _, job := range jobs {
		report["Stage"] = "overlap_" + job.name
		candidate := driver.PCMContentCandidates[job.candidate].Candidate
		if job.refFirst-job.queryFirst != candidate.DisplacementSamples {
			diagnosticErr = transcode.ErrInvalidInput
			break
		}
		result, err := transcode.CompareGeneratedAVPCMOverlapDifferenceDiagnostic(ctx, pcm[job.ref], pcm[job.query], transcode.GeneratedAVPCMOverlapDifferenceOptions{
			Channels: 2, SampleRate: 48000, ReferenceSHA256: captures[job.ref].PCM.SHA256, QuerySHA256: captures[job.query].PCM.SHA256, EncodedSourceSHA256: sourceHash,
			ReferenceFirstSample: job.refFirst, QueryFirstSample: job.queryFirst, ComparedSamples: job.samples,
		})
		name := job.name + "-difference.json"
		if hlsGeneratedAVDiagnosticWriteJSON(filepath.Join(output, name), result) != nil {
			diagnosticErr = errors.Join(diagnosticErr, transcode.ErrTimelineLimit)
		}
		shards = append(shards, map[string]any{"Name": job.name, "File": name, "HypothesisCandidate": candidate, "ErrorType": fmt.Sprintf("%T", err),
			"Complete": result.Complete, "BytesEqual": result.BytesEqual, "MismatchFrames": result.MismatchFrames, "UncomparedQueryPrefixSamples": result.UncomparedQueryPrefixSamples, "UncomparedQuerySuffixSamples": result.UncomparedQuerySuffixSamples})
		if err != nil || !result.Complete || !result.ComparedParsed || !result.CapturedBytesVerified || !result.DerivedHypothesis || result.Qualified || result.NativeClockKnown || result.DecodedOriginComplete || result.ContentBound ||
			result.WholeQueryCompared != (job.queryFirst == 0 && job.samples == captures[job.query].PCM.Samples) || result.UncomparedQueryPrefixSamples != job.queryFirst || result.UncomparedQuerySuffixSamples != 0 {
			diagnosticErr = errors.Join(diagnosticErr, err, transcode.ErrInvalidInput)
		}
	}
	unchanged := true
	report["Stage"] = "final_input_fences"
	for _, value := range owned {
		unchanged = unchanged && value.fence() == nil
	}
	after := media.GetProcessCapacityStats()
	report["HistoricalInputRunId"], report["HistoricalEncodedSourceSHA256"] = driver.RunId, driver.EncodedSourceSHA256
	report["HistoricalCandidatesValidated"], report["StartupCandidateWasUsed"] = true, false
	report["OwnedInputReaderCount"] = len(owned)
	report["OverlapShards"], report["AllInputIdentitiesAndOffsetsUnchanged"] = shards, unchanged
	report["MediaHelpersBefore"], report["MediaHelpersAfter"] = before, after
	report["DiagnosticErrorType"] = fmt.Sprintf("%T", diagnosticErr)
	closeErr := closeOwned()
	if !unchanged || closeErr != nil || after != before || diagnosticErr != nil || len(shards) != 5 {
		t.Fatal("artifact overlap diagnostic failed after closing owned input readers")
	}
	report["Complete"] = true
	report["Stage"] = "complete"
	t.Log("actual_pcm_artifact_overlap_complete=true live_encoded_source_held=false content_bound=false native_clock_known=false qualified=false")
}
