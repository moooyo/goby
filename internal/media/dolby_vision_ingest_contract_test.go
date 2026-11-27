package media

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func ingestContractDolbyVisionStream(index, profile int, compression string) Stream {
	compatibility := 1
	if profile == 5 {
		compatibility = 0
	} else if profile == 7 {
		compatibility = 6
	}
	return Stream{Index: index, Codec: "hevc", CodecType: "video", BitDepth: 10, PixelFormat: "yuv420p10le",
		VideoRange: "DOVI", VideoRangeKnown: true,
		DolbyVision: &DolbyVisionMetadata{Profile: profile, Level: 6, RPUPresent: true, BLPresent: true,
			ELPresent: profile == 7, CompatibilityID: compatibility, MetadataCompression: compression}}
}

func TestDolbyVisionIngestPendingStreamsUseCurrentUnambiguousDeclarations(t *testing.T) {
	info := Info{ProbeVersion: CurrentProbeVersion, Streams: []Stream{
		ingestContractDolbyVisionStream(19, 8, "extended"),
		ingestContractDolbyVisionStream(7, 7, "none"),
		ingestContractDolbyVisionStream(5, 5, "none"),
		ingestContractDolbyVisionStream(8, 8, "limited"),
		ingestContractDolbyVisionStream(13, 8, "none"),
		ingestContractDolbyVisionStream(6, 8, "none"),
		ingestContractDolbyVisionStream(2, 8, "none"),
		ingestContractDolbyVisionStream(15, 8, "none"),
		ingestContractDolbyVisionStream(21, 8, "none"),
		ingestContractDolbyVisionStream(22, 8, "none"),
		ingestContractDolbyVisionStream(23, 8, "none"),
		ingestContractDolbyVisionStream(24, 8, "none"),
		ingestContractDolbyVisionStream(25, 7, "limited"),
		ingestContractDolbyVisionStream(26, 8, "reserved"),
		ingestContractDolbyVisionStream(27, 4, "none"),
		ingestContractDolbyVisionStream(-1, 8, "none"),
		ingestContractDolbyVisionStream(4096, 8, "none"),
		{Index: 13, CodecType: "audio"},
		{Index: 20, Codec: "hevc", CodecType: "video", VideoRange: "DOVI", VideoRangeKnown: true},
		ingestContractDolbyVisionStream(28, 8, "none"),
		ingestContractDolbyVisionStream(29, 8, "none"),
	}}
	info.Streams[19].VideoRangeKnown = false
	info.Streams[20].DolbyVision.RPUFrameCount = 100
	info.Streams[20].DolbyVision.RPUValidationReason = dolbyVisionRPUTimeout
	info.Streams[2].DolbyVision.RPUValidationReason = dolbyVisionRPUTimeout
	info.Streams[3].DolbyVision.RPUValidationReason = dolbyVisionRPUScanFailed
	info.Streams[5].DolbyVision.RPUVerified = true
	info.Streams[6].DolbyVision.RPUValidationReason = dolbyVisionRPUInvalidScan
	info.Streams[7].DolbyVision.RPUValidationReason = dolbyVisionRPUResidualUnknown
	info.Streams[8].IsAttachedPicture = true
	info.Streams[9].IsExternal = true
	info.Streams[10].Codec = "h264"
	info.Streams[11].DolbyVision.BLPresent = false
	if got, want := PendingDolbyVisionStreams(info), []int{19, 7, 5, 8}; !slices.Equal(got, want) {
		t.Fatalf("pending RPU stream indexes = %v, want %v", got, want)
	}
	info.ProbeVersion--
	if got := PendingDolbyVisionStreams(info); len(got) != 0 {
		t.Fatalf("previous probe version queued Dolby Vision work: %v", got)
	}
}

func TestDolbyVisionIngestPendingStreamsAreBounded(t *testing.T) {
	info := Info{ProbeVersion: CurrentProbeVersion}
	for i := range maxDolbyVisionIngestStreams + 1 {
		info.Streams = append(info.Streams, ingestContractDolbyVisionStream(maxDolbyVisionStreamIndex-i, 8, "none"))
	}
	got := PendingDolbyVisionStreams(info)
	if len(got) != maxDolbyVisionIngestStreams || got[0] != maxDolbyVisionStreamIndex ||
		got[len(got)-1] != maxDolbyVisionStreamIndex-maxDolbyVisionIngestStreams+1 {
		t.Fatalf("pending indexes are unbounded or reordered: %v", got)
	}
}

