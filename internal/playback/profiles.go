// Package playback evaluates original-file playback using supplied media facts.
// It performs no authorization, file access, token creation, or URL generation.
//
// DeviceProfile DTOs follow the pinned Emby SDK schema. Detailed condition
// combination behavior has not been established against an Emby reference:
// Goby intersects all matching container and codec constraints. Unknown facts
// fail required conditions; IsRequired defaults to false and permits unknown
// facts to remain unverified. Unknown applicability never disables a required
// codec constraint. These conservative rules are Goby's initial contract.
package playback

import (
	"errors"
	"fmt"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

const (
	maxProfileEntries = 256
	maxConditions     = 1024
	maxProfileText    = 4096
	maxSourceStreams  = 1024
)

var (
	ErrInvalidRequest = errors.New("invalid playback request")
	ErrInvalidSource  = errors.New("invalid playback source")
)

// Source contains already obtained facts about an original local media file.
// Its caller owns availability and permission checks. MediaSourceID defaults
// to media.SourceID(ItemID) when omitted. Streams may also contain verified,
// indexed external subtitle metadata with stable global stream indices.
type Source struct {
	ItemID, MediaSourceID, Path, ItemType string
	Info                                  media.Info
}

// Reason explains a declined original-file decision without exposing paths.
type Reason struct {
	Code, Property, Message string
	Unverified              bool
	ProfileOnly             bool
}

// Decision never authorizes remuxing, transcoding, or subtitle extraction.
// DirectStream means HTTP delivery of the same original file. OriginalCompatible
// and ProfileMatched preserve the strict evaluation even when an explicit
// client request to disable transcoding permits an original-file fallback.
// With no profile, DirectPlay only describes original-file delivery under the
// explicit request limits; ClientMustValidate requires the client to decide
// container/codec compatibility. Selected nondefault audio remains in the
// complete original file and requires client-side track selection.
// SubtitleFormat names an indexed external text subtitle's selected srt/vtt
// representation; the caller still authorizes and delivers that separate file.
type Decision struct {
	DirectPlay, DirectStream, ProfileEvaluated, ClientMustValidate bool
	OriginalCompatible, ProfileMatched                             bool
	RequiresAudioTrackSelection                                    bool
	DefaultAudioStreamIndex, DefaultSubtitleStreamIndex            *int
	SubtitleMethod                                                 SubtitleDeliveryMethod
	SubtitleFormat                                                 string
	Reasons                                                        []Reason
}

type selectedStreams struct {
	video, audio, subtitle *media.Stream
	audioCount, videoCount int
	audioDefault           *media.Stream
}

// Evaluate returns a truthful direct-play decision for the complete source.
// Invalid source/request structure returns a sentinel-wrapped error. A valid
// request with unsupported capabilities returns a decision with reasons.
func Evaluate(source Source, request Request) (Decision, error) {
	if err := validateRequest(source, request); err != nil {
		return Decision{}, err
	}
	selection, err := selectStreams(source, request)
	if err != nil {
		return Decision{}, err
	}
	disabledSubtitle := -1
	decision := Decision{
		ProfileEvaluated:           request.DeviceProfile != nil,
		ClientMustValidate:         request.DeviceProfile == nil,
		DefaultSubtitleStreamIndex: &disabledSubtitle,
	}
	if selection.audio != nil {
		index := selection.audio.Index
		decision.DefaultAudioStreamIndex = &index
		decision.RequiresAudioTrackSelection = selection.audioDefault != nil && selection.audioDefault.Index != index
	}
	if selection.subtitle != nil {
		index := selection.subtitle.Index
		decision.DefaultSubtitleStreamIndex = &index
	}
	decline := func(code, property, message string) {
		decision.Reasons = append(decision.Reasons, Reason{Code: code, Property: property, Message: message})
	}
	if request.LiveStreamID != "" {
		decline("live_stream_unsupported", "LiveStreamId", "This evaluator supports original local files only.")
	}
	kind := sourceKind(source, selection)
	if kind == "" {
		decline("unsupported_media_type", "Type", "Only audio and video original files are supported.")
	}
	if selection.audio != nil && selection.audio.IsExternal {
		decline("external_audio_unsupported", "AudioStreamIndex", "External audio is not part of the original file.")
	}
	if request.MaxAudioChannels != nil && selection.audio != nil {
		if selection.audio.Channels <= 0 {
			decline("unknown_audio_channels", "MaxAudioChannels", "Audio channel count is required to apply the requested limit.")
		} else if selection.audio.Channels > *request.MaxAudioChannels {
			decline("audio_channel_limit", "MaxAudioChannels", "The selected audio stream exceeds the requested channel limit.")
		}
	}
	if request.MaxStreamingBitrate != nil {
		if source.Info.Bitrate <= 0 {
			decline("unknown_source_bitrate", "MaxStreamingBitrate", "Source bitrate is required to apply the requested limit.")
		} else if source.Info.Bitrate > *request.MaxStreamingBitrate {
			decline("bitrate_limit", "MaxStreamingBitrate", "The original file exceeds the streaming bitrate limit.")
		}
	}
	if profile := request.DeviceProfile; profile != nil && kind != "" {
		profileDecline := func(code, property, message string) {
			decision.Reasons = append(decision.Reasons, Reason{Code: code, Property: property, Message: message, ProfileOnly: true})
		}
		profileReasons := func(reasons []Reason) {
			for _, reason := range reasons {
				reason.ProfileOnly = true
				decision.Reasons = append(decision.Reasons, reason)
			}
		}
		bitrateLimit := profile.MaxStreamingBitrate
		if kind == DlnaProfileTypeAudio && profile.MaxStaticMusicBitrate != nil {
			limit := int64(*profile.MaxStaticMusicBitrate)
			bitrateLimit = lowerLimit(bitrateLimit, &limit)
		}
		if bitrateLimit != nil {
			if source.Info.Bitrate <= 0 {
				profileDecline("unknown_source_bitrate", "DeviceProfile.MaxStreamingBitrate", "Source bitrate is required to verify the device bitrate limit.")
			} else if source.Info.Bitrate > *bitrateLimit {
				profileDecline("device_bitrate_limit", "DeviceProfile.MaxStreamingBitrate", "The original file exceeds the device profile bitrate limit.")
			}
		}
		container := media.CanonicalContainer(source.Info, source.Path)
		if !matchesList(profile.SupportedMediaTypes, string(kind)) {
			profileDecline("unsupported_media_type", "SupportedMediaTypes", "The client does not declare this media type.")
		}
		matched := false
		for _, direct := range profile.DirectPlayProfiles {
			if !strings.EqualFold(string(direct.Type), string(kind)) || !matchesList(direct.Container, container) {
				continue
			}
			if kind == DlnaProfileTypeVideo && selection.video != nil && !matchesList(direct.VideoCodec, selection.video.Codec) {
				continue
			}
			if selection.audio != nil && !matchesList(direct.AudioCodec, selection.audio.Codec) {
				continue
			}
			matched = true
			break
		}
		if !matched {
			profileDecline("no_direct_play_profile", "DirectPlayProfiles", "No declared direct-play profile matches the selected container and codecs.")
		}
		facts := conditionFacts{source: source, streams: selection}
		for _, constraint := range profile.ContainerProfiles {
			if strings.EqualFold(string(constraint.Type), string(kind)) && matchesList(constraint.Container, container) {
				profileReasons(evaluateConditions(constraint.Conditions, facts))
			}
		}
		for _, constraint := range profile.CodecProfiles {
			stream := codecProfileStream(constraint.Type, kind, selection)
			if stream == nil || !matchesList(constraint.Container, container) || !matchesList(constraint.Codec, stream.Codec) {
				continue
			}
			apply, reasons := applies(constraint.ApplyConditions, facts)
			profileReasons(reasons)
			if apply {
				profileReasons(evaluateConditions(constraint.Conditions, facts))
			}
		}
	}
	subtitleSupported := selection.subtitle == nil
	if selection.subtitle != nil {
		decision.SubtitleMethod, decision.SubtitleFormat = selectSubtitleDelivery(source, request.DeviceProfile, selection.subtitle)
		subtitleSupported = decision.SubtitleMethod == SubtitleDeliveryMethodEmbed ||
			decision.SubtitleMethod == SubtitleDeliveryMethodExternal && decision.SubtitleFormat != ""
		if !subtitleSupported {
			decline("subtitle_delivery_unsupported", "SubtitleStreamIndex", "The selected subtitle requires unsupported delivery, extraction, or burn-in.")
		}
	}
	compatible, hardFailure, profileMismatch := true, false, false
	for _, reason := range decision.Reasons {
		compatible = compatible && reason.Unverified
		decision.ClientMustValidate = decision.ClientMustValidate || reason.Unverified
		if !reason.Unverified {
			profileMismatch = profileMismatch || reason.ProfileOnly
			hardFailure = hardFailure || !reason.ProfileOnly
		}
	}
	decision.OriginalCompatible = compatible
	decision.ProfileMatched = decision.ProfileEvaluated && kind != "" && !profileMismatch &&
		subtitleSupported
	allowed := compatible
	// Controlled reference samples establish that explicitly disabling
	// transcoding requests the original file despite a codec profile mismatch.
	// Preserve the mismatch as a fact, while never bypassing request limits,
	// invalid stream selection, or unsupported subtitle/audio delivery needs.
	if !allowed && !hardFailure && profileMismatch && request.EnableTranscoding != nil && !*request.EnableTranscoding {
		allowed = true
		decision.ClientMustValidate = true
		decision.Reasons = append(decision.Reasons, Reason{
			Code: "original_fallback", Property: "EnableTranscoding", Unverified: true,
			Message: "The client explicitly disabled transcoding and must validate the original file despite its profile mismatch.",
		})
	}
	decision.DirectPlay = allowed && (request.EnableDirectPlay == nil || *request.EnableDirectPlay)
	decision.DirectStream = allowed && (request.EnableDirectStream == nil || *request.EnableDirectStream)
	if request.EnableDirectPlay != nil && !*request.EnableDirectPlay {
		decline("direct_play_disabled", "EnableDirectPlay", "Direct-play delivery was disabled by the request.")
	}
	if request.EnableDirectStream != nil && !*request.EnableDirectStream {
		decline("direct_stream_disabled", "EnableDirectStream", "Original-file HTTP delivery was disabled by the request.")
	}
	return decision, nil
}

func lowerLimit(first, second *int64) *int64 {
	if first == nil || second != nil && *second < *first {
		return second
	}
	return first
}

func selectStreams(source Source, request Request) (selectedStreams, error) {
	var result selectedStreams
	byIndex := make(map[int]*media.Stream, len(source.Info.Streams))
	for index := range source.Info.Streams {
		stream := &source.Info.Streams[index]
		if stream.Index < 0 || byIndex[stream.Index] != nil {
			return result, fmt.Errorf("%w: stream indices must be unique nonnegative values", ErrInvalidSource)
		}
		byIndex[stream.Index] = stream
		switch strings.ToLower(stream.CodecType) {
		case "video":
			if stream.IsAttachedPicture {
				continue
			}
			result.videoCount++
			if result.video == nil || stream.IsDefault && !result.video.IsDefault {
				result.video = stream
			}
		case "audio":
			result.audioCount++
			if result.audio == nil || stream.IsDefault && !result.audio.IsDefault {
				result.audio = stream
			}
		}
	}
	result.audioDefault = result.audio
	if request.AudioStreamIndex != nil {
		stream := byIndex[*request.AudioStreamIndex]
		if stream == nil || !strings.EqualFold(stream.CodecType, "audio") {
			return result, fmt.Errorf("%w: selected audio stream does not exist", ErrInvalidRequest)
		}
		result.audio = stream
	}
	if request.SubtitleStreamIndex != nil && *request.SubtitleStreamIndex != -1 {
		stream := byIndex[*request.SubtitleStreamIndex]
		if stream == nil || !strings.EqualFold(stream.CodecType, "subtitle") {
			return result, fmt.Errorf("%w: selected subtitle stream does not exist", ErrInvalidRequest)
		}
		result.subtitle = stream
	}
	if result.video == nil && result.audio == nil {
		return result, fmt.Errorf("%w: no audio or video stream is present", ErrInvalidSource)
	}
	return result, nil
}

func sourceKind(source Source, streams selectedStreams) DlnaProfileType {
	switch strings.ToLower(source.ItemType) {
	case "audio":
		if streams.audio != nil {
			return DlnaProfileTypeAudio
		}
	case "movie", "episode", "video", "musicvideo", "trailer":
		if streams.video != nil {
			return DlnaProfileTypeVideo
		}
	case "":
		if streams.video != nil {
			return DlnaProfileTypeVideo
		}
		if streams.audio != nil {
			return DlnaProfileTypeAudio
		}
	}
	return ""
}

func codecProfileStream(kind CodecType, mediaKind DlnaProfileType, streams selectedStreams) *media.Stream {
	switch {
	case strings.EqualFold(string(kind), string(CodecTypeVideo)) && mediaKind == DlnaProfileTypeVideo:
		return streams.video
	case strings.EqualFold(string(kind), string(CodecTypeVideoAudio)) && mediaKind == DlnaProfileTypeVideo:
		return streams.audio
	case strings.EqualFold(string(kind), string(CodecTypeAudio)) && mediaKind == DlnaProfileTypeAudio:
		return streams.audio
	default:
		return nil
	}
}

// Empty selectors are unconstrained. Nonempty selectors are comma-delimited
// exact names, with case-insensitive comparison and no inferred codec aliases.
func matchesList(selector, actual string) bool {
	if strings.TrimSpace(selector) == "" {
		return true
	}
	if actual == "" {
		return false
	}
	for _, candidate := range strings.Split(selector, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), actual) {
			return true
		}
	}
	return false
}

