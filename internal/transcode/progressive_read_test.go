//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type progressiveTestChunk struct {
	data   []byte
	result chan error
}

type progressiveTestProducer struct {
	initial []byte
	dir     string
	started chan struct{}
	chunks  chan progressiveTestChunk
	finish  chan error
}

func progressiveTestSpec(index int) Spec {
	spec := managerTestSpec(index)
	spec.Plan = Plan{OutputMode: "progressive", Container: "mp3", VideoStreamIndex: -1, AudioStreamIndex: 0,
		AudioCodec: "mp3", AudioBitrate: 128000, AudioChannels: 2, AudioSampleRate: 44100, DurationTicks: 600 * ticksPerSecond}
	return spec
}

func newProgressiveTestProducer(initial []byte) *progressiveTestProducer {
	return &progressiveTestProducer{initial: initial, started: make(chan struct{}), chunks: make(chan progressiveTestChunk), finish: make(chan error, 1)}
}

func (p *progressiveTestProducer) run(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, progress func(Progress)) (RunResult, error) {
	p.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "stream.bin"), p.initial, 0o600); err != nil {
		return RunResult{}, err
	}
	progress(Progress{Ready: true, Bytes: int64(len(p.initial))})
	close(p.started)
	for {
		select {
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		case err := <-p.finish:
			return RunResult{}, err
		case chunk := <-p.chunks:
			file, err := os.OpenFile(filepath.Join(dir, "stream.bin"), os.O_WRONLY|os.O_APPEND, 0)
			if err == nil {
				_, err = file.Write(chunk.data)
				err = errors.Join(err, file.Close())
			}
			chunk.result <- err
			if err != nil {
				return RunResult{}, err
			}
			// Growth deliberately emits no Progress callback. A reader must
			// observe appended bytes even when the writer reports no progress.
		}
	}
}

func (p *progressiveTestProducer) append(t *testing.T, data []byte) {
	t.Helper()
	chunk := progressiveTestChunk{data: data, result: make(chan error, 1)}
	select {
	case p.chunks <- chunk:
	case <-time.After(3 * time.Second):
		t.Fatal("progressive producer did not accept another chunk")
	}
	select {
	case err := <-chunk.result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("progressive producer did not append another chunk")
	}
}

func progressiveTestRunning(t *testing.T, initial []byte, configure func(*Options)) (*Manager, Spec, Record, *progressiveTestProducer) {
	t.Helper()
	producer := newProgressiveTestProducer(initial)
	o := managerAccessOptions(t, producer.run)
	if configure != nil {
		configure(&o)
	}
	m := newTestManager(t, o)
	spec := progressiveTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerAccessWait(t, producer.started, "the progressive producer")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := m.WaitReady(ctx, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	return m, spec, record, producer
}

type progressiveTestOpenResult struct {
	reader *ProgressiveReader
	err    error
}

func progressiveTestStartOpen(t *testing.T, m *Manager, ctx context.Context, scope Scope, id string) <-chan progressiveTestOpenResult {
	t.Helper()
	result := make(chan progressiveTestOpenResult)
	abandoned := make(chan struct{})
	t.Cleanup(func() { close(abandoned) })
	go func() {
		reader, err := m.OpenProgressive(ctx, scope, id)
		select {
		case result <- progressiveTestOpenResult{reader: reader, err: err}:
		case <-abandoned:
			if reader != nil {
				_ = reader.Close()
			}
		}
	}()
	return result
}

func progressiveTestReceiveOpen(t *testing.T, result <-chan progressiveTestOpenResult) (*ProgressiveReader, error) {
	t.Helper()
	select {
	case received := <-result:
		if received.reader != nil {
			t.Cleanup(func() {
				if err := progressiveTestCloseError(received.reader); err != nil {
					t.Errorf("close progressive test reader: %v", err)
				}
			})
		}
		return received.reader, received.err
	case <-time.After(3 * time.Second):
		t.Fatal("progressive reader did not finish opening")
		return nil, nil
	}
}

func progressiveTestCloseError(reader *ProgressiveReader) error {
	result := make(chan error, 1)
	go func() { result <- reader.Close() }()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		return errors.New("progressive Close did not interrupt the active read")
	}
}

