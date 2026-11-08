package library

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

const adminMediaStreamLimit = 256
const adminMediaChapterLimit = 4096

// A catalog page needs only the primary video and audio observations. Keep the
// bounded page projection independent of chapter, seek-index and track counts.
const adminMediaSummaryProjectionSQL = `jsonb_build_object(
	'ProbeVersion',i.media->'ProbeVersion','Container',i.media->'Container',
	'DurationTicks',i.media->'DurationTicks','Size',i.media->'Size','Bitrate',i.media->'Bitrate',
	'Streams',COALESCE((SELECT jsonb_agg(selected.value ORDER BY selected.ordinality) FROM (
		SELECT DISTINCT ON (stream.value->>'CodecType')
			stream.value - 'AudioTiming' - 'Filename' - 'SubtitleTag' - 'DolbyVision' AS value,stream.ordinality
		FROM jsonb_array_elements(CASE WHEN jsonb_typeof(i.media->'Streams')='array'
			THEN i.media->'Streams' ELSE '[]'::jsonb END) WITH ORDINALITY AS stream(value,ordinality)
		WHERE stream.value->>'CodecType' IN ('video','audio')
			AND (stream.value->>'CodecType'<>'video' OR stream.value->'IsAttachedPicture' IS DISTINCT FROM 'true'::jsonb)
		ORDER BY stream.value->>'CodecType',CASE WHEN stream.value->'IsDefault'='true'::jsonb THEN 0 ELSE 1 END,stream.ordinality
	) selected),'[]'::jsonb))`

// AdminMediaSummary describes indexed catalog observations. It does not assert
// that an external media file or a generated sidecar has been revalidated.
type AdminMediaSummary struct {
	State         string
	DurationTicks *string
	Size          *string
	Bitrate       *string
	Container     string
	Video         *AdminMediaStream
	Audio         *AdminMediaStream
	Images        AdminMediaImages
	Analysis      AdminMediaAnalysis
}

type AdminMediaImages struct {
	Count int
	Types []string
}

// Analysis fields retain the recorded detection or generation state. A null
// field means no record; a ready queue is not proof of current sidecar bytes.
type AdminMediaAnalysis struct {
	Intro             *string
	Credits           *string
	PreviewCount      int
	BackgroundPreview *string
	AudioWaveform     *string
	SubtitleTimeline  *string
}

// AdminMediaStream omits private decoder evidence, subtitle identity tokens,
// filenames and attachments while preserving the original stream index.
type AdminMediaStream struct {
	Index             int
	Type              string
	Codec             string
	Language          string
	Title             string
	Width             *int
	Height            *int
	Channels          *int
	SampleRate        *int
	Bitrate           *string
	Profile           string
	Level             *int
	BitDepth          *int
	PixelFormat       string
	FrameRate         string
	ChannelLayout     string
	VideoRange        string
	ColorSpace        string
	ColorTransfer     string
	ColorPrimaries    string
	IsDefault         bool
	IsForced          bool
	IsExternal        bool
	IsAttachedPicture bool
	IsHearingImpaired bool
}

type AdminMediaChapter struct {
	Index      int
	Name       string
	StartTicks string
	EndTicks   *string
}

type AdminMediaInfo struct {
	ID                string `json:"Id"`
	LibraryID         string `json:"LibraryId"`
	Summary           AdminMediaSummary
	ProbeVersion      *int
	UpdatedAt         time.Time
	Streams           []AdminMediaStream
	Chapters          []AdminMediaChapter
	StreamCount       int
	ChapterCount      int
	StreamsTruncated  bool
	ChaptersTruncated bool
}

