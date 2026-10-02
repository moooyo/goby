//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This exercises the actual production Version2 BuildArgs/Run path. No argument
// decorator, clock rewrite or nominal packet envelope supplies emission proof.
func TestGeneratedFMP4EmissionActualProductionCommand(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual version-two emission")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, rate := range []int{24, 25} {
		for _, origin := range []int{0, 2} {
			sourceDirectory := t.TempDir()
			sourceName := filepath.Join(sourceDirectory, "source.mp4")
			arguments := []string{"-hide_banner", "-nostdin", "-v", "error", "-n", "-filter_threads", "1", "-f", "lavfi", "-i",
				fmt.Sprintf("color=c=red:s=160x96:r=%d:d=100", rate), "-vf",
				"drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='gte(t,90)',drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='gte(t,93)'",
				"-an", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-g", strconv.Itoa(rate), "-preset", "ultrafast", "-crf", "0", "-pix_fmt", "yuv420p", "-output_ts_offset", strconv.Itoa(origin)}
			if origin == 0 {
				arguments = append(arguments, "-use_editlist", "0")
			}
			arguments = append(arguments, "-movflags", "+faststart", sourceName)
			_, stderr, _, _, err := generatedFMP4DiagnosticCommand(ctx, ffmpeg, sourceDirectory, nil, nil, arguments)
			if err != nil || len(stderr) != 0 {
				t.Fatal("controlled version-two source generation failed")
			}
			source, before := generatedClosureMediaOpenSource(t, sourceName)
			certificate, err := MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
			if err != nil || certificate.DurationTicks != 100*ticksPerSecond || certificate.SampleCount != int64(100*rate) ||
				certificate.Origin != (GeneratedRational{Num: int64(origin), Den: 1}) {
				t.Fatal("actual production source lacks its independently observed endpoint")
			}
			for _, start := range []int64{0, 6, 90, 96} {
				for _, adaptive := range []bool{false, true} {
					count := 0
					if adaptive {
						count = 2
					}
					t.Run(fmt.Sprintf("r%d_o%d_s%d_n%d", rate, origin, start, max(1, count)), func(t *testing.T) {
						end := min(start+6, int64(100))
						plan := generatedClosureMediaPlan("fmp4", 100, start, end, adaptive)
						plan.FrameRate, plan.HLS.Window.StartNumber, plan.HLS.Window.NativeClockVersion = float64(rate), int(start/6), GeneratedWindowNativeClockV2
						directory, result, mux := generatedClosureMediaRun(t, ctx, ffmpeg, source, plan)
						if result.ExitCode != 0 || result.WindowInputEvidence == nil {
							t.Fatal("actual version-two producer did not normally complete with input evidence")
						}
						coverage, err := MeasureGeneratedSourceRange(ctx, ffprobe, source, plan, int64(origin)*ticksPerSecond)
						if err != nil {
							t.Fatal("actual source interval could not be independently observed")
						}
						if end == 100 {
							tail, err := MeasureGeneratedMP4SourceTail(ctx, ffprobe, source, plan, certificate)
							if err != nil || tail != coverage {
								t.Fatal("actual production tail disagrees with its independently measured endpoint and last sample")
							}
						}
						var packets [MaxHLSRenditions]GeneratedSegmentBounds
						var native [MaxHLSRenditions]GeneratedFMP4NativeClock
						var lists [MaxHLSRenditions]MediaPlaylist
						for variant := 0; variant < max(1, count); variant++ {
							list := generatedWindowReadList(t, directory, HLSPlaylistName(variant, count), plan.HLS.Window.StartNumber)
							initialization, err := os.Open(filepath.Join(directory, list.InitName))
							if err != nil {
								t.Fatal("actual production initialization could not be held")
							}
							segment, err := os.Open(filepath.Join(directory, list.Segments[0].Name))
							if err != nil {
								_ = initialization.Close()
								t.Fatal("actual production media could not be held")
							}
							func() {
								defer initialization.Close()
								defer segment.Close()
								initStamp, initErr := initialization.Stat()
								mediaStamp, mediaErr := segment.Stat()
								if initErr != nil || mediaErr != nil {
									t.Fatal("actual production file identity could not be captured")
								}
								initOffset, initErr := initialization.Seek(0, io.SeekCurrent)
								mediaOffset, mediaErr := segment.Seek(0, io.SeekCurrent)
								if initErr != nil || mediaErr != nil {
									t.Fatal("actual production borrowed offsets could not be captured")
								}
								packets[variant], err = MeasureGeneratedWindowSegment(ctx, ffprobe, plan, initialization, segment)
								if err != nil {
									t.Fatal("actual production packets/framing/restart observation failed")
								}
								native[variant], err = MeasureGeneratedFMP4NativeClock(ctx, plan, initialization, segment)
								if err != nil || !transcodeSourceUnchanged(initialization, initStamp) || !transcodeSourceUnchanged(segment, mediaStamp) {
									t.Fatal("actual production native clock and whole bytes lost their common identity fence")
								}
								combined := filepath.Join(directory, fmt.Sprintf("v2-decode-%d.mp4", variant))
								generatedFMP4EmissionCopyHeldFiles(t, combined, initialization, segment, initStamp.Size(), mediaStamp.Size(), native[variant])
								width, height := plan.Width, plan.Height
								if adaptive {
									width, height = plan.HLS.Renditions[variant].Width, plan.HLS.Renditions[variant].Height
								}
								decoded, stderr, _, _, err := generatedFMP4DiagnosticCommand(ctx, ffmpeg, sourceDirectory, nil, nil,
									[]string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-i", combined, "-map", "0:v:0", "-an", "-sn", "-dn", "-pix_fmt", "rgb24", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1"})
								if err != nil || len(stderr) != 0 || !generatedFMP4DiagnosticContent(decoded, width, height, rate, start, end) {
									t.Fatal("actual production media did not independently decode its complete requested source content")
								}
								initAfter, initErr := initialization.Seek(0, io.SeekCurrent)
								mediaAfter, mediaErr := segment.Seek(0, io.SeekCurrent)
								if initErr != nil || mediaErr != nil || initAfter != initOffset || mediaAfter != mediaOffset ||
									!transcodeSourceUnchanged(initialization, initStamp) || !transcodeSourceUnchanged(segment, mediaStamp) {
									t.Fatal("actual production decode lost its held byte identity or changed a borrowed offset")
								}
							}()
							lists[variant] = list
						}
						closure, err := ValidateGeneratedWindowClosure(plan, coverage, *result.WindowInputEvidence, mux, packets, lists)
						if err != nil || closure.NativeClockVersion != GeneratedWindowNativeClockV2 {
							t.Fatal("actual source/input/complete-output closure did not bind version two")
						}
						proof, err := ValidateGeneratedFMP4NativeEmission(plan, mux, packets, native)
						if err != nil || proof.StartTicks != plan.StartTicks || proof.EndTicks != plan.HLS.Window.EndTicks || proof.RenditionCount != max(1, count) {
							t.Fatal("actual source-global native emission proof rejected normally completed output")
						}
						if ValidateGeneratedMP4SourceEndpointIdentity(source, certificate) != nil || !transcodeSourceUnchanged(source, before) {
							t.Fatal("source changed across the complete actual production proof")
						}
						t.Logf("actual version-two emission: rate=%d origin=%d start=%d end=%d renditions=%d source_frames=%d graph_qualified=false",
							rate, origin, start, end, max(1, count), coverage.FrameCount)
					})
				}
			}
		}
	}
}

