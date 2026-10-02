package library

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/subtitle"
)

var providerSubtitleWorkers = make(chan struct{}, 4)

// An uncertain retirement keeps its exact descriptors strongly reachable. The
// shared retained-owner limit bounds this quarantine without adding workers.
var providerSubtitleRetainedResources = struct {
	sync.Mutex
	prepared map[*preparedProviderSubtitle]struct{}
}{prepared: make(map[*preparedProviderSubtitle]struct{})}

type providerSubtitlePhaseContext struct{ context context.Context }
type providerSubtitleRetirementFailure struct{ err error }
type providerSubtitleAdmissionBusy struct{ err error }
type providerSubtitleAttemptBusy struct{ err error }

func (failure *providerSubtitleAdmissionBusy) Error() string { return failure.err.Error() }
func (failure *providerSubtitleAdmissionBusy) Unwrap() error { return failure.err }
func (failure *providerSubtitleAttemptBusy) Error() string   { return failure.err.Error() }
func (failure *providerSubtitleAttemptBusy) Unwrap() error   { return failure.err }

// ErrSubtitleTargetNotWritable distinguishes an unusable sidecar destination
// before a caller spends a provider's download quota.
var ErrSubtitleTargetNotWritable = errors.New("subtitle destination is not writable")

type providerSubtitleSnapshot struct {
	primary         indexedMediaSource
	mediaJSON       []byte
	bindingRevision int64
}

type preparedProviderSubtitle struct {
	observation         storageObservationLifetime
	primaryIO           *PrimaryRootIO
	ioClass             primaryio.Class
	cleanupPhase        atomic.Pointer[providerSubtitlePhaseContext]
	retirementFailure   atomic.Pointer[providerSubtitleRetirementFailure]
	retirementCompleted atomic.Bool
	retirementMu        sync.Mutex
	uncertainClosers    []io.Closer
	keep                atomic.Bool
	publicationFinished atomic.Bool
	created             bool
	entry               *scannedSubtitle
	snapshot            providerSubtitleSnapshot
	lease               *libraryRootLease
	root                *os.Root
	parent              *os.Root
	primary             *os.File
	primaryInfo         os.FileInfo
	stageName           string
	stageInfo           os.FileInfo
	candidate           subtitleCandidate
	relativePath        string
	digest              string
}

// CheckWritableSubtitleTarget performs an early, authorized write check and
// returns the exact source tag checked. An empty expected tag selects the
// current source; a nonempty tag must still match. A successful check does not
// reserve the directory or replace publication's source and authority checks.
func (s *Store) CheckWritableSubtitleTarget(ctx context.Context, actor identity.Principal, itemID, expectedSourceTag string) (string, error) {
	if (actor.Kind != "admin" && actor.Kind != "emby") || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
		!metadataIdentifier(actor.User.ID) || !metadataIdentifier(actor.SessionID) {
		return "", ErrForbidden
	}
	return s.checkWritableSubtitleTarget(ctx, &actor, itemID, expectedSourceTag)
}

