package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CollectionItems(ctx context.Context, subject Subject, id, kind string, start, limit int, projections ...QueryProjection) (ItemResult, error) {
	if !validCollectionID(id) || !validCollectionKind(kind) || start < 0 || limit < 0 || limit > 1000 {
		return ItemResult{}, ErrInvalidInput
	}
	if limit == 0 {
		limit = 100
	}
	tx, access, err := s.beginCollectionRead(ctx, subject)
	if err != nil {
		return ItemResult{}, err
	}
	defer rollback(tx)
	if _, err := readCollection(ctx, tx, access, id, kind, false); err != nil {
		return ItemResult{}, err
	}
	query := Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID, ParentID: id, StartIndex: start, Limit: limit}
	if len(projections) != 0 {
		query.Projection = projections[0]
	}
	result, _, err := queryCollectionItems(ctx, tx, query, access, false)
	if err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, err
	}
	return result, nil
}

// queryCollectionItems shares the caller's read snapshot and accepts the same
// filters as normal Items queries. A playlist result counts ordered entries,
// including repeated media identities; pagination therefore preserves repeats.
func queryCollectionItems(ctx context.Context, tx pgx.Tx, query Query, access libraryAccess, countOnly bool) (ItemResult, bool, error) {
	if query.ParentID == "" {
		return ItemResult{}, false, nil
	}
	var kind string
	err := tx.QueryRow(ctx, `SELECT kind FROM media_collections WHERE item_id=$1`, query.ParentID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ItemResult{}, false, nil
	}
	if err != nil {
		return ItemResult{}, true, err
	}
	collection, err := readCollection(ctx, tx, access, query.ParentID, kind, false)
	if err != nil {
		return ItemResult{}, true, err
	}
	memberQuery := query
	memberQuery.ParentID = ""
	memberQuery.Recursive = true
	prefix, filter, args := itemQuerySQL(memberQuery, access, "")
	args = append(args, collection.ID)
	containerParameter := fmt.Sprintf("$%d", len(args))
	from := " FROM media_collection_entries e JOIN items i ON i.id=e.item_id WHERE " + filter + " AND e.collection_id=" + containerParameter
	recursiveBoxSet := kind == BoxSetKind && query.Recursive
	if recursiveBoxSet {
		// UNION bounds both corrupt physical cycles and repeated membership.
		// Collection edges remain references; they never rewrite source parents.
		prefix = `WITH RECURSIVE collection_descendants AS (
			SELECT i.id FROM media_collection_entries seed JOIN items i ON i.id=seed.item_id
			WHERE seed.collection_id=` + containerParameter + ` AND ` + access.ordinarySQL("i") + `
			UNION SELECT child.id FROM collection_descendants descendant JOIN items ancestor ON ancestor.id=descendant.id
			JOIN LATERAL (
				SELECT physical.id FROM items physical WHERE physical.parent_id=ancestor.id AND physical.library_id=ancestor.library_id AND ancestor.type NOT IN ('Playlist','BoxSet')
				UNION SELECT nested.item_id FROM media_collection_entries nested WHERE nested.collection_id=ancestor.id
			) relationship ON true JOIN items child ON child.id=relationship.id
			WHERE child.id<>` + containerParameter + ` AND ` + access.ordinarySQL("ancestor") + ` AND ` + access.ordinarySQL("child") + `
		) `
		from = ` FROM (SELECT id AS item_id,0::bigint AS id,0::integer AS position FROM collection_descendants) e JOIN items i ON i.id=e.item_id WHERE ` + filter
	}
	result := ItemResult{Items: []Item{}}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*)"+from, args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, true, err
	}
	if countOnly {
		return result, true, nil
	}
	order := "e.position,e.id"
	if recursiveBoxSet {
		order = "i.sort_name,i.id"
	}
	if strings.TrimSpace(query.SortBy) != "" {
		userOrderParameter := 0
		if itemSortUsesUserData(query.SortBy) {
			args = append(args, query.UserID)
			userOrderParameter = len(args)
		}
		order = itemOrderSQL(query, userOrderParameter) + ", e.position,e.id"
	}
	args = append(args, query.Limit, query.StartIndex)
	rows, err := tx.Query(ctx, prefix+"SELECT "+access.scopeSQL(itemQueryColumns(query))+",e.id::text"+from+" ORDER BY "+access.scopeSQL(order)+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return ItemResult{}, true, err
	}
	for rows.Next() {
		var entryID string
		item, err := scanItem(rows, &entryID)
		if err != nil {
			rows.Close()
			return ItemResult{}, true, err
		}
		item.CanPlay = access.canPlay
		if kind == PlaylistKind {
			item.PlaylistItemID = entryID
		}
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ItemResult{}, true, err
	}
	if err := attachUserData(ctx, tx, query.UserID, result.Items, access); err != nil {
		return ItemResult{}, true, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, true, err
	}
	if err := attachCollectionInfo(ctx, tx, access, result.Items); err != nil {
		return ItemResult{}, true, err
	}
	return result, true, nil
}

