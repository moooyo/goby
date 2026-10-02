//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// The source changes color at known source times. Decode the actual outputs and
// compare those transitions with their measured packet clock, rather than
// assuming that a segment number or requested start is a mux timestamp.
func TestGeneratedWindowActualVideoEpochsAndSourceClock(t *testing.T) {
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	const fps = 30000.0 / 1001.0
	source := filepath.Join(t.TempDir(), "source.mkv")
	generatedWindowMediaCommand(t, ctx, ffmpeg,
		"-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i",
		"color=c=red:s=160x96:r=30000/1001:d=12,drawbox=x=0:y=0:w=iw:h=ih:color=green:t=fill:enable='gte(t,7)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,8)'",
		"-f", "lavfi", "-i", "aevalsrc=0.15*sin(2*PI*(173*t+127*t*t)):s=48000:d=12",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-bf", "0",
		"-g", "30", "-pix_fmt", "yuv420p", "-c:a", "pcm_s16le", "-output_ts_offset", "2", source)
	// A nonzero container origin must not become an extra source-time offset.
	// The fixture's color changes are at source-relative seconds seven/eight,
	// while its container packets begin at two seconds.
	originData := generatedWindowMediaCommand(t, ctx, ffprobe, "-v", "error", "-show_entries", "format=start_time", "-of", "json", source)
	var originFacts struct {
		Format struct {
			Start string `json:"start_time"`
		} `json:"format"`
	}
	if err := json.Unmarshal(originData, &originFacts); err != nil {
		t.Fatal(err)
	}
	formatStart, err := strconv.ParseFloat(originFacts.Format.Start, 64)
	if err != nil || math.Abs(formatStart-2) > .002 {
		t.Fatalf("fixture did not retain its independent nonzero container origin: %s", originData)
	}
	for _, fixture := range []struct {
		name, segmentType string
		adaptive          bool
		copyVideo         bool
	}{{"ts", "mpegts", false, false}, {"fmp4", "fmp4", false, false}, {"adaptive_fmp4", "fmp4", true, false},
		{"copy_ts", "mpegts", false, true}, {"copy_fmp4", "fmp4", false, true}} {
		t.Run(fixture.name, func(t *testing.T) {
			p := Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: 1, AudioCodec: "aac",
				Width: 160, Height: 96, FrameRate: fps, VideoBitrate: 256000, AudioBitrate: 96000, AudioChannels: 1, AudioSampleRate: 48000,
				DurationTicks: 12 * ticksPerSecond, StartTicks: 61_234_567, SegmentSeconds: 1,
				HLS: HLSPlan{SegmentType: fixture.segmentType, Window: HLSWindow{EndTicks: 94_567_891, StartNumber: 12000}}}
			if fixture.segmentType == "fmp4" {
				p.Container = "mp4"
			}
			if fixture.copyVideo {
				p.VideoCodec, p.VideoCopyCodec = "copy", "h264"
				p.Width, p.Height, p.FrameRate, p.VideoBitrate = 0, 0, 0, 0
			}
			if fixture.adaptive {
				p.HLS.RenditionCount = 2
				p.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 96, VideoBitrate: 256000}
				p.HLS.Renditions[1] = HLSRendition{Width: 80, Height: 48, VideoBitrate: 96000}
			}
			directory, clocks := generatedWindowRun(t, ctx, ffmpeg, source, p)
			count := max(1, p.HLS.RenditionCount)
			if len(clocks) != count {
				t.Fatalf("finite outputs did not retain their own measured clocks: %+v", clocks)
			}
			var referencePTS []float64
			for rendition := 0; rendition < count; rendition++ {
				list := generatedWindowReadList(t, directory, HLSPlaylistName(rendition, p.HLS.RenditionCount), p.HLS.Window.StartNumber)
				if len(list.Segments) < 3 || len(list.Segments) > 5 {
					t.Fatalf("finite production did not publish its measured window: %+v", list)
				}
				path := generatedWindowCombine(t, directory, list, "media."+p.Container)
				width, height := 160, 96
				if p.HLS.RenditionCount != 0 {
					width, height = p.HLS.Renditions[rendition].Width, p.HLS.Renditions[rendition].Height
				}
				frames := generatedWindowVideoFrames(t, ctx, ffprobe, path, width, height)
				clock := clocks[rendition]
				anchor := generatedWindowAssertSourceAnchor(t, p, clock, new(big.Rat).SetInt64(p.StartTicks))
				anchorSeconds, _ := new(big.Rat).Quo(anchor, new(big.Rat).SetInt64(ticksPerSecond)).Float64()
				span := frames[len(frames)-1] - frames[0] + 1/fps
				requestedSpan := float64(p.HLS.Window.EndTicks-p.StartTicks) / float64(ticksPerSecond)
				wantSpan := requestedSpan
				if fixture.copyVideo {
					// Copy may begin at a retained reference packet before S. Its
					// measured anchor, rather than the requested seek, owns that
					// epoch's first packet. This is not a fixed-cut copy proof.
					wantSpan = float64(p.HLS.Window.EndTicks)/float64(ticksPerSecond) - anchorSeconds
				}
				if math.Abs(span-wantSpan) > 2/fps+.002 || span >= float64(p.DurationTicks-p.StartTicks)/float64(ticksPerSecond)-1 {
					t.Fatalf("finite output retained the wrong source interval: span=%g, window=%g, full source=%d", span, wantSpan, p.DurationTicks)
				}
				delta := frames[0] - anchorSeconds
				pixels := generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", path,
					"-map", "0:v:0", "-fps_mode", "passthrough", "-pix_fmt", "rgb24", "-f", "rawvideo", "-")
				frameBytes := width * height * 3
				if len(pixels) != len(frames)*frameBytes {
					t.Fatalf("decoded frame count disagrees with presentation evidence: pixels=%d, frames=%d", len(pixels), len(frames))
				}
				firstGreen, firstBlue := -1, -1
				for frame := range frames {
					pixel := pixels[frame*frameBytes+(width*height/2)*3:]
					r, g, b := int(pixel[0]), int(pixel[1]), int(pixel[2])
					if frame == 0 && (r <= g+40 || r <= b+40) {
						t.Fatal("the finite window did not begin in the source's red interval")
					}
					if firstGreen < 0 && g > r+40 && g > b+40 {
						firstGreen = frame
					}
					if firstBlue < 0 && b > r+40 && b > g+40 {
						firstBlue = frame
					}
				}
				for _, marker := range []struct {
					frame  int
					second float64
				}{{firstGreen, 7}, {firstBlue, 8}} {
					if marker.frame < 0 || math.Abs(frames[marker.frame]-(marker.second+delta)) > 2/fps+.002 {
						t.Fatalf("source marker did not follow the measured source clock: frame=%d, source second=%g, delta=%g", marker.frame, marker.second, delta)
					}
				}
				pcm := generatedWindowPCM(t, ctx, ffmpeg, path, 48000)
				if math.Abs(float64(len(pcm))/48000-requestedSpan) > 3*1024.0/48000+.005 {
					t.Fatalf("audio escaped the finite video window: samples=%d, seconds=%g", len(pcm), requestedSpan)
				}
				if rendition == 0 {
					referencePTS = frames
				} else {
					if len(referencePTS) != len(frames) {
						t.Fatal("adaptive outputs selected different finite frame intervals")
					}
					for index := range frames {
						if math.Abs(frames[index]-referencePTS[index]) > .002 {
							t.Fatal("adaptive outputs lost their shared presentation clock")
						}
					}
				}
			}
		})
	}
}

