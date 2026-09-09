//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestProgressiveVideoVerifiedSeekMatchesLinearOutput(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	recorder := progressiveVideoSeekRecorder(t, ffmpeg)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	for _, source := range progressiveVideoSources(t, ctx, ffmpeg) {
		t.Run(filepath.Ext(source)[1:], func(t *testing.T) {
			input, info, candidate := progressiveVideoSeekSource(t, ctx, ffprobe, recorder, source, 43_700_000)
			modes := []struct {
				audioCodec string
				threads    int
			}{{"aac", 1}, {"copy", 1}, {"", 1}}
			if filepath.Ext(source) == ".mp4" {
				modes = append(modes, struct {
					audioCodec string
					threads    int
				}{"aac", 2})
			}
			for _, mode := range modes {
				name := mode.audioCodec
				if name == "" {
					name = "no-audio"
				}
				if mode.threads != 1 {
					name += fmt.Sprintf("-threads-%d", mode.threads)
				}
				t.Run(name, func(t *testing.T) {
					plan := progressiveVideoFixturePlan(info, "h264", mode.audioCodec, 43_700_000)
					linear := runRecordedProgressiveVideoSeek(t, ctx, recorder, ffmpeg, ffprobe, input, plan, mode.threads)
					assertRecordedProgressiveVideoSeek(t, linear.args, plan, false)
					plan.VideoSeekCandidate = candidate
					fast := runRecordedProgressiveVideoSeek(t, ctx, recorder, ffmpeg, ffprobe, input, plan, mode.threads)
					assertRecordedProgressiveVideoSeek(t, fast.args, plan, true)
					assertRecordedProgressiveVideoSeekProof(t, fast)
					assertProgressiveVideoSeekOutputsEqual(t, linear, fast)
					if mode.threads > 1 {
						assertProgressiveVideoSeekFullColorSourceWindow(t, ctx, ffmpeg, ffprobe, source, plan, linear, fast)
					}
				})
			}
		})
	}
}

