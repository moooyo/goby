//go:build linux

package transcode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestLiveActualContinuousPublicationPreservesCompleteAlignedMediaAndClock(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "continuous.ts")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=6", "-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=6",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-g", "25", "-keyint_min", "25", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "64000", "-t", "6", "-f", "mpegts", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"mpegts", "fmp4"} {
		for _, renditions := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s-%d", format, renditions), func(t *testing.T) {
				p := liveTestPlan()
				p.HLS.SegmentType, p.HLS.RenditionCount = format, renditions
				if format == "fmp4" {
					p.Container = "mp4"
				}
				p.SourceFormatStartKnown, p.SourceFormatStartTicks = info.FormatStartKnown, info.FormatStartTicks
				if renditions > 0 {
					p.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 90, VideoBitrate: 300000}
					p.HLS.Renditions[1] = HLSRendition{Width: 96, Height: 54, VideoBitrate: 160000}
				}
				input, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				feedDone := make(chan error, 1)
				go func() {
					file, err := os.Open(source)
					if err == nil {
						_, err = io.Copy(writer, file)
						_ = file.Close()
					}
					_ = writer.Close()
					feedDone <- err
				}()
				directory := t.TempDir()
				var mediaFiles [MaxHLSRenditions]bytes.Buffer
				var segmentEnds [MaxHLSRenditions][]int
				var initializations [MaxHLSRenditions][]byte
				var clockDeltas [MaxHLSRenditions]int64
				var firstStart [MaxHLSRenditions]int64
				var previousEnd int64
				published := 0
				publish := func(work context.Context, _ Spec, _ string, bundle LiveSegment) error {
					if bundle.Sequence != int64(published) || bundle.RenditionCount != liveRenditionCount(p) || bundle.DurationTicks <= 0 || bundle.DurationTicks > 2*ticksPerSecond {
						return fmt.Errorf("invalid actual live bundle: %+v", bundle)
					}
					if published > 0 && !liveTicksClose(bundle.StartTicks, previousEnd) {
						return fmt.Errorf("live source window omitted or duplicated time")
					}
					for rendition := 0; rendition < bundle.RenditionCount; rendition++ {
						actual := bundle.Renditions[rendition]
						if actual.Media == nil || actual.Size <= 0 || actual.Size > 8<<20 {
							return ErrQuota
						}
						post, err := MeasureHLSMuxClock(work, ffprobe, actual.Init, actual.Media, true)
						if err != nil {
							return err
						}
						delta := post - actual.PreMuxClockTicks
						if published == 0 {
							clockDeltas[rendition], firstStart[rendition] = delta, actual.PreMuxClockTicks
						} else if math.Abs(float64(delta-clockDeltas[rendition])) > 112 {
							return fmt.Errorf("container reset its clock between live segments")
						}
						if rendition > 0 && math.Abs(float64(delta-clockDeltas[0])) > 112 {
							return fmt.Errorf("renditions do not share an actual container clock")
						}
						if format == "fmp4" {
							if actual.Init == nil {
								return fmt.Errorf("fMP4 lacks independent initialization")
							}
							stat, err := actual.Init.Stat()
							if err != nil {
								return err
							}
							if stat.Size() <= 0 || stat.Size() > MaxProgressivePrefixBytes {
								return ErrQuota
							}
							data := make([]byte, stat.Size())
							if _, err := actual.Init.ReadAt(data, 0); err != nil {
								return err
							}
							if published == 0 {
								initializations[rendition] = data
								mediaFiles[rendition].Write(data)
							} else if !bytes.Equal(data, initializations[rendition]) {
								return fmt.Errorf("live initialization changed")
							}
						}
						if int64(mediaFiles[rendition].Len())+actual.Size > 16<<20 {
							return ErrQuota
						}
						if _, err := io.Copy(&mediaFiles[rendition], io.NewSectionReader(actual.Media, 0, actual.Size)); err != nil {
							return err
						}
						segmentEnds[rendition] = append(segmentEnds[rendition], mediaFiles[rendition].Len())
					}
					previousEnd = bundle.StartTicks + bundle.DurationTicks
					published++
					return nil
				}
				runCtx := withLiveRuntime(ctx, liveRuntime{inputs: StreamInputs{Media: input}, maxBytes: 32 << 20, timeout: 15 * time.Second, publish: publish})
				result, err := Run(runCtx, ffmpeg, directory, input, p, 1, nil)
				_ = input.Close()
				feedErr := <-feedDone
				if err != nil {
					t.Fatalf("continuous live producer: %v: %s", err, result.StderrTail)
				}
				if feedErr != nil {
					t.Fatalf("continuous source feed: %v", feedErr)
				}
				if published < 5 {
					t.Fatalf("closed-segment journal missed actual segments: %d", published)
				}
				for rendition := 0; rendition < liveRenditionCount(p); rendition++ {
					// HLS clients open each advertised segment independently. Keep
					// the concatenated continuity check as well: a new TS muxer must
					// announce its counter reset rather than look like packet loss.
					start, pieceFrames := len(initializations[rendition]), 0
					for sequence, end := range segmentEnds[rendition] {
						part := mediaFiles[rendition].Bytes()[start:end]
						if format == "fmp4" {
							part = append(bytes.Clone(initializations[rendition]), part...)
						}
						path := filepath.Join(t.TempDir(), fmt.Sprintf("segment-%06d.bin", sequence))
						if err := os.WriteFile(path, part, 0600); err != nil {
							t.Fatal(err)
						}
						strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
						pieceFrames += len(probeProgressiveVideoFrames(t, ctx, ffprobe, path).video)
						start = end
					}
					if pieceFrames != 150 {
						t.Fatalf("independent HLS segments lost decoded frames: %d", pieceFrames)
					}
					output := filepath.Join(t.TempDir(), "published.bin")
					if err := os.WriteFile(output, mediaFiles[rendition].Bytes(), 0600); err != nil {
						t.Fatal(err)
					}
					facts := probeProgressiveVideoFrames(t, ctx, ffprobe, output)
					strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
					if len(facts.video) != 150 || len(facts.audio) == 0 {
						t.Fatalf("continuous output dropped or repeated media: video=%d audio=%d", len(facts.video), len(facts.audio))
					}
					first := float64(firstStart[rendition]+clockDeltas[rendition]) / float64(ticksPerSecond)
					for index, frame := range facts.video {
						if math.Abs(frame.time(t)-(first+float64(index)/25)) > .002 {
							t.Fatalf("live packet clock changed at decoded frame %d: %.6f", index, frame.time(t))
						}
					}
				}
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 0 {
					t.Fatalf("closed scratch accumulated after source EOF: %v %v", entries, err)
				}
			})
		}
	}
}