func progressiveTestOpen(t *testing.T, m *Manager, ctx context.Context, scope Scope, id string) *ProgressiveReader {
	t.Helper()
	reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, ctx, scope, id))
	if err != nil || reader == nil {
		t.Fatalf("open progressive output: %v", err)
	}
	return reader
}

type progressiveTestReadResult struct {
	data []byte
	err  error
}

func progressiveTestStartRead(reader *ProgressiveReader, size int) <-chan progressiveTestReadResult {
	result := make(chan progressiveTestReadResult, 1)
	go func() {
		buffer := make([]byte, size)
		n, err := reader.Read(buffer)
		result <- progressiveTestReadResult{data: buffer[:n], err: err}
	}()
	return result
}

func progressiveTestReceiveRead(t *testing.T, result <-chan progressiveTestReadResult) progressiveTestReadResult {
	t.Helper()
	select {
	case received := <-result:
		return received
	case <-time.After(3 * time.Second):
		t.Fatal("progressive reader did not wake up")
		return progressiveTestReadResult{}
	}
}

func progressiveTestWaitRead(t *testing.T, reader *ProgressiveReader, result <-chan progressiveTestReadResult) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if !reader.readMu.TryLock() {
			return
		}
		reader.readMu.Unlock()
		select {
		case received := <-result:
			t.Fatalf("read ended before its controlled event: %q, %v", received.data, received.err)
		case <-timer.C:
			t.Fatal("progressive reader did not start its waiting read")
		case <-ticker.C:
		}
	}
}

func progressiveTestDrainInitial(t *testing.T, reader *ProgressiveReader, initial []byte) {
	t.Helper()
	got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, len(initial)))
	if got.err != nil || !bytes.Equal(got.data, initial) {
		t.Fatalf("initial progressive payload = %q, %v", got.data, got.err)
	}
}

func progressiveTestReaderCount(t *testing.T, m *Manager, want int) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		readers := m.readers
		m.mu.Unlock()
		if readers == want {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("reader count = %d, want %d", readers, want)
		case <-ticker.C:
		}
	}
}

func TestProgressiveReaderWaitsForAppendAndDurableCompletion(t *testing.T) {
	initial := []byte("fake MP3 first payload")
	m, spec, record, producer := progressiveTestRunning(t, initial, nil)
	reader := progressiveTestOpen(t, m, context.Background(), spec.Scope, record.ID)
	if _, ok := any(reader).(io.Seeker); ok {
		t.Fatal("progressive reader exposes seeking")
	}
	if _, ok := any(reader).(interface{ Fd() uintptr }); ok {
		t.Fatal("progressive reader exposes its file descriptor")
	}
	readerType := reflect.TypeOf(reader)
	if readerType.NumMethod() != 2 || readerType.Method(0).Name != "Close" || readerType.Method(1).Name != "Read" {
		t.Fatal("progressive reader exposes methods beyond Read and Close")
	}
	for i := 0; i < readerType.Elem().NumField(); i++ {
		if field := readerType.Elem().Field(i); field.PkgPath == "" {
			t.Fatalf("progressive reader exposes field %q", field.Name)
		}
	}
	oldAccess := time.Now().UTC().Add(-time.Minute)
	m.mu.Lock()
	m.jobs[record.ID].record.LastAccessAt = oldAccess
	m.mu.Unlock()
	progressiveTestDrainInitial(t, reader, initial)
	if snapshot, err := m.Snapshot(spec.Scope, record.ID); err != nil || !snapshot.LastAccessAt.After(oldAccess) {
		t.Fatalf("consuming payload did not refresh idle access: %+v, %v", snapshot, err)
	}
	next := progressiveTestStartRead(reader, 64)
	progressiveTestWaitRead(t, reader, next)
	appended := []byte("another MP3 payload")
	producer.append(t, appended)
	got := progressiveTestReceiveRead(t, next)
	if got.err != nil || !bytes.Equal(got.data, appended) {
		t.Fatalf("temporary EOF hid appended payload: %q, %v", got.data, got.err)
	}
	last := progressiveTestStartRead(reader, 1)
	progressiveTestWaitRead(t, reader, last)
	producer.finish <- nil
	if final := managerTestWaitFinished(t, m, record.ID); final.State != "completed" {
		t.Fatalf("progressive completion state = %q", final.State)
	}
	got = progressiveTestReceiveRead(t, last)
	if len(got.data) != 0 || !errors.Is(got.err, io.EOF) {
		t.Fatalf("completed and drained output did not end: %q, %v", got.data, got.err)
	}
	managerAccessAssertReaders(t, m, record.ID, 1, 1)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
	if got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 1)); !errors.Is(got.err, os.ErrClosed) {
		t.Fatalf("explicitly closed reader returned %v", got.err)
	}
}

