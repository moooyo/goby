package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func (s *Server) registerAudioWaveformRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/audio-waveforms/items", s.requireAdmin(s.adminAudioWaveformItems))
	mux.HandleFunc("GET /admin/v1/items/{id}/audio-waveforms", s.requireAdmin(s.adminAudioWaveformItem))
	mux.HandleFunc("GET /emby/Items/{Id}/AudioWaveforms", s.requireEmby(s.embyAudioWaveforms))
	mux.HandleFunc("GET /emby/Items/{Id}/AudioWaveforms/{StreamIndex}", s.requireEmby(s.embyAudioWaveformLevel))
}

func (s *Server) audioWaveformError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrAudioWaveformConflict) || errors.Is(err, library.ErrAudioWaveformStorageConflict) {
		apiError(w, r, http.StatusConflict, "audio_waveform_conflict", "The waveform state changed or its existing files need explicit review.")
		return
	}
	if errors.Is(err, library.ErrAudioWaveformStale) {
		apiError(w, r, http.StatusNotFound, "audio_waveform_stale", "The preserved waveform belongs to an earlier source. Explicit regeneration is required.")
		return
	}
	s.libraryError(w, r, err)
}

func (s *Server) adminAudioWaveformItems(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	start, limit := 0, 50
	valid := err == nil && (!r.URL.ForceQuery || r.URL.RawQuery != "")
	for name, values := range query {
		if len(values) != 1 || values[0] == "" {
			valid = false
			break
		}
		switch name {
		case "LibraryId", "SearchTerm", "State":
		case "StartIndex", "Limit":
			value, err := strconv.Atoi(values[0])
			if err != nil || value < 0 || strconv.Itoa(value) != values[0] {
				valid = false
				break
			}
			if name == "Limit" {
				limit = value
			} else {
				start = value
			}
		default:
			valid = false
		}
	}
	if !valid || limit < 1 || limit > 200 || start > 1000000 {
		adminTaskInputError(w, r, map[string]string{"Query": "Supply supported waveform filters and pagination."})
		return
	}
	page, err := s.library.GetAudioWaveformItems(r.Context(), adminMediaAnalysisActor(r), query.Get("LibraryId"), query.Get("SearchTerm"), query.Get("State"), start, limit)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, page)
}

func (s *Server) adminAudioWaveformItem(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMediaAnalysisNoQuery(w, r) {
		return
	}
	actor := adminMediaAnalysisActor(r)
	detail, err := s.library.GetAudioWaveformItem(r.Context(), actor, id)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	artifact, err := s.library.GetAudioWaveformFor(r.Context(), library.Subject{UserID: actor.User.ID}, id)
	if err != nil && !errors.Is(err, library.ErrNotFound) {
		s.audioWaveformError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, struct {
		library.AudioWaveformDetail
		Artifact library.AudioWaveformArtifact `json:"Artifact"`
	}{detail, artifact})
}

func (s *Server) startAdminAudioWaveformRun(w http.ResponseWriter, r *http.Request, input adminMediaAnalysisRunInput) {
	if s.taskStore == nil || s.taskManager == nil || !(audioWaveformTaskExecutor{s}).Available() {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.GetByKey(r.Context(), library.TaskAudioWaveformGenerationKey)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	if !definition.Enabled {
		s.taskError(w, r, tasks.ErrDisabled)
		return
	}
	queued, err := s.library.QueueAudioWaveforms(r.Context(), adminMediaAnalysisActor(r), input.Selection, input.RequestID)
	if err != nil {
		s.audioWaveformError(w, r, err)
		return
	}
	result, err := s.taskManager.Start(r.Context(), adminTaskActor(r), tasks.StartRequest{TaskID: definition.ID, RequestID: input.RequestID})
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"RunId": result.Run.ID, "TaskId": result.Run.TaskID, "Admitted": result.Admitted, "Queued": queued.Queued})
}

func audioWaveformQuery(w http.ResponseWriter, r *http.Request, level bool) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	valid := err == nil && (!r.URL.ForceQuery || r.URL.RawQuery != "")
	for name, values := range query {
		if len(values) != 1 || values[0] == "" {
			valid = false
			break
		}
		if name == "UserId" || name == "api_key" {
			continue
		}
		if level && (name == "buckets" || name == "tag") {
			continue
		}
		valid = false
	}
	if !valid {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply documented waveform query parameters once.")
	}
	return query, valid
}

type audioWaveformLevelDTO struct {
	BucketCount int    `json:"BucketCount"`
	URL         string `json:"Url"`
}
type audioWaveformStreamDTO struct {
	StreamIndex   int                     `json:"StreamIndex"`
	Channels      int                     `json:"Channels"`
	SampleRate    int                     `json:"SampleRate"`
	ChannelLayout string                  `json:"ChannelLayout,omitempty"`
	Levels        []audioWaveformLevelDTO `json:"Levels"`
}
type audioWaveformDescriptor struct {
	Available     bool                     `json:"Available"`
	Stale         bool                     `json:"Stale,omitempty"`
	MediaSourceID string                   `json:"MediaSourceId,omitempty"`
	SourceVersion string                   `json:"SourceVersion,omitempty"`
	DurationTicks int64                    `json:"DurationTicks,omitempty"`
	Streams       []audioWaveformStreamDTO `json:"Streams,omitempty"`
}

