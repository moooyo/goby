package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func externalSubtitleTestFile(t *testing.T, name string, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return file
}

func externalSubtitleTestSUP(offset90k uint32) []byte {
	var data []byte
	for _, packet := range subtitleFixturePGSPackets(true) {
		for offset := 0; offset < len(packet.Data); {
			length := 13 + int(binary.BigEndian.Uint16(packet.Data[offset+11:offset+13]))
			segment := bytes.Clone(packet.Data[offset : offset+length])
			for _, start := range []int{2, 6} {
				binary.BigEndian.PutUint32(segment[start:start+4], binary.BigEndian.Uint32(segment[start:start+4])+offset90k)
			}
			data = append(data, segment...)
			offset += length
		}
	}
	return data
}

// This authors a real MPEG-2 pack/private_stream_1 envelope independently of
// the parser. Each fragment carries its DVD substream ID and an MPEG PTS.
func externalSubtitleTestPES(id byte, payload []byte, stamp90k uint64) []byte {
	pack := []byte{0, 0, 1, 0xba, 0x44, 0, 4, 0, 4, 1, 0, 0, 3, 0xf8}
	pts := []byte{0x21 | byte((stamp90k>>30)&7)<<1, byte(stamp90k >> 22), byte(stamp90k>>14)&0xfe | 1, byte(stamp90k >> 7), byte(stamp90k<<1) | 1}
	body := append([]byte{0x80, 0x80, 5}, pts...)
	body = append(body, id)
	body = append(body, payload...)
	pack = append(pack, 0, 0, 1, 0xbd, byte(len(body)>>8), byte(len(body)))
	pack = append(pack, body...)
	// Legal MPEG padding packets remain outside the SPU declared length.
	return append(pack, 0, 0, 1, 0xbe, 0, 3, 0xff, 0xff, 0xff)
}

func externalSubtitleTestVobSub() ([]byte, []byte) {
	packet, palette := subtitleFixtureDVDPacket(false)
	spu := packet.Data
	var sub []byte
	positions := make([]int, 3)
	positions[0] = len(sub)
	sub = append(sub, externalSubtitleTestPES(0x23, spu[:31], 90000)...)
	sub = append(sub, externalSubtitleTestPES(0x23, spu[31:], 90000)...)
	positions[1] = len(sub)
	sub = append(sub, externalSubtitleTestPES(0x27, spu, 2*90000)...)
	positions[2] = len(sub)
	sub = append(sub, externalSubtitleTestPES(0x23, spu, 8*90000)...)
	idx := fmt.Sprintf("# VobSub index file, v7 (do not modify this line!)\n%salpha: 100%%\nforced subs: OFF\ncustom colors: OFF, tridx: 1000, colors: 000000,ffffff,ffffff,ffffff\nid: en, index: 3\ndelay: +00:00:02:000\ntimestamp: 00:00:01:000, filepos: %09x\ntimestamp: 00:00:08:000, filepos: %09x\nid: zh, index: 7\ndelay: -00:00:01:000\ntimestamp: 00:00:02:000, filepos: %09x\n", palette, positions[0], positions[2], positions[1])
	return []byte(idx), sub
}

func TestExternalBitmapSUPInspectionAndRealDisplayClock(t *testing.T) {
	for _, shift := range []uint32{0, 20 * 90000, (1 << 32) - 90000} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			file := externalSubtitleTestFile(t, "captions.sup", externalSubtitleTestSUP(shift))
			tracks, err := InspectExternalBitmapSubtitles(context.Background(), "sup", file, nil)
			if err != nil || !slices.Equal(tracks, []ExternalBitmapSubtitleTrack{{SourceStreamIndex: 0, Codec: "hdmv_pgs_subtitle"}}) {
				t.Fatalf("tracks=%+v err=%v", tracks, err)
			}
			offset := int64(shift) * TicksPerSecond / 90000
			collector := subtitleTimelineCollector{duration: offset + 10*TicksPerSecond}
			warnings, err := walkExternalBitmapSubtitles(context.Background(), ExternalSubtitleTimelineInput{StreamIndex: 1000001, Codec: "hdmv_pgs_subtitle", Input: file}, collector.duration, func(cue BitmapSubtitleCue) error { return collector.add(context.Background(), cue) })
			// Rescale the full 90 kHz clock once, rather than adding rounded values.
			want := []SubtitleTimelineInterval{{StartTicks: (int64(shift) + 1024*90) * TicksPerSecond / 90000, EndTicks: (int64(shift) + 5632*90) * TicksPerSecond / 90000}}
			if err != nil || len(warnings) != 0 || !slices.Equal(collector.union(), want) {
				t.Fatalf("coverage=%+v want=%+v warnings=%v err=%v", collector.union(), want, warnings, err)
			}
			if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 3 {
				t.Fatalf("borrowed file position=%d error=%v", position, err)
			}
		})
	}
}

