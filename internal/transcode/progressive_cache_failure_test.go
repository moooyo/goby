//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// progressiveCacheFailureContext pauses the watcher's first select after it
// has captured the completed job's current changed channel. Releasing it only
// after cache failure makes the regression independent of goroutine timing.
type progressiveCacheFailureContext struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *progressiveCacheFailureContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return nil
}

func TestProgressiveReaderReleasesCompletedJobAfterGlobalCacheFailure(t *testing.T) {
	payload := []byte("fake MP3 payload for cache failure notification")
	run := func(_ context.Context, _, directory string, _ *os.File, _ Plan, _ int, progress func(Progress)) (RunResult, error) {
		if err := os.WriteFile(filepath.Join(directory, "stream.bin"), payload, 0o600); err != nil {
			return RunResult{}, err
		}
		progress(Progress{Ready: true, Bytes: int64(len(payload))})
		return RunResult{}, nil
	}
	options := managerTestOptions(t, run)
	options.IdleTimeout = time.Hour
	m, err := NewManager(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	watchContext := &progressiveCacheFailureContext{
		Context: context.Background(), entered: make(chan struct{}), release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	releaseWatcher := func() { releaseOnce.Do(func() { close(watchContext.release) }) }
	var reader *ProgressiveReader
	var unknownPath, failedJobID string
	unknownPayload := []byte("owned test fixture: preserve this unknown cache entry")
	var expectCacheFailure bool
	t.Cleanup(func() {
		releaseWatcher()
		if reader != nil {
			if err := reader.Close(); err != nil {
				t.Errorf("close progressive reader: %v", err)
			}
		}
		// Stop maintenance before manually removing the owned fixture and the
		// directory whose failed removal deliberately cleared job.directory.
		m.cancel()
		select {
		case <-m.loopDone:
			if unknownPath != "" {
				body, err := os.ReadFile(unknownPath)
				if err != nil || !bytes.Equal(body, unknownPayload) {
					t.Errorf("unknown cache fixture changed before cleanup: %q, %v", body, err)
				}
				if err := os.Remove(unknownPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Errorf("remove owned unknown cache fixture: %v", err)
				}
				m.mu.Lock()
				if job := m.jobs[failedJobID]; job != nil {
					job.directory = false
				}
				m.mu.Unlock()
				m.filesMu.Lock()
				err = m.cache.RemoveJob(failedJobID)
				m.filesMu.Unlock()
				if err != nil {
					t.Errorf("remove owned failed job directory: %v", err)
				}
			}
		case <-time.After(3 * time.Second):
			t.Error("cache maintenance did not stop during cleanup")
		}
		closeContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(closeContext); err != nil && !(expectCacheFailure && errors.Is(err, ErrCacheUnsafe)) {
			t.Errorf("close manager after cache failure: %v", err)
		}
	})
	var records []Record
	for index := 1; index <= 2; index++ {
		spec := managerTestSpec(index)
		spec.Plan = Plan{OutputMode: "progressive", Container: "mp3", VideoStreamIndex: -1, AudioStreamIndex: 0,
			AudioCodec: "mp3", AudioBitrate: 128000, AudioChannels: 2, AudioSampleRate: 44100, DurationTicks: 600 * ticksPerSecond}
		record, err := m.Ensure(context.Background(), spec, managerTestInput(t))
		if err != nil {
			t.Fatal(err)
		}
		if completed := managerTestWaitFinished(t, m, record.ID); completed.State != "completed" {
			t.Fatalf("progressive fixture did not complete: %+v", completed)
		}
		records = append(records, record)
	}
	observer, culprit := records[0], records[1]
	reader, err = m.OpenProgressive(watchContext, observer.Spec.Scope, observer.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-watchContext.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("completed reader watcher did not capture its notification channel")
	}
	m.mu.Lock()
	observerJob, culpritJob := m.jobs[observer.ID], m.jobs[culprit.ID]
	observingCompleted := observerJob.finished && observerJob.record.State == "completed" && observerJob.readers == 1 && m.readers == 1
	m.mu.Unlock()
	if !observingCompleted {
		t.Fatal("the test did not retain one unread completed progressive output")
	}
	failedJobID = culprit.ID
	fixturePath := filepath.Join(options.Root, failedJobID, "unknown-cache-fixture.txt")
	m.filesMu.Lock()
	err = os.WriteFile(fixturePath, unknownPayload, 0o600)
	if err == nil {
		unknownPath = fixturePath
		expectCacheFailure = true
		err = m.CancelJob(culprit.ID, culprit.Spec.Scope)
	}
	m.filesMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	// Either this call or maintenance performs the same real, guarded removal.
	// Unknown content must cause a sticky cache failure without being deleted.
	m.reclaim(culpritJob, false)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		failed := m.cacheFailed
		guardedFailure := errors.Is(m.closeErr, ErrCacheUnsafe) && !culpritJob.directory
		m.mu.Unlock()
		if failed {
			if !guardedFailure {
				t.Fatal("the cache failure did not originate from guarded job removal")
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("unknown cache content did not fail guarded reclamation")
		case <-ticker.C:
		}
	}
	if body, err := os.ReadFile(unknownPath); err != nil || !bytes.Equal(body, unknownPayload) {
		t.Fatalf("failed reclamation altered unknown content: %q, %v", body, err)
	}
	releaseWatcher()
	deadline.Reset(3 * time.Second)
	for {
		m.mu.Lock()
		released := m.readers == 0 && observerJob.readers == 0
		completed := observerJob.finished && observerJob.record.State == "completed"
		failed := m.cacheFailed
		m.mu.Unlock()
		if released {
			if !completed || !failed {
				t.Fatal("reader cleanup changed completed history or cleared the cache failure")
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("global cache failure did not release an unread completed reader")
		case <-ticker.C:
		}
	}
	buffer := make([]byte, 1)
	if n, err := reader.Read(buffer); n != 0 || !errors.Is(err, ErrOutputUnavailable) {
		t.Fatalf("automatically closed reader returned %d bytes, %v; want cache failure", n, err)
	}
}
