//go:build linux

package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

const primaryScanReadHelperEnvironment = "GOBY_LIBRARY_PRIMARY_SCAN_READ_HELPER"

var primaryScanReadSourceBytes = bytes.Repeat([]byte("primary-scan-fd:"), 2200)[:originalMediaReadChunk]

// This child reads the production gateway's inherited descriptor, not the
// source pathname. The optional socket is an actual process readiness gate.
// The helper proves conventional child ownership; it makes no native-domain
// or descendant containment claim.
func TestPrimaryScanReadInheritedFDHelper(t *testing.T) {
	if os.Getenv(primaryScanReadHelperEnvironment) != "1" {
		return
	}
	source := os.NewFile(3, "primary-scan-inherited-source")
	data := make([]byte, originalMediaReadChunk)
	if source == nil {
		os.Exit(81)
	}
	if n, err := source.ReadAt(data, 0); err != nil || n != len(data) || !bytes.Equal(data, primaryScanReadSourceBytes) {
		os.Exit(82)
	}
	if path := os.Getenv("GOBY_LIBRARY_PRIMARY_SCAN_READ_SOCKET"); path != "" {
		connection, err := net.Dial("unix", path)
		if err != nil {
			os.Exit(83)
		}
		if err := json.NewEncoder(connection).Encode(primaryScanReadChildReady{PID: os.Getpid(), Bytes: len(data)}); err != nil {
			os.Exit(84)
		}
		var signal [1]byte
		if _, err := io.ReadFull(connection, signal[:]); err != nil {
			os.Exit(85)
		}
		_ = connection.Close()
	}
	info, err := source.Stat()
	if err != nil {
		os.Exit(86)
	}
	output := map[string]any{
		"format":  map[string]any{"format_name": "mov,mp4", "duration": "1.5", "size": strconv.FormatInt(info.Size(), 10), "bit_rate": "128000"},
		"streams": []map[string]any{{"index": 0, "codec_name": "h264", "codec_type": "video", "width": 16, "height": 16}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(output); err != nil {
		os.Exit(87)
	}
	os.Exit(0)
}

type primaryScanReadChildReady struct {
	PID   int
	Bytes int
}

type primaryScanReadTestProber struct {
	media.Prober
	joined   bool
	actual   bool
	calls    atomic.Int64
	entered  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	once     sync.Once
	unblock  sync.Once
}

func (p *primaryScanReadTestProber) ProbeFileJoinedContract() bool { return p.joined }

// The direct callback borrows the real FD synchronously and joins its read.
// Embedding media.Prober also exercises dispatch precedence over its promoted
// ProbeFileOwned method. A nil callback error is not an OS retirement witness.
func (p *primaryScanReadTestProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	p.calls.Add(1)
	var info media.Info
	var err error
	if p.actual {
		info, err = p.Prober.ProbeFile(ctx, file)
	} else {
		data := make([]byte, originalMediaReadChunk)
		if n, readErr := file.ReadAt(data, 0); readErr != nil || n != len(data) || !bytes.Equal(data, primaryScanReadSourceBytes) {
			return media.Info{}, fmt.Errorf("actual callback source read: bytes=%d error=%v", n, readErr)
		}
		info, err = scanProbeFixtureInfo(file)
	}
	if err != nil {
		return media.Info{}, err
	}
	if p.entered != nil {
		p.once.Do(func() { close(p.entered) })
	}
	if p.release != nil {
		stop := context.AfterFunc(ctx, func() {
			if p.canceled != nil {
				close(p.canceled)
			}
		})
		defer stop()
		<-p.release // The actual callback owner must join before its FD retires.
	}
	return info, ctx.Err()
}

func (p *primaryScanReadTestProber) openGate() {
	if p.release != nil {
		p.unblock.Do(func() { close(p.release) })
	}
}

func primaryScanReadTestGate() *primaryScanReadTestProber {
	return &primaryScanReadTestProber{joined: true, entered: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
}

type primaryScanReadFixture struct {
	ctx               context.Context
	pool              *pgxpool.Pool
	store             *Store
	state             *scanState
	path              string
	userID            string
	input             *primaryScanRead
	file              *os.File
	allowedCloseError error
}

func primaryScanReadFixtureAt(t *testing.T, prober Prober, allowedPath string) primaryScanReadFixture {
	t.Helper()
	ctx, pool, store, configured, user := libraryIntegrationStore(t, prober)
	if allowedPath == "" {
		allowedPath = configured
	} else if allowedPath != configured {
		store.mu.Lock()
		store.roots = append(store.roots, approvedRoot{path: allowedPath})
		store.mu.Unlock()
	}
	directory, err := os.MkdirTemp(allowedPath, "primary-scan-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := libraryIntegrationFile(t, directory, "Feature.mp4", string(primaryScanReadSourceBytes))
	library := libraryIntegrationCreate(t, ctx, store, "Primary scan read", "movies", directory)
	task, _ := scanUnchangedProgressTask(t, ctx, pool, library)
	store.mu.Lock()
	store.active[task.job.ID] = task
	store.mu.Unlock()
	var root libraryRoot
	if err := pool.QueryRow(ctx, "SELECT id,library_id,path,allowed_path,relative_path FROM library_roots WHERE library_id=$1", library.ID).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
		t.Fatal(err)
	}
	opened, err := store.openLibraryRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &scanState{store: store, task: task, library: library, root: root, opened: opened}
	t.Cleanup(func() {
		task.cancel()
		_ = opened.Close()
		store.mu.Lock()
		delete(store.active, task.job.ID)
		store.mu.Unlock()
	})
	return primaryScanReadFixture{ctx: ctx, pool: pool, store: store, state: state, path: path, userID: user}
}

func (f *primaryScanReadFixture) prepare(t *testing.T) {
	t.Helper()
	input, err := f.state.preparePrimaryScanRead()
	if err != nil {
		t.Fatal(err)
	}
	file, err := openScanFile(f.state.opened, "Feature.mp4")
	if err != nil {
		_ = primaryScanReadClose(t, input, nil)
		t.Fatal(err)
	}
	if err := input.attach(file, "Feature.mp4"); err != nil {
		_ = primaryScanReadClose(t, input, file.Close)
		t.Fatal(err)
	}
	f.input, f.file = input, file
	// Registered last so actual source cleanup precedes the fixture's root,
	// task, Store, and schema cleanup on every early assertion failure.
	t.Cleanup(func() {
		if err := primaryScanReadClose(t, input, file.Close); err != nil && (f.allowedCloseError == nil || !errors.Is(err, f.allowedCloseError)) {
			t.Errorf("retire actual scan input: %v", err)
		}
	})
}

func primaryScanReadClose(t *testing.T, input *primaryScanRead, closeFile func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- input.close(closeFile) }()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		return errors.New("actual scan descriptor cleanup did not join within its bounded fixture deadline")
	}
}

type primaryScanReadProbeResult struct {
	info media.Info
	err  error
}

func primaryScanReadStartProbe(t *testing.T, f primaryScanReadFixture, prober Prober) <-chan primaryScanReadProbeResult {
	t.Helper()
	result := make(chan primaryScanReadProbeResult, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		if gate, ok := prober.(*primaryScanReadTestProber); ok {
			gate.openGate()
		}
		f.state.task.cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("actual scan probe did not join during bounded cleanup")
		}
	})
	go func() {
		defer close(finished)
		info, err := f.input.probe(prober, f.file)
		result <- primaryScanReadProbeResult{info, err}
	}()
	return result
}

