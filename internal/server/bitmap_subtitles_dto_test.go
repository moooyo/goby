package server

import (
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestExternalBitmapSubtitleDTOAdvertisesBurnWithoutTextDelivery(t *testing.T) {
	item := library.Item{ID: "movie", Path: "/media/movie.mkv", Media: &media.Info{Streams: []media.Stream{
		{Index: 0, CodecType: "video", Codec: "h264"},
		{Index: 1, CodecType: "audio", Codec: "aac"},
	}}, Subtitles: []library.Subtitle{{Index: 70000, Codec: "srt", Language: "eng", IsDefault: true}}}
	originalPlanning := playbackMediaInfo(item)
	item.BitmapSubtitles = []library.BitmapSubtitle{
		{Index: 65536, SourceStreamIndex: 0, Codec: "hdmv_pgs_subtitle", Format: "sup", Language: "zho", Title: "Chinese bitmap", Filename: "movie.zho.sup", IsDefault: true, Tag: strings.Repeat("a", 64)},
		{Index: 65537, SourceStreamIndex: 1, Codec: "dvd_subtitle", Format: "vobsub", Language: "eng", Filename: "movie.idx", Tag: strings.Repeat("b", 64)},
	}
	streams := itemMediaStreamsDTO(item)
	if len(streams) != 5 {
		t.Fatalf("expected all media, text and bitmap tracks, got %d", len(streams))
	}
	for position, index := range []int{0, 1, 65536, 65537, 70000} {
		if streams[position]["Index"] != index {
			t.Fatalf("public stream order changed: %+v", streams)
		}
	}
	for _, stream := range streams[2:4] {
		if stream["IsExternal"] != true || stream["IsTextSubtitleStream"] != false ||
			stream["SupportsExternalStream"] != false || stream["DeliveryMethod"] != "Encode" {
			t.Fatalf("bitmap track did not advertise encoded delivery: %+v", stream)
		}
		for _, field := range []string{"DeliveryUrl", "GobySubtitleTimelineOnly", "Path", "Components", "SourceStreamIndex", "SubtitleSourceStreamIndex"} {
			if _, exists := stream[field]; exists {
				t.Errorf("bitmap track exposed private or unavailable field %s", field)
			}
		}
	}
	if streams[4]["IsTextSubtitleStream"] != true || streams[4]["SupportsExternalStream"] != true {
		t.Fatal("adding a bitmap timeline changed text subtitle delivery")
	}
	planning := playbackMediaInfo(item)
	if len(planning.Streams) != len(originalPlanning.Streams)+2 ||
		!reflect.DeepEqual(planning.Streams[:len(originalPlanning.Streams)], originalPlanning.Streams) || len(item.Media.Streams) != 2 {
		t.Fatal("bitmap planning changed primary probe facts or text subtitle selection")
	}
	for position, source := range item.BitmapSubtitles {
		stream := planning.Streams[len(originalPlanning.Streams)+position]
		if stream.Index != source.Index || stream.Codec != source.Codec || stream.SubtitleTag != source.Tag ||
			stream.SubtitleSourceStreamIndex != source.SourceStreamIndex || !stream.IsExternal || stream.IsTextSubtitleStream {
			t.Fatalf("bitmap planning lost indexed identity or the selected source track: %+v", stream)
		}
	}
	addSubtitleDeliveryCredentials(map[string]any{"MediaStreams": streams}, item.ID, "token", nil)
	for _, stream := range streams[2:4] {
		if _, exists := stream["DeliveryUrl"]; exists {
			t.Fatal("credential projection fabricated a text delivery URL for a bitmap track")
		}
	}
}
