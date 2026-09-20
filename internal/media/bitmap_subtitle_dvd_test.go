package media

import (
	"context"
	"encoding/binary"
	"errors"
	"image/color"
	"strings"
	"testing"
)

func dvdTestPalette() []byte {
	return []byte("size: 720x480\npalette: 000000, ffffff, ff0000, 0000ff, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000\n")
}

// The two field payloads encode [1,1,2,2] and [1,2,1,2]. These
// hand-authored nibble codes exercise both field offsets and byte alignment.
func dvdTestSPU(forced bool) []byte {
	data := []byte{0, 37, 0, 7, 0x9a, 0x56, 0x56, 0, 0, 0, 31}
	start := byte(1)
	if forced {
		start = 0
	}
	data = append(data, start, 3, 0x32, 0x10, 4, 0xff, 0xf0,
		5, 0, 0xa0, 13, 1, 0x40, 21, 6, 0, 4, 0, 5, 0xff,
		0, 90, 0, 31, 2, 0xff)
	return data
}

func TestDVDSubtitleDecodesControlClockAndInterlacedPixels(t *testing.T) {
	cues, warnings, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{PTS: TicksPerSecond, Data: dvdTestSPU(true)}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || len(warnings) != 0 {
		t.Fatalf("cues=%d warnings=%v", len(cues), warnings)
	}
	cue := cues[0]
	if cue.StartTicks != TicksPerSecond || cue.EndTicks != TicksPerSecond+10_240_000 || !cue.Forced || cue.X != 10 || cue.Y != 20 || cue.Image.Bounds().Dx() != 4 || cue.Image.Bounds().Dy() != 2 {
		t.Fatalf("unexpected timed bitmap: %+v", cue)
	}
	white, red := color.NRGBA{255, 255, 255, 255}, color.NRGBA{255, 0, 0, 255}
	expected := [][]color.NRGBA{{white, white, red, red}, {white, red, white, red}}
	for y, row := range expected {
		for x, value := range row {
			if got := cue.Image.NRGBAAt(x, y); got != value {
				t.Fatalf("pixel(%d,%d)=%v want=%v", x, y, got, value)
			}
		}
	}
}

func TestDVDSubtitlePreservesOverlappingPacketsAndFragmentedSPU(t *testing.T) {
	packet := dvdTestSPU(false)
	cues, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{
		{PTS: TicksPerSecond, Data: packet[:8]},
		{PTS: TicksPerSecond, Data: packet[8:]},
		{PTS: TicksPerSecond + 5_000_000, Data: packet},
	}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 2 || cues[0].EndTicks <= cues[1].StartTicks || cues[1].StartTicks != 15_000_000 || cues[0].Forced {
		t.Fatalf("overlap or fragment origin lost: %+v", cues)
	}
}

func TestDVDSubtitleAlphaChangeUsesARealCommandEvent(t *testing.T) {
	data := dvdTestSPU(false)
	data = data[:31]
	data = append(data, 0, 45, 0, 39, 4, 0, 0, 0xff, 0, 90, 0, 39, 2, 0xff)
	binary.BigEndian.PutUint16(data[:2], uint16(len(data)))
	cues, warnings, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{PTS: TicksPerSecond, Data: data}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(cues) != 1 || cues[0].EndTicks != 15_120_000 || !subtitleWarningPresent(warnings, "transparent_dvd_display_ignored") {
		t.Fatalf("alpha event not respected: %+v %v", cues, warnings)
	}
}

func TestDVDSubtitleMissingEndNeverInventsDuration(t *testing.T) {
	data := dvdTestSPU(false)[:31]
	binary.BigEndian.PutUint16(data[:2], uint16(len(data)))
	binary.BigEndian.PutUint16(data[9:11], 7)
	if _, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: data}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond)); err == nil {
		t.Fatal("source duration silently substituted for a missing DVD stop")
	}
	cues, warnings, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{PTS: TicksPerSecond, Duration: 2 * TicksPerSecond, Data: data}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond))
	if err != nil || len(cues) != 1 || cues[0].EndTicks != 3*TicksPerSecond || !subtitleWarningPresent(warnings, "dvd_display_closed_at_packet_duration") {
		t.Fatalf("explicit packet duration fallback: %+v %v %v", cues, warnings, err)
	}
}

