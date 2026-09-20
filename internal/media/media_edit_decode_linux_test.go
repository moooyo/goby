package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const mediaEditTestDecodeOutputLimit = 64 << 10

// A successful process exit, clean diagnostics, a complete progress record and
// an unchanged borrowed descriptor are all required before Complete is set.
type mediaEditTestDecodeEvidence struct {
	Complete    bool
	ProgressEnd bool
	OutTimeUS   int64
	// VideoFrames is FFmpeg's progress frame field, not a per-stream total.
	VideoFrames   int64
	AudioStreams  int
	VideoStreams  int
	MappedIndexes []int
	Diagnostics   string `json:",omitempty"`
}

// decodeMediaEditTestCandidate decodes every retained audio and ordinary video
// stream to the null muxer in one invocation. It never seeks or closes the
// borrowed candidate and does not use duration, frame or seek truncation.
func decodeMediaEditTestCandidate(ctx context.Context, executable string, candidate *os.File, info Info) (evidence mediaEditTestDecodeEvidence, resultErr error) {
	args, evidence, err := mediaEditTestDecodeArgs(info)
	if err != nil {
		return evidence, err
	}
	if err := ctx.Err(); err != nil {
		return mediaEditTestDecodeFailure(evidence, "context_unavailable", err)
	}
	decodeContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if candidate == nil || strings.TrimSpace(executable) == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return mediaEditTestDecodeFailure(evidence, "invalid_decode_input", nil)
	}
	before, err := candidate.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxSubtitleRemovalInputBytes+mediaEditOutputAllowance {
		return mediaEditTestDecodeFailure(evidence, "invalid_candidate_descriptor", nil)
	}
	defer func() {
		if mediaEditCheckUnchanged(candidate, before) != nil {
			resultErr = errors.Join(resultErr, mediaEditTestDecodeFail(&evidence, "candidate_changed", nil))
		}
		if err := decodeContext.Err(); err != nil && !errors.Is(resultErr, err) {
			resultErr = errors.Join(resultErr, mediaEditTestDecodeFail(&evidence, "decode_context_ended", err))
		}
	}()
	if info.ProbeVersion != CurrentProbeVersion || info.Size != before.Size() || info.FileChangeTimeNs != FileChangeTime(before) {
		return mediaEditTestDecodeFailure(evidence, "candidate_probe_facts_changed", nil)
	}
	limiter, limitedArgs, err := mediaEditLimitedTool(executable, 0)
	if err != nil {
		return mediaEditTestDecodeFailure(evidence, "resource_limiter_unavailable", nil)
	}
	limitedArgs = append(limitedArgs, args...)
	output, err := runLimitedFilesOutput(decodeContext, 30*time.Second, mediaEditTestDecodeOutputLimit, limiter, []*os.File{candidate}, limitedArgs...)
	if err != nil {
		// The shared runner discards captured output when execution fails. Keep
		// the admitted stream mapping, but never invent progress for that case.
		switch {
		case errors.Is(err, context.Canceled):
			return mediaEditTestDecodeFailure(evidence, "decode_canceled", context.Canceled)
		case errors.Is(err, context.DeadlineExceeded):
			return mediaEditTestDecodeFailure(evidence, "decode_timed_out", context.DeadlineExceeded)
		case errors.Is(err, ErrOutputLimit):
			return mediaEditTestDecodeFailure(evidence, "decode_output_limit", ErrOutputLimit)
		default:
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				return mediaEditTestDecodeFailure(evidence, "decode_exit_"+strconv.Itoa(exit.ExitCode()), nil)
			}
			return mediaEditTestDecodeFailure(evidence, "decode_execution_failed", nil)
		}
	}
	evidence, resultErr = parseMediaEditTestDecodeProgress(output.stdout, evidence)
	if len(output.stderr) != 0 {
		resultErr = errors.Join(resultErr, mediaEditTestDecodeFail(&evidence, "decode_error_diagnostics", nil))
	}
	if resultErr != nil {
		return evidence, resultErr
	}
	evidence.Complete = true
	return evidence, nil
}

