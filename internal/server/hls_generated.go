package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsArtifactURL(session *hlsSession, resource, name, token string, start int64) string {
	return hlsSessionURL(session, resource, "hls2/"+session.id+"/"+name, token, start)
}

func hlsGeneratedMaster(session *hlsSession, resource, token string, start int64) ([]byte, error) {
	view, err := hlsSubtitleRequestView(nil, session)
	if err != nil {
		return nil, err
	}
	return hlsGeneratedMasterView(session, resource, token, start, view)
}

func hlsGeneratedMasterView(session *hlsSession, resource, token string, start int64, view playback.HLSSubtitleView) ([]byte, error) {
	plan := session.key.plan
	var result strings.Builder
	result.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	if err := writeHLSSubtitleRenditions(&result, session, resource, token, start, view); err != nil {
		return nil, err
	}
	for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
		bandwidth, width, height := session.output.Info.Bitrate, plan.Width, plan.Height
		if width == 0 {
			for _, stream := range session.output.Info.Streams {
				if stream.CodecType == "video" && !stream.IsAttachedPicture {
					width, height = stream.Width, stream.Height
					break
				}
			}
		}
		if plan.HLS.RenditionCount > 0 {
			r := plan.HLS.Renditions[index]
			width, height = r.Width, r.Height
			bandwidth = (r.VideoBitrate + max(plan.AudioBitrate, hlsOutputAudioBitrate(session))) * 10 / 9
		}
		if bandwidth <= 0 {
			return nil, errInvalidHLSManifest
		}
		child := hlsArtifactURL(session, resource, transcode.HLSPlaylistName(index, plan.HLS.RenditionCount), token, start)
		if transcode.HasHLSSubtitles(plan) {
			child = hlsSubtitleURLView(child, view)
		}
		if !validHLSManifestURL(child) {
			return nil, errInvalidHLSManifest
		}
		fmt.Fprintf(&result, "#EXT-X-STREAM-INF:BANDWIDTH=%d", bandwidth)
		if width > 0 && height > 0 {
			fmt.Fprintf(&result, ",RESOLUTION=%dx%d", width, height)
		}
		if plan.FrameRate > 0 {
			fmt.Fprintf(&result, ",FRAME-RATE=%.3f", plan.FrameRate)
		}
		if transcode.HasHLSSubtitles(plan) {
			result.WriteString(",SUBTITLES=\"subs\"")
		}
		fmt.Fprintf(&result, "\n%s\n", child)
	}
	return []byte(result.String()), nil
}

func hlsOutputAudioBitrate(session *hlsSession) int64 {
	for _, stream := range session.output.Info.Streams {
		if stream.CodecType == "audio" {
			return max(stream.Bitrate, session.output.Info.Bitrate*9/10-session.key.plan.VideoBitrate)
		}
	}
	return 0
}

// generatedArtifact borrows input and shares one producer across every variant
// and map. Only an independently owned descriptor is transferred to Ensure.
// Scope remains in every lookup, and retirement cancels the complete graph.
func (h *hlsRuntime) generatedArtifact(ctx context.Context, session *hlsSession, input *os.File, name string) (*transcode.ReadHandle, error) {
	if !hlsPlanArtifact(session.key.plan, name) {
		return nil, transcode.ErrJobNotFound
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil, transcode.ErrJobNotFound
	}
	for _, producer := range session.producers {
		if producer.first != -1 {
			continue
		}
		state, err := h.manager.Snapshot(session.key.scope, producer.id)
		if err == nil && (state.State == "queued" || state.State == "running" || state.State == "completed") {
			session.accessed = time.Now()
			session.mu.Unlock()
			return h.manager.Open(ctx, session.key.scope, producer.id, name)
		}
	}
	producerInput, err := transcode.DuplicateInput(input)
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}
	record, err := h.manager.Ensure(ctx, transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: session.key.plan}, producerInput)
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}
	if len(session.producers) >= maxHLSProducers {
		_ = h.manager.CancelJob(session.producers[0].id, session.key.scope)
		session.producers = session.producers[1:]
	}
	session.producers = append(session.producers, hlsProducer{id: record.ID, first: -1, last: -1})
	session.accessed = time.Now()
	session.mu.Unlock()
	return h.manager.Open(ctx, session.key.scope, record.ID, name)
}