func TestProgressiveReaderTemporaryEOFWaitsForOwnerDeadline(t *testing.T) {
	initial := []byte("fake MP3 payload")
	m, spec, record, _ := progressiveTestRunning(t, initial, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reader := progressiveTestOpen(t, m, ctx, spec.Scope, record.ID)
	progressiveTestDrainInitial(t, reader, initial)
	got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 1))
	if len(got.data) != 0 || !errors.Is(got.err, context.DeadlineExceeded) {
		t.Fatalf("temporary EOF escaped before owner deadline: %q, %v", got.data, got.err)
	}
	progressiveTestReaderCount(t, m, 0)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 1)); !errors.Is(got.err, context.DeadlineExceeded) {
		t.Fatalf("explicit close lost the automatic deadline reason: %v", got.err)
	}
}

func TestProgressiveReaderTerminalEventsWakeAndReleaseReaders(t *testing.T) {
	for _, operation := range []string{"runner failure", "job cancellation", "session cancellation", "owner cancellation", "reader close", "manager close"} {
		t.Run(operation, func(t *testing.T) {
			initial := []byte("fake MP3 payload")
			m, spec, record, producer := progressiveTestRunning(t, initial, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := progressiveTestOpen(t, m, ctx, spec.Scope, record.ID)
			progressiveTestDrainInitial(t, reader, initial)
			result := progressiveTestStartRead(reader, 32)
			progressiveTestWaitRead(t, reader, result)
			var want error
			switch operation {
			case "runner failure":
				want = ErrJobFailed
				producer.finish <- errors.New("private progressive failure")
				managerTestWaitFinished(t, m, record.ID)
			case "job cancellation":
				want = ErrJobCancelled
				if err := managerAccessCancelJob(t, m, spec.Scope, record.ID); err != nil {
					t.Fatal(err)
				}
			case "session cancellation":
				want = ErrJobCancelled
				m.CancelSession(spec.Scope.AuthSessionID)
			case "owner cancellation":
				want = context.Canceled
				cancel()
			case "reader close":
				want = os.ErrClosed
				if err := progressiveTestCloseError(reader); err != nil {
					t.Fatal(err)
				}
			case "manager close":
				want = ErrManagerClosed
				closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancelClose()
				if err := m.Close(closeCtx); err != nil {
					t.Fatalf("manager shutdown waited for an EOF reader: %v", err)
				}
			}
			got := progressiveTestReceiveRead(t, result)
			if len(got.data) != 0 || !errors.Is(got.err, want) {
				t.Fatalf("terminal read = %q, %v; want %v", got.data, got.err, want)
			}
			progressiveTestReaderCount(t, m, 0)
			var closes sync.WaitGroup
			closeErrors := make(chan error, 8)
			for i := 0; i < 8; i++ {
				closes.Add(1)
				go func() { defer closes.Done(); closeErrors <- reader.Close() }()
			}
			closed := make(chan struct{})
			go func() { closes.Wait(); close(closed) }()
			managerAccessWait(t, closed, "concurrent reader closes")
			close(closeErrors)
			for err := range closeErrors {
				if err != nil {
					t.Errorf("repeated close returned %v", err)
				}
			}
			if got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 1)); !errors.Is(got.err, want) {
				t.Fatalf("repeated close lost terminal reason: %v, want %v", got.err, want)
			}
			progressiveTestReaderCount(t, m, 0)
		})
	}
}