func primaryScanReadReceive(t *testing.T, ctx context.Context, result <-chan primaryScanReadProbeResult) primaryScanReadProbeResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-ctx.Done():
		t.Fatalf("actual scan probe did not join: %v", ctx.Err())
		return primaryScanReadProbeResult{}
	}
}

func primaryScanReadSignal(t *testing.T, ctx context.Context, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatalf("waiting for %s: %v", label, ctx.Err())
	}
}

// Real blocked callback owners establish admission state. Scheduling yields
// only let the pending acquire run; no elapsed interval is the queue proof.
func primaryScanReadQueued(t *testing.T, ctx context.Context, baseline primaryio.Stats, result <-chan primaryScanReadProbeResult) {
	t.Helper()
	for {
		stats := originalMediaReadGovernor.Stats()
		if stats.Queued == baseline.Queued+1 {
			if stats.Active != baseline.Active || stats.Background != baseline.Background {
				t.Fatalf("queued scan was charged before grant: before=%+v after=%+v", baseline, stats)
			}
			select {
			case got := <-result:
				t.Fatalf("queued scan returned before its grant: %+v", got)
			default:
			}
			return
		}
		select {
		case got := <-result:
			t.Fatalf("scan did not become a queue member: result=%+v stats=%+v", got, stats)
		case <-ctx.Done():
			t.Fatalf("scan queue membership not observed: %+v", stats)
		default:
			runtime.Gosched()
		}
	}
}

