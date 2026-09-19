package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
)

// AvatarCollection shares the managed-artwork revision and byte limits. A
// deleted avatar retains a revisioned empty set rather than reusing an old tag.
type AvatarCollection struct {
	Revision string
	Images   []artwork.StoredImage
}

type AvatarProjection struct {
	Tag         string
	AspectRatio float64
}

func avatarCollection(userID string, set artwork.ManagedSet) AvatarCollection {
	return AvatarCollection{Revision: artwork.RevisionToken(artwork.Target{Kind: "user", ID: userID}, set.Revision, set.Images), Images: set.Images}
}

// Avatar authorization locks all actor/target accounts in account-management
// order before credentials, then the caller may acquire the artwork quota and
// state locks. User preferences restrict self mutation, not an authorized read.
func authorizeAvatar(ctx context.Context, tx pgx.Tx, actor Principal, userID string, lock, write bool) error {
	if !validRevalidationID(userID) {
		return ErrInvalidInput
	}
	check := func() error {
		if actor.IsApplicationKey() {
			return CheckAdministrator(ctx, tx, actor, AdministratorEmby, false)
		}
		if actor.Kind == "admin" {
			return CheckAdministrator(ctx, tx, actor, AdministratorNative, false)
		}
		if actor.Kind != "emby" || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" ||
			!validRevalidationID(actor.User.ID) || !validRevalidationID(actor.SessionID) {
			return ErrUnauthorized
		}
		var administrator bool
		var policyJSON json.RawMessage
		var deviceID string
		var observedAt time.Time
		err := tx.QueryRow(ctx, `SELECT u.is_administrator,u.policy,s.device_id,clock_timestamp()
			FROM users u JOIN sessions s ON s.user_id=u.id
			WHERE u.id=$1 AND s.id=$2 AND s.kind='emby' AND NOT u.is_disabled
			AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp()`, actor.User.ID, actor.SessionID).
			Scan(&administrator, &policyJSON, &deviceID, &observedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		if err != nil {
			return fmt.Errorf("read avatar authority: %w", err)
		}
		policy, err := ParseRuntimePolicy(policyJSON)
		if err != nil || deviceID != actor.Client.DeviceID || !loginPolicyAllows(policyJSON, deviceID, observedAt) ||
			!policy.EnableRemoteAccess && !IsLocalPeer(actor.PeerIP) {
			return ErrUnauthorized
		}
		if userID != actor.User.ID && !administrator {
			return ErrClientSessionForbidden
		}
		if write && userID == actor.User.ID && (!policy.EnableUserPreferenceAccess || !policy.AllowsFeature(FeaturePreferences)) {
			return ErrClientSessionForbidden
		}
		return nil
	}
	if err := check(); err != nil || !lock {
		return err
	}
	ids := []string{userID}
	if !actor.IsApplicationKey() && actor.User.ID != userID {
		ids = append(ids, actor.User.ID)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, ids)
	if err != nil {
		return fmt.Errorf("lock avatar accounts: %w", err)
	}
	found := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		found = found || id == userID
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if actor.IsApplicationKey() {
		if err := CheckAdministrator(ctx, tx, actor, AdministratorEmby, true); err != nil {
			return err
		}
	} else {
		var credential string
		err := tx.QueryRow(ctx, `SELECT id FROM sessions WHERE id=$1 AND user_id=$2 AND kind=$3 FOR SHARE`, actor.SessionID, actor.User.ID, actor.Kind).Scan(&credential)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnauthorized
		}
		if err != nil {
			return fmt.Errorf("lock avatar credential: %w", err)
		}
	}
	if err := check(); err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAvatar(ctx context.Context, actor Principal, userID string) (AvatarCollection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarCollection{}, err
	}
	defer rollback(tx)
	if err := authorizeAvatar(ctx, tx, actor, userID, true, false); err != nil {
		return AvatarCollection{}, err
	}
	set, err := artwork.ReadManagedSet(ctx, tx, artwork.Target{Kind: "user", ID: userID}, true, false)
	if err != nil {
		return AvatarCollection{}, err
	}
	if err := authorizeAvatar(ctx, tx, actor, userID, false, false); err != nil {
		return AvatarCollection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AvatarCollection{}, err
	}
	return avatarCollection(userID, set), nil
}

func (s *Store) PutAvatar(ctx context.Context, actor Principal, userID string, revision *string, content []byte) (AvatarCollection, error) {
	// Reject revoked callers before decoding, then repeat authority in the actual
	// mutation after the bounded image decoder has finished outside SQL locks.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarCollection{}, err
	}
	err = authorizeAvatar(ctx, tx, actor, userID, true, true)
	rollback(tx)
	if err != nil {
		return AvatarCollection{}, err
	}
	image, err := artwork.PrepareManagedImage(ctx, "Primary", 0, content)
	if err != nil {
		return AvatarCollection{}, err
	}
	return s.updateAvatar(ctx, actor, userID, revision, []artwork.StoredImage{image})
}

func (s *Store) DeleteAvatar(ctx context.Context, actor Principal, userID string, revision *string) (AvatarCollection, error) {
	return s.updateAvatar(ctx, actor, userID, revision, []artwork.StoredImage{})
}

