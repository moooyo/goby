package library

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

const (
	PlaylistKind        = "Playlist"
	BoxSetKind          = "BoxSet"
	collectionLibraryID = "00000000000000000000000000000030"
	// CollectionsLibraryID is reserved for collections without a physical parent.
	CollectionsLibraryID = collectionLibraryID
	collectionInputLimit = 1000
	collectionEntryLimit = 10000
)

type CollectionInput struct {
	Name, ParentID, MediaType string
	IsPublic, IsLocked        bool
	ItemIDs                   []string
}

type CollectionShare struct {
	UserID  string
	CanEdit bool
}

type CollectionInfo struct {
	ID, Name, ParentID, OwnerID, Kind, MediaType string
	IsPublic, IsLocked                           bool
	ItemCount                                    int
	Shares                                       []CollectionShare
	UserData                                     *UserData
}

type CollectionPatch struct {
	Name               *string
	IsPublic, IsLocked *bool
	Shares             *[]CollectionShare
}

type CollectionPreview struct {
	ItemCount          int
	ContainsDuplicates bool
}

type collectionAdministratorContextKey struct{}
type collectionActorContextKey struct{}

// WithCollectionActor retains the authenticated credential across admission and
// commit. HTTP callers bind this independently from the projected Subject.
func WithCollectionActor(ctx context.Context, actor identity.Principal) context.Context {
	return context.WithValue(ctx, collectionActorContextKey{}, actor)
}

func collectionActor(ctx context.Context) (identity.Principal, bool) {
	actor, exists := ctx.Value(collectionActorContextKey{}).(identity.Principal)
	return actor, exists
}

// WithCollectionAdministrator binds native management operations to the
// authenticated administrator. The owned write revalidates and locks that
// credential, independently from the collection owner's ordinary permissions.
func WithCollectionAdministrator(ctx context.Context, actor identity.Principal) context.Context {
	return context.WithValue(WithCollectionActor(ctx, actor), collectionAdministratorContextKey{}, &catalogAdministrator{actor: actor, audience: identity.AdministratorNative})
}

func collectionAdministrator(ctx context.Context) *catalogAdministrator {
	administrator, _ := ctx.Value(collectionAdministratorContextKey{}).(*catalogAdministrator)
	return administrator
}

func commitCollectionWrite(ctx context.Context, tx pgx.Tx) error {
	protected := tx.(*ownedTx).ctx
	if actor, exists := collectionActor(ctx); exists {
		if _, err := checkFileMutationActor(protected, tx, actor, false); err != nil {
			return err
		}
	}
	if err := collectionAdministrator(ctx).check(protected, tx, false); err != nil {
		return err
	}
	return tx.Commit(protected)
}

func (s *Store) beginCollectionRead(ctx context.Context, subject Subject) (pgx.Tx, libraryAccess, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, libraryAccess{}, err
	}
	if err := collectionAdministrator(ctx).check(ctx, tx, false); err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	return tx, access, nil
}

func validCollectionKind(kind string) bool { return kind == PlaylistKind || kind == BoxSetKind }

func validCollectionID(id string) bool {
	return len(id) > 0 && len(id) <= 128 && utf8.ValidString(id) &&
		strings.TrimSpace(id) == id && strings.IndexFunc(id, unicode.IsControl) < 0
}

func normalizeCollectionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) > 1024 || !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 ||
		utf8.RuneCountInString(name) > 256 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrInvalidInput
	}
	return name, nil
}

func validateCollectionIDs(ids []string, required bool) error {
	if len(ids) > collectionInputLimit || required && len(ids) == 0 {
		return ErrInvalidInput
	}
	for _, id := range ids {
		if !validCollectionID(id) {
			return ErrInvalidInput
		}
	}
	return nil
}

// collectionAccessSQL applies independent ownership and sharing to every
// collection projection. Sharing does not grant access to its member media.
func collectionAccessSQL(alias, userID string, administrator bool) string {
	user := policySQLString(userID)
	permission := "collection_acl.is_public OR collection_acl.owner_id=" + user + " OR EXISTS (SELECT 1 FROM media_collection_shares collection_share WHERE collection_share.collection_id=collection_acl.item_id AND collection_share.user_id=" + user + ")"
	if administrator {
		permission = "true"
	}
	return "(" + alias + ".type NOT IN ('Playlist','BoxSet') OR EXISTS (SELECT 1 FROM media_collections collection_acl WHERE collection_acl.item_id=" + alias + ".id AND collection_acl.kind=" + alias + ".type AND (" + permission + ")))"
}

