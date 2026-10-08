package primaryio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

type primaryIOReadSeekerTestSource struct {
	mu      sync.Mutex
	reads   int
	seeks   int
	closes  int
	readFn  func([]byte) (int, error)
	seekFn  func(int64, int) (int64, error)
	closeFn func() error
}

func (s *primaryIOReadSeekerTestSource) Read(buffer []byte) (int, error) {
	s.mu.Lock()
	s.reads++
	s.mu.Unlock()
	if s.readFn != nil {
		return s.readFn(buffer)
	}
	return 0, io.EOF
}

func (s *primaryIOReadSeekerTestSource) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	s.seeks++
	s.mu.Unlock()
	if s.seekFn != nil {
		return s.seekFn(offset, whence)
	}
	return offset, nil
}

func (s *primaryIOReadSeekerTestSource) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *primaryIOReadSeekerTestSource) counts() (reads, seeks, closes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads, s.seeks, s.closes
}

type primaryIOReadSeekerReadResult struct {
	n   int
	err error
}

func primaryIOReadSeekerReadAsync(reader *ReadSeeker, buffer []byte) <-chan primaryIOReadSeekerReadResult {
	result := make(chan primaryIOReadSeekerReadResult, 1)
	go func() {
		n, err := reader.Read(buffer)
		result <- primaryIOReadSeekerReadResult{n, err}
	}()
	return result
}

func primaryIOReadSeekerCloseAsync(reader *ReadSeeker) <-chan error {
	result := make(chan error, 1)
	go func() { result <- reader.Close() }()
	return result
}

func primaryIOReadSeekerAssertPending[T any](t *testing.T, result <-chan T) {
	t.Helper()
	select {
	case value := <-result:
		t.Fatalf("operation completed before its actual retirement gate: %v", value)
	default:
	}
}

func primaryIOReadSeekerGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return gate, release
}

func primaryIOReadSeekerNew(t *testing.T, owner *Owner, route Route, source ReadSeekCloser, chunk int, retired func()) *ReadSeeker {
	t.Helper()
	reader, err := NewReadSeeker(owner, route, Foreground, source, chunk, retired)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// Owner.Cancel occurs after Close sets its method fence. Observe the actual
// cancel invocation rather than relying on the scheduler having run Close.
func primaryIOReadSeekerObserveCancel(reader *ReadSeeker) <-chan struct{} {
	observed := make(chan struct{}, 4)
	reader.owner.state.mu.Lock()
	cancel := reader.owner.state.cancel
	reader.owner.state.cancel = func() {
		cancel()
		select {
		case observed <- struct{}{}:
		default:
		}
	}
	reader.owner.state.mu.Unlock()
	return observed
}

type primaryIOReadSeekerWriterFunc func([]byte) (int, error)

func (f primaryIOReadSeekerWriterFunc) Write(buffer []byte) (int, error) { return f(buffer) }

func TestPrimaryIOReadSeekerBorrowsBoundedBufferAndReleasesBeforeNetworkWrite(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	buffer := make([]byte, 2*MaxReadChunkBytes)
	var borrowed []int
	var retired atomic.Int32
	source := &primaryIOReadSeekerTestSource{readFn: func(part []byte) (int, error) {
		primaryIOAssertCounts(t, g, 1, 0, 0)
		if &part[0] != &buffer[0] {
			t.Error("source did not borrow the caller's read buffer")
		}
		borrowed = append(borrowed, len(part))
		for index := range part {
			part[index] = 'x'
		}
		return len(part), nil
	}}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, MaxReadChunkBytes, func() { retired.Add(1) })
	if err := owner.Complete(); !errors.Is(err, ErrStaleOwner) {
		t.Fatalf("constructor did not consume retained-owner rights: %v", err)
	}
	n, err := reader.Read(buffer)
	if err != nil || n != MaxReadChunkBytes || buffer[n-1] != 'x' {
		t.Fatalf("bounded read: n=%d err=%v", n, err)
	}
	writer := primaryIOReadSeekerWriterFunc(func(part []byte) (int, error) {
		primaryIOAssertCounts(t, g, 0, 0, 0)
		if stats := owners.Stats(); stats.RegisteredOwners != 1 {
			t.Errorf("network write lost retained descriptor ownership: %+v", stats)
		}
		return len(part), nil
	})
	if written, err := writer.Write(buffer[:n]); err != nil || written != n {
		t.Fatalf("network write: n=%d err=%v", written, err)
	}
	n, err = reader.Read(buffer[:7])
	if err != nil || n != 7 || len(borrowed) != 2 || borrowed[0] != MaxReadChunkBytes || borrowed[1] != 7 {
		t.Fatalf("caller/chunk bounds: n=%d err=%v borrowed=%v", n, err, borrowed)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	reads, seeks, closes := source.counts()
	if reads != 2 || seeks != 0 || closes != 1 || retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("final retirement: read=%d seek=%d close=%d retired=%d owners=%+v", reads, seeks, closes, retired.Load(), owners.Stats())
	}
}

