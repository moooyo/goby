package library

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestAdminMediaInfoPreservesUnknownAndLargeIndexedValues(t *testing.T) {
	value := AdminMediaInfo{UpdatedAt: time.Date(2026, 11, 6, 12, 0, 0, 0, time.FixedZone("fixture", 8*60*60))}
	if err := projectAdminMediaInfo(&value, nil, false, 0, true); err != nil {
		t.Fatal(err)
	}
	if value.Summary.State != "not_scanned" || value.Summary.Size != nil || value.Summary.DurationTicks != nil ||
		value.Summary.Bitrate != nil || value.ProbeVersion != nil || value.Summary.Video != nil || value.Summary.Audio != nil ||
		value.Streams == nil || value.Chapters == nil || value.Summary.Images.Types == nil || value.UpdatedAt.Location() != time.UTC {
		t.Fatalf("unknown catalog facts acquired values or null collections: %#v", value)
	}
	info := media.Info{ProbeVersion: media.CurrentProbeVersion, DurationTicks: math.MaxInt64, Size: math.MaxInt64, Bitrate: 36000000, Container: "matroska"}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := projectAdminMediaInfo(&value, raw, false, 100, true); err != nil {
		t.Fatal(err)
	}
	if value.Summary.State != "available" || value.Summary.Size == nil || *value.Summary.Size != "9223372036854775807" ||
		value.Summary.DurationTicks == nil || *value.Summary.DurationTicks != "9223372036854775807" {
		t.Fatalf("int64 facts lost exact decimal representation: %#v", value)
	}
	folder := AdminMediaInfo{}
	if err := projectAdminMediaInfo(&folder, raw, true, 100, true); err != nil || folder.Summary.State != "not_applicable" || folder.Summary.Size != nil {
		t.Fatalf("organizational folder acquired a media file: %#v, %v", folder, err)
	}
}

func TestAdminMediaInfoSelectsRealTracksAndOmitsPrivateEvidence(t *testing.T) {
	info := media.Info{ProbeVersion: media.CurrentProbeVersion, DurationTicks: 1500000000, Container: "matroska", Streams: []media.Stream{
		{Index: 5, CodecType: "video", Codec: "mjpeg", IsAttachedPicture: true, Filename: "/private/cover.jpg", SubtitleTag: "private-token"},
		{Index: 9, CodecType: "audio", Codec: "aac", Channels: 2},
		{Index: 12, CodecType: "video", Codec: "hevc", Width: 3840, Height: 2160, AverageFrameRate: "24000/1001"},
		{Index: 14, CodecType: "audio", Codec: "truehd", Channels: 8, IsDefault: true, Bitrate: 7000000},
		{Index: 17, CodecType: "subtitle", Codec: "ass", Language: "eng", IsForced: true},
	}, Chapters: []media.Chapter{{StartTicks: 0, EndTicks: 10000000, Title: "Opening"}, {StartTicks: 10000000, Title: "Chapter two"}}}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	value := AdminMediaInfo{}
	if err := projectAdminMediaInfo(&value, raw, false, 0, true); err != nil {
		t.Fatal(err)
	}
	if value.Summary.Video == nil || value.Summary.Video.Index != 12 || value.Summary.Audio == nil || value.Summary.Audio.Index != 14 ||
		len(value.Streams) != 5 || value.Streams[4].Index != 17 || value.Streams[4].Type != "subtitle" ||
		len(value.Chapters) != 2 || value.Chapters[0].StartTicks != "0" || value.Chapters[1].EndTicks != nil {
		t.Fatalf("technical projection lost track identity or complete chapters: %#v", value)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"/private/cover.jpg", "private-token", "Filename", "SubtitleTag", "VideoSeekIndexes", "AudioTiming", "FileChangeTimeNs"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("administrator display DTO exposed private evidence %q", private)
		}
	}
}

func TestAdminMediaInfoReportsDisplayBoundsAndInvalidFacts(t *testing.T) {
	info := media.Info{DurationTicks: 20000000, Streams: make([]media.Stream, adminMediaStreamLimit+1), Chapters: make([]media.Chapter, adminMediaChapterLimit+1)}
	for index := range info.Streams {
		info.Streams[index] = media.Stream{Index: index, CodecType: "audio", Codec: "aac"}
	}
	for index := range info.Chapters {
		info.Chapters[index] = media.Chapter{StartTicks: int64(index)}
	}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	value := AdminMediaInfo{}
	if err := projectAdminMediaInfo(&value, raw, false, 0, true); err != nil {
		t.Fatal(err)
	}
	if len(value.Streams) != adminMediaStreamLimit || len(value.Chapters) != adminMediaChapterLimit ||
		!value.StreamsTruncated || !value.ChaptersTruncated || value.StreamCount != len(info.Streams) || value.ChapterCount != len(info.Chapters) {
		t.Fatal("display limits silently changed the indexed totals")
	}
	if err := projectAdminMediaInfo(&AdminMediaInfo{}, []byte(`{"DurationTicks":"invalid"}`), false, 0, true); err == nil {
		t.Fatal("invalid indexed media was treated as available")
	}
}

func TestAdminMediaInfoIncludesIndexedExternalTextAndBitmapTracks(t *testing.T) {
	info := media.Info{DurationTicks: 1500000000, Streams: []media.Stream{
		{Index: 7, CodecType: "video", Codec: "hevc", VideoRange: "HDR10", VideoRangeKnown: false},
		{Index: 11, CodecType: "audio", Codec: "aac", IsHearingImpaired: true},
	}}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	value := AdminMediaInfo{}
	if err := projectAdminMediaInfo(&value, raw, false, 0, true); err != nil {
		t.Fatal(err)
	}
	item := Item{Media: &info, Subtitles: []Subtitle{{Index: 100001, Codec: "vtt", Language: "eng", Title: "English SDH",
		IsHearingImpaired: true, IsDefault: true, Filename: "private-caption.vtt", Tag: "private-caption-token"}},
		BitmapSubtitles: []BitmapSubtitle{{Index: 100000, Codec: "dvd_subtitle", Language: "zho", Title: "Forced Chinese",
			IsForced: true, Filename: "private-bitmap.idx", Tag: "private-bitmap-token", SourceStreamIndex: 1}}}
	if err := appendAdminMediaSubtitles(&value, item); err != nil {
		t.Fatal(err)
	}
	if value.StreamCount != 4 || len(value.Streams) != 4 || value.StreamsTruncated ||
		value.Streams[2].Index != 100000 || value.Streams[2].Codec != "dvd_subtitle" || !value.Streams[2].IsExternal || !value.Streams[2].IsForced ||
		value.Streams[3].Index != 100001 || value.Streams[3].Codec != "webvtt" || !value.Streams[3].IsHearingImpaired || !value.Streams[3].IsDefault ||
		value.Streams[0].VideoRange != "" || !value.Streams[1].IsHearingImpaired {
		t.Fatalf("external namespace, flags or unknown video range changed: %#v", value)
	}
	known := projectAdminMediaStream(media.Stream{VideoRange: "HDR10", VideoRangeKnown: true})
	if known.VideoRange != "HDR10" {
		t.Fatal("known video range was discarded")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-caption", "private-bitmap", "SourceStreamIndex", "Filename", "Tag", "Components"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("external track projection exposed private identity %q", private)
		}
	}
	collision := AdminMediaInfo{StreamCount: len(info.Streams), Streams: []AdminMediaStream{}}
	item.Subtitles[0].Index = 7
	if err := appendAdminMediaSubtitles(&collision, item); err == nil {
		t.Fatal("a duplicate public stream index was projected")
	}
}