// GetAdminMediaInfo is read-only and uses indexed database data exclusively.
// Revalidation after the repeatable snapshot closes rejects a revoked actor.
func (s *Store) GetAdminMediaInfo(ctx context.Context, actor identity.Principal, itemID string) (AdminMediaInfo, error) {
	if !metadataIdentifier(itemID) {
		return AdminMediaInfo{}, ErrInvalidInput
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return AdminMediaInfo{}, err
	}
	defer rollback(tx)
	items, err := readAdminMediaInfo(ctx, tx, []string{itemID}, true)
	if err != nil {
		return AdminMediaInfo{}, err
	}
	result, exists := items[itemID]
	if !exists {
		return AdminMediaInfo{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return AdminMediaInfo{}, fmt.Errorf("commit administrator media read: %w", err)
	}
	if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
		return AdminMediaInfo{}, err
	}
	return result, nil
}

// The caller has selected at most one bounded page in its authenticated native
// administrator snapshot. All attachments below are batch queries over that
// same page; none opens a source or performs one query per item.
func readAdminMediaInfo(ctx context.Context, tx pgx.Tx, ids []string, details bool) (map[string]AdminMediaInfo, error) {
	result := make(map[string]AdminMediaInfo, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 200 {
		return nil, ErrInvalidInput
	}
	subtitleItems := make([]Item, 0, len(ids))
	projection := "i.media - 'VideoSeekIndexes' - 'EmbeddedMusic'"
	if !details {
		projection = adminMediaSummaryProjectionSQL
	}
	rows, err := tx.Query(ctx, `SELECT i.id,i.library_id,i.is_folder,
		CASE WHEN jsonb_typeof(i.media)='object' THEN `+projection+` ELSE NULL END,
		i.file_size,i.updated_at,
		COALESCE(CASE WHEN intro.start_ticks IS NOT NULL THEN 'manual' END, detection.status),
		COALESCE(CASE WHEN credits.start_ticks IS NOT NULL THEN 'manual' END, credits_detection.status),
		(SELECT count(*) FROM analysis_previews preview WHERE preview.item_id=i.id),
		background.state,waveform.state,timeline.state
		FROM items i
		LEFT JOIN item_intro_state intro ON intro.item_id=i.id
		LEFT JOIN item_credits_state credits ON credits.item_id=i.id
		LEFT JOIN analysis_detections detection ON detection.item_id=i.id
		LEFT JOIN analysis_credits_detections credits_detection ON credits_detection.item_id=i.id
		LEFT JOIN background_preview_queue background ON background.item_id=i.id
		LEFT JOIN audio_waveform_queue waveform ON waveform.item_id=i.id
		LEFT JOIN subtitle_timeline_queue timeline ON timeline.item_id=i.id
		WHERE i.id=ANY($1::text[]) AND i.library_id<>$2 AND `+ordinaryItemSQL("i"), ids, collectionLibraryID)
	if err != nil {
		return nil, fmt.Errorf("query administrator media facts: %w", err)
	}
	for rows.Next() {
		var value AdminMediaInfo
		var raw []byte
		var folder bool
		var fileSize int64
		if err := rows.Scan(&value.ID, &value.LibraryID, &folder, &raw, &fileSize, &value.UpdatedAt,
			&value.Summary.Analysis.Intro, &value.Summary.Analysis.Credits, &value.Summary.Analysis.PreviewCount,
			&value.Summary.Analysis.BackgroundPreview, &value.Summary.Analysis.AudioWaveform, &value.Summary.Analysis.SubtitleTimeline); err != nil {
			rows.Close()
			return nil, fmt.Errorf("read administrator media facts: %w", err)
		}
		if err := projectAdminMediaInfo(&value, raw, folder, fileSize, details); err != nil {
			rows.Close()
			return nil, err
		}
		if details && !folder && value.Summary.State == "available" {
			var indexed media.Info
			if err := json.Unmarshal(raw, &indexed); err != nil {
				rows.Close()
				return nil, fmt.Errorf("%w: invalid indexed subtitle source", ErrUnavailable)
			}
			subtitleItems = append(subtitleItems, Item{ID: value.ID, LibraryID: value.LibraryID, Media: &indexed})
		}
		result[value.ID] = value
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("complete administrator media facts: %w", err)
	}
	if details && len(subtitleItems) != 0 {
		// Reuse the public namespace allocator's indexed text, owned and bitmap
		// projection in this exact authorized snapshot. It performs no file I/O.
		if err := attachSubtitles(ctx, tx, subtitleItems); err != nil {
			return nil, err
		}
		for _, item := range subtitleItems {
			value := result[item.ID]
			if err := appendAdminMediaSubtitles(&value, item); err != nil {
				return nil, err
			}
			result[item.ID] = value
		}
	}
	images, err := searchHintItemImages(ctx, tx, unrestrictedLibraryAccess(), ids)
	if err != nil {
		return nil, err
	}
	for id, value := range result {
		types := make(map[string]bool)
		value.Summary.Images.Count = len(images[id])
		for _, image := range images[id] {
			types[image.ImageType] = true
		}
		for name := range types {
			value.Summary.Images.Types = append(value.Summary.Images.Types, name)
		}
		sort.Strings(value.Summary.Images.Types)
		result[id] = value
	}
	return result, nil
}

func projectAdminMediaInfo(value *AdminMediaInfo, raw []byte, folder bool, fileSize int64, details bool) error {
	value.UpdatedAt = value.UpdatedAt.UTC()
	value.Streams, value.Chapters = []AdminMediaStream{}, []AdminMediaChapter{}
	value.ProbeVersion = nil
	value.StreamCount, value.ChapterCount = 0, 0
	value.StreamsTruncated, value.ChaptersTruncated = false, false
	value.Summary = AdminMediaSummary{State: "not_scanned", Images: AdminMediaImages{Types: []string{}}, Analysis: value.Summary.Analysis}
	if folder {
		value.Summary.State = "not_applicable"
		return nil
	}
	value.Summary.Size = positiveMediaDecimal(fileSize)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var info media.Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return fmt.Errorf("%w: invalid indexed media information", ErrUnavailable)
	}
	if info.DurationTicks <= 0 && len(info.Streams) == 0 && info.Size <= 0 && info.Container == "" {
		return nil
	}
	value.Summary.State = "available"
	value.ProbeVersion = positiveMediaInt(info.ProbeVersion)
	value.Summary.DurationTicks = positiveMediaDecimal(info.DurationTicks)
	value.Summary.Bitrate = positiveMediaDecimal(info.Bitrate)
	value.Summary.Container = info.Container
	if size := positiveMediaDecimal(info.Size); size != nil {
		value.Summary.Size = size
	}
	value.StreamCount = len(info.Streams)
	value.ChapterCount = len(info.Chapters)
	value.StreamsTruncated = len(info.Streams) > adminMediaStreamLimit
	value.ChaptersTruncated = len(info.Chapters) > adminMediaChapterLimit
	for index, stream := range info.Streams {
		projected := projectAdminMediaStream(stream)
		if stream.CodecType == "video" && !stream.IsAttachedPicture &&
			(value.Summary.Video == nil || stream.IsDefault && !value.Summary.Video.IsDefault) {
			selected := projected
			value.Summary.Video = &selected
		}
		if stream.CodecType == "audio" &&
			(value.Summary.Audio == nil || stream.IsDefault && !value.Summary.Audio.IsDefault) {
			selected := projected
			value.Summary.Audio = &selected
		}
		if details && index < adminMediaStreamLimit {
			value.Streams = append(value.Streams, projected)
		}
	}
	if !details {
		return nil
	}
	for index, chapter := range info.Chapters {
		if index == adminMediaChapterLimit {
			break
		}
		if chapter.StartTicks < 0 {
			return fmt.Errorf("%w: invalid indexed chapter timestamp", ErrUnavailable)
		}
		var end *string
		if chapter.EndTicks > chapter.StartTicks {
			end = positiveMediaDecimal(chapter.EndTicks)
		}
		value.Chapters = append(value.Chapters, AdminMediaChapter{Index: index, Name: chapter.Title,
			StartTicks: strconv.FormatInt(chapter.StartTicks, 10), EndTicks: end})
	}
	return nil
}