// A nil actor is reserved for the store's internal scheduled provider work.
func (s *Store) checkWritableSubtitleTarget(ctx context.Context, actor *identity.Principal, itemID, expectedSourceTag string) (string, error) {
	if ctx == nil || !metadataIdentifier(itemID) || len(expectedSourceTag) > 256 {
		return "", ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return "", ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := runSubtitleWorker(ctx, providerSubtitleWorkers, func() (result SubtitleContent, err error) {
		snapshot, err := s.readProviderSubtitleSnapshot(ctx, actor, itemID)
		if err != nil {
			return SubtitleContent{}, err
		}
		tag := mediaSnapshotTag(snapshot.primary)
		if expectedSourceTag != "" && tag != expectedSourceTag {
			return SubtitleContent{}, ErrSourceChanged
		}
		prepared, err := s.openProviderSubtitleTarget(ctx, actor, snapshot, providerSubtitleIOClass(actor))
		if err != nil {
			return SubtitleContent{}, err
		}
		defer func() { err = errors.Join(err, prepared.closeTarget()) }()
		if err := prepared.primaryIO.Run(ctx, snapshot.primary.root.id, prepared.ioClass, func(work context.Context) error {
			prepared.rememberPhase(work)
			if err := s.checkProviderSubtitleSnapshot(work, actor, snapshot); err != nil {
				return err
			}
			if err := prepared.verify(work, nil); err != nil {
				return err
			}
			if err := probeWritableSubtitleParentOwned(work, prepared.parent, prepared); err != nil {
				return err
			}
			return prepared.verify(work, nil)
		}); err != nil {
			return SubtitleContent{}, err
		}
		current, err := s.readProviderSubtitleSnapshot(ctx, actor, itemID)
		if err != nil {
			return SubtitleContent{}, err
		}
		if !snapshot.same(current) {
			return SubtitleContent{}, ErrSourceChanged
		}
		return SubtitleContent{Info: Subtitle{Tag: tag}}, nil
	})
	if err != nil {
		return "", err
	}
	return result.Info.Tag, nil
}

// A real exclusive create checks mount flags and effective filesystem access,
// including ACLs. Cleanup ignores cancellation, checks the private name against
// the still-open inode, and contributes close or removal failures to the error.
func probeWritableSubtitleParent(ctx context.Context, parent *os.Root) (err error) {
	return probeWritableSubtitleParentOwned(ctx, parent, nil)
}

func probeWritableSubtitleParentOwned(ctx context.Context, parent *os.Root, prepared *preparedProviderSubtitle) (err error) {
	if ctx == nil || parent == nil {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("prepare subtitle preflight name: %w", err)
	}
	name := ".goby-provider-subtitle-check-" + hex.EncodeToString(nonce[:]) + ".tmp"
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w: %w: create subtitle preflight file: %w", ErrUnavailable, ErrSubtitleTargetNotWritable, err)
	}
	info, statErr := file.Stat()
	defer func() {
		// A transient first fstat failure must not prevent a second attempt to
		// identify our descriptor before removing the private name and closing.
		if info == nil {
			var retryErr error
			info, retryErr = file.Stat()
			err = errors.Join(err, retryErr)
		}
		removeErr := removeSubtitlePreflightFile(parent, name, info)
		var closeErr error
		if prepared == nil {
			closeErr = file.Close()
		} else {
			closeErr = prepared.closeTemporary(file)
		}
		err = errors.Join(err, removeErr, closeErr)
		if err != nil {
			err = fmt.Errorf("%w: %w: subtitle preflight did not complete: %w", ErrUnavailable, ErrSubtitleTargetNotWritable, err)
		}
	}()
	if statErr != nil {
		return statErr
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() != 0 {
		return ErrSourceChanged
	}
	return ctx.Err()
}

func removeSubtitlePreflightFile(parent *os.Root, name string, expected os.FileInfo) error {
	if expected == nil {
		return fmt.Errorf("subtitle preflight file identity is unavailable")
	}
	current, err := parent.Lstat(name)
	if err != nil {
		return fmt.Errorf("inspect subtitle preflight cleanup: %w", err)
	}
	if !current.Mode().IsRegular() || !os.SameFile(current, expected) {
		return ErrSourceChanged
	}
	if err := parent.Remove(name); err != nil {
		return fmt.Errorf("remove subtitle preflight file: %w", err)
	}
	return nil
}

// RegisterDownloadedSubtitle accepts catalog identity and validated provider
// output only. No caller-controlled filesystem path is accepted.
func (s *Store) RegisterDownloadedSubtitle(ctx context.Context, actor identity.Principal, itemID string, download providers.SubtitleDownload) error {
	if !validMetadataActor(actor) {
		return ErrForbidden
	}
	return s.registerDownloadedSubtitle(ctx, &actor, itemID, download)
}

// RegisterDownloadedSubtitleAsUser preserves ordinary login policy checks.
func (s *Store) RegisterDownloadedSubtitleAsUser(ctx context.Context, actor identity.Principal, itemID string, download providers.SubtitleDownload) error {
	if actor.Kind != "emby" || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" {
		return ErrForbidden
	}
	return s.registerDownloadedSubtitle(ctx, &actor, itemID, download)
}

// RegisterDownloadedSubtitleForSource binds a selected remote result to the
// exact media snapshot that was authorized when its selection token was issued.
func (s *Store) RegisterDownloadedSubtitleForSource(ctx context.Context, actor identity.Principal, itemID, expectedSourceTag string, download providers.SubtitleDownload) error {
	if expectedSourceTag == "" || len(expectedSourceTag) > 256 ||
		(actor.Kind != "admin" && actor.Kind != "emby") || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" {
		return ErrForbidden
	}
	return s.registerDownloadedSubtitleForSource(ctx, &actor, itemID, expectedSourceTag, download)
}

// A nil actor is reserved for the store's internal scheduled provider work.
func (s *Store) registerDownloadedSubtitle(ctx context.Context, actor *identity.Principal, itemID string, download providers.SubtitleDownload) error {
	return s.registerDownloadedSubtitleWithSource(ctx, actor, itemID, "", download)
}

func (s *Store) registerDownloadedSubtitleForSource(ctx context.Context, actor *identity.Principal, itemID, expectedSourceTag string, download providers.SubtitleDownload) error {
	if expectedSourceTag == "" || len(expectedSourceTag) > 256 {
		return ErrInvalidInput
	}
	return s.registerDownloadedSubtitleWithSource(ctx, actor, itemID, expectedSourceTag, download)
}

func (s *Store) registerDownloadedSubtitleWithSource(ctx context.Context, actor *identity.Principal, itemID, expectedSourceTag string, download providers.SubtitleDownload) error {
	if ctx == nil || !metadataIdentifier(itemID) || download.Provider != "opensubtitles" ||
		!metadataIdentifier(download.RemoteID) || len(download.Data) == 0 || len(download.Data) > subtitle.MaxInputBytes {
		return ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return ErrUnavailable
	}
	if actor != nil && (actor.Kind != "admin" && actor.Kind != "emby" || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
		!metadataIdentifier(actor.User.ID) || !metadataIdentifier(actor.SessionID)) {
		return ErrForbidden
	}
	format, err := subtitle.NormalizeFormat(download.Format)
	if err != nil {
		return ErrInvalidInput
	}
	language := strings.ToLower(strings.TrimSpace(download.Language))
	if language == "" {
		language = "und"
	}
	if len(language) > 64 || !subtitleLanguagePattern.MatchString(language) {
		return ErrInvalidInput
	}
	_, err = runSubtitleWorker(ctx, providerSubtitleWorkers, func() (SubtitleContent, error) {
		data := bytes.Clone(download.Data)
		if _, err := subtitle.Parse(data, format); err != nil {
			return SubtitleContent{}, fmt.Errorf("%w: invalid downloaded subtitle", ErrInvalidInput)
		}
		snapshot, err := s.readProviderSubtitleSnapshot(ctx, actor, itemID)
		if err != nil {
			return SubtitleContent{}, err
		}
		if expectedSourceTag != "" && mediaSnapshotTag(snapshot.primary) != expectedSourceTag {
			return SubtitleContent{}, fmt.Errorf("%w: %w: subtitle selection belongs to a changed media source", ErrUnavailable, ErrSourceChanged)
		}
		return SubtitleContent{}, s.registerProviderSubtitleSnapshot(ctx, actor, snapshot, download, language, string(format), data, s.publishProviderSubtitle)
	})
	return err
}

