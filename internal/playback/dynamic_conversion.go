package playback

import "github.com/moooyo/goby/internal/transcode"

// PlanDynamicConversion reuses the bounded HLS codec/profile candidate planner
// for an authorized nonseekable input. Unknown duration is preserved as zero;
// no finite-file timeline or sample-accurate seek claim is invented.
func PlanDynamicConversion(source Source, request Request, limits ConversionLimits) (ConversionDecision, error) {
	if request.StartTimeTicks != nil && *request.StartTimeTicks != 0 {
		return ConversionDecision{}, ErrInvalidRequest
	}
	// Lease identity was authenticated by the source adapter. The local-file
	// evaluator remains useful for output profile predicates, without treating
	// that opaque lease as a claim about the projected container.
	request.LiveStreamID = ""
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
		// A live source has no complete restart index at negotiation time.
		// Prefer forced closed-GOP video while preserving compatible audio.
		// Remux-only policy still reaches copied candidates, whose published
		// segments require independent packet/configuration restart evidence.
		for _, mode := range [][2]bool{{false, true}, {false, false}, {true, true}, {true, false}} {
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
			for _, candidate := range conversionEncodingCandidates(profile, kind, videoCopy, audioCopy) {
				candidateProfile := candidate.profile
				plan, projected, reason := conversionCandidate(source, request, limits, candidateProfile, kind, selection, videoCopy, audioCopy, true, candidate.video)
				if reason != nil {
					result.Reasons = appendConversionReasons(result.Reasons, *reason)
					continue
				}
				plan.SourceMode, plan.DurationTicks, plan.StartTicks = "stream", 0, 0
				plan.SourceFormatStartKnown = source.Info.FormatStartKnown
				if plan.SourceFormatStartKnown {
					plan.SourceFormatStartTicks = source.Info.FormatStartTicks
				}
				projected.Info.DurationTicks, projected.Info.Size = 0, 0
				if selection.subtitle != nil && !transcode.HasHLSSubtitles(plan) && (plan.Subtitle.Mode != "burn" || !transcode.IsBitmapSubtitle(plan.Subtitle.Codec)) {
					result.Reasons = appendConversionReasons(result.Reasons, Reason{Code: "dynamic_subtitle_delivery_unsupported", Property: "SubtitleStreamIndex",
						Message: "Dynamic subtitles require a bound text HLS rendition or bitmap burn-in; finite-file text extraction is not available on an unbounded input."})
					continue
				}
				outputRequest := conversionOutputRequest(request, candidateProfile, kind)
				if transcode.HasHLSSubtitles(plan) || plan.Subtitle.Mode == "burn" {
					disabled := -1
					outputRequest.SubtitleStreamIndex = &disabled
				}
				output, err := Evaluate(projected, outputRequest)
				if err != nil {
					return result, err
				}
				if !output.OriginalCompatible || !output.ProfileMatched || transcode.ValidatePlan(plan) != nil {
					continue
				}
				result.Plan, result.Output, result.OutputSource = &plan, output, projected
				result.Reasons = output.Reasons
				result.outputValidationRequest = snapshotConversionOutputRequest(outputRequest)
				if transcode.HasHLSSubtitles(plan) {
					result.SubtitleView, err = HLSSubtitleViewFor(plan, request.SubtitleStreamIndex, 0)
					if err != nil {
						return result, err
					}
					selected := result.SubtitleView.SelectedStreamIndex
					result.Output.DefaultSubtitleStreamIndex = &selected
					result.Output.SubtitleMethod, result.Output.SubtitleFormat = "", ""
					if selected >= 0 {
						result.Output.SubtitleMethod, result.Output.SubtitleFormat = SubtitleDeliveryMethodHls, "vtt"
					}
				} else if plan.Subtitle.Mode == "burn" {
					selected := plan.Subtitle.StreamIndex
					result.Output.DefaultSubtitleStreamIndex = &selected
					result.Output.SubtitleMethod, result.Output.SubtitleFormat = SubtitleDeliveryMethodEncode, ""
				}
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