func projectAdminMediaStream(stream media.Stream) AdminMediaStream {
	frameRate := stream.AverageFrameRate
	if frameRate == "" || frameRate == "0/0" {
		frameRate = stream.RealFrameRate
	}
	videoRange := ""
	if stream.VideoRangeKnown {
		videoRange = stream.VideoRange
	}
	return AdminMediaStream{
		Index: stream.Index, Type: stream.CodecType, Codec: stream.Codec, Language: stream.Language, Title: stream.Title,
		Width: positiveMediaInt(stream.Width), Height: positiveMediaInt(stream.Height), Channels: positiveMediaInt(stream.Channels),
		SampleRate: positiveMediaInt(stream.SampleRate), Bitrate: positiveMediaDecimal(stream.Bitrate), Profile: stream.Profile,
		Level: positiveMediaInt(stream.Level), BitDepth: positiveMediaInt(stream.BitDepth), PixelFormat: stream.PixelFormat,
		FrameRate: frameRate, ChannelLayout: stream.ChannelLayout, VideoRange: videoRange, ColorSpace: stream.ColorSpace,
		ColorTransfer: stream.ColorTransfer, ColorPrimaries: stream.ColorPrimaries, IsDefault: stream.IsDefault,
		IsForced: stream.IsForced, IsExternal: stream.IsExternal, IsAttachedPicture: stream.IsAttachedPicture,
		IsHearingImpaired: stream.IsHearingImpaired,
	}
}