// Retry only complete, retired attempts. Every restart stays bound to the
// initial authorized source, including requests without an expected source tag.
func (s *Store) registerProviderSubtitleSnapshot(ctx context.Context, actor *identity.Principal, snapshot providerSubtitleSnapshot,
	download providers.SubtitleDownload, language, codec string, data []byte,
	publish func(context.Context, *identity.Principal, *preparedProviderSubtitle, providers.SubtitleDownload) error) error {
	class := providerSubtitleIOClass(actor)
	hint := mediaSourceRootHint{root: snapshot.primary.root, bindingRevision: snapshot.bindingRevision}
	for {
		prepared, err := s.prepareProviderSubtitle(ctx, actor, snapshot, class, download.RemoteID, language, codec, download.IsForced, download.HearingImpaired, data)
		if err == nil {
			err = publish(ctx, actor, prepared, download)
			closeErr := prepared.close()
			if closeErr != nil {
				return errors.Join(err, closeErr)
			}
			if _, retryable := err.(*providerSubtitleAttemptBusy); !retryable {
				return err
			}
			// Pure admission Busy launches no unfinished observation. An
			// unexpected retained operation is a hard retirement failure.
			if failure := prepared.retirementFailure.Load(); failure != nil {
				return errors.Join(err, failure.err)
			}
			if !prepared.retirementCompleted.Load() {
				return errors.Join(err, errSidecarRetirementUnknown)
			}
		} else if !sidecarAdmissionRetryable(err) {
			return err
		}
		if err := s.waitSidecarAdmission(ctx, hint, class, func(work context.Context) error {
			return s.checkProviderSubtitleSnapshot(work, actor, snapshot)
		}); err != nil {
			return err
		}
	}
}

func (s *Store) readProviderSubtitleSnapshot(ctx context.Context, actor *identity.Principal, itemID string) (providerSubtitleSnapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return providerSubtitleSnapshot{}, fmt.Errorf("read subtitle target: %w", err)
	}
	defer rollback(tx)
	if err := checkSubtitleProviderActor(ctx, tx, actor, itemID, false); err != nil {
		return providerSubtitleSnapshot{}, err
	}
	snapshot, err := queryProviderSubtitleSnapshot(ctx, tx, itemID, false)
	if err != nil {
		return providerSubtitleSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return providerSubtitleSnapshot{}, fmt.Errorf("complete subtitle target read: %w", err)
	}
	return snapshot, nil
}

func queryProviderSubtitleSnapshot(ctx context.Context, tx pgx.Tx, itemID string, lock bool) (providerSubtitleSnapshot, error) {
	query := `SELECT i.id, i.library_id, COALESCE(i.parent_id, ''), i.path, i.type,
		i.relative_path, i.file_identity, i.file_size, i.modified_at,
		CASE WHEN octet_length(i.media::text) <= 1048576 THEN i.media END,
		r.id, r.library_id, r.path, r.allowed_path, r.relative_path, r.binding_revision
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE i.id = $1 AND NOT i.is_folder AND i.type IN ('Movie', 'Episode', 'Video')
		AND i.media IS NOT NULL AND ` + ordinaryItemSQL("i")
	if lock {
		query += " FOR UPDATE OF i FOR SHARE OF r"
	}
	var result providerSubtitleSnapshot
	primary := &result.primary
	item := &primary.mediaFile.Item
	var modified *time.Time
	err := tx.QueryRow(ctx, query, itemID).Scan(&item.ID, &item.LibraryID, &item.ParentID, &item.Path, &item.Type,
		&primary.relativePath, &primary.identity, &primary.mediaFile.Size, &modified, &result.mediaJSON,
		&primary.root.id, &primary.root.libraryID, &primary.root.path, &primary.root.allowedPath,
		&primary.root.relativePath, &result.bindingRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return providerSubtitleSnapshot{}, ErrNotFound
	}
	if err != nil {
		return providerSubtitleSnapshot{}, fmt.Errorf("read subtitle primary snapshot: %w", err)
	}
	var probe media.Info
	if json.Unmarshal(result.mediaJSON, &probe) != nil || probe.ProbeVersion < media.CurrentProbeVersion ||
		probe.FileChangeTimeNs <= 0 || len(probe.Streams) == 0 || modified == nil || modified.IsZero() ||
		primary.identity == "" || primary.mediaFile.Size <= 0 || result.bindingRevision < 1 {
		return providerSubtitleSnapshot{}, fmt.Errorf("%w: subtitle target requires a current media scan", ErrUnavailable)
	}
	video := false
	for _, stream := range probe.Streams {
		video = video || stream.CodecType == "video"
	}
	if !video {
		return providerSubtitleSnapshot{}, ErrInvalidInput
	}
	item.Media = &probe
	primary.mediaFile.ModifiedAt = modified.UTC()
	primary.mediaFile.SourceID = media.SourceID(item.ID)
	if err := validateMediaSource(*primary); err != nil {
		return providerSubtitleSnapshot{}, err
	}
	return result, nil
}

