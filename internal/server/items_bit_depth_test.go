package server

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func TestProbedModernVideoPrecisionReachesDTOAndCopiedHLSWithoutMutatingFacts(t *testing.T) {
	for _, codec := range []string{"hevc", "av1"} {
		for _, format := range []struct {
			name  string
			depth int
		}{{"yuv420p", 8}, {"nv12", 8}, {"yuv420p10le", 10}, {"yuv420p10be", 10}, {"p010le", 10}, {"p010be", 10}} {
			t.Run(codec+"/"+format.name, func(t *testing.T) {
				streams := []media.Stream{{Index: 2, CodecType: "video", Codec: codec, PixelFormat: format.name}}
				before := append([]media.Stream(nil), streams...)
				dto := mediaStreamsDTO(streams)[0]
				if dto["BitDepth"] != format.depth || dto["PixelFormat"] != format.name {
					t.Fatal("the DTO omitted precision established by a modern video pixel format")
				}
				session := &hlsSession{key: hlsKey{plan: transcode.Plan{VideoCodec: "copy", VideoCopyCodec: codec}},
					output: playback.Source{Info: media.Info{Streams: streams}}}
				if query := hlsVideoEncodingQuery(session); query.Get("VideoCodec") != codec || query.Get("VideoBitDepth") != strconv.Itoa(format.depth) {
					t.Fatal("copied HLS signaling disagreed with the declared video precision")
				}
				if !reflect.DeepEqual(before, streams) || streams[0].BitDepth != 0 {
					t.Fatal("DTO or HLS projection overwrote the cached probe's unknown bit-depth field")
				}
			})
		}
	}
}

func TestMediaStreamPrecisionProjectionPreservesDeclaredAndUnknownFacts(t *testing.T) {
	for _, test := range []struct {
		name, kind, codec, pixel string
		declared, want           int
		query                    string
	}{
		{"conflicting reported depth remains visible", "video", "hevc", "p010le", 8, 8, ""},
		{"consistent reported depth remains visible", "video", "av1", "p010le", 10, 10, "10"},
		{"reported unsupported depth remains a fact", "video", "av1", "yuv420p", 12, 12, ""},
		{"negative depth does not infer", "video", "hevc", "p010le", -1, 0, ""},
		{"unknown pixel format stays unknown", "video", "av1", "unknown", 0, 0, ""},
		{"unlisted precision stays unknown", "video", "hevc", "yuv420p12le", 0, 0, ""},
		{"H264 unknown precision stays unknown", "video", "h264", "yuv420p", 0, 0, ""},
		{"audio retains declared precision", "audio", "flac", "", 24, 24, ""},
		{"audio does not infer video precision", "audio", "hevc", "p010le", 0, 0, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := media.Stream{Index: 2, CodecType: test.kind, Codec: test.codec, PixelFormat: test.pixel, BitDepth: test.declared}
			dto := mediaStreamsDTO([]media.Stream{stream})[0]
			value, present := dto["BitDepth"]
			if test.want == 0 && present || test.want > 0 && (!present || value != test.want) {
				t.Fatal("the DTO changed declared precision or invented unknown precision")
			}
			session := &hlsSession{key: hlsKey{plan: transcode.Plan{VideoCodec: "copy"}},
				output: playback.Source{Info: media.Info{Streams: []media.Stream{stream}}}}
			if hlsVideoEncodingQuery(session).Get("VideoBitDepth") != test.query {
				t.Fatal("copied HLS signaling replaced a declared fact or guessed unsupported precision")
			}
		})
	}
}