// collectionMembershipSQL selects containers that contain requested media.
// Both the container and the matching member retain the current subject ACL.
func collectionMembershipSQL(alias string, listIDs []string, access libraryAccess) string {
	if len(listIDs) == 0 {
		return "true"
	}
	quoted := make([]string, len(listIDs))
	for i, id := range listIDs {
		quoted[i] = policySQLString(id)
	}
	// Playlist folder additions expand to leaves. Resolve current physical
	// ancestors so the album detail query can still find those playlists.
	return `EXISTS (SELECT 1 FROM media_collection_entries membership JOIN items membership_item ON membership_item.id=membership.item_id WHERE membership.collection_id=` + alias + `.id AND ` + access.ordinarySQL("membership_item") + ` AND EXISTS (
		WITH RECURSIVE collection_member_ancestors AS (
			SELECT membership_item.id,membership_item.parent_id,membership_item.library_id
			UNION SELECT member_parent.id,member_parent.parent_id,member_parent.library_id
			FROM items member_parent JOIN collection_member_ancestors descendant ON member_parent.id=descendant.parent_id AND member_parent.library_id=descendant.library_id
			WHERE ` + ordinaryItemSQL("member_parent") + `
		) SELECT 1 FROM collection_member_ancestors ancestor JOIN items requested_member ON requested_member.id=ancestor.id
		WHERE requested_member.id IN (` + strings.Join(quoted, ",") + `) AND (requested_member.id=membership_item.id OR requested_member.is_folder) AND ` + access.ordinarySQL("requested_member") + `))`
}

// attachCollectionInfo leaves the stable scanItem column contract unchanged.
// Call it in the same subject snapshot as the item list or item detail lookup.
func attachCollectionInfo(ctx context.Context, tx pgx.Tx, access libraryAccess, items []Item) error {
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for _, item := range items {
		if !validCollectionKind(item.Type) {
			continue
		}
		if !validCollectionID(item.ID) {
			return ErrInvalidInput
		}
		if !collectionFeatureAllowed(access, item.Type) {
			return ErrNotFound
		}
		if !seen[item.ID] {
			ids = append(ids, item.ID)
			seen[item.ID] = true
		}
	}
	if len(ids) == 0 {
		return attachCollectionFolderInfo(ctx, tx, items)
	}
	rows, err := tx.Query(ctx, `SELECT i.id,i.name,COALESCE(i.parent_id,''),c.owner_id,c.kind,c.media_type,c.is_public,c.is_locked
		FROM items i JOIN media_collections c ON c.item_id=i.id
		WHERE i.id=ANY($1::text[]) AND `+access.itemPolicySQL("i"), ids)
	if err != nil {
		return fmt.Errorf("read collections: %w", err)
	}
	infos := make(map[string]*CollectionInfo, len(ids))
	managedIDs := make([]string, 0, len(ids))
	for rows.Next() {
		info := &CollectionInfo{Shares: []CollectionShare{}}
		if err := rows.Scan(&info.ID, &info.Name, &info.ParentID, &info.OwnerID, &info.Kind, &info.MediaType, &info.IsPublic, &info.IsLocked); err != nil {
			rows.Close()
			return fmt.Errorf("read collections: %w", err)
		}
		infos[info.ID] = info
		if collectionOwner(access, *info) {
			managedIDs = append(managedIDs, info.ID)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read collections: %w", err)
	}
	for _, item := range items {
		if validCollectionKind(item.Type) && (infos[item.ID] == nil || infos[item.ID].Kind != item.Type) {
			return ErrNotFound
		}
	}
	rows, err = tx.Query(ctx, `SELECT e.collection_id,count(*) FROM media_collection_entries e JOIN items i ON i.id=e.item_id
		WHERE e.collection_id=ANY($1::text[]) AND `+access.ordinarySQL("i")+` GROUP BY e.collection_id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			rows.Close()
			return err
		}
		infos[id].ItemCount = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Only owners and administrators may see sharing rosters. Limit the batch
	// itself so public and shared readers never load that management data.
	if len(managedIDs) != 0 {
		rows, err = tx.Query(ctx, `SELECT collection_id,user_id,can_edit FROM media_collection_shares
			WHERE collection_id=ANY($1::text[]) ORDER BY collection_id,user_id`, managedIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			var share CollectionShare
			if err := rows.Scan(&id, &share.UserID, &share.CanEdit); err != nil {
				rows.Close()
				return err
			}
			infos[id].Shares = append(infos[id].Shares, share)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for index := range items {
		item := &items[index]
		if !validCollectionKind(item.Type) {
			continue
		}
		// A repeated playlist entry retains its own user-state projection and
		// position even though the collection metadata was loaded only once.
		info := *infos[item.ID]
		item.Collection = &info
		item.Collection.UserData = item.UserData
		count := info.ItemCount
		item.ChildCount = &count
	}
	return attachCollectionFolderInfo(ctx, tx, items)
}

func attachCollectionFolderInfo(ctx context.Context, tx pgx.Tx, items []Item) error {
	ids := make([]string, 0)
	seen := make(map[string]bool)
	for _, item := range items {
		if item.Type == "CollectionFolder" && !seen[item.LibraryID] {
			ids = append(ids, item.LibraryID)
			seen[item.LibraryID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id,collection_type FROM libraries WHERE id=ANY($1::text[]) AND id<>$2`, ids, collectionLibraryID)
	if err != nil {
		return err
	}
	types := make(map[string]string, len(ids))
	for rows.Next() {
		var id, kind string
		if err := rows.Scan(&id, &kind); err != nil {
			rows.Close()
			return err
		}
		types[id] = kind
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for index := range items {
		item := &items[index]
		if item.Type == "CollectionFolder" {
			kind, exists := types[item.LibraryID]
			if !exists {
				return ErrNotFound
			}
			item.CollectionType = kind
		}
	}
	return nil
}
