//go:build linux

package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

type diagnosticSyncGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newDiagnosticSyncGate() *diagnosticSyncGate {
	return &diagnosticSyncGate{entered: make(chan struct{}), release: make(chan struct{})}
}

func (gate *diagnosticSyncGate) block() {
	close(gate.entered)
	<-gate.release
}

func (gate *diagnosticSyncGate) allow() {
	gate.once.Do(func() { close(gate.release) })
}

// Call only inside a synctest bubble. A gate in the writer makes admission
// order and queue saturation independent of scheduler timing or disk speed.
func startDiagnosticAppend(store *Store, ctx context.Context, line []byte) <-chan error {
	done := make(chan error, 1)
	go func() { done <- store.appendRecord(ctx, line) }()
	synctest.Wait()
	return done
}

func requireDiagnosticResult(t *testing.T, done <-chan error, wanted error) {
	t.Helper()
	if err := <-done; !errors.Is(err, wanted) {
		t.Fatalf("write result = %v, want %v", err, wanted)
	}
}

func requireDiagnosticPending(t *testing.T, done <-chan error) {
	t.Helper()
	synctest.Wait()
	select {
	case err := <-done:
		t.Fatalf("operation completed before the durability gate: %v", err)
	default:
	}
}

func requireDiagnosticQueue(t *testing.T, store *Store, records, size int) {
	t.Helper()
	store.writerMu.Lock()
	defer store.writerMu.Unlock()
	if len(store.writeQueue) != records || store.queuedBytes != size {
		t.Fatalf("queued records/bytes = %d/%d, want %d/%d", len(store.writeQueue), store.queuedBytes, records, size)
	}
}

func TestStoreWriterGroupsConcurrentDurableRecords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		firstGate, groupGate := newDiagnosticSyncGate(), newDiagnosticSyncGate()
		defer firstGate.allow()
		defer groupGate.allow()
		var syncs atomic.Int32
		store.syncFile = func(file *os.File) error {
			if strings.HasSuffix(file.Name(), ".jsonl") {
				switch syncs.Add(1) {
				case 1:
					firstGate.block()
				case 2:
					groupGate.block()
				}
			}
			return file.Sync()
		}
		firstLine := []byte("{\"sequence\":0}\n")
		first := startDiagnosticAppend(store, context.Background(), firstLine)
		<-firstGate.entered
		var expected bytes.Buffer
		expected.Write(firstLine)
		const queued = maxBatchRecords + 16
		results := make([]<-chan error, 0, queued)
		queuedBytes := 0
		for index := 1; index <= queued; index++ {
			line := []byte(fmt.Sprintf("{\"sequence\":%d}\n", index))
			results = append(results, startDiagnosticAppend(store, context.Background(), line))
			expected.Write(line)
			queuedBytes += len(line)
		}
		requireDiagnosticQueue(t, store, queued, queuedBytes)
		requireDiagnosticPending(t, first)
		firstGate.allow()
		requireDiagnosticResult(t, first, nil)
		<-groupGate.entered
		for _, result := range results {
			requireDiagnosticPending(t, result)
		}
		groupGate.allow()
		for _, result := range results {
			requireDiagnosticResult(t, result, nil)
		}
		if got := syncs.Load(); got != 3 {
			t.Fatalf("%d concurrent records used %d log syncs, want 3 bounded batches", queued+1, got)
		}
		data, err := os.ReadFile(filepath.Join(store.cfg.Directory, store.activeName))
		if err != nil || !bytes.Equal(data, expected.Bytes()) {
			t.Fatalf("JSONL order or complete boundaries changed: %q, %v", data, err)
		}
	})
}

