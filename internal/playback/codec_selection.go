package playback

import (
	"slices"

	"github.com/moooyo/goby/internal/transcode"
)

func hasSoftwareCodecPriority(configurations transcode.CodecConfigurations, hardware transcode.Hardware) bool {
	if hardware.Encode != "" && hardware.Encode != "software" {
		return false
	}
	resolved, err := transcode.ResolveCodecConfigurations(configurations)
	return err == nil && resolved[0].Priority != resolved[1].Priority
}

// orderVideoEncodingFormats applies native encoder priorities only to formats
// already offered by the client. Unmanaged formats retain their positions, and
// stable ties retain profile, bit-depth, and range ordering for each encoder.
func orderVideoEncodingFormats(formats []videoEncodingFormat, configurations transcode.CodecConfigurations, hardware transcode.Hardware) []videoEncodingFormat {
	if !hasSoftwareCodecPriority(configurations, hardware) {
		return formats
	}
	result := slices.Clone(formats)
	var positions []int
	var managed []videoEncodingFormat
	for index, format := range result {
		if _, found := transcode.SoftwareCodecConfiguration(configurations, format.codec, hardware.Encode); found {
			positions = append(positions, index)
			managed = append(managed, format)
		}
	}
	slices.SortStableFunc(managed, func(first, second videoEncodingFormat) int {
		left, _ := transcode.SoftwareCodecConfiguration(configurations, first.codec, hardware.Encode)
		right, _ := transcode.SoftwareCodecConfiguration(configurations, second.codec, hardware.Encode)
		return right.Priority - left.Priority
	})
	for index, position := range positions {
		result[position] = managed[index]
	}
	return result
}

// orderConversionEncodingCandidates retains container and audio candidate
// preference. Video priority applies within each existing encoded output group;
// copied video has no encoder identity and therefore keeps its original order.
func orderConversionEncodingCandidates(candidates []conversionEncodingCandidate, configurations transcode.CodecConfigurations, hardware transcode.Hardware) []conversionEncodingCandidate {
	if !hasSoftwareCodecPriority(configurations, hardware) {
		return candidates
	}
	result := slices.Clone(candidates)
	for start := 0; start < len(result); {
		end := start + 1
		for end < len(result) && result[end].profile.Container == result[start].profile.Container &&
			result[end].profile.AudioCodec == result[start].profile.AudioCodec {
			end++
		}
		var positions []int
		var managed []conversionEncodingCandidate
		for index := start; index < end; index++ {
			if _, found := transcode.SoftwareCodecConfiguration(configurations, result[index].video.codec, hardware.Encode); found {
				positions = append(positions, index)
				managed = append(managed, result[index])
			}
		}
		slices.SortStableFunc(managed, func(first, second conversionEncodingCandidate) int {
			left, _ := transcode.SoftwareCodecConfiguration(configurations, first.video.codec, hardware.Encode)
			right, _ := transcode.SoftwareCodecConfiguration(configurations, second.video.codec, hardware.Encode)
			return right.Priority - left.Priority
		})
		for index, position := range positions {
			result[position] = managed[index]
		}
		start = end
	}
	return result
}

func configuredVideoEncodingReason(plan transcode.Plan, configurations transcode.CodecConfigurations) *Reason {
	if !transcode.ConfiguredVideoEncodingAllowed(configurations, plan) {
		return conversionReason("conversion_encoder_disabled", "CodecConfigurations", "The selected software video encoder is disabled by the server codec configuration.")
	}
	return nil
}
