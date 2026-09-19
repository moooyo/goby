package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

// A view is carried by every manifest URL. Selecting a subtitle or a retained
// start never mutates another viewer's manifest or the encoded presentation.
type dynamicPlaybackView struct {
	Subtitles  playback.HLSSubtitleView
	StartTicks *int64
	Live       bool
}

func dynamicEncodingRevision(candidate *transcode.Plan, revision transcode.Plan) (transcode.Plan, bool) {
	if candidate == nil {
		return transcode.Plan{}, false
	}
	actual := revision
	actual.SourceFormatStartKnown, actual.SourceFormatStartTicks = candidate.SourceFormatStartKnown, candidate.SourceFormatStartTicks
	return actual, hardwareEncodingRevisionMatches(candidate, actual)
}

func (s *Server) tryDynamicSubtitlePlaylist(w http.ResponseWriter, r *http.Request) bool {
	values, err := hlsValues(r)
	if err != nil || values["gobyliveid"] == "" {
		return false
	}
	if s.dynamicStreams == nil {
		s.dynamicWindowError(w, r, timeshift.ErrNotFound)
		return true
	}
	if values["manifestsubtitles"] != "" || values["gobyhlsid"] != "" {
		s.hlsError(w, r, errHLSRequestUnsupported)
		return true
	}
	s.dynamicStreams.mu.Lock()
	session := s.dynamicStreams.sessions[values["gobyliveid"]]
	s.dynamicStreams.mu.Unlock()
	if session == nil || session.scope.ItemID != r.PathValue("Id") ||
		values["livestreamid"] != "" && values["livestreamid"] != session.key.liveID {
		s.dynamicWindowError(w, r, timeshift.ErrNotFound)
		return true
	}
	length, err := hlsQueryInteger(values, 1, 86400, "subtitlesegmentlength")
	if err != nil || length != nil && *length != int64(session.key.plan.SegmentSeconds) {
		s.hlsError(w, r, errHLSRequestUnsupported)
		return true
	}
	copy := r.Clone(r.Context())
	query := copy.URL.Query()
	for key := range query {
		if strings.EqualFold(key, "ManifestSubtitles") || strings.EqualFold(key, "SubtitleSegmentLength") || strings.EqualFold(key, "LiveStreamId") {
			query.Del(key)
		}
	}
	copy.URL.RawQuery = query.Encode()
	copy.SetPathValue("LiveStreamId", session.key.liveID)
	copy.SetPathValue("Artifact", "subtitles.m3u8")
	s.dynamicHLSArtifact(w, copy)
	return true
}

func timeshiftScope(scope transcode.Scope) timeshift.Scope {
	return timeshift.Scope{UserID: scope.UserID, AuthSessionID: scope.AuthSessionID,
		DeviceID: scope.DeviceID, PlaySessionID: scope.PlaySessionID, ItemID: scope.ItemID,
		SourceID: scope.SourceID, ApplicationKey: scope.ApplicationKey, ApplicationClientID: scope.ApplicationClientID}
}

func dynamicVariants(plan transcode.Plan) []timeshift.Variant {
	format, kind := "ts", "video"
	if plan.Container == "mp4" {
		format = "fmp4"
	}
	if plan.VideoStreamIndex < 0 {
		kind = "audio"
	}
	variants := make([]timeshift.Variant, max(1, plan.HLS.RenditionCount))
	for index := range variants {
		variants[index] = timeshift.Variant{ID: "r" + strconv.Itoa(index), Kind: kind, Format: format}
	}
	return variants
}

func dynamicTargetDuration(plan transcode.Plan) int64 {
	seconds := plan.SegmentSeconds + 1
	if plan.VideoStreamIndex >= 0 && plan.VideoCodec == "copy" || plan.VideoStreamIndex < 0 && plan.AudioCodec == "copy" {
		seconds = 2 * plan.SegmentSeconds
	}
	return int64(seconds) * media.TicksPerSecond
}

