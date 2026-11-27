package media

import (
	"context"
	"fmt"
	"os"
	"slices"
)

// DolbyVisionVerificationVersion identifies the complete RPU verification
// algorithm, independently of the probe, evidence storage, and executable.
const DolbyVisionVerificationVersion = 1

const (
	maxDolbyVisionIngestStreams = 32
	maxDolbyVisionStreamIndex   = 4095
	// A proved access unit contains an AUD and an RPU, each with at
	// least a three-byte Annex B start code and a three-byte NAL.
	maxDolbyVisionRPUFrames = maxDolbyVisionProbeOutput / 12
)

// DolbyVisionRPUEvidence records only physical RPU facts for one stream, not
// its declared configuration, source identity, or eligibility for conversion.
// Verified means both passes finished; anomalies may still prevent projection.
// Persistence must bind this result to the accepted source and verifier tool.
type DolbyVisionRPUEvidence struct {
	StreamIndex         int
	Verified            bool
	Profile             int
	ResidualMixed       bool
	ResidualDisabled    bool
	FrameCount          int64
	InBandParameterSets bool
	Reason              string
}

// Valid checks the closed result shape without consulting the declaration.
func (e DolbyVisionRPUEvidence) Valid() bool {
	if !validDolbyVisionStreamIndex(e.StreamIndex) || e.FrameCount < 0 || e.FrameCount > maxDolbyVisionRPUFrames {
		return false
	}
	if !e.Verified {
		if e.Profile != 0 || e.FrameCount != 0 || e.ResidualDisabled || e.ResidualMixed || e.InBandParameterSets {
			return false
		}
		switch e.Reason {
		case dolbyVisionRPUUnsupported, dolbyVisionRPUOutputLimit, dolbyVisionRPUTimeout, dolbyVisionRPUScanFailed,
			dolbyVisionRPUDecoderError, dolbyVisionRPUInvalidScan, dolbyVisionRPUNoFrames, dolbyVisionRPUMetadataMissing, dolbyVisionRPUResidualUnknown:
			return true
		default:
			return false
		}
	}
	if e.FrameCount == 0 || e.ResidualMixed && (e.ResidualDisabled || e.FrameCount < 2) {
		return false
	}
	switch e.Profile {
	case 0, 5, 8:
	case 4, 7:
		// The parser can infer these profiles only from enabled residuals.
		if e.ResidualDisabled || e.ResidualMixed {
			return false
		}
	default:
		return false
	}
	switch e.Reason {
	case "":
		return e.ResidualDisabled
	case dolbyVisionRPUResidualEnabled:
		return !e.ResidualDisabled && !e.ResidualMixed
	case dolbyVisionRPUResidualMixed:
		return e.ResidualMixed
	case dolbyVisionRPUProfileMixed:
		return e.Profile == 0 && e.FrameCount > 1
	default:
		return false
	}
}

// Retryable reports whether the scan ended without a deterministic result.
func (e DolbyVisionRPUEvidence) Retryable() bool {
	return (e.Reason == dolbyVisionRPUTimeout || e.Reason == dolbyVisionRPUScanFailed) && e.Valid()
}

func validDolbyVisionStreamIndex(index int) bool {
	return index >= 0 && index <= maxDolbyVisionStreamIndex
}

func dolbyVisionStreamIndexCounts(streams []Stream) (counts [maxDolbyVisionStreamIndex + 1]uint8) {
	for _, stream := range streams {
		index := stream.Index
		if validDolbyVisionStreamIndex(index) && counts[index] < 2 {
			counts[index]++
		}
	}
	return counts
}

func dolbyVisionIngestStreamDeclared(stream Stream) bool {
	return stream.CodecType == "video" && stream.VideoRangeKnown && stream.VideoRange == "DOVI" &&
		stream.DolbyVision != nil && !stream.IsExternal && !stream.IsAttachedPicture
}

func dolbyVisionIngestStreamSupported(stream Stream) bool {
	return dolbyVisionIngestStreamDeclared(stream) && DolbyVisionRPUVerificationSupported(stream)
}

// PendingDolbyVisionStreams returns up to 32 unambiguous original stream
// indexes in probe order. Complete or deterministic scans do not queue again.
func PendingDolbyVisionStreams(info Info) []int {
	if info.ProbeVersion != CurrentProbeVersion {
		return nil
	}
	counts := dolbyVisionStreamIndexCounts(info.Streams)
	var pending []int
	for _, stream := range info.Streams {
		if !validDolbyVisionStreamIndex(stream.Index) || counts[stream.Index] != 1 || !dolbyVisionIngestStreamSupported(stream) {
			continue
		}
		metadata := stream.DolbyVision
		existing := DolbyVisionRPUEvidence{
			StreamIndex: stream.Index, Verified: metadata.RPUVerified, Profile: metadata.RPUProfile,
			ResidualMixed: metadata.RPUResidualMixed, ResidualDisabled: metadata.ResidualDisabled,
			FrameCount: metadata.RPUFrameCount, InBandParameterSets: metadata.InBandParameterSets, Reason: metadata.RPUValidationReason,
		}
		if existing == (DolbyVisionRPUEvidence{StreamIndex: stream.Index}) || existing.Retryable() {
			pending = append(pending, stream.Index)
			if len(pending) == maxDolbyVisionIngestStreams {
				break
			}
		}
	}
	return pending
}

