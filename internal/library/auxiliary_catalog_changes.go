package library

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

type auxiliaryCatalogItem struct {
	change                             CatalogChange
	ordinary, visible, subject         bool
	themeOwner, extraOwner, properties string
}

type auxiliaryCatalogSnapshot struct {
	items  map[string]auxiliaryCatalogItem
	seeds  []string
	ids    []string
	resync bool
}

// Auxiliary resources have semantic owners, which can be nonfolder Movies.
// They do not belong to ordinary browse containers. Keep ownership separately
// from the parent carried by a catalog notification.
func (item auxiliaryCatalogItem) fact(kind CatalogChangeKind) CatalogChange {
	change := item.change
	change.Kind = kind
	if !item.ordinary {
		change.ParentID = ""
	}
	return change
}

// Hash accepted properties in PostgreSQL so a batch retains bounded digests,
// not an entire media/metadata document for every resource. Private inspection
// timestamps and metadata bookkeeping are deliberately excluded.
var auxiliaryCatalogProperties = `encode(sha256(convert_to(jsonb_build_object(
	'RootId', i.root_id, 'ParentId', i.parent_id, 'Type', i.type, 'IsFolder', i.is_folder,
	'Path', i.path, 'RelativePath', i.relative_path, 'Name', i.name, 'SortName', i.sort_name,
	'Overview', i.overview, 'IndexNumber', i.index_number, 'ParentIndexNumber', i.parent_index_number,
	'Media', i.media, 'FileIdentity', i.file_identity, 'FileSize', i.file_size, 'ModifiedAt', i.modified_at,
	'Metadata', COALESCE(ms.effective, i.local_metadata), 'Entities', ` + itemEntitiesColumn + `,
	'InheritedGenres', CASE WHEN theme.active AND NOT COALESCE(ms.overrides ? 'Genres' OR ms.locked_values ? 'Genres', false)
		AND COALESCE(COALESCE(ms.effective, i.local_metadata)->'Genres', 'null'::jsonb) IN ('null'::jsonb, '[]'::jsonb)
		AND NOT EXISTS(SELECT 1 FROM item_entities credit JOIN catalog_entities entity ON entity.id=credit.entity_id
			WHERE credit.item_id=i.id AND entity.kind='Genre') THEN (
		SELECT jsonb_build_object('Genres', COALESCE(NULLIF(COALESCE(owner_state.effective, owner.local_metadata)->'Genres', 'null'::jsonb), '[]'::jsonb),
			'Entities', (SELECT COALESCE(jsonb_agg(jsonb_build_object('ID',entity.id,'Name',credit.display_name)
				ORDER BY credit.position,entity.id),'[]'::jsonb)
				FROM item_entities credit JOIN catalog_entities entity ON entity.id=credit.entity_id
				WHERE credit.item_id=owner.id AND entity.kind='Genre' AND credit.credit_group=0 AND credit.credit_type=''))
		FROM items owner LEFT JOIN item_metadata_state owner_state ON owner_state.item_id=owner.id
		WHERE owner.id=theme.owner_item_id AND owner.library_id=i.library_id AND ` + ordinaryItemSQL("owner") + `) END,
	'ThemeOwner', CASE WHEN theme.active THEN theme.owner_item_id END,
	'ThemeKind', CASE WHEN theme.active THEN theme.kind END,
	'ExtraOwner', CASE WHEN extra.active THEN extra.owner_item_id END,
	'ExtraKind', CASE WHEN extra.active THEN extra.kind END)::text, 'UTF8')), 'hex')`

// Capture both sides of affected relationships before any visibility or owner
// mutation. The same transaction owns all observations. Historical owners and
// parents remain in ids for the after snapshot even when a relationship moves.
func readAuxiliaryCatalogSnapshot(ctx context.Context, tx pgx.Tx, ids []string) (auxiliaryCatalogSnapshot, error) {
	return readAuxiliaryCatalogRows(ctx, tx, ids, nil)
}