// beginCollectionWrite uses the reserved catalog session and protects policy
// rows before any collection state. Cancellation cannot fence that session.
func (s *Store) beginCollectionWrite(ctx context.Context, subject Subject, containerID string, recipientIDs []string) (pgx.Tx, libraryAccess, error) {
	return s.beginCollectionWriteWithScope(ctx, subject, containerID, recipientIDs, false)
}

func (s *Store) beginCollectionWriteWithScope(ctx context.Context, subject Subject, containerID string, recipientIDs []string, independentUserState bool) (pgx.Tx, libraryAccess, error) {
	if ctx == nil || !validSubject(subject) {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	if s == nil {
		return nil, libraryAccess{}, ErrUnavailable
	}
	actor, hasActor := collectionActor(ctx)
	if !independentUserState && hasActor && ((actor.IsApplicationKey() && subject.ApplicationCredentialID != actor.SessionID) ||
		(!actor.IsApplicationKey() && (subject.UserID != actor.User.ID || subject.ApplicationCredentialID != ""))) {
		return nil, libraryAccess{}, ErrForbidden
	}
	tx, err := s.beginOwnedAdmission(ctx, false)
	if err != nil {
		return nil, libraryAccess{}, err
	}
	s.mu.Unlock()
	protected := tx.(*ownedTx).ctx
	// Match managed-user deletion's account order before either side touches
	// cascading collection/share rows. The owner is immutable, so a preliminary
	// lookup followed by account locks and the normal collection lookup is safe.
	accounts := append([]string{subject.UserID}, recipientIDs...)
	if independentUserState && subject.Actor != nil {
		accounts = append(accounts, subject.Actor.User.ID)
	}
	if hasActor {
		accounts = append(accounts, actor.User.ID)
	}
	if administrator := collectionAdministrator(ctx); administrator != nil {
		accounts = append(accounts, administrator.actor.User.ID)
	}
	if containerID != "" {
		var ownerID string
		err := tx.QueryRow(protected, `SELECT owner_id FROM media_collections WHERE item_id=$1`, containerID).Scan(&ownerID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			rollback(tx)
			return nil, libraryAccess{}, err
		}
		if err == nil {
			accounts = append(accounts, ownerID)
		}
	}
	rows, err := tx.Query(protected, `SELECT id FROM users WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, accounts)
	if err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	for rows.Next() {
		var account string
		if err := rows.Scan(&account); err != nil {
			rows.Close()
			rollback(tx)
			return nil, libraryAccess{}, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	if err := collectionAdministrator(ctx).check(protected, tx, true); err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	if hasActor {
		if _, err := checkFileMutationActor(protected, tx, actor, true); err != nil {
			rollback(tx)
			return nil, libraryAccess{}, err
		}
	}
	if independentUserState {
		access, err := checkSubjectStateWrite(protected, tx, subject, false, true)
		if err != nil {
			rollback(tx)
			return nil, libraryAccess{}, err
		}
		return tx, access, nil
	}
	access := libraryAccess{all: true, folders: []string{}, canPlay: true, userID: subject.UserID, administrator: subject.UserID == "" && subject.ApplicationCredentialID != ""}
	if subject.UserID != "" {
		var administrator, disabled bool
		var policy []byte
		err = tx.QueryRow(protected, `SELECT is_administrator, is_disabled, policy FROM users WHERE id=$1 FOR SHARE`, subject.UserID).Scan(&administrator, &disabled, &policy)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		if err == nil && disabled && subject.ApplicationCredentialID == "" {
			err = ErrForbidden
		}
		if err == nil {
			access, err = parseLibraryPolicy(policy)
		}
		access.userID, access.administrator = subject.UserID, administrator
		if administrator {
			access.all = true
		}
		access.canPlay = subject.ApplicationCredentialID != "" || playbackAllowed(policy)
	}
	if err == nil && subject.ApplicationCredentialID != "" {
		err = checkSubjectApplicationKey(protected, tx, subject.ApplicationCredentialID, true)
		access.policy.RestrictedFeatures = nil
	}
	if err != nil {
		rollback(tx)
		return nil, libraryAccess{}, err
	}
	return tx, access, nil
}

func collectionOwner(access libraryAccess, collection CollectionInfo) bool {
	return access.administrator || access.userID != "" && access.userID == collection.OwnerID
}

func collectionFeatureAllowed(access libraryAccess, kind string) bool {
	if kind == PlaylistKind {
		return access.policy.AllowsFeature(identity.FeaturePlaylists)
	}
	if kind == BoxSetKind {
		return access.policy.AllowsFeature(identity.FeatureCollections)
	}
	return false
}

func readCollection(ctx context.Context, tx pgx.Tx, access libraryAccess, id, kind string, lock bool) (CollectionInfo, error) {
	if !validCollectionID(id) || !validCollectionKind(kind) {
		return CollectionInfo{}, ErrInvalidInput
	}
	if !collectionFeatureAllowed(access, kind) {
		return CollectionInfo{}, ErrNotFound
	}
	statement := `SELECT i.id,i.name,COALESCE(i.parent_id,''),c.owner_id,c.kind,c.media_type,c.is_public,c.is_locked
		FROM items i JOIN media_collections c ON c.item_id=i.id
		WHERE i.id=$1 AND c.kind=$2 AND ` + access.itemPolicySQL("i")
	if lock {
		statement += " FOR UPDATE OF c"
	}
	var result CollectionInfo
	err := tx.QueryRow(ctx, statement, id, kind).Scan(&result.ID, &result.Name, &result.ParentID, &result.OwnerID, &result.Kind, &result.MediaType, &result.IsPublic, &result.IsLocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return CollectionInfo{}, ErrNotFound
	}
	if err != nil {
		return CollectionInfo{}, fmt.Errorf("read collection: %w", err)
	}
	result.Shares = []CollectionShare{}
	if owned, ok := tx.(*ownedTx); ok && lock {
		owned.rememberNotificationScope(collectionLibraryID, result.ID, result.ParentID)
	}
	return result, nil
}

func canEditCollection(ctx context.Context, tx pgx.Tx, access libraryAccess, collection CollectionInfo) error {
	if collection.IsLocked {
		return fmt.Errorf("%w: collection is locked", ErrForbidden)
	}
	if collectionOwner(access, collection) {
		return nil
	}
	var canEdit bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media_collection_shares WHERE collection_id=$1 AND user_id=$2 AND can_edit)`, collection.ID, access.userID).Scan(&canEdit); err != nil {
		return err
	}
	if !canEdit {
		return ErrForbidden
	}
	return nil
}

func fillCollectionInfo(ctx context.Context, tx pgx.Tx, access libraryAccess, collection *CollectionInfo) error {
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM media_collection_entries e JOIN items i ON i.id=e.item_id
		WHERE e.collection_id=$1 AND `+access.itemPolicySQL("i")+` AND `+ordinaryItemSQL("i"), collection.ID).Scan(&collection.ItemCount); err != nil {
		return err
	}
	// The sharing roster is management data, not information for public readers.
	if !collectionOwner(access, *collection) {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT user_id,can_edit FROM media_collection_shares WHERE collection_id=$1 ORDER BY user_id`, collection.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var share CollectionShare
		if err := rows.Scan(&share.UserID, &share.CanEdit); err != nil {
			return err
		}
		collection.Shares = append(collection.Shares, share)
	}
	return rows.Err()
}

