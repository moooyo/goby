//go:build linux

package transcode

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"testing"
)

// This matrix calibrates actual stats_mux_pre and container packets separately.
// It does not infer either epoch from the command's nominal offset, nor treat
// decoding a finite interval as a complete-source EOF certificate.
func TestGeneratedWindowNativeClockActualEmissionCalibration(t *testing.T) {
	for _, rate := range []int{24, 25, 30, 60} {
		for _, origin := range []int{0, 2} {
			t.Run(fmt.Sprintf("rate%d_origin%d", rate, origin), func(t *testing.T) {
				ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
				sourcePath := generatedNativeClockMediaSource(t, ctx, ffmpeg, rate, origin)
				source, before := generatedClosureMediaOpenSource(t, sourcePath)
				certificate, err := MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
				if err != nil || certificate.DurationTicks != 100*ticksPerSecond || !certificate.DurationTicksExact ||
					certificate.Origin != (GeneratedRational{Num: int64(origin), Den: 1}) || certificate.SampleCount != int64(100*rate) {
					t.Fatalf("calibration source has no independent exact native endpoint: %+v, %v", certificate, err)
				}
				for _, start := range []int64{0, 6, 90} {
					for _, count := range []int{0, 2, 4} {
						t.Run(fmt.Sprintf("start%d_renditions%d", start, max(1, count)), func(t *testing.T) {
							plan := generatedNativeClockTestPlan(count)
							plan.FrameRate, plan.DurationTicks, plan.StartTicks, plan.SegmentSeconds = float64(rate), 100*ticksPerSecond, start*ticksPerSecond, 6
							plan.HLS.Window.EndTicks, plan.HLS.Window.StartNumber = (start+6)*ticksPerSecond, int(start/6)
							directory, result, clocks := generatedClosureMediaRun(t, ctx, ffmpeg, source, plan)
							if result.ExitCode != 0 || result.WindowInputEvidence == nil {
								t.Fatal("calibration did not retain normally completed actual input evidence")
							}
							coverage, err := MeasureGeneratedSourceRange(ctx, ffprobe, source, plan, int64(origin)*ticksPerSecond)
							if err != nil || coverage.FrameCount != int64(6*rate) {
								t.Fatalf("source calibration did not preserve its independently observed range: %+v, %v", coverage, err)
							}
							var outputs [MaxHLSRenditions]GeneratedSegmentBounds
							var lists [MaxHLSRenditions]MediaPlaylist
							for index := 0; index < max(1, count); index++ {
								clock := clocks[index]
								if clock.Rendition != index || clock.TimeBaseNumerator <= 0 || clock.TimeBaseDenominator <= 0 || clock.PTS != 0 {
									t.Fatalf("actual premux clock differs from the unique native-v1 local epoch: %+v", clock)
								}
								list := generatedWindowReadList(t, directory, HLSPlaylistName(index, count), plan.HLS.Window.StartNumber)
								bounds := generatedClosureMediaMeasure(t, ctx, ffprobe, directory, plan, list)
								video := bounds.Video
								first := generatedClockSeconds(video.FirstPTS, video.TimeBase.Num, video.TimeBase.Den)
								end := generatedClockSeconds(video.EndPTS, video.TimeBase.Num, video.TimeBase.Den)
								if first.Cmp(new(big.Rat).SetInt64(start)) != 0 || end.Cmp(new(big.Rat).SetInt64(start+6)) != 0 {
									t.Fatalf("actual TS clock is local, biased or rounded: first=%s end=%s requested=[%d,%d)", first.RatString(), end.RatString(), start, start+6)
								}
								outputs[index], lists[index] = bounds, list
								width, height := plan.Width, plan.Height
								if count != 0 {
									width, height = plan.HLS.Renditions[index].Width, plan.HLS.Renditions[index].Height
								}
								generatedNativeClockAssertDecode(t, ctx, ffmpeg, filepath.Join(directory, list.Segments[0].Name), width, height, rate, start)
								t.Logf("native clock actual calibration: source_origin=%d rate=%d start=%d rendition=%d premux=%+v first=%s end=%s packets=%d",
									origin, rate, start, index, clock, first.RatString(), end.RatString(), video.PacketCount)
							}
							closure, err := ValidateGeneratedWindowClosure(plan, coverage, *result.WindowInputEvidence, clocks, outputs, lists)
							if err != nil || closure.NativeClockVersion != GeneratedWindowNativeClockV1 || closure.RenditionCount != max(1, count) {
								t.Fatalf("actual native source emission did not establish complete per-rendition closure: %+v, %v", closure, err)
							}
							if !transcodeSourceUnchanged(source, before) {
								t.Fatal("source changed across native production and independent observations")
							}
						})
					}
				}
			})
		}
	}
}

func generatedNativeClockMediaSource(t *testing.T, ctx context.Context, ffmpeg string, rate, origin int) string {
	t.Helper()
	name := filepath.Join(t.TempDir(), "native-clock-source.mp4")
	arguments := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=red:s=160x96:r=%d:d=100", rate),
		"-vf", "drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='gte(t,90)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,93)'",
		"-an", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-crf", "0", "-pix_fmt", "yuv420p",
		"-bf", "0", "-g", strconv.Itoa(rate), "-keyint_min", strconv.Itoa(rate), "-sc_threshold", "0",
		"-output_ts_offset", strconv.Itoa(origin)}
	if origin == 0 {
		arguments = append(arguments, "-use_editlist", "0")
	}
	arguments = append(arguments, "-movflags", "+faststart", name)
	generatedWindowMediaCommand(t, ctx, ffmpeg, arguments...)
	return name
}

func generatedNativeClockAssertDecode(t *testing.T, ctx context.Context, ffmpeg, name string, width, height, rate int, start int64) {
	t.Helper()
	data := generatedWindowMediaCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-threads", "1",
		"-i", name, "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	frameBytes, frames := width*height*3, 6*rate
	if frameBytes <= 0 || len(data) != frames*frameBytes {
		t.Fatalf("native TS did not independently decode exactly its requested frames: bytes=%d expected_frames=%d dimensions=%dx%d", len(data), frames, width, height)
	}
	for index := 0; index < frames; index++ {
		frame := data[index*frameBytes : (index+1)*frameBytes]
		var channels [3]int64
		for pixel := 0; pixel < len(frame); pixel += 3 {
			for channel := 0; channel < 3; channel++ {
				channels[channel] += int64(frame[pixel+channel])
			}
		}
		wanted := 0
		if start == 90 {
			wanted = 1
			if index >= 3*rate {
				wanted = 2
			}
		}
		for channel, total := range channels {
			average := total / int64(width*height)
			if channel == wanted && average < 170 || channel != wanted && average > 60 {
				t.Fatalf("native TS decoded content outside the observed source interval: start=%d frame=%d channel=%d average=%d", start, index, channel, average)
			}
		}
	}
}