func TestPrimaryIOReadSeekerKeepsPreparedRoutePrivateAcrossChunks(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	root := RootKey{Catalog: "catalog", RootID: "source"}
	route := Route{Roots: []RootKey{root, root}, Domains: []string{"disk", "disk"}}
	source := &primaryIOReadSeekerTestSource{readFn: func(part []byte) (int, error) {
		g.mu.Lock()
		rootCount, domainCount := g.roots[root], g.domains["disk"]
		rootKeys, domainKeys := len(g.roots), len(g.domains)
		g.mu.Unlock()
		if rootCount.active != 1 || domainCount.active != 1 || rootKeys != 1 || domainKeys != 1 {
			t.Fatalf("source read used mutated or duplicate route keys: root=%+v domain=%+v keys=%d/%d", rootCount, domainCount, rootKeys, domainKeys)
		}
		return len(part), nil
	}}
	reader := primaryIOReadSeekerNew(t, owner, route, source, 8, nil)
	t.Cleanup(func() {
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	})
	for chunk := range 3 {
		route.Roots[0], route.Roots[1] = RootKey{}, RootKey{Catalog: "other", RootID: "root"}
		route.Domains[0], route.Domains[1] = "", "other-disk"
		if n, err := reader.Read(make([]byte, 8)); err != nil || n != 8 {
			t.Fatalf("chunk %d: n=%d err=%v", chunk, n, err)
		}
		primaryIOAssertCounts(t, g, 0, 0, 0)
	}
}

func TestPrimaryIOReadSeekerLogicalSeekDoesNotWaitForActualIOBudget(t *testing.T) {
	limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 2, RootQueued: 2, DomainQueued: 2}
	g, owners := primaryIOFixture(t, limits)
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, primaryIORoute("a", "da"), Foreground)
	owner := primaryIOOwner(t, owners, context.Background())
	metadata := bytes.NewReader([]byte("metadata-only offset"))
	source := &primaryIOReadSeekerTestSource{seekFn: metadata.Seek}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, nil)
	type seekResult struct {
		position int64
		err      error
	}
	result := make(chan seekResult, 1)
	go func() {
		position, err := reader.Seek(3, io.SeekStart)
		result <- seekResult{position, err}
	}()
	seek := primaryIOAwaitValue(t, result)
	if seek.err != nil || seek.position != 3 {
		t.Fatalf("logical seek under a full I/O budget: %+v", seek)
	}
	primaryIOAssertCounts(t, g, 1, 0, 0)
	reads, seeks, _ := source.counts()
	if reads != 0 || seeks != 1 {
		t.Fatalf("memory-only seek touched the read path: read=%d seek=%d", reads, seeks)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	primaryIOAssertCounts(t, g, 1, 0, 0)
	primaryIORelease(t, holderLease)
	primaryIOComplete(t, holder)
}