func (s *Store) CreateCollection(ctx context.Context, subject Subject, kind string, input CollectionInput) (CollectionInfo, error) {
	if input.ParentID == VirtualRootItemID {
		input.ParentID = ""
	}
	if !validCollectionKind(kind) || subject.UserID == "" || input.ParentID != "" && !validCollectionID(input.ParentID) {
		return CollectionInfo{}, ErrInvalidInput
	}
	name, err := normalizeCollectionName(input.Name)
	if err != nil {
		return CollectionInfo{}, err
	}
	if input.MediaType != "" && input.MediaType != "Audio" && input.MediaType != "Video" || kind == BoxSetKind && input.MediaType != "" {
		return CollectionInfo{}, ErrInvalidInput
	}
	if err := validateCollectionIDs(input.ItemIDs, false); err != nil {
		return CollectionInfo{}, err
	}
	id, err := randomID()
	if err != nil {
		return CollectionInfo{}, err
	}
	tx, access, err := s.beginCollectionWrite(ctx, subject, input.ParentID, nil)
	if err != nil {
		return CollectionInfo{}, err
	}
	defer rollback(tx)
	if !collectionFeatureAllowed(access, kind) {
		return CollectionInfo{}, ErrForbidden
	}
	protected := tx.(*ownedTx).ctx
	libraryID := collectionLibraryID
	var parent any
	var parentCollection *CollectionInfo
	if input.ParentID != "" {
		var parentType string
		err = tx.QueryRow(protected, `SELECT i.library_id,i.type FROM items i WHERE i.id=$1 AND i.is_folder AND `+access.itemPolicySQL("i")+` AND `+ordinaryItemSQL("i"), input.ParentID).Scan(&libraryID, &parentType)
		if errors.Is(err, pgx.ErrNoRows) {
			return CollectionInfo{}, ErrNotFound
		}
		if err != nil {
			return CollectionInfo{}, err
		}
		if parentType == PlaylistKind {
			return CollectionInfo{}, ErrInvalidInput
		}
		if parentType == BoxSetKind {
			p, e := readCollection(protected, tx, access, input.ParentID, BoxSetKind, true)
			if e != nil {
				return CollectionInfo{}, e
			}
			if !collectionOwner(access, p) || p.IsLocked {
				return CollectionInfo{}, ErrForbidden
			}
			parentCollection = &p
		}
		parent = input.ParentID
	}
	if libraryID == collectionLibraryID {
		if _, err := tx.Exec(protected, `INSERT INTO libraries (id,name,collection_type) VALUES ($1,'Collections','mixed') ON CONFLICT (id) DO NOTHING`, collectionLibraryID); err != nil {
			return CollectionInfo{}, err
		}
	}
	if _, err = tx.Exec(protected, `INSERT INTO items (id,library_id,parent_id,name,sort_name,type,is_folder) VALUES ($1,$2,$3,$4,$5,$6,true)`, id, libraryID, parent, name, strings.ToLower(name), kind); err != nil {
		return CollectionInfo{}, err
	}
	if err := syncScannedMetadata(protected, tx, id); err != nil {
		return CollectionInfo{}, err
	}
	if _, err = tx.Exec(protected, `INSERT INTO media_collections (item_id,owner_id,kind,media_type,is_public,is_locked) VALUES ($1,$2,$3,$4,$5,$6)`, id, subject.UserID, kind, input.MediaType, input.IsPublic, input.IsLocked); err != nil {
		return CollectionInfo{}, err
	}
	collection := CollectionInfo{ID: id, Name: name, ParentID: input.ParentID, OwnerID: subject.UserID, Kind: kind, MediaType: input.MediaType, IsPublic: input.IsPublic, IsLocked: input.IsLocked, Shares: []CollectionShare{}}
	ids, err := resolveCollectionMembers(protected, tx, access, collection, input.ItemIDs)
	if err != nil {
		return CollectionInfo{}, err
	}
	if _, err := appendCollectionMembers(protected, tx, collection, ids); err != nil {
		return CollectionInfo{}, err
	}
	if parentCollection != nil {
		if _, err := appendCollectionMembers(protected, tx, *parentCollection, []string{id}); err != nil {
			return CollectionInfo{}, err
		}
		if err := touchCollection(protected, tx, parentCollection.ID); err != nil {
			return CollectionInfo{}, err
		}
	}
	if err := fillCollectionInfo(protected, tx, access, &collection); err != nil {
		return CollectionInfo{}, err
	}
	tx.(*ownedTx).rememberNotificationScope(collectionLibraryID, collection.ID, collection.ParentID)
	tx.(*ownedTx).catalogChanges.requireResync()
	if err := commitCollectionWrite(ctx, tx); err != nil {
		return CollectionInfo{}, err
	}
	return collection, nil
}

