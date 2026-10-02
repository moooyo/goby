//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

const (
	generatedAudioProjectionStdoutLimit = 4 << 20
	generatedAudioProjectionStderrLimit = 64 << 10
)

type generatedAudioProjectionBuffer struct {
	bytes.Buffer
	limit  int
	cancel context.CancelFunc
	err    error
}

func (buffer *generatedAudioProjectionBuffer) Write(data []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	if len(data) > buffer.limit-buffer.Len() {
		buffer.err = ErrTimelineLimit
		buffer.cancel()
		return 0, buffer.err
	}
	return buffer.Buffer.Write(data)
}

// This diagnostic uses the same admission and owned retirement sequence as the
// audio calibration wrapper, with tighter independent output budgets. Commands
// inherit foreground classification and never bypass the shared governor.
func generatedAudioProjectionCommand(ctx context.Context, executable string, borrowed *os.File, args ...string) ([]byte, []byte, error) {
	processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var source *os.File
	var before os.FileInfo
	var beforeIdentity string
	var beforeOffset int64
	if borrowed != nil {
		var err error
		before, err = borrowed.Stat()
		if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > 16<<20 {
			return nil, nil, ErrInvalidInput
		}
		beforeIdentity, err = media.VideoSeekSourceIdentity(before)
		if err != nil {
			return nil, nil, ErrInvalidInput
		}
		stat, ok := before.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) || before.Mode().Perm() != 0600 {
			return nil, nil, ErrInvalidInput
		}
		beforeOffset, err = borrowed.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, nil, ErrInvalidInput
		}
		source, err = DuplicateInput(borrowed)
		if err != nil {
			return nil, nil, err
		}
		defer source.Close()
	}
	command := exec.CommandContext(processCtx, executable, args...)
	command.Env = processEnvironment()
	command.Dir = "/"
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	if source != nil {
		command.ExtraFiles = []*os.File{source}
	}
	stdout := &generatedAudioProjectionBuffer{limit: generatedAudioProjectionStdoutLimit, cancel: cancel}
	stderr := &generatedAudioProjectionBuffer{limit: generatedAudioProjectionStderrLimit, cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
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
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		waitErr := waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			// Do not retry a numeric process-group signal after unknown waitid.
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	var sourceErr error
	if borrowed != nil {
		after, statErr := borrowed.Stat()
		afterOffset, offsetErr := borrowed.Seek(0, io.SeekCurrent)
		if statErr != nil || offsetErr != nil || afterOffset != beforeOffset || !transcodeSourceUnchanged(borrowed, before) {
			sourceErr = ErrInvalidInput
		} else if identity, err := media.VideoSeekSourceIdentity(after); err != nil || identity != beforeIdentity {
			sourceErr = ErrInvalidInput
		}
	}
	return stdout.Bytes(), stderr.Bytes(), errors.Join(runErr, stdout.err, stderr.err, processCtx.Err(), sourceErr)
}

// Error summaries intentionally omit executable names, arguments, paths and
// raw diagnostics. Detailed stderr is retained only in the private artifacts.
func generatedAudioProjectionErrorCode(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, media.ErrProcessRetirementUnknown):
		return "retirement_unknown"
	case errors.Is(err, ErrInvalidInput):
		return "source_changed"
	case errors.Is(err, ErrTimelineLimit):
		return "output_limit"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "process_failed"
	}
}

func generatedAudioProjectionSave(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func generatedAudioProjectionInteger(value json.RawMessage) (int64, bool) {
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return 0, false
	}
	text := string(value)
	if value[0] == '"' {
		if json.Unmarshal(value, &text) != nil {
			return 0, false
		}
	}
	integer, err := strconv.ParseInt(text, 10, 64)
	return integer, err == nil
}

type generatedAudioProjectionSummary struct {
	Source                    string `json:"source"`
	Mode                      string `json:"mode"`
	Packets                   int64  `json:"packets"`
	PacketPTS                 int64  `json:"packet_pts"`
	PacketDTS                 int64  `json:"packet_dts"`
	PacketDuration            int64  `json:"packet_duration"`
	Frames                    int64  `json:"frames"`
	FramePTS                  int64  `json:"frame_pts"`
	FrameDTS                  int64  `json:"frame_pkt_dts"`
	FrameBestEffort           int64  `json:"frame_best_effort"`
	FrameDuration             int64  `json:"frame_duration"`
	FramesWithSamples         int64  `json:"frames_with_samples"`
	DecodedFrameSamples       int64  `json:"decoded_frame_samples"`
	TimeBase                  string `json:"time_base"`
	SampleRate                int64  `json:"sample_rate"`
	FormatStartKnown          bool   `json:"format_start_known"`
	StrictNativePTSObserved   bool   `json:"strict_native_pts_observed"`
	BorrowedIdentityUnchanged bool   `json:"borrowed_identity_unchanged"`
	BorrowedOffsetUnchanged   bool   `json:"borrowed_offset_unchanged"`
	Qualified                 bool   `json:"qualified"`
}

