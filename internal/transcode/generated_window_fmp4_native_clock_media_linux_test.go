//go:build linux

package transcode

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This tests raw track-scale association on actual generic fMP4 windows.
// Version zero intentionally retains its local mux epoch. Matching packet and
// tfdt clocks here grants no source-global emission or client-playback contract.
func TestGeneratedFMP4NativeClockActualPacketAssociation(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual native MP4 clock observation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	directory := t.TempDir()
	sourceName := filepath.Join(directory, "source.mp4")
	_, stderr, _, _, err := generatedFMP4DiagnosticCommand(ctx, ffmpeg, directory, nil, nil,
		[]string{"-hide_banner", "-nostdin", "-v", "error", "-n", "-f", "lavfi", "-i", "testsrc2=s=160x96:r=24:d=100",
			"-an", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-g", "24", "-preset", "ultrafast", "-output_ts_offset", "2", "-movflags", "+faststart", sourceName})
	if err != nil || len(stderr) != 0 {
		t.Fatal("controlled source generation failed")
	}
	source, before := generatedClosureMediaOpenSource(t, sourceName)
	for _, adaptive := range []bool{false, true} {
		name := "single"
		if adaptive {
			name = "two_renditions"
		}
		t.Run(name, func(t *testing.T) {
			plan := generatedClosureMediaPlan("fmp4", 100, 90, 96, adaptive)
			output, _, _ := generatedClosureMediaRun(t, ctx, ffmpeg, source, plan)
			for variant := 0; variant < max(1, plan.HLS.RenditionCount); variant++ {
				list := generatedWindowReadList(t, output, HLSPlaylistName(variant, plan.HLS.RenditionCount), plan.HLS.Window.StartNumber)
				initialization, err := os.Open(filepath.Join(output, list.InitName))
				if err != nil {
					t.Fatal("actual initialization could not be held")
				}
				segment, err := os.Open(filepath.Join(output, list.Segments[0].Name))
				if err != nil {
					_ = initialization.Close()
					t.Fatal("actual media could not be held")
				}
				func() {
					defer initialization.Close()
					defer segment.Close()
					initStamp, initErr := initialization.Stat()
					mediaStamp, mediaErr := segment.Stat()
					if initErr != nil || mediaErr != nil {
						t.Fatal("actual held-byte identity could not be observed")
					}
					native, err := MeasureGeneratedFMP4NativeClock(ctx, plan, initialization, segment)
					if err != nil {
						t.Fatal("actual raw native MP4 clock could not be read")
					}
					packets, err := MeasureGeneratedWindowSegment(ctx, ffprobe, plan, initialization, segment)
					if err != nil || !packets.Video.Present || packets.Audio.Present || packets.Video.PacketCount != 144 ||
						!packets.Video.PresentationDecodeAligned || packets.Video.MinPacketDuration <= 0 || packets.Video.MinPacketDuration != packets.Video.MaxPacketDuration ||
						native.TotalSamples != packets.Video.PacketCount || native.InitializationSHA256 != packets.InitializationSHA256 || native.SegmentSHA256 != packets.SegmentSHA256 ||
						generatedClockSeconds(1, 1, native.MediaTimeScale).Cmp(generatedClockSeconds(1, packets.Video.TimeBase.Num, packets.Video.TimeBase.Den)) != 0 {
						t.Fatal("native headers and actual packets lost their common track scale or whole-byte binding")
					}
					var samples int64
					for _, fragment := range native.Fragments[:native.FragmentCount] {
						if fragment.DecodeUnits > math.MaxInt64 || fragment.TrackID != native.TrackID ||
							int64(fragment.DecodeUnits) != packets.Video.FirstDTS+samples*packets.Video.MinPacketDuration {
							t.Fatal("a later native fragment borrowed another track or diverged from actual packet decode order")
						}
						samples += fragment.SampleCount
					}
					if !transcodeSourceUnchanged(initialization, initStamp) || !transcodeSourceUnchanged(segment, mediaStamp) || !transcodeSourceUnchanged(source, before) {
						t.Fatal("held bytes changed across raw metadata and independent packet observations")
					}
					t.Logf("actual raw fMP4 clock: rendition=%d track=%d scale=%d fragments=%d samples=%d edited=%t source_global_qualified=false",
						variant, native.TrackID, native.MediaTimeScale, native.FragmentCount, native.TotalSamples, native.EditPresent)
				}()
			}
		})
	}
}
