package media

import (
	"encoding/binary"
	"errors"
	"math"
)

// These are fixed content-policy budgets, not claims about an unexecuted codec
// or GPU. Any future change requires new sample-specific validation evidence.
const (
	DiagnosticContentPolicyVersion      = 1
	DiagnosticVideoMAELimit             = 8
	DiagnosticVideoRMSELimit            = 16
	DiagnosticVideoTileMAELimit         = 18
	DiagnosticAudioAlignmentLimit       = 4096
	DiagnosticAudioPaddingLimit         = 4096
	DiagnosticAudioPaddingPeakLimit     = 4096
	diagnosticAudioBlockFrames          = 256
	diagnosticAudioGlobalErrorNumerator = 225  // Squared normalized RMSE: 0.15^2.
	diagnosticAudioBlockErrorNumerator  = 1225 // Squared normalized RMSE: 0.35^2.
	diagnosticAudioPaddingNumerator     = 144  // Squared relative padding RMS: 0.12^2.
	diagnosticAudioErrorDenominator     = 10000
)

var ErrDiagnosticContent = errors.New("diagnostic_content_invalid")

// Code is one of the fixed codes emitted below. Error text never incorporates
// supplied metadata, decoded samples, tool output, or filesystem identifiers.
type DiagnosticContentError struct {
	Code string
}

func (err *DiagnosticContentError) Error() string { return err.Code }
func (err *DiagnosticContentError) Unwrap() error { return ErrDiagnosticContent }

func diagnosticContentError(code string) error {
	return &DiagnosticContentError{Code: code}
}

type DiagnosticVideoPlaneMetrics struct {
	MeanAbsoluteError     float64
	RootMeanSquareError   float64
	WorstTileMeanAbsError float64
}

type DiagnosticVideoFrameMetrics struct {
	Frame              int
	BestReferenceFrame int
	Planes             [3]DiagnosticVideoPlaneMetrics
}

// Metrics are bounded to eight fixed frames. Content validation does not prove
// a codec, frame rate, process identity, or hardware path was actually used.
type DiagnosticVideoContent struct {
	PolicyVersion   int
	Width           int
	Height          int
	Frames          int
	PixelFormat     string
	ReferenceSHA256 string
	FrameMetrics    [DiagnosticVideoFrames]DiagnosticVideoFrameMetrics
}

// ValidateDiagnosticVideoContent compares against the independently generated
// original fixture. No caller-supplied or newly decoded output can become the
// reference. Plane-wide and local-tile budgets tolerate lossy coding while
// preventing a damaged marker or blank region from hiding in a global average.
func ValidateDiagnosticVideoContent(decoded []byte, width, height int, pixelFormat string) (DiagnosticVideoContent, error) {
	result := DiagnosticVideoContent{PolicyVersion: DiagnosticContentPolicyVersion}
	if width != DiagnosticWidth || height != DiagnosticHeight || pixelFormat != "yuv420p" || len(decoded) != DiagnosticVideoBytes {
		return result, diagnosticContentError("diagnostic_video_shape")
	}
	reference, err := GenerateDiagnosticSample(DiagnosticRawVideo)
	if err != nil {
		return result, diagnosticContentError("diagnostic_reference_unavailable")
	}
	result.Width, result.Height, result.Frames = width, height, DiagnosticVideoFrames
	result.PixelFormat, result.ReferenceSHA256 = pixelFormat, reference.SHA256
	wrongOrder, invalidContent := false, false
	for frame := 0; frame < DiagnosticVideoFrames; frame++ {
		observed := decoded[frame*DiagnosticVideoFrameBytes : (frame+1)*DiagnosticVideoFrameBytes]
		bestFrame, bestError := 0, int64(math.MaxInt64)
		for candidate := 0; candidate < DiagnosticVideoFrames; candidate++ {
			original := reference.Data[candidate*DiagnosticVideoFrameBytes : (candidate+1)*DiagnosticVideoFrameBytes]
			var squared int64
			for index, value := range observed {
				difference := int64(value) - int64(original[index])
				squared += difference * difference
			}
			if squared < bestError {
				bestFrame, bestError = candidate, squared
			}
		}
		original := reference.Data[frame*DiagnosticVideoFrameBytes : (frame+1)*DiagnosticVideoFrameBytes]
		planes, accepted := diagnosticVideoFrameContent(observed, original)
		result.FrameMetrics[frame] = DiagnosticVideoFrameMetrics{Frame: frame, BestReferenceFrame: bestFrame, Planes: planes}
		if bestFrame != frame {
			best := reference.Data[bestFrame*DiagnosticVideoFrameBytes : (bestFrame+1)*DiagnosticVideoFrameBytes]
			_, matchesOtherFrame := diagnosticVideoFrameContent(observed, best)
			wrongOrder = wrongOrder || matchesOtherFrame
			invalidContent = invalidContent || !matchesOtherFrame
		} else {
			invalidContent = invalidContent || !accepted
		}
	}
	if invalidContent {
		return result, diagnosticContentError("diagnostic_video_content")
	}
	if wrongOrder {
		return result, diagnosticContentError("diagnostic_video_frame_order")
	}
	return result, nil
}

