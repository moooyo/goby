//go:build linux

package transcode

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestHLSOggSampleSeekingPreservesGlobalTimelineAndContent(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual Ogg HLS verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, fixture := range []struct {
		name, codec, duration string
		rate                  int
		nonzeroOrigin         bool
		largePage             bool
		fractionalWindow      bool
	}{
		{name: "flac_page_seek", codec: "flac", duration: "5.013", rate: 48000},
		{name: "vorbis_later_segment", codec: "libvorbis", duration: "4.013", rate: 44100, nonzeroOrigin: true, largePage: true},
		{name: "vorbis_overlap_origin", codec: "libvorbis", duration: "0.1", rate: 44100, nonzeroOrigin: true, fractionalWindow: true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "marked-source.ogg")
			// The changing frequency identifies source position. A single sine
			// wave or a duration-only assertion can conceal an Ogg page seek that
			// produces the requested sample count from the wrong source position.
			args := []string{"-v", "error", "-nostdin", "-f", "lavfi", "-i",
				"aevalsrc=0.15*sin(2*PI*(173*t+511*t*t)):s=" + strconv.Itoa(fixture.rate) + ":d=" + fixture.duration,
				"-map", "0:a:0", "-c:a", fixture.codec, "-threads:a", "1"}
			if fixture.codec == "libvorbis" {
				args = append(args, "-b:a", "64000")
			}
			if fixture.largePage {
				args = append(args, "-page_duration", "10000000")
			}
			args = append(args, "-f", "ogg", source)
			runTimelineMediaCommand(t, ctx, ffmpeg, args...)
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if info.ProbeVersion < 4 || !info.AudioDurationExact || len(info.Streams) != 1 ||
				info.Streams[0].AudioTiming == nil || !info.Streams[0].AudioTiming.Exact || info.Streams[0].AudioTiming.StartTicks != 0 {
				t.Fatalf("Ogg source lacks exact presentation facts: %+v", info)
			}
			stream := info.Streams[0]
			if stream.SampleRate != fixture.rate || stream.Channels != 1 {
				t.Fatalf("unexpected source format: %+v", stream)
			}
			if fixture.nonzeroOrigin && info.PresentationOriginTicks <= 0 {
				t.Fatal("Vorbis fixture did not exercise a nonzero presentation origin")
			}
			reference := audioOggTimelinePCM(t, ctx, ffmpeg, source, stream.SampleRate)
			if int64(len(reference)) != stream.AudioTiming.SampleCount {
				t.Fatalf("exact facts differ from independent full decode: %d != %d", stream.AudioTiming.SampleCount, len(reference))
			}
			timeline, err := BuildAudioTimeline(info.DurationTicks, 1, AudioTimelineOptions{Codec: "aac", SampleRate: stream.SampleRate})
			if err != nil {
				t.Fatal(err)
			}
			plan := Plan{Container: "ts", VideoStreamIndex: -1, AudioStreamIndex: stream.Index, AudioCodec: "aac",
				AudioSampleRate: stream.SampleRate, AudioChannels: 1, AudioBitrate: 96000, AudioSampleSeek: true,
				AudioSourceSampleRate: stream.SampleRate, AudioSourceSampleCount: stream.AudioTiming.SampleCount,
				DurationTicks: info.DurationTicks, SegmentSeconds: 1, SegmentMode: "vod"}
			windows := [][2]int{{0, len(timeline.Segments) - 1}}
			if fixture.name == "flac_page_seek" {
				if len(timeline.Segments) != 5 {
					t.Fatalf("unexpected FLAC timeline: %+v", timeline)
				}
				windows = append(windows, [2]int{3, 4}, [2]int{1, 2})
			} else if fixture.name == "vorbis_later_segment" {
				if len(timeline.Segments) != 4 {
					t.Fatalf("unexpected Vorbis timeline: %+v", timeline)
				}
				windows = append(windows, [2]int{3, 3})
			}
			for _, window := range windows {
				t.Run(fmt.Sprintf("segments_%d_through_%d", window[0], window[1]), func(t *testing.T) {
					cuts, err := timeline.BoundaryTicks(window[0], window[1])
					if err != nil {
						t.Fatal(err)
					}
					values := make([]string, len(cuts))
					for index, cut := range cuts {
						values[index] = strconv.FormatInt(cut, 10)
					}
					producer := plan
					producer.StartTicks = timeline.Segments[window[0]].StartTicks
					last := timeline.Segments[window[1]]
					producer.EndTicks = last.StartTicks + last.DurationTicks
					producer.SegmentStartNumber = window[0]
					producer.SegmentTimes = strings.Join(values, ",")
					audioOggTimelineAssertProducer(t, ctx, ffmpeg, ffprobe, input, producer, reference, window[1]-window[0]+1, fixture.fractionalWindow)
				})
			}
			if fixture.fractionalWindow {
				t.Run("fractional_source_window", func(t *testing.T) {
					// A 100 ms source has no normal one-second HLS boundary. Test
					// the worker's explicit source-time window independently from
					// the canonical full-VOD manifest above. This reproduces the
					// 128-sample content shift of input seeking on short Vorbis.
					producer := plan
					producer.StartTicks, producer.EndTicks = 234_567, info.DurationTicks
					producer.SegmentStartNumber = 1
					audioOggTimelineAssertProducer(t, ctx, ffmpeg, ffprobe, input, producer, reference, 1, false)
				})
			}
		})
	}
}

