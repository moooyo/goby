package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

type canonicalVobSubCase struct {
	name          string
	index         string
	width, height int
	pts           [][]int64
}

func canonicalVobSubCases(t *testing.T) ([]canonicalVobSubCase, []byte) {
	t.Helper()
	idx, sub := externalSubtitleTestVobSub()
	base := string(idx)
	normalPTS := [][]int64{{3 * TicksPerSecond, 10 * TicksPerSecond}, {TicksPerSecond}}
	var palette, lexical string
	for _, line := range strings.Split(base, "\n") {
		if strings.HasPrefix(line, "palette:") {
			palette = strings.ReplaceAll(line, ",", ","+strings.Repeat(" ", 180))
		} else if line != "" && !strings.HasPrefix(line, "size:") {
			lexical += "\t" + line + "  \n"
		}
	}
	lexical = "\ufeff" + strings.TrimPrefix(lexical, "\t")
	lexical = strings.ReplaceAll(lexical, "00:00:01:000", "000:0:001:0")
	lexical += "  size: 00720 x 00480\n  " + palette + "\nalt: " + strings.Repeat("a", 4096) + "\n"
	global := strings.ReplaceAll(base, "delay: +00:00:02:000\n", "")
	global = strings.ReplaceAll(global, "delay: -00:00:01:000\n", "")
	global = strings.Replace(global, "id: en", "delay: +001:02:03:004\nid: en", 1)
	wide := strings.Replace(base, "+00:00:02:000", "-167:00:00:000", 1)
	wide = strings.Replace(wide, "timestamp: 00:00:01:000", "timestamp: 0:0:0:0", 1)
	wide = strings.Replace(wide, "timestamp: 00:00:08:000", "delay: +0:0:0:0\ntimestamp: 167:59:59:999", 1)
	wide = strings.Replace(wide, "-00:00:01:000", "+100:00:00:000", 1)
	return []canonicalVobSubCase{
		{name: "ordinary track delays", index: base, width: 320, height: 192, pts: normalPTS},
		{name: "indented wide fields and late presentation headers", index: lexical, width: 720, height: 480, pts: normalPTS},
		{name: "global delay inherited across tracks", index: global, width: 320, height: 192, pts: [][]int64{{37240040000, 37310040000}, {37250040000}}},
		{name: "negative crossing zero", index: strings.Replace(base, "+00:00:02:000", "-00:00:02:000", 1), width: 320, height: 192, pts: [][]int64{{-TicksPerSecond, 6 * TicksPerSecond}, {TicksPerSecond}}},
		{name: "full signed clock and per-entry delays", index: wide, width: 320, height: 192, pts: [][]int64{{-167 * 3600 * TicksPerSecond, 6047999990000}, {360002 * TicksPerSecond}}},
	}, sub
}

