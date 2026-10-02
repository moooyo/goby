package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// Preparation is database-only and releases its budget before waiting for
// storage. A saturated storage domain must not prevent another domain from
// obtaining its routing hint. Both preparation and storage queues are bounded.
var mediaSourcePreparationAdmission = newMediaSourceAdmission()

// A routing failure must not replace the caller's original authorization error.
// Its database-only guard has a separate bounded queue so storage and initial
// preparation saturation cannot consume this failure-path capacity.
var mediaSourceFailureAdmission = newMediaSourceAdmission()

// This hint classifies server-owned catalog data. It grants no authority and
// must never be returned to a client or used instead of fresh authorization.
// The revision protects a queued mapping, but is deliberately not a lane key.
type mediaSourceRootHint struct {
	root            libraryRoot
	bindingRevision int64
}

type mediaSourceRootContextKey struct{}

func (s *Store) readMediaSourceRootHint(ctx context.Context, itemID string) (mediaSourceRootHint, error) {
	var hint mediaSourceRootHint
	if s == nil || s.pool == nil || itemID == "" || len(itemID) > 256 || strings.ContainsRune(itemID, '\x00') {
		return hint, ErrUnavailable
	}
	err := s.pool.QueryRow(ctx, `SELECT `+rootBindingMetadataColumns+`
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND NOT i.is_folder AND i.media IS NOT NULL
		AND i.type IN ('Movie','Episode','Video','Audio')`, itemID).
		Scan(&hint.root.id, &hint.root.libraryID, &hint.root.path, &hint.root.allowedPath, &hint.root.relativePath, &hint.bindingRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return mediaSourceRootHint{}, ErrNotFound
	}
	if err != nil {
		return mediaSourceRootHint{}, fmt.Errorf("%w: read source routing hint: %w", ErrUnavailable, err)
	}
	if (rootBindingRow{root: hint.root, revision: hint.bindingRevision}).validateMapping() != nil {
		return mediaSourceRootHint{}, ErrUnavailable
	}
	return hint, nil
}

