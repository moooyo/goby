package media

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This harness calls only pre-existing APIs and can be copied unchanged into
// the buffered baseline. The synthetic extraction tool exercises the complete
// production runner and envelope parser; actual FFmpeg syntax tests remain a
// separate compatibility gate and must not be inferred from this measurement.
func dolbyVisionResourceCorpus(tb testing.TB, targetBytes int64) (*os.File, string, int64, int64) {
	tb.Helper()
	directory := tb.TempDir()
	path := filepath.Join(directory, "corpus.hevc")
	file, err := os.Create(path)
	if err != nil {
		tb.Fatal(err)
	}
	writer := bufio.NewWriterSize(file, 128<<10)
	unit := dolbyVisionAccessUnitFixture(true)
	var written, frames int64
	for written < targetBytes {
		if _, err := writer.Write(unit); err != nil {
			tb.Fatal(err)
		}
		written += int64(len(unit))
		frames++
	}
	if err := writer.Flush(); err != nil {
		tb.Fatal(err)
	}
	if err := file.Close(); err != nil {
		tb.Fatal(err)
	}
	input, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { input.Close() })
	tool := filepath.Join(directory, "extract-tool")
	program := "#!/bin/sh\nset -eu\ncase \"$*\" in\n*dovi_rpu=compression=none*) exit 0 ;;\n*) exec cat /proc/self/fd/3 ;;\nesac\n"
	if err := os.WriteFile(tool, []byte(program), 0700); err != nil {
		tb.Fatal(err)
	}
	return input, tool, written, frames
}

func dolbyVisionResourceInfo() Info {
	return Info{Streams: []Stream{{Index: 0, Codec: "hevc", CodecType: "video", DolbyVision: &DolbyVisionMetadata{
		Profile: 8, RPUPresent: true, BLPresent: true, MetadataCompression: "none",
	}}}}
}

func BenchmarkDolbyVisionRPUProbeLargeCorpus(b *testing.B) {
	for _, size := range []int64{1 << 20, 64 << 20} {
		b.Run(strconv.FormatInt(size>>20, 10)+"MiB", func(b *testing.B) {
			input, tool, written, frames := dolbyVisionResourceCorpus(b, size)
			info := dolbyVisionResourceInfo()
			b.SetBytes(written)
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				got, err := runDolbyVisionRPUProbe(context.Background(), tool, input, written, info)
				if err != nil || !got.Streams[0].DolbyVision.RPUVerified || got.Streams[0].DolbyVision.RPUFrameCount != frames {
					b.Fatalf("large corpus was not fully verified: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestDolbyVisionStreamingResourceProfile(t *testing.T) {
	if os.Getenv("GOBY_TEST_DOLBY_VISION_STREAMING_PERFORMANCE") != "1" {
		t.Skip("the explicit streaming resource profile is required")
	}
	input, tool, written, frames := dolbyVisionResourceCorpus(t, 64<<20)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestDolbyVisionStreamingResourceProfileHelper$", "-test.v")
	command.Env = append(os.Environ(), "GOBY_DOLBY_VISION_RESOURCE_HELPER=1", "GOBY_DOLBY_VISION_RESOURCE_INPUT="+input.Name(),
		"GOBY_DOLBY_VISION_RESOURCE_TOOL="+tool, "GOBY_DOLBY_VISION_RESOURCE_BYTES="+strconv.FormatInt(written, 10),
		"GOBY_DOLBY_VISION_RESOURCE_FRAMES="+strconv.FormatInt(frames, 10))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated profile failed: %v\n%s", err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "dolby_vision_resource_profile=") {
			t.Log(line)
			return
		}
	}
	t.Fatalf("isolated profile did not report resource evidence: %s", output)
}

func TestDolbyVisionStreamingResourceProfileHelper(t *testing.T) {
	if os.Getenv("GOBY_DOLBY_VISION_RESOURCE_HELPER") != "1" {
		return
	}
	input, err := os.Open(os.Getenv("GOBY_DOLBY_VISION_RESOURCE_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	written, err := strconv.ParseInt(os.Getenv("GOBY_DOLBY_VISION_RESOURCE_BYTES"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	frames, err := strconv.ParseInt(os.Getenv("GOBY_DOLBY_VISION_RESOURCE_FRAMES"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var beforeUsage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &beforeUsage); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	got, err := runDolbyVisionRPUProbe(context.Background(), os.Getenv("GOBY_DOLBY_VISION_RESOURCE_TOOL"), input, written, dolbyVisionResourceInfo())
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		t.Fatal(err)
	}
	if err != nil || !got.Streams[0].DolbyVision.RPUVerified || got.Streams[0].DolbyVision.RPUFrameCount != frames {
		t.Fatalf("isolated corpus was not fully verified: %+v, %v", got, err)
	}
	encoded, err := json.Marshal(map[string]any{
		"corpus_bytes": written, "verified_frames": frames, "elapsed_ns": elapsed.Nanoseconds(),
		"allocated_bytes": after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs,
		"heap_after_bytes": after.HeapAlloc, "max_rss_kib": usage.Maxrss,
		"user_cpu_us":   usage.Utime.Sec*1_000_000 + usage.Utime.Usec - beforeUsage.Utime.Sec*1_000_000 - beforeUsage.Utime.Usec,
		"system_cpu_us": usage.Stime.Sec*1_000_000 + usage.Stime.Usec - beforeUsage.Stime.Sec*1_000_000 - beforeUsage.Stime.Usec,
	})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("dolby_vision_resource_profile=%s\n", encoded)
}