func TestWriteCanonicalVobSubIndexPreservesClockTracksAndPresentation(t *testing.T) {
	cases, sub := canonicalVobSubCases(t)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := externalSubtitleTestFile(t, "source.idx", []byte(test.index))
			companion := externalSubtitleTestFile(t, "source.sub", sub)
			var output bytes.Buffer
			if err := WriteCanonicalVobSubIndex(context.Background(), input, companion, &output); err != nil {
				t.Fatal(err)
			}
			document, err := readExternalVobSubIndex(context.Background(), bytes.NewReader(output.Bytes()), int64(len(sub)))
			if err != nil {
				t.Fatalf("canonical index: %v\n%s", err, &output)
			}
			var actual [][]int64
			for ordinal, track := range document.tracks {
				wantID, wantLanguage := 3, "en"
				if ordinal == 1 {
					wantID, wantLanguage = 7, "zh"
				}
				if track.SourceStreamIndex != ordinal || track.id != wantID || track.Language != wantLanguage {
					t.Fatalf("track ordering or DVD ID changed: %+v", track)
				}
				var pts []int64
				for _, entry := range track.entries {
					pts = append(pts, entry.pts)
				}
				actual = append(actual, pts)
			}
			if !reflect.DeepEqual(actual, test.pts) {
				t.Fatalf("effective clocks=%v want=%v", actual, test.pts)
			}
			text := output.String()
			if strings.Index(text, "palette:") > strings.Index(text, "id:") || strings.Contains(text, "alt:") || strings.Contains(text, "source.sub") {
				t.Fatalf("unexpected presentation header or source name: %s", text)
			}
			wantHeader := fmt.Sprintf("# VobSub index file, v7 (do not modify this line!)\nsize: %dx%d\npalette:", test.width, test.height)
			if !strings.HasPrefix(text, wantHeader) {
				t.Fatalf("size/palette were not normalized before streams: %s", text)
			}
			for _, line := range strings.Split(text, "\n") {
				if len(line) >= 2048 {
					t.Fatalf("line exceeds the FFmpeg lexical limit: %d", len(line))
				}
				if strings.HasPrefix(line, "timestamp:") && (len(line) < 14 || line[13] != ':') {
					t.Fatalf("timestamp hour field is not exactly two digits: %s", line)
				}
			}
			for _, file := range []*os.File{input, companion} {
				if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 3 {
					t.Fatalf("borrowed file position=%d error=%v", position, err)
				}
			}
		})
	}
}

func TestWriteCanonicalVobSubIndexRejectsAmbiguousOrInvalidSourcesBeforeOutput(t *testing.T) {
	idx, sub := externalSubtitleTestVobSub()
	for _, test := range []struct {
		name  string
		index string
		sub   []byte
	}{
		{"invalid canvas", string(idx) + "size: 720x0\n", sub},
		{"oversized canvas", string(idx) + "size: 4096x4096\n", sub},
		{"ambiguous canvas", string(idx) + "size: 720x480\nsize: 720x576\n", sub},
		{"canvas suffix", string(idx) + "size: 720x480 invalid\n", sub},
		{"mismatched SUB", strings.Replace(string(idx), "index: 3", "index: 4", 1), sub},
		{"truncated SUB", string(idx), sub[:len(sub)-40]},
		{"source path directive", string(idx) + "file: ../../private.sub\n", sub},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			err := WriteCanonicalVobSubIndex(context.Background(), externalSubtitleTestFile(t, "input.idx", []byte(test.index)), externalSubtitleTestFile(t, "input.sub", test.sub), &output)
			if err == nil || output.Len() != 0 {
				t.Fatalf("error=%v output bytes=%d", err, output.Len())
			}
		})
	}
}

type canonicalVobSubWriterFunc func([]byte) (int, error)

func (write canonicalVobSubWriterFunc) Write(data []byte) (int, error) {
	return write(data)
}

func TestWriteCanonicalVobSubIndexCancellationAndWriterFailures(t *testing.T) {
	idx, sub := externalSubtitleTestVobSub()
	input, companion := externalSubtitleTestFile(t, "input.idx", idx), externalSubtitleTestFile(t, "input.sub", sub)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	if err := WriteCanonicalVobSubIndex(ctx, input, companion, &output); !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("canceled call: error=%v output=%d", err, output.Len())
	}
	if err := WriteCanonicalVobSubIndex(context.Background(), input, companion, nil); !errors.Is(err, ErrBitmapSubtitle) {
		t.Fatalf("nil output: %v", err)
	}
	failure := errors.New("output unavailable")
	writer := canonicalVobSubWriterFunc(func([]byte) (int, error) { return 0, failure })
	if err := WriteCanonicalVobSubIndex(context.Background(), input, companion, writer); !errors.Is(err, failure) {
		t.Fatalf("writer failure: %v", err)
	}
	writer = canonicalVobSubWriterFunc(func(data []byte) (int, error) { return len(data) - 1, nil })
	if err := WriteCanonicalVobSubIndex(context.Background(), input, companion, writer); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short writer: %v", err)
	}
}

