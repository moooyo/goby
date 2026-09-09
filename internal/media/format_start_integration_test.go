package media

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeActualFormatStartForMP4MatroskaAndTransportStream(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "format-start.mp4")
	// Four tiny all-intra frames begin at 1.25 seconds. A demuxer can report an
	// unknown format origin even when these packet timestamps are fully known.
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=64x64:rate=10",
		"-t", "0.4", "-an", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-bf", "0", "-g", "1", "-pix_fmt", "yuv420p", "-output_ts_offset", "1.25", source)
	for _, fixture := range []struct {
		name, extension string
		options         []string
		withAudio       bool
		formatStart     string
		known           bool
		ticks           int64
	}{
		{name: "MP4", extension: ".mp4", formatStart: "1.250000", known: true, ticks: 12_500_000},
		{name: "MatroskaUnknown", extension: ".mkv", options: []string{"-avoid_negative_ts", "disabled"}, formatStart: "N/A"},
		{name: "MatroskaWithAudio", extension: ".with-audio.mkv", options: []string{"-avoid_negative_ts", "disabled"},
			withAudio: true, formatStart: "0.000000", known: true},
		{name: "TransportStream", extension: ".ts", options: []string{"-mpegts_copyts", "1", "-muxdelay", "0", "-muxpreload", "0"},
			formatStart: "1.250000", known: true, ticks: 12_500_000},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			path := source
			if fixture.extension != ".mp4" {
				path = filepath.Join(directory, "format-start"+fixture.extension)
				args := []string{"-copyts", "-i", source, "-map", "0:v:0", "-c", "copy"}
				if fixture.withAudio {
					args = []string{"-copyts", "-i", source, "-f", "lavfi", "-t", "0.4", "-i", "sine=frequency=800:sample_rate=48000",
						"-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", "-c:a", "aac"}
				}
				args = append(args, fixture.options...)
				args = append(args, path)
				audioProbeRunFFmpeg(t, ffmpeg, args...)
			}
			raw, err := runLimited(context.Background(), 10*time.Second, 1024, ffprobe,
				"-v", "error", "-show_entries", "format=start_time", "-of", "default=noprint_wrappers=1:nokey=1", path)
			if err != nil || strings.TrimSpace(string(raw)) != fixture.formatStart {
				t.Fatalf("the fixture did not preserve its independently observed format clock: %q, %v", raw, err)
			}
			if !fixture.known {
				// Do not replace the missing format value with a packet/stream
				// origin. This valid MKV still contains all four timestamped frames.
				packets, err := runLimited(context.Background(), 10*time.Second, 4096, ffprobe,
					"-v", "error", "-select_streams", "v:0", "-show_packets", "-count_packets",
					"-show_entries", "packet=pts_time:stream=nb_read_packets", "-of", "json", path)
				var observed struct {
					Packets []struct {
						PTS string `json:"pts_time"`
					} `json:"packets"`
					Streams []struct {
						Count string `json:"nb_read_packets"`
					} `json:"streams"`
				}
				if err != nil || json.Unmarshal(packets, &observed) != nil || len(observed.Packets) != 4 || len(observed.Streams) != 1 ||
					observed.Streams[0].Count != "4" || observed.Packets[0].PTS != "1.250000" || observed.Packets[3].PTS != "1.550000" {
					t.Fatalf("unknown format origin fixture lost its independently observed packets: %s, %v", packets, err)
				}
			}
			info, err := (Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(context.Background(), path)
			if err != nil || info.FormatStartKnown != fixture.known || info.FormatStartTicks != fixture.ticks {
				t.Fatalf("probe lost the independently reported %s format clock: %+v, %v", fixture.name, info, err)
			}
			if info.PresentationOriginTicks != 0 || info.AudioDurationExact || info.ProbeVersion != CurrentProbeVersion {
				t.Fatalf("format origin altered unrelated presentation facts: %+v", info)
			}
		})
	}
}
