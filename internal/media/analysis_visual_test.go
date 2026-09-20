package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strings"
	"testing"
)

func visualAnalysisTestInfo() (Info, Stream) {
	stream := Stream{Index: 2, CodecType: "video", Codec: "h264", Width: 1920, Height: 1080, TimeBase: "1/1000"}
	return Info{FormatStartKnown: true, FormatStartTicks: TicksPerSecond, DurationTicks: 15 * TicksPerSecond / 10, Streams: []Stream{stream}}, stream
}

func visualAnalysisTestLog(t *testing.T) *analysisVisualLog {
	t.Helper()
	info, stream := visualAnalysisTestInfo()
	limits := DefaultAnalysisLimits()
	plan, err := analysisVisualOptions(info, VisualAnalysisOptions{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	log, err := newAnalysisVisualLog(info, stream, plan, limits)
	if err != nil {
		t.Fatal(err)
	}
	return log
}

func visualAnalysisPacket(index int, pts string) string {
	return fmt.Sprintf("[vist#0:%d/h264 @ 0x1] [info] demuxer -> ist_index:0:%d type:video pkt_pts:%s pkt_pts_time:0 pkt_dts:0 pkt_dts_time:0 duration:40 duration_time:0.04\n", index, index, pts)
}

func visualAnalysisConfig(name, base string) string {
	if name == "sample" {
		return ""
	}
	return fmt.Sprintf("[showinfo@analysis_%s @ 0x2] [info] config in time_base: %s, frame_rate: 0/0\n", name, base)
}

func visualAnalysisFrame(name string, number int, pts string, width, height int, format string) string {
	if name == "sample" {
		return visualAnalysisOutput(pts, "1/1000")
	}
	return fmt.Sprintf("[showinfo@analysis_%s @ 0x2] [info] n:%4d pts:%7s pts_time:999.999 duration:40 duration_time:0.04 fmt:%s cl:unspecified sar:1/1 s:%dx%d i:P iskey:0 type:P \n", name, number, pts, format, width, height)
}

func visualAnalysisOutput(pts, base string) string {
	return fmt.Sprintf("[vf#0:0 @ 0x3] [info] filter_raw -> pts:%s pts_time:999.999 time_base:%s\n", pts, base)
}

func visualAnalysisTestTranscript() string {
	// Packet order differs from presentation order. The source filter can run
	// ahead of the output filter, and pts_time is intentionally untrustworthy.
	return visualAnalysisConfig("source", "1/1000") + visualAnalysisConfig("sample", "1/1000") +
		visualAnalysisPacket(2, "1000") + visualAnalysisPacket(2, "2020") + visualAnalysisPacket(2, "1300") + visualAnalysisPacket(2, "1650") +
		visualAnalysisFrame("source", 0, "1000", 1920, 1080, "yuv420p") +
		visualAnalysisFrame("source", 1, "1300", 1920, 1080, "yuv420p") +
		visualAnalysisFrame("source", 2, "1650", 1920, 1080, "yuv420p") +
		visualAnalysisFrame("source", 3, "2020", 1920, 1080, "yuv420p") +
		visualAnalysisFrame("sample", 0, "1000", 32, 32, "gray") +
		visualAnalysisFrame("sample", 1, "1650", 32, 32, "gray") +
		visualAnalysisFrame("sample", 2, "2020", 32, 32, "gray")
}

func TestAnalysisVisualPreservesRealVFRPTSAndOrigin(t *testing.T) {
	log := visualAnalysisTestLog(t)
	transcript := visualAnalysisTestTranscript()
	for start := 0; start < len(transcript); start += 7 {
		if _, err := log.Write([]byte(transcript[start:min(start+7, len(transcript))])); err != nil {
			t.Fatal(err)
		}
	}
	log.Close(nil)
	var actual, nominal []int64
	err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, 3*32*32)), log, func(frame analysisVisualFrame, gray []byte) error {
		if len(gray) != 32*32 {
			t.Fatalf("unexpected raster length %d", len(gray))
		}
		actual, nominal = append(actual, frame.actual), append(nominal, frame.nominal)
		return nil
	})
	if err != nil || log.result() != nil {
		t.Fatalf("parse returned %v; metadata returned %v", err, log.result())
	}
	if fmt.Sprint(actual) != "[0 6500000 10200000]" || fmt.Sprint(nominal) != "[0 5000000 10000000]" {
		t.Fatalf("actual %v nominal %v", actual, nominal)
	}
}

