package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// PlaybackMediaAuthorization contains the current authority and source facts
// for one delivery authorization stage. It is never reusable across stages.
type PlaybackMediaAuthorization struct {
	Principal identity.Principal
	Play      PlaySession
	Source    MediaFile
}

// AuthorizePlaybackMediaFor combines credential, canonical playback and media
// validation in one short transaction. The principal must come from trusted
// authentication, and playID must be the canonical ID retained by the server.
// Compatible row locks protect the facts until the final database-clock check;
// no database connection or row lock survives a filesystem operation.
func (s *Store) AuthorizePlaybackMediaFor(ctx context.Context, principal identity.Principal, playID, itemID, sourceID string, includeSubtitles bool) (*os.File, PlaybackMediaAuthorization, error) {
	return s.AuthorizePlaybackMediaForChecked(ctx, principal, playID, itemID, sourceID, includeSubtitles, nil)
}

// AuthorizePlaybackMediaForChecked also applies an optional delivery-specific
// policy check after completing authority and before opening the source. The
// callback never runs while database locks or a database connection are held.
func (s *Store) AuthorizePlaybackMediaForChecked(ctx context.Context, principal identity.Principal, playID, itemID, sourceID string, includeSubtitles bool, check func(PlaybackMediaAuthorization) error) (*os.File, PlaybackMediaAuthorization, error) {
	owner := playbackMediaOwner(principal)
	if !validPlaybackOwner(owner) || strings.TrimSpace(playID) == "" || strings.ContainsRune(playID, '\x00') ||
		strings.TrimSpace(itemID) == "" || strings.ContainsRune(itemID, '\x00') || strings.ContainsRune(sourceID, '\x00') ||
		(!owner.ApplicationKey && principal.Kind != "emby") || (owner.ApplicationKey && principal.ApplicationKeyID <= 0) {
		return nil, PlaybackMediaAuthorization{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil || s.closing.Load() {
		return nil, PlaybackMediaAuthorization{}, ErrUnavailable
	}
	return s.openPlaybackMediaAuthorization(ctx, principal, owner, playID, itemID, sourceID, includeSubtitles, check)
}

func playbackMediaOwner(principal identity.Principal) PlaybackOwner {
	return PlaybackOwner{UserID: principal.User.ID, SessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		PeerIP: principal.PeerIP, ApplicationClientID: principal.ClientSessionID, ApplicationKey: principal.IsApplicationKey()}
}

func (s *Store) readPlaybackMediaAuthorization(ctx context.Context, principal identity.Principal, owner PlaybackOwner, playID, itemID, sourceID string, includeSubtitles bool) (indexedMediaSource, PlaybackMediaAuthorization, error) {
	return s.readPlaybackMediaAuthorizationWithAdmission(ctx, principal, owner, playID, itemID, sourceID, includeSubtitles, nil)
}

func (s *Store) readPlaybackMediaAuthorizationWithAdmission(ctx context.Context, principal identity.Principal, owner PlaybackOwner, playID, itemID, sourceID string, includeSubtitles bool, admission func(indexedMediaSource) error) (indexedMediaSource, PlaybackMediaAuthorization, error) {
	return s.readPlaybackMediaAuthorizationPrepared(ctx, principal, owner, playID, itemID, sourceID, includeSubtitles, nil, admission)
}

func (s *Store) readPlaybackMediaAuthorizationPrepared(ctx context.Context, principal identity.Principal, owner PlaybackOwner, playID, itemID, sourceID string, includeSubtitles bool, expectedBinding *mediaSourceRootHint, admission func(indexedMediaSource) error) (indexedMediaSource, PlaybackMediaAuthorization, error) {
	ctx, release, err := beginMediaSourceAuthorization(ctx)
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	defer release()
	connection, err := s.pool.Acquire(ctx)
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, fmt.Errorf("%w: acquire playback media authorization: %w", ErrUnavailable, err)
	}
	defer connection.Release()
	tx, err := connection.Begin(ctx)
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, fmt.Errorf("%w: begin playback media authorization: %w", ErrUnavailable, err)
	}
	defer rollback(tx)
	// Keep the same account -> credential -> item -> playback lock order as
	// reports, Stop, policy mutation and playback preparation.
	if err := lockPlaybackMediaAuthorityBatch(ctx, tx, principal, owner); err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	fresh, access, err := readPlaybackMediaPrincipal(ctx, tx, principal)
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	// Credential policy is fresh before item/source reads. Their complete
	// results are closed and validated before the play batch can wait.
	snapshot, err := readIndexedPlaybackMediaBatch(ctx, tx, access, itemID, sourceID, includeSubtitles)
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	if snapshot.mediaFile.Item.Media.DurationTicks < 0 {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, ErrNotFound
	}
	play, observedAt, isolation, binding, bindingErr, err := readPlaybackMediaPlayAndBindingBatch(ctx, tx, owner, playID, itemID, expectedBinding != nil)
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	if play.IsDynamic {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, ErrSourceChanged
	}
	if play.ItemID != itemID || play.MediaSourceID != snapshot.mediaFile.SourceID || !play.live || !observedAt.Before(play.ExpiresAt) ||
		(play.State != "Prepared" && play.State != "Playing" && play.State != "Paused") {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, ErrNotFound
	}
	if err := identity.ValidateRevalidatedSessionAt(fresh, observedAt); err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	if expectedBinding != nil {
		if bindingErr != nil {
			return indexedMediaSource{}, PlaybackMediaAuthorization{}, bindingErr
		}
		if binding != *expectedBinding || snapshot.root != binding.root || snapshot.rootBindingRevision != binding.bindingRevision {
			return indexedMediaSource{}, PlaybackMediaAuthorization{}, fmt.Errorf("%w: %w: prepared source binding changed", ErrUnavailable, ErrSourceChanged)
		}
	}
	// The admission hook is strictly memory-only and never enqueues. All
	// authority SHARE locks and the final database-clock check still apply at
	// the grant. The transaction ends before policy callbacks or filesystem work.
	if admission != nil {
		if err := admission(snapshot); err != nil {
			return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
		}
	}
	// This transaction only reads and locks rows. READ COMMITTED has no
	// commit-time validation to retain, so discard its locks without committing
	// a transaction that has no business writes. Other isolation levels retain
	// their existing commit semantics. The acquired connection stays owned
	// until completion, including this active-transaction status check.
	if isolation == "read committed" {
		if connection.Conn().PgConn().TxStatus() != 'T' {
			return indexedMediaSource{}, PlaybackMediaAuthorization{}, fmt.Errorf("%w: playback media authorization transaction is not active", ErrUnavailable)
		}
		err = tx.Rollback(ctx)
	} else {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, fmt.Errorf("%w: complete playback media authorization: %w", ErrUnavailable, err)
	}
	connection.Release()
	if err := s.sealPrimaryMediaReadSnapshot(ctx, &snapshot); err != nil {
		return indexedMediaSource{}, PlaybackMediaAuthorization{}, err
	}
	return snapshot, PlaybackMediaAuthorization{Principal: fresh, Play: play}, nil
}