// No topology or filesystem probe is used to classify work. Canonical
// configured anchors aggregate registrations and nested configured paths. This
// is configured-domain isolation, not proof that separate paths use independent
// physical storage. Explicit binding replacement retains the same logical lane.
func (s *Store) mediaSourceRootLane(hint mediaSourceRootHint) (mediaSourceRootKey, string, error) {
	if s == nil || s.pool == nil || s.ownership == nil || hint.root.id == "" || hint.bindingRevision <= 0 {
		return mediaSourceRootKey{}, "", ErrUnavailable
	}
	s.mediaSourceOwners.mu.Lock()
	catalog := s.mediaSourceOwners.catalogScope
	s.mediaSourceOwners.mu.Unlock()
	if catalog == "" {
		return mediaSourceRootKey{}, "", ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.closing.Load() {
		return mediaSourceRootKey{}, "", ErrUnavailable
	}
	configured := false
	domain := hint.root.allowedPath
	for _, approved := range s.roots {
		if approved.path == hint.root.allowedPath {
			configured = true
		}
		if pathWithin(approved.path, hint.root.allowedPath) && len(approved.path) < len(domain) {
			domain = approved.path
		}
	}
	if !configured || domain == "" || !filepath.IsAbs(domain) {
		return mediaSourceRootKey{}, "", ErrUnavailable
	}
	return mediaSourceRootKey{catalog: catalog, id: hint.root.id}, domain, nil
}

// Unlike the older fixture adapter, this lifetime covers database preparation,
// queue cancellation and actual filesystem owners. Registration precedes every
// stage under the same close fence, so shutdown cannot race WaitGroup.Add.
func (s *Store) beginMediaSourceLifetime(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil {
		return nil, nil, ErrUnavailable
	}
	runtime := &s.mediaSourceOwners
	runtime.mu.Lock()
	if runtime.closed || s.closing.Load() {
		runtime.mu.Unlock()
		return nil, nil, ErrUnavailable
	}
	if runtime.ctx == nil {
		runtime.ctx, runtime.cancel = context.WithCancel(context.Background())
	}
	if runtime.catalogScope == "" && s.pool != nil && s.ownership != nil {
		configuration := s.pool.Config().ConnConfig
		// The ownership key includes the actual database and schema captured
		// at startup. No credential or Store generation enters a lane key.
		runtime.catalogScope = configuration.Host + "\x00" + strconv.Itoa(int(configuration.Port)) + "\x00" + strconv.FormatInt(s.ownership.key, 10)
	}
	lifetime := runtime.ctx
	runtime.owners.Add(1)
	runtime.mu.Unlock()
	worker, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	var once sync.Once
	return worker, func() {
		once.Do(func() {
			stop()
			cancel()
			runtime.owners.Done()
		})
	}, nil
}

func (s *Store) prepareMediaSourceRoot(ctx context.Context, background bool, prepare func(context.Context) (mediaSourceRootHint, error)) (mediaSourceRootHint, error) {
	waiter, err := mediaSourcePreparationAdmission.requestRoot(ctx, background, true, mediaSourceRootKey{}, "", mediaSourceScopedQueueLimit)
	if err != nil {
		return mediaSourceRootHint{}, err
	}
	release, err := mediaSourcePreparationAdmission.wait(waiter)
	if err != nil {
		return mediaSourceRootHint{}, err
	}
	defer release()
	return prepare(ctx)
}

func mediaSourceAdmissionFailure(ctx context.Context, background bool, cause error, guards []func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(guards) == 0 || guards[0] == nil {
		return cause
	}
	waiter, err := mediaSourceFailureAdmission.requestRoot(ctx, background, true, mediaSourceRootKey{}, "", mediaSourceScopedQueueLimit)
	if err != nil {
		return err
	}
	release, err := mediaSourceFailureAdmission.wait(waiter)
	if err != nil {
		return err
	}
	defer release()
	if err := guards[0](ctx); err != nil {
		return err
	}
	return cause
}

// prepare is strictly database-only and returns a server-owned routing hint.
// work must perform its original fresh authorization and source read after
// admission. No transaction or global storage slot survives preparation or a
// root/domain wait. openMediaSource verifies the queued mapping before I/O.
func (s *Store) runPreparedMediaSourceWorker(ctx context.Context, background bool, prepare func(context.Context) (mediaSourceRootHint, error), work func(context.Context) (*os.File, MediaFile, error), failureGuards ...func(context.Context) error) (*os.File, MediaFile, error) {
	if prepare == nil || work == nil || len(failureGuards) > 1 {
		return nil, MediaFile{}, ErrInvalidInput
	}
	if background {
		if err := analysisContext(ctx); err != nil {
			return nil, MediaFile{}, err
		}
		ctx = media.WithBackgroundProcess(ctx)
	}
	worker, finish, err := s.beginMediaSourceLifetime(ctx)
	if err != nil {
		return nil, MediaFile{}, err
	}
	owned := false
	defer func() {
		if !owned {
			finish()
		}
	}()
	hint, err := s.prepareMediaSourceRoot(worker, background, prepare)
	if err != nil {
		return nil, MediaFile{}, mediaSourceAdmissionFailure(worker, background, err, failureGuards)
	}
	root, domain, err := s.mediaSourceRootLane(hint)
	if err != nil {
		return nil, MediaFile{}, mediaSourceAdmissionFailure(worker, background, err, failureGuards)
	}
	worker = mediaSourceAuthorizationContext(worker, root, domain, background)
	release, err := mediaSourceAdmission.acquireRoot(worker, background, root, domain)
	if err != nil {
		return nil, MediaFile{}, mediaSourceAdmissionFailure(worker, background, err, failureGuards)
	}
	worker = context.WithValue(worker, mediaSourceRootContextKey{}, hint)
	var once sync.Once
	finishOwner := func() { once.Do(func() { release(); finish() }) }
	owned = true
	if background {
		return runAdmittedAnalysisSourceWorker(worker, finishOwner, func() (*os.File, MediaFile, error) { return work(worker) })
	}
	return runAdmittedMediaSourceWorker(worker, finishOwner, func() (*os.File, MediaFile, error) { return work(worker) })
}

// Pre-captured internal source operations preserve their own authority contract.
// Their current server-owned item mapping is reread for routing; the descriptor
// open still checks the captured source and current binding after admission.
func (s *Store) runIndexedMediaSourceWorker(ctx context.Context, background bool, source indexedMediaSource, work func(context.Context) (*os.File, MediaFile, error)) (*os.File, MediaFile, error) {
	return s.runPreparedMediaSourceWorker(ctx, background, func(ctx context.Context) (mediaSourceRootHint, error) {
		hint, err := s.readMediaSourceRootHint(ctx, source.mediaFile.Item.ID)
		if err == nil && hint.root != source.root {
			err = ErrUnavailable
		}
		return hint, err
	}, work)
}

func (s *Store) checkMediaSourceRootAdmission(ctx context.Context, source indexedMediaSource) error {
	hint, governed := ctx.Value(mediaSourceRootContextKey{}).(mediaSourceRootHint)
	if !governed {
		// Direct internal fixtures and the separate subtitle pool retain their
		// existing contract; production primary workers supply a routed hint.
		return nil
	}
	if hint.root != source.root {
		return fmt.Errorf("%w: %w: source root changed while queued", ErrUnavailable, ErrSourceChanged)
	}
	current, err := s.readMediaSourceRootHint(ctx, source.mediaFile.Item.ID)
	if err != nil {
		return err
	}
	if current != hint {
		return fmt.Errorf("%w: %w: source binding changed while queued", ErrUnavailable, ErrSourceChanged)
	}
	return ctx.Err()
}