func (snapshot providerSubtitleSnapshot) same(other providerSubtitleSnapshot) bool {
	first, second := snapshot.primary, other.primary
	return snapshot.bindingRevision == other.bindingRevision && first.root == second.root &&
		first.relativePath == second.relativePath && first.identity == second.identity &&
		first.mediaFile.Item.Path == second.mediaFile.Item.Path && first.mediaFile.Item.Type == second.mediaFile.Item.Type &&
		first.mediaFile.Item.ParentID == second.mediaFile.Item.ParentID && first.mediaFile.Size == second.mediaFile.Size &&
		first.mediaFile.ModifiedAt.Equal(second.mediaFile.ModifiedAt) && bytes.Equal(snapshot.mediaJSON, other.mediaJSON)
}

func providerSubtitleIOClass(actor *identity.Principal) primaryio.Class {
	if actor == nil {
		return primaryio.Background
	}
	return primaryio.Foreground
}

// A queue grants capacity only. Repeat the committed principal, source and
// binding checks after admission before touching the originally selected root.
func (s *Store) checkProviderSubtitleSnapshot(ctx context.Context, actor *identity.Principal, expected providerSubtitleSnapshot) error {
	current, err := s.readProviderSubtitleSnapshot(ctx, actor, expected.primary.mediaFile.Item.ID)
	if err != nil {
		return err
	}
	if !expected.same(current) {
		return fmt.Errorf("%w: %w: subtitle target changed while queued", ErrUnavailable, ErrSourceChanged)
	}
	return ctx.Err()
}

func (s *Store) openProviderSubtitleTarget(ctx context.Context, actor *identity.Principal, snapshot providerSubtitleSnapshot, class primaryio.Class) (prepared *preparedProviderSubtitle, err error) {
	operation, err := s.preparePrimaryRootIO(ctx,
		[]mediaSourceRootHint{{root: snapshot.primary.root, bindingRevision: snapshot.bindingRevision}})
	if err != nil {
		return nil, err
	}
	prepared = &preparedProviderSubtitle{snapshot: snapshot, primaryIO: operation, ioClass: class}
	defer func() {
		if err != nil {
			err = errors.Join(err, prepared.closeTarget())
			prepared = nil
		}
	}()
	err = operation.Run(ctx, snapshot.primary.root.id, class, func(work context.Context) error {
		prepared.rememberPhase(work)
		if err := s.checkProviderSubtitleSnapshot(work, actor, snapshot); err != nil {
			return err
		}
		return s.openProviderSubtitleTargetObserved(work, prepared)
	})
	return prepared, err
}

func (s *Store) openProviderSubtitleTargetObserved(ctx context.Context, prepared *preparedProviderSubtitle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot := prepared.snapshot
	var err error
	prepared.lease, err = s.leaseLibraryRoot(snapshot.primary.root)
	if err != nil {
		return err
	}
	prepared.root, err = prepared.lease.Open()
	if err != nil {
		return err
	}
	path := filepath.FromSlash(snapshot.primary.relativePath)
	prepared.parent, err = openRegisteredRoot(prepared.root, filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("%w: subtitle parent cannot be opened", ErrUnavailable)
	}
	before, err := prepared.parent.Lstat(filepath.Base(path))
	if err != nil || !snapshot.primary.matches(before) {
		return fmt.Errorf("%w: subtitle primary changed", ErrUnavailable)
	}
	prepared.primary, err = openScanFile(prepared.parent, filepath.Base(path))
	if err != nil {
		return fmt.Errorf("%w: subtitle primary cannot be opened safely", ErrUnavailable)
	}
	prepared.primaryInfo, err = prepared.primary.Stat()
	if err != nil || !snapshot.primary.matches(prepared.primaryInfo) || !sameMediaSourceFile(before, prepared.primaryInfo) {
		return fmt.Errorf("%w: subtitle primary changed while opening", ErrUnavailable)
	}
	return ctx.Err()
}

func (prepared *preparedProviderSubtitle) closeTarget() error {
	return prepared.retireResources(prepared.closeTargetResources, prepared.closeTargetResources)
}

func (prepared *preparedProviderSubtitle) closeTargetResources() error {
	var err error
	if prepared.primary != nil {
		closeErr := prepared.primary.Close()
		if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
			prepared.primary = nil
		} else {
			err = errors.Join(err, closeErr)
		}
	}
	if prepared.parent != nil {
		closeErr := prepared.parent.Close()
		if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
			prepared.parent = nil
		} else {
			err = errors.Join(err, closeErr)
		}
	}
	if prepared.root != nil {
		closeErr := prepared.root.Close()
		if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
			prepared.root = nil
		} else {
			err = errors.Join(err, closeErr)
		}
	}
	if prepared.lease != nil {
		closeErr := prepared.lease.Close()
		if closeErr == nil {
			prepared.lease = nil
		} else {
			err = errors.Join(err, closeErr)
		}
	}
	return err
}

func (prepared *preparedProviderSubtitle) rememberPhase(ctx context.Context) {
	prepared.cleanupPhase.Store(&providerSubtitlePhaseContext{context: ctx})
}