func TestStoreWriterSaturationCancellationAndByteBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		gate := newDiagnosticSyncGate()
		defer gate.allow()
		var sizes []int64
		store.syncFile = func(file *os.File) error {
			if strings.HasSuffix(file.Name(), ".jsonl") {
				if len(sizes) == 0 {
					gate.block()
				}
				stat, err := file.Stat()
				if err != nil {
					return err
				}
				sizes = append(sizes, stat.Size())
			}
			return file.Sync()
		}
		writingContext, cancelWriting := context.WithCancel(context.Background())
		defer cancelWriting()
		firstLine := []byte("{\"first\":true}\n")
		first := startDiagnosticAppend(store, writingContext, firstLine)
		<-gate.entered
		cancelWriting()
		line := []byte("{\"message\":\"" + strings.Repeat("x", MaxRecordBytes-len("{\"message\":\"\"}\n")) + "\"}\n")
		queuedContext, cancelQueued := context.WithCancel(context.Background())
		defer cancelQueued()
		cancelled := startDiagnosticAppend(store, queuedContext, line)
		var results []<-chan error
		for index := 1; index < maxQueuedRecords; index++ {
			results = append(results, startDiagnosticAppend(store, context.Background(), line))
		}
		requireDiagnosticQueue(t, store, maxQueuedRecords, maxQueuedBytes)
		overflowContext, cancelOverflow := context.WithCancel(context.Background())
		defer cancelOverflow()
		overflow := startDiagnosticAppend(store, overflowContext, line)
		requireDiagnosticPending(t, overflow)
		cancelOverflow()
		requireDiagnosticResult(t, overflow, context.Canceled)
		cancelQueued()
		synctest.Wait()
		requireDiagnosticPending(t, cancelled)
		requireDiagnosticPending(t, first)
		requireDiagnosticQueue(t, store, maxQueuedRecords, maxQueuedBytes)
		gate.allow()
		// Cancellation after writing began cannot retract the durable result.
		requireDiagnosticResult(t, first, nil)
		requireDiagnosticResult(t, cancelled, context.Canceled)
		for _, result := range results {
			requireDiagnosticResult(t, result, nil)
		}
		if len(sizes) != 5 {
			t.Fatalf("maximum-size queue used %d log syncs, want 5", len(sizes))
		}
		for index := 1; index < len(sizes); index++ {
			if size := sizes[index] - sizes[index-1]; size > maxBatchBytes {
				t.Fatalf("batch wrote %d bytes, limit %d", size, maxBatchBytes)
			}
		}
		data, err := os.ReadFile(filepath.Join(store.cfg.Directory, store.activeName))
		expected := append(bytes.Clone(firstLine), bytes.Repeat(line, maxQueuedRecords-1)...)
		if err != nil || !bytes.Equal(data, expected) {
			t.Fatalf("cancelled or saturated records reached the file: bytes=%d, %v", len(data), err)
		}
	})
}

func TestStoreWriterCloseDrainsAcceptedShutdownAndKeepsProcessLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		name := store.activeName
		gate := newDiagnosticSyncGate()
		defer gate.allow()
		firstSync := true
		store.syncFile = func(file *os.File) error {
			if firstSync && strings.HasSuffix(file.Name(), ".jsonl") {
				firstSync = false
				gate.block()
			}
			return file.Sync()
		}
		line := []byte("{}\n")
		first := startDiagnosticAppend(store, context.Background(), line)
		<-gate.entered
		var results []<-chan error
		for index := 0; index < maxQueuedRecords-1; index++ {
			results = append(results, startDiagnosticAppend(store, context.Background(), line))
		}
		shutdown := make(chan error, 1)
		handler := NewHandler(store, nil)
		go func() {
			shutdown <- handler.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "server shutdown completed", 0))
		}()
		synctest.Wait()
		overflow := startDiagnosticAppend(store, context.Background(), line)
		requireDiagnosticPending(t, overflow)
		closed := []chan error{make(chan error, 1), make(chan error, 1)}
		for _, done := range closed {
			go func() { done <- store.Close() }()
		}
		synctest.Wait()
		requireDiagnosticResult(t, overflow, ErrUnavailable)
		if err := store.appendRecord(context.Background(), line); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("closing store accepted a new record: %v", err)
		}
		for _, done := range closed {
			requireDiagnosticPending(t, done)
		}
		if reopened, err := Open(store.cfg); !errors.Is(err, ErrBusy) {
			if reopened != nil {
				reopened.Close()
			}
			t.Fatalf("process lock released before writer exit: %v", err)
		}
		gate.allow()
		requireDiagnosticResult(t, first, nil)
		for _, result := range results {
			requireDiagnosticResult(t, result, nil)
		}
		requireDiagnosticResult(t, shutdown, nil)
		for _, done := range closed {
			requireDiagnosticResult(t, done, nil)
		}
		select {
		case <-store.writerDone:
		default:
			t.Fatal("Close returned before writer termination")
		}
		data, err := os.ReadFile(filepath.Join(store.cfg.Directory, name))
		if err != nil || bytes.Count(data, []byte{'\n'}) != maxQueuedRecords+1 || !bytes.Contains(data, []byte("\"event\":\"server.shutdown.completed\"")) {
			t.Fatalf("Close lost accepted records or the shutdown event: %q, %v", data, err)
		}
		if status := store.Status(); !status.Closed || status.Healthy || status.Degraded {
			t.Fatalf("closed writer status: %+v", status)
		}
		reopened := testStore(t, store.cfg)
		appendTestRecord(t, reopened, "after complete writer shutdown")
	})
}

