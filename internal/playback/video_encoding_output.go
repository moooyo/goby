package playback

import (
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// ReprojectVideoEncodingOutput updates framing after a server-owned encoder
// change and rechecks the original output constraints, including every adaptive
// rendition. A nil returned Plan is an ordinary incompatible output decision.
func ReprojectVideoEncodingOutput(decision ConversionDecision, plan transcode.Plan) (ConversionDecision, error) {
	if err := transcode.ValidatePlan(plan); err != nil {
		return decision, err
	}
	decision.Plan = &plan
	decision.OutputSource.Info.Streams = append([]media.Stream(nil), decision.OutputSource.Info.Streams...)
	for index := range decision.OutputSource.Info.Streams {
		video := &decision.OutputSource.Info.Streams[index]
		if video.CodecType == "video" {
			videoOutputFraming(video, plan)
		}
	}
	if decision.outputValidationRequest == nil {
		// Manual progressive requests already specify closed output fields;
		// they have no client profile condition set to reevaluate.
		return decision, nil
	}
	for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
		projected := decision.OutputSource
		if plan.HLS.RenditionCount != 0 {
			rendition := plan.HLS.Renditions[index]
			projected.Info.Streams = append([]media.Stream(nil), projected.Info.Streams...)
			for streamIndex := range projected.Info.Streams {
				video := &projected.Info.Streams[streamIndex]
				if video.CodecType == "video" {
					video.Width, video.Height, video.Bitrate = rendition.Width, rendition.Height, rendition.VideoBitrate
				}
			}
			projected.Info.Bitrate = (rendition.VideoBitrate + max(plan.AudioBitrate, decision.OutputSource.Info.Bitrate*9/10-plan.VideoBitrate)) * 10 / 9
		}
		output, err := Evaluate(projected, *decision.outputValidationRequest)
		if err != nil {
			return decision, err
		}
		if !output.OriginalCompatible || !output.ProfileMatched {
			decision.Plan, decision.Output = nil, output
			decision.Reasons = appendConversionReasons(output.Reasons, Reason{Code: "conversion_encoder_framing_incompatible", Property: "VideoCodecTag",
				Message: "The selected encoder framing does not satisfy the original client output constraints."})
			return decision, nil
		}
		if index == 0 {
			if plan.Subtitle.Mode == "burn" || transcode.HasHLSSubtitles(plan) {
				output.SubtitleMethod, output.SubtitleFormat = decision.Output.SubtitleMethod, decision.Output.SubtitleFormat
				output.DefaultSubtitleStreamIndex = decision.Output.DefaultSubtitleStreamIndex
			}
			decision.Output = output
			decision.Reasons = append([]Reason(nil), output.Reasons...)
		}
	}
	return decision, nil
}

// Evaluate consumes only these bounded profile fields. Drop unused client
// labels, response metadata and transcoding declarations, and detach all slices
// and pointers so later request changes cannot alter admitted constraints.
func snapshotConversionOutputRequest(request Request) *Request {
	result := &Request{ID: request.ID, MediaSourceID: request.MediaSourceID, LiveStreamID: request.LiveStreamID,
		MaxStreamingBitrate: snapshotOutputValue(request.MaxStreamingBitrate), MaxAudioChannels: snapshotOutputValue(request.MaxAudioChannels),
		AudioStreamIndex: snapshotOutputValue(request.AudioStreamIndex), SubtitleStreamIndex: snapshotOutputValue(request.SubtitleStreamIndex),
		StartTimeTicks: snapshotOutputValue(request.StartTimeTicks), EnableDirectPlay: snapshotOutputValue(request.EnableDirectPlay),
		EnableDirectStream: snapshotOutputValue(request.EnableDirectStream), EnableTranscoding: snapshotOutputValue(request.EnableTranscoding)}
	if request.DeviceProfile == nil {
		return result
	}
	source := request.DeviceProfile
	profile := &DeviceProfile{SupportedMediaTypes: source.SupportedMediaTypes,
		MaxStreamingBitrate: snapshotOutputValue(source.MaxStreamingBitrate), MaxStaticBitrate: snapshotOutputValue(source.MaxStaticBitrate),
		MaxStaticMusicBitrate: snapshotOutputValue(source.MaxStaticMusicBitrate),
		DirectPlayProfiles:    append([]DirectPlayProfile(nil), source.DirectPlayProfiles...),
		ContainerProfiles:     append([]ContainerProfile(nil), source.ContainerProfiles...),
		CodecProfiles:         append([]CodecProfile(nil), source.CodecProfiles...)}
	for index := range profile.ContainerProfiles {
		profile.ContainerProfiles[index].Conditions = snapshotOutputConditions(profile.ContainerProfiles[index].Conditions)
	}
	for index := range profile.CodecProfiles {
		profile.CodecProfiles[index].Conditions = snapshotOutputConditions(profile.CodecProfiles[index].Conditions)
		profile.CodecProfiles[index].ApplyConditions = snapshotOutputConditions(profile.CodecProfiles[index].ApplyConditions)
	}
	for _, subtitle := range source.SubtitleProfiles {
		profile.SubtitleProfiles = append(profile.SubtitleProfiles, SubtitleProfile{Format: subtitle.Format, Method: subtitle.Method,
			Language: subtitle.Language, Container: subtitle.Container, Protocol: subtitle.Protocol})
	}
	result.DeviceProfile = profile
	return result
}

func snapshotOutputValue[T int | int64 | bool](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func snapshotOutputConditions(source []ProfileCondition) []ProfileCondition {
	result := append([]ProfileCondition(nil), source...)
	for index := range result {
		result[index].IsRequired = snapshotOutputValue(result[index].IsRequired)
	}
	return result
}