func generatedAudioProjectionSummarize(data []byte, source, mode string) (generatedAudioProjectionSummary, error) {
	result := generatedAudioProjectionSummary{Source: source, Mode: mode}
	var document struct {
		Records []struct {
			Type       string          `json:"type"`
			Stream     json.RawMessage `json:"stream_index"`
			PTS        json.RawMessage `json:"pts"`
			DTS        json.RawMessage `json:"dts"`
			PacketDTS  json.RawMessage `json:"pkt_dts"`
			BestEffort json.RawMessage `json:"best_effort_timestamp"`
			Duration   json.RawMessage `json:"duration"`
			Samples    json.RawMessage `json:"nb_samples"`
		} `json:"packets_and_frames"`
		Streams []struct {
			Index    json.RawMessage `json:"index"`
			Kind     string          `json:"codec_type"`
			Codec    string          `json:"codec_name"`
			TimeBase string          `json:"time_base"`
			Rate     string          `json:"sample_rate"`
		} `json:"streams"`
		Format struct {
			Start string `json:"start_time"`
		} `json:"format"`
	}
	if len(data) == 0 || len(data) > generatedAudioProjectionStdoutLimit || json.Unmarshal(data, &document) != nil ||
		len(document.Streams) != 1 || len(document.Records) < 1 || len(document.Records) > 16_384 {
		return result, ErrTimelineProbe
	}
	stream := document.Streams[0]
	index, knownIndex := generatedAudioProjectionInteger(stream.Index)
	_, _, baseErr := generatedInputTimeBase([]byte(stream.TimeBase))
	rate, rateErr := strconv.ParseInt(stream.Rate, 10, 64)
	if !knownIndex || index != 0 || stream.Kind != "audio" || stream.Codec != "pcm_s16le" || baseErr != nil || rateErr != nil || rate != 48_000 {
		return result, ErrTimelineProbe
	}
	result.TimeBase, result.SampleRate = stream.TimeBase, rate
	result.FormatStartKnown = document.Format.Start != "" && document.Format.Start != "N/A"
	for _, record := range document.Records {
		streamIndex, known := generatedAudioProjectionInteger(record.Stream)
		if !known || streamIndex != index {
			return result, ErrTimelineProbe
		}
		_, pts := generatedAudioProjectionInteger(record.PTS)
		_, duration := generatedAudioProjectionInteger(record.Duration)
		switch record.Type {
		case "packet":
			result.Packets++
			if pts {
				result.PacketPTS++
			}
			if _, known := generatedAudioProjectionInteger(record.DTS); known {
				result.PacketDTS++
			}
			if duration {
				result.PacketDuration++
			}
		case "frame":
			result.Frames++
			if pts {
				result.FramePTS++
			}
			if _, known := generatedAudioProjectionInteger(record.PacketDTS); known {
				result.FrameDTS++
			}
			if _, known := generatedAudioProjectionInteger(record.BestEffort); known {
				result.FrameBestEffort++
			}
			if duration {
				result.FrameDuration++
			}
			samples, known := generatedAudioProjectionInteger(record.Samples)
			if known {
				if samples < 1 || samples > generatedAudioInputMaxFrameSamples || samples > generatedAudioInputMaxSamples-result.DecodedFrameSamples {
					return result, ErrTimelineLimit
				}
				result.FramesWithSamples++
				result.DecodedFrameSamples += samples
			}
		default:
			return result, ErrTimelineProbe
		}
	}
	if result.Packets == 0 || result.Frames == 0 {
		return result, ErrTimelineProbe
	}
	// This flag labels a strict projection's observed presence only. It is
	// not independent source coverage, timestamp authenticity or A/V closure.
	result.StrictNativePTSObserved = mode == "A_strict" && result.PacketPTS == result.Packets && result.FramePTS == result.Frames
	return result, nil
}