func TestPrimaryScanReadQueuedBindingAndTaskFence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(primaryScanReadFixture) error
		want   error
	}{
		{"binding-revision", func(f primaryScanReadFixture) error {
			_, err := f.pool.Exec(f.ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", f.state.root.id)
			return err
		}, ErrRootBindingConflict},
		{"persisted-task-cancel", func(f primaryScanReadFixture) error {
			_, err := f.pool.Exec(f.ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", f.state.task.job.ID)
			return err
		}, context.Canceled},
		{"queued-source-change", func(f primaryScanReadFixture) error {
			info, err := os.Stat(f.path)
			if err != nil {
				return err
			}
			return os.Chtimes(f.path, info.ModTime().Add(time.Second), info.ModTime().Add(time.Second))
		}, errScanProbeSourceChanged},
	} {
		t.Run(test.name, func(t *testing.T) {
			firstGate, secondGate := primaryScanReadTestGate(), primaryScanReadTestGate()
			first := primaryScanReadFixtureAt(t, firstGate, "")
			second := primaryScanReadFixtureAt(t, secondGate, first.state.root.allowedPath)
			candidateProber := &primaryScanReadTestProber{joined: true}
			candidate := primaryScanReadFixtureAt(t, candidateProber, first.state.root.allowedPath)
			first.prepare(t)
			second.prepare(t)
			candidate.prepare(t)
			t.Cleanup(firstGate.openGate)
			t.Cleanup(secondGate.openGate)
			firstResult, secondResult := primaryScanReadStartProbe(t, first, firstGate), primaryScanReadStartProbe(t, second, secondGate)
			primaryScanReadSignal(t, first.ctx, firstGate.entered, "first actual background source read")
			primaryScanReadSignal(t, first.ctx, secondGate.entered, "second actual background source read")
			baseline := originalMediaReadGovernor.Stats()
			if baseline.Active != 2 || baseline.Background != 2 {
				t.Fatalf("actual background fixture did not own both phases: %+v", baseline)
			}
			result := primaryScanReadStartProbe(t, candidate, candidateProber)
			primaryScanReadQueued(t, first.ctx, baseline, result)
			if err := test.change(candidate); err != nil {
				t.Fatal(err)
			}
			firstGate.openGate()
			if got := primaryScanReadReceive(t, first.ctx, firstResult); got.err != nil {
				t.Fatal(got.err)
			}
			got := primaryScanReadReceive(t, first.ctx, result)
			if !errors.Is(got.err, test.want) || candidateProber.calls.Load() != 0 || got.info.Size != 0 {
				t.Fatalf("grant used stale authority or delivered payload: info=%+v calls=%d error=%v", got.info, candidateProber.calls.Load(), got.err)
			}
			var failure *primaryScanReadFailure
			if test.want == errScanProbeSourceChanged && errors.As(got.err, &failure) {
				t.Fatalf("retired pre-backend source rejection became an ownership fault: %v", got.err)
			}
			if err := primaryScanReadClose(t, candidate.input, candidate.file.Close); err != nil {
				t.Fatal(err)
			}
			secondGate.openGate()
			if got := primaryScanReadReceive(t, first.ctx, secondResult); got.err != nil {
				t.Fatal(got.err)
			}
		})
	}
}

func primaryScanReadTool(t *testing.T, socket string) media.Prober {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	tool := filepath.Join(t.TempDir(), "primary-scan-ffprobe")
	script := "#!/bin/sh\nexport " + primaryScanReadHelperEnvironment + "=1\nexport GOBY_LIBRARY_PRIMARY_SCAN_READ_SOCKET=" + quote(socket) + "\nexec " + quote(executable) + " -test.run='^TestPrimaryScanReadInheritedFDHelper$' -- \"$@\"\n"
	if err := os.WriteFile(tool, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return media.Prober{FFprobePath: tool, Timeout: 15 * time.Second}
}

func TestPrimaryScanReadStoreCloseRetainsActualFD(t *testing.T) {
	directory, err := os.MkdirTemp("", "psr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove actual reader socket directory: %v", err)
		}
	})
	socket := filepath.Join(directory, "reader.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	gate := primaryScanReadTestGate()
	gate.actual, gate.Prober = true, primaryScanReadTool(t, socket)
	f := primaryScanReadFixtureAt(t, gate, "")
	f.prepare(t)
	t.Cleanup(gate.openGate)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners - 1
	beforeIO := originalMediaReadGovernor.Stats()
	result := primaryScanReadStartProbe(t, f, gate)
	type child struct {
		connection net.Conn
		ready      primaryScanReadChildReady
		err        error
	}
	children := make(chan child, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			children <- child{err: err}
			return
		}
		var ready primaryScanReadChildReady
		err = json.NewDecoder(connection).Decode(&ready)
		children <- child{connection, ready, err}
	}()
	var actual child
	select {
	case actual = <-children:
	case <-f.ctx.Done():
		t.Fatal("actual inherited-FD helper never became ready")
	}
	if actual.connection != nil {
		t.Cleanup(func() { _ = actual.connection.Close() })
	}
	if actual.err != nil || actual.ready.PID <= 0 || actual.ready.Bytes != originalMediaReadChunk {
		t.Fatalf("invalid actual child source observation: %+v", actual)
	}
	if stats := originalMediaReadGovernor.Stats(); stats.Active != beforeIO.Active+1 || stats.Background != beforeIO.Background+1 {
		t.Fatalf("child source use did not retain one logical background phase: %+v", stats)
	}
	if _, err := actual.connection.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	primaryScanReadSignal(t, f.ctx, gate.entered, "joined real child and blocked actual callback")
	if err := syscall.Kill(actual.ready.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("actual probe child was not reaped before callback gate: %v", err)
	}
	f.store.mu.Lock()
	anchor, anchorErr := f.store.approvedLibraryRootLocked(f.state.root)
	f.store.mu.Unlock()
	if anchorErr != nil || anchor == nil {
		t.Fatalf("fixture did not retain its actual root binding witness: %v", anchorErr)
	}
	closeContext, cancelClose := context.WithCancel(context.Background())
	cancelClose()
	if err := f.store.Close(closeContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("bounded Store close did not return its canceled deadline: %v", err)
	}
	primaryScanReadSignal(t, f.ctx, gate.canceled, "actual callback Store cancellation")
	if _, err := f.file.Stat(); err != nil {
		t.Fatalf("cancellation closed the actual borrowed FD: %v", err)
	}
	if _, err := anchor.Stat("."); err != nil {
		t.Fatalf("Store retired the root witness before actual join: %v", err)
	}
	if stats := originalMediaReadGovernor.Stats(); stats.Active != beforeIO.Active+1 || stats.Background != beforeIO.Background+1 {
		t.Fatalf("Store cancellation released an actual callback phase: %+v", stats)
	}
	owners := originalMediaReadOwners.Stats()
	if owners.RegisteredOwners != beforeOwners+1 || owners.MaximumOwners != 64 {
		t.Fatalf("canceled actual FD lost its finite retained owner: %+v", owners)
	}
	select {
	case <-f.store.done:
		t.Fatal("Store completed before actual callback and FD cleanup")
	default:
	}
	// Empty registrations exercise only the finite retained-owner admission
	// boundary. The canceled scan's real FD is the one retirement witness.
	var placeholders []*primaryio.Owner
	t.Cleanup(func() {
		for _, owner := range placeholders {
			if err := owner.Complete(); err != nil {
				t.Errorf("retire empty capacity registration: %v", err)
			}
		}
	})
	for count := owners.RegisteredOwners; count < owners.MaximumOwners; count++ {
		owner, err := originalMediaReadOwners.Register(f.ctx)
		if err != nil {
			t.Fatalf("fill finite retained-owner admission: %v", err)
		}
		placeholders = append(placeholders, owner)
	}
	if extra, err := originalMediaReadOwners.Register(f.ctx); !errors.Is(err, primaryio.ErrBusy) || extra != nil {
		if extra != nil {
			_ = extra.Complete()
		}
		t.Fatalf("canceled actual FD escaped the 64-owner boundary: owner=%v error=%v", extra, err)
	}
	for _, owner := range placeholders {
		if err := owner.Complete(); err != nil {
			t.Fatal(err)
		}
	}
	gate.openGate()
	got := primaryScanReadReceive(t, f.ctx, result)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("canceled callback delivered accepted facts: %+v", got)
	}
	if err := primaryScanReadClose(t, f.input, f.file.Close); err != nil {
		t.Fatal(err)
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCleanup()
	if err := f.store.Close(cleanupContext); err != nil {
		t.Fatalf("actual scan owner did not finish Store drain: %v", err)
	}
	if _, err := f.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("retired actual input FD remains open: %v", err)
	}
	if _, err := anchor.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("completed Store drain retained its root witness: %v", err)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
		t.Fatalf("scan retirement retained phase charges: before=%+v after=%+v", beforeIO, stats)
	}
	if stats := originalMediaReadOwners.Stats(); stats.RegisteredOwners != beforeOwners {
		t.Fatalf("actual scan retirement retained ownership: %+v", stats)
	}
}

