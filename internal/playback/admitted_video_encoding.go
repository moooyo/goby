package playback

import (
	"fmt"

	"github.com/moooyo/goby/internal/transcode"
)

// AdmittedVideoEncoding binds a retained dynamic revision to the concrete
// output tuple already accepted by the client. It is internal reauthorization
// state, never an additional client capability or a managed settings field.
type AdmittedVideoEncoding struct {
	Codec, Profile, Container, VideoRange string
	BitDepth, Width, Height               int
}

func validateAdmittedVideoEncoding(value AdmittedVideoEncoding) error {
	if value == (AdmittedVideoEncoding{}) {
		return nil
	}
	if value.Width < 2 || value.Height < 2 || value.Width > 8192 || value.Height > 8192 || value.Width%2 != 0 || value.Height%2 != 0 {
		return fmt.Errorf("%w: invalid retained output dimensions", ErrInvalidRequest)
	}
	for _, format := range videoEncodingFormats(value.Codec, value.Container) {
		if matchesAdmittedVideoEncoding(format, value.Container, value) {
			return nil
		}
	}
	return fmt.Errorf("%w: invalid retained video output tuple", ErrInvalidRequest)
}

func matchesAdmittedVideoEncoding(format videoEncodingFormat, container string, admitted AdmittedVideoEncoding) bool {
	return admitted == (AdmittedVideoEncoding{}) || container == admitted.Container && format.codec == admitted.Codec &&
		format.profile == admitted.Profile && format.bitDepth == admitted.BitDepth && format.videoRange == admitted.VideoRange
}

// Encoder priority and profile limits are new-admission choices. A retained
// revision keeps the concrete tuple accepted before later settings edits or
// hardware fallback, while the planner still checks current output ceilings.
func conversionAdmissionCandidates(candidates []conversionEncodingCandidate, limits ConversionLimits) []conversionEncodingCandidate {
	if limits.AdmittedVideoEncoding == (AdmittedVideoEncoding{}) {
		ordered := orderConversionEncodingCandidates(candidates, limits.CodecConfigurations, limits.Hardware)
		var result []conversionEncodingCandidate
		for _, candidate := range ordered {
			if candidate.video.codec == "" || transcode.VideoProfileAllowed(limits.CodecProfileLimits, candidate.video.codec, candidate.video.profile) {
				result = append(result, candidate)
			}
		}
		return result
	}
	var result []conversionEncodingCandidate
	for _, candidate := range candidates {
		if matchesAdmittedVideoEncoding(candidate.video, candidate.profile.Container, limits.AdmittedVideoEncoding) {
			result = append(result, candidate)
		}
	}
	return result
}