func TestExternalBitmapVobSubMultipleLanguagesDelayAndFragments(t *testing.T) {
	idx, sub := externalSubtitleTestVobSub()
	index := externalSubtitleTestFile(t, "captions.idx", idx)
	companion := externalSubtitleTestFile(t, "captions.sub", sub)
	tracks, err := InspectExternalBitmapSubtitles(context.Background(), "vobsub", index, companion)
	wantTracks := []ExternalBitmapSubtitleTrack{{SourceStreamIndex: 0, Codec: "dvd_subtitle", Language: "en"}, {SourceStreamIndex: 1, Codec: "dvd_subtitle", Language: "zh"}}
	if err != nil || !slices.Equal(tracks, wantTracks) {
		t.Fatalf("tracks=%+v err=%v", tracks, err)
	}
	for ordinal, want := range [][]SubtitleTimelineInterval{{{3 * TicksPerSecond, 60_720_000}, {10 * TicksPerSecond, 130_720_000}}, {{TicksPerSecond, 40_720_000}}} {
		collector := subtitleTimelineCollector{duration: 20 * TicksPerSecond}
		warnings, err := walkExternalBitmapSubtitles(context.Background(), ExternalSubtitleTimelineInput{StreamIndex: 1000001 + ordinal, SourceStreamIndex: ordinal, Codec: "dvd_subtitle", Input: index, Companion: companion}, collector.duration, func(cue BitmapSubtitleCue) error { return collector.add(context.Background(), cue) })
		if err != nil || len(warnings) != 0 || !slices.Equal(collector.union(), want) {
			t.Fatalf("track=%d got=%+v want=%+v warnings=%v err=%v", ordinal, collector.union(), want, warnings, err)
		}
	}
	for _, file := range []*os.File{index, companion} {
		if pos, err := file.Seek(0, io.SeekCurrent); err != nil || pos != 3 {
			t.Fatalf("borrowed position=%d error=%v", pos, err)
		}
	}
}

func TestExternalBitmapClipsWithoutFillingInitialBlank(t *testing.T) {
	file := externalSubtitleTestFile(t, "captions.sup", externalSubtitleTestSUP(0))
	collector := subtitleTimelineCollector{duration: 3 * TicksPerSecond}
	warnings, err := walkExternalBitmapSubtitles(context.Background(), ExternalSubtitleTimelineInput{Codec: "hdmv_pgs_subtitle", Input: file}, collector.duration, func(cue BitmapSubtitleCue) error { return collector.add(context.Background(), cue) })
	if err != nil || !slices.Equal(collector.union(), []SubtitleTimelineInterval{{10_240_000, 3 * TicksPerSecond}}) || !slices.Contains(warnings, "cue_intervals_clipped_to_source_presentation") {
		t.Fatalf("coverage=%v warnings=%v err=%v", collector.union(), warnings, err)
	}
	idx, sub := externalSubtitleTestVobSub()
	idx = bytes.Replace(idx, []byte("+00:00:02:000"), []byte("-00:00:02:000"), 1)
	index, companion := externalSubtitleTestFile(t, "captions.idx", idx), externalSubtitleTestFile(t, "captions.sub", sub)
	collector = subtitleTimelineCollector{duration: 20 * TicksPerSecond}
	warnings, err = walkExternalBitmapSubtitles(context.Background(), ExternalSubtitleTimelineInput{Codec: "dvd_subtitle", Input: index, Companion: companion}, collector.duration, func(cue BitmapSubtitleCue) error { return collector.add(context.Background(), cue) })
	if err != nil || !slices.Equal(collector.union(), []SubtitleTimelineInterval{{0, 20_720_000}, {6 * TicksPerSecond, 90_720_000}}) || !slices.Contains(warnings, "cue_intervals_clipped_to_source_presentation") {
		t.Fatalf("coverage=%v warnings=%v err=%v", collector.union(), warnings, err)
	}
}