func TestAnalysisVisualRequiresExactSourceToOutputPTSEquality(t *testing.T) {
	for _, subTickOffset := range []int64{0, 1} {
		log := visualAnalysisTestLog(t)
		text := visualAnalysisConfig("source", "1/1000") +
			visualAnalysisPacket(2, "1000") + visualAnalysisFrame("source", 0, "1000", 1920, 1080, "yuv420p") +
			visualAnalysisOutput(fmt.Sprint(1_000_000_000+subTickOffset), "1/1000000000")
		_, err := log.Write([]byte(text))
		if subTickOffset == 0 && err != nil {
			t.Fatalf("equivalent rational timestamp was rejected: %v", err)
		}
		if subTickOffset != 0 && err == nil {
			t.Fatal("different rational timestamps were joined after tick truncation")
		}
	}
}

func TestAnalysisVisualRejectsUnprovenAndMalformedEvidence(t *testing.T) {
	valid := visualAnalysisTestTranscript()
	for _, fixture := range []struct{ name, text string }{
		{"missing_original_pts", strings.Replace(valid, "pkt_pts:1000 ", "pkt_pts:NOPTS ", 1)},
		{"fabricated_frame_pts", strings.Replace(valid, visualAnalysisPacket(2, "1000"), visualAnalysisPacket(2, "999"), 1)},
		{"missing_frame_pts", strings.Replace(valid, "pts:   1000 ", "pts:  NOPTS ", 1)},
		{"duplicate_frame", strings.Replace(valid, visualAnalysisFrame("source", 1, "1300", 1920, 1080, "yuv420p"), visualAnalysisFrame("source", 1, "1000", 1920, 1080, "yuv420p"), 1)},
		{"backward_frame", strings.Replace(valid, visualAnalysisFrame("source", 1, "1300", 1920, 1080, "yuv420p"), visualAnalysisFrame("source", 1, "900", 1920, 1080, "yuv420p"), 1)},
		{"ordinal_gap", strings.Replace(valid, "n:   1 ", "n:   7 ", 1)},
		{"reconfigured", visualAnalysisConfig("source", "1/1000") + valid},
		{"wrong_sample_time", strings.Replace(valid, visualAnalysisFrame("sample", 1, "1650", 32, 32, "gray"), visualAnalysisFrame("sample", 1, "1700", 32, 32, "gray"), 1)},
		{"changed_source_size", strings.Replace(valid, "s:1920x1080", "s:1280x720", 1)},
		{"non_square_pixels", strings.Replace(valid, "sar:1/1", "sar:2/1", 1)},
		{"unknown_pixel_ratio", strings.Replace(valid, "sar:1/1", "sar:0/1", 1)},
		{"missing_output_pts", strings.Replace(valid, "filter_raw -> pts:1650", "filter_raw -> pts:NOPTS", 1)},
		{"changed_output_time_base", strings.Replace(valid, visualAnalysisOutput("1650", "1/1000"), visualAnalysisOutput("3300", "1/2000"), 1)},
		{"incomplete_metadata", strings.TrimSuffix(valid, visualAnalysisFrame("sample", 2, "2020", 32, 32, "gray"))},
		{"unterminated_line", strings.TrimSuffix(valid, "\n")},
		{"decoder_error", valid + "[h264 @ 0x1] [error] invalid reference picture\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			log := visualAnalysisTestLog(t)
			_, err := log.Write([]byte(fixture.text))
			log.Close(nil)
			if err == nil {
				_, err = log.next(context.Background())
				if err == nil {
					log.mu.Lock()
					err = log.err
					log.mu.Unlock()
				}
			}
			if err == nil {
				t.Fatal("unproven metadata was accepted")
			}
		})
	}
}