func mediaEditTestDecodeArgs(info Info) ([]string, mediaEditTestDecodeEvidence, error) {
	var evidence mediaEditTestDecodeEvidence
	fail := func(code string) ([]string, mediaEditTestDecodeEvidence, error) {
		err := mediaEditTestDecodeFail(&evidence, code, nil)
		return nil, evidence, err
	}
	if len(info.Streams) == 0 || len(info.Streams) > mediaEditMaxStreams {
		return fail("invalid_stream_inventory")
	}
	args := []string{
		"-hide_banner", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode",
		"-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-max_alloc", "268435456",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2", "-i", "/proc/self/fd/3",
	}
	seen := make(map[int]bool, len(info.Streams))
	for _, stream := range info.Streams {
		if stream.Index < 0 || stream.Index > 4095 || seen[stream.Index] {
			return fail("invalid_stream_index")
		}
		seen[stream.Index] = true
		if stream.CodecType != "audio" && (stream.CodecType != "video" || stream.IsAttachedPicture) {
			continue
		}
		if stream.IsExternal || stream.IsAttachedPicture {
			return fail("invalid_av_stream")
		}
		if stream.CodecType == "audio" {
			evidence.AudioStreams++
		} else {
			evidence.VideoStreams++
		}
		evidence.MappedIndexes = append(evidence.MappedIndexes, stream.Index)
		args = append(args, "-map", "0:"+strconv.Itoa(stream.Index))
	}
	if len(evidence.MappedIndexes) == 0 {
		return fail("no_av_streams")
	}
	args = append(args,
		"-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
		"-c:v", "wrapped_avframe", "-c:a", "pcm_s16le", "-threads:v", "1", "-threads:a", "1",
		"-fps_mode", "passthrough", "-progress", "pipe:1", "-nostats", "-f", "null", "-",
	)
	return args, evidence, nil
}

// The null muxer emits no media bytes. Its stdout contains only FFmpeg's real
// progress records; a partial record must not inherit counters from an earlier
// record to qualify as completion. Complete remains the caller's decision.
func parseMediaEditTestDecodeProgress(data []byte, evidence mediaEditTestDecodeEvidence) (mediaEditTestDecodeEvidence, error) {
	evidence.Complete, evidence.ProgressEnd = false, false
	evidence.OutTimeUS, evidence.VideoFrames, evidence.Diagnostics = 0, 0, ""
	if len(data) == 0 || len(data) > mediaEditTestDecodeOutputLimit {
		return mediaEditTestDecodeFailure(evidence, "invalid_progress_size", nil)
	}
	terminated := data[len(data)-1] == '\n'
	lines := bytes.Split(data, []byte{'\n'})
	// The last element is either empty or an unfinished line. Parse only the
	// preceding complete lines so a truncated tail retains observed counters.
	lines = lines[:len(lines)-1]
	seen := make(map[string]bool)
	var outTime, frames int64
	haveOutTime, knownOutTime, haveFrames := false, false, false
	for _, raw := range lines {
		if evidence.ProgressEnd {
			return mediaEditTestDecodeFailure(evidence, "progress_after_end", nil)
		}
		raw = bytes.TrimSuffix(raw, []byte{'\r'})
		key, value, ok := bytes.Cut(raw, []byte{'='})
		if !ok || len(key) == 0 || len(key) > 128 || len(value) == 0 || len(value) > 1024 {
			return mediaEditTestDecodeFailure(evidence, "malformed_progress_line", nil)
		}
		for _, character := range key {
			if character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return mediaEditTestDecodeFailure(evidence, "invalid_progress_key", nil)
			}
		}
		for _, character := range value {
			if character < 0x20 || character > 0x7e {
				return mediaEditTestDecodeFailure(evidence, "invalid_progress_value", nil)
			}
		}
		field := string(key)
		if seen[field] {
			return mediaEditTestDecodeFailure(evidence, "duplicate_progress_field", nil)
		}
		seen[field] = true
		switch field {
		case "out_time_us", "frame":
			if field == "out_time_us" && string(value) == "N/A" {
				// An initial report can precede the first output packet. Only
				// a continue record can contain this unknown timestamp.
				haveOutTime = true
				break
			}
			number, err := mediaEditTestDecodeCounter(value)
			if err != nil {
				return mediaEditTestDecodeFailure(evidence, "invalid_progress_counter", nil)
			}
			if field == "out_time_us" {
				if number < evidence.OutTimeUS {
					return mediaEditTestDecodeFailure(evidence, "progress_time_regressed", nil)
				}
				outTime, evidence.OutTimeUS, haveOutTime, knownOutTime = number, number, true, true
			} else {
				if number < evidence.VideoFrames {
					return mediaEditTestDecodeFailure(evidence, "progress_frames_regressed", nil)
				}
				frames, evidence.VideoFrames, haveFrames = number, number, true
			}
		case "progress":
			if string(value) != "continue" && string(value) != "end" {
				return mediaEditTestDecodeFailure(evidence, "invalid_progress_stage", nil)
			}
			if string(value) == "end" {
				evidence.ProgressEnd = true
			}
			if !haveOutTime || evidence.VideoStreams > 0 && !haveFrames {
				return mediaEditTestDecodeFailure(evidence, "incomplete_progress_record", nil)
			}
			if evidence.ProgressEnd {
				if !knownOutTime || outTime <= 0 || evidence.VideoStreams > 0 && frames <= 0 {
					return mediaEditTestDecodeFailure(evidence, "empty_decode_progress", nil)
				}
			}
			clear(seen)
			outTime, frames, haveOutTime, knownOutTime, haveFrames = 0, 0, false, false, false
		}
	}
	if !terminated {
		return mediaEditTestDecodeFailure(evidence, "unterminated_progress_line", nil)
	}
	if !evidence.ProgressEnd {
		return mediaEditTestDecodeFailure(evidence, "missing_progress_end", nil)
	}
	return evidence, nil
}