func TestExternalBitmapInspectionRejectsMalformedAndMismatchedSources(t *testing.T) {
	idx, sub := externalSubtitleTestVobSub()
	for _, test := range []struct {
		name             string
		format           string
		index, companion []byte
		unsupported      bool
	}{
		{"missing companion", "vobsub", idx, nil, false},
		{"raw SUB is not SUP", "sup", sub, nil, false},
		{"truncated SUP", "sup", externalSubtitleTestSUP(0)[:19], nil, false},
		{"unterminated SUP", "sup", externalSubtitleTestSUP(0)[:len(externalSubtitleTestSUP(0))-13], nil, false},
		{"unknown SUP segment", "sup", append([]byte{'P', 'G', 0, 0, 0, 0, 0, 0, 0, 0, 0x7f, 0, 0}, externalSubtitleTestSUP(0)...), nil, false},
		{"SUP companion forbidden", "sup", externalSubtitleTestSUP(0), sub, false},
		{"mismatched language ID", "vobsub", bytes.Replace(idx, []byte("index: 3"), []byte("index: 4"), 1), sub, false},
		{"duplicate language ID", "vobsub", bytes.Replace(idx, []byte("index: 7"), []byte("index: 3"), 1), sub, false},
		{"missing index signature", "vobsub", []byte("id: en, index: 0\n"), sub, false},
		{"unknown command", "vobsub", append(bytes.Clone(idx), []byte("command: execute\n")...), sub, true},
		{"path directive", "vobsub", append(bytes.Clone(idx), []byte("file: ../../private.sub\n")...), sub, true},
		{"nondefault alpha", "vobsub", bytes.Replace(idx, []byte("alpha: 100%"), []byte("alpha: 0%"), 1), sub, true},
		{"oversized position", "vobsub", bytes.Replace(idx, []byte("filepos: 000000000"), []byte("filepos: 7fffffffffffffff"), 1), sub, false},
		{"duplicate position", "vobsub", append(bytes.Clone(idx), []byte("timestamp: 00:00:20:000, filepos: 000000000\n")...), sub, false},
		{"negative timestamp", "vobsub", bytes.Replace(idx, []byte("00:00:01:000"), []byte("-00:00:01:000"), 1), sub, false},
		{"invalid delay", "vobsub", bytes.Replace(idx, []byte("+00:00:02:000"), []byte("+00:99:02:000"), 1), sub, false},
		{"truncated SUB", "vobsub", idx, sub[:len(sub)-40], false},
		{"wrong binary companion", "vobsub", idx, externalSubtitleTestSUP(0), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := externalSubtitleTestFile(t, "input", test.index)
			var companion *os.File
			if test.companion != nil {
				companion = externalSubtitleTestFile(t, "companion", test.companion)
			}
			tracks, err := InspectExternalBitmapSubtitles(context.Background(), test.format, file, companion)
			if err == nil || len(tracks) != 0 || errors.Is(err, ErrSubtitleTimelineUnsupported) != test.unsupported {
				t.Fatalf("tracks=%v err=%v want unsupported=%t", tracks, err, test.unsupported)
			}
		})
	}
}