func TestStoreWriterPartialFailurePreservesAcknowledgedPrefixAndCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		gate := newDiagnosticSyncGate()
		defer gate.allow()
		writingContext, cancelWriting := context.WithCancel(context.Background())
		defer cancelWriting()
		writes, syncs := 0, 0
		store.write = func(file *os.File, data []byte) (int, error) {
			if strings.HasSuffix(file.Name(), ".jsonl") {
				writes++
				if writes == 3 {
					cancelWriting()
					n, _ := file.Write(data[:len(data)/2])
					return n, syscall.ENOSPC
				}
			}
			return file.Write(data)
		}
		store.syncFile = func(file *os.File) error {
			if strings.HasSuffix(file.Name(), ".jsonl") {
				syncs++
				if syncs == 1 {
					gate.block()
				}
			}
			return file.Sync()
		}
		firstLine := []byte("{\"sequence\":0}\n")
		first := startDiagnosticAppend(store, context.Background(), firstLine)
		<-gate.entered
		completeLine := []byte("{\"sequence\":1}\n")
		complete := startDiagnosticAppend(store, context.Background(), completeLine)
		partial := startDiagnosticAppend(store, writingContext, []byte("{\"sequence\":2}\n"))
		cancelledContext, cancelQueued := context.WithCancel(context.Background())
		defer cancelQueued()
		cancelled := startDiagnosticAppend(store, cancelledContext, []byte("{\"sequence\":3}\n"))
		cancelQueued()
		gate.allow()
		requireDiagnosticResult(t, first, nil)
		requireDiagnosticResult(t, complete, ErrUnavailable)
		requireDiagnosticResult(t, partial, ErrUnavailable)
		requireDiagnosticResult(t, cancelled, context.Canceled)
		data, err := os.ReadFile(filepath.Join(store.cfg.Directory, store.activeName))
		if err != nil || !bytes.Equal(data, append(bytes.Clone(firstLine), completeLine...)) {
			t.Fatalf("rollback changed the earlier boundary: %q, %v", data, err)
		}
		if !store.Status().Degraded {
			t.Fatal("partial write did not leave a sticky degraded state")
		}
		if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("degraded writer accepted another record: %v", err)
		}
		if writes != 3 {
			t.Fatalf("unprocessed suffix was written after failure: %d writes", writes)
		}
	})
}

func TestStoreWriterSyncFailureDoesNotAcknowledgeBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		firstGate, failedGate := newDiagnosticSyncGate(), newDiagnosticSyncGate()
		defer firstGate.allow()
		defer failedGate.allow()
		syncs := 0
		store.syncFile = func(file *os.File) error {
			if strings.HasSuffix(file.Name(), ".jsonl") {
				syncs++
				switch syncs {
				case 1:
					firstGate.block()
				case 2:
					failedGate.block()
					return syscall.EIO
				}
			}
			return file.Sync()
		}
		first := startDiagnosticAppend(store, context.Background(), []byte("{}\n"))
		<-firstGate.entered
		second := startDiagnosticAppend(store, context.Background(), []byte("{}\n"))
		third := startDiagnosticAppend(store, context.Background(), []byte("{}\n"))
		firstGate.allow()
		requireDiagnosticResult(t, first, nil)
		<-failedGate.entered
		requireDiagnosticPending(t, second)
		requireDiagnosticPending(t, third)
		failedGate.allow()
		requireDiagnosticResult(t, second, ErrUnavailable)
		requireDiagnosticResult(t, third, ErrUnavailable)
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("sync failure was lost during shutdown: %v", err)
		}
	})
}