func mediaEditTestDecodeCounter(raw []byte) (int64, error) {
	value := bytes.Trim(raw, " ")
	if len(value) == 0 {
		return 0, errors.New("empty decode counter")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, errors.New("invalid decode counter")
		}
	}
	return strconv.ParseInt(string(value), 10, 64)
}

// Diagnostic codes are generated locally and contain neither untrusted tool
// output nor paths. Only known context/output-limit errors retain their cause.
func mediaEditTestDecodeFailure(evidence mediaEditTestDecodeEvidence, code string, cause error) (mediaEditTestDecodeEvidence, error) {
	err := mediaEditTestDecodeFail(&evidence, code, cause)
	return evidence, err
}

func mediaEditTestDecodeFail(evidence *mediaEditTestDecodeEvidence, code string, cause error) error {
	evidence.Complete = false
	if evidence.Diagnostics == "" {
		evidence.Diagnostics = code
	} else if len(evidence.Diagnostics)+len(code)+1 <= 256 {
		evidence.Diagnostics += ";" + code
	}
	if cause != nil {
		return fmt.Errorf("candidate complete decode %s: %w", code, cause)
	}
	return fmt.Errorf("candidate complete decode %s", code)
}

func TestMediaEditDecodeProgressAcceptsCompleteAVAndAudio(t *testing.T) {
	for _, test := range []struct {
		name, data string
		video      int
		frames     int64
	}{
		{"video", "frame=0\nfps=0.00\nstream_0_0_q=-0.0\nout_time_us=0\nprogress=continue\nframe=50\nout_time_us=2000000\nprogress=end\n", 1, 50},
		{"pending_time", "frame=0\nout_time_us=N/A\nprogress=continue\nframe=50\nout_time_us=2000000\nprogress=end\n", 1, 50},
		{"audio", "bitrate=N/A\ntotal_size=N/A\nout_time_us=2000000\nout_time_ms=2000000\nout_time=00:00:02.000000\nspeed=10.0x\nprogress=end\n", 0, 0},
		{"crlf", "frame=  50\r\nout_time_us=2000000\r\nprogress=end\r\n", 2, 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			initial := mediaEditTestDecodeEvidence{AudioStreams: 2, VideoStreams: test.video, MappedIndexes: []int{0, 2}}
			evidence, err := parseMediaEditTestDecodeProgress([]byte(test.data), initial)
			if err != nil || evidence.Complete || !evidence.ProgressEnd || evidence.OutTimeUS != 2000000 || evidence.VideoFrames != test.frames || evidence.AudioStreams != 2 || evidence.VideoStreams != test.video || !reflect.DeepEqual(evidence.MappedIndexes, initial.MappedIndexes) || evidence.Diagnostics != "" {
				t.Fatalf("complete progress evidence mismatch: %+v, %v", evidence, err)
			}
		})
	}
}

