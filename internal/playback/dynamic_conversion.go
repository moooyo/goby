package playback

import "github.com/moooyo/goby/internal/transcode"

// PlanDynamicConversion reuses the bounded HLS codec/profile candidate planner
// for an authorized nonseekable input. Unknown duration is preserved as zero;
// no finite-file timeline or sample-accurate seek claim is invented.
func PlanDynamicConversion(source Source, request Request, limits ConversionLimits) (ConversionDecision, error) {
	if request.StartTimeTicks != nil && *request.StartTimeTicks != 0 || request.SubtitleStreamIndex != nil && *request.SubtitleStreamIndex != -1 {
		return ConversionDecision{}, ErrInvalidRequest
	}
	// Lease identity was authenticated by the source adapter. The local-file
	// evaluator remains useful for output profile predicates, without treating
	// that opaque lease as a claim about the projected container.
	request.LiveStreamID = ""
	disabled := -1
	request.SubtitleStreamIndex = &disabled
	if err := validateRequest(source, request); err != nil {
		return ConversionDecision{}, err
	}
	result := ConversionDecision{}
	decline := func() (ConversionDecision, error) {
		result.Reasons = appendConversionReasons(result.Reasons, Reason{Code: "no_supported_dynamic_conversion", Property: "TranscodingProfiles", Message: "No authorized HLS profile supports this nonseekable input."})
		return result, nil
	}
	var err error
	limits, err = normalizeConversionLimits(limits)
	if err != nil {
		return result, err
	}
	if request.DeviceProfile == nil {
		return decline()
	}
	if err := validateConversionProfiles(request.DeviceProfile.TranscodingProfiles); err != nil {
		return result, err
	}
	request = conversionRequestAliases(request)
	selection, err := selectStreams(source, request)
	if err != nil {
		return result, err
	}
	kind := sourceKind(source, selection)
	if kind == "" || selection.audio != nil && selection.audio.IsExternal || selection.video != nil && selection.video.IsExternal {
		return decline()
	}
	for _, profile := range request.DeviceProfile.TranscodingProfiles {
		if !conversionProfileMatches(profile, kind) || unsupportedConversionOptions(profile) != nil {
			continue
		}
		container := hlsProfileContainer(profile, kind)
		if container != "ts" && container != "mp4" {
			continue
		}
		for _, mode := range [][2]bool{{true, true}, {true, false}, {false, true}, {false, false}} {
			videoCopy, audioCopy := mode[0], mode[1]
			if isTrue(profile.EnableAdaptiveBitrate) && videoCopy {
				continue
			}
			if selection.video == nil || kind == DlnaProfileTypeAudio {
				videoCopy = true
			}
			if selection.audio == nil {
				audioCopy = true
			}
			if !conversionModeAllowed(selection, kind, request, limits, videoCopy, audioCopy) {
				continue
			}
			for _, candidateProfile := range conversionAudioProfiles(profile, audioCopy) {
				plan, projected, reason := conversionCandidate(source, request, limits, candidateProfile, kind, selection, videoCopy, audioCopy, true)
				if reason != nil {
					result.Reasons = appendConversionReasons(result.Reasons, *reason)
					continue
				}
				plan.SourceMode, plan.DurationTicks, plan.StartTicks = "stream", 0, 0
				projected.Info.DurationTicks, projected.Info.Size = 0, 0
				output, err := Evaluate(projected, conversionOutputRequest(request, candidateProfile, kind))
				if err != nil {
					return result, err
				}
				if !output.OriginalCompatible || !output.ProfileMatched || transcode.ValidatePlan(plan) != nil {
					continue
				}
				result.Plan, result.Output, result.OutputSource = &plan, output, projected
				result.Method = "DirectStream"
				if !videoCopy || !audioCopy {
					result.Method = "Transcode"
				}
				return result, nil
			}
		}
	}
	return decline()
}