func TestPrimaryIOReadSeekerCancelCloseAckKeepsActualReadCharged(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	readEntered, closeAck := make(chan struct{}), make(chan struct{})
	readMayReturn, releaseRead := primaryIOReadSeekerGate(t)
	source := &primaryIOReadSeekerTestSource{
		readFn: func([]byte) (int, error) {
			close(readEntered)
			<-readMayReturn
			return 0, os.ErrClosed
		},
		closeFn: func() error { close(closeAck); return nil },
	}
	var retired atomic.Int32
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() { retired.Add(1) })
	result := primaryIOReadSeekerReadAsync(reader, make([]byte, 8))
	primaryIOAwaitClosed(t, readEntered)
	if err := reader.Cancel(); err != nil {
		t.Fatal(err)
	}
	primaryIOAwaitClosed(t, closeAck)
	primaryIOReadSeekerAssertPending(t, result)
	primaryIOAssertCounts(t, g, 1, 0, 0)
	if owners.Stats().RegisteredOwners != 1 || retired.Load() != 0 {
		t.Fatalf("descriptor-close ACK retired a still-running read: owners=%+v retired=%d", owners.Stats(), retired.Load())
	}
	releaseRead()
	read := primaryIOAwaitValue(t, result)
	if read.n != 0 || !errors.Is(read.err, context.Canceled) || !errors.Is(reader.Err(), context.Canceled) {
		t.Fatalf("canceled actual read: %+v remembered=%v", read, reader.Err())
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	if owners.Stats().RegisteredOwners != 1 {
		t.Fatal("returning a read retired the retained descriptor owner")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("successful cleanup replayed historical read cancellation: %v", err)
	}
	if retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("joined actual retirement: retired=%d owners=%+v", retired.Load(), owners.Stats())
	}
}

func TestPrimaryIOReadSeekerCloseJoinsEnteredLogicalSeek(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	seekEntered, closeAck := make(chan struct{}), make(chan struct{})
	seekMayReturn, releaseSeek := primaryIOReadSeekerGate(t)
	metadata := bytes.NewReader([]byte("memory-only metadata"))
	var retired atomic.Int32
	source := &primaryIOReadSeekerTestSource{
		seekFn: func(offset int64, whence int) (int64, error) {
			close(seekEntered)
			<-seekMayReturn
			return metadata.Seek(offset, whence)
		},
		closeFn: func() error { close(closeAck); return nil },
	}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() { retired.Add(1) })
	cancelObserved := primaryIOReadSeekerObserveCancel(reader)
	type seekResult struct {
		position int64
		err      error
	}
	result := make(chan seekResult, 1)
	go func() {
		position, err := reader.Seek(2, io.SeekStart)
		result <- seekResult{position, err}
	}()
	primaryIOAwaitClosed(t, seekEntered)
	closeResult := primaryIOReadSeekerCloseAsync(reader)
	primaryIOAwaitValue(t, cancelObserved)
	primaryIOAwaitClosed(t, closeAck)
	primaryIOReadSeekerAssertPending(t, closeResult)
	primaryIOReadSeekerAssertPending(t, result)
	primaryIOAssertCounts(t, g, 0, 0, 0)
	if retired.Load() != 0 || owners.Stats().RegisteredOwners != 1 {
		t.Fatal("Close retired an entered metadata seek")
	}
	releaseSeek()
	seek := primaryIOAwaitValue(t, result)
	if seek.position != 2 || seek.err != nil {
		t.Fatalf("joined metadata seek: %+v", seek)
	}
	if err := primaryIOAwaitValue(t, closeResult); err != nil {
		t.Fatal(err)
	}
	reads, seeks, closes := source.counts()
	if reads != 0 || seeks != 1 || closes != 1 || retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("metadata seek retirement: read=%d seek=%d close=%d retired=%d owners=%+v", reads, seeks, closes, retired.Load(), owners.Stats())
	}
}