func (s *Server) embyAudioWaveforms(w http.ResponseWriter, r *http.Request) {
	if _, ok := audioWaveformQuery(w, r, false); !ok {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	subject, id := requestLibrarySubject(r, userID), r.PathValue("Id")
	item, err := s.library.GetItemFor(r.Context(), subject, id)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	if item.IsFolder || item.Media == nil || item.Type != "Movie" && item.Type != "Episode" {
		jsonResponse(w, http.StatusOK, audioWaveformDescriptor{})
		return
	}
	artifact, err := s.library.GetAudioWaveformFor(r.Context(), subject, id)
	if errors.Is(err, library.ErrNotFound) {
		if _, err := s.library.GetItemFor(r.Context(), subject, id); err != nil {
			s.libraryError(w, r, err)
			return
		}
		jsonResponse(w, http.StatusOK, audioWaveformDescriptor{})
		return
	}
	if err != nil {
		s.audioWaveformError(w, r, err)
		return
	}
	result := audioWaveformDescriptor{Available: artifact.Available && !artifact.Stale, Stale: artifact.Stale}
	if result.Available {
		result.MediaSourceID, result.SourceVersion, result.DurationTicks = media.SourceID(id), artifact.SourceStamp, artifact.DurationTicks
		for _, track := range artifact.Tracks {
			stream := audioWaveformStreamDTO{StreamIndex: track.StreamIndex, Channels: track.Channels, SampleRate: track.SampleRate, ChannelLayout: track.ChannelLayout}
			for _, count := range []int{512, 1024, 2048, 4096} {
				query := url.Values{"buckets": {strconv.Itoa(count)}, "tag": {artifact.ETag}}
				if r.URL.Query().Has("UserId") {
					query.Set("UserId", userID)
				}
				stream.Levels = append(stream.Levels, audioWaveformLevelDTO{count, "/emby/Items/" + url.PathEscape(id) + "/AudioWaveforms/" + strconv.Itoa(track.StreamIndex) + "?" + query.Encode()})
			}
			result.Streams = append(result.Streams, stream)
		}
	}
	jsonResponse(w, http.StatusOK, result)
}

// GAWL is a bounded little-endian projection of one validated stored level.
// Validity bits distinguish absent presentation samples from digital silence.
func marshalAudioWaveformLevel(duration int64, index int, level media.AudioWaveformLevel) ([]byte, error) {
	count := level.BucketCount
	if duration <= 0 || index < 0 || index > 4095 || count != 512 && count != 1024 && count != 2048 && count != 4096 || len(level.Peaks) != count || len(level.RMS) != count || len(level.Validity) != (count+7)/8 {
		return nil, media.ErrAudioWaveformUnsupported
	}
	data := make([]byte, 32+count*4+len(level.Validity))
	copy(data, "GAWL")
	binary.LittleEndian.PutUint16(data[4:], 1)
	binary.LittleEndian.PutUint16(data[6:], 32)
	binary.LittleEndian.PutUint32(data[8:], uint32(index))
	binary.LittleEndian.PutUint32(data[12:], uint32(count))
	binary.LittleEndian.PutUint64(data[16:], uint64(duration))
	binary.LittleEndian.PutUint32(data[24:], uint32(len(level.Validity)))
	for i, peak := range level.Peaks {
		if level.RMS[i] > peak || level.Validity[i/8]&(1<<uint(i%8)) == 0 && (peak != 0 || level.RMS[i] != 0) {
			return nil, media.ErrAudioWaveformUnsupported
		}
		binary.LittleEndian.PutUint16(data[32+i*4:], peak)
		binary.LittleEndian.PutUint16(data[34+i*4:], level.RMS[i])
	}
	copy(data[32+count*4:], level.Validity)
	return data, nil
}

func (s *Server) embyAudioWaveformLevel(w http.ResponseWriter, r *http.Request) {
	query, ok := audioWaveformQuery(w, r, true)
	if !ok {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	index, indexErr := strconv.Atoi(r.PathValue("StreamIndex"))
	count, countErr := strconv.Atoi(query.Get("buckets"))
	if indexErr != nil || index < 0 || index > 4095 || strconv.Itoa(index) != r.PathValue("StreamIndex") || countErr != nil || strconv.Itoa(count) != query.Get("buckets") || count != 512 && count != 1024 && count != 2048 && count != 4096 || query.Get("tag") == "" {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply a stream index, supported bucket count, and current version tag.")
		return
	}
	data, artifact, err := s.library.ReadAudioWaveformFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"))
	if err != nil {
		s.audioWaveformError(w, r, err)
		return
	}
	if query.Get("tag") != artifact.ETag {
		apiError(w, r, http.StatusNotFound, "audio_waveform_changed", "The waveform changed. Reload its current descriptor.")
		return
	}
	for _, track := range data.Tracks {
		if track.StreamIndex != index {
			continue
		}
		for _, level := range track.Levels {
			if level.BucketCount != count {
				continue
			}
			payload, err := marshalAudioWaveformLevel(data.DurationTicks, index, level)
			if err != nil {
				s.audioWaveformError(w, r, library.ErrAudioWaveformStorageConflict)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "private, no-cache")
			w.Header().Set("ETag", `"`+strings.Trim(artifact.ETag, `"`)+"-"+strconv.Itoa(index)+"-"+strconv.Itoa(count)+`"`)
			http.ServeContent(w, r, "waveform.bin", artifact.ModifiedAt, bytes.NewReader(payload))
			return
		}
	}
	apiError(w, r, http.StatusNotFound, "not_found", "The waveform track or level was not found.")
}
