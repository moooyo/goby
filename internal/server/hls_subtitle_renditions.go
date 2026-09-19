package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

// Every child carries its own presentation view. Reusing an A/V producer never
// changes the meaning of an earlier manifest, subtitle segment, or cache key.
func hlsSubtitleURLView(raw string, view playback.HLSSubtitleView) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	values := parsed.Query()
	values.Set("SubtitleStreamIndex", strconv.Itoa(view.SelectedStreamIndex))
	values.Set("SubtitleOffsetTicks", strconv.FormatInt(view.OffsetTicks, 10))
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func hlsSubtitleRequestView(values map[string]string, session *hlsSession) (playback.HLSSubtitleView, error) {
	if !transcode.HasHLSSubtitles(session.key.plan) && values["gobyhlsid"] == "" {
		// An initial burn/external request has already been validated by the
		// planner. Those controls are not an HLS text-rendition view.
		return playback.HLSSubtitleView{SelectedStreamIndex: -1}, nil
	}
	view := session.subtitleView
	if !view.SelectionSet {
		if session.key.plan.Subtitle.Mode == "hls" {
			view.OffsetTicks = session.key.plan.Subtitle.OffsetTicks
		}
		var err error
		view, err = playback.HLSSubtitleViewFor(session.key.plan, nil, view.OffsetTicks)
		if err != nil {
			return view, errHLSRequestInvalid
		}
	}
	selected := view.SelectedStreamIndex
	index, err := hlsQueryInteger(values, -1, 1<<31-1, "subtitlestreamindex")
	if err != nil {
		return view, err
	}
	if index != nil {
		selected = int(*index)
	}
	for _, key := range []string{"subtitlemethod", "subtitledeliverymethod"} {
		switch strings.ToLower(values[key]) {
		case "", "hls":
		case "none":
			if index != nil && selected != -1 {
				return view, errHLSRequestInvalid
			}
			selected = -1
		default:
			return view, errHLSRequestInvalid
		}
	}
	offset, err := hlsQueryInteger(values, -24*60*60*media.TicksPerSecond, 24*60*60*media.TicksPerSecond, "subtitleoffsetticks")
	if err != nil {
		return view, err
	}
	if offset != nil {
		view.OffsetTicks = *offset
	}
	view, err = playback.HLSSubtitleViewFor(session.key.plan, &selected, view.OffsetTicks)
	if err != nil {
		return view, errHLSRequestInvalid
	}
	return view, nil
}

func hlsSubtitleArtifactURL(session *hlsSession, resource, name, token string, start int64, view playback.HLSSubtitleView) string {
	return hlsSubtitleURLView(hlsArtifactURL(session, resource, name, token, start), view)
}

func hlsSubtitlePlaylistName(slot int) string { return "subtitles-" + strconv.Itoa(slot) + ".m3u8" }

func hlsSubtitleSegmentName(slot int, sequence int64) string {
	return fmt.Sprintf("subtitles-%d-segment-%06d.vtt", slot, sequence)
}

// Slots are stable within one bound track set. Stream selection in the query
// controls defaults only; it cannot retarget a previously published slot URL.
func hlsSubtitleArtifact(plan transcode.Plan, name string, view playback.HLSSubtitleView) (slot int, sequence int64, playlist, valid bool) {
	if !transcode.HasHLSSubtitles(plan) {
		return 0, 0, false, false
	}
	if name == "subtitles.m3u8" || name == "subtitles.vtt" {
		tracks := transcode.PlanHLSSubtitles(plan)
		for index := 0; index < tracks.Count; index++ {
			if tracks.Tracks[index].StreamIndex == view.SelectedStreamIndex {
				return index, -1, name == "subtitles.m3u8", true
			}
		}
		return 0, 0, false, false
	}
	for index := 0; index < transcode.PlanHLSSubtitles(plan).Count; index++ {
		if name == hlsSubtitlePlaylistName(index) {
			return index, -1, true, true
		}
		prefix := "subtitles-" + strconv.Itoa(index) + "-segment-"
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".vtt") {
			number, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".vtt"), 10, 32)
			if err == nil && number >= 0 && name == hlsSubtitleSegmentName(index, number) {
				return index, number, false, true
			}
		}
	}
	return 0, 0, false, false
}

func hlsSubtitleLabel(value string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\\' {
			return -1
		}
		if r == '"' {
			return '\''
		}
		return r
	}, value))
	return strings.TrimSpace(string(runes[:min(len(runes), 128)]))
}