func (prepared *preparedProviderSubtitle) retainUncertainRetirement(err error) {
	err = errors.Join(errSidecarRetirementUnknown, err)
	prepared.retirementFailure.CompareAndSwap(nil, &providerSubtitleRetirementFailure{err: err})
	if prepared.primaryIO != nil {
		_ = prepared.primaryIO.MarkUnknown(err)
	}
	providerSubtitleRetainedResources.Lock()
	providerSubtitleRetainedResources.prepared[prepared] = struct{}{}
	providerSubtitleRetainedResources.Unlock()
}

// A later ErrClosed cannot prove that an earlier failed close retired the exact
// OS descriptor. Keep that closer and all of its surrounding root resources.
func (prepared *preparedProviderSubtitle) closeTemporary(closer io.Closer) error {
	err := closer.Close()
	if err != nil && !errors.Is(err, os.ErrClosed) {
		prepared.retirementMu.Lock()
		prepared.uncertainClosers = append(prepared.uncertainClosers, closer)
		prepared.retirementMu.Unlock()
		prepared.retainUncertainRetirement(err)
	}
	return err
}

// Retirement reuses an observation worker's still-held phase, or queues only
// after database ownership has ended. Caller cancellation cannot skip cleanup.
func (prepared *preparedProviderSubtitle) retireResources(closeResources, closeIdleResources func() error) error {
	if failure := prepared.retirementFailure.Load(); failure != nil {
		return failure.err
	}
	operation := prepared.primaryIO
	if operation == nil {
		return closeResources()
	}
	if prepared.primary == nil && prepared.parent == nil && prepared.root == nil &&
		prepared.lease == nil && prepared.entry == nil {
		prepared.primaryIO = nil
		err := operation.Close()
		if err == nil {
			prepared.retirementCompleted.Store(true)
		}
		return err
	}
	called := false
	var closeErr error
	work := func(context.Context) error {
		called = true
		closeErr = closeResources()
		return closeErr
	}
	var err error
	if phase := prepared.cleanupPhase.Load(); phase != nil {
		err = operation.Run(context.WithoutCancel(phase.context), prepared.snapshot.primary.root.id, prepared.ioClass, work)
	}
	if !called {
		for {
			err = operation.Run(context.Background(), prepared.snapshot.primary.root.id, prepared.ioClass, work)
			if called || !errors.Is(err, ErrBusy) {
				break
			}
			// The observation can report zero lifetimes while its final phase
			// release is still completing. Join that short transition instead
			// of treating a known worker's retirement as an unknown owner.
			time.Sleep(time.Millisecond)
		}
	}
	if !called && errors.Is(err, context.Canceled) {
		// Shutdown can reject new admission after every actual observation
		// has joined. Closing idle descriptors performs no payload read; keep
		// registration until it completes and leave private names untouched.
		called = true
		closeErr = closeIdleResources()
		err = closeErr
	}
	if !called || closeErr != nil {
		prepared.retainUncertainRetirement(err)
		return prepared.retirementFailure.Load().err
	}
	prepared.primaryIO = nil
	completionErr := operation.Close()
	if completionErr == nil {
		prepared.retirementCompleted.Store(true)
	}
	return errors.Join(err, completionErr)
}

func (s *Store) prepareProviderSubtitle(ctx context.Context, actor *identity.Principal, snapshot providerSubtitleSnapshot, class primaryio.Class, remoteID, language, codec string, forced, hearingImpaired bool, data []byte) (_ *preparedProviderSubtitle, resultErr error) {
	prepared, err := s.openProviderSubtitleTarget(ctx, actor, snapshot, class)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			if closeErr := prepared.close(); closeErr != nil {
				resultErr = errors.Join(resultErr, errSidecarRetirementUnknown, closeErr)
			}
		}
	}()
	err = prepared.primaryIO.Run(ctx, snapshot.primary.root.id, class, func(work context.Context) error {
		prepared.rememberPhase(work)
		if err := s.checkProviderSubtitleSnapshot(work, actor, snapshot); err != nil {
			return err
		}
		return prepared.preparePayload(work, remoteID, language, codec, forced, hearingImpaired, data)
	})
	if err != nil {
		return nil, err
	}
	success = true
	return prepared, nil
}