func TestExternalBitmapInspectionDoesNotRasterize(t *testing.T) {
	data := externalSubtitleTestSUP(0)
	for offset := 0; offset < len(data); {
		length := 13 + int(binary.BigEndian.Uint16(data[offset+11:offset+13]))
		if data[offset+10] == pgsObjectSegment {
			for index := offset + 24; index < offset+length; index++ {
				data[index] = 0
			}
		}
		offset += length
	}
	file := externalSubtitleTestFile(t, "invalid-rle.sup", data)
	if _, err := InspectExternalBitmapSubtitles(context.Background(), "sup", file, nil); err != nil {
		t.Fatalf("structure-only inspection decoded pixels: %v", err)
	}
	if _, err := walkExternalBitmapSubtitles(context.Background(), ExternalSubtitleTimelineInput{Codec: "hdmv_pgs_subtitle", Input: file}, 10*TicksPerSecond, func(BitmapSubtitleCue) error { return nil }); err == nil {
		t.Fatal("generation accepted malformed RLE")
	}
}

func TestExternalBitmapInspectionDVDControlEnvelopes(t *testing.T) {
	for _, test := range []struct {
		name        string
		mutate      func([]byte)
		unsupported bool
	}{
		{"invalid offset", func(data []byte) { data[2], data[3] = 0xff, 0xff }, false},
		{"invalid command", func(data []byte) { data[11] = 0x7f }, false},
		{"unsupported color change", func(data []byte) { data[11], data[12], data[13] = 7, 0, 6 }, true},
		{"truncated color change", func(data []byte) { data[35] = 7 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			spu := dvdTestSPU(false)
			test.mutate(spu)
			sub := externalSubtitleTestPES(0x20, spu, 90000)
			idx := []byte("# VobSub index file, v7\nid: en, index: 0\ntimestamp: 00:00:01:000, filepos: 0\n")
			_, err := InspectExternalBitmapSubtitles(context.Background(), "vobsub", externalSubtitleTestFile(t, "input.idx", idx), externalSubtitleTestFile(t, "input.sub", sub))
			if err == nil || errors.Is(err, ErrSubtitleTimelineUnsupported) != test.unsupported {
				t.Fatalf("err=%v unsupported=%t", err, test.unsupported)
			}
		})
	}
}

