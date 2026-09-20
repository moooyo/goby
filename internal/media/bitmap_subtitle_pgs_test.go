package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestPGSFragmentedObjectUsesDisplayInterval(t *testing.T) {
	rle := []byte{1, 2, 0, 0, 2, 1, 0, 0}
	packets := []bitmapSubtitlePacket{
		{PTS: 10, Data: pgsTestJoin(
			pgsTestPresentation(0x80, pgsTestReference(7, 11, 13, false)),
			pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestPalette(),
			pgsTestObject(7, 3, 0x80, 2, 2, len(rle), rle[:3]))},
		{PTS: 10, Data: pgsTestJoin(pgsTestObject(7, 3, 0x40, 0, 0, 0, rle[3:]), pgsTestSegment(0x80, nil))},
		pgsTestClear(40),
	}
	cues, warnings, err := decodePGSSubtitles(context.Background(), packets, pgsTestLimits())
	if err != nil || len(cues) != 1 || len(warnings) != 0 {
		t.Fatalf("decode fragmented display: cues=%d warnings=%v error=%v", len(cues), warnings, err)
	}
	cue := cues[0]
	if cue.StartTicks != 10 || cue.EndTicks != 40 || cue.X != 11 || cue.Y != 13 || cue.Forced || cue.HearingImpaired || cue.ImageSHA256 != "" {
		t.Fatalf("incorrect display metadata: %+v", cue)
	}
	if cue.Image.Bounds() != image.Rect(0, 0, 2, 2) || cue.Image.NRGBAAt(0, 0) != (color.NRGBA{255, 255, 255, 255}) ||
		cue.Image.NRGBAAt(1, 0) != (color.NRGBA{0, 0, 0, 255}) || cue.Image.NRGBAAt(0, 1) != cue.Image.NRGBAAt(1, 0) {
		t.Fatalf("fragmented RLE pixels differ: %v", cue.Image.Pix)
	}
}

func TestPGSPaletteUpdateKeepsObjectsAndEarlierPixels(t *testing.T) {
	first := pgsTestDisplay(10, 1, 1, []byte{1, 0, 0})
	update := bitmapSubtitlePacket{PTS: 20, Data: pgsTestJoin(
		pgsTestPresentation(0, pgsTestReference(1, 10, 20, false)),
		pgsTestSegment(0x14, []byte{0, 1, 1, 16, 128, 128, 255}), pgsTestSegment(0x80, nil))}
	cues, warnings, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{first, update, pgsTestClear(30)}, pgsTestLimits())
	if err != nil || len(cues) != 2 || len(warnings) != 0 {
		t.Fatalf("decode palette update: cues=%d warnings=%v error=%v", len(cues), warnings, err)
	}
	if cues[0].StartTicks != 10 || cues[0].EndTicks != 20 || cues[1].StartTicks != 20 || cues[1].EndTicks != 30 ||
		cues[0].Image.NRGBAAt(0, 0).R != 255 || cues[1].Image.NRGBAAt(0, 0).R != 0 {
		t.Fatalf("palette update altered the earlier image or interval: %+v", cues)
	}
	update.Data[10] = 0x80 // Start an epoch without supplying its palette/object/window.
	if _, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{first, update, pgsTestClear(30)}, pgsTestLimits()); err == nil {
		t.Fatal("a new epoch reused stale object or window definitions")
	}
}

