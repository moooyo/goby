package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestImageExtractionThreadsActualPreviewsPreserveFramesAndFrozenBuildPolicy(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "source.mkv")
	video := "color=c=red:size=96x64:rate=25,drawbox=x=0:y=0:w=iw:h=ih:color=lime:t=fill:enable='eq(n,1)+eq(n,3)'," +
		"drawbox=x=0:y=0:w=iw:h=ih:color=blue:t=fill:enable='eq(n,2)+eq(n,4)',trim=end_frame=5," +
		"settb=expr=1/1000,setpts='if(eq(N,0),1000,if(eq(N,1),1100,if(eq(N,2),4000,if(eq(N,3),4100,8000))))'"
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", video, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=mono,atrim=end_sample=436800",
		"-map", "0:v:0", "-map", "1:a:0", "-copyts", "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-bf", "2", "-g", "25", "-pix_fmt", "yuv420p", "-c:a", "pcm_s16le", path)
	file, info, streamIndex := previewBuildTestProbe(t, ffprobe, path)
	if info.DurationTicks != 91*TicksPerSecond/10 || !info.FormatStartKnown || info.FormatStartTicks != 0 {
		t.Fatalf("fixture lost its late video start or audio tail: %+v", info)
	}
	var stream Stream
	for _, candidate := range info.Streams {
		if candidate.Index == streamIndex {
			stream = candidate
		}
	}
	actualSource := analysisPreviewReferenceFrameTicks(t, ffprobe, path, info, stream)
	wantSource := []int64{TicksPerSecond, 11 * TicksPerSecond / 10, 4 * TicksPerSecond, 41 * TicksPerSecond / 10, 8 * TicksPerSecond}
	if !reflect.DeepEqual(actualSource, wantSource) {
		t.Fatalf("fixture did not retain its independently decoded source frame PTS: %v", actualSource)
	}
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buildContext, cancelBuild := context.WithCancel(context.Background())
	t.Cleanup(cancelBuild)
	logPath := filepath.Join(directory, "preview-arguments")
	audit := newImageExtractionTestArgumentCapture(t, logPath, cancelBuild)
	wrapper := imageExtractionTestRecordingTool(t, ffmpeg, logPath)
	policies := []struct {
		name    string
		options ImageExtractionThreads
	}{
		{name: "legacy"},
		{name: "automatic", options: ImageExtractionThreads{Configured: true}},
	}
	if runtime.NumCPU() > 1 {
		limited := min(2, runtime.NumCPU()-1)
		policies = append(policies, struct {
			name    string
			options ImageExtractionThreads
		}{name: "limited", options: ImageExtractionThreads{Count: limited, Configured: true}})
	}
	if runtime.NumCPU() <= 16 {
		policies = append(policies, struct {
			name    string
			options ImageExtractionThreads
		}{name: "processor_threshold", options: ImageExtractionThreads{Count: runtime.NumCPU(), Configured: true}})
	}
	widths := []int{240, 320, 400}
	baselineFrames := make([][]PreviewFrame, len(widths))
	baselineSummaries := make([]PreviewAnalysisSummary, len(widths))
	var baselineCommands [][]string
	for policyIndex, policy := range policies {
		if !t.Run(policy.name, func(t *testing.T) {
			extractor := AnalysisExtractor{FFmpegPath: wrapper, FFprobePath: ffprobe, ImageExtractionThreads: policy.options,
				Limits: AnalysisLimits{MaxSourceFrames: 2 * len(actualSource)}}
			err := extractor.WithPreviewBuild(buildContext, file, info, streamIndex, func(extract PreviewAnalysisExtract) error {
				for variant, width := range widths {
					callbacks, preceding := 0, 0
					summary, err := extract(PreviewAnalysisOptions{Width: width, IntervalTicks: 2 * TicksPerSecond}, func(frame PreviewFrame) error {
						nominal := int64(callbacks) * 2 * TicksPerSecond
						for preceding+1 < len(actualSource) && actualSource[preceding+1] <= nominal {
							preceding++
						}
						if callbacks >= 5 || frame.NominalTicks != nominal || frame.ActualTicks != actualSource[preceding] || frame.Width != width ||
							sha256.Sum256(frame.JPEG) != frame.SHA256 {
							return fmt.Errorf("width %d slot %d lost its preceding source picture or JPEG identity", width, callbacks)
						}
						image, err := jpeg.Decode(bytes.NewReader(frame.JPEG))
						if err != nil {
							return err
						}
						if err := analysisGeometryAssertColor(image, image.Bounds().Dx()/2, image.Bounds().Dy()/2, "RGBGB"[callbacks]); err != nil {
							return fmt.Errorf("width %d slot %d changed the selected source color: %w", width, callbacks, err)
						}
						if policyIndex == 0 {
							frame.JPEG = bytes.Clone(frame.JPEG)
							baselineFrames[variant] = append(baselineFrames[variant], frame)
						} else if callbacks >= len(baselineFrames[variant]) || !reflect.DeepEqual(frame, baselineFrames[variant][callbacks]) {
							return fmt.Errorf("width %d slot %d changed under a decoder resource policy", width, callbacks)
						}
						callbacks++
						return nil
					})
					if err != nil {
						return err
					}
					if callbacks != 5 || summary.FrameCount != callbacks || summary.Profile != PreviewAnalysisProfile {
						return fmt.Errorf("width %d lost complete preview slots or its output profile", width)
					}
					if policyIndex == 0 {
						baselineSummaries[variant] = summary
					} else if summary != baselineSummaries[variant] {
						return fmt.Errorf("width %d changed its preview summary under a decoder resource policy", width)
					}
					// The value receiver must retain the admitted choice for all
					// remaining widths even if its caller changes its own copy.
					extractor.ImageExtractionThreads = ImageExtractionThreads{Count: 17, Configured: true}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			commands := audit.commands(t)
			if len(commands) != 4 {
				t.Fatalf("three widths did not share one source audit and three output decodes: %d", len(commands))
			}
			wantDecoder, err := ResolveImageExtractionDecoderThreads(policy.options, runtime.NumCPU())
			if err != nil {
				t.Fatal(err)
			}
			for index, args := range commands {
				decoder, other := imageExtractionTestSplitCommand(t, args)
				if decoder != wantDecoder {
					t.Fatalf("pass %d did not retain its input decoder choice: %d, want %d", index, decoder, wantDecoder)
				}
				if policyIndex == 0 {
					baselineCommands = append(baselineCommands, other)
				} else if !reflect.DeepEqual(other, baselineCommands[index]) {
					t.Fatalf("pass %d changed a non-decoder command stage", index)
				}
			}
			if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 17 {
				t.Fatalf("preview decoding changed the borrowed descriptor offset: %d, %v", offset, err)
			}
		}) {
			return
		}
	}
}

func imageExtractionTestRecordingTool(t *testing.T, executable, logPath string) string {
	t.Helper()
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	path := filepath.Join(filepath.Dir(logPath), "record-preview-ffmpeg")
	program := "#!/bin/sh\nset -eu\nfor argument in \"$@\"; do\n" +
		"  if [ \"$argument\" = \"-debug_ts\" ]; then\n" +
		"    { printf '%s\\000' \"$@\"; printf '\\000'; } > " + quote(logPath) + "\n" +
		"    break\n  fi\ndone\nexec " + quote(executable) + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

type imageExtractionTestArgumentRecord struct {
	arguments []string
	barrier   bool
	err       error
}

type imageExtractionTestArgumentCapture struct {
	file    *os.File
	records chan imageExtractionTestArgumentRecord
	cancel  context.CancelFunc
	stop    chan struct{}
	done    chan struct{}
}

// Analysis children retain RLIMIT_FSIZE=0. A FIFO transports real argv without
// granting the wrapper a regular-file write or changing its executable bytes.
func newImageExtractionTestArgumentCapture(t *testing.T, path string, cancel context.CancelFunc) *imageExtractionTestArgumentCapture {
	t.Helper()
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	capture := &imageExtractionTestArgumentCapture{file: file, records: make(chan imageExtractionTestArgumentRecord, 16), cancel: cancel,
		stop: make(chan struct{}), done: make(chan struct{})}
	go capture.read()
	t.Cleanup(func() {
		close(capture.stop)
		_ = file.SetReadDeadline(time.Now())
		_ = file.Close()
		select {
		case <-capture.done:
		case <-time.After(5 * time.Second):
			t.Error("image argument reader did not retire during cleanup")
		}
	})
	return capture
}

func (capture *imageExtractionTestArgumentCapture) read() {
	defer close(capture.done)
	defer capture.cancel()
	scanner := bufio.NewScanner(capture.file)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if end := bytes.Index(data, []byte{0, 0}); end >= 0 {
			return end + 2, data[:end], nil
		}
		if atEOF && len(data) != 0 {
			return 0, nil, io.ErrUnexpectedEOF
		}
		return 0, nil, nil
	})
	count := 0
	for scanner.Scan() {
		record := imageExtractionTestArgumentRecord{barrier: len(scanner.Bytes()) == 0}
		if !record.barrier {
			if count == 16 {
				capture.cancel()
				select {
				case capture.records <- imageExtractionTestArgumentRecord{err: fmt.Errorf("image build exceeded its bounded argument record count")}:
				case <-capture.stop:
				}
				return
			}
			count++
			record.arguments = strings.Split(string(scanner.Bytes()), "\x00")
		} else {
			count = 0
		}
		select {
		case capture.records <- record:
		case <-capture.stop:
			return
		}
	}
	if err := scanner.Err(); err != nil {
		capture.cancel()
		select {
		case capture.records <- imageExtractionTestArgumentRecord{err: err}:
		case <-capture.stop:
		}
	}
}

func (capture *imageExtractionTestArgumentCapture) commands(t *testing.T) [][]string {
	t.Helper()
	// All child processes have returned before this marker is written. Receiving
	// it proves that every preceding argv record belongs to this completed build.
	if err := capture.file.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.file.Write([]byte{0, 0}); err != nil {
		t.Fatalf("write image argument completion marker: %v", err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var commands [][]string
	for {
		select {
		case record := <-capture.records:
			if record.err != nil {
				t.Fatalf("read real image decoder arguments: %v", record.err)
			}
			if record.barrier {
				return commands
			}
			if len(commands) == 16 {
				t.Fatal("image build exceeded its bounded argument record count")
			}
			commands = append(commands, record.arguments)
		case <-capture.done:
			t.Fatal("image argument reader exited before the completed build marker")
		case <-deadline.C:
			t.Fatal("image argument audit did not drain the completed build")
		}
	}
}
