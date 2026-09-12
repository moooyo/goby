package library

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Validate the exact membership that surviving albums will have after DELETE.
// The owned transaction's SQL context protects its session; the original scan
// context remains authoritative for cancellation before any deletion commits.
func checkScanReconciliationMusic(ctx context.Context, tx pgx.Tx, libraryID string, parents map[string]bool, removed map[string]scanReconciliationItem) error {
	if ctx == nil || tx == nil || !validCatalogLibraryIdentifier(libraryID) {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(parents) > scanReconciliationMaxItems || len(removed) > scanReconciliationMaxItems {
		return scanReconciliationBudget()
	}
	excluded := make([]string, 0, len(removed))
	argumentBytes := 0
	for id := range removed {
		if !validCatalogLibraryIdentifier(id) {
			return scanReconciliationUnavailable("removed music member has an invalid identity")
		}
		argumentBytes += 128 + len(id)
		if argumentBytes > scanReconciliationMaxBytes {
			return scanReconciliationBudget()
		}
		excluded = append(excluded, id)
	}
	sort.Strings(excluded)
	frontier := make([]string, 0, len(parents))
	visited := make(map[string]bool)
	for id := range parents {
		if !validCatalogLibraryIdentifier(id) {
			return scanReconciliationUnavailable("pending music parent has an invalid identity")
		}
		if _, absent := removed[id]; absent {
			continue
		}
		argumentBytes += 128 + len(id)
		if argumentBytes > scanReconciliationMaxBytes {
			return scanReconciliationBudget()
		}
		frontier = append(frontier, id)
		visited[id] = true
	}
	sort.Strings(frontier)
	queryCtx := ctx
	if owned, ok := tx.(*ownedTx); ok {
		queryCtx = owned.ctx
	}
	if argumentBytes > scanReconciliationMaxBytes-acceptedMusicAlbumScratchBytes {
		return scanReconciliationBudget()
	}
	budget := &acceptedMusicAlbumBudget{bytes: argumentBytes + acceptedMusicAlbumScratchBytes}
	albums := make(map[string]bool)
	for depth := 0; len(frontier) != 0; depth++ {
		var next []string
		for start := 0; start < len(frontier); start += acceptedMusicAlbumReadBatch {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+acceptedMusicAlbumReadBatch, len(frontier))
			rows, err := tx.Query(queryCtx, `SELECT
				CASE WHEN octet_length(i.id)<=256 THEN i.id ELSE '' END,
				CASE WHEN i.parent_id IS NULL THEN '' WHEN octet_length(i.parent_id)<=256 THEN i.parent_id ELSE '' END,
				i.type='MusicAlbum' AND i.is_folder,
				COALESCE(octet_length(i.parent_id)>256,false)
				FROM items i WHERE i.id=ANY($1::text[]) AND i.library_id=$2
				AND NOT (i.id=ANY($3::text[])) AND `+ordinaryItemSQL("i")+`
				ORDER BY i.id`, frontier[start:end], libraryID, excluded)
			if err != nil {
				return fmt.Errorf("resolve surviving music albums: %w", err)
			}
			ready := true
			for rows.Next() {
				var id, parentID string
				var album, oversized bool
				if err := rows.Scan(&id, &parentID, &album, &oversized); err != nil {
					rows.Close()
					return fmt.Errorf("read surviving music album ancestry: %w", err)
				}
				if !validCatalogLibraryIdentifier(id) || oversized || depth > scanReconciliationMaxDepth ||
					!budget.retain(scanReconciliationItemBytes+len(id)+len(parentID)) {
					ready = false
					continue
				}
				if album {
					albums[id] = true
					continue
				}
				if parentID == "" || visited[parentID] {
					continue
				}
				if !validCatalogLibraryIdentifier(parentID) {
					ready = false
					continue
				}
				if _, absent := removed[parentID]; absent {
					continue
				}
				visited[parentID] = true
				next = append(next, parentID)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return fmt.Errorf("finish surviving music album ancestry: %w", err)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !ready {
				return scanReconciliationUnavailable("surviving music album ancestry is invalid or exceeds its bound")
			}
		}
		frontier = next
	}
	albumIDs := make([]string, 0, len(albums))
	for id := range albums {
		albumIDs = append(albumIDs, id)
	}
	sort.Strings(albumIDs)
	for _, albumID := range albumIDs {
		_, ready, err := readAcceptedMusicAlbumSource(ctx, tx, libraryID, albumID, excluded, budget)
		if err != nil {
			return err
		}
		if !ready {
			return scanReconciliationUnavailable("surviving music album has incomplete or unbounded accepted members")
		}
	}
	return ctx.Err()
}