type playbackMediaClockRow struct {
	pgx.Row
	observedAt *time.Time
	isolation  *string
}

func (row playbackMediaClockRow) Scan(destinations ...any) error {
	return row.Row.Scan(append(destinations, row.observedAt, row.isolation)...)
}

func lockPlaybackMediaAuthority(ctx context.Context, tx pgx.Tx, statement string, arguments ...any) error {
	var id string
	err := tx.QueryRow(ctx, statement, arguments...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("%w: lock playback media authority: %w", ErrUnavailable, err)
	}
	return nil
}

func indexedPlaybackMediaSQL(access libraryAccess) string {
	// Capture the source and its complete publication/binding revisions in one
	// READ COMMITTED statement. Never lock roots after locking the item.
	return `SELECT i.id, i.library_id, i.type, i.path, i.media,
		i.relative_path, i.file_identity, i.file_size, i.modified_at,
		r.id, r.library_id, r.path, r.allowed_path, r.relative_path, r.binding_revision,
		` + MediaOperationSourceRevisionSQL + `, EXISTS (SELECT 1 FROM media_operations publication
		WHERE publication.source_item_id=i.id AND publication.publication_phase IN ('prepared','catalog_committed'))
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND NOT i.is_folder AND i.media IS NOT NULL
		AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')
		AND ($2::boolean OR i.library_id=ANY($3::text[])) AND ` + access.directSQL("i")
}

func scanIndexedPlaybackMedia(row rowScanner) (indexedMediaSource, *time.Time, error) {
	var snapshot indexedMediaSource
	var modified *time.Time
	var encoded []byte
	var publishing bool
	item := &snapshot.mediaFile.Item
	err := row.Scan(&item.ID, &item.LibraryID, &item.Type, &item.Path, &encoded,
		&snapshot.relativePath, &snapshot.identity, &snapshot.mediaFile.Size, &modified,
		&snapshot.root.id, &snapshot.root.libraryID, &snapshot.root.path, &snapshot.root.allowedPath, &snapshot.root.relativePath, &snapshot.rootBindingRevision,
		&snapshot.publicationRevision, &publishing)
	if errors.Is(err, pgx.ErrNoRows) {
		return indexedMediaSource{}, nil, ErrNotFound
	}
	if err != nil {
		return indexedMediaSource{}, nil, fmt.Errorf("%w: read playback media snapshot: %w", ErrUnavailable, err)
	}
	if publishing {
		return indexedMediaSource{}, nil, fmt.Errorf("%w: this media source is being published", ErrBusy)
	}
	if len(encoded) != 0 && string(encoded) != "null" {
		item.Media = &media.Info{}
		if err := json.Unmarshal(encoded, item.Media); err != nil {
			return indexedMediaSource{}, nil, fmt.Errorf("%w: decode playback media snapshot: %w", ErrUnavailable, err)
		}
	}
	return snapshot, modified, nil
}

