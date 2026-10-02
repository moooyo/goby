//go:build linux

package transcode

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const generatedBoundsHelperPrefix = "goby-generated-bounds-test-helper-"

func generatedBoundsTestFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}

func generatedBoundsHelperExecutable(t *testing.T, mode string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), generatedBoundsHelperPrefix+mode)
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	return link
}

func TestMeasureGeneratedSegmentBoundsBorrowsWholeInputsAndBindsDigests(t *testing.T) {
	t.Setenv("GOBY_DATABASE_URL", "synthetic-database-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-cloud-secret")
	t.Setenv("FFREPORT", "file=unexpected-report.txt")
	for _, withInit := range []bool{false, true} {
		t.Run(fmt.Sprintf("initialization-%t", withInit), func(t *testing.T) {
			media := []byte("the entire generated media fragment")
			segment := generatedBoundsTestFile(t, media)
			if _, err := segment.Seek(5, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			var initialization *os.File
			initData := []byte("entire initialization|")
			if withInit {
				initialization = generatedBoundsTestFile(t, initData)
				if _, err := initialization.Seek(3, io.SeekStart); err != nil {
					t.Fatal(err)
				}
			}
			got, err := MeasureGeneratedSegmentBounds(context.Background(), generatedBoundsHelperExecutable(t, "valid"), initialization, segment, true)
			if err != nil || got.Video.PacketCount != 3 || got.Video.EndPTS != 3 || got.SegmentSHA256 != sha256.Sum256(media) {
				t.Fatalf("complete bounds = %+v, %v", got, err)
			}
			if withInit && got.InitializationSHA256 != sha256.Sum256(initData) || !withInit && got.InitializationSHA256 != ([32]byte{}) {
				t.Fatalf("initialization digest = %x", got.InitializationSHA256)
			}
			if offset, err := segment.Seek(0, io.SeekCurrent); err != nil || offset != 5 {
				t.Fatalf("borrowed media offset = %d, %v", offset, err)
			}
			if withInit {
				if offset, err := initialization.Seek(0, io.SeekCurrent); err != nil || offset != 3 {
					t.Fatalf("borrowed initialization offset = %d, %v", offset, err)
				}
			}
		})
	}
}

func TestMeasureGeneratedSegmentBoundsRejectsFailedAndIncompleteProbe(t *testing.T) {
	for mode, want := range map[string]error{
		"truncated":            ErrTimelineProbe,
		"trailing":             ErrTimelineProbe,
		"diagnostic":           ErrTimelineProbe,
		"failure":              ErrTimelineProbe,
		"oversized-record":     ErrTimelineLimit,
		"no-input-consumption": ErrTimelineProbe,
	} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("entire fragment")
			if mode == "no-input-consumption" {
				data = make([]byte, 8<<20)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := MeasureGeneratedSegmentBounds(ctx, generatedBoundsHelperExecutable(t, mode), nil, generatedBoundsTestFile(t, data), true)
			if !errors.Is(err, want) {
				t.Fatalf("probe error = %v, want %v", err, want)
			}
		})
	}
}

func TestMeasureGeneratedSegmentBoundsRejectsInvalidBorrowedFiles(t *testing.T) {
	for _, kind := range []string{"nil", "closed", "directory", "pipe", "empty", "hardlink", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			var input *os.File
			want := ErrInvalidInput
			switch kind {
			case "nil":
			case "closed":
				input = generatedBoundsTestFile(t, []byte("segment"))
				_ = input.Close()
			case "directory":
				var err error
				input, err = os.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = input.Close() })
			case "pipe":
				var writer *os.File
				var err error
				input, writer, err = os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = input.Close(); _ = writer.Close() })
			case "empty":
				input = generatedBoundsTestFile(t, nil)
			case "hardlink":
				input = generatedBoundsTestFile(t, []byte("segment"))
				if err := os.Link(input.Name(), filepath.Join(t.TempDir(), "alias")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				input = generatedBoundsTestFile(t, []byte("segment"))
				if err := os.Truncate(input.Name(), maxGeneratedBoundsInputBytes+1); err != nil {
					t.Fatal(err)
				}
				want = ErrTimelineLimit
			}
			_, err := MeasureGeneratedSegmentBounds(context.Background(), "unused", nil, input, true)
			if !errors.Is(err, want) {
				t.Fatalf("borrowed %s = %v", kind, err)
			}
		})
	}
}

func TestMeasureGeneratedSegmentBoundsDetectsSameSizeMutationWithRestoredMtime(t *testing.T) {
	input := generatedBoundsTestFile(t, []byte("placeholder"))
	if err := os.WriteFile(input.Name(), []byte(input.Name()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := MeasureGeneratedSegmentBounds(context.Background(), generatedBoundsHelperExecutable(t, "mutate"), nil, input, true)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mutated input = %v", err)
	}
}

func TestMeasureGeneratedSegmentBoundsCancellationRetiresDescendants(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "bounds-child.pid")
	input := generatedBoundsTestFile(t, []byte(pidFile))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan error, 1)
	executable := generatedBoundsHelperExecutable(t, "parent")
	go func() {
		_, err := MeasureGeneratedSegmentBounds(ctx, executable, nil, input, true)
		done <- err
	}()
	var childPID int
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			childPID, _ = strconv.Atoi(string(data))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled bounds probe = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bounds cancellation did not join the child and copiers")
	}
	if childPID == 0 {
		t.Fatal("bounds descendant did not start")
	}
	assertProcessStopped(t, childPID)
}

func TestMeasureGeneratedSegmentBoundsHonorsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := MeasureGeneratedSegmentBounds(ctx, generatedBoundsHelperExecutable(t, "stall"), nil, generatedBoundsTestFile(t, []byte("segment")), true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe deadline = %v", err)
	}
}

func init() {
	if mode, ok := strings.CutPrefix(filepath.Base(os.Args[0]), generatedBoundsHelperPrefix); ok {
		os.Exit(runGeneratedBoundsHelper(mode))
	}
}

func runGeneratedBoundsHelper(mode string) int {
	if mode == "child" {
		if len(os.Args) != 2 || os.WriteFile(os.Args[1], []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			return 80
		}
		for {
			time.Sleep(time.Second)
		}
	}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOBY_") || strings.HasPrefix(entry, "AWS_") || strings.HasPrefix(entry, "FFREPORT=") {
			return 81
		}
	}
	if !generatedBoundsHelperArguments(os.Args[1:]) {
		return 82
	}
	if mode != "no-input-consumption" {
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			return 83
		}
		if mode == "mutate" {
			path := strings.TrimSuffix(string(input), "\n")
			info, err := os.Stat(path)
			if err != nil || len(input) == 0 {
				return 84
			}
			input[0] ^= 1
			if os.WriteFile(path, input, 0600) != nil || os.Chtimes(path, info.ModTime(), info.ModTime()) != nil {
				return 85
			}
		}
		if mode == "parent" {
			child := exec.Command("/proc/self/exe")
			child.Args = []string{generatedBoundsHelperPrefix + "child", string(input)}
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if child.Start() != nil {
				return 86
			}
			_ = child.Wait()
			return 87
		}
	}
	packets := strings.Join([]string{
		generatedBoundsPacketJSON(0, 0, -2, 1, "K__"),
		generatedBoundsPacketJSON(0, 2, -1, 1, "___"),
		generatedBoundsPacketJSON(0, 1, 0, 1, "___"),
	}, ",")
	document := generatedBoundsDocument(packets, generatedBoundsVideoStream)
	switch mode {
	case "valid", "mutate", "no-input-consumption":
		fmt.Fprint(os.Stdout, document)
	case "truncated":
		fmt.Fprint(os.Stdout, strings.TrimSuffix(document, "}"))
	case "trailing":
		fmt.Fprint(os.Stdout, document+`{}`)
	case "diagnostic":
		fmt.Fprint(os.Stdout, document)
		fmt.Fprint(os.Stderr, "synthetic parse failure")
	case "failure":
		fmt.Fprint(os.Stdout, document)
		return 88
	case "oversized-record":
		fmt.Fprint(os.Stdout, `{"packets":[{"unknown":"`+strings.Repeat("x", maxGeneratedBoundsRecordBytes+1))
	case "stall":
		for {
			time.Sleep(time.Second)
		}
	default:
		return 89
	}
	return 0
}

func generatedBoundsHelperArguments(args []string) bool {
	seenPackets, seenStreams, seenEntries := false, false, false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-show_packets":
			seenPackets = true
		case "-show_streams":
			seenStreams = true
		case "-read_intervals", "-select_streams":
			return false
		case "-show_entries":
			index++
			if index == len(args) || !strings.Contains(args[index], "pts,dts,duration") || !strings.Contains(args[index], "skip_samples,discard_padding") {
				return false
			}
			seenEntries = true
		default:
			index++
			if index == len(args) {
				return false
			}
		}
	}
	return seenPackets && seenStreams && seenEntries
}

func TestMeasureGeneratedSegmentBoundsRealClosedHLSSegments(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set GOBY_FFMPEG and GOBY_FFPROBE for real media verification")
	}
	for _, segmentType := range []string{"mpegts", "fmp4"} {
		t.Run(segmentType, func(t *testing.T) {
			directory := t.TempDir()
			extension := "ts"
			if segmentType == "fmp4" {
				extension = "m4s"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-threads", "1", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=12", "-f", "lavfi", "-i", "sine=frequency=800:sample_rate=48000",
				"-t", "2", "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-preset", "ultrafast", "-bf", "0", "-g", "12",
				"-c:a", "aac", "-threads", "1", "-f", "hls", "-hls_time", "1", "-hls_list_size", "0", "-hls_flags", "independent_segments",
				"-hls_segment_type", segmentType, "-hls_segment_filename", filepath.Join(directory, "seg-%03d."+extension), filepath.Join(directory, "main.m3u8"))
			command.Env, command.Dir = processEnvironment(), directory
			if data, err := command.CombinedOutput(); err != nil {
				t.Fatalf("generate closed media: %v: %s", err, data)
			}
			var initialization *os.File
			if segmentType == "fmp4" {
				var err error
				initialization, err = os.Open(filepath.Join(directory, "init.mp4"))
				if err != nil {
					t.Fatal(err)
				}
				defer initialization.Close()
			}
			for index := 0; index < 2; index++ {
				path := filepath.Join(directory, fmt.Sprintf("seg-%03d.%s", index, extension))
				segment, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				bounds, probeErr := MeasureGeneratedSegmentBounds(ctx, ffprobe, initialization, segment, true)
				_ = segment.Close()
				if probeErr != nil || !bounds.Video.Present || !bounds.Audio.Present || bounds.Video.PacketCount != 12 || bounds.Video.EndPTS <= bounds.Video.FirstPTS {
					t.Fatalf("closed %s segment %d bounds = %+v, %v", segmentType, index, bounds, probeErr)
				}
			}
		})
	}
}