func TestPrimaryIOReadSeekerCloseJoinsEnteredMultipartReadAndRejectsLateCalls(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	readEntered, closeAck := make(chan struct{}), make(chan struct{})
	readMayReturn, releaseRead := primaryIOReadSeekerGate(t)
	var retired atomic.Int32
	source := &primaryIOReadSeekerTestSource{
		readFn: func([]byte) (int, error) {
			close(readEntered)
			<-readMayReturn
			return 0, os.ErrClosed
		},
		closeFn: func() error { close(closeAck); return nil },
	}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() { retired.Add(1) })
	cancelObserved := primaryIOReadSeekerObserveCancel(reader)
	producerRead := primaryIOReadSeekerReadAsync(reader, make([]byte, 8))
	primaryIOAwaitClosed(t, readEntered)
	// A multipart producer has entered Read when its consumer returns. Close
	// must join that producer even though source.Close has already acknowledged.
	closeResult := primaryIOReadSeekerCloseAsync(reader)
	primaryIOAwaitValue(t, cancelObserved)
	primaryIOAwaitClosed(t, closeAck)
	primaryIOReadSeekerAssertPending(t, closeResult)
	primaryIOReadSeekerAssertPending(t, producerRead)
	primaryIOAssertCounts(t, g, 1, 0, 0)
	if owners.Stats().RegisteredOwners != 1 || retired.Load() != 0 {
		t.Fatal("Close completed retained ownership before the entered multipart read retired")
	}
	if n, err := reader.Read(make([]byte, 8)); n != 0 || !errors.Is(err, ErrClosed) {
		t.Fatalf("late read: n=%d err=%v", n, err)
	}
	if position, err := reader.Seek(0, io.SeekStart); position != 0 || !errors.Is(err, ErrClosed) {
		t.Fatalf("late seek: position=%d err=%v", position, err)
	}
	reads, seeks, closes := source.counts()
	if reads != 1 || seeks != 0 || closes != 1 {
		t.Fatalf("late methods reached the source: read=%d seek=%d close=%d", reads, seeks, closes)
	}
	releaseRead()
	read := primaryIOAwaitValue(t, producerRead)
	if !errors.Is(read.err, context.Canceled) {
		t.Fatalf("entered producer result: %+v", read)
	}
	if err := primaryIOAwaitValue(t, closeResult); err != nil {
		t.Fatalf("close after actual producer return: %v", err)
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
	if retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("multipart retirement: retired=%d owners=%+v", retired.Load(), owners.Stats())
	}
}

func TestPrimaryIOReadSeekerCloseCancelsQueuedAcquireWithoutSourceRead(t *testing.T) {
	limits := Limits{Owners: 1, RootOwners: 1, DomainOwners: 1, Queued: 2, RootQueued: 2, DomainQueued: 2}
	g, owners := primaryIOFixture(t, limits)
	holder := primaryIOOwner(t, owners, context.Background())
	holderLease := primaryIOAcquire(t, holder, primaryIORoute("a", "da"), Foreground)
	owner := primaryIOOwner(t, owners, context.Background())
	source := &primaryIOReadSeekerTestSource{}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, nil)
	reader.owner.state.mu.Lock()
	observed := &primaryIOQueueContext{Context: reader.owner.state.ctx, entered: make(chan struct{})}
	reader.owner.state.ctx = observed
	reader.owner.state.mu.Unlock()
	readResult := primaryIOReadSeekerReadAsync(reader, make([]byte, 8))
	primaryIOAwaitClosed(t, observed.entered)
	primaryIOAssertCounts(t, g, 1, 0, 1)
	primaryIOReadSeekerAssertPending(t, readResult)
	closeResult := primaryIOReadSeekerCloseAsync(reader)
	read := primaryIOAwaitValue(t, readResult)
	if read.n != 0 || !errors.Is(read.err, context.Canceled) {
		t.Fatalf("canceled admission: %+v", read)
	}
	if err := primaryIOAwaitValue(t, closeResult); err != nil {
		t.Fatalf("queued consumer cleanup: %v", err)
	}
	reads, seeks, closes := source.counts()
	if reads != 0 || seeks != 0 || closes != 1 || owners.Stats().RegisteredOwners != 1 {
		t.Fatalf("queued consumer touched source or released unrelated owner: read=%d seek=%d close=%d owners=%+v", reads, seeks, closes, owners.Stats())
	}
	primaryIOAssertCounts(t, g, 1, 0, 0)
	primaryIORelease(t, holderLease)
	primaryIOComplete(t, holder)
}

