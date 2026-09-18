package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/providers"
)

// checkSubtitleProviderActor revalidates a trusted principal in the caller's
// transaction. A nil principal is reserved for internal scheduled work. Mutation
// callers lock the account before its session and recheck after business locks
// and filesystem work, immediately before committing the catalog transaction.
func checkSubtitleProviderActor(ctx context.Context, tx pgx.Tx, actor *identity.Principal, itemID string, lock bool) error {
	if ctx == nil || tx == nil || !metadataIdentifier(itemID) {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor == nil {
		return nil
	}
	if actor.Kind == "admin" {
		administrator := catalogAdministrator{actor: *actor, audience: identity.AdministratorNative}
		if err := administrator.check(ctx, tx, lock); err != nil {
			return err
		}
		if _, err := readQueryParent(ctx, tx, itemID, unrestrictedLibraryAccess()); err != nil {
			return err
		}
		return administrator.check(ctx, tx, false)
	}
	if actor.Kind != "emby" || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
		!validCatalogLibraryIdentifier(actor.User.ID) || !validCatalogLibraryIdentifier(actor.SessionID) {
		return ErrForbidden
	}
	statement := `SELECT is_disabled,
		CASE WHEN octet_length(policy::text) <= 131072 THEN policy END,
		is_administrator FROM users WHERE id = $1`
	if lock {
		statement += " FOR SHARE"
	}
	var disabled, administrator bool
	var policyJSON []byte
	err := tx.QueryRow(ctx, statement, actor.User.ID).Scan(&disabled, &policyJSON, &administrator)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && disabled {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("read subtitle provider account: %w", err)
	}
	statement = `SELECT id FROM sessions WHERE id = $1 AND user_id = $2 AND kind = 'emby'`
	if lock {
		statement += " FOR SHARE"
	}
	var sessionID string
	if err := tx.QueryRow(ctx, statement, actor.SessionID, actor.User.ID).Scan(&sessionID); errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	} else if err != nil {
		return fmt.Errorf("read subtitle provider session: %w", err)
	}
	// This separate statement evaluates expiry after any account or session lock
	// wait and reads the persisted device identity rather than principal metadata.
	var authorized bool
	var deviceID string
	var observedAt time.Time
	err = tx.QueryRow(ctx, `SELECT COALESCE(revoked_at IS NULL AND expires_at > clock_timestamp(), false),
		device_id, clock_timestamp() FROM sessions
		WHERE id = $1 AND user_id = $2 AND kind = 'emby'`, actor.SessionID, actor.User.ID).
		Scan(&authorized, &deviceID, &observedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !authorized {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("revalidate subtitle provider session: %w", err)
	}
	access, err := parseLibraryPolicy(policyJSON)
	if err != nil {
		return err
	}
	policy := access.policy
	if !policy.EnableSubtitleDownloading || !policy.AllowsFeature(identity.FeatureSubtitleDownloads) || !policy.AllowsDevice(deviceID) || !policy.AllowsAccessAt(observedAt) ||
		!policy.EnableRemoteAccess && !identity.IsLocalPeer(actor.PeerIP) {
		return ErrForbidden
	}
	var loginState struct {
		LockedOutDate *int64
	}
	if json.Unmarshal(policyJSON, &loginState) != nil || loginState.LockedOutDate != nil && *loginState.LockedOutDate != 0 {
		return ErrForbidden
	}
	access.userID, access.administrator = actor.User.ID, administrator
	if administrator {
		access.all = true
	}
	if _, err := readQueryParent(ctx, tx, itemID, access); err != nil {
		return err
	}
	// A visibility query may wait on catalog data. Keep the final database
	// operation a fresh credential check, then apply time and device policy again.
	err = tx.QueryRow(ctx, `SELECT COALESCE(revoked_at IS NULL AND expires_at > clock_timestamp(), false),
		device_id, clock_timestamp() FROM sessions
		WHERE id = $1 AND user_id = $2 AND kind = 'emby'`, actor.SessionID, actor.User.ID).
		Scan(&authorized, &deviceID, &observedAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !authorized {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("complete subtitle provider session authorization: %w", err)
	}
	if !policy.AllowsDevice(deviceID) || !policy.AllowsAccessAt(observedAt) ||
		!policy.EnableRemoteAccess && !identity.IsLocalPeer(actor.PeerIP) {
		return ErrForbidden
	}
	return nil
}

// SubtitleProviderTarget returns only authorized search facts and a tag for the
// exact indexed media snapshot to which a later subtitle download must belong.
func (s *Store) SubtitleProviderTarget(ctx context.Context, actor identity.Principal, itemID, sourceID string) (providers.Query, string, error) {
	if ctx == nil || !metadataIdentifier(itemID) {
		return providers.Query{}, "", ErrInvalidInput
	}
	if sourceID != media.SourceID(itemID) {
		return providers.Query{}, "", ErrNotFound
	}
	if s == nil || s.pool == nil {
		return providers.Query{}, "", ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return providers.Query{}, "", fmt.Errorf("begin subtitle provider target read: %w", err)
	}
	defer rollback(tx)
	if err := checkSubtitleProviderActor(ctx, tx, &actor, itemID, false); err != nil {
		return providers.Query{}, "", err
	}
	record, err := readMetadataRecord(ctx, tx, itemID, false)
	if err != nil {
		return providers.Query{}, "", err
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return providers.Query{}, "", err
	}
	snapshot, err := queryProviderSubtitleSnapshot(ctx, tx, itemID, false)
	if err != nil {
		return providers.Query{}, "", err
	}
	query, err := resolveProviderQuery(ctx, tx, detail)
	if err != nil {
		return providers.Query{}, "", err
	}
	tag := mediaSnapshotTag(snapshot.primary)
	if err := checkSubtitleProviderActor(ctx, tx, &actor, itemID, false); err != nil {
		return providers.Query{}, "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return providers.Query{}, "", fmt.Errorf("complete subtitle provider target read: %w", err)
	}
	return query, tag, nil
}

// DownloadedSubtitleIndex resolves the newest active index for a provider
// identity within the same authorized, root-matched media snapshot.
func (s *Store) DownloadedSubtitleIndex(ctx context.Context, actor identity.Principal, itemID, remoteID string) (int, error) {
	if ctx == nil || !metadataIdentifier(itemID) || !metadataIdentifier(remoteID) {
		return 0, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return 0, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return 0, fmt.Errorf("begin downloaded subtitle index read: %w", err)
	}
	defer rollback(tx)
	if err := checkSubtitleProviderActor(ctx, tx, &actor, itemID, false); err != nil {
		return 0, err
	}
	snapshot, err := queryProviderSubtitleSnapshot(ctx, tx, itemID, false)
	if err != nil {
		return 0, err
	}
	var index int
	err = tx.QueryRow(ctx, `SELECT s.stream_index FROM item_subtitle_provider_sources p
		JOIN item_subtitles s ON s.item_id = p.item_id AND s.stream_index = p.stream_index
		JOIN items i ON i.id = s.item_id AND i.root_id = s.root_id
		WHERE p.item_id = $1 AND p.provider = 'opensubtitles' AND p.provider_id = $2
		AND s.active AND s.stream_index > $3
		ORDER BY s.stream_index DESC LIMIT 1`, itemID, remoteID,
		highestEmbeddedStreamIndex(snapshot.primary.mediaFile.Item.Media)).Scan(&index)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read downloaded subtitle index: %w", err)
	}
	if err := checkSubtitleProviderActor(ctx, tx, &actor, itemID, false); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("complete downloaded subtitle index read: %w", err)
	}
	return index, nil
}
