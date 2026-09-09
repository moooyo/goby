package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func playbackOwner(principal identity.Principal) library.PlaybackOwner {
	return library.PlaybackOwner{UserID: principal.User.ID, SessionID: principal.SessionID, DeviceID: principal.Client.DeviceID}
}

func (s *Server) registerPlaybackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Items/{Id}/PlaybackInfo", s.requireEmby(s.playbackInfo))
	mux.HandleFunc("POST /emby/Items/{Id}/PlaybackInfo", s.requireEmby(s.playbackInfo))
	for _, event := range []struct{ route, name string }{
		{"/emby/Sessions/Playing", "Started"},
		{"/emby/Sessions/Playing/Progress", "Progress"},
		{"/emby/Sessions/Playing/Stopped", "Stopped"},
	} {
		mux.HandleFunc("POST "+event.route, s.requireEmby(s.playbackReport(event.name)))
	}
	mux.HandleFunc("POST /emby/Sessions/Playing/Ping", s.requireEmby(s.playbackPing))
	mux.HandleFunc("GET /emby/Users/{UserId}/Items/Resume", s.requireEmby(s.resumeItems))
	for _, action := range []struct {
		path     string
		favorite bool
	}{{"PlayedItems", false}, {"FavoriteItems", true}} {
		base := "/emby/Users/{UserId}/" + action.path + "/{Id}"
		mux.HandleFunc("POST "+base, s.requireEmby(s.setUserFlag(action.favorite, true)))
		mux.HandleFunc("DELETE "+base, s.requireEmby(s.setUserFlag(action.favorite, false)))
		mux.HandleFunc("POST "+base+"/Delete", s.requireEmby(s.setUserFlag(action.favorite, false)))
	}
}

func mergePlaybackQuery(request *playback.Request, values map[string]string) error {
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"id", &request.ID}, {"userid", &request.UserID}, {"mediasourceid", &request.MediaSourceID},
		{"livestreamid", &request.LiveStreamID}, {"currentplaysessionid", &request.CurrentPlaySessionID},
	} {
		if value, exists := values[field.name]; exists {
			if *field.target != "" && *field.target != value {
				return fmt.Errorf("conflicting playback identifiers")
			}
			*field.target = value
		}
	}
	for _, field := range []struct {
		name   string
		target **int64
	}{
		{"maxstreamingbitrate", &request.MaxStreamingBitrate}, {"starttimeticks", &request.StartTimeTicks},
	} {
		if raw, exists := values[field.name]; exists {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || (*field.target != nil && **field.target != value) {
				return fmt.Errorf("invalid or conflicting playback integer")
			}
			*field.target = &value
		}
	}
	for _, field := range []struct {
		name   string
		target **int
	}{
		{"audiostreamindex", &request.AudioStreamIndex}, {"subtitlestreamindex", &request.SubtitleStreamIndex}, {"maxaudiochannels", &request.MaxAudioChannels},
	} {
		if raw, exists := values[field.name]; exists {
			parsed, err := strconv.ParseInt(raw, 10, 32)
			value := int(parsed)
			if err != nil || (*field.target != nil && **field.target != value) {
				return fmt.Errorf("invalid or conflicting playback stream selection")
			}
			*field.target = &value
		}
	}
	for _, field := range []struct {
		name   string
		target **bool
	}{
		{"enabledirectplay", &request.EnableDirectPlay}, {"enabledirectstream", &request.EnableDirectStream}, {"enabletranscoding", &request.EnableTranscoding},
		{"allowinterlacedvideostreamcopy", &request.AllowInterlacedVideoStreamCopy}, {"allowvideostreamcopy", &request.AllowVideoStreamCopy},
		{"allowaudiostreamcopy", &request.AllowAudioStreamCopy}, {"isplayback", &request.IsPlayback}, {"autoopenlivestream", &request.AutoOpenLiveStream},
	} {
		if raw, exists := values[field.name]; exists {
			value, err := strconv.ParseBool(raw)
			if err != nil || (*field.target != nil && **field.target != value) {
				return fmt.Errorf("invalid or conflicting playback boolean")
			}
			*field.target = &value
		}
	}
	return nil
}