func TestPrimaryIOReadSeekerValidatesCountsAndRecordsNonEOFErrors(t *testing.T) {
	sourceFailure := errors.New("source failure")
	cases := []struct {
		name       string
		count      int
		returned   error
		wantCount  int
		wantErr    error
		remembered error
	}{
		{"negative", -1, nil, 0, ErrReadResult, ErrReadResult},
		{"oversized", 9, nil, 0, ErrReadResult, ErrReadResult},
		{"non EOF with bytes", 1, sourceFailure, 1, sourceFailure, sourceFailure},
		{"EOF", 0, io.EOF, 0, io.EOF, nil},
		{"short EOF", 1, io.EOF, 1, io.EOF, nil},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g, owners := primaryIOFixture(t, primaryIOLimits())
			owner := primaryIOOwner(t, owners, context.Background())
			source := &primaryIOReadSeekerTestSource{readFn: func([]byte) (int, error) { return test.count, test.returned }}
			reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, nil)
			n, err := reader.Read(make([]byte, 16))
			if n != test.wantCount || !errors.Is(err, test.wantErr) || !errors.Is(reader.Err(), test.remembered) {
				t.Fatalf("read validation: n=%d err=%v remembered=%v", n, err, reader.Err())
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
			if err := reader.Close(); err != nil {
				t.Fatalf("cleanup replayed a historical source error: %v", err)
			}
			if owners.Stats().RegisteredOwners != 0 {
				t.Fatal("normal cleanup retained the owner after source-read failure")
			}
		})
	}
}

func TestPrimaryIOReadSeekerFirstErrorIncludesAdmissionAndRemainsSticky(t *testing.T) {
	t.Run("admission", func(t *testing.T) {
		g, owners := primaryIOFixture(t, primaryIOLimits())
		owner := primaryIOOwner(t, owners, context.Background())
		source := &primaryIOReadSeekerTestSource{}
		reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, nil)
		g.Close()
		if n, err := reader.Read(make([]byte, 8)); n != 0 || !errors.Is(err, ErrClosed) || !errors.Is(reader.Err(), ErrClosed) {
			t.Fatalf("admission failure: n=%d err=%v remembered=%v", n, err, reader.Err())
		}
		reads, _, _ := source.counts()
		if reads != 0 {
			t.Fatal("failed admission reached the source")
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("cleanup replayed an admission failure: %v", err)
		}
	})
	t.Run("first source error", func(t *testing.T) {
		_, owners := primaryIOFixture(t, primaryIOLimits())
		owner := primaryIOOwner(t, owners, context.Background())
		first, second := errors.New("read failed"), errors.New("seek failed")
		source := &primaryIOReadSeekerTestSource{
			readFn: func([]byte) (int, error) { return 0, first },
			seekFn: func(int64, int) (int64, error) { return 0, second },
		}
		reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, nil)
		if _, err := reader.Read(make([]byte, 8)); !errors.Is(err, first) {
			t.Fatalf("first read: %v", err)
		}
		if _, err := reader.Seek(0, io.SeekStart); !errors.Is(err, second) {
			t.Fatalf("subsequent seek: %v", err)
		}
		if !errors.Is(reader.Err(), first) {
			t.Fatalf("later failure replaced the first: %v", reader.Err())
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPrimaryIOReadSeekerFailedConstructorLeavesOwnershipAndCleanupWithCaller(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil source", ErrInvalid},
		{"zero chunk", ErrInvalid},
		{"oversized chunk", ErrInvalid},
		{"invalid route", ErrInvalid},
		{"invalid class", ErrInvalid},
		{"canceled owner", context.Canceled},
		{"active owner", ErrOwnerBusy},
		{"stale owner", ErrStaleOwner},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, owners := primaryIOFixture(t, primaryIOLimits())
			owner := primaryIOOwner(t, owners, context.Background())
			cleanupOwner := owner
			source := &primaryIOReadSeekerTestSource{}
			var sourceArg ReadSeekCloser = source
			route, class, chunk := primaryIORoute("a", "da"), Foreground, 8
			var held *PrimaryReadLease
			var retired atomic.Int32
			switch test.name {
			case "nil source":
				sourceArg = nil
			case "zero chunk":
				chunk = 0
			case "oversized chunk":
				chunk = MaxReadChunkBytes + 1
			case "invalid route":
				route = Route{}
			case "invalid class":
				class = Class(255)
			case "canceled owner":
				if err := owner.Cancel(); err != nil {
					t.Fatal(err)
				}
			case "active owner":
				held = primaryIOAcquire(t, owner, route, Foreground)
			case "stale owner":
				var err error
				cleanupOwner, err = owner.Transfer()
				if err != nil {
					t.Fatal(err)
				}
			}
			owner.state.mu.Lock()
			before := owner.state.generation
			owner.state.mu.Unlock()
			reader, err := NewReadSeeker(owner, route, class, sourceArg, chunk, func() { retired.Add(1) })
			if reader != nil || !errors.Is(err, test.err) {
				t.Fatalf("failed construction: reader=%v err=%v", reader, err)
			}
			owner.state.mu.Lock()
			after := owner.state.generation
			owner.state.mu.Unlock()
			reads, seeks, closes := source.counts()
			if after != before || reads != 0 || seeks != 0 || closes != 0 || retired.Load() != 0 {
				t.Fatalf("constructor failure moved rights or cleanup: generation=%d->%d read=%d seek=%d close=%d retired=%d", before, after, reads, seeks, closes, retired.Load())
			}
			if held != nil {
				primaryIORelease(t, held)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			primaryIOComplete(t, cleanupOwner)
			if owners.Stats().RegisteredOwners != 0 {
				t.Fatal("constructor failure prevented caller cleanup")
			}
		})
	}
}

