package library

import (
	"context"
)

const (
	scanReconciliationPageItems      = 256
	scanReconciliationPageBytes      = 4 << 20
	scanReconciliationInspectedItems = 262144
)

// readScanReconciliationPage excludes accepted identities in PostgreSQL before
// row locking or retaining a page. The cursor is the exact admitted primary key;
// an oversized/invalid identity aborts rather than advancing a truncated cursor.
func readScanReconciliationPage(tx OwnedTx, libraryID, after string, first bool, staging *scanReconciliationStaging) ([]scanReconciliationItem, error) {
	statement, arguments, err := scanReconciliationPageQuery(libraryID, after, first, staging)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]scanReconciliationItem, 0, scanReconciliationPageItems)
	used := 0
	for rows.Next() {
		item, err := scanReconciliationReadItem(rows)
		if err != nil {
			return nil, err
		}
		if item.oversized || !validCatalogLibraryIdentifier(item.id) {
			return nil, scanReconciliationBudget()
		}
		cost := scanReconciliationItemCost(item)
		if len(items) >= scanReconciliationPageItems || cost > scanReconciliationPageBytes-used {
			return nil, scanReconciliationBudget()
		}
		used += cost
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanReconciliationPageQuery(libraryID, after string, first bool, staging *scanReconciliationStaging) (string, []any, error) {
	predicate := `i.library_id=$1 AND ($3::boolean OR i.id>$2)
		AND i.root_id IS NOT NULL AND i.type<>'CollectionFolder'
		AND i.path<>'' AND i.relative_path<>'' AND left(i.path,2)<>'//'
		AND left(i.relative_path,2)<>'//' AND ` + ordinaryItemSQL("i")
	arguments := []any{libraryID, after, first, scanReconciliationPageItems}
	if staging != nil {
		generation, scanID, stagedLibrary := staging.Scope()
		if stagedLibrary != libraryID {
			return "", nil, ErrInvalidInput
		}
		predicate += ` AND NOT EXISTS (SELECT 1 FROM pg_temp.goby_scan_reconciliation_seen seen
			WHERE seen.generation=$5 AND seen.scan_id=$6 AND seen.library_id=$7
			AND seen.item_id=i.id COLLATE "C")`
		arguments = append(arguments, generation, scanID, stagedLibrary)
	}
	return `SELECT ` + scanReconciliationItemColumns + `
		FROM items i LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
		LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
		WHERE ` + predicate + ` ORDER BY i.id LIMIT $4 FOR UPDATE OF i`, arguments, nil
}

func scanReconciliationReadItem(rows OwnedRows) (scanReconciliationItem, error) {
	var item scanReconciliationItem
	var root, parent, themeOwner, extraOwner *string
	err := rows.Scan(&item.id, &item.libraryID, &root, &parent, &item.typeName, &item.path, &item.relative,
		&themeOwner, &extraOwner, &item.isFolder, &item.ordinary, &item.roleValid, &item.visible, &item.oversized)
	if root != nil {
		item.rootID = *root
	}
	if parent != nil {
		item.parentID = *parent
	}
	if themeOwner != nil {
		item.themeOwner = *themeOwner
	}
	if extraOwner != nil {
		item.extraOwner = *extraOwner
	}
	return item, err
}

func scanReconciliationItemCost(item scanReconciliationItem) int {
	return scanReconciliationItemBytes + len(item.id) + len(item.libraryID) + len(item.rootID) +
		len(item.parentID) + len(item.typeName) + len(item.path) + len(item.relative) + len(item.themeOwner) + len(item.extraOwner)
}

// The returned map is owned by the caller and immutable before any filesystem
// worker receives it. An observation worker never retains the staging object
// or an OwnedTx after its caller can roll back.
func scanReconciliationSeen(tx OwnedTx, staging *scanReconciliationStaging, evidence *scanReconciliationEvidence, ids []string) (map[string]bool, error) {
	if len(ids) > scanReconciliationMaxItems {
		return nil, scanReconciliationBudget()
	}
	seen := make(map[string]bool)
	if staging == nil {
		for _, id := range ids {
			if evidence.Seen(id) {
				seen[id] = true
			}
		}
		return seen, nil
	}
	for start := 0; start < len(ids); {
		end, bytes := start, 20
		for end < len(ids) && end-start < 512 {
			cost := len(ids[end]) + 4
			if cost > (128<<10)-bytes {
				break
			}
			bytes += cost
			end++
		}
		if end == start {
			return nil, scanReconciliationBudget()
		}
		part, err := staging.Contains(tx, ids[start:end])
		if err != nil {
			return nil, err
		}
		for id, found := range part {
			if found {
				seen[id] = true
			}
		}
		start = end
	}
	return seen, nil
}

func collectScanReconciliationCandidates(tx OwnedTx, ctx context.Context, libraryID string,
	roots map[string]*rootBindingScanCapture, evidence *scanReconciliationEvidence,
	staging *scanReconciliationStaging, budget *scanReconciliationBudgetState, first []scanReconciliationItem) (map[string]scanReconciliationItem, error) {
	members := make(map[string]scanReconciliationItem)
	page, inspected := first, 0
	for len(page) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(page) > scanReconciliationInspectedItems-inspected {
			return nil, scanReconciliationBudget()
		}
		inspected += len(page)
		ids := make([]string, len(page))
		for index, item := range page {
			ids[index] = item.id
		}
		seen, err := scanReconciliationSeen(tx, staging, evidence, ids)
		if err != nil {
			return nil, err
		}
		observed, err := observeScanReconciliationCandidates(ctx, libraryID, roots, evidence, page, seen)
		if err != nil {
			return nil, err
		}
		for id, item := range observed {
			if err := budget.retain(item); err != nil {
				return nil, err
			}
			members[id] = item
		}
		after := page[len(page)-1].id
		if len(page) < scanReconciliationPageItems {
			break
		}
		page, err = readScanReconciliationPage(tx, libraryID, after, false, staging)
		if err != nil {
			return nil, err
		}
	}
	return members, nil
}