func TestProgressiveReaderCancellationReleasesAnIdleReader(t *testing.T) {
	for _, operation := range []string{"owner", "job", "manager"} {
		t.Run(operation, func(t *testing.T) {
			m, spec, record, _ := progressiveTestRunning(t, []byte("fake MP3 payload"), nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := progressiveTestOpen(t, m, ctx, spec.Scope, record.ID)
			var want error
			switch operation {
			case "owner":
				want = context.Canceled
				cancel()
			case "job":
				want = ErrJobCancelled
				if err := managerAccessCancelJob(t, m, spec.Scope, record.ID); err != nil {
					t.Fatal(err)
				}
			case "manager":
				want = ErrManagerClosed
				closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancelClose()
				if err := m.Close(closeCtx); err != nil {
					t.Fatalf("idle reader blocked shutdown: %v", err)
				}
			}
			progressiveTestReaderCount(t, m, 0)
			if got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 1)); !errors.Is(got.err, want) {
				t.Fatalf("idle reader retained stale access: %v", got.err)
			}
		})
	}
}

func TestProgressiveReaderRequiresCompleteScopeAndPrivateOutputMode(t *testing.T) {
	m, spec, record, _ := progressiveTestRunning(t, []byte("fake MP3 payload"), nil)
	for _, change := range []func(*Scope){
		func(scope *Scope) { scope.UserID = "foreign-user" },
		func(scope *Scope) { scope.AuthSessionID = "foreign-auth" },
		func(scope *Scope) { scope.DeviceID = "foreign-device" },
		func(scope *Scope) { scope.PlaySessionID = "foreign-play" },
		func(scope *Scope) { scope.ItemID = "foreign-item" },
		func(scope *Scope) { scope.SourceID = "foreign-source" },
	} {
		foreign := spec.Scope
		change(&foreign)
		if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), foreign, record.ID)); reader != nil || !errors.Is(err, ErrJobNotFound) {
			t.Fatalf("foreign progressive scope was accepted: %v", err)
		}
	}
	if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), spec.Scope, "missing")); reader != nil || !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("missing progressive job was accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, ctx, spec.Scope, record.ID)); reader != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open retained a reader: %v", err)
	}
	for _, name := range []string{"stream.bin", "stream.bin.tmp", "../stream.bin"} {
		if reader, err := managerAccessTryOpen(t, m, spec.Scope, record.ID, name); reader != nil || !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("TryOpen exposed private progressive output %q: %v", name, err)
		}
		if reader, err := m.Open(context.Background(), spec.Scope, record.ID, name); reader != nil || !errors.Is(err, ErrOutputUnavailable) {
			if reader != nil {
				_ = reader.Close()
			}
			t.Fatalf("Open exposed private progressive output %q: %v", name, err)
		}
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
	reader := progressiveTestOpen(t, m, context.Background(), spec.Scope, record.ID)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := managerAccessCancelJob(t, m, spec.Scope, record.ID); err != nil {
		t.Fatal(err)
	}
	if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), spec.Scope, record.ID)); reader != nil || !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("cancelled job opened a new progressive reader: %v", err)
	}
}

func TestProgressiveReaderRejectsAnHLSJob(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, _ func(Progress)) (RunResult, error) {
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	m := newTestManager(t, managerAccessOptions(t, run))
	spec := managerTestSpec(1)
	record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	managerTestWaitFinished(t, m, record.ID)
	if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), spec.Scope, record.ID)); reader != nil || !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("progressive reader accepted an HLS job: %v", err)
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
}

