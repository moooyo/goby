package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/storagebinding"
)

const (
	maxItemCapabilityIDs          = 1000
	maxItemCapabilityBindingBytes = 8 << 20
)

// ItemCapabilities describes current actor authority and retained catalog
// eligibility. It does not reserve an operation or assert live filesystem state.
type ItemCapabilities struct {
	CanDelete   bool
	CanDownload bool
}

type itemCapabilityFact struct {
	source          indexedMediaSource
	modified        *time.Time
	mediaFacts      []byte
	hasStreams      bool
	ordinary        bool
	independent     bool
	deletionAllowed bool
	deletionPending bool
	publishing      bool
	libraryBusy     bool
	collection      CollectionInfo
}

// ItemCapabilitiesFor uses the credential that would perform Download/Delete,
// independently from a UserId chosen for another account's catalog projection.
// A bounded batch uses one authorization snapshot and at most two data queries.
// Missing, hidden, entity-only and expected-episode IDs retain false values.
func (s *Store) ItemCapabilitiesFor(ctx context.Context, actor identity.Principal, ids []string) (map[string]ItemCapabilities, error) {
	if ctx == nil || len(ids) > maxItemCapabilityIDs {
		return nil, ErrInvalidInput
	}
	result := make(map[string]ItemCapabilities, len(ids))
	selected := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 256 || !utf8.ValidString(id) || strings.ContainsRune(id, '\x00') {
			return nil, ErrInvalidInput
		}
		if _, exists := result[id]; !exists {
			result[id] = ItemCapabilities{}
			selected = append(selected, id)
		}
	}
	if len(selected) == 0 {
		return result, nil
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	// Authorization helpers may evolve to acquire credential locks. This
	// transaction performs no writes without requiring PostgreSQL READ ONLY.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("begin item capability read: %w", err)
	}
	defer rollback(tx)
	access, err := checkFileMutationActor(ctx, tx, actor, false)
	if err != nil {
		return nil, err
	}
	facts, err := readItemCapabilityFacts(ctx, tx, access, selected)
	if err != nil {
		return nil, err
	}
	rootIDs := make([]string, 0)
	rootSeen := make(map[string]bool)
	for _, fact := range facts {
		id := fact.source.root.id
		if fact.ordinary && fact.independent && fact.deletionAllowed && itemCapabilitySourceEligible(fact) && !rootSeen[id] {
			rootSeen[id] = true
			rootIDs = append(rootIDs, id)
		}
	}
	bindings, err := readItemCapabilityBindings(ctx, tx, rootIDs)
	if err != nil {
		return nil, err
	}
	for _, fact := range facts {
		capability := ItemCapabilities{}
		item := fact.source.mediaFile.Item
		if validCollectionKind(item.Type) {
			capability.CanDelete = fact.collection.ID == item.ID && fact.collection.Kind == item.Type &&
				collectionFeatureAllowed(access, item.Type) && collectionOwner(access, fact.collection) && !fact.collection.IsLocked
		} else if itemCapabilitySourceEligible(fact) {
			capability.CanDownload = !fact.deletionPending && !fact.publishing &&
				(actor.IsApplicationKey() || access.policy.EnableContentDownloading && access.policy.AllowsFeature(identity.FeatureDownloads))
			capability.CanDelete = fact.ordinary && fact.independent && fact.deletionAllowed &&
				!fact.deletionPending && !fact.libraryBusy && bindings[fact.source.root.id]
		}
		result[item.ID] = capability
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete item capability read: %w", err)
	}
	return result, nil
}