func TestAnalysisVisualAcceptsInterleavedAtomicLogBodies(t *testing.T) {
	log := visualAnalysisTestLog(t)
	packet := visualAnalysisPacket(2, "1650")
	packet = packet[strings.Index(packet, "demuxer -> "):]
	frame := visualAnalysisFrame("source", 1, "1300", 1920, 1080, "yuv420p")
	frame = frame[strings.Index(frame, "n:"):]
	text := visualAnalysisConfig("source", "1/1000") + visualAnalysisPacket(2, "1000") + visualAnalysisPacket(2, "2020") + visualAnalysisPacket(2, "1300") +
		strings.TrimSuffix(visualAnalysisFrame("source", 0, "1000", 1920, 1080, "yuv420p"), "\n") + packet +
		"[showinfo@analysis_source @ 0x2] [info] alpha_mode:straight\n" +
		"color_range:unknown " + frame +
		visualAnalysisFrame("source", 2, "1650", 1920, 1080, "yuv420p") +
		strings.TrimSuffix(visualAnalysisFrame("source", 3, "2020", 1920, 1080, "yuv420p"), "\n") + visualAnalysisOutput("1000", "1/1000") +
		visualAnalysisOutput("1650", "1/1000") + visualAnalysisOutput("2020", "1/1000")
	if _, err := log.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	log.Close(nil)
	if err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, 3*32*32)), log,
		func(analysisVisualFrame, []byte) error { return nil }); err != nil || log.result() != nil {
		t.Fatalf("interleaved metadata returned %v, %v", err, log.result())
	}
}

func TestAnalysisVisualRejectsSkippedNominalSlot(t *testing.T) {
	log := visualAnalysisTestLog(t)
	text := visualAnalysisConfig("source", "1/1000") + visualAnalysisPacket(2, "1000") +
		visualAnalysisFrame("source", 0, "1000", 1920, 1080, "yuv420p") + visualAnalysisPacket(2, "2020") +
		visualAnalysisFrame("source", 1, "2020", 1920, 1080, "yuv420p")
	if _, err := log.Write([]byte(text)); err == nil {
		t.Fatal("a large VFR gap was assigned to an earlier nominal slot")
	}
}

func TestAnalysisVisualBoundsAndMissingMetadataCancellation(t *testing.T) {
	for _, kind := range []string{"line", "bytes", "source_frames"} {
		t.Run(kind, func(t *testing.T) {
			log := visualAnalysisTestLog(t)
			text := visualAnalysisTestTranscript()
			switch kind {
			case "line":
				text = strings.Repeat("x", maxAnalysisVisualLogLine+1)
			case "bytes":
				log.limits.MaxStderrBytes = 5
			case "source_frames":
				log.limits.MaxSourceFrames = 1
			}
			if _, err := log.Write([]byte(text)); !errors.Is(err, ErrAnalysisBudget) {
				t.Fatalf("budget returned %v", err)
			}
		})
	}
	log := visualAnalysisTestLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := log.next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("metadata wait returned %v", err)
	}
	log.Close(io.ErrUnexpectedEOF)
	if _, err := log.next(context.Background()); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("closed metadata wait returned %v", err)
	}
}

