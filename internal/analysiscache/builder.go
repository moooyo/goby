package analysiscache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"sort"
	"sync"
)

type Builder struct {
	mu                            sync.Mutex
	store                         *Store
	key, token, directory         string
	reservation, used, ownerBytes int64
	ctx                           context.Context
	cancel                        context.CancelFunc
	root                          *os.Root
	created, published, finished  bool
	artifacts                     map[string]Artifact
	abortOnce                     sync.Once
	done                          chan struct{}
	abortErr                      error
	temporaryMu                   sync.Mutex
	temporaries                   map[string]*temporaryState
	temporaryChanged              chan struct{}
	acceptTemporary               bool
}

type temporaryState struct {
	artifact Artifact
	refs     int
	deleting bool
}

// TemporaryLease is a source for streaming BIF.FrameSource.Open. It implements
// io.ReadCloser and may be opened inside WriteFile's producer callback.
type TemporaryLease struct {
	*os.File
	ownedFile  *os.File
	Artifact   Artifact
	builder    *Builder
	state      *temporaryState
	retirement *closeResult
}

func (lease *TemporaryLease) Close() error {
	if lease == nil {
		return nil
	}
	if lease.ownedFile == nil || lease.builder == nil || lease.state == nil || lease.retirement == nil {
		return ErrInvalidInput
	}
	lease.retirement.once.Do(func() {
		lease.retirement.err = lease.ownedFile.Close()
		if errors.Is(lease.retirement.err, os.ErrClosed) {
			lease.retirement.err = nil
		}
		b := lease.builder
		b.temporaryMu.Lock()
		lease.state.refs--
		b.signalTemporaryLocked()
		b.temporaryMu.Unlock()
	})
	return lease.retirement.err
}

func (b *Builder) initialize() error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	b.token = token
	b.directory = b.store.temporaryPrefix() + token
	if err := b.store.disk.check(); err != nil {
		return err
	}
	if err := b.store.disk.root.Mkdir(b.directory, 0700); err != nil {
		return err
	}
	b.created = true
	b.root, err = openChild(b.store.disk.root, b.directory)
	if err != nil {
		return err
	}
	owner := ownerRecord{Marker: "goby-analysis-entry-v1", Owner: b.store.disk.owner, Key: b.key, Token: token}
	encoded, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	if err := writeControl(b.root, ownerName, encoded); err != nil {
		return err
	}
	b.used = int64(len(encoded))
	b.ownerBytes = b.used
	if err := syncDirectory(b.root); err != nil {
		return err
	}
	return syncDirectory(b.store.disk.root)
}

func (b *Builder) ensure() error {
	if b.finished || b.published {
		return ErrClosed
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	return b.checkWorkspace()
}

func (b *Builder) checkWorkspace() error {
	if err := b.store.disk.check(); err != nil {
		return err
	}
	if b.root == nil {
		return ErrUnsafe
	}
	held, err := b.root.Stat(".")
	if err != nil {
		return err
	}
	current, err := b.store.disk.root.Lstat(b.directory)
	if err != nil || !os.SameFile(held, current) || current.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafe
	}
	return nil
}