func hlsPlanArtifact(plan transcode.Plan, name string) bool {
	_, valid := transcode.HLSArtifact(name)
	if !valid || !transcode.GeneratedHLS(plan) {
		return false
	}
	count := plan.HLS.RenditionCount
	if strings.HasSuffix(name, ".m3u8") {
		for index := 0; index < max(1, count); index++ {
			if name == transcode.HLSPlaylistName(index, count) {
				return true
			}
		}
		return false
	}
	prefix := ""
	if count > 0 {
		if len(name) < 3 || name[0] != 'v' || name[1] < '0' || int(name[1]-'0') >= count || name[2] != '-' {
			return false
		}
		prefix = name[:3]
	} else if strings.HasPrefix(name, "v") {
		return false
	}
	if name == prefix+"init.mp4" {
		return plan.HLS.SegmentType == "fmp4"
	}
	ext := ".ts"
	if plan.HLS.SegmentType == "fmp4" {
		ext = ".m4s"
	}
	if plan.HLS.SegmentType == "packed" {
		ext = "." + plan.AudioCodec
	}
	return strings.HasPrefix(name, prefix+"segment-") && strings.HasSuffix(name, ext)
}

func (h *hlsRuntime) generatedPlaylist(ctx context.Context, session *hlsSession, input *os.File, name, resource, token string, start int64, views ...playback.HLSSubtitleView) ([]byte, error) {
	view, err := hlsSubtitleRequestView(nil, session)
	if err != nil {
		return nil, err
	}
	if len(views) != 0 {
		view = views[0]
	}
	handle, err := h.generatedArtifact(ctx, session, input, name)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, transcode.MaxPlaylistBytes+1))
	if err != nil {
		return nil, err
	}
	prefix := ""
	if name != "main.m3u8" {
		prefix = strings.TrimSuffix(name, ".m3u8") + "-"
	}
	data, err = transcode.RewriteMediaPlaylistWithMap(data, func(segment transcode.MediaSegment) string {
		if !hlsPlanArtifact(session.key.plan, segment.Name) || !strings.HasPrefix(segment.Name, prefix+"segment-") {
			return ""
		}
		child := hlsArtifactURL(session, resource, segment.Name, token, start)
		if transcode.HasHLSSubtitles(session.key.plan) {
			child = hlsSubtitleURLView(child, view)
		}
		return child
	}, func(init string) string {
		if !hlsPlanArtifact(session.key.plan, init) || init != prefix+"init.mp4" {
			return ""
		}
		child := hlsArtifactURL(session, resource, init, token, start)
		if transcode.HasHLSSubtitles(session.key.plan) {
			child = hlsSubtitleURLView(child, view)
		}
		return child
	})
	if err == nil && start > 0 {
		data = []byte(strings.Replace(string(data), "#EXTM3U\n", fmt.Sprintf("#EXTM3U\n#EXT-X-START:TIME-OFFSET=%d.%07d,PRECISE=YES\n", start/media.TicksPerSecond, start%media.TicksPerSecond), 1))
	}
	if len(data) > maxHLSManifestBytes {
		return nil, errHLSManifestLimit
	}
	return data, err
}

