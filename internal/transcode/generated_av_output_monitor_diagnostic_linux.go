//go:build linux

package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// One private observer process owns this bounded sampler. It applies a sampled
// diagnostic limit during production and validates the stopped-child extent
// after normal retirement. It is joined before the process lease returns. This
// is not a physical hard quota; the baseline manager's existing charged storage
// and production resource contracts remain separate.
type generatedAVDiagnosticOutputMonitor struct {
	directory string
	limit     int64
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	err       error
}

func generatedAVDiagnosticOutputExtent(directory string, limit int64) error {
	if limit < 1 {
		return ErrTimelineLimit
	}
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	// ReadDir(n) bounds the entry slice even if an unexpected child generates
	// excessive files. Expected output is flat and privately owned.
	entries, err := file.ReadDir(65)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > 64 {
		return ErrTimelineLimit
	}
	var bytes int64
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrInvalidInput
		}
		info, err := entry.Info()
		// temp_file publication can atomically rename an entry after this
		// bounded directory snapshot. The final stopped-child sample is stable.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			return ErrInvalidInput
		}
		if info.Size() > limit-bytes {
			return ErrTimelineLimit
		}
		bytes += info.Size()
	}
	return nil
}

func newGeneratedAVDiagnosticOutputMonitor(ctx context.Context, directory string, limit int64, cancel context.CancelFunc) (*generatedAVDiagnosticOutputMonitor, error) {
	if err := generatedAVDiagnosticOutputExtent(directory, limit); err != nil {
		return nil, err
	}
	monitor := &generatedAVDiagnosticOutputMonitor{directory: directory, limit: limit, stop: make(chan struct{}), done: make(chan struct{})}
	check := func() {
		if err := generatedAVDiagnosticOutputExtent(directory, limit); err != nil {
			monitor.mu.Lock()
			if monitor.err == nil {
				monitor.err = err
			}
			monitor.mu.Unlock()
			cancel()
		}
	}
	go func() {
		defer close(monitor.done)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				check()
			case <-monitor.stop:
				check()
				return
			case <-ctx.Done():
				check()
				return
			}
		}
	}()
	return monitor, nil
}

func (monitor *generatedAVDiagnosticOutputMonitor) finish() {
	monitor.once.Do(func() { close(monitor.stop) })
	<-monitor.done
	// ctx cancellation can end the sampler before child retirement. Always
	// perform this separate post-join check, including that cancellation path.
	if err := generatedAVDiagnosticOutputExtent(monitor.directory, monitor.limit); err != nil {
		monitor.mu.Lock()
		if monitor.err == nil {
			monitor.err = err
		}
		monitor.mu.Unlock()
	}
}
func (monitor *generatedAVDiagnosticOutputMonitor) failure() error {
	monitor.mu.Lock()
	defer monitor.mu.Unlock()
	return monitor.err
}