func TestExternalBitmapInspectionBudgetAndCancellation(t *testing.T) {
	file := externalSubtitleTestFile(t, "large.sup", []byte{1})
	large, err := os.OpenFile(file.Name(), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(MaxExternalBitmapSubtitleBytes + 1); err != nil {
		t.Fatal(err)
	}
	large.Close()
	if _, err := InspectExternalBitmapSubtitles(context.Background(), "sup", file, nil); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("oversized input: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InspectExternalBitmapSubtitles(ctx, "sup", file, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled inspection: %v", err)
	}
	if _, err := InspectExternalBitmapSubtitles(nil, "sup", file, nil); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestGenerateSubtitleTimelinesWithExternalIsAtomicAndUsesPublicIndex(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("generation requires the Linux verification host")
	}
	// The external-only generator needs source identity and duration, but never
	// demuxes this placeholder source or executes the pinned provenance file.
	source := externalSubtitleTestFile(t, "source.bin", []byte("source identity only"))
	stat, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	info := Info{Size: stat.Size(), FileChangeTimeNs: FileChangeTime(stat), DurationTicks: 20 * TicksPerSecond, FormatStartTicks: 47 * TicksPerSecond, FormatStartKnown: true, Streams: []Stream{{Index: 0, CodecType: "video", Codec: "h264"}}}
	tool := externalSubtitleTestFile(t, "provenance.bin", []byte("pinned provenance only"))
	digest := sha256.Sum256([]byte("pinned provenance only"))
	config := BitmapSubtitleConfig{FFprobePath: tool.Name(), FFprobeSHA256: hex.EncodeToString(digest[:])}
	sup := externalSubtitleTestFile(t, "captions.sup", externalSubtitleTestSUP(0))
	idx, sub := externalSubtitleTestVobSub()
	index, companion := externalSubtitleTestFile(t, "captions.idx", idx), externalSubtitleTestFile(t, "captions.sub", sub)
	external := []ExternalSubtitleTimelineInput{{StreamIndex: 1000003, Codec: "hdmv_pgs_subtitle", Input: sup}, {StreamIndex: 1000001, SourceStreamIndex: 1, Codec: "dvd_subtitle", Input: index, Companion: companion}}
	var output bytes.Buffer
	var completed []int
	summary, err := GenerateSubtitleTimelinesWithExternal(context.Background(), config, source, info, external, &output, func(progress SubtitleTimelineProgress) {
		if progress.TotalTracks != 2 {
			t.Fatalf("total tracks=%d", progress.TotalTracks)
		}
		completed = append(completed, progress.CompletedTracks)
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSubtitleTimelines(output.Bytes())
	if err != nil || summary.Profile != SubtitleTimelineExternalProfile || parsed.Profile != SubtitleTimelineExternalProfile || len(parsed.Tracks) != 2 || parsed.Tracks[0].StreamIndex != 1000001 || parsed.Tracks[1].StreamIndex != 1000003 || !slices.Equal(completed, []int{0, 1, 1, 2}) {
		t.Fatalf("summary=%+v parsed=%+v completed=%v err=%v", summary, parsed, completed, err)
	}
	if !slices.Equal(parsed.Tracks[0].Intervals, []SubtitleTimelineInterval{{10_000_000, 40_720_000}}) || !slices.Equal(parsed.Tracks[1].Intervals, []SubtitleTimelineInterval{{10_240_000, 56_320_000}}) {
		t.Fatalf("media container origin changed the absolute external clock: %+v", parsed.Tracks)
	}
	for _, test := range []struct {
		name     string
		change   func([]ExternalSubtitleTimelineInput)
		cancelAt int
	}{
		{"missing ordinal", func(tracks []ExternalSubtitleTimelineInput) { tracks[1].SourceStreamIndex = 2 }, -1},
		{"duplicate public index", func(tracks []ExternalSubtitleTimelineInput) { tracks[1].StreamIndex = tracks[0].StreamIndex }, -1},
		{"colliding internal index", func(tracks []ExternalSubtitleTimelineInput) { tracks[1].StreamIndex = 0 }, -1},
		{"invalid codec", func(tracks []ExternalSubtitleTimelineInput) { tracks[1].Codec = "subrip" }, -1},
		{"duplicate external ordinal", func(tracks []ExternalSubtitleTimelineInput) { tracks[1] = tracks[0]; tracks[1].StreamIndex++ }, -1},
		{"cancel after first track", func([]ExternalSubtitleTimelineInput) {}, 1},
		{"cancel after final track", func([]ExternalSubtitleTimelineInput) {}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracks := slices.Clone(external)
			test.change(tracks)
			var staged bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := GenerateSubtitleTimelinesWithExternal(ctx, config, source, info, tracks, &staged, func(progress SubtitleTimelineProgress) {
				if progress.CompletedTracks == test.cancelAt {
					cancel()
				}
			})
			if err == nil || staged.Len() != 0 {
				t.Fatalf("error=%v output=%d", err, staged.Len())
			}
		})
	}
	t.Run("changed companion before publication", func(t *testing.T) {
		var staged bytes.Buffer
		changed := false
		_, err := GenerateSubtitleTimelinesWithExternal(context.Background(), config, source, info, external, &staged, func(progress SubtitleTimelineProgress) {
			if progress.CompletedTracks == 2 && !changed {
				changed = true
				file, err := os.OpenFile(companion.Name(), os.O_WRONLY|os.O_APPEND, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Write([]byte{0}); err != nil {
					t.Fatal(err)
				}
				file.Close()
			}
		})
		if !changed || err == nil || staged.Len() != 0 {
			t.Fatalf("changed=%t error=%v output=%d", changed, err, staged.Len())
		}
	})
}

// Optional independent demux oracle verifies that the authored fixture really
// interoperates with FFprobe. It does not replace pixel/display-clock checks.
func TestExternalBitmapVobSubFFprobeOracle(t *testing.T) {
	tool := os.Getenv("GOBY_TEST_EXTERNAL_SUBTITLE_FFPROBE")
	if tool == "" {
		t.Skip("set GOBY_TEST_EXTERNAL_SUBTITLE_FFPROBE for independent VobSub demux acceptance")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("the demux oracle must run on the Linux verification host")
	}
	idx, sub := externalSubtitleTestVobSub()
	directory := t.TempDir()
	if exportDirectory := os.Getenv("GOBY_TEST_EXTERNAL_SUBTITLE_OUTPUT_DIR"); exportDirectory != "" {
		externalSubtitleExportFixtures(t, exportDirectory, idx, sub)
	}
	if err := os.WriteFile(filepath.Join(directory, "captions.idx"), idx, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "captions.sub"), sub, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(context.Background(), tool, "-v", "error", "-f", "vobsub", "-show_streams", "-show_packets", "-show_entries", "stream=index,codec_name:stream_tags=language:packet=stream_index,pts_time", "-of", "json", "-i", filepath.Join(directory, "captions.idx")).CombinedOutput()
	if err != nil {
		t.Fatalf("oracle: %v: %s", err, output)
	}
	var result struct {
		Streams []struct {
			Index int
			Codec string `json:"codec_name"`
			Tags  map[string]string
		}
		Packets []struct {
			StreamIndex int    `json:"stream_index"`
			PTS         string `json:"pts_time"`
		}
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 2 || len(result.Packets) != 3 || result.Streams[0].Codec != "dvd_subtitle" || result.Streams[0].Tags["language"] != "en" || result.Streams[1].Tags["language"] != "zh" {
		t.Fatalf("unexpected oracle: %s", output)
	}
	var actual []string
	for _, packet := range result.Packets {
		actual = append(actual, fmt.Sprintf("%d:%s", packet.StreamIndex, packet.PTS))
	}
	if strings.Join(actual, ",") != "1:1.000000,0:3.000000,0:10.000000" {
		t.Fatalf("oracle clock/ordinal changed: %v", actual)
	}
}

func externalSubtitleExportFixtures(t *testing.T, directory string, idx, sub []byte) {
	t.Helper()
	if !filepath.IsAbs(directory) {
		t.Fatal("fixture evidence directory must be absolute")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files := map[string][]byte{"captions.sup": externalSubtitleTestSUP(0), "captions.idx": idx, "captions.sub": sub}
	type fixtureFile struct {
		Name   string
		SHA256 string
		Bytes  int
	}
	type fixtureTrack struct {
		SourceStreamIndex int
		Codec             string
		Language          string
		Intervals         []SubtitleTimelineInterval
	}
	type fixtureSource struct {
		Format    string
		Input     string
		Companion string `json:",omitempty"`
		Tracks    []fixtureTrack
	}
	manifest := struct {
		Version       int
		DurationTicks int64
		Description   string
		Files         []fixtureFile
		Sources       []fixtureSource
	}{
		Version: 1, DurationTicks: 20 * TicksPerSecond, Description: "Authored SUP and MPEG-2 VobSub fixtures. Expected display intervals are independently declared from authored control clocks; no video or OCR result is included.",
		Sources: []fixtureSource{
			{Format: "sup", Input: "captions.sup", Tracks: []fixtureTrack{{SourceStreamIndex: 0, Codec: "hdmv_pgs_subtitle", Intervals: []SubtitleTimelineInterval{{10_240_000, 56_320_000}}}}},
			{Format: "vobsub", Input: "captions.idx", Companion: "captions.sub", Tracks: []fixtureTrack{{SourceStreamIndex: 0, Codec: "dvd_subtitle", Language: "en", Intervals: []SubtitleTimelineInterval{{30_000_000, 60_720_000}, {100_000_000, 130_720_000}}}, {SourceStreamIndex: 1, Codec: "dvd_subtitle", Language: "zh", Intervals: []SubtitleTimelineInterval{{10_000_000, 40_720_000}}}}},
		},
	}
	for _, name := range []string{"captions.sup", "captions.idx", "captions.sub"} {
		data := files[name]
		digest := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, fixtureFile{Name: name, SHA256: hex.EncodeToString(digest[:]), Bytes: len(data)})
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	files["expected.json"] = append(encoded, '\n')
	for _, name := range []string{"captions.sup", "captions.idx", "captions.sub", "expected.json"} {
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(files[name])
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatalf("write fixture %s: %v %v", name, writeErr, closeErr)
		}
	}
}
