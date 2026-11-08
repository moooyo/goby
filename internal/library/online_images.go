package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

const TaskOnlineImagesKey = "images.download"

// OnlineImageProvider exposes only the bounded operations consumed by the
// missing-image worker. It cannot change metadata or select a different item.
type OnlineImageProvider interface {
	Status() []providers.Status
	Images(context.Context, providers.Selection) ([]providers.RemoteImage, error)
	DownloadImage(context.Context, providers.RemoteImage) ([]byte, error)
}

// An explicit managed type also represents a deliberate deletion. Its absence
// of visible images must never be treated as permission to download replacements.
// SQL is only an admission hint; every publication rechecks the full artwork
// revision and effective provider identity under the catalog owner.
const onlineImageCandidateSQL = `i.type IN ('Movie','Series','Season','Episode')
	AND (` + onlineImageKnownIdentitySQL + `)
	AND EXISTS (SELECT 1 FROM unnest(CASE i.type
		WHEN 'Episode' THEN ARRAY['Thumb'] WHEN 'Season' THEN ARRAY['Primary']
		ELSE ARRAY['Primary','Backdrop','Logo'] END) wanted(image_type)
		WHERE NOT EXISTS (SELECT 1 FROM artwork_state a WHERE a.item_id=i.id
			AND wanted.image_type=ANY(a.managed_types))
		AND NOT EXISTS (SELECT 1 FROM item_images im WHERE im.item_id=i.id AND im.image_type=wanted.image_type)
		AND NOT EXISTS (SELECT 1 FROM item_provider_images im WHERE im.item_id=i.id AND im.image_type=wanted.image_type))`

const onlineImageKnownIdentitySQL = `EXISTS (SELECT 1 FROM jsonb_each_text(CASE WHEN jsonb_typeof(
	COALESCE(ms.effective->'ProviderIDs',ms.effective->'ProviderIds'))='object'
	THEN COALESCE(ms.effective->'ProviderIDs',ms.effective->'ProviderIds') ELSE '{}'::jsonb END) provider
	WHERE lower(provider.key)='tmdb' AND btrim(provider.value)<>'')
	OR (i.type IN ('Season','Episode') AND EXISTS (
		SELECT 1 FROM items parent JOIN items series ON
		series.id=CASE WHEN i.type='Season' THEN parent.id ELSE parent.parent_id END
		AND series.library_id=i.library_id AND series.type='Series'
		JOIN item_metadata_state series_ms ON series_ms.item_id=series.id
		WHERE parent.id=i.parent_id AND parent.library_id=i.library_id
		AND EXISTS (SELECT 1 FROM jsonb_each_text(CASE WHEN jsonb_typeof(COALESCE(series_ms.effective->'ProviderIDs',
			series_ms.effective->'ProviderIds'))='object' THEN COALESCE(series_ms.effective->'ProviderIDs',
			series_ms.effective->'ProviderIds') ELSE '{}'::jsonb END) provider
			WHERE lower(provider.key)='tmdb' AND btrim(provider.value)<>'')))`