func TestWriteCanonicalVobSubIndexRejectsSourceMutationDuringOutput(t *testing.T) {
	idx, sub := externalSubtitleTestVobSub()
	for _, component := range []string{"index", "companion"} {
		t.Run(component, func(t *testing.T) {
			input, companion := externalSubtitleTestFile(t, "input.idx", idx), externalSubtitleTestFile(t, "input.sub", sub)
			changed := false
			writer := canonicalVobSubWriterFunc(func(data []byte) (int, error) {
				if !changed {
					file := input
					if component == "companion" {
						file = companion
					}
					handle, err := os.OpenFile(file.Name(), os.O_WRONLY|os.O_APPEND, 0)
					if err != nil {
						return 0, err
					}
					_, writeErr := handle.Write([]byte{0})
					closeErr := handle.Close()
					if err := errors.Join(writeErr, closeErr); err != nil {
						return 0, err
					}
					changed = true
				}
				return len(data), nil
			})
			err := WriteCanonicalVobSubIndex(context.Background(), input, companion, writer)
			if !changed || !errors.Is(err, ErrBitmapSubtitle) {
				t.Fatalf("changed=%t error=%v", changed, err)
			}
		})
	}
}

// This oracle checks the decoder's actual stream ordinals, DVD IDs, effective
// packet clocks and canvas, including cases its original IDX lexer cannot read.
func TestWriteCanonicalVobSubIndexFFprobeOracle(t *testing.T) {
	tool := os.Getenv("GOBY_TEST_EXTERNAL_SUBTITLE_FFPROBE")
	if tool == "" {
		t.Skip("set GOBY_TEST_EXTERNAL_SUBTITLE_FFPROBE for independent canonical IDX acceptance")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("the demux oracle must run on the Linux verification host")
	}
	cases, sub := canonicalVobSubCases(t)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, companion := externalSubtitleTestFile(t, "original.idx", []byte(test.index)), externalSubtitleTestFile(t, "original.sub", sub)
			var canonical bytes.Buffer
			if err := WriteCanonicalVobSubIndex(context.Background(), input, companion, &canonical); err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			path := filepath.Join(directory, "subtitle.idx")
			if err := os.WriteFile(path, canonical.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "subtitle.sub"), sub, 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(context.Background(), tool, "-v", "error", "-f", "vobsub", "-sub_name", "subtitle.sub", "-show_streams", "-show_packets", "-show_entries", "stream=index,id,codec_name,width,height:stream_tags=language:packet=stream_index,pts", "-of", "json", "-i", path)
			command.Dir = directory
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("oracle: %v: %s", err, output)
			}
			var result struct {
				Streams []struct {
					Index  int
					ID     string
					Codec  string `json:"codec_name"`
					Width  int
					Height int
					Tags   map[string]string
				}
				Packets []struct {
					StreamIndex int `json:"stream_index"`
					PTS         int64
				}
			}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Streams) != 2 || result.Streams[0].Index != 0 || result.Streams[0].ID != "0x3" || result.Streams[0].Tags["language"] != "en" || result.Streams[1].Index != 1 || result.Streams[1].ID != "0x7" || result.Streams[1].Tags["language"] != "zh" {
				t.Fatalf("oracle stream order or DVD IDs changed: %s", output)
			}
			var actual, expected []string
			for _, packet := range result.Packets {
				actual = append(actual, fmt.Sprintf("%d:%d", packet.StreamIndex, packet.PTS))
			}
			for ordinal, pts := range test.pts {
				for _, ticks := range pts {
					expected = append(expected, fmt.Sprintf("%d:%d", ordinal, ticks/10000))
				}
			}
			sort.Strings(actual)
			sort.Strings(expected)
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("oracle clocks=%v want=%v: %s", actual, expected, output)
			}
			for _, stream := range result.Streams {
				if stream.Codec != "dvd_subtitle" || stream.Width != test.width || stream.Height != test.height {
					t.Fatalf("oracle presentation header changed: %s", output)
				}
			}
		})
	}
}
