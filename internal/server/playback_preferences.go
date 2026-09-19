package server

import (
	"context"
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

// applyPreferredStreams changes defaults only. Client-supplied selections,
// including subtitle -1, are authoritative and remain for normal validation.
// Dynamic callers pass no remembered state: static catalog stamps do not prove
// anything about a live source generation's track identity.
func applyPreferredStreams(configuration identity.UserConfiguration, info media.Info, remembered library.PlaybackPreferences, request *playback.Request) {
	find := func(index int, kind string) *media.Stream {
		for position := range info.Streams {
			stream := &info.Streams[position]
			if stream.Index == index && stream.CodecType == kind {
				return stream
			}
		}
		return nil
	}
	var audio *media.Stream
	for index := range info.Streams {
		stream := &info.Streams[index]
		if stream.CodecType == "audio" && !stream.IsExternal && (audio == nil || stream.IsDefault && !audio.IsDefault) {
			audio = stream
		}
	}
	if request.AudioStreamIndex == nil {
		if configuration.RememberAudioSelections && remembered.AudioStreamIndex != nil {
			if selected := find(*remembered.AudioStreamIndex, "audio"); selected != nil && !selected.IsExternal {
				audio = selected
				value := selected.Index
				request.AudioStreamIndex = &value
			}
		}
		if request.AudioStreamIndex == nil && (!configuration.PlayDefaultAudioTrack || audio == nil || !audio.IsDefault) {
			language := identity.PreferenceLanguageKey(configuration.AudioLanguagePreference)
			if language != "" {
				for index := range info.Streams {
					stream := &info.Streams[index]
					if stream.CodecType == "audio" && !stream.IsExternal && identity.PreferenceLanguageKey(stream.Language) == language {
						audio = stream
						value := stream.Index
						request.AudioStreamIndex = &value
						break
					}
				}
			}
		}
	} else {
		audio = find(*request.AudioStreamIndex, "audio")
	}
	if request.SubtitleStreamIndex != nil {
		return
	}
	mode := configuration.SubtitleMode
	if mode == "None" {
		value := -1
		request.SubtitleStreamIndex = &value
		return
	}
	if configuration.RememberSubtitleSelections && remembered.SubtitleStreamIndex != nil {
		selected := find(*remembered.SubtitleStreamIndex, "subtitle")
		compatible := selected != nil && (mode != "OnlyForced" || selected.IsForced) && (mode != "HearingImpaired" || selected.IsHearingImpaired)
		if *remembered.SubtitleStreamIndex == -1 || compatible {
			value := *remembered.SubtitleStreamIndex
			request.SubtitleStreamIndex = &value
			return
		}
	}
	language := identity.PreferenceLanguageKey(configuration.SubtitleLanguagePreference)
	if language == "" {
		language = identity.PreferenceLanguageKey(configuration.AudioLanguagePreference)
	}
	forcedOnly := mode == "OnlyForced"
	if mode == "Smart" {
		forcedOnly = language == "" || audio != nil && identity.PreferenceLanguageKey(audio.Language) == language
	}
	var selected *media.Stream
	bestScore := -1
	unknownHearing, anyHearing := false, false
	for _, stream := range info.Streams {
		if stream.CodecType == "subtitle" {
			unknownHearing = unknownHearing || !stream.IsExternal && info.ProbeVersion < 8
			anyHearing = anyHearing || stream.IsHearingImpaired
		}
	}
	for index := range info.Streams {
		stream := &info.Streams[index]
		if stream.CodecType != "subtitle" || forcedOnly && !stream.IsForced || mode == "Default" && !stream.IsDefault {
			continue
		}
		if mode == "HearingImpaired" && (anyHearing && !stream.IsHearingImpaired || !anyHearing && unknownHearing) {
			continue
		}
		score := 0
		if language != "" {
			if identity.PreferenceLanguageKey(stream.Language) == language {
				score += 4
			} else if mode == "Smart" && !forcedOnly {
				continue
			}
		}
		if stream.IsDefault {
			score += 2
		}
		if stream.IsForced {
			score++
		}
		if score > bestScore {
			selected, bestScore = stream, score
		}
	}
	if selected != nil {
		value := selected.Index
		request.SubtitleStreamIndex = &value
	}
}

func (s *Server) applyStaticPlaybackPreferences(ctx context.Context, r *http.Request, principal identity.Principal, source library.MediaFile, request *playback.Request) error {
	// Userless application requests do not acquire another user's remembered
	// state merely because their profile contains a UserId parameter.
	if principal.IsApplicationKey() {
		return nil
	}
	configuration := identity.ProjectUserConfiguration(principal.User.Configuration)
	remembered, err := s.library.GetPlaybackPreferencesFor(ctx, librarySubject(principal, principal.User.ID), source.Item.ID, source.SourceID)
	if err != nil {
		return err
	}
	applyPreferredStreams(configuration, playbackMediaInfo(source.Item), remembered, request)
	if request.StartTimeTicks == nil && request.IsPlayback != nil && *request.IsPlayback && configuration.ResumeRewindSeconds > 0 &&
		remembered.ResumeTicks > 0 && source.Item.Media != nil && remembered.ResumeTicks < source.Item.Media.DurationTicks {
		position := remembered.ResumeTicks - int64(configuration.ResumeRewindSeconds)*media.TicksPerSecond
		if position < 0 {
			position = 0
		}
		request.StartTimeTicks = &position
	}
	return nil
}

// ApplyDynamicUserPreferences is invoked after probing the actual live source.
// It applies only language/mode/default-track preferences, never catalog resume
// positions or remembered indexes from a different live generation.
func ApplyDynamicUserPreferences(principal identity.Principal, info media.Info, request *playback.Request) {
	if principal.IsApplicationKey() {
		return
	}
	applyPreferredStreams(identity.ProjectUserConfiguration(principal.User.Configuration), info, library.PlaybackPreferences{}, request)
}
