package server

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestExternalBitmapSubtitleDTOSeparatesTimelineAndDeliveryCapabilities(t *testing.T) {
	item := library.Item{ID: "movie", Path: "/media/movie.mkv", Media: &media.Info{Streams: []media.Stream{
		{Index: 0, CodecType: "video", Codec: "h264"},
		{Index: 1, CodecType: "audio", Codec: "aac"},
	}}, Subtitles: []library.Subtitle{{Index: 70000, Codec: "srt", Language: "eng", IsDefault: true}}}
	originalPlanning := playbackMediaInfo(item)
	item.BitmapSubtitles = []library.BitmapSubtitle{
		{Index: 65536, SourceStreamIndex: 0, Codec: "hdmv_pgs_subtitle", Format: "sup", Language: "zho", Title: "Chinese bitmap", Filename: "movie.zho.sup", IsDefault: true},
		{Index: 65537, SourceStreamIndex: 1, Codec: "dvd_subtitle", Format: "vobsub", Language: "eng", Filename: "movie.idx"},
	}
	streams := itemMediaStreamsDTO(item)
	if len(streams) != 5 {
		t.Fatalf("expected all media, text and bitmap timeline tracks, got %d", len(streams))
	}
	for position, index := range []int{0, 1, 65536, 65537, 70000} {
		if streams[position]["Index"] != index {
			t.Fatalf("public stream order changed: %+v", streams)
		}
	}
	for _, stream := range streams[2:4] {
		if stream["IsExternal"] != true || stream["IsTextSubtitleStream"] != false ||
			stream["SupportsExternalStream"] != false || stream["GobySubtitleTimelineOnly"] != true {
			t.Fatalf("bitmap timeline track advertises unavailable delivery: %+v", stream)
		}
		for _, field := range []string{"DeliveryUrl", "DeliveryMethod", "Path", "Components", "SourceStreamIndex"} {
			if _, exists := stream[field]; exists {
				t.Errorf("bitmap timeline exposed private or unavailable field %s", field)
			}
		}
	}
	if streams[4]["IsTextSubtitleStream"] != true || streams[4]["SupportsExternalStream"] != true {
		t.Fatal("adding a bitmap timeline changed text subtitle delivery")
	}
	if !reflect.DeepEqual(originalPlanning, playbackMediaInfo(item)) {
		t.Fatal("timeline-only bitmap sidecars entered default playback negotiation")
	}
}