// SnapshotOnlineImageTaskChildren admits only opted-in video libraries with an
// actual missing image and an existing provider identity. It performs no HTTP.
func SnapshotOnlineImageTaskChildren(tx OwnedTx, runID string) (int64, error) {
	if tx == nil || !validTaskChildID(runID) {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		SELECT md5($1||':'||l.id),$1,l.id,l.name,(row_number() OVER (ORDER BY l.id)-1)::integer
		FROM libraries l WHERE l.collection_type IN ('movies','tvshows','mixed')
		AND l.options->'EnableOnlineImages'='true'::jsonb
		AND EXISTS (SELECT 1 FROM items i JOIN item_metadata_state ms ON ms.item_id=i.id
			WHERE i.library_id=l.id AND `+ordinaryItemSQL("i")+` AND `+onlineImageCandidateSQL+`)`, runID)
	return tag.RowsAffected(), err
}

type onlineImageSnapshot struct {
	libraryID, libraryRevision, metadataRevision, artworkRevision string
	rootID                                                        string
	itemType                                                      string
	supported                                                     bool
	selection                                                     providers.Selection
	missing                                                       map[string]bool
	managed                                                       artwork.ManagedSet
}

func onlineImageTypes(itemType string) []string {
	switch itemType {
	case "Movie", "Series":
		return []string{"Primary", "Backdrop", "Logo"}
	case "Season":
		return []string{"Primary"}
	case "Episode":
		return []string{"Thumb"}
	default:
		return nil
	}
}

func missingOnlineImageTypes(itemType string, collection ArtworkCollection, managed artwork.ManagedSet) map[string]bool {
	missing := make(map[string]bool)
	for _, imageType := range onlineImageTypes(itemType) {
		if !managed.Manages(imageType) {
			missing[imageType] = true
		}
	}
	for _, image := range collection.Items {
		delete(missing, image.ImageType)
	}
	return missing
}

func sameOnlineImageIdentity(first, second providers.Selection) bool {
	return first.Provider == second.Provider && first.Type == second.Type && first.ID == second.ID
}

func readOnlineImageSnapshot(ctx context.Context, tx pgx.Tx, libraryID, itemID string, lock bool) (onlineImageSnapshot, error) {
	return readOnlineImageSnapshotWithPolicy(ctx, tx, libraryID, itemID, lock, false)
}

func readOnlineImageSnapshotWithPolicy(ctx context.Context, tx pgx.Tx, libraryID, itemID string, lock, manual bool) (onlineImageSnapshot, error) {
	var result onlineImageSnapshot
	var enabled, supported bool
	statement := `SELECT revision::text,collection_type IN ('movies','tvshows','mixed'),
		COALESCE(options->'EnableOnlineImages'='true'::jsonb,false) FROM libraries WHERE id=$1`
	if lock {
		statement += " FOR SHARE"
	}
	if err := tx.QueryRow(ctx, statement, libraryID).Scan(&result.libraryRevision, &supported, &enabled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrNotFound
		}
		return result, err
	}
	if !manual && (!supported || !enabled) {
		return result, ErrNotFound
	}
	record, err := readMetadataRecord(ctx, tx, itemID, lock)
	if err != nil {
		return result, err
	}
	if record.libraryID != libraryID {
		return result, ErrNotFound
	}
	if manual {
		result.rootID, err = readManualOnlineImageTarget(ctx, tx, libraryID, itemID, lock)
		if err != nil {
			return result, err
		}
	}
	result.itemType, result.supported = record.itemType, supported && len(onlineImageTypes(record.itemType)) != 0
	if !manual && !result.supported {
		return result, ErrNotFound
	}
	if result.supported {
		detail, err := metadataDetail(record)
		if err != nil {
			return result, err
		}
		// This also reads the parent series in this transaction. Comparing the
		// resolved identity during publication protects season/episode rematches.
		query, err := resolveProviderQuery(ctx, tx, detail)
		if err != nil {
			return result, err
		}
		result.selection = providers.Selection{Provider: "tmdb", Type: query.Type,
			ID: providers.MetadataID("tmdb", query.Type, query.ProviderIDs), Language: query.Language}
	}
	if !manual && result.selection.ID == "" {
		return result, ErrNotFound
	}
	target := ArtworkTarget{ItemID: itemID}
	collection, err := readArtworkCollection(ctx, tx, target)
	if err != nil {
		return result, err
	}
	managed, err := artwork.ReadManagedSet(ctx, tx, artwork.Target{Kind: "item", ID: itemID}, false, false)
	if err != nil {
		return result, err
	}
	result.libraryID, result.metadataRevision, result.artworkRevision = libraryID, strconv.FormatInt(record.revision, 10), collection.Revision
	result.missing = missingOnlineImageTypes(record.itemType, collection, managed)
	result.managed = managed
	return result, nil
}

func (s *Store) onlineImageSnapshot(ctx context.Context, libraryID, itemID string) (onlineImageSnapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return onlineImageSnapshot{}, err
	}
	defer rollback(tx)
	result, err := readOnlineImageSnapshot(ctx, tx, libraryID, itemID, false)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) onlineImageTaskItems(ctx context.Context, libraryID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT i.id FROM items i
		JOIN libraries l ON l.id=i.library_id JOIN item_metadata_state ms ON ms.item_id=i.id
		WHERE l.id=$1 AND l.options->'EnableOnlineImages'='true'::jsonb
		AND l.collection_type IN ('movies','tvshows','mixed') AND `+ordinaryItemSQL("i")+`
		AND `+onlineImageCandidateSQL+` ORDER BY i.id LIMIT $2`, libraryID, providerTaskItemLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			return nil, err
		}
		items = append(items, itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > providerTaskItemLimit {
		return nil, fmt.Errorf("%w: online image work exceeds the 10000-item library limit", ErrInvalidInput)
	}
	return items, nil
}

func onlineImageProviderError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// A provider request timeout is a visible provider failure, not a
		// cancellation of the durable task by its administrator.
		return fmt.Errorf("%w: the image provider request did not complete", providers.ErrUnavailable)
	}
	return err
}