// Run explicitly against the remote pinned tools. The evidence directory must
// be a fresh absolute path: historical raw artifacts are never overwritten.
func TestGeneratedAudioSourceProjectionDiagnostic(t *testing.T) {
	if os.Getenv("GOBY_AUDIO_SOURCE_PROJECTION_DIAGNOSTIC") != "1" {
		t.Skip("GOBY_AUDIO_SOURCE_PROJECTION_DIAGNOSTIC=1 is required for this opt-in diagnostic")
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	directory := os.Getenv("GOBY_AUDIO_SOURCE_PROJECTION_EVIDENCE_DIR")
	if ffmpeg == "" || ffprobe == "" || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || strings.ContainsAny(directory, "\x00\r\n") {
		t.Fatal("diagnostic requires pinned tools and a fresh absolute evidence directory")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal("diagnostic evidence directory could not be created exclusively")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("diagnostic evidence directory is not private")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		t.Fatal("diagnostic evidence directory is not owned by this process")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal("diagnostic evidence directory could not be opened")
	}
	defer root.Close()
	var summaries []generatedAudioProjectionSummary
	for _, fixture := range []struct{ label, extension, format string }{{"wav", "wav", "wav"}, {"nut", "nut", "nut"}} {
		sourceName := "source." + fixture.extension
		sourcePath := filepath.Join(directory, sourceName)
		stdout, stderr, runErr := generatedAudioProjectionCommand(context.Background(), ffmpeg, nil,
			"-hide_banner", "-nostdin", "-nostats", "-v", "error", "-n", "-f", "lavfi", "-i",
			"aevalsrc=0.15*sin(2*PI*(173*t+127*t*t)):s=48000:d=24", "-map", "0:a:0", "-ac", "1", "-ar", "48000",
			"-c:a", "pcm_s16le", "-f", fixture.format, sourcePath)
		if generatedAudioProjectionSave(root, fixture.label+"-generation.stdout", stdout) != nil || generatedAudioProjectionSave(root, fixture.label+"-generation.stderr", stderr) != nil {
			t.Fatal("diagnostic generation artifacts could not be saved exclusively")
		}
		if runErr != nil || len(stderr) != 0 {
			t.Fatalf("diagnostic source generation failed: source=%s error_class=%s stderr_bytes=%d", fixture.label, generatedAudioProjectionErrorCode(runErr), len(stderr))
		}
		source, err := root.Open(sourceName)
		if err != nil {
			t.Fatal("diagnostic source could not be opened")
		}
		if err := source.Chmod(0600); err != nil {
			_ = source.Close()
			t.Fatal("diagnostic source permissions could not be restricted")
		}
		func() {
			defer source.Close()
			if _, err := source.Seek(37, io.SeekStart); err != nil {
				t.Fatal("diagnostic source offset could not be initialized")
			}
			for _, projection := range []struct{ label, flags string }{{"A_strict", "+nofillin-genpts"}, {"B_default", ""}, {"C_no_genpts", "-genpts"}} {
				args := []string{"-v", "error", "-threads", "1"}
				if projection.flags != "" {
					args = append(args, "-fflags", projection.flags)
				}
				args = append(args, "-protocol_whitelist", "file,pipe", "-format_whitelist", "wav,nut", "-select_streams", "a:0",
					"-show_packets", "-show_frames", "-show_streams", "-show_format", "-show_entries",
					"packet=stream_index,pts,dts,duration,pos,size,flags:packet_side_data=:frame=media_type,stream_index,pts,pkt_dts,best_effort_timestamp,duration,nb_samples,sample_rate,pkt_pos:frame_side_data=:stream=index,codec_name,codec_type,time_base,sample_rate,start_pts,start_time,duration_ts,duration,nb_frames:stream_tags=:stream_disposition=:stream_side_data=:format=start_time,duration:format_tags=",
					"-of", "json", "-i", "/proc/self/fd/3")
				stdout, stderr, runErr := generatedAudioProjectionCommand(context.Background(), ffprobe, source, args...)
				prefix := fixture.label + "-" + projection.label
				if generatedAudioProjectionSave(root, prefix+".json", stdout) != nil || generatedAudioProjectionSave(root, prefix+".stderr", stderr) != nil {
					t.Fatal("diagnostic projection artifacts could not be saved exclusively")
				}
				if runErr != nil || len(stderr) != 0 {
					t.Errorf("diagnostic projection failed: source=%s mode=%s error_class=%s stderr_bytes=%d", fixture.label, projection.label, generatedAudioProjectionErrorCode(runErr), len(stderr))
					continue
				}
				summary, err := generatedAudioProjectionSummarize(stdout, fixture.label, projection.label)
				if err != nil {
					t.Errorf("diagnostic projection summary failed: source=%s mode=%s error_class=%s", fixture.label, projection.label, generatedAudioProjectionErrorCode(err))
					continue
				}
				summary.BorrowedIdentityUnchanged, summary.BorrowedOffsetUnchanged = true, true
				summaries = append(summaries, summary)
				safe, err := json.Marshal(summary)
				if err != nil {
					t.Fatal("diagnostic safe summary could not be encoded")
				}
				t.Logf("audio source projection: %s", safe)
			}
		}()
	}
	safe, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil || generatedAudioProjectionSave(root, "summary.json", safe) != nil {
		t.Fatal("diagnostic safe summary artifact could not be saved exclusively")
	}
	if len(summaries) != 6 {
		t.Error("diagnostic did not retain every source and mode projection")
	}
}
