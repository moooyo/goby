package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func videoSeekLinuxTestHashOutput() string {
	var output strings.Builder
	output.WriteString("#format: frame checksums\n#version: 2\n#hash: SHA256\n")
	for number, codec := range []string{"h264", "rawvideo", "h264", "h264", "h264"} {
		fmt.Fprintf(&output, "#tb %d: 1/10000000\n#media_type %d: video\n#codec_id %d: %s\n#dimensions %d: 64x64\n", number, number, number, codec, number)
	}
	fmt.Fprintf(&output, "0, 10000000, 10000000, 1, 64, %s\n2, 10000000, 10000000, 1, 20, %s\n1, 10000000, 10000000, 1, 6144, %s\n",
		strings.Repeat("c", 64), strings.Repeat("e", 64), strings.Repeat("d", 64))
	fmt.Fprintf(&output, "3, 10000000, 10000000, 1, 100, %s\n", strings.Repeat("f", 64))
	return output.String()
}

func videoSeekLinuxTestSource(t *testing.T) (*os.File, Info) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "source-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Write([]byte("a controlled regular source")); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	return file, Info{FormatStartKnown: true, DurationTicks: 40_000_000, Streams: []Stream{
		{Index: 2, CodecType: "video", Codec: "h264", Width: 64, Height: 64, PixelFormat: "yuv420p", BitDepth: 8, TimeBase: "1/10000000"},
	}}
}