func TestProgressiveReaderSharesGlobalAndPerJobReaderLimits(t *testing.T) {
	run := func(_ context.Context, _, dir string, _ *os.File, plan Plan, _ int, progress func(Progress)) (RunResult, error) {
		if plan.OutputMode == "progressive" {
			if err := os.WriteFile(filepath.Join(dir, "stream.bin"), []byte("fake MP3 payload"), 0o600); err != nil {
				return RunResult{}, err
			}
			progress(Progress{Ready: true})
			return RunResult{}, nil
		}
		return RunResult{}, publishManagerTestOutput(dir, 188)
	}
	o := managerAccessOptions(t, run)
	o.MaxReaders, o.MaxJobReaders = 2, 1
	m := newTestManager(t, o)
	var records []Record
	for i, spec := range []Spec{progressiveTestSpec(1), progressiveTestSpec(2), managerTestSpec(3)} {
		record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
		if err != nil {
			t.Fatal(err)
		}
		if final := managerTestWaitFinished(t, m, record.ID); final.State != "completed" {
			t.Fatalf("fixture %d did not complete: %+v", i, final)
		}
		records = append(records, record)
	}
	first := progressiveTestOpen(t, m, context.Background(), records[0].Spec.Scope, records[0].ID)
	if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), records[0].Spec.Scope, records[0].ID)); reader != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("per-job progressive quota was bypassed: %v", err)
	}
	hls, err := managerAccessTryOpen(t, m, records[2].Spec.Scope, records[2].ID, "segment-0.ts")
	if err != nil {
		t.Fatal(err)
	}
	if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), records[1].Spec.Scope, records[1].ID)); reader != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("progressive reader ignored an existing HLS reader: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := progressiveTestOpen(t, m, context.Background(), records[1].Spec.Scope, records[1].ID)
	managerAccessAssertReaders(t, m, records[1].ID, 1, 2)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := hls.Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, records[1].ID, 0, 0)
}

func TestProgressiveReaderKeepsCompletedFilesUntilExplicitClose(t *testing.T) {
	payload := bytes.Repeat([]byte("fake MP3 payload;"), 8192)
	m, spec, record, producer := progressiveTestRunning(t, payload, nil)
	reader := progressiveTestOpen(t, m, context.Background(), spec.Scope, record.ID)
	producer.finish <- nil
	managerTestWaitFinished(t, m, record.ID)
	copyResult := make(chan progressiveTestReadResult, 1)
	go func() {
		var output bytes.Buffer
		buffer := make([]byte, 31)
		for {
			n, err := reader.Read(buffer)
			output.Write(buffer[:n])
			if err != nil {
				copyResult <- progressiveTestReadResult{data: output.Bytes(), err: err}
				return
			}
		}
	}()
	copied := progressiveTestReceiveRead(t, copyResult)
	if !errors.Is(copied.err, io.EOF) || !bytes.Equal(copied.data, payload) {
		t.Fatal("bounded reads changed the completed payload")
	}
	managerAccessAssertReaders(t, m, record.ID, 1, 1)
	m.mu.Lock()
	m.jobs[record.ID].record.LastAccessAt = time.Now().UTC().Add(-2 * time.Hour)
	m.mu.Unlock()
	m.maintain()
	if _, err := os.Stat(filepath.Join(producer.dir, "stream.bin")); err != nil {
		t.Fatalf("an EOF reader lost its pinned file: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	managerAccessAssertReaders(t, m, record.ID, 0, 0)
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelClose()
	if err := m.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.options.Root, record.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed progressive reader retained the cache: %v", err)
	}
}

func TestProgressiveReaderRejectsShrinkingAndOversizedOutput(t *testing.T) {
	for _, operation := range []string{"shrink", "quota"} {
		t.Run(operation, func(t *testing.T) {
			initial := bytes.Repeat([]byte("a"), 64)
			m, spec, record, producer := progressiveTestRunning(t, initial, func(o *Options) { o.MaxJobBytes = 128 })
			reader := progressiveTestOpen(t, m, context.Background(), spec.Scope, record.ID)
			if got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 8)); got.err != nil || len(got.data) != 8 {
				t.Fatalf("first partial read failed: %v", got.err)
			}
			// Only the reader may inspect the changed file until its Read
			// returns. This distinguishes Reader.Stat checks from maintenance.
			m.filesMu.Lock()
			var unlockOnce sync.Once
			unlock := func() { unlockOnce.Do(m.filesMu.Unlock) }
			defer unlock()
			want, code := ErrJobFailed, "invalid_output"
			if operation == "shrink" {
				// The new length still exceeds the current read offset. Comparing
				// only offset against size would silently accept this truncation.
				if err := os.Truncate(filepath.Join(producer.dir, "stream.bin"), 32); err != nil {
					t.Fatal(err)
				}
			} else {
				want, code = ErrQuota, "job_quota"
				producer.append(t, bytes.Repeat([]byte("b"), 129))
			}
			got := progressiveTestReceiveRead(t, progressiveTestStartRead(reader, 8))
			unlock()
			if len(got.data) != 0 || !errors.Is(got.err, want) {
				t.Fatalf("%s was silently read: %q, %v", operation, got.data, got.err)
			}
			progressiveTestReaderCount(t, m, 0)
			if final := managerTestWaitFinished(t, m, record.ID); final.ErrorCode != code {
				t.Fatalf("%s error classification = %q", operation, final.ErrorCode)
			}
			if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), spec.Scope, record.ID)); reader != nil || !errors.Is(err, want) {
				t.Fatalf("invalid output admitted a new reader: %v", err)
			}
		})
	}
}