func primaryScanReadIndexForeground(t *testing.T, f primaryScanReadFixture) (string, MediaFile) {
	t.Helper()
	stat, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	info := libraryMediaFixture(primaryScanReadSourceBytes)
	info.ProbeVersion, info.FileChangeTimeNs = media.CurrentProbeVersion, media.FileChangeTime(stat)
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items (id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,media,file_identity,file_size,modified_at)
		VALUES($1,$2,$3,$2,'Feature','feature','Movie',false,$4,'Feature.mp4',$5,$6,$7,$8)`, id, f.state.library.ID, f.state.root.id, f.path, raw, fileIdentity(stat), stat.Size(), catalogModifiedTime(stat)); err != nil {
		t.Fatal(err)
	}
	file, snapshot, err := f.store.OpenMedia(f.ctx, f.userID, id, "")
	if file != nil {
		_ = file.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return id, snapshot
}

func TestPrimaryScanReadSharedForegroundBudgetAcrossStores(t *testing.T) {
	firstGate, secondGate := primaryScanReadTestGate(), primaryScanReadTestGate()
	first := primaryScanReadFixtureAt(t, firstGate, "")
	second := primaryScanReadFixtureAt(t, secondGate, first.state.root.allowedPath)
	foreground := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, first.state.root.allowedPath)
	unrelated := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, "")
	first.prepare(t)
	second.prepare(t)
	unrelated.prepare(t)
	t.Cleanup(firstGate.openGate)
	t.Cleanup(secondGate.openGate)
	firstResult, secondResult := primaryScanReadStartProbe(t, first, firstGate), primaryScanReadStartProbe(t, second, secondGate)
	primaryScanReadSignal(t, first.ctx, firstGate.entered, "first Store actual background input")
	primaryScanReadSignal(t, first.ctx, secondGate.entered, "second Store actual background input")
	if first.input.route.Roots[0].Catalog == second.input.route.Roots[0].Catalog || first.input.route.Domains[0] != second.input.route.Domains[0] {
		t.Fatalf("fixture did not combine independent catalog identities on one domain: first=%+v second=%+v", first.input.route, second.input.route)
	}
	baseline := originalMediaReadGovernor.Stats()
	if baseline.Active != 2 || baseline.Background != 2 {
		t.Fatalf("shared Stores did not consume the process background budget: %+v", baseline)
	}
	queued := primaryScanReadStartProbe(t, unrelated, unrelated.store.prober)
	primaryScanReadQueued(t, first.ctx, baseline, queued)
	id, snapshot := primaryScanReadIndexForeground(t, foreground)
	file, _, reader, err := foreground.store.OpenOriginalMediaFor(foreground.ctx, Subject{UserID: foreground.userID}, id, snapshot.SourceID, snapshot.ETag)
	if err != nil || file == nil || reader == nil {
		t.Fatalf("reserved foreground reader did not open: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	data := make([]byte, originalMediaReadChunk)
	if n, err := reader.Read(data); err != nil || n != len(data) || !bytes.Equal(data, primaryScanReadSourceBytes) {
		t.Fatalf("reserved foreground phase did not read the actual original 32 KiB: bytes=%d error=%v", n, err)
	}
	if stats := originalMediaReadGovernor.Stats(); stats.Active != 2 || stats.Background != 2 || stats.Queued != baseline.Queued+1 {
		t.Fatalf("foreground body read damaged shared background ownership: %+v", stats)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	unrelatedID, unrelatedSnapshot := primaryScanReadIndexForeground(t, unrelated)
	unrelatedFile, _, unrelatedReader, err := unrelated.store.OpenOriginalMediaFor(unrelated.ctx, Subject{UserID: unrelated.userID}, unrelatedID, unrelatedSnapshot.SourceID, unrelatedSnapshot.ETag)
	if err != nil || unrelatedFile == nil || unrelatedReader == nil {
		t.Fatalf("unrelated foreground domain did not bypass the ineligible BG queue: %v", err)
	}
	t.Cleanup(func() { _ = unrelatedReader.Close() })
	data = make([]byte, originalMediaReadChunk)
	if n, err := unrelatedReader.Read(data); err != nil || n != len(data) || !bytes.Equal(data, primaryScanReadSourceBytes) {
		t.Fatalf("unrelated foreground source did not read its actual 32 KiB: bytes=%d error=%v", n, err)
	}
	if err := unrelatedReader.Close(); err != nil {
		t.Fatal(err)
	}
	firstGate.openGate()
	if got := primaryScanReadReceive(t, first.ctx, firstResult); got.err != nil {
		t.Fatal(got.err)
	}
	if got := primaryScanReadReceive(t, first.ctx, queued); got.err != nil || got.info.Size != originalMediaReadChunk {
		t.Fatalf("unrelated domain did not resume on the shared released BG phase: %+v", got)
	}
	secondGate.openGate()
	if got := primaryScanReadReceive(t, first.ctx, secondResult); got.err != nil {
		t.Fatal(got.err)
	}
}

func TestPrimaryScanReadWarmNoopAndJoinedOverride(t *testing.T) {
	prober := &primaryScanReadTestProber{joined: true, Prober: primaryScanReadTool(t, "")}
	f := primaryScanReadFixtureAt(t, prober, "")
	if err := f.state.startThemeScan(); err != nil {
		t.Fatal(err)
	}
	if err := f.state.startExtraScan(); err != nil {
		t.Fatal(err)
	}
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	if err := f.state.scanFile("Feature.mp4", "video", hierarchy{parentID: f.state.library.ID}); err != nil {
		t.Fatal(err)
	}
	if prober.calls.Load() != 1 {
		t.Fatalf("custom joined ProbeFile override was bypassed by promoted Owned method: %d", prober.calls.Load())
	}
	var beforeVersion string
	if err := f.pool.QueryRow(f.ctx, "SELECT xmin::text FROM items WHERE root_id=$1 AND relative_path='Feature.mp4'", f.state.root.id).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	if err := f.state.scanFile("Feature.mp4", "video", hierarchy{parentID: f.state.library.ID}); err != nil {
		t.Fatal(err)
	}
	var afterVersion string
	if err := f.pool.QueryRow(f.ctx, "SELECT xmin::text FROM items WHERE root_id=$1 AND relative_path='Feature.mp4'", f.state.root.id).Scan(&afterVersion); err != nil {
		t.Fatal(err)
	}
	if prober.calls.Load() != 1 || beforeVersion != afterVersion {
		t.Fatalf("warm visit probed or rewrote accepted media: calls=%d xmin=%s -> %s", prober.calls.Load(), beforeVersion, afterVersion)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
		t.Fatalf("warm/no-op dispatch retained an I/O phase: before=%+v after=%+v", beforeIO, stats)
	}
	if stats := originalMediaReadOwners.Stats(); stats.RegisteredOwners != beforeOwners {
		t.Fatalf("warm/no-op dispatch retained a descriptor owner: %+v", stats)
	}
	refused := &primaryScanReadTestProber{joined: false, Prober: primaryScanReadTool(t, "")}
	f.store.prober = refused
	f.prepare(t)
	got := primaryScanReadReceive(t, f.ctx, primaryScanReadStartProbe(t, f, refused))
	if !errors.Is(got.err, ErrUnavailable) || refused.calls.Load() != 0 || got.info.Size != 0 {
		t.Fatalf("false joined marker fell through to promoted Owned probe: %+v calls=%d", got, refused.calls.Load())
	}
	if err := primaryScanReadClose(t, f.input, f.file.Close); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryScanReadCloseFaultRetiresFDAndRemainsSticky(t *testing.T) {
	prober := &primaryScanReadTestProber{joined: true}
	f := primaryScanReadFixtureAt(t, prober, "")
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	f.prepare(t)
	got := primaryScanReadReceive(t, f.ctx, primaryScanReadStartProbe(t, f, prober))
	if got.err != nil || got.info.Size != originalMediaReadChunk {
		t.Fatalf("actual source probe failed before close fault: %+v", got)
	}
	failure := errors.New("actual scan input close reported a fault after descriptor retirement")
	f.allowedCloseError = failure
	closeErr := primaryScanReadClose(t, f.input, func() error { return errors.Join(f.file.Close(), failure) })
	if !errors.Is(closeErr, failure) {
		t.Fatalf("actual close fault was suppressed: %v", closeErr)
	}
	for _, probeErr := range []error{closeErr, errors.Join(errScanProbeSourceChanged, closeErr)} {
		warnings := f.state.warnings
		accepted, acceptanceErr := f.state.acceptScannedMedia(&scannedMediaInput{}, "video", probeErr)
		if accepted || !errors.Is(acceptanceErr, failure) || f.state.warnings != warnings {
			t.Fatalf("actual close fault was reduced to an accepted or recoverable warning pass: accepted=%v warnings=%d -> %d error=%v", accepted, warnings, f.state.warnings, acceptanceErr)
		}
	}
	if again := primaryScanReadClose(t, f.input, f.file.Close); !errors.Is(again, failure) {
		t.Fatalf("retry hid the sticky scan close fault: %v", again)
	}
	if _, err := f.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("close fault did not retire the actual input FD: %v", err)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
		t.Fatalf("retired close fault retained phase charges: before=%+v after=%+v", beforeIO, stats)
	}
	if stats := originalMediaReadOwners.Stats(); stats.RegisteredOwners != beforeOwners {
		t.Fatalf("retired close fault retained descriptor registration: %+v", stats)
	}
}

type primaryAuxiliaryBatchProber struct {
	fixture       libraryFixtureProber
	mu            sync.Mutex
	previous      *os.File
	auxiliary     int
	maximumOwners int
	onAuxiliary   func(context.Context, *os.File, int) error
}

func (*primaryAuxiliaryBatchProber) ProbeFileJoinedContract() bool { return true }

func (prober *primaryAuxiliaryBatchProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	name := filepath.ToSlash(file.Name())
	if strings.Contains(name, "/theme-music/") || strings.Contains(name, "/trailers/") {
		prober.mu.Lock()
		if prober.previous != nil {
			if _, err := prober.previous.Stat(); !errors.Is(err, os.ErrClosed) {
				prober.mu.Unlock()
				return media.Info{}, errors.New("the previous auxiliary descriptor was retained across probe batches")
			}
		}
		prober.previous = file
		prober.auxiliary++
		index := prober.auxiliary
		owners := originalMediaReadOwners.Stats().RegisteredOwners
		if owners > prober.maximumOwners {
			prober.maximumOwners = owners
		}
		callback := prober.onAuxiliary
		prober.mu.Unlock()
		if callback != nil {
			if err := callback(ctx, file, index); err != nil {
				return media.Info{}, err
			}
		}
	}
	return prober.fixture.ProbeFile(ctx, file)
}

func primaryAuxiliaryBatchScan(t *testing.T, ctx context.Context, store *Store, libraryID string, force bool, status string) Job {
	t.Helper()
	job, err := store.StartScanWithOptions(ctx, libraryID, ScanOptions{ForceProbe: force})
	if err != nil {
		t.Fatal(err)
	}
	return libraryIntegrationWaitJobWithTimeout(t, ctx, store, job.ID, status, 90*time.Second)
}

func TestPrimaryScanReadAuxiliaryCompletePopulation(t *testing.T) {
	for _, test := range []struct{ name, directory, extension, header, relation string }{
		{"theme", "theme-music", "mp3", "audio:", "item_theme_resources"},
		{"extra", "trailers", "mp4", "video:", "item_extra_resources"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := &primaryAuxiliaryBatchProber{}
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			libraryIntegrationFile(t, root, "Film/Feature.mp4", "video:primary-auxiliary-owner")
			for index := 0; index < 256; index++ {
				libraryIntegrationFile(t, root, fmt.Sprintf("Film/%s/resource-%03d.%s", test.directory, index, test.extension), test.header+"complete-resource")
			}
			library := libraryIntegrationCreate(t, ctx, store, "Complete auxiliary read batches", "movies", root)
			beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
			job := primaryAuxiliaryBatchScan(t, ctx, store, library.ID, false, "Completed")
			if job.Error != "" {
				t.Fatalf("a supported complete 256-resource population failed: %s", job.Error)
			}
			var active int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+test.relation+" r JOIN items i ON i.id=r.resource_item_id WHERE r.active AND i.library_id=$1", library.ID).Scan(&active); err != nil || active != 256 {
				t.Fatalf("the complete owner was not published atomically: active=%d error=%v", active, err)
			}
			prober.mu.Lock()
			calls, peak, previous := prober.auxiliary, prober.maximumOwners, prober.previous
			prober.mu.Unlock()
			if calls != 256 || peak > beforeOwners+3 {
				t.Fatalf("auxiliary reads accumulated retained owners: probes=%d peak=%d baseline=%d", calls, peak, beforeOwners)
			}
			if _, err := previous.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("the last actual auxiliary FD survived publication: %v", err)
			}
			if after := originalMediaReadOwners.Stats().RegisteredOwners; after != beforeOwners {
				t.Fatalf("the complete population retained owners: before=%d after=%d", beforeOwners, after)
			}
			if after := originalMediaReadGovernor.Stats(); after != beforeIO {
				t.Fatalf("the complete population retained IO charges: before=%+v after=%+v", beforeIO, after)
			}
			warm := primaryAuxiliaryBatchScan(t, ctx, store, library.ID, false, "Completed")
			prober.mu.Lock()
			warmCalls := prober.auxiliary
			prober.mu.Unlock()
			if warm.Error != "" || warmCalls != calls {
				t.Fatalf("the warm population probed or failed: calls=%d->%d error=%s", calls, warmCalls, warm.Error)
			}
		})
	}
}

func TestPrimaryScanReadAuxiliaryLargeBatchRetainsAtomicPopulation(t *testing.T) {
	for _, scenario := range []string{"probe_failure", "source_change", "task_cancel"} {
		t.Run(scenario, func(t *testing.T) {
			prober := &primaryAuxiliaryBatchProber{}
			ctx, pool, store, root, _ := libraryIntegrationStore(t, prober)
			libraryIntegrationFile(t, root, "Film/Feature.mp4", "video:primary-auxiliary-owner")
			first := ""
			for index := 0; index < 65; index++ {
				path := libraryIntegrationFile(t, root, fmt.Sprintf("Film/theme-music/resource-%03d.mp3", index), "audio:retained-resource")
				if index == 0 {
					first = path
				}
			}
			library := libraryIntegrationCreate(t, ctx, store, "Atomic auxiliary failure", "movies", root)
			if job := primaryAuxiliaryBatchScan(t, ctx, store, library.ID, false, "Completed"); job.Error != "" {
				t.Fatal(job.Error)
			}
			before := themeScanTestSnapshot(t, ctx, pool, library.ID)
			beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
			prober.mu.Lock()
			prober.auxiliary, prober.previous = 0, nil
			prober.onAuxiliary = func(work context.Context, file *os.File, index int) error {
				if index != 65 {
					return nil
				}
				switch scenario {
				case "probe_failure":
					return errors.New("the last actual auxiliary probe failed")
				case "source_change":
					return os.WriteFile(first, []byte("audio:changed-after-first-batch"), 0600)
				case "task_cancel":
					store.mu.Lock()
					var task *scanTask
					for _, active := range store.active {
						if active.job.LibraryID == library.ID {
							task = active
							break
						}
					}
					store.mu.Unlock()
					if task == nil {
						return errors.New("the actual auxiliary scan task was not active")
					}
					return store.CancelJob(ctx, task.job.ID)
				}
				return nil
			}
			prober.mu.Unlock()
			status := "Completed"
			if scenario == "task_cancel" {
				status = "Cancelled"
			}
			job := primaryAuxiliaryBatchScan(t, ctx, store, library.ID, true, status)
			if scenario != "task_cancel" && job.Error == "" {
				t.Fatal("the incomplete auxiliary proof was accepted as a complete owner")
			}
			if after := themeScanTestSnapshot(t, ctx, pool, library.ID); after != before {
				t.Fatal("a late batch failure published a partial or changed auxiliary population")
			}
			if after := originalMediaReadOwners.Stats().RegisteredOwners; after != beforeOwners {
				t.Fatalf("a rejected auxiliary batch retained owners: before=%d after=%d", beforeOwners, after)
			}
			if after := originalMediaReadGovernor.Stats(); after != beforeIO {
				t.Fatalf("a rejected auxiliary batch retained actual IO charges: before=%+v after=%+v", beforeIO, after)
			}
		})
	}
}