// WriteFile retains no caller-owned output descriptor. The producer gets only
// a bounded writer; Sync and Close complete before this method returns.
func (b *Builder) WriteFile(ctx context.Context, name string, producer func(context.Context, io.Writer) error) (Artifact, error) {
	if b == nil || !allowedArtifact(name) || producer == nil {
		return Artifact{}, ErrInvalidInput
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensure(); err != nil {
		return Artifact{}, err
	}
	if _, exists := b.artifacts[name]; exists {
		return Artifact{}, ErrExists
	}
	limit := b.store.config.MaxFileBytes
	if name == "manifest.json" {
		limit = min(limit, maxSmallManifestBytes)
	}
	artifact, err := b.write(ctx, name, limit, producer)
	if err != nil {
		b.cancel()
		return Artifact{}, err
	}
	b.artifacts[name] = artifact
	return artifact, nil
}

// WriteTemporary streams one frame into the same reserved workspace. Temporary
// bytes cannot bypass the per-file, total-entry or temporary-file-count budgets.
func (b *Builder) WriteTemporary(ctx context.Context, name string, producer func(context.Context, io.Writer) error) (Artifact, error) {
	if b == nil || !temporaryPattern.MatchString(name) || producer == nil {
		return Artifact{}, ErrInvalidInput
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensure(); err != nil {
		return Artifact{}, err
	}
	b.temporaryMu.Lock()
	_, exists := b.temporaries[name]
	full := len(b.temporaries) >= b.store.config.MaxTemporaryFiles
	b.temporaryMu.Unlock()
	if exists {
		return Artifact{}, ErrExists
	}
	if full {
		return Artifact{}, ErrLimit
	}
	artifact, err := b.write(ctx, name, min(b.store.config.MaxFileBytes, 8<<20), producer)
	if err != nil {
		b.cancel()
		return Artifact{}, err
	}
	b.temporaryMu.Lock()
	b.temporaries[name] = &temporaryState{artifact: artifact}
	b.temporaryMu.Unlock()
	return artifact, nil
}

func (b *Builder) write(ctx context.Context, name string, limit int64, producer func(context.Context, io.Writer) error) (Artifact, error) {
	operationCtx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	file, err := openRegular(b.root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Artifact{}, err
	}
	writer := &artifactWriter{file: file, ctx: operationCtx, builder: b, limit: limit, digest: sha256.New()}
	// A panic still retires the descriptor. The builder reservation remains
	// owned until its deferred Abort/cancellation cleanup has actually ended.
	defer writer.retire()
	produceErr := producer(operationCtx, writer)
	if produceErr == nil {
		produceErr = operationCtx.Err()
	}
	artifact, closeErr := writer.finish(name, produceErr == nil)
	if err := errors.Join(produceErr, closeErr); err != nil {
		return Artifact{}, err
	}
	if err := b.ensure(); err != nil {
		return Artifact{}, err
	}
	return artifact, nil
}

type artifactWriter struct {
	mu          sync.Mutex
	file        *os.File
	ctx         context.Context
	builder     *Builder
	limit, size int64
	digest      hash.Hash
	closed      bool
	failure     error
}

func (writer *artifactWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return 0, ErrClosed
	}
	if writer.failure != nil {
		return 0, writer.failure
	}
	if err := writer.ctx.Err(); err != nil {
		writer.failure = err
		return 0, err
	}
	if int64(len(data)) > writer.limit-writer.size || int64(len(data)) > writer.builder.reservation-controlReserve-(writer.builder.used-writer.builder.ownerBytes) {
		writer.failure = ErrLimit
		return 0, ErrLimit
	}
	if err := writer.builder.store.disk.check(); err != nil {
		writer.failure = err
		return 0, err
	}
	n, err := writer.file.Write(data)
	if n < 0 || n > len(data) {
		writer.failure = ErrUnsafe
		return 0, ErrUnsafe
	}
	writer.size += int64(n)
	writer.builder.used += int64(n)
	_, _ = writer.digest.Write(data[:n])
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	writer.failure = err
	return n, err
}

func (writer *artifactWriter) finish(name string, sync bool) (Artifact, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return Artifact{}, ErrClosed
	}
	writer.closed = true
	err := writer.failure
	if sync && err == nil {
		err = writer.file.Sync()
	}
	info, statErr := writer.file.Stat()
	closeErr := writer.file.Close()
	if err = errors.Join(err, statErr, closeErr); err != nil {
		return Artifact{}, err
	}
	identity, err := fileIdentity(info)
	if err != nil {
		return Artifact{}, err
	}
	if info.Size() != writer.size {
		return Artifact{}, ErrUnsafe
	}
	return Artifact{Name: name, Size: writer.size, SHA256: hex.EncodeToString(writer.digest.Sum(nil)), Identity: identity}, nil
}

func (writer *artifactWriter) retire() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !writer.closed {
		writer.closed = true
		_ = writer.file.Close()
	}
}