func TestGeneratedWindowActualPackedAudioTransportAndBounds(t *testing.T) {
	ctx, ffmpeg, _ := generatedWindowMediaTools(t)
	source := filepath.Join(t.TempDir(), "source.wav")
	generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i",
		"aevalsrc=0.15*sin(2*PI*(173*t+80*t*t)):s=48000:d=15", "-c:a", "pcm_s16le", source)
	for _, codec := range []string{"aac", "mp3"} {
		t.Run(codec, func(t *testing.T) {
			p := Plan{Container: codec, VideoStreamIndex: -1, AudioStreamIndex: 0, AudioCodec: codec,
				AudioBitrate: 128000, AudioChannels: 1, AudioSampleRate: 48000, DurationTicks: 15 * ticksPerSecond,
				StartTicks: 82_345_678, SegmentSeconds: 1,
				HLS: HLSPlan{SegmentType: "packed", Window: HLSWindow{EndTicks: 116_789_012, StartNumber: 12345}}}
			directory, clocks := generatedWindowRun(t, ctx, ffmpeg, source, p)
			if len(clocks) != 1 {
				t.Fatalf("packed finite output omitted its pre-mux clock: %+v", clocks)
			}
			generatedWindowAssertSourceAnchor(t, p, clocks[0], new(big.Rat).SetInt64(p.StartTicks))
			list := generatedWindowReadList(t, directory, "main.m3u8", p.HLS.Window.StartNumber)
			var elapsed int64
			for _, segment := range list.Segments {
				data, err := os.ReadFile(filepath.Join(directory, segment.Name))
				if err != nil {
					t.Fatal(err)
				}
				_, timestamp := generatedWindowPackedPayload(t, data)
				generatedWindowAssertTransport(t, timestamp, new(big.Rat).SetInt64(p.StartTicks), elapsed)
				elapsed += segment.DurationTicks
			}
			path := generatedWindowCombine(t, directory, list, "audio."+codec)
			pcm := generatedWindowPCM(t, ctx, ffmpeg, path, p.AudioSampleRate)
			span := float64(p.HLS.Window.EndTicks-p.StartTicks) / float64(ticksPerSecond)
			frameSamples := 1024.0
			if codec == "mp3" {
				frameSamples = 1152
			}
			if math.Abs(float64(len(pcm))/48000-span) > 3*frameSamples/48000+.005 || elapsed >= p.DurationTicks-p.StartTicks {
				t.Fatalf("packed job did not stop at its finite source end: samples=%d, EXTINF ticks=%d, window seconds=%g", len(pcm), elapsed, span)
			}
		})
	}
}