func (prepared *preparedProviderSubtitle) preparePayload(ctx context.Context, remoteID, language, codec string, forced, hearingImpaired bool, data []byte) error {
	snapshot := prepared.snapshot
	path := filepath.FromSlash(snapshot.primary.relativePath)
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	digest := sha256.Sum256([]byte(remoteID))
	suffix := "." + language + ".opensubtitles-" + hex.EncodeToString(digest[:])
	if forced {
		suffix += ".forced"
	}
	if hearingImpaired {
		suffix += ".sdh"
	}
	name := stem + suffix + "." + codec
	if len(name) > 255 || !safeSubtitleFilename(name) {
		return fmt.Errorf("%w: downloaded subtitle filename is too long", ErrInvalidInput)
	}
	track, ok := subtitleNameMetadata(name, suffix, codec)
	if !ok {
		return ErrInvalidInput
	}
	prepared.candidate = subtitleCandidate{filename: name, info: track}
	prepared.relativePath = filepath.ToSlash(filepath.Join(filepath.Dir(path), name))
	contentDigest := sha256.Sum256(data)
	prepared.digest = hex.EncodeToString(contentDigest[:])
	if err := prepared.verify(ctx, nil); err != nil {
		return err
	}
	if err := prepared.checkFilenameOwner(ctx); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("prepare subtitle staging name: %w", err)
	}
	prepared.stageName = ".goby-provider-subtitle-" + hex.EncodeToString(nonce[:]) + ".tmp"
	staged, err := prepared.parent.OpenFile(prepared.stageName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		prepared.stageName = ""
		return fmt.Errorf("%w: subtitle staging file cannot be created", ErrUnavailable)
	}
	defer prepared.closeTemporary(staged)
	prepared.stageInfo, err = staged.Stat()
	if err != nil || !prepared.stageInfo.Mode().IsRegular() {
		return fmt.Errorf("%w: subtitle staging identity is unavailable", ErrUnavailable)
	}
	reader := subtitleContextReader{ctx: ctx, reader: bytes.NewReader(data)}
	if written, err := io.Copy(staged, reader); err != nil || written != int64(len(data)) {
		return fmt.Errorf("%w: subtitle staging write did not complete", ErrUnavailable)
	}
	if err := staged.Sync(); err != nil {
		return fmt.Errorf("%w: subtitle staging write could not be synchronized", ErrUnavailable)
	}
	if err := prepared.closeTemporary(staged); err != nil {
		return fmt.Errorf("%w: subtitle staging file could not be closed", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// A more specific media stem must not take ownership of this sidecar on the
// next ordinary scan. Bound the directory walk independently from the payload.
func (prepared *preparedProviderSubtitle) checkFilenameOwner(ctx context.Context) (resultErr error) {
	directory, err := openScanFile(prepared.parent, ".")
	if err != nil {
		return fmt.Errorf("%w: subtitle directory cannot be inspected", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, prepared.closeTemporary(directory)) }()
	primaryName := filepath.Base(filepath.FromSlash(prepared.snapshot.primary.relativePath))
	owner := strings.ToLower(strings.TrimSuffix(primaryName, filepath.Ext(primaryName)))
	filename := prepared.candidate.filename
	stem := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(256)
		for _, entry := range entries {
			count++
			if count > 65536 {
				return fmt.Errorf("%w: subtitle directory exceeds inspection limit", ErrUnavailable)
			}
			name := entry.Name()
			if !entry.Type().IsRegular() || ignoredName(name) || extensionKind(name) == "" {
				continue
			}
			candidate := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
			if candidate != owner && (stem == candidate || strings.HasPrefix(stem, candidate+".")) && len(candidate) > len(owner) {
				return fmt.Errorf("%w: subtitle filename belongs to another media item", ErrInvalidInput)
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("%w: subtitle directory cannot be inspected", ErrUnavailable)
		}
	}
}

func (prepared *preparedProviderSubtitle) verify(ctx context.Context, entry *scannedSubtitle) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentRoot, err := prepared.lease.Open()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, prepared.closeTemporary(currentRoot)) }()
	path := filepath.FromSlash(prepared.snapshot.primary.relativePath)
	currentParent, err := openRegisteredRoot(currentRoot, filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("%w: subtitle directory changed", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, prepared.closeTemporary(currentParent)) }()
	current, currentErr := currentParent.Lstat(filepath.Base(path))
	after, afterErr := prepared.primary.Stat()
	if !sameMediaSourceDirectory(prepared.root, currentRoot) || !sameMediaSourceDirectory(prepared.parent, currentParent) ||
		currentErr != nil || afterErr != nil || !prepared.snapshot.primary.matches(current) ||
		!prepared.snapshot.primary.matches(after) || !sameMediaSourceFile(prepared.primaryInfo, current) ||
		!sameMediaSourceFile(prepared.primaryInfo, after) {
		return fmt.Errorf("%w: subtitle target changed during publication", ErrUnavailable)
	}
	if entry != nil {
		if err := verifyScannedSubtitle(currentParent, entry); err != nil {
			return fmt.Errorf("%w: subtitle changed during publication", ErrUnavailable)
		}
	}
	return ctx.Err()
}

func (prepared *preparedProviderSubtitle) close() error {
	err := prepared.observation.retire(prepared.closeResources)
	if err != nil && !prepared.retirementCompleted.Load() && prepared.retirementFailure.Load() == nil {
		prepared.retainUncertainRetirement(err)
		return prepared.retirementFailure.Load().err
	}
	return err
}

func (prepared *preparedProviderSubtitle) closeResources() error {
	return prepared.retireResources(prepared.closePublicationResources, prepared.closeIdlePublicationResources)
}

func (prepared *preparedProviderSubtitle) closePublicationEntry() error {
	var err error
	if prepared.entry != nil {
		closeErr := prepared.entry.file.Close()
		if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
			prepared.entry = nil
		} else {
			err = errors.Join(err, closeErr)
		}
	}
	return err
}

func (prepared *preparedProviderSubtitle) closeIdlePublicationResources() error {
	return errors.Join(prepared.closePublicationEntry(), prepared.closeTargetResources())
}

func (prepared *preparedProviderSubtitle) closePublicationResources() error {
	err := prepared.closePublicationEntry()
	if prepared.created && !prepared.keep.Load() {
		prepared.removeCreated(prepared.candidate.filename, prepared.stageInfo)
	}
	if prepared.parent != nil {
		prepared.removeCreated(prepared.stageName, prepared.stageInfo)
	}
	return errors.Join(err, prepared.closeTargetResources())
}

func (prepared *preparedProviderSubtitle) removeCreated(name string, expected os.FileInfo) {
	if name == "" || expected == nil {
		return
	}
	current, err := prepared.parent.Lstat(name)
	if err == nil && current.Mode().IsRegular() && os.SameFile(current, expected) {
		_ = prepared.parent.Remove(name)
	}
}