// Decode copies originate only from the two held descriptors measured above.
// Each part is independently paired with its native/bounds whole-file digest;
// opening a pathname cannot replace the proven bytes with another inode.
func generatedFMP4EmissionCopyHeldFiles(t *testing.T, name string, initialization, segment *os.File, initSize, mediaSize int64, native GeneratedFMP4NativeClock) {
	t.Helper()
	output, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("actual production decode copy could not be created")
	}
	defer output.Close()
	parts := []struct {
		file   *os.File
		size   int64
		digest [32]byte
	}{{initialization, initSize, native.InitializationSHA256}, {segment, mediaSize, native.SegmentSHA256}}
	for _, part := range parts {
		if part.file == nil || part.size <= 0 || part.digest == ([32]byte{}) {
			t.Fatal("actual production decode copy lacks a held complete part")
		}
		digest := sha256.New()
		written, err := io.Copy(io.MultiWriter(output, digest), io.NewSectionReader(part.file, 0, part.size))
		var observed [32]byte
		copy(observed[:], digest.Sum(nil))
		if err != nil || written != part.size || observed != part.digest {
			t.Fatal("actual production decode copy differs from its independently measured bytes")
		}
	}
	if err := output.Close(); err != nil {
		t.Fatal("actual production decode copy did not close normally")
	}
}