func (b *Builder) signalTemporaryLocked() {
	close(b.temporaryChanged)
	b.temporaryChanged = make(chan struct{})
}

func (b *Builder) OpenTemporary(ctx context.Context, name string) (*TemporaryLease, error) {
	if b == nil || !temporaryPattern.MatchString(name) {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.temporaryMu.Lock()
	if !b.acceptTemporary || b.ctx.Err() != nil {
		b.temporaryMu.Unlock()
		return nil, ErrClosed
	}
	state := b.temporaries[name]
	if state == nil {
		b.temporaryMu.Unlock()
		return nil, ErrNotFound
	}
	if state.deleting {
		b.temporaryMu.Unlock()
		return nil, ErrBusy
	}
	state.refs++
	b.temporaryMu.Unlock()
	var file *os.File
	delivered := false
	defer func() {
		if !delivered {
			if file != nil {
				_ = file.Close()
			}
			b.temporaryMu.Lock()
			state.refs--
			b.signalTemporaryLocked()
			b.temporaryMu.Unlock()
		}
	}()
	if err := b.checkWorkspace(); err != nil {
		return nil, err
	}
	file, err := openRegular(b.root, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	count, err := copyWithContext(ctx, digest, io.LimitReader(file, state.artifact.Size+1))
	if err != nil {
		return nil, err
	}
	if count != state.artifact.Size || hex.EncodeToString(digest.Sum(nil)) != state.artifact.SHA256 {
		return nil, ErrUnsafe
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	identity, err := fileIdentity(info)
	if err != nil || identity != state.artifact.Identity {
		return nil, ErrUnsafe
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := b.checkWorkspace(); err != nil {
		return nil, err
	}
	delivered = true
	return &TemporaryLease{File: file, ownedFile: file, Artifact: state.artifact, builder: b, state: state, retirement: &closeResult{}}, nil
}

func (b *Builder) DeleteTemporary(ctx context.Context, name string) error {
	if b == nil || !temporaryPattern.MatchString(name) {
		return ErrInvalidInput
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensure(); err != nil {
		return err
	}
	return b.deleteTemporary(ctx, name)
}

func (b *Builder) deleteTemporary(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.temporaryMu.Lock()
	state := b.temporaries[name]
	if state == nil {
		b.temporaryMu.Unlock()
		return ErrNotFound
	}
	if state.refs != 0 || state.deleting {
		b.temporaryMu.Unlock()
		return ErrBusy
	}
	state.deleting = true
	b.temporaryMu.Unlock()
	file, err := openRegular(b.root, name, os.O_RDONLY, 0)
	if err == nil {
		info, statErr := file.Stat()
		closeErr := file.Close()
		err = errors.Join(statErr, closeErr)
		if err == nil {
			identity, identityErr := fileIdentity(info)
			err = identityErr
			if identity != state.artifact.Identity {
				err = ErrUnsafe
			}
		}
	}
	if err == nil {
		err = b.root.Remove(name)
	}
	if err == nil {
		err = syncDirectory(b.root)
	}
	b.temporaryMu.Lock()
	state.deleting = false
	if err == nil {
		delete(b.temporaries, name)
		b.used -= state.artifact.Size
	}
	b.signalTemporaryLocked()
	b.temporaryMu.Unlock()
	return err
}

// Publish deletes closed JPEG scratch files, seals metadata, and atomically
// renames the complete directory. An open temporary lease returns ErrBusy.
// A returned publication remains pinned until Keep or Discard, even if a final
// directory Sync reports an error after the atomic rename has already occurred.
func (b *Builder) Publish(ctx context.Context) (*Publication, error) {
	if b == nil {
		return nil, ErrInvalidInput
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensure(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(b.artifacts) == 0 {
		return nil, ErrInvalidInput
	}
	b.temporaryMu.Lock()
	for _, state := range b.temporaries {
		if state.refs != 0 || state.deleting {
			b.temporaryMu.Unlock()
			return nil, ErrBusy
		}
	}
	b.acceptTemporary = false
	temporaries := make([]string, 0, len(b.temporaries))
	for name := range b.temporaries {
		temporaries = append(temporaries, name)
	}
	b.temporaryMu.Unlock()
	for _, name := range temporaries {
		if err := b.deleteTemporary(ctx, name); err != nil {
			b.cancel()
			return nil, err
		}
	}
	artifacts := make([]Artifact, 0, len(b.artifacts))
	for _, artifact := range b.artifacts {
		artifacts = append(artifacts, artifact)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	encoded, err := json.Marshal(manifestRecord{Version: 1, Owner: b.store.disk.owner, Key: b.key, Artifacts: artifacts})
	if err != nil || int64(len(encoded)) > maxManifestBytes {
		b.cancel()
		if err == nil {
			err = ErrLimit
		}
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	seal := hex.EncodeToString(digest[:])
	if b.used+int64(len(encoded))+65 > b.reservation {
		b.cancel()
		return nil, ErrLimit
	}
	if err := writeControl(b.root, manifestName, encoded); err != nil {
		b.cancel()
		return nil, err
	}
	b.used += int64(len(encoded))
	if err := writeControl(b.root, sealName, []byte(seal+"\n")); err != nil {
		b.cancel()
		return nil, err
	}
	b.used += 65
	entry, err := loadEntry(b.root, b.store.disk.owner, b.key)
	if err != nil {
		b.cancel()
		return nil, err
	}
	if entry.Bytes != b.used || entry.Bytes > b.reservation {
		b.cancel()
		return nil, ErrUnsafe
	}
	if err := syncDirectory(b.root); err != nil {
		b.cancel()
		return nil, err
	}
	if err := b.ensure(); err != nil {
		b.cancel()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		b.cancel()
		return nil, err
	}
	if err := renameNoReplace(b.store.disk.root, b.directory, readyDirectory(b.key)); err != nil {
		b.cancel()
		return nil, err
	}
	b.published = true
	b.finished = true
	state := &entryState{entry: entry, directory: readyDirectory(b.key), pending: true}
	s := b.store
	s.mu.Lock()
	delete(s.builders, b.key)
	s.clock++
	state.lastUse = s.clock
	s.entries[b.key] = state
	s.signalLocked()
	s.mu.Unlock()
	closeErr := b.root.Close()
	b.root = nil
	syncErr := syncDirectory(s.disk.root)
	close(b.done)
	b.cancel()
	return &Publication{Entry: cloneEntry(entry), store: s, state: state}, errors.Join(closeErr, syncErr)
}

func (b *Builder) abortAsync() {
	b.abortOnce.Do(func() {
		go func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.published {
				return
			}
			b.finished = true
			b.cancel()
			b.temporaryMu.Lock()
			b.acceptTemporary = false
			for {
				refs := 0
				for _, state := range b.temporaries {
					refs += state.refs
				}
				if refs == 0 {
					break
				}
				changed := b.temporaryChanged
				b.temporaryMu.Unlock()
				<-changed
				b.temporaryMu.Lock()
			}
			b.temporaryMu.Unlock()
			if b.root != nil {
				b.abortErr = b.root.Close()
				b.root = nil
			}
			if b.created {
				b.abortErr = errors.Join(b.abortErr, b.store.removeOwnedDirectory(context.Background(), b.directory, b.key))
			}
			s := b.store
			s.mu.Lock()
			if b.abortErr == nil {
				delete(s.builders, b.key)
			}
			s.active--
			s.signalLocked()
			s.mu.Unlock()
			close(b.done)
		}()
	})
}

// Abort cancels admission immediately, but waits for real producer writes and
// temporary leases before closing descriptors and removing owned files. A
// deadline only stops this wait; it does not release the reservation early.
func (b *Builder) Abort(ctx context.Context) error {
	if b == nil {
		return nil
	}
	b.cancel()
	b.abortAsync()
	select {
	case <-b.done:
		return b.abortErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