func TestMediaEditDecodeProgressRejectsIncompleteOrAmbiguousRecords(t *testing.T) {
	valid := "frame=50\nout_time_us=2000000\nprogress=end\n"
	for _, test := range []struct{ name, data string }{
		{"empty", ""},
		{"oversized", strings.Repeat("x", mediaEditTestDecodeOutputLimit+1)},
		{"unterminated", strings.TrimSuffix(valid, "\n")},
		{"missing_end", "frame=50\nout_time_us=2000000\nprogress=continue\n"},
		{"missing_record_time", "frame=50\nprogress=end\n"},
		{"missing_record_frames", "out_time_us=2000000\nprogress=end\n"},
		{"stale_counters", "frame=50\nout_time_us=2000000\nprogress=continue\nprogress=end\n"},
		{"regressed_frames", "frame=100\nout_time_us=2000000\nprogress=continue\n" + valid},
		{"regressed_time", "frame=50\nout_time_us=4000000\nprogress=continue\n" + valid},
		{"duplicate_time", "frame=50\nout_time_us=1000000\nout_time_us=2000000\nprogress=end\n"},
		{"duplicate_frame", "frame=49\nframe=50\nout_time_us=2000000\nprogress=end\n"},
		{"duplicate_optional", "fps=0.00\nfps=0.00\n" + valid},
		{"duplicate_end", valid + "progress=end\n"},
		{"trailing_record", valid + "out_time_us=3000000\n"},
		{"trailing_blank", valid + "\n"},
		{"invalid_stage", "frame=50\nout_time_us=2000000\nprogress=done\n"},
		{"stage_whitespace", "frame=50\nout_time_us=2000000\nprogress= end\n"},
		{"zero_time", "frame=50\nout_time_us=0\nprogress=end\n"},
		{"zero_frames", "frame=0\nout_time_us=2000000\nprogress=end\n"},
		{"overflow_time", "frame=50\nout_time_us=9223372036854775808\nprogress=end\n"},
		{"overflow_frames", "frame=9223372036854775808\nout_time_us=2000000\nprogress=end\n"},
		{"negative_time", "frame=50\nout_time_us=-1\nprogress=end\n"},
		{"negative_frames", "frame=-1\nout_time_us=2000000\nprogress=end\n"},
		{"unknown_time", "frame=50\nout_time_us=N/A\nprogress=end\n"},
		{"fractional_time", "frame=50\nout_time_us=1.0\nprogress=end\n"},
		{"signed_counter", "frame=+50\nout_time_us=2000000\nprogress=end\n"},
		{"malformed_line", "decoder output\n" + valid},
		{"empty_key", "=value\n" + valid},
		{"empty_value", "speed=\n" + valid},
		{"invalid_key", "path/secret=1\n" + valid},
		{"control_value", "speed=\x001\n" + valid},
		{"large_value", "speed=" + strings.Repeat("x", 1025) + "\n" + valid},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := parseMediaEditTestDecodeProgress([]byte(test.data), mediaEditTestDecodeEvidence{VideoStreams: 1})
			if err == nil || evidence.Complete || evidence.Diagnostics == "" || len(evidence.Diagnostics) > 256 || strings.ContainsAny(evidence.Diagnostics, "/\\") {
				t.Fatalf("invalid progress accepted or unsafe diagnostics: %+v, %v", evidence, err)
			}
		})
	}
}