func TestProgressiveManagerRequiresPayloadSignalAndRejectsMixedOutput(t *testing.T) {
	for _, test := range []struct {
		name  string
		body  []byte
		ready bool
		mixed bool
	}{
		{"empty output with signal", nil, true, false},
		{"headers without payload signal", []byte("ID3 header only"), false, false},
		{"mixed HLS output", []byte("fake MP3 payload"), true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func(_ context.Context, _, dir string, _ *os.File, _ Plan, _ int, progress func(Progress)) (RunResult, error) {
				if err := os.WriteFile(filepath.Join(dir, "stream.bin"), test.body, 0o600); err != nil {
					return RunResult{}, err
				}
				if test.mixed {
					if err := os.WriteFile(filepath.Join(dir, "main.m3u8"), []byte("#EXTM3U\n"), 0o600); err != nil {
						return RunResult{}, err
					}
				}
				progress(Progress{Ready: test.ready})
				return RunResult{}, nil
			}
			m := newTestManager(t, managerAccessOptions(t, run))
			spec := progressiveTestSpec(1)
			record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			if final := managerTestWaitFinished(t, m, record.ID); final.State != "failed" {
				t.Fatalf("invalid progressive output completed: %+v", final)
			}
			if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), spec.Scope, record.ID)); reader != nil || !errors.Is(err, ErrJobFailed) {
				t.Fatalf("invalid progressive output became readable: %v", err)
			}
		})
	}
}

func TestProgressiveManagerAcceptsBothPayloadReadinessOrders(t *testing.T) {
	for _, signalFirst := range []bool{false, true} {
		name := "bytes before signal"
		if signalFirst {
			name = "signal before bytes"
		}
		t.Run(name, func(t *testing.T) {
			first, publish := make(chan struct{}), make(chan struct{})
			payload := []byte("fake MP3 payload")
			run := func(ctx context.Context, _, dir string, _ *os.File, _ Plan, _ int, progress func(Progress)) (RunResult, error) {
				initial := payload
				if signalFirst {
					initial = nil
				}
				if err := os.WriteFile(filepath.Join(dir, "stream.bin"), initial, 0o600); err != nil {
					return RunResult{}, err
				}
				if signalFirst {
					progress(Progress{Ready: true})
				}
				close(first)
				select {
				case <-ctx.Done():
					return RunResult{}, ctx.Err()
				case <-publish:
				}
				if signalFirst {
					if err := os.WriteFile(filepath.Join(dir, "stream.bin"), payload, 0o600); err != nil {
						return RunResult{}, err
					}
				} else {
					progress(Progress{Ready: true})
				}
				<-ctx.Done()
				return RunResult{}, ctx.Err()
			}
			m := newTestManager(t, managerAccessOptions(t, run))
			spec := progressiveTestSpec(1)
			record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
			if err != nil {
				t.Fatal(err)
			}
			managerAccessWait(t, first, "the first readiness condition")
			m.maintain()
			m.mu.Lock()
			ready := m.jobs[record.ID].ready
			m.mu.Unlock()
			if ready {
				t.Fatal("one readiness condition advertised playable output")
			}
			waitingCtx, cancelWaiting := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelWaiting()
			if reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, waitingCtx, spec.Scope, record.ID)); reader != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("one readiness condition opened a reader: %v", err)
			}
			managerAccessAssertReaders(t, m, record.ID, 0, 0)
			close(publish)
			reader, err := progressiveTestReceiveOpen(t, progressiveTestStartOpen(t, m, context.Background(), spec.Scope, record.ID))
			if err != nil {
				t.Fatal(err)
			}
			progressiveTestDrainInitial(t, reader, payload)
		})
	}
}