func TestAnalysisVisualRawStreamRejectsTruncationExcessAndCallbackFailure(t *testing.T) {
	for _, size := range []int{0, 3*32*32 - 1, 3*32*32 + 1, 4 * 32 * 32} {
		log := visualAnalysisTestLog(t)
		if _, err := log.Write([]byte(visualAnalysisTestTranscript())); err != nil {
			t.Fatal(err)
		}
		log.Close(nil)
		if err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, size)), log, func(analysisVisualFrame, []byte) error { return nil }); err == nil {
			t.Fatalf("raw size %d was accepted", size)
		}
	}
	log := visualAnalysisTestLog(t)
	_, _ = log.Write([]byte(visualAnalysisTestTranscript()))
	log.Close(nil)
	want := errors.New("cache write failed")
	if err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, 3*32*32)), log,
		func(analysisVisualFrame, []byte) error { return want }); !errors.Is(err, want) {
		t.Fatalf("callback error became %v", err)
	}
}

func TestAnalysisVisualHashContrastAndOrientation(t *testing.T) {
	constant := bytes.Repeat([]byte{177}, 32*32)
	if hash, contrast := analysisVisualHash(constant); hash != 0 || contrast != 0 {
		t.Fatalf("constant image produced %x/%d", hash, contrast)
	}
	ramp := make([]byte, 32*32)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			ramp[y*32+x] = byte(255 - x*8)
		}
	}
	if hash, contrast := analysisVisualHash(ramp); hash != math.MaxUint64 || contrast < 280 || contrast > 300 {
		t.Fatalf("descending ramp produced %x/%d", hash, contrast)
	}
	for index := range ramp {
		ramp[index] = byte((index % 2) * 255)
	}
	if _, contrast := analysisVisualHash(ramp); contrast != 500 {
		t.Fatalf("half black/white contrast = %d", contrast)
	}
}

func TestAnalysisPTSUsesExactRationalsAndRejectsUnknownValues(t *testing.T) {
	base := big.NewRat(1, 90000)
	if ticks, err := analysisVisualTicks(90001, base, TicksPerSecond); err != nil || ticks != 111 {
		t.Fatalf("fractional timestamp = %d, %v", ticks, err)
	}
	if _, err := analysisVisualTicks(math.MaxInt64, big.NewRat(1, 1), math.MinInt64); err == nil {
		t.Fatal("overflowed timestamp was accepted")
	}
	for _, value := range []string{"NOPTS", "N/A", "-9223372036854775808", "9223372036854775808"} {
		if _, err := analysisPTSInteger(value); err == nil {
			t.Errorf("unknown PTS %q was accepted", value)
		}
	}
	for _, value := range []string{"0/1", "1/0", "-1/90", "1/2147483648", "1", "1/2/3"} {
		if _, err := analysisTimeBase(value); err == nil {
			t.Errorf("invalid time base %q was accepted", value)
		}
	}
}

func TestAnalysisPacketPTSBoundsReorderingAndAuditOnly(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	audit, err := newAnalysisPacketPTS(stream, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for _, pts := range []string{"3000", "1000", "2000"} {
		if _, err := audit.line(visualAnalysisPacket(2, pts)); err != nil {
			t.Fatal(err)
		}
	}
	if err := audit.match(90000, big.NewRat(1, 90000)); err != nil {
		t.Fatalf("exact rescale failed: %v", err)
	}
	if err := audit.match(90000, big.NewRat(1, 90000)); err == nil {
		t.Fatal("a consumed packet PTS was reused")
	}
	for index := 0; index < maxAnalysisPendingPTS; index++ {
		_, err = audit.line(visualAnalysisPacket(2, fmt.Sprint(100000+index)))
		if err != nil {
			break
		}
	}
	if !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("pending budget returned %v", err)
	}
	observer, err := newAnalysisPacketPTSAudit(stream, maxAnalysisPendingPTS+2)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxAnalysisPendingPTS+1; index++ {
		if _, err := observer.line(visualAnalysisPacket(2, fmt.Sprint(index))); err != nil {
			t.Fatalf("audit-only retained pending timestamps: %v", err)
		}
	}
	if len(observer.pending) != 0 {
		t.Fatal("audit-only retained timestamp data")
	}
}