func TestLiveActualTwentyFourFPSKeepsThreeSecondCuts(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "twenty-four.ts")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=9", "-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=9",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "64000", "-t", "9", "-muxrate", "600000", "-f", "mpegts", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"mpegts", "fmp4"} {
		t.Run(format, func(t *testing.T) {
			p := liveTestPlan()
			// Preserve the native encoder clock, as a dynamic profile without a
			// frame-rate override does. A first PTS of 25/24 rounds upward when
			// segment.c converts its initial reference clock to microseconds.
			p.FrameRate, p.SegmentSeconds, p.HLS.SegmentType = 0, 3, format
			p.SourceFormatStartKnown, p.SourceFormatStartTicks = info.FormatStartKnown, info.FormatStartTicks
			if format == "fmp4" {
				p.Container = "mp4"
			}
			input, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			feedDone := make(chan error, 1)
			go func() {
				file, err := os.Open(source)
				if err == nil {
					_, err = io.Copy(writer, file)
					_ = file.Close()
				}
				_ = writer.Close()
				feedDone <- err
			}()
			var segments [][]byte
			var starts []int64
			var firstDelta, previousEnd int64
			publish := func(work context.Context, _ Spec, _ string, bundle LiveSegment) error {
				if bundle.Sequence != int64(len(segments)) || bundle.RenditionCount != 1 ||
					math.Abs(float64(bundle.DurationTicks-3*ticksPerSecond)) > 112 {
					return fmt.Errorf("rational frame clock skipped the requested three-second cut: sequence=%d duration=%d", bundle.Sequence, bundle.DurationTicks)
				}
				if len(segments) > 0 && !liveTicksClose(bundle.StartTicks, previousEnd) {
					return fmt.Errorf("three-second segments changed the continuous source clock")
				}
				actual := bundle.Renditions[0]
				post, err := MeasureHLSMuxClock(work, ffprobe, actual.Init, actual.Media, true)
				if err != nil {
					return err
				}
				delta := post - actual.PreMuxClockTicks
				if len(segments) == 0 {
					firstDelta = delta
				} else if math.Abs(float64(delta-firstDelta)) > 112 {
					return fmt.Errorf("three-second segments reset their container clock")
				}
				var data bytes.Buffer
				if actual.Init != nil {
					stat, err := actual.Init.Stat()
					if err != nil {
						return err
					}
					if stat.Size() <= 0 || stat.Size() > MaxProgressivePrefixBytes {
						return ErrQuota
					}
					if _, err := io.Copy(&data, io.NewSectionReader(actual.Init, 0, stat.Size())); err != nil {
						return err
					}
				}
				if actual.Size <= 0 || actual.Size > 8<<20 {
					return ErrQuota
				}
				if _, err := io.Copy(&data, io.NewSectionReader(actual.Media, 0, actual.Size)); err != nil {
					return err
				}
				segments = append(segments, data.Bytes())
				starts = append(starts, post)
				previousEnd = bundle.StartTicks + bundle.DurationTicks
				return nil
			}
			directory := t.TempDir()
			runCtx := withLiveRuntime(ctx, liveRuntime{inputs: StreamInputs{Media: input}, maxBytes: 32 << 20, timeout: 15 * time.Second, publish: publish})
			result, err := Run(runCtx, ffmpeg, directory, input, p, 1, nil)
			_ = input.Close()
			feedErr := <-feedDone
			if err != nil {
				t.Fatalf("24 fps live producer: %v: %s", err, result.StderrTail)
			}
			if feedErr != nil {
				t.Fatalf("24 fps source feed: %v", feedErr)
			}
			if len(segments) != 3 {
				t.Fatalf("nine seconds at 24 fps did not produce three complete segments: %d", len(segments))
			}
			for sequence, data := range segments {
				path := filepath.Join(t.TempDir(), fmt.Sprintf("segment-%06d.bin", sequence))
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
				facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
				if len(facts.video) != 72 || len(facts.audio) == 0 {
					t.Fatalf("three-second fragment lost or repeated frames: video=%d audio=%d", len(facts.video), len(facts.audio))
				}
				first := float64(starts[sequence]) / float64(ticksPerSecond)
				for index, frame := range facts.video {
					if math.Abs(frame.time(t)-(first+float64(index)/24)) > .002 {
						t.Fatalf("24 fps decoded clock changed at sequence=%d frame=%d", sequence, index)
					}
				}
			}
			if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
				t.Fatalf("24 fps producer retained closed scratch files: %v %v", entries, err)
			}
		})
	}
}