type progressiveCompletionRepository struct {
	*managerTestRepository
	entered chan struct{}
	release <-chan struct{}
	fail    bool
}

func (r *progressiveCompletionRepository) Update(ctx context.Context, record Record) error {
	if record.State == "completed" {
		close(r.entered)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-r.release:
		}
		if r.fail {
			return errors.New("private final status persistence failure")
		}
	}
	return r.managerTestRepository.Update(ctx, record)
}

func TestProgressiveReaderWaitsForFinalPersistenceBeforeEOF(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "commit"
		if fail {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			repository := &progressiveCompletionRepository{managerTestRepository: &managerTestRepository{}, entered: make(chan struct{}), release: release, fail: fail}
			initial := []byte("fake MP3 payload")
			m, spec, record, producer := progressiveTestRunning(t, initial, func(o *Options) { o.Repository = repository })
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancelDeadline()
			deadlineReader := progressiveTestOpen(t, m, deadlineCtx, spec.Scope, record.ID)
			progressiveTestDrainInitial(t, deadlineReader, initial)
			reader := progressiveTestOpen(t, m, context.Background(), spec.Scope, record.ID)
			progressiveTestDrainInitial(t, reader, initial)
			producer.finish <- nil
			managerAccessWait(t, repository.entered, "the final durable status update")
			m.mu.Lock()
			job := m.jobs[record.ID]
			pendingCompletion := job.record.State == "completed" && !job.finished
			m.mu.Unlock()
			if !pendingCompletion {
				t.Fatal("the test did not reach the completion persistence window")
			}
			// Keep final persistence blocked for this reader's entire remaining
			// lifetime. State="completed" alone must never produce an EOF.
			deadlineRead := progressiveTestReceiveRead(t, progressiveTestStartRead(deadlineReader, 1))
			if len(deadlineRead.data) != 0 || !errors.Is(deadlineRead.err, context.DeadlineExceeded) {
				t.Fatalf("pending persistence escaped as EOF: %q, %v", deadlineRead.data, deadlineRead.err)
			}
			progressiveTestReaderCount(t, m, 1)
			reading := progressiveTestStartRead(reader, 1)
			progressiveTestWaitRead(t, reader, reading)
			releaseOnce.Do(func() { close(release) })
			managerTestWaitFinished(t, m, record.ID)
			got := progressiveTestReceiveRead(t, reading)
			want := io.EOF
			if fail {
				want = ErrPersistence
			}
			if len(got.data) != 0 || !errors.Is(got.err, want) {
				t.Fatalf("pending final persistence returned %q, %v; want %v", got.data, got.err, want)
			}
			if fail {
				progressiveTestReaderCount(t, m, 0)
			} else if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProgressiveReaderRechecksOpenCancellationBarriers(t *testing.T) {
	for _, operation := range []string{"owner", "job", "manager"} {
		t.Run(operation, func(t *testing.T) {
			m, spec, record, _ := progressiveTestRunning(t, []byte("fake MP3 payload"), nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			m.filesMu.Lock()
			var unlockOnce sync.Once
			unlock := func() { unlockOnce.Do(m.filesMu.Unlock) }
			defer unlock()
			opening := progressiveTestStartOpen(t, m, ctx, spec.Scope, record.ID)
			managerAccessWaitReaders(t, m, record.ID, 1)
			var want error
			switch operation {
			case "owner":
				want = context.Canceled
				cancel()
			case "job":
				want = ErrJobCancelled
				if err := managerAccessCancelJob(t, m, spec.Scope, record.ID); err != nil {
					t.Fatal(err)
				}
			case "manager":
				want = ErrManagerClosed
				closeCtx, cancelClose := context.WithCancel(context.Background())
				cancelClose()
				if err := m.Close(closeCtx); !errors.Is(err, context.Canceled) {
					t.Fatalf("shutdown initiation returned %v", err)
				}
			}
			unlock()
			if reader, err := progressiveTestReceiveOpen(t, opening); reader != nil || !errors.Is(err, want) {
				t.Fatalf("%s cancellation admitted a progressive reader: %v", operation, err)
			}
			progressiveTestReaderCount(t, m, 0)
		})
	}
}