func TestPGSCropAndWindowSelectTheCorrectSourcePixels(t *testing.T) {
	reference := pgsTestReference(1, 10, 20, false)
	reference[3] |= 0x80
	reference = append(reference, 0, 1, 0, 0, 0, 3, 0, 2)
	packet := bitmapSubtitlePacket{PTS: 10, Data: pgsTestJoin(
		pgsTestPresentation(0x80, reference), pgsTestWindows(image.Rect(11, 20, 12, 22)), pgsTestPalette(),
		pgsTestObject(1, 0, 0xc0, 4, 2, 12, []byte{1, 1, 2, 1, 0, 0, 1, 2, 1, 2, 0, 0}), pgsTestSegment(0x80, nil))}
	cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, pgsTestLimits())
	if err != nil || len(cues) != 1 {
		t.Fatalf("decode cropped display: cues=%d error=%v", len(cues), err)
	}
	if cues[0].X != 11 || cues[0].Y != 20 || cues[0].Image.Bounds() != image.Rect(0, 0, 1, 2) ||
		cues[0].Image.NRGBAAt(0, 0).R != 0 || cues[0].Image.NRGBAAt(0, 1).R != 255 {
		t.Fatalf("crop/window combination used the wrong source coordinates: %+v pixels=%v", cues[0], cues[0].Image.Pix)
	}
}

func TestPGSMixedForcedObjectsKeepSeparateFlags(t *testing.T) {
	packet := bitmapSubtitlePacket{PTS: 10, Data: pgsTestJoin(
		pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false), pgsTestReference(1, 30, 40, true)),
		pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestPalette(),
		pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{1, 0, 0}), pgsTestSegment(0x80, nil))}
	cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, pgsTestLimits())
	if err != nil || len(cues) != 2 {
		t.Fatalf("decode mixed forced objects: cues=%d error=%v", len(cues), err)
	}
	if cues[0].Forced || !cues[1].Forced || cues[0].X != 10 || cues[1].X != 30 || cues[0].StartTicks != cues[1].StartTicks || cues[0].EndTicks != cues[1].EndTicks {
		t.Fatalf("forced flags or shared event timing were lost: %+v", cues)
	}
}

func TestPGSOverlappingObjectsCompositeStraightAlpha(t *testing.T) {
	packet := bitmapSubtitlePacket{PTS: 10, Data: pgsTestJoin(
		pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false), pgsTestReference(2, 10, 20, false)),
		pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestPalette(),
		pgsTestSegment(0x14, []byte{0, 1, 3, 235, 128, 128, 128}),
		pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{2, 0, 0}),
		pgsTestObject(2, 0, 0xc0, 1, 1, 3, []byte{3, 0, 0}), pgsTestSegment(0x80, nil))}
	cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, pgsTestLimits())
	if err != nil || len(cues) != 1 {
		t.Fatalf("decode overlapping objects: cues=%d error=%v", len(cues), err)
	}
	if got := cues[0].Image.NRGBAAt(0, 0); got != (color.NRGBA{128, 128, 128, 255}) {
		t.Fatalf("overlapping straight-alpha pixels were not composited: %v", got)
	}
}

func TestPGSPaletteUsesVideoColorMatrixAndPreservesAlpha(t *testing.T) {
	for _, test := range []struct {
		height int
		want   color.NRGBA
	}{
		{192, color.NRGBA{254, 0, 0, 96}},
		{1080, color.NRGBA{255, 24, 0, 96}},
	} {
		presentation := pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false))
		binary.BigEndian.PutUint16(presentation[5:7], uint16(test.height))
		clear := pgsTestClear(20)
		binary.BigEndian.PutUint16(clear.Data[5:7], uint16(test.height))
		packet := bitmapSubtitlePacket{PTS: 10, Data: pgsTestJoin(presentation,
			pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestSegment(0x14, []byte{0, 0, 1, 81, 240, 90, 96}),
			pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{1, 0, 0}), pgsTestSegment(0x80, nil))}
		cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, clear}, pgsTestLimits())
		if err != nil || len(cues) != 1 || cues[0].Image.NRGBAAt(0, 0) != test.want {
			t.Fatalf("wrong palette matrix for video height %d: cues=%v error=%v", test.height, cues, err)
		}
	}
}

