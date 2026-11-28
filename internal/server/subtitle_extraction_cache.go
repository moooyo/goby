package server

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

const (
	subtitlePreparingHeader      = "X-Goby-Subtitle-Preparing"
	subtitleExtractionQueueSize  = 8
	subtitleExtractionEntries    = 128
	subtitleExtractionBytes      = 64 << 20
	subtitleExtractionTTL        = 30 * time.Minute
	subtitleExtractionBackoff    = 30 * time.Second
	subtitleExtractionFastWait   = 500 * time.Millisecond
	subtitleExtractionNativeWait = 20 * time.Second
)

var (
	errSubtitlePreparing      = errors.New("embedded subtitle extraction is pending")
	errSubtitleExtractionBusy = errors.New("embedded subtitle extraction queue is full")
)

type subtitleExtractionKey struct {
	item, source, codec, format, tool string
	index                             int
	version                           library.MediaSourceReadVersion
}

// Only the bytes from a complete, validated extraction are shared. Authorization,
// subtitle metadata, cue filtering and transport timestamps belong to each request.
type subtitleExtractionCache struct {
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
	done   chan struct{}

	mu      sync.Mutex
	queue   []*subtitleExtractionEntry
	entries map[subtitleExtractionKey]*subtitleExtractionEntry
	bytes   int
	fault   *subtitleExtractionEntry
}

type subtitleExtractionEntry struct {
	key    subtitleExtractionKey
	tool   os.FileInfo
	load   func(context.Context) ([]byte, error)
	ready  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc

	// Accessed under cache.mu. Closing ready announces actual retirement.
	running  bool
	retired  bool
	finished bool
	used     time.Time
	expires  time.Time
	data     []byte
	err      error
}

func newSubtitleExtractionCache() *subtitleExtractionCache {
	ctx, cancel := context.WithCancel(context.Background())
	c := &subtitleExtractionCache{
		ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
		done: make(chan struct{}), entries: make(map[subtitleExtractionKey]*subtitleExtractionEntry),
	}
	go c.run()
	return c
}

// Keep the configured invocation unchanged. A symlink or relative wrapper may
// read files relative to $0 that an executable stat cannot witness. A normal
// executable resolved through PATH is cacheable when its path is direct.
func subtitleExtractionTool(configured string) (string, os.FileInfo) {
	path, err := exec.LookPath(configured)
	if err != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}
	return path, info
}

func sameSubtitleExtractionTool(first, second os.FileInfo) bool {
	return first != nil && second != nil && os.SameFile(first, second) && first.Size() == second.Size() &&
		first.ModTime().Equal(second.ModTime()) && media.FileChangeTime(first) == media.FileChangeTime(second)
}

func (c *subtitleExtractionCache) read(ctx context.Context, key subtitleExtractionKey, tool os.FileInfo, wait time.Duration, load func(context.Context) ([]byte, error)) ([]byte, error) {
	if c == nil {
		return nil, library.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry, err := c.findOrQueue(key, tool, load)
	if err != nil {
		return nil, err
	}
	// A small read can finish before the first response without borrowing the
	// background deadline. In-flight work is not owned by this HTTP request.
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-entry.ready:
	case <-timer.C:
		return nil, errSubtitlePreparing
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, library.ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.ctx.Err() != nil {
		return nil, library.ErrUnavailable
	}
	if entry.err != nil {
		return nil, entry.err
	}
	if entry.retired {
		return nil, library.ErrSourceChanged
	}
	entry.used = time.Now()
	return entry.data, nil
}

func (c *subtitleExtractionCache) findOrQueue(key subtitleExtractionKey, tool os.FileInfo, load func(context.Context) ([]byte, error)) (*subtitleExtractionEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil {
		return nil, library.ErrUnavailable
	}
	now := time.Now()
	if entry := c.entries[key]; entry != nil {
		if entry.retired {
			return nil, library.ErrSourceChanged
		}
		if !sameSubtitleExtractionTool(entry.tool, tool) {
			if !entry.finished {
				return nil, errSubtitlePreparing
			}
			c.remove(entry)
		} else if !entry.finished {
			if load == nil {
				return nil, errSubtitlePreparing
			}
			return entry, nil
		} else if now.Before(entry.expires) {
			return entry, nil
		} else {
			c.remove(entry)
		}
	}
	if c.fault != nil {
		return nil, library.ErrUnavailable
	}
	if load == nil {
		return nil, errSubtitlePreparing
	}
	for _, entry := range c.entries {
		if entry.finished && !now.Before(entry.expires) {
			c.remove(entry)
		}
	}
	if len(c.queue) >= subtitleExtractionQueueSize {
		return nil, errSubtitleExtractionBusy
	}
	if len(c.entries) >= subtitleExtractionEntries && !c.evict() {
		return nil, errSubtitleExtractionBusy
	}
	ctx, cancel := context.WithDeadline(c.ctx, now.Add(media.BackgroundSubtitleExtractionTimeout))
	entry := &subtitleExtractionEntry{key: key, tool: tool, load: load, ready: make(chan struct{}), ctx: ctx, cancel: cancel}
	c.entries[key] = entry
	c.queue = append(c.queue, entry)
	select {
	case c.wake <- struct{}{}:
	default:
	}
	return entry, nil
}

func (c *subtitleExtractionCache) remove(entry *subtitleExtractionEntry) {
	c.bytes -= len(entry.data)
	delete(c.entries, entry.key)
}

func (c *subtitleExtractionCache) evict() bool {
	var oldest *subtitleExtractionEntry
	for _, entry := range c.entries {
		if entry.finished && (oldest == nil || entry.used.Before(oldest.used)) {
			oldest = entry
		}
	}
	if oldest == nil {
		return false
	}
	c.remove(oldest)
	return true
}

func (c *subtitleExtractionCache) next() *subtitleExtractionEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx.Err() != nil || len(c.queue) == 0 {
		return nil
	}
	entry := c.queue[0]
	copy(c.queue, c.queue[1:])
	c.queue[len(c.queue)-1] = nil
	c.queue = c.queue[:len(c.queue)-1]
	entry.running = true
	return entry
}