func TestStoreWriterRotationCommitsBeforeRetentionAndLaterFailure(t *testing.T) {
	for _, failNewFileSync := range []bool{false, true} {
		t.Run(fmt.Sprintf("new_file_sync_failure=%t", failNewFileSync), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				store := testStore(t, Config{MaxFileBytes: MaxRecordBytes, MaxFiles: 1})
				oldName := store.activeName
				oldFile, err := os.Open(filepath.Join(store.cfg.Directory, oldName))
				if err != nil {
					t.Fatal(err)
				}
				defer oldFile.Close()
				firstGate, rotationGate, nextGate := newDiagnosticSyncGate(), newDiagnosticSyncGate(), newDiagnosticSyncGate()
				defer firstGate.allow()
				defer rotationGate.allow()
				defer nextGate.allow()
				logSyncs := 0
				oldCommitted, intentSeen, prematureIntent := false, false, false
				store.write = func(file *os.File, data []byte) (int, error) {
					if strings.HasSuffix(file.Name(), ".tmp") && bytes.Contains(data, []byte("\"deleting\":true")) {
						intentSeen = true
						prematureIntent = !oldCommitted
					}
					return file.Write(data)
				}
				store.syncFile = func(file *os.File) error {
					if strings.HasSuffix(file.Name(), ".jsonl") {
						logSyncs++
						switch logSyncs {
						case 1:
							firstGate.block()
						case 2:
							rotationGate.block()
						case 3:
							nextGate.block()
							if failNewFileSync {
								return syscall.EIO
							}
						}
						if err := file.Sync(); err != nil {
							return err
						}
						if logSyncs == 2 {
							oldCommitted = true
						}
						return nil
					}
					return file.Sync()
				}
				firstLine := []byte("{}\n")
				first := startDiagnosticAppend(store, context.Background(), firstLine)
				<-firstGate.entered
				oldLine := []byte("{\"message\":\"" + strings.Repeat("a", 5000) + "\"}\n")
				newLine := []byte("{\"message\":\"" + strings.Repeat("b", 5000) + "\"}\n")
				old := startDiagnosticAppend(store, context.Background(), oldLine)
				newFile := startDiagnosticAppend(store, context.Background(), newLine)
				last := startDiagnosticAppend(store, context.Background(), []byte("{}\n"))
				firstGate.allow()
				requireDiagnosticResult(t, first, nil)
				<-rotationGate.entered
				requireDiagnosticPending(t, old)
				rotationGate.allow()
				<-nextGate.entered
				requireDiagnosticResult(t, old, nil)
				requireDiagnosticPending(t, newFile)
				requireDiagnosticPending(t, last)
				if !intentSeen || prematureIntent {
					t.Fatalf("retention intent preceded old-file sync: seen=%t premature=%t", intentSeen, prematureIntent)
				}
				if _, err := os.Lstat(filepath.Join(store.cfg.Directory, oldName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("rotation did not reclaim the closed file: %v", err)
				}
				data, err := io.ReadAll(oldFile)
				if err != nil || !bytes.Equal(data, append(bytes.Clone(firstLine), oldLine...)) {
					t.Fatalf("acknowledged old inode changed: bytes=%d, %v", len(data), err)
				}
				nextGate.allow()
				var wanted error
				if failNewFileSync {
					wanted = ErrUnavailable
				}
				requireDiagnosticResult(t, newFile, wanted)
				requireDiagnosticResult(t, last, wanted)
				if got := store.Status().Degraded; got != failNewFileSync {
					t.Fatalf("rotation degradation = %t, want %t", got, failNewFileSync)
				}
			})
		})
	}
}

func TestStoreWriterPanicReleasesAcceptedCallsAndPreservesFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		gate := newDiagnosticSyncGate()
		defer gate.allow()
		store.syncFile = func(file *os.File) error {
			if strings.HasSuffix(file.Name(), ".jsonl") {
				gate.block()
				panic("private writer panic")
			}
			return file.Sync()
		}
		var fallback bytes.Buffer
		handler := NewHandler(store, slog.NewJSONHandler(&fallback, nil))
		first := make(chan error, 1)
		go func() {
			first <- handler.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "server stopped", 0))
		}()
		<-gate.entered
		second := startDiagnosticAppend(store, context.Background(), []byte("{}\n"))
		closed := make(chan error, 1)
		go func() { closed <- store.Close() }()
		synctest.Wait()
		requireDiagnosticPending(t, closed)
		gate.allow()
		requireDiagnosticResult(t, first, ErrUnavailable)
		requireDiagnosticResult(t, second, ErrUnavailable)
		requireDiagnosticResult(t, closed, ErrUnavailable)
		if !strings.Contains(fallback.String(), "server.stopped") || strings.Contains(fallback.String(), "private writer panic") {
			t.Fatalf("safe fallback did not survive writer failure: %s", fallback.String())
		}
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("repeated Close lost writer failure: %v", err)
		}
		if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("stopped writer accepted another record: %v", err)
		}
	})
}

func TestStoreWriterUnopenedStoreFailsWithoutWaiting(t *testing.T) {
	var store Store
	if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unopened store accepted a record: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("unopened store did not close: %v", err)
	}
}