func selectSubtitleDelivery(source Source, profile *DeviceProfile, stream *media.Stream) (SubtitleDeliveryMethod, string) {
	if stream.IsExternal {
		return selectExternalSubtitle(source, profile, stream)
	}
	if profile == nil {
		return "", ""
	}
	container := media.CanonicalContainer(source.Info, source.Path)
	for _, candidate := range profile.SubtitleProfiles {
		if !matchesList(candidate.Format, stream.Codec) || !matchesList(candidate.Container, container) ||
			!matchesList(candidate.Language, stream.Language) {
			continue
		}
		if candidate.Method == SubtitleDeliveryMethodEmbed {
			return SubtitleDeliveryMethodEmbed, ""
		}
	}
	return "", ""
}

func selectExternalSubtitle(source Source, profile *DeviceProfile, stream *media.Stream) (SubtitleDeliveryMethod, string) {
	if !stream.IsTextSubtitleStream {
		return "", ""
	}
	var native, converted string
	switch strings.ToLower(stream.Codec) {
	case "srt":
		native, converted = "srt", "vtt"
	case "webvtt":
		native, converted = "vtt", "srt"
	default:
		return "", ""
	}
	if profile == nil {
		// This describes indexed delivery facts, not proof that an unknown
		// client supports the subtitle format. Evaluate retains that distinction.
		return SubtitleDeliveryMethodExternal, native
	}
	container := media.CanonicalContainer(source.Info, source.Path)
	// Prefer any applicable native-format candidate over every conversion
	// candidate, regardless of the order in which the client lists profiles.
	for _, format := range []string{native, converted} {
		for _, candidate := range profile.SubtitleProfiles {
			if candidate.Method != SubtitleDeliveryMethodExternal || !matchesList(candidate.Format, format) ||
				!matchesList(candidate.Container, container) || !matchesList(candidate.Language, stream.Language) {
				continue
			}
			if candidate.Protocol != "" && !strings.EqualFold(candidate.Protocol, "http") && !strings.EqualFold(candidate.Protocol, "https") {
				continue
			}
			return SubtitleDeliveryMethodExternal, format
		}
	}
	return "", ""
}