func dynamicRequestView(values map[string]string, session *dynamicStreamSession) (dynamicPlaybackView, error) {
	view := dynamicPlaybackView{Subtitles: session.subtitleView}
	selected := view.Subtitles.SelectedStreamIndex
	if !view.Subtitles.SelectionSet {
		selected = -1
	}
	index, err := hlsQueryInteger(values, -1, math.MaxInt32, "subtitlestreamindex")
	if err != nil {
		return view, err
	}
	if index != nil {
		selected = int(*index)
	}
	offset, err := hlsQueryInteger(values, -24*60*60*media.TicksPerSecond, 24*60*60*media.TicksPerSecond, "subtitleoffsetticks")
	if err != nil {
		return view, err
	}
	if offset != nil {
		view.Subtitles.OffsetTicks = *offset
	}
	view.Subtitles, err = playback.HLSSubtitleViewFor(session.key.plan, &selected, view.Subtitles.OffsetTicks)
	if err != nil {
		return view, err
	}
	view.StartTicks, err = hlsQueryInteger(values, 0, math.MaxInt64, "starttimeticks")
	if err != nil {
		return view, err
	}
	if value, exists := values["live"]; exists {
		if value != "true" && value != "false" {
			return view, timeshift.ErrInvalid
		}
		view.Live = value == "true"
	}
	if view.Live && view.StartTicks != nil {
		return view, timeshift.ErrInvalid
	}
	return view, nil
}

func dynamicArtifactURLView(session *dynamicStreamSession, name, token string, view dynamicPlaybackView, manifest bool) string {
	if !view.Subtitles.SelectionSet {
		view.Subtitles.SelectedStreamIndex = -1
	}
	query := url.Values{"api_key": {token}, "GobyLiveId": {session.id}, "PlaySessionId": {session.scope.PlaySessionID},
		"DeviceId": {session.scope.DeviceID}, "MediaSourceId": {session.scope.SourceID},
		"SubtitleStreamIndex": {strconv.Itoa(view.Subtitles.SelectedStreamIndex)},
		"SubtitleOffsetTicks": {strconv.FormatInt(view.Subtitles.OffsetTicks, 10)}}
	if manifest && view.StartTicks != nil {
		query.Set("StartTimeTicks", strconv.FormatInt(*view.StartTicks, 10))
	}
	if manifest && view.Live {
		query.Set("Live", "true")
	}
	return "/emby/LiveStreams/" + url.PathEscape(session.key.liveID) + "/hls/" + url.PathEscape(name) + "?" + query.Encode()
}

func dynamicMediaPlaylistIndex(plan transcode.Plan, name string) (int, bool) {
	for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
		if name == transcode.HLSPlaylistName(index, plan.HLS.RenditionCount) {
			return index, true
		}
	}
	return 0, false
}

func dynamicArtifactName(id, format string, initialization bool) string {
	extension := ".ts"
	if format == "fmp4" {
		extension = ".m4s"
	}
	if initialization {
		extension = ".mp4"
	}
	return id + extension
}

func dynamicSnapshotArtifact(snapshot timeshift.WindowSnapshot, name string) (timeshift.Artifact, string, bool) {
	for _, variant := range snapshot.Variants {
		for _, segment := range snapshot.Segments {
			for _, artifact := range segment.Artifacts {
				if artifact.VariantID == variant.ID && name == dynamicArtifactName(artifact.ID, variant.Format, false) {
					return artifact, variant.Kind, true
				}
			}
		}
		for _, epoch := range snapshot.Epochs {
			for _, artifact := range epoch.Initializations {
				if artifact.VariantID == variant.ID && name == dynamicArtifactName(artifact.ID, variant.Format, true) {
					return artifact, variant.Kind, true
				}
			}
		}
	}
	return timeshift.Artifact{}, "", false
}