func diagnosticVideoFrameContent(observed, reference []byte) ([3]DiagnosticVideoPlaneMetrics, bool) {
	var metrics [3]DiagnosticVideoPlaneMetrics
	start, accepted := 0, true
	for plane := 0; plane < 3; plane++ {
		width, height := DiagnosticWidth, DiagnosticHeight
		if plane != 0 {
			width, height = width/2, height/2
		}
		count := width * height
		var absolute, squared, worstTile int64
		for top := 0; top < height; top += 16 {
			for left := 0; left < width; left += 16 {
				var tileAbsolute int64
				for y := top; y < top+16; y++ {
					for x := left; x < left+16; x++ {
						position := start + y*width + x
						difference := int64(observed[position]) - int64(reference[position])
						squared += difference * difference
						if difference < 0 {
							difference = -difference
						}
						tileAbsolute += difference
					}
				}
				absolute += tileAbsolute
				worstTile = max(worstTile, tileAbsolute)
			}
		}
		metrics[plane] = DiagnosticVideoPlaneMetrics{MeanAbsoluteError: float64(absolute) / float64(count),
			RootMeanSquareError: math.Sqrt(float64(squared) / float64(count)), WorstTileMeanAbsError: float64(worstTile) / 256}
		accepted = accepted && absolute <= int64(DiagnosticVideoMAELimit*count) &&
			squared <= int64(DiagnosticVideoRMSELimit*DiagnosticVideoRMSELimit*count) && worstTile <= DiagnosticVideoTileMAELimit*256
		start += count
	}
	return metrics, accepted
}

// OffsetFrames is a waveform alignment candidate, not a claim about an AAC
// header's encoder delay. On error the metrics describe the closest candidate;
// only a nil error establishes that its content and padding passed this policy.
type DiagnosticAudioContent struct {
	PolicyVersion        int
	SampleRate           int
	Channels             int
	SampleFrames         int
	ReferenceFrames      int
	ReferenceSHA256      string
	CandidateOffsets     int
	OffsetFrames         int
	TrailingFrames       int
	NormalizedRMSE       [2]float64
	WorstBlockNRMSE      [2]float64
	SignalRMS            [2]float64
	LeadingPaddingRMS    [2]float64
	TrailingPaddingRMS   [2]float64
	WorstPaddingBlockRMS [2]float64
	PaddingPeak          [2]int
}