// Ordinary owners without active auxiliary children have no dependent resource
// visibility to reconcile. Avoid duplicating their normal item snapshot work.
func readAuxiliaryChildCatalogSnapshot(ctx context.Context, tx pgx.Tx, ownerID string) (auxiliaryCatalogSnapshot, error) {
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM item_theme_resources WHERE active AND owner_item_id=$1)
		OR EXISTS(SELECT 1 FROM item_extra_resources WHERE active AND owner_item_id=$1)`, ownerID).Scan(&active)
	if err != nil {
		return auxiliaryCatalogSnapshot{}, err
	}
	if !active {
		return auxiliaryCatalogSnapshot{}, nil
	}
	return readAuxiliaryCatalogSnapshot(ctx, tx, []string{ownerID})
}

func readAuxiliaryCatalogRows(ctx context.Context, tx pgx.Tx, ids, retained []string) (auxiliaryCatalogSnapshot, error) {
	// ownedTx.Query is the embedded pgx method. Keep its rows on the bounded
	// ownership context so request cancellation cannot close the lease session.
	if owned, ok := tx.(*ownedTx); ok {
		ctx = owned.ctx
	}
	snapshot := auxiliaryCatalogSnapshot{items: make(map[string]auxiliaryCatalogItem)}
	seeds := make(map[string]bool)
	for _, id := range ids {
		if !validCatalogLibraryIdentifier(id) || len(seeds) >= maxCatalogChanges && !seeds[id] {
			return auxiliaryCatalogSnapshot{resync: true}, nil
		}
		seeds[id] = true
	}
	for id := range seeds {
		snapshot.ids = append(snapshot.ids, id)
	}
	if len(snapshot.ids) == 0 {
		return snapshot, nil
	}
	sort.Strings(snapshot.ids)
	snapshot.seeds = append([]string{}, snapshot.ids...)
	rows, err := tx.Query(ctx, `WITH seeds AS (SELECT unnest($1::text[]) id), resources AS (
		SELECT id FROM seeds UNION SELECT resource_item_id FROM item_theme_resources
		WHERE active AND owner_item_id IN (SELECT id FROM seeds)
		UNION SELECT resource_item_id FROM item_extra_resources
		WHERE active AND owner_item_id IN (SELECT id FROM seeds)
	), related AS (
		SELECT id FROM resources UNION SELECT owner_item_id FROM item_theme_resources
		WHERE resource_item_id IN (SELECT id FROM resources)
		UNION SELECT owner_item_id FROM item_extra_resources WHERE resource_item_id IN (SELECT id FROM resources)
	), affected AS (
		SELECT id FROM related UNION SELECT parent_id FROM items WHERE id IN (SELECT id FROM resources) AND parent_id IS NOT NULL
		UNION SELECT unnest($3::text[])
	), bounded AS (SELECT id FROM items WHERE id IN (SELECT id FROM affected) ORDER BY id LIMIT $2)
	SELECT i.id, i.library_id, COALESCE(i.parent_id, ''), i.is_folder, i.type='CollectionFolder',
		`+ordinaryItemSQL("i")+`, `+directItemSQL("i")+`,
		CASE WHEN theme.active AND `+database.ThemeResourceItemSQL("i", true)+` THEN theme.owner_item_id ELSE '' END,
		CASE WHEN extra.active AND `+database.ExtraResourceItemSQL("i", true)+` THEN extra.owner_item_id ELSE '' END,
		`+auxiliaryCatalogProperties+`, EXISTS(SELECT 1 FROM resources WHERE resources.id=i.id)
	FROM bounded JOIN items i ON i.id=bounded.id
	LEFT JOIN item_metadata_state ms ON ms.item_id=i.id
	LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
	LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
	ORDER BY i.id`, snapshot.ids, maxCatalogChanges+1, retained)
	if err != nil {
		return auxiliaryCatalogSnapshot{}, fmt.Errorf("read auxiliary catalog notification snapshot: %w", err)
	}
	defer rows.Close()
	bytes := 0
	for rows.Next() {
		var item auxiliaryCatalogItem
		if err := rows.Scan(&item.change.ItemID, &item.change.LibraryID, &item.change.ParentID,
			&item.change.IsFolder, &item.change.IsCollectionFolder, &item.ordinary, &item.visible,
			&item.themeOwner, &item.extraOwner, &item.properties, &item.subject); err != nil {
			return auxiliaryCatalogSnapshot{}, err
		}
		bytes += 160 + len(item.change.ItemID) + len(item.change.LibraryID) + len(item.change.ParentID) +
			len(item.themeOwner) + len(item.extraOwner) + len(item.properties)
		if len(snapshot.items) >= maxCatalogChanges || bytes > maxCatalogChangeBytes {
			return auxiliaryCatalogSnapshot{resync: true}, nil
		}
		snapshot.items[item.change.ItemID] = item
		seeds[item.change.ItemID] = true
	}
	if err := rows.Err(); err != nil {
		return auxiliaryCatalogSnapshot{}, err
	}
	snapshot.ids = snapshot.ids[:0]
	for id := range seeds {
		snapshot.ids = append(snapshot.ids, id)
	}
	sort.Strings(snapshot.ids)
	return snapshot, nil
}

func (before auxiliaryCatalogSnapshot) record(ctx context.Context, tx pgx.Tx, forcedIDs []string) error {
	if before.resync {
		return mergeAuxiliaryCatalogChanges(tx, nil, true)
	}
	if len(before.seeds) == 0 {
		return nil
	}
	after, err := readAuxiliaryCatalogRows(ctx, tx, before.seeds, before.ids)
	if err != nil {
		return err
	}
	if after.resync {
		return mergeAuxiliaryCatalogChanges(tx, nil, true)
	}
	forced := make(map[string]bool, len(forcedIDs))
	for _, id := range forcedIDs {
		forced[id] = true
	}
	ids := make(map[string]bool, len(before.items)+len(after.items))
	for id := range before.items {
		ids[id] = true
	}
	for id := range after.items {
		ids[id] = true
	}
	changes := make(map[string]CatalogChange)
	owners := make(map[string]bool)
	themeOwners := make(map[string]bool)
	containersAdded, containersRemoved := make(map[string]bool), make(map[string]bool)
	for id := range ids {
		old, current := before.items[id], after.items[id]
		if !old.subject && !current.subject {
			continue
		}
		changed := false
		switch {
		case !old.visible && current.visible:
			changes[id], changed = current.fact(CatalogAdded), true
		case old.visible && !current.visible:
			changes[id], changed = old.fact(CatalogRemoved), true
		case old.visible && current.visible && (!old.ordinary || !current.ordinary) &&
			(old.properties != current.properties || old.ordinary != current.ordinary || forced[id]):
			changes[id], changed = current.fact(CatalogUpdated), true
			if old.ordinary {
				containersRemoved[old.change.ParentID] = true
			}
			if current.ordinary {
				containersAdded[current.change.ParentID] = true
			}
		}
		if !changed {
			continue
		}
		for _, item := range []auxiliaryCatalogItem{old, current} {
			if item.visible && item.themeOwner != "" {
				owners[item.themeOwner], themeOwners[item.themeOwner] = true, true
			}
			if item.visible && item.extraOwner != "" {
				owners[item.extraOwner] = true
			}
		}
	}
	for id := range owners {
		item := after.items[id]
		if item.visible {
			if _, exists := changes[id]; !exists {
				changes[id] = item.fact(CatalogUpdated)
			}
		}
	}
	for _, entry := range []struct {
		ids   map[string]bool
		added bool
	}{{containersAdded, true}, {containersRemoved, false}} {
		for id := range entry.ids {
			if id == "" {
				continue
			}
			item := after.items[id]
			if !item.visible {
				continue
			}
			if !item.change.IsFolder {
				return mergeAuxiliaryCatalogChanges(tx, nil, true)
			}
			change, exists := changes[id]
			if !exists {
				change = item.fact(CatalogUpdated)
			}
			if change.Kind == CatalogUpdated {
				if entry.added {
					change.ChildrenAdded = true
				} else {
					change.ChildrenRemoved = true
				}
			}
			changes[id] = change
		}
	}
	// Theme queries can inherit from a changed owner. Invalidate visible
	// descendants as well, with a hard bound instead of a partial population.
	if len(themeOwners) != 0 {
		queryCtx := ctx
		if owned, ok := tx.(*ownedTx); ok {
			queryCtx = owned.ctx
		}
		ownerIDs := make([]string, 0, len(themeOwners))
		for id := range themeOwners {
			ownerIDs = append(ownerIDs, id)
		}
		rows, err := tx.Query(queryCtx, `WITH RECURSIVE descendants AS (
			SELECT i.id, i.library_id, ARRAY[i.id] seen, 0 depth FROM items i WHERE i.id=ANY($1::text[])
			UNION ALL SELECT child.id, child.library_id, parent.seen||child.id, parent.depth+1
			FROM descendants parent JOIN items child ON child.parent_id=parent.id AND child.library_id=parent.library_id
			WHERE parent.depth<$2 AND NOT child.id=ANY(parent.seen) AND `+directItemSQL("child")+`
		) SELECT DISTINCT i.id, i.library_id, COALESCE(i.parent_id,''), i.is_folder, i.type='CollectionFolder',
			`+ordinaryItemSQL("i")+`, d.depth=$2 FROM descendants d JOIN items i ON i.id=d.id
			WHERE `+directItemSQL("i")+` ORDER BY i.id LIMIT $3`, ownerIDs, MaxThemeAncestorDepth, maxCatalogChanges+1)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item auxiliaryCatalogItem
			var boundary bool
			if err := rows.Scan(&item.change.ItemID, &item.change.LibraryID, &item.change.ParentID,
				&item.change.IsFolder, &item.change.IsCollectionFolder, &item.ordinary, &boundary); err != nil {
				rows.Close()
				return err
			}
			if boundary || len(changes) >= maxCatalogChanges && changes[item.change.ItemID].ItemID == "" {
				rows.Close()
				return mergeAuxiliaryCatalogChanges(tx, nil, true)
			}
			if _, exists := changes[item.change.ItemID]; !exists {
				changes[item.change.ItemID] = item.fact(CatalogUpdated)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	ordered := make([]CatalogChange, 0, len(changes))
	for _, change := range changes {
		ordered = append(ordered, change)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ItemID < ordered[j].ItemID })
	return mergeAuxiliaryCatalogChanges(tx, ordered, false)
}

// Ordinary scan facts may already describe an affected owner. Merge only
// compatible producer facts before handing the completed batch to the strict
// transport validator. Conflicting identities still require resynchronization.
func mergeAuxiliaryCatalogChanges(tx pgx.Tx, changes []CatalogChange, resync bool) error {
	owned, ok := tx.(*ownedTx)
	if !ok || owned == nil || owned.finished {
		return fmt.Errorf("%w: auxiliary notifications require an active owned transaction", ErrInvalidInput)
	}
	if resync || owned.catalogChanges.resync {
		owned.catalogChanges.requireResync()
		return nil
	}
	merged := make(map[string]CatalogChange)
	for _, batch := range [][]CatalogChange{owned.catalogChanges.changes, changes} {
		for _, change := range batch {
			if !validCatalogChange(change) {
				owned.catalogChanges.requireResync()
				return nil
			}
			old, exists := merged[change.ItemID]
			if !exists && len(merged) >= maxCatalogChanges {
				owned.catalogChanges.requireResync()
				return nil
			}
			if exists {
				if old.LibraryID != change.LibraryID || old.ParentID != change.ParentID || old.IsFolder != change.IsFolder ||
					old.IsCollectionFolder != change.IsCollectionFolder || old.Kind != change.Kind && !(old.Kind == CatalogAdded && change.Kind == CatalogUpdated) ||
					old.PreviousParentID != "" && change.PreviousParentID != "" && old.PreviousParentID != change.PreviousParentID {
					owned.catalogChanges.requireResync()
					return nil
				}
				if old.Kind == CatalogUpdated {
					old.ChildrenAdded = old.ChildrenAdded || change.ChildrenAdded
					old.ChildrenRemoved = old.ChildrenRemoved || change.ChildrenRemoved
					if old.PreviousParentID == "" {
						old.PreviousParentID = change.PreviousParentID
					}
				}
				change = old
			}
			merged[change.ItemID] = change
		}
	}
	ordered := make([]CatalogChange, 0, len(merged))
	for _, change := range merged {
		ordered = append(ordered, change)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ItemID < ordered[j].ItemID })
	owned.catalogChanges = catalogChangeBatch{}
	return recordCatalogChanges(tx, ordered...)
}