func TestPGSFinalIntervalNeedsExplicitTiming(t *testing.T) {
	for _, test := range []struct {
		name      string
		pts       int64
		duration  int64
		sourceEnd int64
		wantEnd   int64
		warning   string
		wantError bool
	}{
		{name: "packet duration", pts: 10, duration: 15, wantEnd: 25, warning: "packet duration"},
		{name: "source duration", pts: 10, sourceEnd: 90, wantEnd: 90, warning: "source duration"},
		{name: "source clips packet duration", pts: 10, duration: 100, sourceEnd: 90, wantEnd: 90, warning: "source duration"},
		{name: "no duration", pts: 10, wantError: true},
		{name: "negative interval ends at zero", pts: -10, duration: 10, wantEnd: 0, warning: "packet duration"},
		{name: "negative interval has no guessed zero end", pts: -10, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := pgsTestDisplay(test.pts, 1, 1, []byte{1, 0, 0})
			packet.Duration = test.duration
			limits := pgsTestLimits()
			limits.DurationTicks = test.sourceEnd
			cues, warnings, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet}, limits)
			if test.wantError {
				if !errors.Is(err, ErrBitmapSubtitle) || len(cues) != 0 {
					t.Fatalf("missing explicit end was accepted: cues=%v error=%v", cues, err)
				}
				return
			}
			if err != nil || len(cues) != 1 || len(warnings) != 1 || !strings.Contains(warnings[0], test.warning) || cues[0].StartTicks != test.pts || cues[0].EndTicks != test.wantEnd {
				t.Fatalf("incorrect explicit final interval: cues=%+v warnings=%v error=%v", cues, warnings, err)
			}
		})
	}
}

func TestPGSNegativePrerollAndSameTimestampReplacement(t *testing.T) {
	packets := []bitmapSubtitlePacket{pgsTestDisplay(-20, 1, 1, []byte{1, 0, 0}), pgsTestClear(-10),
		pgsTestDisplay(10, 1, 1, []byte{1, 0, 0}), pgsTestClear(10),
		pgsTestDisplay(20, 1, 1, []byte{2, 0, 0}), pgsTestClear(30)}
	cues, warnings, err := decodePGSSubtitles(context.Background(), packets, pgsTestLimits())
	if err != nil || len(cues) != 2 || len(warnings) != 0 || cues[0].StartTicks != -20 || cues[0].EndTicks != -10 || cues[1].StartTicks != 20 || cues[1].EndTicks != 30 {
		t.Fatalf("preroll or zero-duration replacement was changed: cues=%+v warnings=%v error=%v", cues, warnings, err)
	}
}

func TestPGSSUPHeaderRetainsNormalizedPacketTiming(t *testing.T) {
	packet := pgsTestDisplay(10, 1, 1, []byte{1, 0, 0})
	var wrapped []byte
	for remaining := packet.Data; len(remaining) > 0; {
		length := 3 + int(binary.BigEndian.Uint16(remaining[1:]))
		wrapped = append(wrapped, 'P', 'G', 0, 1, 95, 144, 0, 1, 95, 144)
		wrapped = append(wrapped, remaining[:length]...)
		remaining = remaining[length:]
	}
	packet.Data = wrapped
	cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, pgsTestLimits())
	if err != nil || len(cues) != 1 || cues[0].StartTicks != 10 || cues[0].EndTicks != 20 {
		t.Fatalf("SUP wrapper overrode the normalized container timing: cues=%+v error=%v", cues, err)
	}
}