func writeHLSSubtitleRenditions(out *strings.Builder, session *hlsSession, resource, token string, start int64, view playback.HLSSubtitleView) error {
	tracks := transcode.PlanHLSSubtitles(session.key.plan)
	for slot := 0; slot < tracks.Count; slot++ {
		track := tracks.Tracks[slot]
		metadata, _ := playback.HLSSubtitleMetadata(session.subtitleSource, session.key.plan, slot)
		label := hlsSubtitleLabel(metadata.Title)
		if label == "" {
			label = hlsSubtitleLabel(metadata.Language)
		}
		if label == "" {
			label = "Subtitle"
		}
		label += " [" + strconv.Itoa(track.StreamIndex) + "]"
		child := hlsSubtitleArtifactURL(session, resource, hlsSubtitlePlaylistName(slot), token, start, view)
		if !validHLSManifestURL(child) {
			return errInvalidHLSManifest
		}
		selected := "NO"
		if view.SelectedStreamIndex == track.StreamIndex {
			selected = "YES"
		}
		fmt.Fprintf(out, "#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=\"%s\",DEFAULT=%s,AUTOSELECT=%s", label, selected, selected)
		if language := hlsSubtitleLabel(metadata.Language); language != "" {
			fmt.Fprintf(out, ",LANGUAGE=\"%s\"", language)
		}
		fmt.Fprintf(out, ",URI=\"%s\"\n", child)
	}
	return nil
}

func (s *Server) authorizeHLSSubtitles(ctx context.Context, principal identity.Principal, scope transcode.Scope, source library.MediaFile, plan transcode.Plan) error {
	tracks := transcode.PlanHLSSubtitles(plan)
	planning := playback.Source{ItemID: source.Item.ID, MediaSourceID: source.SourceID, ItemType: source.Item.Type, Info: playbackMediaInfo(source.Item)}
	for slot := 0; slot < tracks.Count; slot++ {
		if _, ok := playback.HLSSubtitleMetadata(planning, plan, slot); !ok {
			return library.ErrSourceChanged
		}
		track := tracks.Tracks[slot]
		if track.ExternalTag != "" {
			bound := plan
			bound.Subtitle = transcode.SubtitlePlan{Mode: "hls", StreamIndex: track.StreamIndex, Codec: track.Codec, ExternalTag: track.ExternalTag}
			if _, err := s.readPlannedExternalSubtitle(ctx, principal, scope, bound); err != nil {
				return err
			}
		}
	}
	return nil
}

type hlsSubtitleWindow struct {
	Start, End    int64
	Discontinuity bool
}

type hlsSubtitleTimeline struct {
	Job     string
	Windows map[int64]hlsSubtitleWindow
}

// Observe actual EXTINF boundaries, retaining the epoch anchor after segment
// zero leaves a rolling playlist. Unknown gaps are rejected rather than filled
// with sequence * nominal duration. Published overlapping boundaries cannot
// change under an existing segment URL.
func (timeline *hlsSubtitleTimeline) observe(job string, origin int64, list transcode.MediaPlaylist) (map[int64]hlsSubtitleWindow, error) {
	if job == "" || len(list.Segments) == 0 {
		return nil, transcode.ErrInvalidTimeline
	}
	if timeline.Job != job {
		if list.Sequence != 0 {
			return nil, transcode.ErrInvalidTimeline
		}
		timeline.Job, timeline.Windows = job, make(map[int64]hlsSubtitleWindow)
	}
	start := origin
	if list.Sequence != 0 {
		if first, ok := timeline.Windows[list.Sequence]; ok {
			start = first.Start
		} else if prior, ok := timeline.Windows[list.Sequence-1]; ok {
			start = prior.End
		} else {
			return nil, transcode.ErrInvalidTimeline
		}
	}
	result := make(map[int64]hlsSubtitleWindow, len(list.Segments))
	for _, segment := range list.Segments {
		if segment.DurationTicks <= 0 || start > 1<<62-segment.DurationTicks {
			return nil, transcode.ErrInvalidTimeline
		}
		window := hlsSubtitleWindow{Start: start, End: start + segment.DurationTicks, Discontinuity: segment.Discontinuity}
		if prior, ok := timeline.Windows[segment.Number]; ok && prior != window {
			return nil, transcode.ErrInvalidTimeline
		}
		result[segment.Number] = window
		start = window.End
	}
	for number, window := range result {
		timeline.Windows[number] = window
	}
	// Retain only bounded recent anchors; older published ranges are not
	// available again after the authoritative media window has evicted them.
	if len(timeline.Windows) > transcode.MaxPlaylistSegments {
		for number := range timeline.Windows {
			if number < list.Sequence {
				delete(timeline.Windows, number)
			}
		}
	}
	return result, nil
}