func appendAdminMediaSubtitles(value *AdminMediaInfo, item Item) error {
	seen := make(map[int]bool, value.StreamCount+len(item.Subtitles)+len(item.BitmapSubtitles))
	if item.Media != nil {
		for _, stream := range item.Media.Streams {
			if seen[stream.Index] {
				return fmt.Errorf("%w: duplicate indexed media stream", ErrUnavailable)
			}
			seen[stream.Index] = true
		}
	}
	additional := make([]media.Stream, 0, len(item.Subtitles)+len(item.BitmapSubtitles))
	for _, subtitle := range item.Subtitles {
		codec := subtitle.Codec
		if codec == "vtt" {
			codec = "webvtt"
		}
		additional = append(additional, media.Stream{Index: subtitle.Index, CodecType: "subtitle", Codec: codec,
			Language: subtitle.Language, Title: subtitle.Title, IsExternal: true, IsDefault: subtitle.IsDefault,
			IsForced: subtitle.IsForced, IsHearingImpaired: subtitle.IsHearingImpaired})
	}
	for _, subtitle := range item.BitmapSubtitles {
		additional = append(additional, media.Stream{Index: subtitle.Index, CodecType: "subtitle", Codec: subtitle.Codec,
			Language: subtitle.Language, Title: subtitle.Title, IsExternal: true, IsDefault: subtitle.IsDefault,
			IsForced: subtitle.IsForced, IsHearingImpaired: subtitle.IsHearingImpaired})
	}
	sort.SliceStable(additional, func(left, right int) bool { return additional[left].Index < additional[right].Index })
	for _, stream := range additional {
		if seen[stream.Index] {
			return fmt.Errorf("%w: duplicate indexed external subtitle stream", ErrUnavailable)
		}
		seen[stream.Index] = true
		value.StreamCount++
		if len(value.Streams) < adminMediaStreamLimit {
			value.Streams = append(value.Streams, projectAdminMediaStream(stream))
		}
	}
	value.StreamsTruncated = value.StreamCount > len(value.Streams)
	return nil
}

func positiveMediaDecimal(value int64) *string {
	if value <= 0 {
		return nil
	}
	result := strconv.FormatInt(value, 10)
	return &result
}

func positiveMediaInt(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