func validateRequest(source Source, request Request) error {
	if source.ItemID == "" || len(source.Info.Streams) == 0 || len(source.Info.Streams) > maxSourceStreams ||
		source.Info.Bitrate < 0 || source.Info.DurationTicks < 0 || source.Info.Size < 0 {
		return fmt.Errorf("%w: invalid source identity or media facts", ErrInvalidSource)
	}
	id := source.MediaSourceID
	if id == "" {
		id = media.SourceID(source.ItemID)
	} else if id != media.SourceID(source.ItemID) {
		return fmt.Errorf("%w: original media source identity is inconsistent", ErrInvalidSource)
	}
	if request.ID != "" && request.ID != source.ItemID || request.MediaSourceID != "" && request.MediaSourceID != id {
		return fmt.Errorf("%w: requested item or media source does not match", ErrInvalidRequest)
	}
	if request.MaxStreamingBitrate != nil && *request.MaxStreamingBitrate <= 0 ||
		request.MaxAudioChannels != nil && *request.MaxAudioChannels <= 0 ||
		request.AudioStreamIndex != nil && *request.AudioStreamIndex < 0 ||
		request.SubtitleStreamIndex != nil && *request.SubtitleStreamIndex < -1 ||
		request.StartTimeTicks != nil && (*request.StartTimeTicks < 0 || source.Info.DurationTicks > 0 && *request.StartTimeTicks > source.Info.DurationTicks) {
		return fmt.Errorf("%w: invalid playback limit, stream index, or start position", ErrInvalidRequest)
	}
	if request.DeviceProfile != nil {
		return validateProfile(request.DeviceProfile)
	}
	return nil
}