// ApplyDolbyVisionRPUEvidence projects compatible positive evidence onto a
// current probe without changing any declared facts or replacing older proof.
// The caller must already have matched the evidence to this source revision,
// verification version, and verifier tool.
func ApplyDolbyVisionRPUEvidence(info Info, evidence []DolbyVisionRPUEvidence) Info {
	if info.ProbeVersion != CurrentProbeVersion || len(evidence) == 0 {
		return info
	}
	counts := dolbyVisionStreamIndexCounts(info.Streams)
	var evidenceCounts [maxDolbyVisionStreamIndex + 1]uint8
	for _, result := range evidence {
		if i := result.StreamIndex; validDolbyVisionStreamIndex(i) && evidenceCounts[i] < 2 {
			evidenceCounts[i]++
		}
	}
	positions := make(map[int]int)
	for position, stream := range info.Streams {
		if validDolbyVisionStreamIndex(stream.Index) && counts[stream.Index] == 1 && dolbyVisionIngestStreamSupported(stream) && !stream.DolbyVision.RPUVerified {
			positions[stream.Index] = position
		}
	}
	cloned := false
	for _, result := range evidence {
		position, ok := positions[result.StreamIndex]
		if !ok || evidenceCounts[result.StreamIndex] != 1 || !result.Verified || !result.Valid() {
			continue
		}
		metadata := info.Streams[position].DolbyVision
		if result.Profile != metadata.Profile || result.ResidualMixed || result.ResidualDisabled != (metadata.Profile != 7) {
			continue
		}
		if !cloned {
			info.Streams = slices.Clone(info.Streams)
			cloned = true
		}
		info.Streams[position].DolbyVision = result.applyTo(metadata)
	}
	return info
}

func (e DolbyVisionRPUEvidence) applyTo(metadata *DolbyVisionMetadata) *DolbyVisionMetadata {
	result := *metadata
	result.RPUVerified, result.RPUProfile = e.Verified, e.Profile
	result.RPUResidualMixed, result.ResidualDisabled = e.ResidualMixed, e.ResidualDisabled
	result.RPUFrameCount, result.RPUValidationReason = e.FrameCount, e.Reason
	result.InBandParameterSets = e.InBandParameterSets
	return &result
}

// VerifyDolbyVisionIngest runs the complete verification of one indexed stream
// without reprobing the source. It borrows the accepted regular descriptor
// without closing or seeking it. The caller retains the source until all readers
// retire and must treat an unknown retirement as an ownership failure.
func VerifyDolbyVisionIngest(ctx context.Context, executable string, file *os.File, info Info, streamIndex int) (DolbyVisionRPUEvidence, error) {
	if ctx == nil {
		return DolbyVisionRPUEvidence{}, fmt.Errorf("Dolby Vision verification context is nil")
	}
	if err := ctx.Err(); err != nil {
		return DolbyVisionRPUEvidence{}, err
	}
	if info.ProbeVersion != CurrentProbeVersion || file == nil || !validDolbyVisionStreamIndex(streamIndex) {
		return DolbyVisionRPUEvidence{}, fmt.Errorf("invalid Dolby Vision verification source or stream index")
	}
	var selected *Stream
	for i := range info.Streams {
		stream := &info.Streams[i]
		if stream.Index != streamIndex {
			continue
		}
		if selected != nil || !dolbyVisionIngestStreamDeclared(*stream) {
			return DolbyVisionRPUEvidence{}, fmt.Errorf("ambiguous or invalid Dolby Vision stream index %d", streamIndex)
		}
		selected = stream
	}
	if selected == nil {
		return DolbyVisionRPUEvidence{}, fmt.Errorf("no Dolby Vision declaration for stream %d", streamIndex)
	}

	var result DolbyVisionRPUEvidence
	err := RunSourceReadPhase(WithBackgroundProcess(ctx), func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		before, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat media file before Dolby Vision verification: %w", err)
		}
		changeTime := FileChangeTime(before)
		if !before.Mode().IsRegular() || before.Size() != info.Size || changeTime == 0 || changeTime != info.FileChangeTimeNs {
			return fmt.Errorf("media file does not match the accepted Dolby Vision source")
		}
		timeout := dolbyVisionRPUScanTimeout(before.Size())
		scanContext, cancel := context.WithTimeout(work, timeout)
		defer cancel()
		evidence, err := scanDolbyVisionRPUStream(work, scanContext, timeout, executable, file, *selected)
		if err != nil {
			return err
		}
		after, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat media file after Dolby Vision verification: %w", err)
		}
		if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || changeTime != FileChangeTime(after) {
			return fmt.Errorf("media file changed during Dolby Vision verification")
		}
		if err := work.Err(); err != nil {
			return err
		}
		result = evidence
		return nil
	})
	if err != nil {
		return DolbyVisionRPUEvidence{}, err
	}
	if err := ctx.Err(); err != nil {
		return DolbyVisionRPUEvidence{}, err
	}
	if !result.Valid() {
		return DolbyVisionRPUEvidence{}, fmt.Errorf("invalid Dolby Vision verification result")
	}
	return result, nil
}