func TestPrimaryIOReadSeekerExposesNoSourceFastPath(t *testing.T) {
	_, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), &primaryIOReadSeekerTestSource{}, 8, nil)
	if _, ok := any(reader).(io.WriterTo); ok {
		t.Error("WriterTo bypasses per-read admission")
	}
	if _, ok := any(reader).(io.ReaderAt); ok {
		t.Error("ReaderAt bypasses per-read admission")
	}
	if _, ok := any(reader).(interface{ Fd() uintptr }); ok {
		t.Error("raw descriptor bypasses per-read admission")
	}
	for _, name := range []string{"WriteTo", "ReadAt", "Fd", "Unwrap", "Source"} {
		if _, ok := reflect.TypeOf(reader).MethodByName(name); ok {
			t.Errorf("exposed source fast path %s", name)
		}
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryIOReadSeekerAbnormalSourceCloseRetainsOwner(t *testing.T) {
	cleanupFailure := errors.New("descriptor retirement failed")
	cases := []struct {
		name    string
		closeFn func() error
		wantErr error
	}{
		{"error", func() error { return cleanupFailure }, cleanupFailure},
		{"panic", func() error { panic("source Close panic") }, ErrConsumerRetirement},
		{"goexit", func() error { runtime.Goexit(); return nil }, ErrConsumerRetirement},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			g, owners := primaryIOFixture(t, primaryIOLimits())
			owner := primaryIOOwner(t, owners, context.Background())
			var retired atomic.Int32
			source := &primaryIOReadSeekerTestSource{closeFn: test.closeFn}
			reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() { retired.Add(1) })
			if err := primaryIOAwaitValue(t, primaryIOReadSeekerCloseAsync(reader)); !errors.Is(err, test.wantErr) {
				t.Fatalf("uncertain source cleanup: %v", err)
			}
			if err := reader.Close(); !errors.Is(err, test.wantErr) {
				t.Fatalf("repeated Close fabricated retirement: %v", err)
			}
			reads, seeks, closes := source.counts()
			if reads != 0 || seeks != 0 || closes != 1 || retired.Load() != 0 || owners.Stats().RegisteredOwners != 1 {
				t.Fatalf("abnormal source retirement released retained owner: read=%d seek=%d close=%d retired=%d owners=%+v", reads, seeks, closes, retired.Load(), owners.Stats())
			}
			primaryIOAssertCounts(t, g, 0, 0, 0)
			if n, err := reader.Read(make([]byte, 8)); n != 0 || !errors.Is(err, ErrClosed) {
				t.Fatalf("read after uncertain cleanup: n=%d err=%v", n, err)
			}
		})
	}
}

func TestPrimaryIOReadSeekerAlreadyClosedDescriptorEstablishesRetirement(t *testing.T) {
	_, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	var retired atomic.Int32
	source := &primaryIOReadSeekerTestSource{closeFn: func() error { return os.ErrClosed }}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() { retired.Add(1) })
	if err := primaryIOAwaitValue(t, primaryIOReadSeekerCloseAsync(reader)); err != nil {
		t.Fatalf("already-closed descriptor cleanup: %v", err)
	}
	if retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("known retired descriptor: retired=%d owners=%+v", retired.Load(), owners.Stats())
	}
}