func readItemCapabilityFacts(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string) ([]itemCapabilityFact, error) {
	// The media projection contains only the indexed source facts used by
	// readIndexedMediaSource. It never transfers decoder indexes or artwork.
	mediaFacts := `jsonb_build_object('ProbeVersion',i.media->'ProbeVersion','FileChangeTimeNs',i.media->'FileChangeTimeNs','Size',i.media->'Size')`
	statement := `WITH RECURSIVE selected AS (
		SELECT i.* FROM items i WHERE i.id=ANY($1::text[]) AND ` + access.directSQL("i") + `
	), ancestors AS (
		SELECT id AS source_id,id,parent_id,library_id,path,ARRAY[id] AS visited FROM selected
		UNION ALL
		SELECT child.source_id,parent.id,parent.parent_id,parent.library_id,parent.path,child.visited||parent.id
		FROM ancestors child JOIN items parent ON parent.id=child.parent_id AND parent.library_id=child.library_id
		WHERE NOT parent.id=ANY(child.visited)
	)
	SELECT i.id,i.library_id,COALESCE(i.parent_id,''),i.type,i.path,i.is_folder,
		CASE WHEN octet_length((` + mediaFacts + `)::text)<=256 THEN ` + mediaFacts + ` END,
		CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN jsonb_array_length(i.media->'Streams')>0 ELSE false END,
		COALESCE(i.relative_path,''),COALESCE(i.file_identity,''),COALESCE(i.file_size,0),i.modified_at,
		COALESCE(r.id,''),COALESCE(r.library_id,''),COALESCE(r.path,''),COALESCE(r.allowed_path,''),COALESCE(r.relative_path,''),
		` + access.ordinarySQL("i") + `,
		NOT EXISTS(SELECT 1 FROM items child WHERE child.parent_id=i.id)
			AND NOT EXISTS(SELECT 1 FROM item_theme_resources theme WHERE theme.owner_item_id=i.id)
			AND NOT EXISTS(SELECT 1 FROM item_extra_resources extra WHERE extra.owner_item_id=i.id),
		$2::boolean OR EXISTS(SELECT 1 FROM ancestors ancestor WHERE ancestor.source_id=i.id
			AND (ancestor.id=ANY($3::text[]) OR ancestor.library_id=ANY($3::text[]) OR ancestor.path=ANY($3::text[]))),
		EXISTS(SELECT 1 FROM media_deletion_operations operation WHERE operation.item_id=i.id),
		EXISTS(SELECT 1 FROM media_operations publication WHERE publication.source_item_id=i.id
			AND publication.publication_phase IN ('prepared','catalog_committed')),
		EXISTS(SELECT 1 FROM scan_jobs scan WHERE scan.library_id=i.library_id AND scan.status IN ('Queued','Running'))
			OR EXISTS(SELECT 1 FROM media_operations publication WHERE publication.source_library_id=i.library_id
				AND publication.publication_phase IN ('prepared','catalog_committed')),
		COALESCE(collection.item_id,''),COALESCE(collection.owner_id,''),COALESCE(collection.kind,''),COALESCE(collection.is_locked,false)
	FROM selected i LEFT JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
	LEFT JOIN media_collections collection ON collection.item_id=i.id ORDER BY i.id`
	rows, err := tx.Query(ctx, statement, ids, access.policy.EnableContentDeletion, access.policy.EnableContentDeletionFromFolders)
	if err != nil {
		return nil, fmt.Errorf("query item capabilities: %w", err)
	}
	defer rows.Close()
	result := make([]itemCapabilityFact, 0, len(ids))
	for rows.Next() {
		var fact itemCapabilityFact
		item, source, root := &fact.source.mediaFile.Item, &fact.source, &fact.source.root
		if err := rows.Scan(&item.ID, &item.LibraryID, &item.ParentID, &item.Type, &item.Path, &item.IsFolder,
			&fact.mediaFacts, &fact.hasStreams, &source.relativePath, &source.identity, &source.mediaFile.Size, &fact.modified,
			&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath,
			&fact.ordinary, &fact.independent, &fact.deletionAllowed, &fact.deletionPending, &fact.publishing, &fact.libraryBusy,
			&fact.collection.ID, &fact.collection.OwnerID, &fact.collection.Kind, &fact.collection.IsLocked); err != nil {
			return nil, fmt.Errorf("read item capability facts: %w", err)
		}
		result = append(result, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish item capability facts: %w", err)
	}
	return result, nil
}

func itemCapabilitySourceEligible(fact itemCapabilityFact) bool {
	source := fact.source
	item := &source.mediaFile.Item
	if item.IsFolder || (item.Type != "Movie" && item.Type != "Episode" && item.Type != "Video" && item.Type != "Audio") ||
		!fact.hasStreams || fact.modified == nil || fact.modified.IsZero() || source.identity == "" || source.mediaFile.Size <= 0 {
		return false
	}
	item.Media = &media.Info{}
	if len(fact.mediaFacts) > 256 || json.Unmarshal(fact.mediaFacts, item.Media) != nil ||
		item.Media.ProbeVersion < media.CurrentProbeVersion || item.Media.FileChangeTimeNs <= 0 {
		return false
	}
	return validateMediaSource(source) == nil
}

func readItemCapabilityBindings(ctx context.Context, tx pgx.Tx, ids []string) (map[string]bool, error) {
	result := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+rootBindingMetadataColumns+`,r.storage_binding IS NOT NULL,
		CASE WHEN octet_length(r.storage_binding::text)<=$2 THEN r.storage_binding::text END,r.bound_at,
		CASE WHEN octet_length(r.bound_by)<=256 THEN r.bound_by ELSE '' END
		FROM library_roots r WHERE r.id=ANY($1::text[]) ORDER BY r.id`, ids, storagebinding.MaxDocumentBytes)
	if err != nil {
		return nil, fmt.Errorf("query item capability bindings: %w", err)
	}
	defer rows.Close()
	retainedBytes := 0
	for rows.Next() {
		var row rootBindingRow
		if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
			&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy); err != nil {
			return nil, fmt.Errorf("read item capability binding: %w", err)
		}
		retainedBytes += len(row.document)
		if retainedBytes > maxItemCapabilityBindingBytes {
			return nil, fmt.Errorf("%w: item capability binding budget exceeded", ErrUnavailable)
		}
		approved, err := row.validate()
		result[row.root.id] = err == nil && approved != nil
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish item capability bindings: %w", err)
	}
	return result, nil
}