func TestProgressiveVideoUnverifiedSeekFallsBackToLinearOutput(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	recorder := progressiveVideoSeekRecorder(t, ffmpeg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// Matroska audio is sensitive to losing the decoder's preceding history.
	source := progressiveVideoSources(t, ctx, ffmpeg)[1]
	input, info, encoded := progressiveVideoSeekSource(t, ctx, ffprobe, recorder, source, 43_700_000)
	plan := progressiveVideoFixturePlan(info, "h264", "aac", 43_700_000)
	linear := runRecordedProgressiveVideoSeek(t, ctx, recorder, ffmpeg, ffprobe, input, plan, 1)
	assertRecordedProgressiveVideoSeek(t, linear.args, plan, false)
	for _, mismatch := range []string{"tool-identity", "decoded-digest"} {
		t.Run(mismatch, func(t *testing.T) {
			candidate, err := media.ValidateVideoSeekCandidate(encoded)
			if err != nil {
				t.Fatal(err)
			}
			switch mismatch {
			case "tool-identity":
				candidate.Index.ToolIdentity = differentProgressiveVideoSeekDigest(candidate.Index.ToolIdentity)
			case "decoded-digest":
				// The verified landing may match any preceding indexed point,
				// so every possible decoded digest must differ in this fixture.
				for i := range candidate.Index.Entries {
					candidate.Index.Entries[i].DecodedSHA256 = differentProgressiveVideoSeekDigest(candidate.Index.Entries[i].DecodedSHA256)
				}
			}
			data, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			unverified := plan
			unverified.VideoSeekCandidate = string(data)
			if err := ValidatePlan(unverified); err != nil {
				t.Fatalf("mismatch fixture must remain a valid canonical plan: %v", err)
			}
			fallback := runRecordedProgressiveVideoSeek(t, ctx, recorder, ffmpeg, ffprobe, input, unverified, 1)
			assertRecordedProgressiveVideoSeek(t, fallback.args, unverified, false)
			assertProgressiveVideoSeekOutputsEqual(t, linear, fallback)
		})
	}
}

const (
	progressiveVideoSeekRecorderName = "goby-video-seek-record"
	progressiveVideoSeekArgvPrefix   = "goby-video-seek-argv:"
	progressiveVideoSeekProofLogName = "video-seek-proof-argv.jsonl"
	progressiveVideoSeekFilePosition = int64(17)
)

// A copied test executable can record the real conversion arguments while
// transparently executing FFmpeg for analysis, proof, and media production.
func init() {
	if filepath.Base(os.Args[0]) != progressiveVideoSeekRecorderName {
		return
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(91)
	}
	args := os.Args[1:]
	if len(args) > 0 && args[len(args)-1] == "pipe:4" {
		data, err := json.Marshal(args)
		if err != nil {
			os.Exit(92)
		}
		if _, err := fmt.Fprintln(os.Stderr, progressiveVideoSeekArgvPrefix+string(data)); err != nil {
			os.Exit(93)
		}
	}
	if len(args) > 0 && args[len(args)-1] == "pipe:1" && hasArgumentPair(args, "-f", "framehash") && slices.Contains(args, "-ss") {
		if err := recordProgressiveVideoSeekProof(args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(95)
		}
	}
	if err := syscall.Exec(ffmpeg, append([]string{ffmpeg}, args...), os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(94)
	}
}

func recordProgressiveVideoSeekProof(args []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	// Proof diagnostics are consumed privately by media verification. Record
	// their arguments beside the immutable owned wrapper without changing its
	// identity or adding bytes to the framehash/version output.
	file, err := os.OpenFile(filepath.Join(filepath.Dir(executable), progressiveVideoSeekProofLogName), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(file, string(data))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func progressiveVideoSeekRecorder(t *testing.T, ffmpeg string) string {
	t.Helper()
	resolved, err := exec.LookPath(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	// PATH is retained by both sanitized child environments. This owned alias
	// selects the requested binary even when it is not itself named ffmpeg.
	toolsDirectory := t.TempDir()
	if err := os.Symlink(resolved, filepath.Join(toolsDirectory, "ffmpeg")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", toolsDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	path := filepath.Join(t.TempDir(), progressiveVideoSeekRecorderName)
	output, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy recording executable: copy=%v close=%v", copyErr, closeErr)
	}
	// Tool identity resolves symlinks before invoking -version. A real copy
	// keeps the dispatch basename and remains unchanged for every proof.
	return path
}

func progressiveVideoSeekSource(t *testing.T, ctx context.Context, ffprobe, recorder, path string, start int64) (*os.File, media.Info, string) {
	t.Helper()
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	if _, err := input.Seek(progressiveVideoSeekFilePosition, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info, err := (media.Prober{FFprobePath: ffprobe, FFmpegPath: recorder, AnalyzeVideoSeek: true, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
	if err != nil || len(info.VideoSeekIndexes) != 1 || len(info.VideoSeekIndexes[0].Entries) < 3 {
		t.Fatalf("real fixture did not produce bounded restart evidence: indexes=%+v, error=%v", info.VideoSeekIndexes, err)
	}
	candidate, err := media.SelectVideoSeekCandidate(info.VideoSeekIndexes[0], start)
	if err != nil {
		t.Fatal(err)
	}
	assertProgressiveVideoSeekBorrowedFile(t, input)
	return input, info, candidate
}

type recordedProgressiveVideoSeek struct {
	args      []string
	proofArgs [][]string
	facts     progressiveVideoFacts
	hashes    []string
	pcm       []byte
	path      string
	threads   int
	preencode []byte
}

func runRecordedProgressiveVideoSeek(t *testing.T, ctx context.Context, recorder, ffmpeg, ffprobe string, input *os.File, plan Plan, threads int) recordedProgressiveVideoSeek {
	t.Helper()
	directory := t.TempDir()
	proofLog := filepath.Join(filepath.Dir(recorder), progressiveVideoSeekProofLogName)
	if err := os.WriteFile(proofLog, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var ready, ended bool
	result, err := Run(ctx, recorder, directory, input, plan, threads, func(progress Progress) {
		ready = ready || progress.Ready && progress.Bytes > 0
		ended = ended || progress.Ended
	})
	if err != nil || !ready || !ended {
		t.Fatalf("recorded video seek failed: %v ready=%t ended=%t: %s", err, ready, ended, result.StderrTail)
	}
	assertProgressiveVideoSeekBorrowedFile(t, input)
	output := recordedProgressiveVideoSeek{threads: threads}
	records := 0
	for _, line := range strings.Split(result.StderrTail, "\n") {
		if encoded, ok := strings.CutPrefix(line, progressiveVideoSeekArgvPrefix); ok {
			records++
			if err := json.Unmarshal([]byte(encoded), &output.args); err != nil {
				t.Fatalf("decode actual conversion arguments: %v", err)
			}
		}
	}
	if records != 1 {
		t.Fatalf("expected exactly one real conversion record, got %d: %s", records, result.StderrTail)
	}
	assertRecordedProgressiveVideoInputThreads(t, output.args, threads)
	proofData, err := os.ReadFile(proofLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(bytes.TrimSpace(proofData), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var args []string
		if err := json.Unmarshal(line, &args); err != nil {
			t.Fatalf("decode actual proof arguments: %v", err)
		}
		if assertRecordedProgressiveVideoInputThreads(t, args, threads) != 1 || !hasArgumentPair(args, "-i", "/proc/self/fd/3") {
			t.Fatalf("proof changed its borrowed input layout: %v", args)
		}
		output.proofArgs = append(output.proofArgs, args)
	}
	path := filepath.Join(directory, "stream.bin")
	output.path = path
	assertProgressiveFileClosed(t, path)
	strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
	output.facts = probeProgressiveVideoFrames(t, ctx, ffprobe, path)
	assertProgressiveVideoStreams(t, output.facts, plan.AudioStreamIndex >= 0)
	output.hashes = progressiveVideoSeekFrameHashes(t, ctx, ffmpeg, path)
	if len(output.hashes) != len(output.facts.video) {
		t.Fatalf("decoded frame hashes disagree with the complete frame scan: hashes=%d frames=%d", len(output.hashes), len(output.facts.video))
	}
	if threads > 1 {
		output.preencode = progressiveVideoSeekPreencodeFrameHash(t, ctx, ffmpeg, input, output.args, threads)
		if count := len(parseProgressiveVideoSeekFrameHashes(t, output.preencode)); count != len(output.facts.video) {
			t.Fatalf("pre-encode frames disagree with the complete output scan: preencode=%d frames=%d", count, len(output.facts.video))
		}
		assertProgressiveVideoSeekBorrowedFile(t, input)
	}
	if plan.AudioStreamIndex >= 0 {
		output.pcm = decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
		if len(output.pcm) == 0 {
			t.Fatal("selected audio decoded no samples")
		}
	}
	return output
}

func assertRecordedProgressiveVideoInputThreads(t *testing.T, args []string, threads int) int {
	t.Helper()
	inputs, first := 0, 0
	for i, arg := range args {
		if arg != "-i" {
			continue
		}
		found := false
		for j := first; j < i; j++ {
			if args[j] != "-threads" && !strings.HasPrefix(args[j], "-threads:") {
				continue
			}
			if j+1 >= i || args[j+1] != strconv.Itoa(threads) {
				t.Fatalf("input %d did not use the requested %d decoder threads: %v", inputs, threads, args)
			}
			found = true
		}
		if !found {
			t.Fatalf("input %d has no explicit decoder thread limit: %v", inputs, args)
		}
		inputs++
		first = i + 2
	}
	if inputs == 0 {
		t.Fatalf("recorded media command has no input: %v", args)
	}
	return inputs
}

func assertRecordedProgressiveVideoSeekProof(t *testing.T, output recordedProgressiveVideoSeek) {
	t.Helper()
	if len(output.proofArgs) == 0 || len(output.proofArgs) > 8 {
		t.Fatalf("fast conversion requires its own bounded runtime proof: attempts=%d", len(output.proofArgs))
	}
	last := output.proofArgs[len(output.proofArgs)-1]
	proofSeek, proofInput := slices.Index(last, "-ss"), slices.Index(last, "-i")
	producerSeek := slices.Index(output.args, "-ss")
	if proofSeek < 0 || proofSeek >= proofInput || producerSeek < 0 || last[proofSeek+1] != output.args[producerSeek+1] {
		t.Fatalf("producer input seek differs from its final runtime proof: proof=%v producer=%v", last, output.args)
	}
}

func assertRecordedProgressiveVideoSeek(t *testing.T, args []string, plan Plan, fast bool) {
	t.Helper()
	var inputs, seeks, offsets []int
	for i, arg := range args {
		switch arg {
		case "-i":
			inputs = append(inputs, i)
		case "-ss":
			seeks = append(seeks, i)
		case "-itsoffset":
			offsets = append(offsets, i)
		}
	}
	wantInputs := 1
	if fast && plan.AudioStreamIndex >= 0 {
		wantInputs = 2
	}
	if len(inputs) != wantInputs || len(offsets) != wantInputs || len(args) == 0 || args[len(args)-1] != "pipe:4" {
		t.Fatalf("actual conversion changed its bounded input/output layout: %v", args)
	}
	for i, input := range inputs {
		if input+1 >= len(args) || args[input+1] != "/proc/self/fd/3" || offsets[i]+1 >= input ||
			i > 0 && offsets[i] <= inputs[i-1] ||
			args[offsets[i]+1] != signedTickSeconds(-plan.SourceFormatStartTicks) {
			t.Fatalf("actual input %d lost the borrowed source or common origin: %v", i, args)
		}
	}
	audioInput := "0:"
	if fast {
		candidate, err := media.ValidateVideoSeekCandidate(plan.VideoSeekCandidate)
		if err != nil {
			t.Fatal(err)
		}
		seekTimestamp, noAccurateSeek := slices.Index(args, "-seek_timestamp"), slices.Index(args, "-noaccurate_seek")
		if len(seeks) != 2 || seeks[0] >= inputs[0] || seeks[1] <= inputs[len(inputs)-1] ||
			!isRecordedProgressiveVideoSeekIndexed(candidate, args[seeks[0]+1]) ||
			seekTimestamp < 0 || seekTimestamp >= inputs[0] || noAccurateSeek < 0 || noAccurateSeek >= inputs[0] ||
			!hasArgumentPair(args, "-seek_timestamp", "1") {
			t.Fatalf("candidate did not activate its freshly verified input seek: %v", args)
		}
		if plan.AudioStreamIndex >= 0 {
			audioInput = "1:"
			discard := slices.Index(args, "-discard:v")
			if discard <= inputs[0] || discard >= inputs[1] || !hasArgumentPair(args, "-discard:v", "all") {
				t.Fatalf("audio did not retain an independent linear input: %v", args)
			}
		}
	} else if len(seeks) != 1 || seeks[0] <= inputs[0] || slices.Contains(args, "-seek_timestamp") || slices.Contains(args, "-noaccurate_seek") || slices.Contains(args, "-discard:v") {
		t.Fatalf("unverified conversion did not use the original linear input: %v", args)
	}
	if !hasArgumentPair(args, "-map", "0:"+strconv.Itoa(plan.VideoStreamIndex)) || args[seeks[len(seeks)-1]+1] != tickSeconds(plan.StartTicks) {
		t.Fatalf("actual conversion lost its selected video or exact output seek: %v", args)
	}
	if plan.AudioStreamIndex >= 0 {
		if !hasArgumentPair(args, "-map", audioInput+strconv.Itoa(plan.AudioStreamIndex)) {
			t.Fatalf("actual conversion selected audio from the wrong input: %v", args)
		}
	} else if !slices.Contains(args, "-an") || slices.Contains(args, "-discard:v") {
		t.Fatalf("video-only conversion introduced an audio input: %v", args)
	}
}

func isRecordedProgressiveVideoSeekIndexed(candidate media.VideoSeekCandidate, seconds string) bool {
	index := candidate.Index
	if index.TimeBaseNumerator <= 0 || index.TimeBaseDenominator <= 0 || candidate.RequestedStartTicks <= 0 {
		return false
	}
	actual, ok := new(big.Rat).SetString(seconds)
	if !ok {
		return false
	}
	actual.Mul(actual, new(big.Rat).SetInt64(media.TicksPerSecond))
	if !actual.IsInt() {
		return false
	}
	relative := new(big.Int).Sub(actual.Num(), big.NewInt(index.FormatStartTicks))
	if !relative.IsInt64() || relative.Sign() <= 0 || relative.Cmp(big.NewInt(candidate.RequestedStartTicks)) >= 0 {
		return false
	}
	// A successful runtime proof may use a preceding IDR's PTS or DTS. Only
	// the last four indexed points supply the bounded eight-proposal window.
	for i := len(index.Entries) - 1; i >= max(0, len(index.Entries)-4); i-- {
		for _, timestamp := range []int64{index.Entries[i].PTS, index.Entries[i].DTS} {
			numerator := new(big.Int).Mul(big.NewInt(timestamp), big.NewInt(index.TimeBaseNumerator))
			numerator.Mul(numerator, big.NewInt(media.TicksPerSecond))
			// Euclidean division by the positive denominator floors signed
			// source timestamps to the exact 100 ns argument boundary.
			floored := new(big.Int).Div(numerator, big.NewInt(index.TimeBaseDenominator))
			if floored.Cmp(actual.Num()) == 0 {
				return true
			}
		}
	}
	return false
}

func TestProgressiveVideoRecordedSeekRequiresRecentIndexedTimestamp(t *testing.T) {
	candidate := media.VideoSeekCandidate{RequestedStartTicks: 50_000_000, Index: media.VideoSeekIndex{
		FormatStartTicks: 1_000_000, TimeBaseNumerator: 1, TimeBaseDenominator: 90000,
		Entries: []media.VideoSeekPoint{
			{PTS: 90000, DTS: 89999}, {PTS: 180000, DTS: 179999}, {PTS: 270000, DTS: 269999},
			{PTS: 360000, DTS: 359999}, {PTS: 450000, DTS: 449999},
		},
	}}
	for _, test := range []struct {
		seconds string
		want    bool
	}{
		{"5.0000000", true}, {"4.9999888", true}, {"2.0000000", true}, {"1.9999888", true},
		{"1.0000000", false}, {"0.9999888", false}, {"4.3700000", false},
		{"4.9999889", false}, {"4.99998888", false}, {"0.1000000", false},
		{"5.1000000", false}, {"5.1000001", false}, {"NaN", false},
	} {
		if got := isRecordedProgressiveVideoSeekIndexed(candidate, test.seconds); got != test.want {
			t.Errorf("indexed input argument %q: got %t, want %t", test.seconds, got, test.want)
		}
	}
	candidate.Index.FormatStartTicks = -20_000_000
	candidate.RequestedStartTicks = 15_000_000
	candidate.Index.Entries = []media.VideoSeekPoint{{PTS: -90000, DTS: -90001}}
	if !isRecordedProgressiveVideoSeekIndexed(candidate, "-1.0000112") || isRecordedProgressiveVideoSeekIndexed(candidate, "-1.0000111") {
		t.Fatal("negative native DTS must round down instead of toward zero")
	}
	// Even an indexed value cannot authorize zero relative progress or an
	// input proposal at or beyond this test's fractional requested boundary.
	candidate.Index.Entries = []media.VideoSeekPoint{{PTS: -180000, DTS: -180001}, {PTS: -45000, DTS: -44999}}
	for _, seconds := range []string{"-2.0000000", "-2.0000112", "-0.5000000", "-0.4999889"} {
		if isRecordedProgressiveVideoSeekIndexed(candidate, seconds) {
			t.Errorf("out-of-window indexed input argument %q was accepted", seconds)
		}
	}
}

func assertProgressiveVideoSeekBorrowedFile(t *testing.T, input *os.File) {
	t.Helper()
	position, err := input.Seek(0, io.SeekCurrent)
	if err != nil || position != progressiveVideoSeekFilePosition {
		t.Fatalf("analysis or conversion closed or repositioned the borrowed source: position=%d error=%v", position, err)
	}
}

func progressiveVideoSeekFrameHashes(t *testing.T, ctx context.Context, ffmpeg, path string) []string {
	t.Helper()
	data := progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-copyts", "-threads", "1",
		"-i", path, "-map", "0:v:0", "-an", "-sn", "-dn", "-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "yuv420p",
		"-fps_mode", "passthrough", "-enc_time_base", "demux", "-f", "framehash", "-hash", "sha256", "pipe:1")
	return parseProgressiveVideoSeekFrameHashes(t, data)
}

func parseProgressiveVideoSeekFrameHashes(t *testing.T, data []byte) []string {
	t.Helper()
	var hashes []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 6 || len(strings.TrimSpace(fields[5])) != 64 {
			t.Fatalf("invalid decoded video frame hash record: %q", line)
		}
		hashes = append(hashes, strings.TrimSpace(fields[5]))
	}
	return hashes
}

func progressiveVideoSeekPreencodeFrameHash(t *testing.T, ctx context.Context, ffmpeg string, input *os.File, producer []string, threads int) []byte {
	t.Helper()
	firstMap := slices.Index(producer, "-map")
	if firstMap < 0 || firstMap+1 >= len(producer) {
		t.Fatalf("recorded producer has no selected video: %v", producer)
	}
	// Replay the actual producer's input clocks, decoder thread limits, seek,
	// output window, and selected video. Replace only the lossy encoding and
	// muxing with raw frame hashes; progress must not share the hash output.
	var args []string
	for i := 0; i < firstMap+2; i++ {
		if producer[i] == "-progress" {
			i++
			continue
		}
		args = append(args, producer[i])
	}
	for _, option := range []string{"-vf", "-fps_mode", "-enc_time_base:v"} {
		index := slices.Index(producer, option)
		if index < firstMap || index+1 >= len(producer) {
			t.Fatalf("recorded producer has no video transform %s: %v", option, producer)
		}
		args = append(args, option, producer[index+1])
	}
	args = append(args, "-an", "-sn", "-dn", "-xerror", "-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "yuv420p",
		"-f", "framehash", "-hash", "sha256", "pipe:1")
	assertRecordedProgressiveVideoInputThreads(t, args, threads)
	command := exec.CommandContext(ctx, ffmpeg, args...)
	command.ExtraFiles = []*os.File{input}
	var output, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err != nil || diagnostic.Len() != 0 {
		t.Fatalf("recorded producer pre-encode replay failed: %v: %s", err, diagnostic.String())
	}
	return output.Bytes()
}

func assertProgressiveVideoSeekFullColorSourceWindow(t *testing.T, ctx context.Context, ffmpeg, ffprobe, source string, plan Plan, outputs ...recordedProgressiveVideoSeek) {
	t.Helper()
	const frameBytes = 160 * 90 * 3 / 2
	decode := func(path string) []byte {
		return progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1", "-filter_threads", "1",
			"-i", path, "-map", "0:v:0", "-an", "-sn", "-dn", "-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "yuv420p",
			"-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
	}
	sourceFacts, sourcePixels := probeProgressiveVideoFrames(t, ctx, ffprobe, source), decode(source)
	if len(sourceFacts.video) != 150 || len(sourcePixels) != len(sourceFacts.video)*frameBytes {
		t.Fatal("full-color source oracle lost its complete fixed-size fixture")
	}
	first := 0
	boundary := float64(plan.SourceFormatStartTicks+plan.StartTicks) / float64(ticksPerSecond)
	for first < len(sourceFacts.video) && sourceFacts.video[first].time(t) < boundary-.000001 {
		first++
	}
	for _, output := range outputs {
		pixels := decode(output.path)
		if len(output.facts.video) != len(sourceFacts.video)-first || len(pixels) != len(output.facts.video)*frameBytes {
			t.Fatal("full-color output does not cover every selected source frame")
		}
		// Keep the established source-content and neighbor-separation bounds,
		// and apply them to every output frame and all three color planes.
		for index := range output.facts.video {
			actual := pixels[index*frameBytes : (index+1)*frameBytes]
			expectedIndex := first + index
			expected := sourcePixels[expectedIndex*frameBytes : (expectedIndex+1)*frameBytes]
			mse := progressiveVideoMSE(actual, expected)
			if mse > .001 {
				t.Fatalf("full-color frame %d differs from source frame %d: normalized MSE %.6f", index, expectedIndex, mse)
			}
			for _, neighbor := range []int{expectedIndex - 1, expectedIndex + 1} {
				if neighbor < 0 || neighbor >= len(sourceFacts.video) {
					continue
				}
				wrong := sourcePixels[neighbor*frameBytes : (neighbor+1)*frameBytes]
				if other := progressiveVideoMSE(actual, wrong); other <= mse*2 {
					t.Fatalf("full-color frame %d does not identify source frame %d: correct MSE %.6f, neighbor %d MSE %.6f", index, expectedIndex, mse, neighbor, other)
				}
			}
		}
	}
}

func assertProgressiveVideoSeekOutputsEqual(t *testing.T, linear, actual recordedProgressiveVideoSeek) {
	t.Helper()
	if linear.threads != actual.threads {
		t.Fatalf("compared outputs used different thread limits: linear=%d actual=%d", linear.threads, actual.threads)
	}
	if !slices.Equal(linear.facts.video, actual.facts.video) {
		t.Fatalf("video frame timestamps or durations differ from linear decoding: linear=%+v actual=%+v", linear.facts.video, actual.facts.video)
	}
	if linear.threads > 1 {
		// Multithreaded lossy rate control can change even repeated linear
		// encodes. Exact equality belongs before encoding, including the full
		// framehash header, timestamps, sizes, and all decoded color planes.
		if len(linear.preencode) == 0 || !bytes.Equal(linear.preencode, actual.preencode) {
			t.Fatalf("complete pre-encode video differs from linear decoding: linear=%s actual=%s", linear.preencode, actual.preencode)
		}
	} else if !slices.Equal(linear.hashes, actual.hashes) {
		t.Fatalf("decoded full-color video frames differ from linear decoding: linear=%v actual=%v", linear.hashes, actual.hashes)
	}
	if !slices.Equal(linear.facts.audio, actual.facts.audio) {
		t.Fatal("audio timestamps or decoded sample counts differ from linear decoding")
	}
	if !bytes.Equal(linear.pcm, actual.pcm) {
		t.Fatalf("audio decoder history changed complete PCM output: linear=%d bytes actual=%d bytes", len(linear.pcm), len(actual.pcm))
	}
}

func differentProgressiveVideoSeekDigest(value string) string {
	first := byte('0')
	if value[0] == first {
		first = '1'
	}
	return string(first) + value[1:]
}