func TestPrimaryIOReadSeekerAbnormalRetiredCallbackCannotMakeSecondCloseSucceed(t *testing.T) {
	for _, name := range []string{"panic", "goexit"} {
		t.Run(name, func(t *testing.T) {
			_, owners := primaryIOFixture(t, primaryIOLimits())
			owner := primaryIOOwner(t, owners, context.Background())
			var retired atomic.Int32
			source := &primaryIOReadSeekerTestSource{}
			reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() {
				retired.Add(1)
				if name == "goexit" {
					runtime.Goexit()
				}
				panic("retired callback panic")
			})
			callerDone, returned := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(callerDone)
				defer func() { _ = recover() }()
				_ = reader.Close()
				close(returned)
			}()
			primaryIOAwaitClosed(t, callerDone)
			primaryIOAssertOpen(t, returned)
			if err := reader.Close(); !errors.Is(err, ErrConsumerRetirement) {
				t.Fatalf("second Close after abnormal retired callback: %v", err)
			}
			_, _, closes := source.counts()
			if closes != 1 || retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
				t.Fatalf("external callback uncertainty changed verified native retirement: close=%d retired=%d owners=%+v", closes, retired.Load(), owners.Stats())
			}
		})
	}
}

func TestPrimaryIOReadSeekerCompleteFailurePreservesExternalPin(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	var reader *ReadSeeker
	var retired, externalPin atomic.Int32
	externalPin.Store(1)
	source := &primaryIOReadSeekerTestSource{closeFn: func() error {
		// Inject a bookkeeping fault exactly at the retirement boundary. The
		// descriptor Close itself succeeds, but Complete must reject this
		// outstanding-work assertion and preserve the external Store pin.
		reader.owner.state.mu.Lock()
		reader.owner.state.leases = 1
		reader.owner.state.mu.Unlock()
		return nil
	}}
	reader = primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() {
		retired.Add(1)
		externalPin.Store(0)
	})
	if err := primaryIOAwaitValue(t, primaryIOReadSeekerCloseAsync(reader)); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("owner completion failure: %v", err)
	}
	if err := reader.Close(); !errors.Is(err, ErrOwnerBusy) {
		t.Fatalf("repeated Close hid completion failure: %v", err)
	}
	reads, seeks, closes := source.counts()
	if reads != 0 || seeks != 0 || closes != 1 || retired.Load() != 0 || externalPin.Load() != 1 || owners.Stats().RegisteredOwners != 1 {
		t.Fatalf("Complete failure released external ownership: read=%d seek=%d close=%d retired=%d pin=%d owners=%+v", reads, seeks, closes, retired.Load(), externalPin.Load(), owners.Stats())
	}
	reader.owner.state.mu.Lock()
	completed := reader.owner.state.completed
	reader.owner.state.mu.Unlock()
	if completed {
		t.Fatal("failed Complete marked the retained owner completed")
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}

func TestPrimaryIOReadSeekerConcurrentCloseIsIdempotent(t *testing.T) {
	g, owners := primaryIOFixture(t, primaryIOLimits())
	owner := primaryIOOwner(t, owners, context.Background())
	closeEntered := make(chan struct{})
	closeMayReturn, releaseClose := primaryIOReadSeekerGate(t)
	var retired atomic.Int32
	source := &primaryIOReadSeekerTestSource{closeFn: func() error {
		close(closeEntered)
		<-closeMayReturn
		return nil
	}}
	reader := primaryIOReadSeekerNew(t, owner, primaryIORoute("a", "da"), source, 8, func() { retired.Add(1) })
	first := primaryIOReadSeekerCloseAsync(reader)
	primaryIOAwaitClosed(t, closeEntered)
	second := primaryIOReadSeekerCloseAsync(reader)
	primaryIOReadSeekerAssertPending(t, first)
	primaryIOReadSeekerAssertPending(t, second)
	if retired.Load() != 0 || owners.Stats().RegisteredOwners != 1 {
		t.Fatal("concurrent Close retired ownership before native cleanup returned")
	}
	releaseClose()
	if err := primaryIOAwaitValue(t, first); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := primaryIOAwaitValue(t, second); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
	_, _, closes := source.counts()
	if closes != 1 || retired.Load() != 1 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("duplicate cleanup: close=%d retired=%d owners=%+v", closes, retired.Load(), owners.Stats())
	}
	primaryIOAssertCounts(t, g, 0, 0, 0)
}