func TestDolbyVisionIngestEvidenceValidatesClosedPhysicalOutcomes(t *testing.T) {
	for _, test := range []struct {
		name         string
		result       DolbyVisionRPUEvidence
		valid, retry bool
	}{
		{"verified profile 8", DolbyVisionRPUEvidence{Verified: true, Profile: 8, FrameCount: 2, ResidualDisabled: true, InBandParameterSets: true}, true, false},
		{"verified profile 7", DolbyVisionRPUEvidence{Verified: true, Profile: 7, FrameCount: 2, Reason: dolbyVisionRPUResidualEnabled}, true, false},
		{"verified mixed profile", DolbyVisionRPUEvidence{Verified: true, FrameCount: 2, ResidualMixed: true, Reason: dolbyVisionRPUProfileMixed}, true, false},
		{"verified mixed residual", DolbyVisionRPUEvidence{Verified: true, FrameCount: 2, Profile: 8, ResidualMixed: true, Reason: dolbyVisionRPUResidualMixed}, true, false},
		{"timeout", DolbyVisionRPUEvidence{Reason: dolbyVisionRPUTimeout}, true, true},
		{"scan failed", DolbyVisionRPUEvidence{Reason: dolbyVisionRPUScanFailed}, true, true},
		{"unsupported", DolbyVisionRPUEvidence{Reason: dolbyVisionRPUUnsupported}, true, false},
		{"invalid scan", DolbyVisionRPUEvidence{Reason: dolbyVisionRPUInvalidScan}, true, false},
		{"missing RPU", DolbyVisionRPUEvidence{Reason: dolbyVisionRPUMetadataMissing}, true, false},
		{"unknown reason", DolbyVisionRPUEvidence{Reason: "arbitrary decoder diagnostic"}, false, false},
		{"unknown result", DolbyVisionRPUEvidence{}, false, false},
		{"timeout retained proof", DolbyVisionRPUEvidence{Verified: true, Profile: 8, FrameCount: 10, ResidualDisabled: true, Reason: dolbyVisionRPUTimeout}, false, false},
		{"timeout retained partial scan", DolbyVisionRPUEvidence{FrameCount: 10, Reason: dolbyVisionRPUTimeout}, false, false},
		{"unsupported with in-band parameters", DolbyVisionRPUEvidence{InBandParameterSets: true, Reason: dolbyVisionRPUUnsupported}, false, false},
		{"profile 7 does not imply disabled residuals", DolbyVisionRPUEvidence{Verified: true, Profile: 7, FrameCount: 10, ResidualDisabled: true}, false, false},
		{"mixed evidence from one frame", DolbyVisionRPUEvidence{Verified: true, Profile: 8, FrameCount: 1, ResidualMixed: true, Reason: dolbyVisionRPUResidualMixed}, false, false},
		{"mixed evidence with all residuals disabled", DolbyVisionRPUEvidence{Verified: true, Profile: 8, FrameCount: 2, ResidualMixed: true, ResidualDisabled: true, Reason: dolbyVisionRPUResidualMixed}, false, false},
		{"out of bounds", DolbyVisionRPUEvidence{Verified: true, Profile: 8, FrameCount: maxDolbyVisionProbeOutput, ResidualDisabled: true}, false, false},
		{"invalid stream index", DolbyVisionRPUEvidence{StreamIndex: -1, Reason: dolbyVisionRPUTimeout}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.result.Valid() != test.valid || test.result.Retryable() != test.retry {
				t.Fatalf("incorrect validation for %+v: valid=%t, retryable=%t", test.result, test.result.Valid(), test.result.Retryable())
			}
		})
	}
}