// Each selected resampling window has an exact output length divisible by an
// AAC frame. Rounding the relative source count instead of subtracting absolute
// output indexes adds one sample and therefore a whole encoded frame. The
// reference decodes the complete source, slices it in Go, independently
// resamples it, and supplies exactly the specified samples to an AAC encoder.
// This tests one completed epoch; it makes no gapless restart claim.
func TestGeneratedWindowActualSampleSeekAbsoluteResampleLimits(t *testing.T) {
	ctx, ffmpeg, _ := generatedWindowMediaTools(t)
	for _, fixture := range []struct {
		sourceRate, outputRate int
		start, end, samples    int64
		startTicks, endTicks   int64
	}{
		{44100, 48000, 88201, 220854, 144384, 20_000_113, 50_080_158},
		{48000, 44100, 96001, 249810, 141312, 20_000_104, 52_043_645},
	} {
		t.Run(fmt.Sprintf("%d_to_%d", fixture.sourceRate, fixture.outputRate), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "source.ogg")
			generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i",
				"aevalsrc=0.15*sin(2*PI*(173*t+127*t*t)):s="+strconv.Itoa(fixture.sourceRate)+":d=7",
				"-map", "0:a:0", "-c:a", "flac", "-threads:a", "1", "-f", "ogg", source)
			full := generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", source,
				"-map", "0:a:0", "-c:a", "pcm_f32le", "-f", "f32le", "-")
			if len(full)%4 != 0 || len(full)/4 != 7*fixture.sourceRate {
				t.Fatalf("source reference does not have its complete exact sample count: %d bytes", len(full))
			}
			ceil := func(n, d int64) int64 { return n/d + generatedWindowBoolInt64(n%d != 0) }
			start := ceil(fixture.startTicks*int64(fixture.sourceRate), ticksPerSecond)
			end := ceil(fixture.endTicks*int64(fixture.sourceRate), ticksPerSecond)
			want := ceil(end*int64(fixture.outputRate), int64(fixture.sourceRate)) - ceil(start*int64(fixture.outputRate), int64(fixture.sourceRate))
			if start != fixture.start || end != fixture.end || want != fixture.samples || want%1024 != 0 || ceil((end-start)*int64(fixture.outputRate), int64(fixture.sourceRate)) != want+1 {
				t.Fatal("fixture does not distinguish absolute sample indexes from relative rounding")
			}
			p := Plan{Container: "aac", VideoStreamIndex: -1, AudioStreamIndex: 0, AudioCodec: "aac", AudioBitrate: 96000,
				AudioChannels: 1, AudioSampleRate: fixture.outputRate, AudioSampleSeek: true,
				AudioSourceSampleRate: fixture.sourceRate, AudioSourceSampleCount: int64(len(full) / 4), DurationTicks: 7 * ticksPerSecond,
				StartTicks: fixture.startTicks, SegmentSeconds: 1,
				HLS: HLSPlan{SegmentType: "packed", Window: HLSWindow{EndTicks: fixture.endTicks, StartNumber: 7000}}}
			directory, clocks := generatedWindowRun(t, ctx, ffmpeg, source, p)
			if len(clocks) != 1 {
				t.Fatalf("sample-bound finite output omitted its clock: %+v", clocks)
			}
			origin := new(big.Rat).SetFrac(big.NewInt(start*ticksPerSecond), big.NewInt(int64(fixture.sourceRate)))
			generatedWindowAssertSourceAnchor(t, p, clocks[0], origin)
			list := generatedWindowReadList(t, directory, "main.m3u8", p.HLS.Window.StartNumber)
			var elapsed int64
			for _, segment := range list.Segments {
				data, err := os.ReadFile(filepath.Join(directory, segment.Name))
				if err != nil {
					t.Fatal(err)
				}
				_, timestamp := generatedWindowPackedPayload(t, data)
				generatedWindowAssertTransport(t, timestamp, origin, elapsed)
				elapsed += segment.DurationTicks
			}
			actualPath := generatedWindowCombine(t, directory, list, "actual.aac")
			actual := generatedWindowPCM(t, ctx, ffmpeg, actualPath, fixture.outputRate)
			prefix := filepath.Join(t.TempDir(), "source-window.f32")
			if err := os.WriteFile(prefix, full[start*4:end*4], 0600); err != nil {
				t.Fatal(err)
			}
			resampled := generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "f32le",
				"-ar", strconv.Itoa(fixture.sourceRate), "-ac", "1", "-i", prefix,
				"-ar", strconv.Itoa(fixture.outputRate), "-c:a", "pcm_f32le", "-f", "f32le", "-")
			if int64(len(resampled)) < want*4 {
				t.Fatal("independent resampling did not cover the requested output sample range")
			}
			referenceInput := filepath.Join(t.TempDir(), "exact-output-window.f32")
			if err := os.WriteFile(referenceInput, resampled[:want*4], 0600); err != nil {
				t.Fatal(err)
			}
			referencePath := filepath.Join(t.TempDir(), "reference.aac")
			generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "f32le",
				"-ar", strconv.Itoa(fixture.outputRate), "-ac", "1", "-i", referenceInput,
				"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-profile:a", "aac_low", "-f", "adts", referencePath)
			reference := generatedWindowPCM(t, ctx, ffmpeg, referencePath, fixture.outputRate)
			if len(actual) != len(reference) || int64(len(actual)) < want || int64(len(actual)) > want+2048 {
				t.Fatalf("real AAC output violated the absolute sample limit: actual=%d, independent=%d, input limit=%d", len(actual), len(reference), want)
			}
			var difference, power float64
			for index := 4096; index < 8192; index++ {
				delta := float64(actual[index]) - float64(reference[index])
				difference += delta * delta
				power += float64(reference[index]) * float64(reference[index])
			}
			if power == 0 || math.IsNaN(difference) || difference/power >= .02 {
				t.Fatalf("finite sample seek selected the wrong source content: normalized MSE=%g", difference/power)
			}
		})
	}
}