func TestMediaEditDecodeProgressRetainsObservedFailureCounters(t *testing.T) {
	for _, test := range []struct {
		name, data string
		end        bool
	}{
		{"duplicate_end", "frame=50\nout_time_us=2000000\nprogress=end\nprogress=end\n", true},
		{"truncated_end", "frame=50\nout_time_us=2000000\nprogress=en", false},
		{"missing_frames", "frame=50\nout_time_us=2000000\nprogress=continue\nout_time_us=2000000\nprogress=end\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, err := parseMediaEditTestDecodeProgress([]byte(test.data), mediaEditTestDecodeEvidence{VideoStreams: 1, AudioStreams: 2, MappedIndexes: []int{0, 2, 3}})
			if err == nil || evidence.Complete || evidence.ProgressEnd != test.end || evidence.VideoFrames != 50 || evidence.OutTimeUS != 2000000 || evidence.AudioStreams != 2 || !reflect.DeepEqual(evidence.MappedIndexes, []int{0, 2, 3}) {
				t.Fatalf("failure discarded observed progress: %+v, %v", evidence, err)
			}
		})
	}
}

func TestMediaEditDecodeArgsMapAllRetainedAVWithoutTruncation(t *testing.T) {
	args, evidence, err := mediaEditTestDecodeArgs(Info{Streams: []Stream{
		{Index: 9, CodecType: "video", IsAttachedPicture: true},
		{Index: 2, CodecType: "audio"},
		{Index: 5, CodecType: "subtitle"},
		{Index: 8, CodecType: "video"},
		{Index: 1, CodecType: "audio"},
		{Index: 7, CodecType: "attachment"},
		{Index: 6, CodecType: "video"},
	}})
	if err != nil || evidence.Complete || evidence.AudioStreams != 2 || evidence.VideoStreams != 2 || !reflect.DeepEqual(evidence.MappedIndexes, []int{2, 8, 1, 6}) {
		t.Fatalf("retained mapping differs: %+v, %v", evidence, err)
	}
	var maps []string
	for index, arg := range args {
		if arg == "-map" {
			maps = append(maps, args[index+1])
		}
		if arg == "-t" || arg == "-to" || arg == "-ss" || arg == "-sseof" || arg == "-shortest" || arg == "-fs" || strings.HasPrefix(arg, "-frames") || arg == "copy" {
			t.Fatalf("decode args allow incomplete decoding: %v", args)
		}
	}
	if !reflect.DeepEqual(maps, []string{"0:2", "0:8", "0:1", "0:6"}) || !reflect.DeepEqual(args[len(args)-3:], []string{"-f", "null", "-"}) {
		t.Fatalf("decode mapping or sink differs: %v", args)
	}
}

func TestMediaEditDecodeArgsRejectInvalidStreamInventories(t *testing.T) {
	for _, test := range []struct {
		name    string
		streams []Stream
	}{
		{"none", nil},
		{"oversized", make([]Stream, mediaEditMaxStreams+1)},
		{"negative_index", []Stream{{Index: -1, CodecType: "audio"}}},
		{"large_index", []Stream{{Index: 4096, CodecType: "audio"}}},
		{"duplicate_index", []Stream{{Index: 1, CodecType: "audio"}, {Index: 1, CodecType: "video"}}},
		{"duplicate_excluded_index", []Stream{{Index: 1, CodecType: "audio"}, {Index: 1, CodecType: "subtitle"}}},
		{"external_audio", []Stream{{Index: 0, CodecType: "audio", IsExternal: true}}},
		{"external_video", []Stream{{Index: 0, CodecType: "video", IsExternal: true}}},
		{"invalid_audio_picture", []Stream{{Index: 0, CodecType: "audio", IsAttachedPicture: true}}},
		{"only_pictures", []Stream{{Index: 0, CodecType: "video", IsAttachedPicture: true}}},
		{"only_subtitles", []Stream{{Index: 0, CodecType: "subtitle"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args, evidence, err := mediaEditTestDecodeArgs(Info{Streams: test.streams})
			if err == nil || args != nil || evidence.Complete || evidence.Diagnostics == "" {
				t.Fatalf("invalid stream inventory accepted: %v, %+v, %v", args, evidence, err)
			}
		})
	}
}