func validateProfile(profile *DeviceProfile) error {
	invalid := func() error {
		return fmt.Errorf("%w: device profile exceeds limits or contains invalid constraints", ErrInvalidRequest)
	}
	if profile.MaxStreamingBitrate != nil && *profile.MaxStreamingBitrate <= 0 ||
		profile.MaxStaticMusicBitrate != nil && *profile.MaxStaticMusicBitrate <= 0 {
		return invalid()
	}
	count := len(profile.DirectPlayProfiles) + len(profile.ContainerProfiles) + len(profile.CodecProfiles) +
		len(profile.TranscodingProfiles) + len(profile.ResponseProfiles) + len(profile.SubtitleProfiles) + len(profile.DeclaredFeatures)
	if count > maxProfileEntries || len(profile.SupportedMediaTypes) > maxProfileText {
		return invalid()
	}
	conditions := 0
	checkConditions := func(values []ProfileCondition) bool {
		conditions += len(values)
		if conditions > maxConditions {
			return false
		}
		for _, value := range values {
			if len(value.Value) > maxProfileText || len(value.Property) > maxProfileText || len(value.Condition) > maxProfileText {
				return false
			}
		}
		return true
	}
	checkText := func(values ...string) bool {
		for _, value := range values {
			if len(value) > maxProfileText {
				return false
			}
		}
		return true
	}
	validKind := func(kind DlnaProfileType) bool {
		return strings.EqualFold(string(kind), "Audio") || strings.EqualFold(string(kind), "Video") || strings.EqualFold(string(kind), "Photo")
	}
	for _, direct := range profile.DirectPlayProfiles {
		if !validKind(direct.Type) || !checkText(direct.Container, direct.AudioCodec, direct.VideoCodec) {
			return invalid()
		}
	}
	for _, container := range profile.ContainerProfiles {
		if !validKind(container.Type) || !checkText(container.Container) || !checkConditions(container.Conditions) {
			return invalid()
		}
	}
	for _, codec := range profile.CodecProfiles {
		if !strings.EqualFold(string(codec.Type), "Video") && !strings.EqualFold(string(codec.Type), "VideoAudio") && !strings.EqualFold(string(codec.Type), "Audio") ||
			!checkText(codec.Codec, codec.Container) || !checkConditions(codec.Conditions) || !checkConditions(codec.ApplyConditions) {
			return invalid()
		}
	}
	for _, response := range profile.ResponseProfiles {
		if !checkConditions(response.Conditions) {
			return invalid()
		}
	}
	for _, subtitle := range profile.SubtitleProfiles {
		if !checkText(subtitle.Format, subtitle.Container, subtitle.Language, subtitle.Protocol, string(subtitle.Method)) {
			return invalid()
		}
	}
	return nil
}