// ValidateDiagnosticAudioContent searches at most 4097 whole stereo-frame
// offsets against the fixed original PCM. The leading and trailing bounds are
// acceptance-policy limits, not assumed priming/padding lengths. Every reference
// frame, including both fades, participates in the content-error measurement.
// Bounded differences are permitted; a pass is not sample-exact completeness
// or a substitute for independent packet, duration and stage-reference facts.
func ValidateDiagnosticAudioContent(decoded []byte, sampleRate, channels int, sampleFormat string) (DiagnosticAudioContent, error) {
	result := DiagnosticAudioContent{PolicyVersion: DiagnosticContentPolicyVersion}
	const frameBytes = DiagnosticAudioChannels * 2
	if sampleRate != DiagnosticSampleRate || channels != DiagnosticAudioChannels || sampleFormat != "s16le" ||
		len(decoded)%frameBytes != 0 || len(decoded) < DiagnosticAudioBytes || len(decoded) >= DiagnosticAudioFramesLimit*frameBytes {
		return result, diagnosticContentError("diagnostic_audio_shape")
	}
	reference, err := GenerateDiagnosticSample(DiagnosticRawAudio)
	if err != nil {
		return result, diagnosticContentError("diagnostic_reference_unavailable")
	}
	observed, original := diagnosticPCM(decoded), diagnosticPCM(reference.Data)
	result.SampleRate, result.Channels = sampleRate, channels
	result.SampleFrames, result.ReferenceFrames = len(decoded)/frameBytes, DiagnosticAudioSampleFrames
	result.ReferenceSHA256 = reference.SHA256
	var referenceEnergy [2]int64
	for frame := 0; frame < DiagnosticAudioSampleFrames; frame++ {
		for channel := 0; channel < 2; channel++ {
			value := int64(original[frame*2+channel])
			referenceEnergy[channel] += value * value
		}
	}
	first := max(0, result.SampleFrames-DiagnosticAudioSampleFrames-DiagnosticAudioPaddingLimit)
	last := min(DiagnosticAudioAlignmentLimit, result.SampleFrames-DiagnosticAudioSampleFrames)
	result.CandidateOffsets = last - first + 1
	bestOffset, bestError, bestAcceptedError := first, int64(math.MaxInt64), int64(math.MaxInt64)
	var bestSquared [2]int64
	var accepted DiagnosticAudioContent
	for offset := first; offset <= last; offset++ {
		var squared [2]int64
		for frame := 0; frame < DiagnosticAudioSampleFrames; frame++ {
			for channel := 0; channel < 2; channel++ {
				difference := int64(observed[(offset+frame)*2+channel]) - int64(original[frame*2+channel])
				squared[channel] += difference * difference
			}
		}
		total := squared[0] + squared[1]
		if total < bestError {
			bestOffset, bestError, bestSquared = offset, total, squared
		}
		if total >= bestAcceptedError || !diagnosticAudioGlobalError(squared, referenceEnergy) {
			continue
		}
		candidate, code := diagnosticAudioCandidate(result, observed, original, referenceEnergy, squared, offset)
		if code == "" {
			accepted, bestAcceptedError = candidate, total
		}
	}
	if bestAcceptedError != math.MaxInt64 {
		return accepted, nil
	}
	result, code := diagnosticAudioCandidate(result, observed, original, referenceEnergy, bestSquared, bestOffset)
	if code == "" {
		code = "diagnostic_audio_content"
	}
	return result, diagnosticContentError(code)
}

func diagnosticPCM(data []byte) []int32 {
	values := make([]int32, len(data)/2)
	for index := range values {
		values[index] = int32(int16(binary.LittleEndian.Uint16(data[index*2:])))
	}
	return values
}

func diagnosticAudioGlobalError(squared, referenceEnergy [2]int64) bool {
	for channel := 0; channel < 2; channel++ {
		if squared[channel]*diagnosticAudioErrorDenominator > referenceEnergy[channel]*diagnosticAudioGlobalErrorNumerator {
			return false
		}
	}
	return true
}