func TestDolbyVisionIngestApplyOnlyCompatiblePositiveEvidence(t *testing.T) {
	info := Info{ProbeVersion: CurrentProbeVersion, Container: "matroska", Chapters: []Chapter{{Title: "Chapter"}}, Streams: []Stream{
		{Index: 0, Codec: "aac", CodecType: "audio"},
		ingestContractDolbyVisionStream(4, 8, "none"),
		ingestContractDolbyVisionStream(7, 7, "none"),
		ingestContractDolbyVisionStream(9, 8, "none"),
		ingestContractDolbyVisionStream(12, 8, "none"),
		{Index: 13, Codec: "hevc", CodecType: "video", VideoRange: "DOVI", VideoRangeKnown: true},
	}}
	legacy := info.Streams[3].DolbyVision
	legacy.RPUVerified, legacy.RPUProfile, legacy.RPUFrameCount, legacy.ResidualDisabled = true, 8, 48, true
	positive := DolbyVisionRPUEvidence{StreamIndex: 4, Verified: true, Profile: 8, FrameCount: 96, ResidualDisabled: true, InBandParameterSets: true}
	for _, test := range []struct {
		name   string
		change func(*DolbyVisionRPUEvidence)
	}{
		{"wrong stream", func(e *DolbyVisionRPUEvidence) { e.StreamIndex = 0 }},
		{"wrong profile", func(e *DolbyVisionRPUEvidence) { e.Profile = 5 }},
		{"malformed proof", func(e *DolbyVisionRPUEvidence) { e.FrameCount = 0 }},
		{"enabled residual for profile 8", func(e *DolbyVisionRPUEvidence) { e.ResidualDisabled, e.Reason = false, dolbyVisionRPUResidualEnabled }},
		{"mixed residuals", func(e *DolbyVisionRPUEvidence) {
			e.ResidualDisabled, e.ResidualMixed, e.Reason = false, true, dolbyVisionRPUResidualMixed
		}},
		{"conflicting declaration", func(e *DolbyVisionRPUEvidence) { e.StreamIndex = 13 }},
		{"terminal extraction failure", func(e *DolbyVisionRPUEvidence) {
			*e = DolbyVisionRPUEvidence{StreamIndex: e.StreamIndex, Reason: dolbyVisionRPUInvalidScan}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := positive
			test.change(&e)
			got := ApplyDolbyVisionRPUEvidence(info, []DolbyVisionRPUEvidence{e})
			if !reflect.DeepEqual(info, got) || info.Streams[1].DolbyVision.RPUVerified {
				t.Fatalf("incompatible evidence changed declared/observed facts: %+v", e)
			}
		})
	}
	if got := ApplyDolbyVisionRPUEvidence(info, []DolbyVisionRPUEvidence{positive, {StreamIndex: positive.StreamIndex, Reason: dolbyVisionRPUInvalidScan}}); !reflect.DeepEqual(info, got) {
		t.Fatal("conflicting results for a stream projected positive proof")
	}
	oldUpdate := positive
	oldUpdate.StreamIndex = 9
	profile7 := DolbyVisionRPUEvidence{StreamIndex: 7, Verified: true, Profile: 7, FrameCount: 72, Reason: dolbyVisionRPUResidualEnabled}
	got := ApplyDolbyVisionRPUEvidence(info, []DolbyVisionRPUEvidence{positive, profile7, oldUpdate})
	if &info.Streams[0] == &got.Streams[0] || info.Streams[1].DolbyVision == got.Streams[1].DolbyVision || info.Streams[2].DolbyVision == got.Streams[2].DolbyVision {
		t.Fatal("applied proof aliases the original stream slice or metadata")
	}
	if !DolbyVisionConversionSupported(got.Streams[1]) || !got.Streams[1].DolbyVision.InBandParameterSets ||
		!DolbyVisionConversionSupported(got.Streams[2]) || got.Streams[2].DolbyVision.ResidualDisabled ||
		got.Streams[3].DolbyVision != legacy || legacy.RPUFrameCount != 48 || info.Streams[1].DolbyVision.RPUVerified || info.Streams[2].DolbyVision.RPUVerified {
		t.Fatalf("applied proof failed to preserve source facts and earlier proof: %+v", got.Streams)
	}
	for _, i := range []int{1, 2} {
		stream := got.Streams[i]
		stream.DolbyVision = info.Streams[i].DolbyVision
		if !reflect.DeepEqual(stream, info.Streams[i]) {
			t.Fatalf("applied proof changed stream facts or index %d", i)
		}
	}
	if got.Container != info.Container || !slices.Equal(got.Chapters, info.Chapters) || got.Streams[5].VideoRange != "DOVI" {
		t.Fatalf("applied RPU evidence rewrote unrelated probe facts: %+v", got)
	}
}

func TestDolbyVisionIngestRetirementUnknownTakesPrecedence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, processErr := range map[string]error{
		"output limit":   errors.Join(ErrProcessRetirementUnknown, ErrOutputLimit),
		"parser failure": errors.Join(ErrProcessRetirementUnknown, errors.New("test parser failure")),
		"scan timeout":   errors.Join(ErrProcessRetirementUnknown, context.DeadlineExceeded),
	} {
		t.Run(name, func(t *testing.T) {
			// Unknown retirement means the copier might still own both sinks,
			// so the failure path must not inspect either one.
			for _, parent := range []context.Context{ctx, context.Background()} {
				err := dolbyVisionStreamProcessFailure(parent, parent, nil, nil, processErr)
				if !errors.Is(err, ErrProcessRetirementUnknown) || parent.Err() != nil && !errors.Is(err, parent.Err()) {
					t.Fatalf("stream failure hid unknown retirement or parent cancellation: %v", err)
				}
				reason, err := dolbyVisionRPUProcessFailure(parent, parent, err)
				if reason != "" || !errors.Is(err, ErrProcessRetirementUnknown) {
					t.Fatalf("unknown retirement produced a verification result: %q, %v", reason, err)
				}
			}
		})
	}
}