func (h *hlsRuntime) subtitleMediaWindow(ctx context.Context, session *hlsSession, input *os.File) (transcode.MediaPlaylist, map[int64]hlsSubtitleWindow, int64, error) {
	delta, err := h.subtitleClock(ctx, session, input)
	if err != nil {
		return transcode.MediaPlaylist{}, nil, 0, err
	}
	handle, err := h.generatedArtifact(ctx, session, input, transcode.HLSPlaylistName(0, session.key.plan.HLS.RenditionCount))
	if err != nil {
		return transcode.MediaPlaylist{}, nil, 0, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, transcode.MaxPlaylistBytes+1))
	if err != nil {
		return transcode.MediaPlaylist{}, nil, 0, err
	}
	list, err := transcode.ParseMediaPlaylist(data)
	if err != nil {
		return list, nil, 0, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.subtitleClockJob != handle.EncodingID() {
		return list, nil, 0, transcode.ErrOutputUnavailable
	}
	windows, err := session.subtitleWindows.observe(handle.EncodingID(), session.subtitleClockOrigin, list)
	return list, windows, delta, err
}

func hlsSubtitleManifest(session *hlsSession, resource, token string, start int64, slot int, view playback.HLSSubtitleView, list transcode.MediaPlaylist) ([]byte, error) {
	var out strings.Builder
	fmt.Fprintf(&out, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n", list.TargetDuration, list.Sequence)
	if list.Type != "" {
		fmt.Fprintf(&out, "#EXT-X-PLAYLIST-TYPE:%s\n", list.Type)
	}
	for _, segment := range list.Segments {
		child := hlsSubtitleArtifactURL(session, resource, hlsSubtitleSegmentName(slot, segment.Number), token, start, view)
		if !validHLSManifestURL(child) {
			return nil, errInvalidHLSManifest
		}
		if segment.Discontinuity {
			out.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&out, "#EXTINF:%d.%07d,\n%s\n", segment.DurationTicks/media.TicksPerSecond, segment.DurationTicks%media.TicksPerSecond, child)
		if out.Len() > maxHLSManifestBytes {
			return nil, errHLSManifestLimit
		}
	}
	if list.Ended {
		out.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(out.String()), nil
}

func (s *Server) registerHLSSubtitleRoutes(mux *http.ServeMux) {
	for _, name := range []string{"subtitles.m3u8", "live_subtitles.m3u8"} {
		mux.HandleFunc("GET /emby/Videos/{Id}/"+name, s.requireEmby(s.hlsStandardSubtitlePlaylist))
	}
}

// The SDK declares ManifestSubtitles as an opaque string without an encoding.
// This adapter accepts an existing Goby revision plus an explicit track view;
// it never interprets that opaque field as a URL, stream index, or JSON payload.
func (s *Server) hlsStandardSubtitlePlaylist(w http.ResponseWriter, r *http.Request) {
	if s.tryDynamicSubtitlePlaylist(w, r) {
		return
	}
	values, err := hlsValues(r)
	if err != nil || values["gobyhlsid"] == "" {
		s.hlsError(w, r, errHLSRequestInvalid)
		return
	}
	if values["manifestsubtitles"] != "" {
		s.hlsError(w, r, errHLSRequestUnsupported)
		return
	}
	if s.hls == nil {
		s.hlsError(w, r, transcode.ErrOutputUnavailable)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	session, err := s.hls.find(values["gobyhlsid"], principal, r.PathValue("Id"))
	if err != nil {
		s.hlsError(w, r, err)
		return
	}
	if session.key.plan.SourceMode != "" {
		s.hlsError(w, r, errHLSRequestUnsupported)
		return
	}
	length, err := hlsQueryInteger(values, 1, 86400, "subtitlesegmentlength")
	if err != nil || length != nil && *length != int64(session.key.plan.SegmentSeconds) {
		s.hlsError(w, r, errHLSRequestUnsupported)
		return
	}
	copy := r.Clone(r.Context())
	query := copy.URL.Query()
	for key := range query {
		if strings.EqualFold(key, "SubtitleSegmentLength") || strings.EqualFold(key, "ManifestSubtitles") {
			query.Del(key)
		}
	}
	copy.URL.RawQuery = query.Encode()
	copy.SetPathValue("PlaylistId", session.id)
	copy.SetPathValue("Artifact", "subtitles.m3u8")
	s.hlsArtifact(false)(w, copy)
}