func TestPGSRejectsMalformedDisplaySets(t *testing.T) {
	prefix := pgsTestJoin(pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false)),
		pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestPalette())
	end := pgsTestSegment(0x80, nil)
	object := pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{1, 0, 0})
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"empty packet", nil},
		{"truncated header", []byte{0x16, 0}},
		{"truncated payload", []byte{0x16, 0, 11, 0}},
		{"truncated SUP", []byte{'P', 'G', 0}},
		{"unknown segment", pgsTestJoin(prefix, pgsTestSegment(0x91, nil), end)},
		{"definition outside presentation", pgsTestJoin(object, end)},
		{"missing end", pgsTestJoin(prefix, object)},
		{"nonempty end", pgsTestJoin(prefix, object, pgsTestSegment(0x80, []byte{0}))},
		{"duplicate end", pgsTestJoin(prefix, object, end, end)},
		{"nested presentation", pgsTestJoin(prefix, pgsTestPresentation(0), object, end)},
		{"missing object", pgsTestJoin(prefix, end)},
		{"missing window", pgsTestJoin(pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false)), pgsTestPalette(), object, end)},
		{"missing palette", pgsTestJoin(pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false)), pgsTestWindows(image.Rect(0, 0, 320, 192)), object, end)},
		{"truncated palette", pgsTestJoin(prefix, pgsTestSegment(0x14, []byte{0, 1, 2}), object, end)},
		{"undefined color", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{9, 0, 0}), end)},
		{"continuation without start", pgsTestJoin(prefix, pgsTestObject(1, 0, 0x40, 0, 0, 0, []byte{1}), end)},
		{"fragment version mismatch", pgsTestJoin(prefix, pgsTestObject(1, 0, 0x80, 1, 1, 3, []byte{1}), pgsTestObject(1, 1, 0x40, 0, 0, 0, []byte{0, 0}), end)},
		{"incomplete fragment", pgsTestJoin(prefix, pgsTestObject(1, 0, 0x80, 1, 1, 3, []byte{1}), end)},
		{"early last fragment", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{1}), end)},
		{"missing last flag", pgsTestJoin(prefix, pgsTestObject(1, 0, 0x80, 1, 1, 3, []byte{1, 0, 0}), end)},
		{"oversized fragment", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 2, []byte{1, 0, 0}), end)},
		{"short RLE row", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 2, 1, 3, []byte{1, 0, 0}), end)},
		{"long RLE row", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 4, []byte{1, 1, 0, 0}), end)},
		{"trailing RLE", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 4, []byte{1, 0, 0, 1}), end)},
		{"truncated RLE control", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 1, []byte{0}), end)},
		{"truncated long RLE", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 2, []byte{0, 0x40}), end)},
		{"truncated RLE color", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 2, []byte{0, 0x81}), end)},
		{"zero-length color run", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 5, []byte{0, 0x80, 1, 0, 0}), end)},
		{"missing row end", pgsTestJoin(prefix, pgsTestObject(1, 0, 0xc0, 1, 1, 1, []byte{1}), end)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{{PTS: 10, Data: test.data}, pgsTestClear(20)}, pgsTestLimits())
			if !errors.Is(err, ErrBitmapSubtitle) || len(cues) != 0 {
				t.Fatalf("malformed PGS was accepted or leaked partial output: cues=%v error=%v", cues, err)
			}
		})
	}
}