func generatedWindowMediaTools(t *testing.T) (context.Context, string, string) {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual finite generated HLS verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return ctx, ffmpeg, ffprobe
}

func generatedWindowMediaCommand(t *testing.T, ctx context.Context, executable string, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, executable, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil || stderr.Len() != 0 {
		t.Fatalf("media command %s failed: %v: %s", filepath.Base(executable), err, stderr.String())
	}
	return data
}

func generatedWindowRun(t *testing.T, ctx context.Context, ffmpeg, source string, p Plan) (string, map[int]HLSMuxClock) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	directory := t.TempDir()
	var mu sync.Mutex
	clocks := make(map[int]HLSMuxClock)
	result, err := Run(ctx, ffmpeg, directory, input, p, 1, func(progress Progress) {
		if progress.HLSClock != nil {
			mu.Lock()
			clocks[progress.HLSClock.Rendition] = *progress.HLSClock
			mu.Unlock()
		}
	})
	if err != nil {
		t.Fatalf("finite generated producer failed: %v: %s", err, result.StderrTail)
	}
	return directory, clocks
}

func generatedWindowReadList(t *testing.T, directory, name string, first int) MediaPlaylist {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	list, err := ParseMediaPlaylist(data)
	if err != nil || !list.Ended || list.Type != "EVENT" || list.Sequence != int64(first) || len(list.Segments) == 0 {
		t.Fatalf("completed finite job did not retain its EVENT epoch and namespace: %+v, %v", list, err)
	}
	for index, segment := range list.Segments {
		if segment.Number != int64(first+index) || segment.Discontinuity != (index == 0) {
			t.Fatalf("finite epoch changed its numbering or discontinuity boundary: %+v", segment)
		}
	}
	return list
}