func audioOggTimelineAssertProducer(t *testing.T, ctx context.Context, ffmpeg, ffprobe string, input *os.File, plan Plan, reference []int16, expectedSegments int, codecMatchedReference bool) {
	t.Helper()
	directory := t.TempDir()
	result, err := Run(ctx, ffmpeg, directory, input, plan, 1, nil)
	if err != nil {
		t.Fatalf("Ogg HLS source window [%d,%d): %v: %s", plan.StartTicks, plan.EndTicks, err, result.StderrTail)
	}
	data, err := os.ReadFile(filepath.Join(directory, "main.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := ParseMediaPlaylist(data)
	if err != nil || !list.Ended || list.Type != "VOD" || len(list.Segments) != expectedSegments || list.Sequence != int64(plan.SegmentStartNumber) {
		t.Fatalf("completed source-global playlist: %+v, %v", list, err)
	}
	var total int64
	var firstPCM []int16
	for index, segment := range list.Segments {
		if segment.Number != int64(plan.SegmentStartNumber+index) || segment.Name != fmt.Sprintf("segment-%06d.ts", segment.Number) {
			t.Fatalf("source-global numbering changed: %+v", segment)
		}
		pcm := audioOggTimelinePCM(t, ctx, ffmpeg, filepath.Join(directory, segment.Name), plan.AudioSampleRate)
		if len(pcm) == 0 {
			t.Fatalf("empty decoded segment: %s", segment.Name)
		}
		if index == 0 {
			firstPCM = pcm
		}
		total += int64(len(pcm))
	}
	start := audioOggTimelineCeilSamples(plan.StartTicks, plan.AudioSourceSampleRate)
	end := min(int64(len(reference)), audioOggTimelineCeilSamples(plan.EndTicks, plan.AudioSourceSampleRate))
	wantFrames := (end - start + 1023) / 1024
	if total != wantFrames*1024 {
		t.Fatalf("AAC output did not preserve the complete bounded sample window: got %d, want %d", total, wantFrames*1024)
	}
	// Ignore the encoder's initial overlap region, then compare actual content
	// with a slice of an independent, complete source decode. AAC padding is
	// excluded, and no expected content is obtained through the demuxer's seek.
	offset, length := int64(4096), int64(4096)
	if end-start < offset+length {
		offset, length = 2048, 512
	}
	if start+offset+length > end || offset+length > int64(len(firstPCM)) {
		t.Fatal("marker comparison window does not fit the source or first output segment")
	}
	expectedPCM, expectedStart := reference, start
	if codecMatchedReference {
		if start != 0 || end != int64(len(reference)) || expectedSegments != 1 {
			t.Fatal("the codec-matched reference is restricted to the short complete source")
		}
		// AAC's local distortion on this 100 ms chirp exceeds a PCM MSE
		// threshold even without seeking: direct TS and independently encoded
		// raw f32 PCM produce byte-identical decoded output. Match the codec
		// for this complete-source case only. The fractional window and all
		// longer producers still compare against the unencoded source slice.
		expectedPCM = audioOggTimelineAACReference(t, ctx, ffmpeg, input, plan)
		expectedStart = 0
		if int64(len(expectedPCM)) != total {
			t.Fatalf("independent AAC reference has a different sample count: %d != %d", len(expectedPCM), total)
		}
	}
	var squareError, power float64
	for index := range int(length) {
		actual := float64(firstPCM[int(offset)+index])
		wanted := float64(expectedPCM[int(expectedStart+offset)+index])
		squareError += (actual - wanted) * (actual - wanted)
		power += wanted * wanted
	}
	if power == 0 || math.IsNaN(squareError) || squareError/power >= 0.02 {
		t.Fatalf("Ogg seek produced the wrong source content: normalized MSE=%g", squareError/power)
	}
	// MPEG-TS quantizes source-global PTS to 90 kHz; allow one transport tick
	// plus ffprobe's final decimal rounding, without allowing an audio-frame shift.
	firstPath := filepath.Join(directory, list.Segments[0].Name)
	probe, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-read_intervals", "%+#1", "-select_streams", "a:0",
		"-show_entries", "packet=pts_time:packet_side_data=", "-of", "json", firstPath).Output()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Packets []struct {
			PTS string `json:"pts_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(probe, &document); err != nil || len(document.Packets) != 1 {
		t.Fatalf("first transport timestamp: %v: %s", err, probe)
	}
	pts, known := timelineTimestamp(document.Packets[0].PTS)
	difference := pts - (plan.StartTicks + ticksPerSecond)
	if !known || difference < -122 || difference > 122 {
		t.Fatalf("transport lost the source-global origin: PTS=%d, source start=%d", pts, plan.StartTicks)
	}
}

func audioOggTimelineAACReference(t *testing.T, ctx context.Context, ffmpeg string, input *os.File, plan Plan) []int16 {
	t.Helper()
	// Decode the complete source without seeking or retaining its timestamps.
	// Keep its float samples: quantizing to s16 before the reference encoding
	// can change AAC's mode decisions on a very short signal.
	decoder := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-xerror", "-err_detect", "explode", "-threads", "1",
		"-i", "/proc/self/fd/3", "-map", "0:"+strconv.Itoa(plan.AudioStreamIndex), "-c:a", "pcm_f32le", "-f", "f32le", "pipe:1")
	decoder.ExtraFiles = []*os.File{input}
	var stderr strings.Builder
	decoder.Stderr = &stderr
	data, err := decoder.Output()
	if err != nil || int64(len(data)) != plan.AudioSourceSampleCount*4 {
		t.Fatalf("decode independent float reference: samples=%d: %v: %s", len(data)/4, err, stderr.String())
	}
	directory := t.TempDir()
	pcmPath := filepath.Join(directory, "complete-source.f32le")
	if err := os.WriteFile(pcmPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	aacPath := filepath.Join(directory, "independent.aac")
	// This independent raw-PCM encoding has no Ogg input, timestamp origin,
	// demuxer seek, sample-trim filter, or HLS segment muxer. Drop only the AAC
	// encoder's negative-PTS priming packet, as the HLS transport does.
	runTimelineMediaCommand(t, ctx, ffmpeg, "-v", "error", "-nostdin", "-f", "f32le", "-ar", strconv.Itoa(plan.AudioSourceSampleRate), "-ac", "1", "-i", pcmPath,
		"-map", "0:a:0", "-c:a", "aac", "-threads:a", "1", "-b:a", strconv.FormatInt(plan.AudioBitrate, 10), "-ar", strconv.Itoa(plan.AudioSampleRate),
		"-ac", "1", "-profile:a", "aac_low", "-bsf:a", "noise=drop='lt(pts,0)'", "-f", "adts", aacPath)
	return audioOggTimelinePCM(t, ctx, ffmpeg, aacPath, plan.AudioSampleRate)
}

func audioOggTimelineCeilSamples(ticks int64, rate int) int64 {
	return ticks/ticksPerSecond*int64(rate) + (ticks%ticksPerSecond*int64(rate)+ticksPerSecond-1)/ticksPerSecond
}

func audioOggTimelinePCM(t *testing.T, ctx context.Context, ffmpeg, path string, rate int) []int16 {
	t.Helper()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-xerror", "-err_detect", "explode", "-threads", "1",
		"-i", path, "-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(rate), "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil || len(data)%2 != 0 {
		t.Fatalf("decode Ogg/HLS marker audio: %v: %s", err, stderr.String())
	}
	pcm := make([]int16, len(data)/2)
	for index := range pcm {
		pcm[index] = int16(binary.LittleEndian.Uint16(data[index*2:]))
	}
	return pcm
}