func TestPGSBudgetsAndCancellation(t *testing.T) {
	packets := []bitmapSubtitlePacket{pgsTestDisplay(10, 2, 1, []byte{1, 2, 0, 0}), pgsTestClear(20),
		pgsTestDisplay(30, 2, 1, []byte{1, 2, 0, 0}), pgsTestClear(40)}
	for _, test := range []struct {
		name   string
		change func(*BitmapSubtitleLimits)
	}{
		{"invalid limits", func(limits *BitmapSubtitleLimits) { limits.MaxCues = 0 }},
		{"cue count", func(limits *BitmapSubtitleLimits) { limits.MaxCues = 1 }},
		{"individual pixels", func(limits *BitmapSubtitleLimits) { limits.MaxPixels = 1 }},
		{"total pixels", func(limits *BitmapSubtitleLimits) { limits.MaxTotalPixels = 3 }},
		{"encoded bytes", func(limits *BitmapSubtitleLimits) { limits.MaxPacketBytes = len(packets[0].Data) - 1 }},
		{"source end", func(limits *BitmapSubtitleLimits) { limits.DurationTicks = 5 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := pgsTestLimits()
			test.change(&limits)
			if cues, _, err := decodePGSSubtitles(context.Background(), packets, limits); !errors.Is(err, ErrBitmapSubtitle) || len(cues) != 0 {
				t.Fatalf("decoder budget was ignored: cues=%v error=%v", cues, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := decodePGSSubtitles(ctx, packets, pgsTestLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not preserved: %v", err)
	}
	if cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{pgsTestClear(20)}, pgsTestLimits()); err == nil || len(cues) != 0 {
		t.Fatal("an empty subtitle stream was reported as a successful decode")
	}
}

func TestPGSLongAndTransparentRuns(t *testing.T) {
	rle := []byte{0, 0xc1, 44, 1, 0, 5, 0, 0}
	packet := pgsTestDisplay(10, 305, 1, rle)
	cues, _, err := decodePGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, pgsTestLimits())
	if err != nil || len(cues) != 1 || cues[0].Image.NRGBAAt(299, 0).A != 255 || cues[0].Image.NRGBAAt(300, 0).A != 0 {
		t.Fatalf("long or transparent RLE run was decoded incorrectly: cues=%v error=%v", cues, err)
	}
}

func TestPGSWalkerProcessesMovieLengthWithoutRetainingEveryImage(t *testing.T) {
	const cueCount, width, height = 1000, 32, 8
	rle := bytes.Repeat([]byte{0, 0xa0, 1, 0, 0}, height)
	packets := pgsTestRepeatedDisplays(cueCount, width, height, rle)
	limits := pgsTestLimits()
	limits.MaxCues = cueCount
	limits.MaxPixels = width * height
	// A scaled resident limit fits one object and one displayed image. The
	// complete track contains 500 times that many pixels, as a normal movie
	// exceeds a fixed small resident bitmap allowance with repeated cues.
	limits.MaxTotalPixels = 2 * width * height
	limits.MaxWorkPixels = int64((cueCount + 1) * width * height)
	limits.DurationTicks = int64(cueCount * 10)
	seen, totalPixels := 0, 0
	warnings, err := walkPGSSubtitles(context.Background(), packets, limits, func(cue BitmapSubtitleCue) error {
		if cue.StartTicks != int64(seen*10) || cue.EndTicks != int64((seen+1)*10) || cue.X != 10 || cue.Y != 20 || cue.Forced {
			t.Fatalf("streamed cue %d lost its display metadata: %+v", seen, cue)
		}
		if cue.Image.Bounds() != image.Rect(0, 0, width, height) || cue.Image.NRGBAAt(0, 0).R != 255 || cue.Image.NRGBAAt(width-1, height-1).A != 255 {
			t.Fatalf("streamed cue %d contains altered pixels", seen)
		}
		// No image escapes this callback. Mutating its owned output also
		// proves that the next image is rendered from the cached object,
		// rather than aliasing an earlier callback's bitmap.
		cue.Image.Pix[0] = 99
		seen++
		totalPixels += width * height
		return nil
	})
	if err != nil || len(warnings) != 0 || seen != cueCount || totalPixels != 500*limits.MaxTotalPixels {
		t.Fatalf("long streaming track failed: seen=%d pixels=%d warnings=%v error=%v", seen, totalPixels, warnings, err)
	}
	if cues, _, err := decodePGSSubtitles(context.Background(), packets, limits); err == nil || len(cues) != 0 || !strings.Contains(err.Error(), "batch pixel budget") {
		t.Fatalf("batch adapter did not retain its separate memory limit: cues=%d error=%v", len(cues), err)
	}
}

func TestPGSWalkerStopsImmediatelyOnCallbackFailureOrCancellation(t *testing.T) {
	for _, cancelInCallback := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		packets := pgsTestRepeatedDisplays(2, 1, 1, []byte{1, 0, 0})
		// This damaged later packet must never be visited after the callback
		// has refused the first cue or cancelled its synchronous work.
		packets = append(packets, bitmapSubtitlePacket{PTS: 30, Data: []byte{0x16}})
		callbackError := errors.New("fixture callback refused the cue")
		wantError := callbackError
		if cancelInCallback {
			wantError = context.Canceled
		}
		calls := 0
		_, err := walkPGSSubtitles(ctx, packets, pgsTestLimits(), func(BitmapSubtitleCue) error {
			calls++
			if cancelInCallback {
				cancel()
				return nil
			}
			return callbackError
		})
		cancel()
		if !errors.Is(err, wantError) || calls != 1 {
			t.Fatalf("callback failure did not stop delivery: cancellation=%v calls=%d error=%v", cancelInCallback, calls, err)
		}
	}
}

func TestPGSWalkerCountsDeliveredIntervalsAndLimitsRasterWork(t *testing.T) {
	packets := pgsTestRepeatedDisplays(3, 1, 1, []byte{1, 0, 0})
	for _, test := range []struct {
		name, wantError string
		maxCues         int
		maxWork         int64
		wantCalls       int
	}{
		{name: "cue limit", maxCues: 1, maxWork: 100, wantCalls: 1, wantError: "too many visible"},
		{name: "work limit", maxCues: 3, maxWork: 2, wantCalls: 1, wantError: "work pixel budget"},
	} {
		t.Run(test.name, func(t *testing.T) {
			limits := pgsTestLimits()
			limits.MaxCues, limits.MaxWorkPixels = test.maxCues, test.maxWork
			calls := 0
			_, err := walkPGSSubtitles(context.Background(), packets, limits, func(BitmapSubtitleCue) error { calls++; return nil })
			if err == nil || !strings.Contains(err.Error(), test.wantError) || calls != test.wantCalls {
				t.Fatalf("streaming budget was ignored: calls=%d error=%v", calls, err)
			}
		})
	}
	limits := pgsTestLimits()
	limits.MaxCues = 1
	packets = []bitmapSubtitlePacket{pgsTestDisplay(0, 1, 1, []byte{1, 0, 0}), pgsTestClear(0),
		pgsTestDisplay(10, 1, 1, []byte{2, 0, 0}), pgsTestClear(20)}
	calls := 0
	_, err := walkPGSSubtitles(context.Background(), packets, limits, func(cue BitmapSubtitleCue) error {
		calls++
		if cue.StartTicks != 10 || cue.EndTicks != 20 {
			t.Fatalf("an unobservable replacement consumed the cue limit: %+v", cue)
		}
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("same-timestamp replacement counted as a delivered cue: calls=%d error=%v", calls, err)
	}
	limits.MaxWorkPixels = 3
	packets = pgsTestRepeatedDisplays(3, 1, 1, []byte{0, 1, 0, 0})
	_, err = walkPGSSubtitles(context.Background(), packets, limits, func(BitmapSubtitleCue) error {
		t.Fatal("transparent display was delivered")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "work pixel budget") {
		t.Fatalf("transparent display bypassed the raster work budget: %v", err)
	}
}

func TestPGSWalkerBudgetsCacheAndBothPendingForcedGroupsTogether(t *testing.T) {
	packet := bitmapSubtitlePacket{PTS: 10, Data: pgsTestJoin(
		pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false), pgsTestReference(1, 30, 40, true)),
		pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestPalette(),
		pgsTestObject(1, 0, 0xc0, 1, 1, 3, []byte{1, 0, 0}), pgsTestSegment(0x80, nil))}
	limits := pgsTestLimits()
	limits.MaxWorkPixels = 100
	limits.MaxTotalPixels = 2
	_, err := walkPGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, limits, func(BitmapSubtitleCue) error {
		t.Fatal("over-budget pending display was delivered")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "pixel budget") {
		t.Fatalf("separate forced groups bypassed the resident budget: %v", err)
	}
	limits.MaxTotalPixels = 3
	calls := 0
	_, err = walkPGSSubtitles(context.Background(), []bitmapSubtitlePacket{packet, pgsTestClear(20)}, limits, func(cue BitmapSubtitleCue) error {
		if cue.Forced != (calls == 1) || cue.StartTicks != 10 || cue.EndTicks != 20 {
			t.Fatalf("forced groups lost callback ordering or timing: %+v", cue)
		}
		calls++
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("bounded mixed display failed: calls=%d error=%v", calls, err)
	}
}

func pgsTestRepeatedDisplays(count, width, height int, rle []byte) []bitmapSubtitlePacket {
	packets := []bitmapSubtitlePacket{pgsTestDisplay(0, width, height, rle)}
	for index := 1; index < count; index++ {
		packets = append(packets, bitmapSubtitlePacket{PTS: int64(index * 10), Data: pgsTestJoin(
			pgsTestPresentation(0, pgsTestReference(1, 10, 20, false)), pgsTestSegment(0x80, nil))})
	}
	return append(packets, pgsTestClear(int64(count*10)))
}

func pgsTestLimits() BitmapSubtitleLimits {
	return BitmapSubtitleLimits{MaxCues: 20, MaxPixels: 320 * 192, MaxTotalPixels: 320 * 192 * 4, MaxPacketBytes: 1 << 20, DurationTicks: 100}
}

func pgsTestDisplay(pts int64, width, height int, rle []byte) bitmapSubtitlePacket {
	return bitmapSubtitlePacket{PTS: pts, Data: pgsTestJoin(
		pgsTestPresentation(0x80, pgsTestReference(1, 10, 20, false)),
		pgsTestWindows(image.Rect(0, 0, 320, 192)), pgsTestPalette(),
		pgsTestObject(1, 0, 0xc0, width, height, len(rle), rle), pgsTestSegment(0x80, nil))}
}

func pgsTestClear(pts int64) bitmapSubtitlePacket {
	return bitmapSubtitlePacket{PTS: pts, Data: pgsTestJoin(pgsTestPresentation(0), pgsTestSegment(0x80, nil))}
}

func pgsTestSegment(kind byte, payload []byte) []byte {
	output := []byte{kind, byte(len(payload) >> 8), byte(len(payload))}
	return append(output, payload...)
}

func pgsTestJoin(segments ...[]byte) []byte {
	return bytes.Join(segments, nil)
}

func pgsTestPresentation(state byte, references ...[]byte) []byte {
	payload := []byte{1, 64, 0, 192, 0x20, 0, 1, state, 0, 0, byte(len(references))}
	for _, reference := range references {
		payload = append(payload, reference...)
	}
	return pgsTestSegment(0x16, payload)
}

func pgsTestReference(id uint16, x, y int, forced bool) []byte {
	flags := byte(0)
	if forced {
		flags = 0x40
	}
	return []byte{byte(id >> 8), byte(id), 0, flags, byte(x >> 8), byte(x), byte(y >> 8), byte(y)}
}

func pgsTestWindows(window image.Rectangle) []byte {
	return pgsTestSegment(0x17, []byte{1, 0, byte(window.Min.X >> 8), byte(window.Min.X), byte(window.Min.Y >> 8), byte(window.Min.Y),
		byte(window.Dx() >> 8), byte(window.Dx()), byte(window.Dy() >> 8), byte(window.Dy())})
}

func pgsTestPalette() []byte {
	return pgsTestSegment(0x14, []byte{0, 0, 0, 16, 128, 128, 0, 1, 235, 128, 128, 255, 2, 16, 128, 128, 255})
}

func pgsTestObject(id uint16, version, flags byte, width, height, size int, data []byte) []byte {
	payload := []byte{byte(id >> 8), byte(id), version, flags}
	if flags&0x80 != 0 {
		length := size + 4
		payload = append(payload, byte(length>>16), byte(length>>8), byte(length), byte(width>>8), byte(width), byte(height>>8), byte(height))
	}
	return pgsTestSegment(0x15, append(payload, data...))
}