func generatedWindowCombine(t *testing.T, directory string, list MediaPlaylist, name string) string {
	t.Helper()
	var data []byte
	if list.InitName != "" {
		var err error
		data, err = os.ReadFile(filepath.Join(directory, list.InitName))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, segment := range list.Segments {
		part, err := os.ReadFile(filepath.Join(directory, segment.Name))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Ext(segment.Name) == ".aac" || filepath.Ext(segment.Name) == ".mp3" {
			part, _ = generatedWindowPackedPayload(t, part)
		}
		if len(data)+len(part) > 32<<20 {
			t.Fatal("finite fixture exceeded its media byte budget")
		}
		data = append(data, part...)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func generatedWindowVideoFrames(t *testing.T, ctx context.Context, ffprobe, path string, width, height int) []float64 {
	t.Helper()
	data := generatedWindowMediaCommand(t, ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_frames",
		"-show_entries", "frame=best_effort_timestamp_time:stream=width,height", "-of", "json", path)
	var facts struct {
		Frames []struct {
			PTS string `json:"best_effort_timestamp_time"`
		} `json:"frames"`
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &facts); err != nil || len(facts.Streams) != 1 || facts.Streams[0].Width != width || facts.Streams[0].Height != height || len(facts.Frames) < 2 {
		t.Fatalf("actual finite video lacks its requested rendition or frame evidence: %s", data)
	}
	frames := make([]float64, len(facts.Frames))
	for index, frame := range facts.Frames {
		value, err := strconv.ParseFloat(frame.PTS, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || index > 0 && value <= frames[index-1] {
			t.Fatalf("invalid finite presentation clock: %q", frame.PTS)
		}
		frames[index] = value
	}
	return frames
}

func generatedWindowPCM(t *testing.T, ctx context.Context, ffmpeg, path string, rate int) []int16 {
	t.Helper()
	data := generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", path,
		"-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(rate), "-c:a", "pcm_s16le", "-f", "s16le", "-")
	if len(data) == 0 || len(data)%2 != 0 {
		t.Fatal("actual finite output has no complete decoded audio samples")
	}
	pcm := make([]int16, len(data)/2)
	for index := range pcm {
		pcm[index] = int16(binary.LittleEndian.Uint16(data[index*2:]))
	}
	return pcm
}

func generatedWindowAssertSourceAnchor(t *testing.T, p Plan, clock HLSMuxClock, origin *big.Rat) *big.Rat {
	t.Helper()
	anchor, err := HLSWindowSourceAnchorTicks(p, clock)
	pre := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(clock.PTS), big.NewInt(clock.TimeBaseNumerator*ticksPerSecond)), big.NewInt(clock.TimeBaseDenominator))
	want := new(big.Rat).Add(new(big.Rat).Set(origin), pre)
	if err != nil || anchor == nil || anchor.Cmp(want) != 0 {
		t.Fatalf("source anchor lost exact sampled origin or pre-mux time base: got=%v, want=%s, error=%v", anchor, want.RatString(), err)
	}
	return anchor
}

func generatedWindowPackedPayload(t *testing.T, data []byte) ([]byte, uint64) {
	t.Helper()
	if len(data) < 10 || string(data[:3]) != "ID3" || data[3] != 4 {
		t.Fatal("packed finite output omitted its ID3v2.4 transport timestamp")
	}
	synchsafe := func(value []byte) int {
		result := 0
		for _, part := range value {
			if part&0x80 != 0 {
				t.Fatal("invalid ID3 synchsafe size")
			}
			result = result<<7 | int(part)
		}
		return result
	}
	end := 10 + synchsafe(data[6:10])
	if end <= 20 || end >= len(data) {
		t.Fatal("packed transport tag did not precede complete audio payload")
	}
	for offset := 10; offset+10 <= end; {
		size := synchsafe(data[offset+4 : offset+8])
		if size == 0 || size > end-offset-10 {
			t.Fatal("invalid packed transport frame size")
		}
		frame := data[offset+10 : offset+10+size]
		if string(data[offset:offset+4]) == "PRIV" {
			ownerEnd := bytes.IndexByte(frame, 0)
			if ownerEnd >= 0 && string(frame[:ownerEnd]) == "com.apple.streaming.transportStreamTimestamp" {
				if len(frame)-ownerEnd-1 != 8 {
					t.Fatal("invalid packed transport timestamp width")
				}
				stamp := binary.BigEndian.Uint64(frame[ownerEnd+1:])
				if stamp>>33 != 0 {
					t.Fatal("packed transport timestamp exceeded its 33-bit clock")
				}
				return data[end:], stamp
			}
		}
		offset += 10 + size
	}
	t.Fatal("packed finite segment omitted its transport PRIV frame")
	return nil, 0
}

func generatedWindowAssertTransport(t *testing.T, got uint64, origin *big.Rat, elapsed int64) {
	t.Helper()
	clock := new(big.Rat).Add(new(big.Rat).Set(origin), new(big.Rat).SetInt64(elapsed))
	clock.Mul(clock, big.NewRat(90000, ticksPerSecond))
	whole := new(big.Int).Quo(clock.Num(), clock.Denom())
	whole.And(whole, new(big.Int).SetUint64((1<<33)-1))
	if got != whole.Uint64() {
		t.Fatalf("packed transport timestamp did not accumulate actual durations from its exact source origin: got=%d, want=%s, elapsed=%d", got, whole.String(), elapsed)
	}
}

func generatedWindowBoolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
