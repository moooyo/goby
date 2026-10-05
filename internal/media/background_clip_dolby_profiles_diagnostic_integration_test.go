//go:build linux

package media

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// This explicit diagnostic test uses the production graph and process owner.
// It never validates, saves or publishes the provisional encoder output.
func TestBackgroundClipActualDolbyProfilesRealDiagnostic(t *testing.T) {
	profile, sources := backgroundDolbyRealSources(t)
	key := map[string]string{"5": "5", "84": "8.4"}[profile]
	source := sources[key]
	ctx, extractor, file, info, video, options := backgroundDolbyRealSetup(t, profile, source)
	limits := DefaultAnalysisLimits()
	limits.Timeout = 2 * time.Minute
	geometry, err := extractor.analysisGeometry(ctx, file, video, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planBackgroundClipWithOptions(info, video, geometry, source.StartTicks, 25*TicksPerSecond, options, limits)
	if err != nil {
		t.Fatal(err)
	}
	tool, err := analysisOpenToolExpected(ctx, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer tool.file.Close()
	args := backgroundClipEncodeArgs(video, plan, limits)
	t.Logf("diagnostic source SHA-256=%s, duration_ticks=%d, start_ticks=%d, video=%+v, dolby_vision=%+v", source.SHA256, info.DurationTicks, source.StartTicks, video, video.DolbyVision)
	t.Logf("production FFmpeg arguments: %q", args)
	sink := &backgroundDolbyDiagnosticStderr{}
	var discarded int64
	started := time.Now()
	err = runBackgroundClipProcess(ctx, "/proc/self/fd/4", file, args, limits.Timeout, MaxBackgroundClipBytes, sink, func(reader io.Reader) error {
		var err error
		discarded, err = io.Copy(io.Discard, reader)
		return err
	}, true, tool.file)
	stderr, stderrErr := sink.result()
	t.Logf("diagnostic elapsed=%v, discarded_stdout_bytes=%d, process_error=%v, stderr_error=%v", time.Since(started), discarded, err, stderrErr)
	t.Logf("bounded FFmpeg stderr:\n%s", stderr)
	backgroundDolbyCheckOffset(t, file)
	if err := errors.Join(err, stderrErr, tool.check()); err != nil {
		t.Fatalf("production renderer diagnostic failed: %v", err)
	}
}

type backgroundDolbyDiagnosticStderr struct {
	mu     sync.Mutex
	data   bytes.Buffer
	closed bool
	err    error
}

func (sink *backgroundDolbyDiagnosticStderr) Write(data []byte) (int, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.closed {
		return 0, io.ErrClosedPipe
	}
	if len(data) > (64<<10)-sink.data.Len() {
		sink.err = ErrAnalysisBudget
		return 0, sink.err
	}
	return sink.data.Write(data)
}

func (sink *backgroundDolbyDiagnosticStderr) Close(err error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if !sink.closed {
		sink.closed = true
		sink.err = errors.Join(sink.err, err)
	}
}

func (sink *backgroundDolbyDiagnosticStderr) result() (string, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return sink.data.String(), sink.err
}
