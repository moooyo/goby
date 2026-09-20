package library

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type analysisSourceWorkerTestResult struct {
	file                  *os.File
	source                MediaFile
	err                   error
	returnedBeforeWorkEnd bool
}

// Deadlines are failure guards only. Ordering assertions use explicit channels,
// never an elapsed delay as evidence that work did or did not finish.
func analysisSourceWorkerTestSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", description)
	}
}

func analysisSourceWorkerTestReceive(t *testing.T, result <-chan analysisSourceWorkerTestResult) analysisSourceWorkerTestResult {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case value := <-result:
		return value
	case <-timer.C:
		t.Fatal("analysis source worker did not finish after its release")
		return analysisSourceWorkerTestResult{}
	}
}

func analysisSourceWorkerTestReserveSlots(t *testing.T, count int) {
	t.Helper()
	held := 0
	t.Cleanup(func() {
		for range held {
			select {
			case <-mediaSourceWorkers:
			default:
				t.Error("analysis source worker released a slot reserved by its test")
			}
		}
	})
	for range count {
		select {
		case mediaSourceWorkers <- struct{}{}:
			held++
		default:
			t.Fatal("global media source slots were unexpectedly occupied before the controlled test")
		}
	}
}

func TestAnalysisSourceWorkerCancellationRetainsTaskAndSlotUntilCleanup(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "analysis-undelivered-*.media")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	analysisSourceWorkerTestReserveSlots(t, cap(mediaSourceWorkers)-1)
	ctx, cancel := context.WithCancel(context.Background())
	started, sawCancellation, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	operationEnded := make(chan struct{})
	returned := make(chan analysisSourceWorkerTestResult, 1)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-started:
			select {
			case <-operationEnded:
			case <-timer.C:
				t.Error("released source operation did not drain during cleanup")
				return
			}
		default:
		}
		select {
		case <-finished:
		case <-timer.C:
			t.Error("cancelled analysis source worker did not drain during cleanup")
		}
		select {
		case value := <-returned:
			if value.file != nil {
				_ = value.file.Close()
			}
		default:
		}
	})
	go func() {
		defer close(finished)
		opened, source, err := runAnalysisSourceWorker(ctx, func() (*os.File, MediaFile, error) {
			close(started)
			<-ctx.Done()
			close(sawCancellation)
			<-release
			close(operationEnded)
			return file, MediaFile{SourceID: "unpublished-source"}, nil
		})
		early := false
		select {
		case <-operationEnded:
		default:
			early = true
		}
		returned <- analysisSourceWorkerTestResult{file: opened, source: source, err: err, returnedBeforeWorkEnd: early}
	}()
	analysisSourceWorkerTestSignal(t, started, "analysis source operation entry")
	cancel()
	analysisSourceWorkerTestSignal(t, sawCancellation, "blocked operation to observe cancellation")
	select {
	case value := <-returned:
		if value.file != nil {
			_ = value.file.Close()
		}
		t.Fatal("cancelled analysis returned before its actual source operation was released")
	default:
	}
	select {
	case mediaSourceWorkers <- struct{}{}:
		<-mediaSourceWorkers
		t.Fatal("cancellation released the active global source slot early")
	default:
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("blocked operation lost its still-owned descriptor: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	result := analysisSourceWorkerTestReceive(t, returned)
	analysisSourceWorkerTestSignal(t, finished, "cancelled source worker completion")
	if result.returnedBeforeWorkEnd || result.file != nil || !reflect.DeepEqual(result.source, MediaFile{}) || !errors.Is(result.err, context.Canceled) {
		if result.file != nil {
			_ = result.file.Close()
		}
		t.Fatalf("cancelled worker exposed undelivered source state: %+v", result)
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("undelivered descriptor outlived the source slot: %v", err)
	}
	select {
	case mediaSourceWorkers <- struct{}{}:
		<-mediaSourceWorkers
	default:
		t.Fatal("completed source cleanup did not release its global slot")
	}
}

func TestAnalysisSourceWorkerCancelledWhileQueuedNeverStartsWork(t *testing.T) {
	analysisSourceWorkerTestReserveSlots(t, cap(mediaSourceWorkers))
	ctx, cancel := context.WithCancel(context.Background())
	attempting, finished := make(chan struct{}), make(chan struct{})
	started := make(chan struct{}, 1)
	returned := make(chan analysisSourceWorkerTestResult, 1)
	t.Cleanup(func() {
		cancel()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-finished:
		case <-timer.C:
			t.Error("queued source worker did not honor cancellation")
		}
	})
	go func() {
		defer close(finished)
		close(attempting)
		file, source, err := runAnalysisSourceWorker(ctx, func() (*os.File, MediaFile, error) { started <- struct{}{}; return nil, MediaFile{}, nil })
		returned <- analysisSourceWorkerTestResult{file: file, source: source, err: err}
	}()
	analysisSourceWorkerTestSignal(t, attempting, "queued worker admission attempt")
	cancel()
	result := analysisSourceWorkerTestReceive(t, returned)
	analysisSourceWorkerTestSignal(t, finished, "queued cancellation completion")
	if result.file != nil || !reflect.DeepEqual(result.source, MediaFile{}) || !errors.Is(result.err, context.Canceled) {
		t.Fatalf("queued cancellation returned source state: %+v", result)
	}
	select {
	case <-started:
		t.Fatal("a cancelled queued worker started source I/O")
	default:
	}
	if len(mediaSourceWorkers) != cap(mediaSourceWorkers) {
		t.Fatal("queued cancellation consumed another operation's slot")
	}
}

func TestAnalysisSourceWorkerFailureClosesReturnedDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "analysis-failed-*.media")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cause := errors.New("bounded source operation failed")
	opened, source, err := runAnalysisSourceWorker(context.Background(), func() (*os.File, MediaFile, error) { return file, MediaFile{SourceID: "unpublished-source"}, cause })
	if opened != nil || !reflect.DeepEqual(source, MediaFile{}) || !errors.Is(err, cause) {
		t.Fatalf("failed source operation returned partial state: %v", err)
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("failed source descriptor was not closed: %v", err)
	}
}