// Admission while catalog ownership is held must never wait for a root lane.
// The observation retains the admitted phase through its actual worker and any
// deferred resource retirement, even when its caller stops waiting.
func (prepared *preparedProviderSubtitle) observeImmediate(ctx context.Context, work func(context.Context) error) error {
	if prepared.primaryIO == nil {
		return ErrUnavailable
	}
	started := false
	err := prepared.primaryIO.RunImmediate(ctx, prepared.snapshot.primary.root.id, prepared.ioClass, func(observed context.Context) error {
		started = true
		prepared.rememberPhase(observed)
		return runStorageObservation(observed, []*storageObservationLifetime{&prepared.observation}, work)
	})
	if !started && sidecarAdmissionRetryable(err) {
		return &providerSubtitleAdmissionBusy{err: err}
	}
	return err
}

func (s *Store) publishProviderSubtitle(ctx context.Context, actor *identity.Principal, prepared *preparedProviderSubtitle, download providers.SubtitleDownload) error {
	return s.publishProviderSubtitleBeforeFinal(ctx, actor, prepared, download, nil)
}

// The private hook supports memory-only contention fixtures. Production callers
// pass nil and preserve the ordinary atomic publication and payload proof.
func (s *Store) publishProviderSubtitleBeforeFinal(ctx context.Context, actor *identity.Principal, prepared *preparedProviderSubtitle,
	download providers.SubtitleDownload, beforeFinal func() error) (resultErr error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	admissionBusy := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rollbackErr := tx.Rollback(cleanup)
		cancel()
		if rollbackErr != nil && (admissionBusy || !errors.Is(rollbackErr, pgx.ErrTxClosed)) {
			admissionBusy = false
			resultErr = errors.Join(resultErr, rollbackErr)
		}
		if admissionBusy {
			resultErr = &providerSubtitleAttemptBusy{err: resultErr}
		}
	}()
	protected := tx.(*ownedTx).ctx
	// Remove a failed publication while catalog writers are still excluded. If
	// storage has not returned, retain the sidecar instead of deleting a file a
	// later scanner may already have adopted after this transaction rolls back.
	defer func() {
		if prepared.keep.Load() {
			return
		}
		if prepared.publicationFinished.Load() {
			cleanupErr := prepared.observeImmediate(protected, prepared.cleanupFinal)
			if cleanupErr != nil {
				if _, retryable := cleanupErr.(*providerSubtitleAdmissionBusy); !retryable {
					admissionBusy = false
				}
			}
			resultErr = errors.Join(resultErr, cleanupErr)
		}
		prepared.keep.Store(true)
	}()
	itemID := prepared.snapshot.primary.mediaFile.Item.ID
	if err := checkSubtitleProviderActor(protected, tx, actor, itemID, true); err != nil {
		return err
	}
	current, err := queryProviderSubtitleSnapshot(ctx, tx, itemID, true)
	if err != nil {
		return err
	}
	if !prepared.snapshot.same(current) {
		return fmt.Errorf("%w: subtitle catalog target changed", ErrUnavailable)
	}
	if err := checkSubtitleProviderActor(protected, tx, actor, itemID, false); err != nil {
		return err
	}
	// Link is an atomic no-replace publication in the already opened directory.
	// Slow payload writes and fsync have finished before the owned transaction.
	if err := prepared.observeImmediate(protected, prepared.publishFile); err != nil {
		_, admissionBusy = err.(*providerSubtitleAdmissionBusy)
		return fmt.Errorf("%w: subtitle publication did not complete: %w", ErrUnavailable, err)
	}
	entry := prepared.entry
	entry.source.relativePath = prepared.relativePath
	entry.source.rootID = current.primary.root.id
	if err := validateSubtitleSnapshot(current.primary, entry.source); err != nil {
		return err
	}
	embedded := highestEmbeddedStreamIndex(current.primary.mediaFile.Item.Media)
	before, err := readSubtitleCatalogProjection(ctx, tx, itemID, embedded)
	if err != nil {
		return err
	}
	total, highest, active, err := subtitleCatalogCapacity(ctx, tx, itemID)
	if err != nil {
		return fmt.Errorf("read subtitle catalog capacity: %w", err)
	}
	previous, previousErr := scanStoredSubtitle(tx.QueryRow(ctx, "SELECT "+subtitleColumns+`
		FROM item_subtitles s WHERE s.item_id = $1 AND s.relative_path = $2 AND s.active`, itemID, prepared.relativePath))
	if previousErr != nil && !errors.Is(previousErr, pgx.ErrNoRows) {
		return fmt.Errorf("read existing downloaded subtitle: %w", previousErr)
	}
	index := -1
	if previousErr == nil && previous.Index > embedded {
		index = previous.Index
	} else {
		if previousErr == nil {
			if _, err := tx.Exec(ctx, `UPDATE item_subtitles SET active = false WHERE item_id = $1 AND stream_index = $2`, itemID, previous.Index); err != nil {
				return err
			}
			active--
		}
		if embedded > highest {
			highest = embedded
		}
		if total >= maxSubtitleIdentities || active >= maxActiveSubtitles || highest >= maxSubtitleStreamIndex {
			return fmt.Errorf("%w: subtitle catalog capacity exceeded", ErrInvalidInput)
		}
		index = highest + 1
	}
	source := entry.source
	_, err = tx.Exec(ctx, `INSERT INTO item_subtitles
		(item_id, root_id, stream_index, active, relative_path, file_identity, source_hash,
		 file_size, modified_at, change_time_ns, codec, language, title,
		 is_default, is_forced, is_hearing_impaired, mime_type)
		VALUES ($1,$2,$3,true,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (item_id, stream_index) DO UPDATE SET root_id = EXCLUDED.root_id,
		 relative_path = EXCLUDED.relative_path, file_identity = EXCLUDED.file_identity,
		 source_hash = EXCLUDED.source_hash, file_size = EXCLUDED.file_size,
		 modified_at = EXCLUDED.modified_at, change_time_ns = EXCLUDED.change_time_ns,
		 codec = EXCLUDED.codec, language = EXCLUDED.language, title = EXCLUDED.title,
		 is_default = EXCLUDED.is_default, is_forced = EXCLUDED.is_forced,
		 is_hearing_impaired = EXCLUDED.is_hearing_impaired, mime_type = EXCLUDED.mime_type`,
		itemID, source.rootID, index, source.relativePath, source.identity, source.Tag,
		source.Size, source.ModifiedAt, source.changeTimeNs, source.Codec, source.Language, source.Title,
		source.IsDefault, source.IsForced, source.IsHearingImpaired, source.MIMEType)
	if err != nil {
		return fmt.Errorf("register downloaded subtitle: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO item_subtitle_provider_sources (item_id, stream_index, provider, provider_id)
		VALUES ($1,$2,$3,$4) ON CONFLICT (item_id, stream_index) DO NOTHING`, itemID, index, download.Provider, download.RemoteID); err != nil {
		return fmt.Errorf("record downloaded subtitle source: %w", err)
	}
	var recordedProvider, recordedID string
	if err := tx.QueryRow(ctx, `SELECT provider, provider_id FROM item_subtitle_provider_sources
		WHERE item_id = $1 AND stream_index = $2`, itemID, index).Scan(&recordedProvider, &recordedID); err != nil {
		return err
	}
	if recordedProvider != download.Provider || recordedID != download.RemoteID {
		return fmt.Errorf("%w: downloaded subtitle source conflicts with its catalog identity", ErrInvalidInput)
	}
	after, err := readSubtitleCatalogProjection(ctx, tx, itemID, embedded)
	if err != nil {
		return err
	}
	if before != after {
		item := current.primary.mediaFile.Item
		if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: item.ID, LibraryID: item.LibraryID, ParentID: item.ParentID}); err != nil {
			return err
		}
	}
	if beforeFinal != nil {
		if err := beforeFinal(); err != nil {
			return err
		}
	}
	if err := prepared.observeImmediate(protected, func(observed context.Context) error {
		if err := prepared.checkFilenameOwner(observed); err != nil {
			return err
		}
		return prepared.verify(observed, entry)
	}); err != nil {
		_, admissionBusy = err.(*providerSubtitleAdmissionBusy)
		return fmt.Errorf("%w: subtitle final observation did not complete: %w", ErrUnavailable, err)
	}
	if err := checkSubtitleProviderActor(protected, tx, actor, itemID, false); err != nil {
		return err
	}
	// A failed commit can have an unknown outcome. Retain the validated sidecar
	// once commit is attempted; an ordinary rescan can safely adopt an orphan.
	prepared.keep.Store(true)
	return tx.Commit(ctx)
}

func (prepared *preparedProviderSubtitle) publishFile(ctx context.Context) error {
	defer prepared.publicationFinished.Store(true)
	if err := prepared.verify(ctx, nil); err != nil {
		return err
	}
	staged, err := prepared.parent.Lstat(prepared.stageName)
	if err != nil || !staged.Mode().IsRegular() || !os.SameFile(prepared.stageInfo, staged) {
		return fmt.Errorf("%w: subtitle staging identity changed", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepared.parent.Link(prepared.stageName, prepared.candidate.filename); err == nil {
		prepared.created = true
	} else if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%w: downloaded subtitle cannot be published without replacement", ErrUnavailable)
	}
	staged, err = prepared.parent.Lstat(prepared.stageName)
	if err != nil || !staged.Mode().IsRegular() || !os.SameFile(prepared.stageInfo, staged) {
		return fmt.Errorf("%w: subtitle staging identity changed during publication", ErrUnavailable)
	}
	// Removing the staging link before inspection stabilizes the inode ctime.
	if err := prepared.parent.Remove(prepared.stageName); err != nil {
		return fmt.Errorf("%w: subtitle staging link cannot be removed", ErrUnavailable)
	}
	prepared.stageName = ""
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := inspectLocalSubtitle(ctx, prepared.parent, prepared.candidate)
	if err != nil {
		return fmt.Errorf("%w: published subtitle cannot be inspected", ErrUnavailable)
	}
	prepared.entry = entry
	if entry.source.Tag != prepared.digest || (prepared.created && !os.SameFile(prepared.stageInfo, entry.info)) {
		return fmt.Errorf("%w: existing subtitle differs from the downloaded content", ErrInvalidInput)
	}
	return ctx.Err()
}

func (prepared *preparedProviderSubtitle) cleanupFinal(ctx context.Context) error {
	if !prepared.created || prepared.keep.Load() {
		return nil
	}
	if prepared.entry != nil {
		if err := prepared.closeTemporary(prepared.entry.file); err != nil && !errors.Is(err, os.ErrClosed) {
			return err
		}
	}
	current, err := prepared.parent.Lstat(prepared.candidate.filename)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(current, prepared.stageInfo) {
		return ErrSourceChanged
	}
	return prepared.parent.Remove(prepared.candidate.filename)
}
