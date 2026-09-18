//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestActualAdaptiveHLSHasDistinctDimensionsAndAlignedKeyframes(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=6", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=6", "-c:v", "libx264", "-threads:v", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", source)
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create fixture: %v: %s", err, data)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	p := adaptiveHLSPlan()
	p.DurationTicks, p.FrameRate, p.SegmentSeconds = 6*ticksPerSecond, 24, 2
	directory := t.TempDir()
	result, err := Run(ctx, ffmpeg, directory, input, p, 1, nil)
	if err != nil {
		t.Fatalf("adaptive producer: %v: %s", err, result.StderrTail)
	}
	var reference []float64
	for index := 0; index < p.HLS.RenditionCount; index++ {
		data, err := os.ReadFile(filepath.Join(directory, HLSPlaylistName(index, p.HLS.RenditionCount)))
		if err != nil {
			t.Fatal(err)
		}
		playlist, err := ParseMediaPlaylist(data)
		if err != nil || !playlist.Ended || !playlist.Independent || playlist.InitName == "" || len(playlist.Segments) < 3 {
			t.Fatalf("rendition playlist: %+v: %v", playlist, err)
		}
		combined, err := os.ReadFile(filepath.Join(directory, playlist.InitName))
		if err != nil {
			t.Fatal(err)
		}
		for _, segment := range playlist.Segments {
			fragment, err := os.ReadFile(filepath.Join(directory, segment.Name))
			if err != nil {
				t.Fatal(err)
			}
			combined = append(combined, fragment...)
		}
		file := filepath.Join(t.TempDir(), "rendition.mp4")
		if err := os.WriteFile(file, combined, 0600); err != nil {
			t.Fatal(err)
		}
		probe := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height:packet=pts_time,flags", "-of", "json", file)
		output, err := probe.Output()
		if err != nil {
			t.Fatal(err)
		}
		var facts struct {
			Streams []struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"streams"`
			Packets []struct {
				PTS   string `json:"pts_time"`
				Flags string `json:"flags"`
			} `json:"packets"`
		}
		if err := json.Unmarshal(output, &facts); err != nil {
			t.Fatal(err)
		}
		if len(facts.Streams) != 1 || facts.Streams[0].Width != p.HLS.Renditions[index].Width || facts.Streams[0].Height != p.HLS.Renditions[index].Height {
			t.Fatalf("variant was not physically resized: %s", output)
		}
		var keys []float64
		for _, packet := range facts.Packets {
			if len(packet.Flags) > 0 && packet.Flags[0] == 'K' {
				value, err := strconv.ParseFloat(packet.PTS, 64)
				if err != nil {
					t.Fatal(err)
				}
				keys = append(keys, value)
			}
		}
		if index == 0 {
			reference = keys
		} else {
			if len(keys) != len(reference) {
				t.Fatal("rendition keyframe counts diverged")
			}
			for key := range keys {
				if math.Abs(keys[key]-reference[key]) > 0.00001 {
					t.Fatal("rendition keyframes are not aligned")
				}
			}
		}
		decode := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", file, "-f", "null", "-")
		if output, err := decode.CombinedOutput(); err != nil || len(output) != 0 {
			t.Fatalf("generated rendition cannot decode: %v: %s", err, output)
		}
	}
}

func TestActualPackedHLSAudioCarriesTransportTimestampAndDecodes(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required")
	}
	for _, codec := range []string{"aac", "mp3"} {
		t.Run(codec, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			source := filepath.Join(t.TempDir(), "source.wav")
			command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=5", "-c:a", "pcm_s16le", source)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("create source: %v: %s", err, output)
			}
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			plan := Plan{Container: codec, HLS: HLSPlan{SegmentType: "packed"}, VideoStreamIndex: -1, AudioStreamIndex: 0, AudioCodec: codec, AudioBitrate: 128000, AudioChannels: 1, AudioSampleRate: 48000, DurationTicks: 5 * ticksPerSecond, SegmentSeconds: 2}
			directory := t.TempDir()
			result, err := Run(ctx, ffmpeg, directory, input, plan, 1, nil)
			if err != nil {
				t.Fatalf("packed producer: %v: %s", err, result.StderrTail)
			}
			data, err := os.ReadFile(filepath.Join(directory, "main.m3u8"))
			if err != nil {
				t.Fatal(err)
			}
			playlist, err := ParseMediaPlaylist(data)
			if err != nil || !playlist.Ended || len(playlist.Segments) < 2 || playlist.InitName != "" {
				t.Fatalf("packed playlist: %+v: %v", playlist, err)
			}
			for _, segment := range playlist.Segments {
				file := filepath.Join(directory, segment.Name)
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(string(data), "ID3") || !strings.Contains(string(data[:min(len(data), 100)]), packedTimestampOwner) {
					t.Fatal("packed segment omitted the transport timestamp")
				}
				decode := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", file, "-f", "null", "-")
				if output, err := decode.CombinedOutput(); err != nil || len(output) != 0 {
					t.Fatalf("packed segment cannot decode: %v: %s", err, output)
				}
			}
		})
	}
}