func (s *Store) GetCollection(ctx context.Context, subject Subject, id, kind string) (CollectionInfo, error) {
	tx, access, err := s.beginCollectionRead(ctx, subject)
	if err != nil {
		return CollectionInfo{}, err
	}
	defer rollback(tx)
	result, err := readCollection(ctx, tx, access, id, kind, false)
	if err != nil {
		return CollectionInfo{}, err
	}
	if err := fillCollectionInfo(ctx, tx, access, &result); err != nil {
		return CollectionInfo{}, err
	}
	items := []Item{{ID: result.ID, Type: kind, IsFolder: true}}
	if err := attachUserData(ctx, tx, subject.UserID, items, access); err != nil {
		return CollectionInfo{}, err
	}
	result.UserData = items[0].UserData
	if err := tx.Commit(ctx); err != nil {
		return CollectionInfo{}, err
	}
	return result, nil
}

func (s *Store) UpdateCollection(ctx context.Context, subject Subject, id, kind string, patch CollectionPatch) (CollectionInfo, error) {
	if patch.Name != nil {
		name, err := normalizeCollectionName(*patch.Name)
		if err != nil {
			return CollectionInfo{}, err
		}
		patch.Name = &name
	}
	if patch.Shares != nil {
		if len(*patch.Shares) > collectionInputLimit {
			return CollectionInfo{}, ErrInvalidInput
		}
		seen := map[string]bool{}
		shares := append([]CollectionShare(nil), (*patch.Shares)...)
		for _, share := range shares {
			if !validCollectionID(share.UserID) || seen[share.UserID] {
				return CollectionInfo{}, ErrInvalidInput
			}
			seen[share.UserID] = true
		}
		sort.Slice(shares, func(i, j int) bool { return shares[i].UserID < shares[j].UserID })
		patch.Shares = &shares
	}
	var recipientIDs []string
	if patch.Shares != nil {
		for _, share := range *patch.Shares {
			recipientIDs = append(recipientIDs, share.UserID)
		}
	}
	tx, access, err := s.beginCollectionWrite(ctx, subject, id, recipientIDs)
	if err != nil {
		return CollectionInfo{}, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	result, err := readCollection(protected, tx, access, id, kind, true)
	if err != nil {
		return CollectionInfo{}, err
	}
	if !collectionOwner(access, result) {
		return CollectionInfo{}, ErrForbidden
	}
	if patch.Name != nil {
		result.Name = *patch.Name
	}
	if patch.IsPublic != nil {
		result.IsPublic = *patch.IsPublic
	}
	if patch.IsLocked != nil {
		result.IsLocked = *patch.IsLocked
	}
	if _, err := tx.Exec(protected, `UPDATE items SET name=$2,sort_name=$3,updated_at=clock_timestamp() WHERE id=$1`, id, result.Name, strings.ToLower(result.Name)); err != nil {
		return CollectionInfo{}, err
	}
	if err := syncScannedMetadata(protected, tx, id); err != nil {
		return CollectionInfo{}, err
	}
	if _, err := tx.Exec(protected, `UPDATE media_collections SET is_public=$2,is_locked=$3,updated_at=clock_timestamp() WHERE item_id=$1`, id, result.IsPublic, result.IsLocked); err != nil {
		return CollectionInfo{}, err
	}
	if patch.Shares != nil {
		// Recipient account locks precede touching share rows. User deletion
		// takes the account lock before cascading those same share rows.
		for _, share := range *patch.Shares {
			if share.UserID == result.OwnerID {
				return CollectionInfo{}, ErrInvalidInput
			}
			var target string
			if err := tx.QueryRow(protected, `SELECT id FROM users WHERE id=$1 AND NOT is_disabled FOR SHARE`, share.UserID).Scan(&target); errors.Is(err, pgx.ErrNoRows) {
				return CollectionInfo{}, ErrNotFound
			} else if err != nil {
				return CollectionInfo{}, err
			}
		}
		if _, err := tx.Exec(protected, `DELETE FROM media_collection_shares WHERE collection_id=$1`, id); err != nil {
			return CollectionInfo{}, err
		}
		for _, share := range *patch.Shares {
			if _, err := tx.Exec(protected, `INSERT INTO media_collection_shares (collection_id,user_id,can_edit) VALUES ($1,$2,$3)`, id, share.UserID, share.CanEdit); err != nil {
				return CollectionInfo{}, err
			}
		}
	}
	if err := fillCollectionInfo(protected, tx, access, &result); err != nil {
		return CollectionInfo{}, err
	}
	tx.(*ownedTx).catalogChanges.requireResync()
	if err := commitCollectionWrite(ctx, tx); err != nil {
		return CollectionInfo{}, err
	}
	return result, nil
}

func (s *Store) DeleteCollection(ctx context.Context, subject Subject, id, kind string) error {
	tx, access, err := s.beginCollectionWrite(ctx, subject, id, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	collection, err := readCollection(protected, tx, access, id, kind, true)
	if err != nil {
		return err
	}
	if !collectionOwner(access, collection) {
		return ErrForbidden
	}
	if collection.IsLocked {
		return fmt.Errorf("%w: collection is locked", ErrForbidden)
	}
	// Removing one container never owns the lifetime of another collection or
	// source item. Detach physical children before the items foreign key acts.
	if _, err := tx.Exec(protected, `UPDATE items SET parent_id=NULL,updated_at=clock_timestamp() WHERE parent_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(protected, `DELETE FROM items WHERE id=$1`, id); err != nil {
		return err
	}
	tx.(*ownedTx).catalogChanges.requireResync()
	return commitCollectionWrite(ctx, tx)
}

func (s *Store) AddCollectionItems(ctx context.Context, subject Subject, id, kind string, ids []string) (int, error) {
	if err := validateCollectionIDs(ids, true); err != nil {
		return 0, err
	}
	tx, access, err := s.beginCollectionWrite(ctx, subject, id, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	collection, err := readCollection(protected, tx, access, id, kind, true)
	if err != nil {
		return 0, err
	}
	if err := canEditCollection(protected, tx, access, collection); err != nil {
		return 0, err
	}
	resolved, err := resolveCollectionMembers(protected, tx, access, collection, ids)
	if err != nil {
		return 0, err
	}
	count, err := appendCollectionMembers(protected, tx, collection, resolved)
	if err != nil {
		return 0, err
	}
	if err := touchCollection(protected, tx, id); err != nil {
		return 0, err
	}
	tx.(*ownedTx).catalogChanges.requireResync()
	if err := commitCollectionWrite(ctx, tx); err != nil {
		return 0, err
	}
	return count, nil
}

func touchCollection(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `UPDATE media_collections SET updated_at=clock_timestamp() WHERE item_id=$1`, id)
	return err
}

func (s *Store) PreviewCollectionItems(ctx context.Context, subject Subject, id, kind string, ids []string) (CollectionPreview, error) {
	if err := validateCollectionIDs(ids, true); err != nil {
		return CollectionPreview{}, err
	}
	tx, access, err := s.beginCollectionRead(ctx, subject)
	if err != nil {
		return CollectionPreview{}, err
	}
	defer rollback(tx)
	collection, err := readCollection(ctx, tx, access, id, kind, false)
	if err != nil {
		return CollectionPreview{}, err
	}
	if err := canEditCollection(ctx, tx, access, collection); err != nil {
		return CollectionPreview{}, err
	}
	resolved, err := resolveCollectionMembers(ctx, tx, access, collection, ids)
	if err != nil {
		return CollectionPreview{}, err
	}
	seen, err := collectionMemberSet(ctx, tx, id)
	if err != nil {
		return CollectionPreview{}, err
	}
	result := CollectionPreview{ItemCount: len(resolved)}
	for _, member := range resolved {
		if seen[member] {
			result.ContainsDuplicates = true
		}
		seen[member] = true
	}
	if err := tx.Commit(ctx); err != nil {
		return CollectionPreview{}, err
	}
	return result, nil
}

func collectionMemberSet(ctx context.Context, tx pgx.Tx, id string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT item_id FROM media_collection_entries WHERE collection_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var member string
		if err := rows.Scan(&member); err != nil {
			return nil, err
		}
		seen[member] = true
	}
	return seen, rows.Err()
}

func (s *Store) RemoveCollectionItems(ctx context.Context, subject Subject, id, kind string, ids []string) error {
	if err := validateCollectionIDs(ids, true); err != nil {
		return err
	}
	if kind == PlaylistKind {
		for _, entry := range ids {
			n, err := strconv.ParseInt(entry, 10, 64)
			if err != nil || n <= 0 {
				return ErrInvalidInput
			}
		}
	}
	tx, access, err := s.beginCollectionWrite(ctx, subject, id, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	collection, err := readCollection(protected, tx, access, id, kind, true)
	if err != nil {
		return err
	}
	if err := canEditCollection(protected, tx, access, collection); err != nil {
		return err
	}
	column := "item_id"
	if kind == PlaylistKind {
		column = "id::text"
	}
	var count int
	unique := map[string]bool{}
	for _, entry := range ids {
		unique[entry] = true
	}
	selection := "e.item_id"
	if kind == PlaylistKind {
		selection = "e.id::text"
	}
	if err := tx.QueryRow(protected, `SELECT count(*) FROM media_collection_entries e JOIN items i ON i.id=e.item_id
		WHERE e.collection_id=$1 AND `+selection+`=ANY($2::text[]) AND `+access.ordinarySQL("i"), id, ids).Scan(&count); err != nil {
		return err
	}
	if count != len(unique) {
		return ErrNotFound
	}
	if _, err := tx.Exec(protected, `DELETE FROM media_collection_entries WHERE collection_id=$1 AND `+column+`=ANY($2::text[])`, id, ids); err != nil {
		return err
	}
	if kind == BoxSetKind {
		if _, err := tx.Exec(protected, `UPDATE items SET parent_id=NULL,updated_at=clock_timestamp() WHERE parent_id=$1 AND id=ANY($2::text[]) AND type IN ('Playlist','BoxSet')`, id, ids); err != nil {
			return err
		}
	}
	if err := normalizeCollectionPositions(protected, tx, id); err != nil {
		return err
	}
	if err := touchCollection(protected, tx, id); err != nil {
		return err
	}
	tx.(*ownedTx).catalogChanges.requireResync()
	return commitCollectionWrite(ctx, tx)
}

func normalizeCollectionPositions(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `WITH ordered AS (SELECT id,(row_number() OVER (ORDER BY position,id)-1)::integer AS ordinal FROM media_collection_entries WHERE collection_id=$1)
		UPDATE media_collection_entries e SET position=ordered.ordinal FROM ordered WHERE e.id=ordered.id`, id)
	return err
}

func (s *Store) MoveCollectionEntry(ctx context.Context, subject Subject, id, entryID string, index int) error {
	entry, err := strconv.ParseInt(entryID, 10, 64)
	if err != nil || entry <= 0 || index < 0 || index >= collectionEntryLimit {
		return ErrInvalidInput
	}
	tx, access, err := s.beginCollectionWrite(ctx, subject, id, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	collection, err := readCollection(protected, tx, access, id, PlaylistKind, true)
	if err != nil {
		return err
	}
	if err := canEditCollection(protected, tx, access, collection); err != nil {
		return err
	}
	if err := normalizeCollectionPositions(protected, tx, id); err != nil {
		return err
	}
	// Clients receive an ACL-filtered sequence. Resolve its destination back
	// to a persistent position without exposing or indexing hidden entries.
	rows, err := tx.Query(protected, `SELECT e.id,e.position FROM media_collection_entries e JOIN items i ON i.id=e.item_id
		WHERE e.collection_id=$1::text AND `+access.ordinarySQL("i")+` ORDER BY e.position,e.id`, id)
	if err != nil {
		return err
	}
	old, destination, visibleCount := -1, -1, 0
	for rows.Next() {
		var visibleID int64
		var position int
		if err := rows.Scan(&visibleID, &position); err != nil {
			rows.Close()
			return err
		}
		if visibleID == entry {
			old = position
		}
		if visibleCount == index {
			destination = position
		}
		visibleCount++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if old < 0 {
		return ErrNotFound
	}
	if index >= visibleCount {
		return ErrInvalidInput
	}
	if _, err := tx.Exec(protected, `UPDATE media_collection_entries SET position=CASE WHEN id=$2::bigint THEN $3::integer WHEN $3::integer<$4::integer AND position >= $3::integer AND position<$4::integer THEN position+1 WHEN $3::integer>$4::integer AND position>$4::integer AND position<=$3::integer THEN position-1 ELSE position END WHERE collection_id=$1::text`, id, entry, destination, old); err != nil {
		return err
	}
	if err := touchCollection(protected, tx, id); err != nil {
		return err
	}
	tx.(*ownedTx).catalogChanges.requireResync()
	return commitCollectionWrite(ctx, tx)
}
