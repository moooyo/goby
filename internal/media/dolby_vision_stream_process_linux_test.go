package media

import (
	"bytes"
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

func TestDolbyVisionStreamingProcessBudgetsDiagnosticsAndDescriptor(t *testing.T) {
	for _, test := range []struct {
		name, body string
		limit      int
		wantErr    error
		wantStderr string
	}{
		{"exact stdout limit", "printf '1234'", 4, nil, ""},
		{"stdout limit", "printf '12345'", 4, ErrOutputLimit, ""},
		{"stderr retained", "printf '1234'; printf 'warning' >&2", 4, nil, "warning"},
		{"stderr limit", "dd if=/dev/zero bs=65537 count=1 >&2 2>/dev/null", 4, ErrOutputLimit, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			tool := analysisProcessTestTool(t, test.body)
			var stdout bytes.Buffer
			output, err := runLimitedFilesStreamOutput(context.Background(), 5*time.Second, test.limit, tool, nil, &stdout)
			if !errors.Is(err, test.wantErr) || string(output.stderr) != test.wantStderr || len(output.stdout) != 0 {
				t.Fatalf("stream output result: %+v, %v; want %v stderr %q", output, err, test.wantErr, test.wantStderr)
			}
		})
	}
	input, err := os.CreateTemp(t.TempDir(), "authorized-source")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	const payload = "authorized descriptor data"
	if _, err := input.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	tool := analysisProcessTestTool(t, "cat /proc/self/fd/3")
	var stdout bytes.Buffer
	if _, err := runLimitedFilesStreamOutput(context.Background(), 5*time.Second, len(payload), tool, []*os.File{input}, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != payload {
		t.Fatalf("descriptor bytes differ: %q", stdout.String())
	}
	if offset, err := input.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("stream runner moved or closed borrowed descriptor: %d, %v", offset, err)
	}
}

type dolbyVisionStreamFailureWriter struct{ err error }

func (writer dolbyVisionStreamFailureWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestDolbyVisionStreamingProcessFailuresReapActualProcessGroup(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout", "stdout limit", "stderr limit", "writer failure", "leader exit"} {
		t.Run(mode, func(t *testing.T) {
			readyPath := filepath.Join(t.TempDir(), "ready")
			action := ":"
			writer := io.Writer(newDolbyVisionRPUStream(context.Background()))
			limit, timeout := 64, 30*time.Second
			wantErr := error(context.Canceled)
			writeErr := errors.New("test stream consumer failure")
			switch mode {
			case "timeout":
				action, timeout, wantErr = "printf '9'", time.Second, context.DeadlineExceeded
			case "stdout limit":
				action, wantErr = "printf '9'; dd if=/dev/zero bs=4096 count=1024 2>/dev/null", ErrOutputLimit
			case "stderr limit":
				action, wantErr = "dd if=/dev/zero bs=65537 count=1 >&2 2>/dev/null", ErrOutputLimit
			case "writer failure":
				action, writer, wantErr = "printf 'data'", dolbyVisionStreamFailureWriter{err: writeErr}, writeErr
			case "leader exit":
				action, wantErr = "exit 0", nil
			}
			tool := analysisProcessTestTool(t, "sleep 60 &\nchild=$!\nprintf '%s %s\\n' \"$$\" \"$child\" > '"+
				strings.ReplaceAll(readyPath, "'", "'\\''")+"'\n"+action+"\nwait \"$child\"")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				_, err := runLimitedFilesStreamOutput(ctx, timeout, limit, tool, nil, writer)
				result <- err
				close(finished)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("stream runner did not join its child during cleanup")
				}
			})
			var ids analysisProcessTestPIDs
			deadline := time.Now().Add(5 * time.Second)
			for {
				data, err := os.ReadFile(readyPath)
				if err == nil {
					if n, err := fmt.Sscanf(string(data), "%d %d", &ids.leader, &ids.child); err == nil && n == 2 && strings.HasSuffix(string(data), "\n") && ids.leader > 1 && ids.child > 1 && ids.leader != ids.child {
						break
					}
				} else if !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
				if time.Now().After(deadline) {
					t.Fatal("stream test process did not report readiness")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, wantErr) {
					t.Fatalf("stream process %s returned %v; want %v", mode, err, wantErr)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("stream failure returned before joining its process group or did not retire")
			}
			analysisProcessTestAssertRetired(t, ids)
		})
	}
}

func TestDolbyVisionStreamingProbePreservesFailurePrecedenceAfterInvalidPrefix(t *testing.T) {
	for _, test := range []struct {
		name, stderr string
		exitCode     int
		wantReason   string
	}{
		{"syntax failure", "", 0, dolbyVisionRPUInvalidScan},
		{"stderr wins", "decoder warning", 0, dolbyVisionRPUDecoderError},
		{"process failure wins", "", 7, dolbyVisionRPUScanFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			executable := dolbyVisionProcessFixture(t, t.TempDir(), "process", []byte{9}, test.stderr, test.exitCode)
			file, err := os.CreateTemp(t.TempDir(), "source")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info := Info{Streams: []Stream{{Index: 0, Codec: "hevc", CodecType: "video", DolbyVision: &DolbyVisionMetadata{
				Profile: 8, RPUPresent: true, BLPresent: true, MetadataCompression: "none",
			}}}}
			got, err := runDolbyVisionRPUProbe(context.Background(), executable, file, 1, info)
			if err != nil || got.Streams[0].DolbyVision.RPUVerified || got.Streams[0].DolbyVision.RPUValidationReason != test.wantReason {
				t.Fatalf("invalid prefix changed failure precedence: %+v, %v", got, err)
			}
		})
	}
}