func (s *Server) hlsArtifact(audioOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, done, ok := s.beginHLS(w, r)
		if !ok {
			return
		}
		defer done()
		values, err := hlsValues(r)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		values["gobyhlsid"] = r.PathValue("PlaylistId")
		session, input, source, err := s.resolveHLS(ctx, r, values)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		defer input.Close()
		if (source.Item.Type == "Audio") != audioOnly || !transcode.GeneratedHLS(session.key.plan) {
			s.hlsError(w, r, library.ErrNotFound)
			return
		}
		name := r.PathValue("Artifact")
		resource := "Videos"
		if audioOnly {
			resource = "Audio"
		}
		token, _, _ := parseEmbyCredentials(r)
		start, err := hlsStart(values, session.startHint, session.key.plan.DurationTicks)
		if session.key.plan.SourceMode == "stream" {
			start, err = 0, nil
		}
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		work, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(session.ctx, cancel)
		defer func() { stop(); cancel() }()
		view, err := hlsSubtitleRequestView(values, session)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		slot, sequence, subtitlePlaylist, allowedSubtitle := hlsSubtitleArtifact(session.key.plan, name, view)
		if !allowedSubtitle && !hlsPlanArtifact(session.key.plan, name) {
			s.hlsError(w, r, library.ErrNotFound)
			return
		}
		if r.Method == http.MethodHead {
			contentType := "video/mp2t"
			switch {
			case strings.HasSuffix(name, ".m3u8"):
				contentType = "application/vnd.apple.mpegurl"
			case strings.HasSuffix(name, ".vtt"):
				contentType = "text/vtt; charset=utf-8"
			case strings.HasSuffix(name, ".mp4"), strings.HasSuffix(name, ".m4s"):
				contentType = "video/mp4"
				if audioOnly {
					contentType = "audio/mp4"
				}
			case strings.HasSuffix(name, ".aac"):
				contentType = "audio/aac"
			case strings.HasSuffix(name, ".mp3"):
				contentType = "audio/mpeg"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			return
		}
		policyDelivered := false
		if r.Method != http.MethodHead {
			policyContext, release, policyErr := s.acquireMediaPolicy(work, r.Context().Value(principalKey).(identity.Principal), session.key.scope)
			if policyErr != nil {
				s.hlsError(w, r, policyErr)
				return
			}
			defer release()
			work = policyContext
			defer func() {
				if work.Err() == nil && policyDelivered {
					s.touchMediaPolicy(work, r.Context().Value(principalKey).(identity.Principal), session.key.scope)
				} else {
					s.failMediaPolicy(work, session.key.scope)
				}
			}()
		}
		if allowedSubtitle {
			if !subtitlePlaylist {
				policyDelivered = s.serveGeneratedHLSSubtitle(w, r.WithContext(work), session, input, slot, sequence, view)
				return
			}
			list, _, _, err := s.hls.subtitleMediaWindow(work, session, input)
			if err != nil {
				s.hlsError(w, r, err)
				return
			}
			body, err := hlsSubtitleManifest(session, resource, token, start, slot, view, list)
			if err != nil {
				s.hlsError(w, r, err)
				return
			}
			if !s.revalidateGeneratedHLS(work, w, r, session) {
				return
			}
			policyDelivered = true
			writeGeneratedHLSManifest(w, r, body)
			return
		}
		if !hlsPlanArtifact(session.key.plan, name) {
			s.hlsError(w, r, library.ErrNotFound)
			return
		}
		if strings.HasSuffix(name, ".m3u8") {
			body, err := s.hls.generatedPlaylist(work, session, input, name, resource, token, start, view)
			if err != nil {
				s.failMediaPolicy(work, session.key.scope)
				s.hlsError(w, r, err)
				return
			}
			if !s.revalidateGeneratedHLS(work, w, r, session) {
				s.failMediaPolicy(work, session.key.scope)
				return
			}
			policyDelivered = true
			writeGeneratedHLSManifest(w, r, body)
			return
		}
		handle, err := s.hls.generatedArtifact(work, session, input, name)
		if err != nil {
			s.failMediaPolicy(work, session.key.scope)
			s.hlsError(w, r, err)
			return
		}
		defer handle.Close()
		if !s.revalidateGeneratedHLS(work, w, r, session) {
			s.failMediaPolicy(work, session.key.scope)
			return
		}
		info, err := handle.Stat()
		if err != nil {
			s.failMediaPolicy(work, session.key.scope)
			s.hlsError(w, r, err)
			return
		}
		contentType := "video/mp2t"
		if strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".m4s") {
			contentType = "video/mp4"
		}
		if strings.HasSuffix(name, ".aac") {
			contentType = "audio/aac"
		}
		if strings.HasSuffix(name, ".mp3") {
			contentType = "audio/mpeg"
		}
		if audioOnly && contentType == "video/mp4" {
			contentType = "audio/mp4"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, no-transform")
		interrupt := context.AfterFunc(work, func() { _ = handle.Close() })
		defer interrupt()
		writer, err := newIdleResponseWriter(w, work, mediaWriteIdle)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		defer writer.finish()
		http.ServeContent(writer, r.WithContext(work), name, info.ModTime(), handle)
		if work.Err() != nil {
			panic(http.ErrAbortHandler)
		}
		policyDelivered = writer.err == nil && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent)
	}
}

func writeGeneratedHLSManifest(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (s *Server) revalidateGeneratedHLS(ctx context.Context, w http.ResponseWriter, r *http.Request, session *hlsSession) bool {
	principal := r.Context().Value(principalKey).(identity.Principal)
	file, _, err := s.authorizeHLS(ctx, principal, session.key.scope, session.key.stamp, session.key.plan)
	if err != nil {
		if permanentHLSError(err) {
			s.hls.retire(session)
		}
		s.hlsError(w, r, err)
		return false
	}
	_ = file.Close()
	return true
}
