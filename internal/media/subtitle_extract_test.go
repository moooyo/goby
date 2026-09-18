package media

import "testing"

func TestSubtitleExtractionCodecBoundary(t *testing.T) {
	for codec, want := range map[string]string{"ass": "ass", "ssa": "ass", "subrip": "srt", "mov_text": "srt", "webvtt": "vtt", "hdmv_pgs_subtitle": "", "dvb_subtitle": "", "unknown": ""} {
		if got := SubtitleExtractFormat(codec); got != want {
			t.Errorf("codec %q maps to %q, want %q", codec, got, want)
		}
	}
}

func TestFontAttachmentRequiresIndexedFontMetadata(t *testing.T) {
	for _, test := range []struct {
		stream Stream
		want   bool
	}{
		{Stream{Index: 3, CodecType: "attachment", Codec: "ttf"}, true},
		{Stream{Index: 4, CodecType: "attachment", MIMEType: "font/otf"}, true},
		{Stream{Index: 5, CodecType: "video", Codec: "ttf"}, false},
		{Stream{Index: -1, CodecType: "attachment", Codec: "ttf"}, false},
		{Stream{Index: 4096, CodecType: "attachment", Codec: "ttf"}, false},
		{Stream{Index: 6, CodecType: "attachment", MIMEType: "image/jpeg", Filename: "font.ttf"}, false},
	} {
		if got := FontAttachment(test.stream); got != test.want {
			t.Errorf("attachment %#v accepted=%t, want %t", test.stream, got, test.want)
		}
	}
}