func (s *Store) updateAvatar(ctx context.Context, actor Principal, userID string, revision *string, images []artwork.StoredImage) (AvatarCollection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarCollection{}, err
	}
	defer rollback(tx)
	if err := authorizeAvatar(ctx, tx, actor, userID, true, true); err != nil {
		return AvatarCollection{}, err
	}
	if err := artwork.LockManagedArtwork(ctx, tx); err != nil {
		return AvatarCollection{}, err
	}
	target := artwork.Target{Kind: "user", ID: userID}
	set, err := artwork.ReadManagedSet(ctx, tx, target, true, false)
	if err != nil {
		return AvatarCollection{}, err
	}
	if err := authorizeAvatar(ctx, tx, actor, userID, false, true); err != nil {
		return AvatarCollection{}, err
	}
	if revision != nil && *revision != artwork.RevisionToken(target, set.Revision, set.Images) {
		return AvatarCollection{}, artwork.ErrManagedConflict
	}
	set, err = artwork.ReplaceManagedType(ctx, tx, target, "Primary", images, false)
	if err != nil {
		return AvatarCollection{}, err
	}
	if err := authorizeAvatar(ctx, tx, actor, userID, false, true); err != nil {
		return AvatarCollection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AvatarCollection{}, err
	}
	return avatarCollection(userID, set), nil
}

func readAvatarImage(ctx context.Context, tx pgx.Tx, userID string) (artwork.StoredImage, error) {
	target := artwork.Target{Kind: "user", ID: userID}
	// Hold the existing set row until the bytes and current authority have both
	// been read. An absent row is an exact empty snapshot, not a default picture.
	set, err := artwork.ReadManagedSet(ctx, tx, target, true, false)
	if err != nil {
		return artwork.StoredImage{}, err
	}
	if len(set.Images) != 1 || set.Images[0].ImageType != "Primary" || set.Images[0].ImageIndex != 0 {
		if len(set.Images) == 0 {
			return artwork.StoredImage{}, ErrNotFound
		}
		return artwork.StoredImage{}, artwork.ErrManagedStorage
	}
	image, _, err := artwork.ReadManagedImage(ctx, tx, target, "Primary", 0)
	if errors.Is(err, pgx.ErrNoRows) {
		return artwork.StoredImage{}, ErrNotFound
	}
	return image, err
}

func (s *Store) ReadAvatar(ctx context.Context, actor Principal, userID string) (artwork.StoredImage, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return artwork.StoredImage{}, err
	}
	defer rollback(tx)
	if err := authorizeAvatar(ctx, tx, actor, userID, true, false); err != nil {
		return artwork.StoredImage{}, err
	}
	image, err := readAvatarImage(ctx, tx, userID)
	if err != nil {
		return artwork.StoredImage{}, err
	}
	if err := authorizeAvatar(ctx, tx, actor, userID, false, false); err != nil {
		return artwork.StoredImage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return artwork.StoredImage{}, err
	}
	return image, nil
}

// PublicAvatarVisible mirrors the public-login account picker. It is a display
// predicate; ReadPublicAvatar applies it with current locked database facts.
func PublicAvatarVisible(user User, remote bool, deviceID string, usedDevice bool) bool {
	policy, err := ParseRuntimePolicy(user.Policy)
	if err != nil || user.IsDisabled || user.IsAdministrator || policy.IsHidden ||
		remote && (policy.IsHiddenRemotely || !policy.EnableRemoteAccess) ||
		policy.IsHiddenFromUnusedDevices && !usedDevice {
		return false
	}
	if !policy.EnableAllDevices {
		if deviceID == "" {
			return false
		}
		for _, id := range policy.EnabledDevices {
			if id == deviceID {
				return true
			}
		}
		return false
	}
	return true
}

func publicAvatarAccount(ctx context.Context, tx pgx.Tx, userID string, remote bool, deviceID string, lock bool) error {
	if !validRevalidationID(userID) {
		return ErrNotFound
	}
	query := "SELECT " + userColumns + " FROM users WHERE id=$1"
	if lock {
		query += " FOR SHARE"
	}
	user, err := scanUser(tx.QueryRow(ctx, query, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	used := false
	if validRevalidationID(deviceID) {
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE user_id=$1 AND device_id=$2 AND kind='emby')`, userID, deviceID).Scan(&used); err != nil {
			return err
		}
	}
	if !PublicAvatarVisible(user, remote, deviceID, used) {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ReadPublicAvatar(ctx context.Context, userID string, remote bool, deviceID string) (artwork.StoredImage, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return artwork.StoredImage{}, err
	}
	defer rollback(tx)
	if err := publicAvatarAccount(ctx, tx, userID, remote, deviceID, true); err != nil {
		return artwork.StoredImage{}, err
	}
	image, err := readAvatarImage(ctx, tx, userID)
	if err != nil {
		return artwork.StoredImage{}, err
	}
	if err := publicAvatarAccount(ctx, tx, userID, remote, deviceID, false); err != nil {
		return artwork.StoredImage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return artwork.StoredImage{}, err
	}
	return image, nil
}

// AvatarProjections enriches already-authorized user DTOs. It is deliberately
// separate from authentication so old-schema migration fixtures and login
// validity never depend on the managed-artwork tables being available.
func (s *Store) AvatarProjections(ctx context.Context, userIDs []string) (map[string]AvatarProjection, error) {
	if len(userIDs) > 256 {
		return nil, ErrInvalidInput
	}
	for _, id := range userIDs {
		if !validRevalidationID(id) {
			return nil, ErrInvalidInput
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT a.user_id,i.source_hash,i.width,i.height FROM artwork_state a
		JOIN artwork_images i ON i.state_id=a.id AND i.image_type='Primary' AND i.image_index=0
		WHERE a.user_id=ANY($1::text[]) AND 'Primary'=ANY(a.managed_types)`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]AvatarProjection)
	for rows.Next() {
		var id, tag string
		var width, height int
		if err := rows.Scan(&id, &tag, &width, &height); err != nil {
			return nil, err
		}
		if len(tag) != 64 || width < 1 || height < 1 {
			return nil, artwork.ErrManagedStorage
		}
		result[id] = AvatarProjection{Tag: tag, AspectRatio: float64(width) / float64(height)}
	}
	return result, rows.Err()
}