// FillMissingOnlineImages never searches by title, changes provider identities,
// overwrites existing artwork, or touches a media directory. Publication uses a
// live task fence; a persisted run DTO cannot create this authority.
func (s *Store) FillMissingOnlineImages(ctx context.Context, client OnlineImageProvider, libraryID string, fence AnalysisFence, progress func(ProviderProgress)) error {
	if ctx == nil || fence == nil || !validCatalogLibraryIdentifier(libraryID) {
		return ErrInvalidInput
	}
	if !s.Available() {
		return ErrUnavailable
	}
	if client == nil {
		return providers.ErrNotConfigured
	}
	configured := false
	for _, status := range client.Status() {
		configured = configured || status.ID == "tmdb" && status.Configured
	}
	if !configured {
		return providers.ErrNotConfigured
	}
	state := ProviderProgress{}
	if err := reportProviderProgress(ctx, progress, state); err != nil {
		return err
	}
	items, err := s.onlineImageTaskItems(ctx, libraryID)
	if err != nil {
		return err
	}
	for _, itemID := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.fillOnlineImageItem(ctx, client, libraryID, itemID, fence, func() error {
			state.Added++
			return reportProviderProgress(ctx, progress, state)
		}); err != nil {
			return err
		}
		state.Processed++
		if err := reportProviderProgress(ctx, progress, state); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *Store) fillOnlineImageItem(ctx context.Context, client OnlineImageProvider, libraryID, itemID string, fence AnalysisFence, added func() error) error {
	before, err := s.onlineImageSnapshot(ctx, libraryID, itemID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil || len(before.missing) == 0 {
		return err
	}
	images, err := client.Images(ctx, before.selection)
	if err != nil {
		return onlineImageProviderError(ctx, err)
	}
	if len(images) > 200 {
		return providers.ErrInvalidInput
	}
	for _, imageType := range onlineImageTypes(before.selection.Type) {
		if !before.missing[imageType] {
			continue
		}
		var selected *providers.RemoteImage
		for index := range images {
			if images[index].ImageType == imageType && sameOnlineImageIdentity(images[index].Selection, before.selection) {
				selected = &images[index]
				break
			}
		}
		if selected == nil {
			continue
		}
		// Prior writes by this worker change the artwork revision. Capture a
		// fresh snapshot for each download rather than relaxing the CAS.
		current, err := s.onlineImageSnapshot(ctx, libraryID, itemID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if !sameOnlineImageIdentity(current.selection, before.selection) {
			return nil
		}
		if !current.missing[imageType] {
			continue
		}
		written, err := s.downloadMissingOnlineImage(ctx, client, itemID, current, *selected, fence)
		if errors.Is(err, ErrRevisionConflict) || errors.Is(err, ErrNotFound) {
			// An administrator edit or a scan won this race. Leave its state
			// intact; the next scheduled snapshot can reconsider other gaps.
			return nil
		}
		if err != nil {
			return err
		}
		if written {
			if err := added(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) downloadMissingOnlineImage(ctx context.Context, client OnlineImageProvider, itemID string, before onlineImageSnapshot, selected providers.RemoteImage, fence AnalysisFence) (bool, error) {
	return s.downloadMissingOnlineImageGuarded(ctx, client, itemID, before, selected, onlineImagePublicationGuard{fence: fence})
}

// Only the durable worker fence or a native administrator may publish a
// missing image. Manual callers cannot manufacture task execution authority.
type onlineImagePublicationGuard struct {
	fence         AnalysisFence
	administrator *catalogAdministrator
}

func (guard onlineImagePublicationGuard) valid() bool {
	return guard.fence != nil && guard.administrator == nil ||
		guard.fence == nil && guard.administrator != nil && guard.administrator.audience == identity.AdministratorNative
}

func (guard onlineImagePublicationGuard) check(ctx context.Context, tx OwnedTx, raw pgx.Tx, lock bool) error {
	if !guard.valid() {
		return ErrInvalidInput
	}
	if guard.administrator != nil {
		return guard.administrator.check(ctx, raw, lock)
	}
	return guard.fence(tx)
}

func (s *Store) downloadMissingOnlineImageGuarded(ctx context.Context, client OnlineImageProvider, itemID string, before onlineImageSnapshot, selected providers.RemoteImage, guard onlineImagePublicationGuard) (bool, error) {
	if !guard.valid() {
		return false, ErrInvalidInput
	}
	select {
	case artworkMutationSlots <- struct{}{}:
		defer func() { <-artworkMutationSlots }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	data, err := client.DownloadImage(ctx, selected)
	if err != nil {
		return false, onlineImageProviderError(ctx, err)
	}
	if len(data) == 0 || int64(len(data)) > storedImageReadLimit {
		return false, providers.ErrInvalidInput
	}
	info, err := artwork.InspectContext(ctx, bytes.NewReader(data))
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil || info.Width > 16384 || info.Height > 16384 || int64(info.Width)*int64(info.Height) > 26214400 ||
		(info.MIMEType != "image/jpeg" && info.MIMEType != "image/png" && info.MIMEType != "image/gif") {
		return false, fmt.Errorf("%w: the provider image is invalid", providers.ErrInvalidInput)
	}
	written := false
	err = s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		view, ok := tx.(*ownedCallbackTx)
		if !ok || view == nil {
			return ErrInvalidInput
		}
		raw, work := view.catalog, view.ctx
		if err := guard.check(work, tx, raw, true); err != nil {
			return err
		}
		if err := artwork.LockManagedArtwork(work, raw); err != nil {
			return err
		}
		current, err := readOnlineImageSnapshotWithPolicy(work, raw, before.libraryID, itemID, true, guard.administrator != nil)
		if err != nil {
			return err
		}
		if guard.administrator != nil {
			if err := guard.administrator.check(work, raw, false); err != nil {
				return err
			}
		}
		if current.libraryRevision != before.libraryRevision || current.metadataRevision != before.metadataRevision ||
			current.artworkRevision != before.artworkRevision || current.rootID != before.rootID || !sameOnlineImageIdentity(current.selection, before.selection) {
			return ErrRevisionConflict
		}
		if !current.supported || !current.missing[selected.ImageType] || !sameOnlineImageIdentity(selected.Selection, current.selection) {
			return ErrRevisionConflict
		}
		var bytesUsed, entries int64
		if err := tx.QueryRow(`SELECT COALESCE(sum(octet_length(content)),0),count(*) FROM item_provider_images`).Scan(&bytesUsed, &entries); err != nil {
			return err
		}
		if bytesUsed+int64(len(data)) > maxProviderImageCacheBytes || entries >= maxProviderImageCacheEntries {
			return fmt.Errorf("%w: the provider image cache is full; run cache maintenance", ErrUnavailable)
		}
		catalog, err := readAuxiliaryCatalogSnapshot(work, raw, []string{itemID})
		if err != nil {
			return err
		}
		tag, err := tx.Exec(`INSERT INTO item_provider_images
			(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
			VALUES($1,$2,0,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(item_id,image_type,image_index) DO NOTHING`,
			itemID, selected.ImageType, selected.Provider, selected.ID, selected.ImageID, data, info.MIMEType, info.Width, info.Height, info.Tag)
		if err != nil {
			return err
		}
		written = tag.RowsAffected() == 1
		if written {
			var parentID string
			var isFolder bool
			if err := tx.QueryRow(`SELECT COALESCE(parent_id,''),is_folder FROM items WHERE id=$1`, itemID).Scan(&parentID, &isFolder); err != nil {
				return err
			}
			if err := recordCatalogChanges(raw, CatalogChange{Kind: CatalogUpdated, ItemID: itemID,
				LibraryID: before.libraryID, ParentID: parentID, IsFolder: isFolder}); err != nil {
				return err
			}
			if err := catalog.record(work, raw, nil); err != nil {
				return err
			}
		}
		if err := analysisFlush(tx); err != nil {
			return err
		}
		return guard.check(work, tx, raw, false)
	})
	return written && err == nil, err
}