func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	var request playback.Request
	if r.Method == http.MethodPost && !decodeBody(w, r, &request) {
		return
	}
	values, err := streamValues(r)
	if err != nil || mergePlaybackQuery(&request, values) != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_playback_request", "Check the playback query and matching body values.")
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	if request.UserID != "" && request.UserID != principal.User.ID {
		apiError(w, r, http.StatusForbidden, "access_denied", "Playback information is bound to the authenticated user.")
		return
	}
	if request.ID != "" && request.ID != r.PathValue("Id") {
		apiError(w, r, http.StatusBadRequest, "invalid_playback_request", "Playback item identifiers must agree.")
		return
	}
	if deviceID := values["deviceid"]; deviceID != "" && deviceID != principal.Client.DeviceID {
		apiError(w, r, http.StatusForbidden, "device_mismatch", "Playback belongs to the authenticated device.")
		return
	}
	request.ID, request.UserID = r.PathValue("Id"), principal.User.ID
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	file, source, err := s.library.OpenMedia(ctx, principal.User.ID, request.ID, request.MediaSourceID)
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	defer file.Close()
	request.MediaSourceID = source.SourceID
	decision, err := playback.Evaluate(playback.Source{ItemID: source.Item.ID, MediaSourceID: source.SourceID,
		Path: source.Item.Path, ItemType: source.Item.Type, Info: playbackMediaInfo(source.Item)}, request)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_playback_request", "The playback request or media facts cannot be evaluated.")
		return
	}
	formats := map[int]string{}
	if decision.SubtitleMethod == playback.SubtitleDeliveryMethodExternal && decision.DefaultSubtitleStreamIndex != nil {
		if _, err := s.library.ReadSubtitle(ctx, principal.User.ID, source.Item.ID, source.SourceID, *decision.DefaultSubtitleStreamIndex); err != nil {
			s.playbackError(w, r, err)
			return
		}
		formats[*decision.DefaultSubtitleStreamIndex] = decision.SubtitleFormat
	}
	session, err := s.library.PreparePlayback(ctx, playbackOwner(principal), source.Item.ID, source.SourceID, request.CurrentPlaySessionID)
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	dto := originalSourceDTO(source.Item)
	token, _, _ := parseEmbyCredentials(r)
	addSubtitleDeliveryCredentials(dto, source.Item.ID, token, formats)
	dto["SupportsDirectPlay"] = decision.DirectPlay
	dto["SupportsDirectStream"] = decision.DirectStream
	if decision.DefaultAudioStreamIndex != nil {
		dto["DefaultAudioStreamIndex"] = *decision.DefaultAudioStreamIndex
	}
	if decision.DefaultSubtitleStreamIndex != nil && decision.SubtitleMethod != playback.SubtitleDeliveryMethodExternal {
		dto["DefaultSubtitleStreamIndex"] = *decision.DefaultSubtitleStreamIndex
	}
	if decision.DirectStream && (request.DeviceProfile != nil || request.EnableDirectPlay != nil || request.EnableDirectStream != nil || (request.IsPlayback != nil && *request.IsPlayback)) {
		token, _, _ := parseEmbyCredentials(r)
		resource := "videos"
		if source.Item.Type == "Audio" {
			resource = "audio"
		}
		query := url.Values{"DeviceId": {principal.Client.DeviceID}, "MediaSourceId": {source.SourceID}, "PlaySessionId": {session.ID}, "api_key": {token}}
		dto["DirectStreamUrl"] = "/" + resource + "/" + url.PathEscape(source.Item.ID) + "/original." + source.Container + "?" + query.Encode()
	}
	response := map[string]any{"MediaSources": []map[string]any{dto}, "PlaySessionId": session.ID}
	allDisabled := request.EnableDirectPlay != nil && !*request.EnableDirectPlay && request.EnableDirectStream != nil && !*request.EnableDirectStream && request.EnableTranscoding != nil && !*request.EnableTranscoding
	if !decision.DirectPlay && !decision.DirectStream && !allDisabled {
		response["ErrorCode"] = "NoCompatibleStream"
	}
	jsonResponse(w, http.StatusOK, response)
}

func originalSourceDTO(item library.Item) map[string]any {
	info := item.Media
	name := strings.TrimSuffix(filepath.Base(item.Path), filepath.Ext(item.Path))
	if name == "" || name == "." {
		name = item.Name
	}
	available := item.CanPlay && info.ProbeVersion == media.CurrentProbeVersion && info.FileChangeTimeNs > 0
	dto := map[string]any{"Id": media.SourceID(item.ID), "ItemId": item.ID, "Name": name, "Path": item.Path,
		"Protocol": "File", "Type": "Default", "Container": media.CanonicalContainer(*info, item.Path), "Formats": []string{},
		"RunTimeTicks": info.DurationTicks, "Bitrate": info.Bitrate, "Size": info.Size, "MediaStreams": itemMediaStreamsDTO(item),
		"SupportsDirectPlay": available, "SupportsDirectStream": available, "SupportsTranscoding": false,
		"RequiresOpening": false, "RequiresClosing": false, "RequiresLooping": false, "IsInfiniteStream": false,
		"IsRemote": false, "HasMixedProtocols": false, "SupportsProbing": true, "RequiredHttpHeaders": map[string]string{},
		"AddApiKeyToDirectStreamUrl": false, "ReadAtNativeFramerate": false}
	var audio *media.Stream
	for index := range info.Streams {
		stream := &info.Streams[index]
		if stream.CodecType == "audio" && (audio == nil || stream.IsDefault) {
			audio = stream
			if stream.IsDefault {
				break
			}
		}
	}
	if audio != nil {
		dto["DefaultAudioStreamIndex"] = audio.Index
	}
	chapters := make([]map[string]any, 0, len(info.Chapters))
	for _, chapter := range info.Chapters {
		chapters = append(chapters, map[string]any{"StartPositionTicks": chapter.StartTicks, "Name": chapter.Title})
	}
	dto["Chapters"] = chapters
	return dto
}

func (s *Server) playbackError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "playback_timeout", "The playback request exceeded its time limit.")
	case errors.Is(err, library.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "RateLimitExceeded", "The active playback session limit has been reached.")
	default:
		s.libraryError(w, r, err)
	}
}
