//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type generatedAudioCalibrationBuffer struct{ bytes.Buffer }

func (buffer *generatedAudioCalibrationBuffer) Write(data []byte) (int, error) {
	if len(data) > (64<<20)-buffer.Len() {
		return 0, ErrTimelineLimit
	}
	return buffer.Buffer.Write(data)
}

// Every calibration process retains its group until the exited leader is
// observed with WNOWAIT. The optional reader starts only after actual Start.
func generatedAudioCalibrationCommand(t *testing.T, ctx context.Context, executable string, observer *generatedAudioInputObserver, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = processEnvironment()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	if observer != nil {
		for _, pipe := range observer.pipes {
			command.ExtraFiles = append(command.ExtraFiles, pipe.write)
		}
	}
	var stdout, stderr generatedAudioCalibrationBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	runErr := media.RunProcessWithRetirement(ctx, command, func() error {
		if observer != nil {
			observer.start()
		}
		waitErr := waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			// Unknown retirement cannot authorize a later numeric-PID retry.
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	if command.Process == nil {
		t.Fatalf("calibration process did not start: %v", runErr)
	}
	var observerErr error
	if observer != nil {
		observerErr = generatedAudioInputObserverFinish(t, observer)
	}
	if runErr != nil || observerErr != nil || stderr.Len() != 0 {
		t.Fatalf("calibration process failed: process=%v, observer=%v, stderr=%s", runErr, observerErr, stderr.String())
	}
	return stdout.Bytes()
}

type generatedAudioCalibrationFrameFacts struct {
	Samples    int64
	Frames     int64
	First, End GeneratedRational
	Contiguous bool
	TimeBase   GeneratedRational
}

func generatedAudioCalibrationFrames(t *testing.T, ctx context.Context, ffprobe, path string) generatedAudioCalibrationFrameFacts {
	t.Helper()
	data := generatedAudioCalibrationCommand(t, ctx, ffprobe, nil, "-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts",
		"-select_streams", "a:0", "-show_frames", "-show_streams", "-show_entries",
		"frame=media_type,stream_index,pts,nb_samples:stream=index,codec_type,time_base,sample_rate", "-of", "json", path)
	result, err := parseGeneratedAudioCalibrationFrames(data)
	if err != nil {
		t.Fatalf("calibration decoded frames lack a complete native sample clock: %v", err)
	}
	return result
}

// Native frame PTS is mandatory. Counts and best-effort timestamps cannot
// repair a demuxer that supplied no original presentation clock.
func parseGeneratedAudioCalibrationFrames(data []byte) (generatedAudioCalibrationFrameFacts, error) {
	invalid := func() (generatedAudioCalibrationFrameFacts, error) {
		return generatedAudioCalibrationFrameFacts{}, ErrTimelineProbe
	}
	var document struct {
		Frames []struct {
			Kind    string `json:"media_type"`
			Stream  *int64 `json:"stream_index"`
			PTS     *int64 `json:"pts"`
			Samples *int64 `json:"nb_samples"`
		} `json:"frames"`
		Streams []struct {
			Index    *int64 `json:"index"`
			Kind     string `json:"codec_type"`
			TimeBase string `json:"time_base"`
			Rate     string `json:"sample_rate"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &document) != nil || len(document.Streams) != 1 || len(document.Frames) < 1 || len(document.Frames) > 8192 {
		return invalid()
	}
	stream := document.Streams[0]
	num, den, err := generatedInputTimeBase([]byte(stream.TimeBase))
	rate, rateErr := strconv.ParseInt(stream.Rate, 10, 64)
	if err != nil || rateErr != nil || stream.Index == nil || stream.Kind != "audio" || rate != 48_000 {
		return invalid()
	}
	result := generatedAudioCalibrationFrameFacts{Contiguous: true, TimeBase: GeneratedRational{Num: num, Den: den}}
	var first, end *big.Rat
	for _, frame := range document.Frames {
		if frame.Kind != "audio" || frame.Stream == nil || *frame.Stream != *stream.Index || frame.PTS == nil || frame.Samples == nil || *frame.Samples < 1 || *frame.Samples > generatedAudioInputMaxFrameSamples {
			return invalid()
		}
		pts := generatedClockSeconds(*frame.PTS, num, den)
		if first == nil {
			first = new(big.Rat).Set(pts)
		} else {
			result.Contiguous = result.Contiguous && end.Cmp(pts) == 0
		}
		end = new(big.Rat).Add(pts, new(big.Rat).SetFrac64(*frame.Samples, rate))
		result.Samples += *frame.Samples
		result.Frames++
	}
	result.First = GeneratedRational{Num: first.Num().Int64(), Den: first.Denom().Int64()}
	result.End = GeneratedRational{Num: end.Num().Int64(), Den: end.Denom().Int64()}
	return result, nil
}

func TestGeneratedAudioInputActualUntimestampedWAVRejected(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for native audio-clock calibration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "untimestamped-source.wav")
	generatedAudioCalibrationCommand(t, ctx, ffmpeg, nil, "-hide_banner", "-nostdin", "-v", "error", "-y",
		"-f", "lavfi", "-i", "aevalsrc=0.15*sin(2*PI*(173*t+127*t*t)):s=48000:d=24",
		"-ac", "1", "-c:a", "pcm_s16le", "-f", "wav", source)
	data := generatedAudioCalibrationCommand(t, ctx, ffprobe, nil, "-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts",
		"-select_streams", "a:0", "-show_frames", "-show_streams", "-show_entries",
		"frame=media_type,stream_index,pts,nb_samples:stream=index,codec_type,time_base,sample_rate", "-of", "json", source)
	var projection struct {
		Frames []struct {
			PTS     *int64 `json:"pts"`
			Samples *int64 `json:"nb_samples"`
		} `json:"frames"`
	}
	if json.Unmarshal(data, &projection) != nil || len(projection.Frames) < 1 || len(projection.Frames) > 8192 {
		t.Fatal("WAV negative calibration omitted bounded actual decoded frames")
	}
	var samples int64
	for _, frame := range projection.Frames {
		if frame.PTS != nil || frame.Samples == nil || *frame.Samples < 1 || *frame.Samples > generatedAudioInputMaxFrameSamples {
			t.Fatal("WAV negative calibration did not preserve its independently observed missing native PTS")
		}
		samples += *frame.Samples
	}
	pcm := generatedAudioCalibrationCommand(t, ctx, ffmpeg, nil, "-hide_banner", "-nostdin", "-v", "error", "-i", source,
		"-map", "0:a:0", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
	if len(pcm) == 0 || len(pcm)%2 != 0 || samples != int64(len(pcm)/2) {
		t.Fatal("WAV negative calibration has no independent whole decoded sample-count match")
	}
	result, err := parseGeneratedAudioCalibrationFrames(data)
	if !errors.Is(err, ErrTimelineProbe) || result != (generatedAudioCalibrationFrameFacts{}) {
		t.Fatal("missing native PTS was repaired by sample counts or accepted as a source clock")
	}
	t.Logf("native WAV clock rejected: frames=%d decoded_samples=%d native_pts_present=0 qualified=false", len(projection.Frames), samples)
}

// This calibrates the diagnostic point and observed MP4 decode behavior. It
// never qualifies generated A/V closure, gapless audio or a public MAP graph.
func TestGeneratedAudioInputActualAACCalibration(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual audio diagnostic calibration")
	}
	for _, seconds := range []int{24, 6} {
		for _, channels := range []int{1, 2} {
			t.Run(fmt.Sprintf("%ds_%dch", seconds, channels), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				directory := t.TempDir()
				// NUT retains original PCM packet PTS. The separately observed
				// WAV demuxer has no native PTS under +nofillin-genpts.
				source := filepath.Join(directory, "source.nut")
				generatedAudioCalibrationCommand(t, ctx, ffmpeg, nil, "-hide_banner", "-nostdin", "-v", "error", "-y",
					"-f", "lavfi", "-i", fmt.Sprintf("aevalsrc=0.15*sin(2*PI*(173*t+127*t*t)):s=48000:d=%d", seconds),
					"-ac", strconv.Itoa(channels), "-c:a", "pcm_s16le", "-f", "nut", source)
				// The source duration string is not the sample-count oracle.
				// Independently decode the actual source PCM bytes and clocks.
				pcm := generatedAudioCalibrationCommand(t, ctx, ffmpeg, nil, "-hide_banner", "-nostdin", "-v", "error", "-i", source,
					"-map", "0:a:0", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
				if len(pcm) == 0 || len(pcm)%(channels*2) != 0 {
					t.Fatal("source PCM byte count is not a whole decoded sample set")
				}
				sourceSamples := int64(len(pcm) / (channels * 2))
				sourceFrames := generatedAudioCalibrationFrames(t, ctx, ffprobe, source)
				if sourceFrames.Samples != sourceSamples || !sourceFrames.Contiguous || sourceFrames.First.Num != 0 ||
					new(big.Rat).SetFrac64(sourceFrames.End.Num, sourceFrames.End.Den).Cmp(new(big.Rat).SetFrac64(sourceSamples, 48000)) != 0 {
					t.Fatalf("source PCM count and decoded clock differ: bytes_samples=%d, %+v", sourceSamples, sourceFrames)
				}
				if sourceSamples != int64(seconds)*48000 {
					t.Fatalf("actual source did not establish the requested calibration sample set: %d", sourceSamples)
				}
				option := generatedAudioInputTestOptions(0)
				option.MaxSamples = sourceSamples + 4096
				observer, err := newGeneratedAudioInputObserver(ctx, []RawGeneratedAudioInputOptions{option}, nil, cancel)
				if err != nil {
					t.Fatal(err)
				}
				defer observer.close()
				generatedAudioCalibrationCommand(t, ctx, ffmpeg, observer, "-hide_banner", "-nostdin", "-nostats", "-v", "error", "-y",
					"-f", "lavfi", "-i", fmt.Sprintf("color=c=red:s=160x96:r=24:d=%d", seconds), "-i", source,
					"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-g", "144", "-force_key_frames", "expr:gte(t,n_forced*6)",
					"-c:a", "aac", "-profile:a", "aac_low", "-threads:a", "1", "-ar", "48000", "-ac", strconv.Itoa(channels), "-b:a", "96000",
					"-stats_enc_pre:a:0", "pipe:3", "-stats_enc_pre_fmt:a:0", "GOBY_AUDIO {fidx} {sidx} {n} {sn} {samp} {tb} {pts} {ni} {tbi} {ptsi}",
					"-avoid_negative_ts", "disabled", "-f", "hls", "-hls_segment_type", "fmp4", "-hls_time", "6", "-hls_list_size", "0", "-hls_playlist_type", "event",
					"-hls_flags", "temp_file+independent_segments+discont_start",
					"-hls_segment_options", "use_editlist=1", "-hls_fmp4_init_filename", "init.mp4", "-hls_segment_filename", filepath.Join(directory, "segment-%06d.m4s"), filepath.Join(directory, "main.m3u8"))
				raw := observer.snapshot()[0]
				if raw.Frames < 1 || !raw.SamplesContiguous || !raw.EncoderSampleClockExact || raw.FirstSampleNumber != 0 ||
					raw.LastSampleNumber+raw.LastFrameSamples != raw.TotalSamples {
					t.Fatalf("actual stats did not establish pre-frame cumulative samples: %+v", raw)
				}
				// The positive joint-grid case requires an observed whole source
				// set. The negative case retains real upstream padding, if any.
				if seconds == 24 && (sourceSamples%1024 != 0 || raw.TotalSamples != sourceSamples) {
					t.Fatalf("joint-grid input is not the independently decoded source count: source=%d, raw=%+v", sourceSamples, raw)
				}
				if seconds == 6 && sourceSamples%1024 == 0 {
					t.Fatal("negative calibration unexpectedly lies on the AAC frame grid")
				}
				list := generatedWindowReadList(t, directory, "main.m3u8", 0)
				combined := generatedWindowCombine(t, directory, list, "combined.mp4")
				decoded := generatedAudioCalibrationFrames(t, ctx, ffprobe, combined)
				initialization, err := os.Open(filepath.Join(directory, list.InitName))
				if err != nil {
					t.Fatal(err)
				}
				defer initialization.Close()
				var mediaBytes []byte
				for _, segment := range list.Segments {
					data, err := os.ReadFile(filepath.Join(directory, segment.Name))
					if err != nil {
						t.Fatal(err)
					}
					mediaBytes = append(mediaBytes, data...)
				}
				mediaPath := filepath.Join(directory, "combined-media.m4s")
				if err := os.WriteFile(mediaPath, mediaBytes, 0600); err != nil {
					t.Fatal(err)
				}
				mediaFile, err := os.Open(mediaPath)
				if err != nil {
					t.Fatal(err)
				}
				defer mediaFile.Close()
				bounds, err := MeasureGeneratedSegmentBounds(ctx, ffprobe, initialization, mediaFile, true)
				if err != nil || !bounds.Audio.Present || bounds.InitializationSHA256 == ([32]byte{}) {
					t.Fatalf("actual AAC packet envelope or init identity is missing: %+v, %v", bounds, err)
				}
				t.Logf("audio calibration only: seconds=%d channels=%d source_decoded=%d source_clock=%+v raw_pre_encoder=%+v effective_output_decoded=%+v raw_output_audio=%+v init_sha256=%x qualified=false",
					seconds, channels, sourceSamples, sourceFrames, raw, decoded, bounds.Audio, bounds.InitializationSHA256)
			})
		}
	}
}