func diagnosticAudioCandidate(result DiagnosticAudioContent, observed, reference []int32, referenceEnergy, squared [2]int64, offset int) (DiagnosticAudioContent, string) {
	result.OffsetFrames = offset
	result.TrailingFrames = result.SampleFrames - offset - DiagnosticAudioSampleFrames
	var signalEnergy [2]int64
	blocksValid := true
	for start := 0; start < DiagnosticAudioSampleFrames; start += diagnosticAudioBlockFrames {
		var blockReference, blockError [2]int64
		for frame := start; frame < start+diagnosticAudioBlockFrames; frame++ {
			for channel := 0; channel < 2; channel++ {
				value := int64(observed[(offset+frame)*2+channel])
				original := int64(reference[frame*2+channel])
				difference := value - original
				signalEnergy[channel] += value * value
				blockReference[channel] += original * original
				blockError[channel] += difference * difference
			}
		}
		for channel := 0; channel < 2; channel++ {
			errorRatio := math.Sqrt(float64(blockError[channel]) / float64(blockReference[channel]))
			result.WorstBlockNRMSE[channel] = max(result.WorstBlockNRMSE[channel], errorRatio)
			blocksValid = blocksValid && blockError[channel]*diagnosticAudioErrorDenominator <= blockReference[channel]*diagnosticAudioBlockErrorNumerator
		}
	}
	silent := false
	for channel := 0; channel < 2; channel++ {
		result.NormalizedRMSE[channel] = math.Sqrt(float64(squared[channel]) / float64(referenceEnergy[channel]))
		result.SignalRMS[channel] = math.Sqrt(float64(signalEnergy[channel]) / DiagnosticAudioSampleFrames)
		silent = silent || signalEnergy[channel]*16 < referenceEnergy[channel]
	}
	leading, leadingWorst, leadingPeak, leadingOK := diagnosticAudioPadding(observed[:offset*2], referenceEnergy)
	tail := (offset + DiagnosticAudioSampleFrames) * 2
	trailing, trailingWorst, trailingPeak, trailingOK := diagnosticAudioPadding(observed[tail:], referenceEnergy)
	result.LeadingPaddingRMS, result.TrailingPaddingRMS = leading, trailing
	for channel := 0; channel < 2; channel++ {
		result.WorstPaddingBlockRMS[channel] = max(leadingWorst[channel], trailingWorst[channel])
		result.PaddingPeak[channel] = max(leadingPeak[channel], trailingPeak[channel])
	}
	if silent {
		return result, "diagnostic_audio_silence"
	}
	if !blocksValid || !diagnosticAudioGlobalError(squared, referenceEnergy) {
		return result, "diagnostic_audio_content"
	}
	if !leadingOK || !trailingOK {
		return result, "diagnostic_audio_padding"
	}
	return result, ""
}

func diagnosticAudioPadding(samples []int32, referenceEnergy [2]int64) (rms, worst [2]float64, peak [2]int, accepted bool) {
	accepted = true
	frames := len(samples) / 2
	var total [2]int64
	for start := 0; start < frames; start += diagnosticAudioBlockFrames {
		end := min(start+diagnosticAudioBlockFrames, frames)
		var squared [2]int64
		for frame := start; frame < end; frame++ {
			for channel := 0; channel < 2; channel++ {
				value := int64(samples[frame*2+channel])
				squared[channel] += value * value
				if value < 0 {
					value = -value
				}
				peak[channel] = max(peak[channel], int(value))
			}
		}
		for channel := 0; channel < 2; channel++ {
			total[channel] += squared[channel]
			worst[channel] = max(worst[channel], math.Sqrt(float64(squared[channel])/float64(end-start)))
			meanLimit := referenceEnergy[channel] * diagnosticAudioPaddingNumerator / (DiagnosticAudioSampleFrames * diagnosticAudioErrorDenominator)
			accepted = accepted && squared[channel] <= meanLimit*int64(end-start) && peak[channel] <= DiagnosticAudioPaddingPeakLimit
		}
	}
	if frames > 0 {
		for channel := 0; channel < 2; channel++ {
			rms[channel] = math.Sqrt(float64(total[channel]) / float64(frames))
		}
	}
	return rms, worst, peak, accepted
}