func dynamicViewStart(snapshot timeshift.WindowSnapshot, view dynamicPlaybackView) (*int64, error) {
	if view.StartTicks == nil && !view.Live {
		return nil, nil
	}
	requested := snapshot.LiveStartTicks
	if view.StartTicks != nil {
		requested = *view.StartTicks
	}
	if requested < snapshot.EarliestTicks {
		return nil, timeshift.ErrWindowExpired
	}
	if requested >= snapshot.LiveEdgeTicks {
		return nil, timeshift.ErrNotBuffered
	}
	for _, segment := range snapshot.Segments {
		if requested >= segment.StartTicks && requested-segment.StartTicks < segment.DurationTicks {
			start := segment.StartTicks - snapshot.EarliestTicks
			return &start, nil
		}
	}
	return nil, timeshift.ErrNotBuffered
}

// Playlist boundaries and epochs come only from committed media. They are not
// inferred from nominal segment length, request position, or wall-clock time.
func dynamicWindowPlaylist(snapshot timeshift.WindowSnapshot, variantID string, view dynamicPlaybackView, resource func(string, string, bool) string) ([]byte, error) {
	var variant timeshift.Variant
	for _, candidate := range snapshot.Variants {
		if candidate.ID == variantID {
			variant = candidate
		}
	}
	if variant.ID == "" {
		return nil, timeshift.ErrNotFound
	}
	if len(snapshot.Segments) == 0 {
		if !snapshot.Ended {
			return nil, timeshift.ErrNotBuffered
		}
		if view.StartTicks != nil {
			if *view.StartTicks < snapshot.EarliestTicks {
				return nil, timeshift.ErrWindowExpired
			}
			return nil, timeshift.ErrNotBuffered
		}
		if snapshot.TargetDurationTicks <= 0 || snapshot.TargetDurationTicks%media.TicksPerSecond != 0 {
			return nil, timeshift.ErrInvalid
		}
		return []byte(fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n#EXT-X-ENDLIST\n", snapshot.TargetDurationTicks/media.TicksPerSecond, snapshot.NextSequence, snapshot.DiscontinuitySequence)), nil
	}
	start, err := dynamicViewStart(snapshot, view)
	if err != nil {
		return nil, err
	}
	if snapshot.TargetDurationTicks <= 0 || snapshot.TargetDurationTicks%media.TicksPerSecond != 0 {
		return nil, timeshift.ErrInvalid
	}
	target := snapshot.TargetDurationTicks / media.TicksPerSecond
	for _, segment := range snapshot.Segments {
		if segment.DurationTicks <= 0 || segment.DurationTicks > math.MaxInt64-media.TicksPerSecond {
			return nil, timeshift.ErrInvalid
		}
		if (segment.DurationTicks+media.TicksPerSecond/2)/media.TicksPerSecond > target {
			return nil, timeshift.ErrInvalid
		}
	}
	var output strings.Builder
	fmt.Fprintf(&output, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", target, snapshot.Segments[0].Sequence, snapshot.DiscontinuitySequence)
	if start != nil {
		fmt.Fprintf(&output, "#EXT-X-START:TIME-OFFSET=%s,PRECISE=NO\n", dynamicTicksSeconds(*start))
	}
	lastInit := ""
	for index, segment := range snapshot.Segments {
		// The leading discontinuity is represented in DISCONTINUITY-SEQUENCE.
		if segment.Discontinuity && index > 0 {
			output.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		var selected timeshift.Artifact
		for _, artifact := range segment.Artifacts {
			if artifact.VariantID == variantID {
				selected = artifact
			}
		}
		if selected.ID == "" {
			return nil, timeshift.ErrInvalid
		}
		if variant.Format == "fmp4" && selected.InitID != lastInit {
			if selected.InitID == "" {
				return nil, timeshift.ErrInvalid
			}
			address := resource(selected.InitID, variant.Format, true)
			if !validHLSManifestURL(address) {
				return nil, errInvalidHLSManifest
			}
			fmt.Fprintf(&output, "#EXT-X-MAP:URI=\"%s\"\n", address)
			lastInit = selected.InitID
		}
		address := resource(selected.ID, variant.Format, false)
		if !validHLSManifestURL(address) {
			return nil, errInvalidHLSManifest
		}
		fmt.Fprintf(&output, "#EXTINF:%s,\n%s\n", dynamicTicksSeconds(segment.DurationTicks), address)
		if output.Len() > maxHLSManifestBytes {
			return nil, errHLSManifestLimit
		}
	}
	if snapshot.Ended {
		output.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(output.String()), nil
}

func dynamicOpaqueArtifactID(name string) (string, bool) {
	for _, suffix := range []string{".mp4", ".m4s", ".ts"} {
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		id := strings.TrimSuffix(name, suffix)
		if len(id) == 34 && strings.HasPrefix(id, "a_") && strings.Trim(id[2:], "0123456789abcdef") == "" {
			return id, true
		}
	}
	return "", false
}

func (s *Server) serveDynamicMediaArtifact(w http.ResponseWriter, r *http.Request, session *dynamicStreamSession, name, id string) bool {
	artifact, variant, err := s.dynamicStreams.store.ResolveArtifact(r.Context(), timeshiftScope(session.scope), session.windowID, id)
	if err != nil {
		s.dynamicWindowError(w, r, err)
		return false
	}
	if name != dynamicArtifactName(artifact.ID, variant.Format, artifact.Initialization) {
		s.dynamicWindowError(w, r, timeshift.ErrNotFound)
		return false
	}
	handle, err := s.dynamicStreams.store.OpenArtifact(r.Context(), timeshiftScope(session.scope), session.windowID, artifact.ID)
	if err != nil {
		s.dynamicWindowError(w, r, err)
		return false
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		s.dynamicWindowError(w, r, err)
		return false
	}
	values, err := hlsValues(r)
	if err != nil {
		s.dynamicWindowError(w, r, timeshift.ErrInvalid)
		return false
	}
	if _, _, err := s.findDynamicSession(r, values); err != nil {
		s.dynamicWindowError(w, r, err)
		return false
	}
	contentType := "video/mp2t"
	if variant.Format == "fmp4" {
		contentType = "video/mp4"
		if variant.Kind == "audio" {
			contentType = "audio/mp4"
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, no-transform")
	w.Header().Set("ETag", strconv.Quote(artifact.ID))
	writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer writer.finish()
	http.ServeContent(writer, r, name, info.ModTime(), handle)
	if r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
	return writer.err == nil && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent || writer.status == http.StatusNotModified)
}

func dynamicTicksSeconds(ticks int64) string {
	return strconv.FormatInt(ticks/media.TicksPerSecond, 10) + "." + fmt.Sprintf("%07d", ticks%media.TicksPerSecond)
}

func (s *Server) heartbeatDynamicPlayback(ctx context.Context, principal identity.Principal, play library.PlaySession) {
	if s.dynamicStreams == nil || s.dynamicSources == nil || !play.IsDynamic || play.State == "Stopped" || play.State == "Expired" {
		return
	}
	owner := dynamicSourceOwner(principal)
	s.dynamicStreams.mu.Lock()
	var sessions []*dynamicStreamSession
	for _, session := range s.dynamicStreams.sessions {
		if session.key.owner == owner.Identity() && session.scope.PlaySessionID == play.ID && session.scope.ItemID == play.ItemID && session.scope.SourceID == play.MediaSourceID {
			sessions = append(sessions, session)
		}
	}
	s.dynamicStreams.mu.Unlock()
	for _, session := range sessions {
		if _, err := s.dynamicSources.Info(ctx, owner, session.key.liveID); err != nil {
			s.retireDynamicSession(session)
			continue
		}
		if s.dynamicStreams.store != nil {
			if err := s.dynamicStreams.store.Touch(ctx, timeshiftScope(session.scope), session.windowID); err != nil {
				s.retireDynamicSession(session)
				continue
			}
		}
		s.touchDynamicProducer(session)
		session.mu.Lock()
		if !session.closed {
			session.accessed = time.Now()
		}
		session.mu.Unlock()
	}
}

// Only authenticated consumer requests and successful playback heartbeats may
// renew the engine's idle lease. Publication and status polling never call it.
func (s *Server) touchDynamicProducer(session *dynamicStreamSession) {
	if s.hls == nil {
		return
	}
	engine, ok := s.hls.manager.(interface {
		TouchStream(transcode.Scope, string) error
	})
	if !ok {
		return
	}
	session.mu.Lock()
	jobID, active := session.jobID, !session.closed && !session.producerEnded && session.producerStarted
	session.mu.Unlock()
	if active && jobID != "" {
		_ = engine.TouchStream(session.scope, jobID)
	}
}

func (s *Server) dynamicMediaWindow(ctx context.Context, session *dynamicStreamSession, wait, advertise bool) (timeshift.WindowSnapshot, error) {
	if s.dynamicStreams == nil || s.dynamicStreams.store == nil {
		return timeshift.WindowSnapshot{}, timeshift.ErrClosed
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	for {
		session.mu.Lock()
		changed, closed, ended := session.changed, session.closed, session.producerEnded
		session.mu.Unlock()
		if closed {
			return timeshift.WindowSnapshot{}, timeshift.ErrClosed
		}
		snapshot, err := s.dynamicStreams.store.Snapshot(ctx, timeshiftScope(session.scope), session.windowID)
		if err != nil {
			return snapshot, err
		}
		ready := len(snapshot.Segments) > 0
		if advertise {
			ready = ready && snapshot.TargetDurationTicks > 0 && snapshot.LiveEdgeTicks-snapshot.EarliestTicks >= 3*snapshot.TargetDurationTicks
		}
		if ready || snapshot.Ended || !advertise {
			return snapshot, nil
		}
		if !wait {
			return snapshot, timeshift.ErrNotFound
		}
		if ended {
			return snapshot, timeshift.ErrNotBuffered
		}
		select {
		case <-ctx.Done():
			return snapshot, ctx.Err()
		case <-session.ctx.Done():
			return snapshot, timeshift.ErrClosed
		case <-deadline.C:
			return snapshot, timeshift.ErrNotBuffered
		case <-changed:
		}
	}
}

func writeDynamicWindow(w http.ResponseWriter, r *http.Request, session *dynamicStreamSession, snapshot timeshift.WindowSnapshot, token string, view dynamicPlaybackView) {
	live := view
	live.StartTicks, live.Live = nil, true
	body, err := json.Marshal(map[string]any{
		"PresentationId": session.id, "EarliestTicks": snapshot.EarliestTicks, "LiveEdgeTicks": snapshot.LiveEdgeTicks,
		"LiveStartTicks": snapshot.LiveStartTicks, "IsEnded": snapshot.Ended, "IsStalled": snapshot.Stalled,
		"CanSeek": len(snapshot.Segments) > 0, "BufferedBytes": snapshot.Bytes,
		"LiveUrl": dynamicArtifactURLView(session, "master.m3u8", token, live, true),
	})
	if err != nil {
		apiError(w, r, http.StatusInternalServerError, "window_unavailable", "The playback window could not be described.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (s *Server) dynamicWindowError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, timeshift.ErrWindowExpired):
		apiError(w, r, http.StatusGone, "window_expired", "The requested media is outside the retained playback window.")
	case errors.Is(err, timeshift.ErrNotBuffered):
		w.Header().Set("Retry-After", "1")
		apiError(w, r, http.StatusServiceUnavailable, "media_not_buffered", "The requested media has not been buffered yet.")
	case errors.Is(err, timeshift.ErrSnapshotChanged):
		w.Header().Set("Retry-After", "1")
		apiError(w, r, http.StatusServiceUnavailable, "window_changed", "The playback window changed while its playlist was being prepared.")
	case errors.Is(err, timeshift.ErrInvalid):
		apiError(w, r, http.StatusBadRequest, "invalid_timeshift_request", "Supply a valid retained playback position and output view.")
	case errors.Is(err, timeshift.ErrNotFound), errors.Is(err, timeshift.ErrClosed):
		apiError(w, r, http.StatusNotFound, "not_found", "The playback window or artifact was not found.")
	case errors.Is(err, timeshift.ErrQuota), errors.Is(err, timeshift.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "timeshift_limit", "The bounded playback storage is at capacity.")
	default:
		s.liveStreamError(w, r, err)
	}
}
