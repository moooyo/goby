//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestBitmapSubtitleActualGPUClockCanvasAndClear(t *testing.T) {
	device := os.Getenv("GOBY_TEST_VAAPI_DEVICE")
	if device == "" {
		t.Skip("an explicitly selected AMD VAAPI device is required")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	directory := t.TempDir()
	subtitles := filepath.Join(directory, "rectangle.sup")
	if err := os.WriteFile(subtitles, bitmapSubtitlePGSFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(directory, "source.mkv")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=black:s=320x192:r=16:d=4.5", "-f", "sup", "-i", subtitles,
		"-map", "0:v:0", "-map", "1:s:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast",
		"-pix_fmt", "yuv420p", "-c:s", "copy", "-t", "4.5", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	// Matroska's millisecond clock may round the final encoded frame. Keep
	// the probed duration authoritative and bound only this fixture check.
	durationDelta := info.DurationTicks - 45*ticksPerSecond/10
	if err != nil || len(info.Streams) != 2 || info.Streams[1].Codec != "hdmv_pgs_subtitle" ||
		info.Streams[1].CodecType != "subtitle" || durationDelta < -ticksPerSecond/1000 || durationDelta > ticksPerSecond/1000 {
		t.Fatalf("fixture does not contain the bounded PGS source and timeline: %+v, %v", info, err)
	}
	for _, mode := range []string{"progressive", "hls"} {
		t.Run(mode, func(t *testing.T) {
			plan := Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: info.Streams[0].Index, AudioStreamIndex: -1,
				Width: 320, Height: 192, FrameRate: 16, VideoBitrate: 768000, SegmentSeconds: 1,
				StartTicks: 2 * ticksPerSecond, DurationTicks: info.DurationTicks,
				Hardware: Hardware{Decode: "vaapi", Encode: "vaapi", Device: device}, VideoFilters: VideoFilters{Backend: "vulkan"},
				Subtitle: SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: info.Streams[1].Index, OffsetTicks: ticksPerSecond / 2}}
			outputName := "main.m3u8"
			if mode == "progressive" {
				plan.OutputMode, plan.Container, plan.SegmentSeconds = "progressive", "mp4", 0
				plan.SourceFormatStartKnown, plan.SourceFormatStartTicks = info.FormatStartKnown, info.FormatStartTicks
				outputName = "stream.bin"
			}
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			outputDirectory := t.TempDir()
			ready, ended := false, false
			result, err := Run(ctx, ffmpeg, outputDirectory, input, plan, 1, func(progress Progress) {
				ready = ready || progress.Ready
				ended = ended || progress.Ended
			})
			if err != nil || !ended || mode == "progressive" && !ready {
				t.Fatalf("GPU bitmap burn did not produce media: err=%v ready=%v ended=%v: %s", err, ready, ended, result.StderrTail)
			}
			output := filepath.Join(outputDirectory, outputName)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
			facts := probeProgressiveVideoFrames(t, ctx, ffprobe, output)
			pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, output)
			const frameBytes = 320 * 192
			if len(facts.video) != 40 || len(pixels) != 40*frameBytes {
				t.Fatalf("bitmap composition changed the selected source window: frames=%d pixels=%d", len(facts.video), len(pixels))
			}
			origin := facts.video[0].time(t)
			if mode == "progressive" && math.Abs(origin) > .002 {
				t.Fatalf("progressive bitmap output retained the source seek origin: %g", origin)
			}
			for frame, fact := range facts.video {
				if math.Abs(fact.time(t)-origin-float64(frame)/16) > .002 {
					t.Fatalf("bitmap events changed video frame %d's presentation clock: %g", frame, fact.time(t))
				}
				visible := frame >= 16 && frame < 32
				inside, outside := 0, 0
				for y := 0; y < 192; y++ {
					for x := 0; x < 320; x++ {
						if pixels[frame*frameBytes+y*320+x] <= 180 {
							continue
						}
						if x >= 112 && x < 208 && y >= 144 && y < 168 {
							inside++
						} else if x < 108 || x >= 212 || y < 140 || y >= 172 {
							outside++
						}
					}
				}
				if visible && inside < 2000 || !visible && inside > 4 || outside > 4 {
					t.Fatalf("PGS canvas or clear event is wrong at frame %d: visible=%v inside=%d outside=%d", frame, visible, inside, outside)
				}
			}
		})
	}
}

// bitmapSubtitlePGSFixture authors a small legal PGS stream: a transparent
// 320x192 canvas, a white 96x24 object at (112,144) from 2.5 to 3.5 seconds,
// and explicit clear display sets before and after it. No third-party asset is
// needed. Segment/RLE layout follows FFmpeg n9.0.1's SUP demuxer and PGS decoder.
func bitmapSubtitlePGSFixture() []byte {
	var output bytes.Buffer
	segment := func(pts uint32, kind byte, data []byte) {
		header := make([]byte, 13)
		copy(header, "PG")
		binary.BigEndian.PutUint32(header[2:6], pts)
		binary.BigEndian.PutUint32(header[6:10], pts)
		header[10] = kind
		binary.BigEndian.PutUint16(header[11:13], uint16(len(data)))
		output.Write(header)
		output.Write(data)
	}
	presentation := func(number uint16, epoch, object bool) []byte {
		data := make([]byte, 11)
		binary.BigEndian.PutUint16(data[0:2], 320)
		binary.BigEndian.PutUint16(data[2:4], 192)
		data[4] = 0x20
		binary.BigEndian.PutUint16(data[5:7], number)
		if epoch {
			data[7] = 0x80
		}
		if object {
			data[10] = 1
			data = append(data, 0, 1, 0, 0, 0, 112, 0, 144)
		}
		return data
	}
	segment(0, 0x16, presentation(0, true, false))
	segment(0, 0x80, nil)
	const start, end = uint32(225000), uint32(315000)
	segment(start, 0x16, presentation(1, true, true))
	segment(start, 0x17, []byte{1, 0, 0, 0, 0, 0, 1, 64, 0, 192})
	segment(start, 0x14, []byte{0, 0, 0, 16, 128, 128, 0, 1, 235, 128, 128, 255})
	rle := bytes.Repeat([]byte{0, 0xc0, 96, 1, 0, 0}, 24)
	object := []byte{0, 1, 0, 0xc0, 0, 0, byte(len(rle) + 4), 0, 96, 0, 24}
	object = append(object, rle...)
	segment(start, 0x15, object)
	segment(start, 0x80, nil)
	segment(end, 0x16, presentation(2, false, false))
	segment(end, 0x80, nil)
	return output.Bytes()
}