func TestDVDSubtitleMalformedInputsFailWithoutPartialSuccess(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{"truncated", func(data []byte) []byte { return data[:len(data)-1] }},
		{"run_overflow", func(data []byte) []byte { data[4] = 0xda; return data }},
		{"bad_chain", func(data []byte) []byte { data[9], data[10] = 0, 6; return data }},
		{"unknown_command", func(data []byte) []byte { data[11] = 0x7f; return data }},
		{"bad_pixel_offset", func(data []byte) []byte { data[27] = 7; return data }},
		{"hd_dvd", func(data []byte) []byte { data[0], data[1] = 0, 0; return data }},
		{"no_opaque_pixels", func(data []byte) []byte { data[16], data[17] = 0, 0; return data }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: test.edit(dvdTestSPU(false))}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond))
			if err == nil {
				t.Fatal("malformed track succeeded")
			}
		})
	}
}

func TestDVDSubtitleLimitsApplyBeforeRetainingEveryDisplay(t *testing.T) {
	limits := bitmapSubtitleLimits(10 * TicksPerSecond)
	limits.MaxTotalPixels = 8
	_, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: dvdTestSPU(false)}, {PTS: 2 * TicksPerSecond, Data: dvdTestSPU(false)}}, dvdTestPalette(), limits)
	if err == nil || !strings.Contains(err.Error(), "pixel") {
		t.Fatalf("expected total pixel rejection: %v", err)
	}
	limits = bitmapSubtitleLimits(10 * TicksPerSecond)
	limits.MaxPixels = 7
	if _, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: dvdTestSPU(false)}}, dvdTestPalette(), limits); err == nil {
		t.Fatal("per-image limit bypassed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := decodeDVDSubtitles(ctx, []bitmapSubtitlePacket{{Data: dvdTestSPU(false)}}, dvdTestPalette(), bitmapSubtitleLimits(10*TicksPerSecond)); err == nil {
		t.Fatal("cancelled decode succeeded")
	}
}

func TestDVDSubtitleSameTimestampRastersStillConsumeWorkBudget(t *testing.T) {
	data := dvdTestSPU(false)[:31]
	data = append(data, 0, 0, 0, 36, 0xff, 0, 0, 0, 41, 0xff, 0, 90, 0, 41, 2, 0xff)
	binary.BigEndian.PutUint16(data[:2], uint16(len(data)))
	limits := bitmapSubtitleLimits(10 * TicksPerSecond)
	limits.MaxTotalPixels = 8
	_, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: data}}, dvdTestPalette(), limits)
	if err == nil || !strings.Contains(err.Error(), "work pixel limit") {
		t.Fatalf("same-time display replacements bypassed raster work budget: %v", err)
	}
}

func TestDVDSubtitleWalkConsumesCuesWithoutRetainingTrackRasters(t *testing.T) {
	const count = 1000
	packets := make([]bitmapSubtitlePacket, count)
	for index := range packets {
		packets[index] = bitmapSubtitlePacket{PTS: int64(index) * 2 * TicksPerSecond, Data: dvdTestSPU(false)}
	}
	limits := bitmapSubtitleLimits(3000 * TicksPerSecond)
	limits.MaxTotalPixels = 8
	limits.MaxWorkPixels = count * 8
	emitted := 0
	_, err := walkDVDSubtitles(context.Background(), packets, dvdTestPalette(), limits, func(cue BitmapSubtitleCue) error {
		if cue.StartTicks != int64(emitted)*2*TicksPerSecond || cue.Image.NRGBAAt(0, 0) != (color.NRGBA{255, 255, 255, 255}) {
			return errors.New("streamed cue differs from original")
		}
		emitted++
		return nil
	})
	if err != nil || emitted != count {
		t.Fatalf("one-raster working set could not process a full track: %d %v", emitted, err)
	}
	stopped := errors.New("review cancelled")
	emitted = 0
	_, err = walkDVDSubtitles(context.Background(), packets, dvdTestPalette(), limits, func(BitmapSubtitleCue) error { emitted++; return stopped })
	if !errors.Is(err, stopped) || emitted != 1 {
		t.Fatalf("consumer failure did not stop decoding: %d %v", emitted, err)
	}
}

func TestDVDSubtitlePaletteFallbackIsExplicitAndInvalidPaletteFails(t *testing.T) {
	_, warnings, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: dvdTestSPU(false)}}, nil, bitmapSubtitleLimits(10*TicksPerSecond))
	if err != nil || !subtitleWarningPresent(warnings, "dvd_palette_missing_monochrome_review") {
		t.Fatalf("%v %v", warnings, err)
	}
	if _, _, err := decodeDVDSubtitles(context.Background(), []bitmapSubtitlePacket{{Data: dvdTestSPU(false)}}, []byte("palette: ffffff"), bitmapSubtitleLimits(10*TicksPerSecond)); err == nil {
		t.Fatal("malformed palette accepted")
	}
}