func videoSeekLinuxTestTool(t *testing.T, prefix, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg-helper")
	var proofOutput strings.Builder
	for _, line := range strings.SplitAfter(output, "\n") {
		if strings.HasPrefix(line, "3,") || strings.HasPrefix(line, "4,") {
			continue
		}
		if !strings.HasPrefix(line, "#") || !strings.Contains(line, " 3:") && !strings.Contains(line, " 4:") {
			proofOutput.WriteString(line)
		}
	}
	program := "#!/bin/sh\nset -eu\n" +
		"if [ -n \"${GOBY_DATABASE_URL-}${FFREPORT-}${LD_PRELOAD-}\" ]; then exit 91; fi\n" +
		"if [ \"${1-}\" = '-version' ]; then printf 'controlled ffmpeg version\\n'; exit 0; fi\n" +
		prefix + "\ncase \" $* \" in *' -ss '*) cat <<'VIDEO_SEEK_HASH_EOF'\n" + proofOutput.String() +
		"VIDEO_SEEK_HASH_EOF\n;; *) cat <<'VIDEO_SEEK_HASH_EOF'\n" + output + "VIDEO_SEEK_HASH_EOF\n;; esac\n"
	if err := os.WriteFile(path, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVideoSeekAnalyzeAndVerifyBorrowDescriptorWithIsolatedEnvironment(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	executable := videoSeekLinuxTestTool(t, "dd if=/proc/self/fd/3 of=/dev/null bs=1 count=1 2>/dev/null", videoSeekLinuxTestHashOutput())
	t.Setenv("GOBY_DATABASE_URL", "must-not-reach-parser")
	t.Setenv("FFREPORT", "file=must-not-write-report")
	t.Setenv("LD_PRELOAD", "/must-not-load-untrusted-library")
	indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, info)
	if err != nil || len(indexes) != 1 || indexes[0].StreamIndex != 2 || len(indexes[0].Entries) != 1 {
		t.Fatalf("bounded index analysis failed: %+v, %v", indexes, err)
	}
	candidate, err := SelectVideoSeekCandidate(indexes[0], 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyVideoSeekCandidate(context.Background(), executable, file, candidate, 1)
	if err != nil || !verification.Verified || verification.InputSeekTicks != 10_000_000 {
		t.Fatalf("matching preflight was not verified: %+v, %v", verification, err)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 {
		t.Fatalf("analysis closed or moved the borrowed source: %d, %v", position, err)
	}
	indexes[0].Entries[0].DecodedSHA256 = strings.Repeat("f", 64)
	candidate, err = SelectVideoSeekCandidate(indexes[0], 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyVideoSeekCandidate(context.Background(), executable, file, candidate, 1)
	if err != nil || verification.Verified {
		t.Fatalf("mismatched decoded evidence authorized a seek: %+v, %v", verification, err)
	}
}

func TestVideoSeekAnalysisUnsupportedEvidencePreservesTheSource(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	for _, body := range []string{"exit 1", "printf '[error] controlled decoder error\\n' >&2", "printf '[fatal] controlled decoder error\\n' >&2"} {
		executable := videoSeekLinuxTestTool(t, body, videoSeekLinuxTestHashOutput())
		indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, info)
		if err != nil || len(indexes) != 0 {
			t.Fatalf("optional failed evidence was not a fallback: %+v, %v", indexes, err)
		}
	}
	executable := videoSeekLinuxTestTool(t, "printf '[warning] 4 bytes left at end of AVCC header.\\n' >&2", videoSeekLinuxTestHashOutput())
	indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, info)
	if err != nil || len(indexes) != 1 {
		t.Fatalf("ordinary supported FFmpeg warning discarded evidence: %+v, %v", indexes, err)
	}
}

func TestVideoSeekAnalysisResourceGateDoesNotLaunchOptionalTools(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	directory := t.TempDir()
	executable := filepath.Join(directory, "unexpected-tool")
	program := "#!/bin/sh\n: > \"$(dirname \"$0\")/launched\"\nexit 1\n"
	if err := os.WriteFile(executable, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Info){
		"display pixels": func(i *Info) { i.Streams[0].Width, i.Streams[0].Height = 32768, 32768 },
		"unknown depth":  func(i *Info) { i.Streams[0].BitDepth = 0 },
		"wrong depth":    func(i *Info) { i.Streams[0].BitDepth = 10 },
		"pixel format":   func(i *Info) { i.Streams[0].PixelFormat = "yuv444p" },
		"duration":       func(i *Info) { i.DurationTicks = MaxVideoSeekDurationTicks + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			unsupported := info
			unsupported.Streams = append([]Stream(nil), info.Streams...)
			change(&unsupported)
			indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, unsupported)
			if err != nil || len(indexes) != 0 {
				t.Fatalf("optional resource rejection changed source availability: %v", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "launched")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("resource rejection launched the optional executable")
			}
		})
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 {
		t.Fatal("resource rejection changed the borrowed descriptor")
	}
}

func TestVideoSeekAnalysisMutationAndCancellationRemainErrors(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	executable := videoSeekLinuxTestTool(t, "printf x >>/proc/self/fd/3", videoSeekLinuxTestHashOutput())
	indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, info)
	if err == nil || !strings.Contains(err.Error(), "changed") || len(indexes) != 0 {
		t.Fatalf("source mutation was hidden as a cache miss: %+v, %v", indexes, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AnalyzeVideoSeekIndexes(ctx, executable, file, info); !errors.Is(err, context.Canceled) {
		t.Fatalf("analysis cancellation returned %v", err)
	}
	if _, err := VerifyVideoSeekCandidate(ctx, executable, file, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("proof cancellation returned %v", err)
	}
}

func TestVideoSeekCancellationInterruptsAnActiveScan(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	ready := filepath.Join(t.TempDir(), "ready")
	executable := videoSeekLinuxTestTool(t, "printf ready > '"+ready+"'\nsleep 30 &\nwait", videoSeekLinuxTestHashOutput())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := AnalyzeVideoSeekIndexes(ctx, executable, file, info)
		result <- err
	}()
	deadline := time.After(5 * time.Second)
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("scan exited before its controlled child started: %v", err)
		case <-deadline:
			t.Fatal("scan never started its controlled child")
		case <-poll.C:
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("active scan cancellation became a cache miss: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled scan retained a live pipe or process")
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 {
		t.Fatalf("cancelled scan changed the borrowed source: %d, %v", position, err)
	}
}

func TestVideoSeekToolAndSourceIdentityInvalidateRewrittenObjects(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	executable := videoSeekLinuxTestTool(t, "true", videoSeekLinuxTestHashOutput())
	indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, info)
	if err != nil || len(indexes) != 1 {
		t.Fatalf("initial analysis failed: %+v, %v", indexes, err)
	}
	candidate, err := SelectVideoSeekCandidate(indexes[0], 20_000_000)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	program = []byte(strings.Replace(string(program), "true\n", ":   \n", 1))
	if err := os.WriteFile(executable, program, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(executable, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyVideoSeekCandidate(context.Background(), executable, file, candidate, 1)
	if err != nil || verification.Verified {
		t.Fatalf("replaced executable retained stale authorization: %+v, %v", verification, err)
	}
	if _, err := file.WriteAt([]byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyVideoSeekCandidate(context.Background(), executable, file, candidate, 1)
	if err != nil || verification.Verified {
		t.Fatalf("stale source identity retained authorization: %+v, %v", verification, err)
	}
}

func TestVideoSeekVerificationRetriesIndexedDTSAndReturnsTheTestedArgument(t *testing.T) {
	file, info := videoSeekLinuxTestSource(t)
	attemptLog := filepath.Join(t.TempDir(), "attempts")
	prefix := "previous=\nseek=\nfor argument in \"$@\"; do\n" +
		"if [ \"$previous\" = '-ss' ]; then seek=$argument; fi\nprevious=$argument\ndone\n" +
		"if [ -n \"$seek\" ]; then printf '%s\\n' \"$seek\" >> '" + attemptLog + "'; fi\n" +
		"if [ \"$seek\" = '2.0000000' ]; then printf '[error] controlled preroll failure\\n' >&2; exit 1; fi"
	output := strings.ReplaceAll(videoSeekLinuxTestHashOutput(), "10000000, 10000000", "10000000, 20000000")
	executable := videoSeekLinuxTestTool(t, prefix, output)
	indexes, err := AnalyzeVideoSeekIndexes(context.Background(), executable, file, info)
	if err != nil || len(indexes) != 1 || indexes[0].Entries[0].PTS != 20_000_000 || indexes[0].Entries[0].DTS != 10_000_000 {
		t.Fatalf("index did not retain distinct PTS and DTS proposals: %+v, %v", indexes, err)
	}
	encoded, err := SelectVideoSeekCandidate(indexes[0], 30_000_000)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyVideoSeekCandidate(context.Background(), executable, file, encoded, 1)
	if err != nil || !verification.Verified || verification.InputSeekTicks != 10_000_000 {
		t.Fatalf("DTS proposal did not preserve the actually tested argument: %+v, %v", verification, err)
	}
	observed, err := os.ReadFile(attemptLog)
	if err != nil || string(observed) != "2.0000000\n1.0000000\n" {
		t.Fatalf("proof did not retry the exact indexed proposals: %q, %v", observed, err)
	}
}

func TestProbeVideoSeekRequiresExplicitAnalysisOptIn(t *testing.T) {
	file, _ := videoSeekLinuxTestSource(t)
	probe := filepath.Join(t.TempDir(), "ffprobe-helper")
	metadata := `{"format":{"format_name":"mov,mp4","duration":"4","start_time":"0"},"streams":[{"index":2,"codec_name":"h264","codec_type":"video","width":64,"height":64,"pix_fmt":"yuv420p","bits_per_raw_sample":"8","time_base":"1/10000000"}]}`
	if err := os.WriteFile(probe, []byte("#!/bin/sh\nprintf '%s\\n' '"+metadata+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ffmpeg := videoSeekLinuxTestTool(t, "true", videoSeekLinuxTestHashOutput())
	for _, enabled := range []bool{false, true} {
		info, err := (Prober{FFprobePath: probe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: enabled, Timeout: time.Second}).ProbeFile(context.Background(), file)
		if err != nil || (len(info.VideoSeekIndexes) == 1) != enabled || info.ProbeVersion != CurrentProbeVersion {
			t.Fatalf("explicit seek analysis=%v returned %+v, %v", enabled, info, err)
		}
	}
	info, err := (Prober{FFprobePath: probe, FFmpegPath: "/missing/ffmpeg", AnalyzeVideoSeek: true, Timeout: time.Second}).ProbeFile(context.Background(), file)
	if err != nil || len(info.VideoSeekIndexes) != 0 || len(info.Streams) != 1 {
		t.Fatalf("missing optional executable hid the playable source: %+v, %v", info, err)
	}
}