func (c *subtitleExtractionCache) run() {
	defer close(c.done)
	for c.ctx.Err() == nil {
		if entry := c.next(); entry != nil {
			var data []byte
			err := entry.ctx.Err()
			if err == nil {
				data, err = entry.load(entry.ctx)
				if entry.ctx.Err() != nil {
					data, err = nil, errors.Join(err, entry.ctx.Err())
				}
			}
			entry.cancel()
			c.finish(entry, data, err)
			continue
		}
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		}
	}
}

func (c *subtitleExtractionCache) finish(entry *subtitleExtractionEntry, data []byte, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer close(entry.ready)
	entry.load = nil
	entry.running = false
	if errors.Is(err, media.ErrProcessRetirementUnknown) {
		// A returned callback is not proof of process or descriptor retirement.
		// Preserve the exact owner's fault and never admit a replacement worker.
		entry.err = media.ErrProcessRetirementUnknown
		c.fault = entry
		c.remove(entry)
		c.cancelQueue()
		return
	}
	if entry.retired || c.ctx.Err() != nil {
		c.remove(entry)
		return
	}
	now := time.Now()
	entry.used = now
	if err == nil && (len(data) == 0 || len(data) > media.MaxSubtitleExtractionBytes) {
		err = media.ErrSubtitleExtraction
	}
	if err != nil {
		// Do not retain stderr or any caller-specific authority failure in a
		// reusable entry. Another authorized caller may start its own work.
		switch {
		case errors.Is(err, media.ErrOutputLimit), errors.Is(err, subtitle.ErrLimitExceeded):
			entry.err = subtitle.ErrLimitExceeded
		case errors.Is(err, media.ErrSubtitleExtraction), errors.Is(err, context.DeadlineExceeded), errors.Is(err, subtitle.ErrInvalidDocument), errors.Is(err, subtitle.ErrInvalidEncoding):
			entry.err = media.ErrSubtitleExtraction
		default:
			entry.err = library.ErrUnavailable
			c.remove(entry)
			return
		}
		entry.finished = true
		entry.expires = now.Add(subtitleExtractionBackoff)
		return
	}
	// Evict finished entries only, never free a slot for work still running.
	for c.bytes+len(data) > subtitleExtractionBytes {
		if !c.evict() {
			entry.err = library.ErrUnavailable
			c.remove(entry)
			return
		}
	}
	entry.finished = true
	entry.expires = now.Add(subtitleExtractionTTL)
	entry.data = make([]byte, len(data))
	copy(entry.data, data)
	c.bytes += len(data)
}

// Publication invalidates both ready data and pending work for the exact source.
// A running job stays charged until its descriptor and read owner really retire.
func (c *subtitleExtractionCache) retireSource(item, source string) *subtitleExtractionEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	matches := func(entry *subtitleExtractionEntry) bool {
		return entry.key.item == item && entry.key.source == source
	}
	var pending *subtitleExtractionEntry
	if c.fault != nil && matches(c.fault) {
		pending = c.fault
	}
	for _, entry := range c.entries {
		if matches(entry) && c.retire(entry) {
			pending = entry // There is only one running worker.
		}
	}
	c.dropRetiredQueueEntries()
	return pending
}

func (entry *subtitleExtractionEntry) waitRetirement(ctx context.Context) error {
	if entry == nil {
		return nil
	}
	select {
	case <-entry.ready:
		if errors.Is(entry.err, media.ErrProcessRetirementUnknown) {
			return media.ErrProcessRetirementUnknown
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Caller holds c.mu. Only the worker settles a running entry's receipt.
func (c *subtitleExtractionCache) retire(entry *subtitleExtractionEntry) bool {
	entry.retired = true
	entry.cancel()
	if entry.running {
		return true
	}
	if !entry.finished {
		close(entry.ready)
	}
	c.remove(entry)
	return false
}

func (c *subtitleExtractionCache) dropRetiredQueueEntries() {
	c.queue = slices.DeleteFunc(c.queue, func(entry *subtitleExtractionEntry) bool { return entry.retired })
}

func (c *subtitleExtractionCache) cancelQueue() {
	for _, entry := range c.queue {
		entry.err = library.ErrUnavailable
		c.retire(entry)
	}
	c.queue = nil
}

func (c *subtitleExtractionCache) beginClose() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.cancel()
	c.cancelQueue()
	for _, entry := range c.entries {
		c.retire(entry)
	}
	c.mu.Unlock()
}

func (c *subtitleExtractionCache) close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.beginClose()
	select {
	case <-c.done:
		if c.fault != nil {
			return media.ErrProcessRetirementUnknown
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