func readIndexedPlaybackMedia(ctx context.Context, tx pgx.Tx, access libraryAccess, itemID, sourceID string, includeSubtitles bool) (indexedMediaSource, error) {
	snapshot, modified, err := scanIndexedPlaybackMedia(tx.QueryRow(ctx, indexedPlaybackMediaSQL(access), itemID, access.all, access.folders))
	if err != nil {
		return indexedMediaSource{}, err
	}
	return completeIndexedMediaSource(ctx, tx, access, snapshot, sourceID, modified, includeSubtitles)
}

// CacheDescribe connections with an enabled statement cache retain one named
// plan for the complex source query. Other configurations keep the source batch.
func readIndexedPlaybackMediaBatch(ctx context.Context, tx pgx.Tx, access libraryAccess, itemID, sourceID string, includeSubtitles bool) (indexedMediaSource, error) {
	configuration := tx.Conn().Config()
	if configuration.DefaultQueryExecMode == pgx.QueryExecModeCacheDescribe && configuration.StatementCacheCapacity > 0 {
		var id string
		if err := tx.QueryRow(ctx, "SELECT id FROM items WHERE id=$1 FOR SHARE", itemID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return indexedMediaSource{}, ErrNotFound
			}
			return indexedMediaSource{}, fmt.Errorf("%w: lock playback media item: %w", ErrUnavailable, err)
		}
		// Each Scan closes its result and returns any completion error. The
		// second statement retains its fresh snapshot after the SHARE lock wait.
		snapshot, modified, err := scanIndexedPlaybackMedia(tx.QueryRow(ctx, indexedPlaybackMediaSQL(access),
			pgx.QueryExecModeCacheStatement, itemID, access.all, access.folders))
		if err != nil {
			return indexedMediaSource{}, err
		}
		return completeIndexedMediaSource(ctx, tx, access, snapshot, sourceID, modified, includeSubtitles)
	}
	batch := &pgx.Batch{}
	batch.Queue("SELECT id FROM items WHERE id=$1 FOR SHARE", itemID)
	batch.Queue(indexedPlaybackMediaSQL(access), itemID, access.all, access.folders)
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	var id string
	if err := results.QueryRow().Scan(&id); err != nil {
		_ = results.Close()
		if errors.Is(err, pgx.ErrNoRows) {
			return indexedMediaSource{}, ErrNotFound
		}
		return indexedMediaSource{}, fmt.Errorf("%w: lock playback media item: %w", ErrUnavailable, err)
	}
	snapshot, modified, err := scanIndexedPlaybackMedia(results.QueryRow())
	closeErr := results.Close()
	if err != nil {
		return indexedMediaSource{}, err
	}
	if closeErr != nil {
		return indexedMediaSource{}, fmt.Errorf("%w: complete playback source batch: %w", ErrUnavailable, closeErr)
	}
	// Optional subtitle queries and all source validation happen after Close,
	// before any playback batch is sent. No busy-source error waits on play.
	return completeIndexedMediaSource(ctx, tx, access, snapshot, sourceID, modified, includeSubtitles)
}

func readPlaybackMediaPrincipal(ctx context.Context, tx pgx.Tx, previous identity.Principal) (identity.Principal, libraryAccess, error) {
	fresh, err := identity.RevalidateSessionInTransaction(ctx, tx, previous)
	if err != nil {
		return identity.Principal{}, libraryAccess{}, err
	}
	if fresh.Client.DeviceID != previous.Client.DeviceID {
		return identity.Principal{}, libraryAccess{}, ErrForbidden
	}
	fresh.PeerIP = previous.PeerIP
	if fresh.IsApplicationKey() {
		return fresh, unrestrictedLibraryAccess(), nil
	}
	access, err := parseLibraryPolicy(fresh.User.Policy)
	if err != nil || !access.canPlay {
		return identity.Principal{}, libraryAccess{}, ErrForbidden
	}
	access.userID, access.administrator = fresh.User.ID, fresh.User.IsAdministrator
	if fresh.User.IsAdministrator {
		access.all = true
	}
	return fresh, access, nil
}
