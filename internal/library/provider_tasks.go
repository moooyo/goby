package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/providers"
	"golang.org/x/text/language"
)

const providerTaskItemLimit = 10000

// ProviderProgress reports committed work. Updated counts removed cache entries during pruning.
type ProviderProgress struct {
	Processed int64
	Added     int64
	Updated   int64
}

type providerTaskItem struct {
	id       string
	itemType string
}

// RefreshOnlineMetadata refreshes known provider identities and accepts only unambiguous searches.
func (s *Store) RefreshOnlineMetadata(ctx context.Context, client *providers.Client, libraryID string, progress func(ProviderProgress)) error {
	state := ProviderProgress{}
	if err := reportProviderProgress(ctx, progress, state); err != nil {
		return err
	}
	if client == nil {
		return providers.ErrNotConfigured
	}
	items, collectionType, err := s.providerTaskSnapshot(ctx, libraryID)
	if err != nil {
		return err
	}
	configured := providerTaskConfiguration(client)
	required := make(map[string]bool)
	for _, item := range items {
		if provider := onlineProviderForType(item.itemType); provider != "" {
			required[provider] = true
		}
	}
	if len(required) == 0 {
		switch collectionType {
		case "movies", "tvshows":
			required["tmdb"] = true
		case "music":
			required["musicbrainz"] = true
		default:
			if !configured["tmdb"] && !configured["musicbrainz"] {
				return providers.ErrNotConfigured
			}
		}
	}
	for provider := range required {
		if !configured[provider] {
			return providers.ErrNotConfigured
		}
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		provider := onlineProviderForType(item.itemType)
		if provider != "" {
			updated, err := s.refreshProviderTaskItem(ctx, client, libraryID, item.id, provider)
			if err != nil {
				return err
			}
			if updated {
				state.Updated++
			}
		}
		state.Processed++
		if err := reportProviderProgress(ctx, progress, state); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *Store) refreshProviderTaskItem(ctx context.Context, client *providers.Client, libraryID, itemID, provider string) (bool, error) {
	detail, _, err := s.readProviderTaskItem(ctx, libraryID, itemID, false)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if onlineProviderForType(detail.Type) != provider {
		return false, nil
	}
	query, err := s.resolvedProviderTaskQuery(ctx, detail)
	if err != nil {
		return false, err
	}
	selection := providers.Selection{Provider: provider, Type: query.Type, Language: query.Language}
	key := provider
	if provider == "musicbrainz" {
		switch query.Type {
		case "MusicAlbum":
			key = "MusicBrainzReleaseGroup"
		case "MusicArtist":
			key = "MusicBrainzArtist"
		case "Audio":
			key = "MusicBrainzRecording"
		}
	}
	for name, value := range query.ProviderIDs {
		if strings.EqualFold(name, key) {
			selection.ID = value
			break
		}
	}
	if selection.ID == "" {
		if strings.TrimSpace(query.Name) == "" {
			return false, nil
		}
		matches, err := client.Search(ctx, provider, query)
		if err != nil {
			return false, err
		}
		candidates := 0
		for _, match := range matches {
			if match.Provider != provider || match.Type != query.Type || !strings.EqualFold(strings.TrimSpace(match.Name), strings.TrimSpace(query.Name)) || (query.Year > 0 && match.Year != query.Year) {
				continue
			}
			selection = match.Selection
			candidates++
		}
		if candidates != 1 {
			return false, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	result, err := client.Lookup(ctx, selection)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if _, err := s.applyOnlineMetadata(ctx, nil, itemID, detail.Revision, result); err != nil {
		return false, err
	}
	return true, nil
}

// DownloadOnlineSubtitles never chooses between multiple candidates for a requested language.
func (s *Store) DownloadOnlineSubtitles(ctx context.Context, client *providers.Client, libraryID string, languages []string, movies, episodes bool, progress func(ProviderProgress)) error {
	state := ProviderProgress{}
	if err := reportProviderProgress(ctx, progress, state); err != nil {
		return err
	}
	// These settings explicitly disable automatic downloads without requiring credentials.
	if len(languages) == 0 || (!movies && !episodes) {
		return nil
	}
	if client == nil || !providerTaskConfiguration(client)["opensubtitles"] {
		return providers.ErrNotConfigured
	}
	if len(languages) > 20 {
		return ErrInvalidInput
	}
	wanted := make([]string, 0, len(languages))
	seen := make(map[string]bool)
	for _, language := range languages {
		normalized, ok := providerTaskLanguage(language)
		if !ok {
			return ErrInvalidInput
		}
		if !seen[normalized] {
			wanted = append(wanted, normalized)
			seen[normalized] = true
		}
	}
	items, _, err := s.providerTaskSnapshot(ctx, libraryID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if item.itemType == "Movie" && movies || item.itemType == "Episode" && episodes {
			err := s.downloadProviderTaskItem(ctx, client, libraryID, item.id, item.itemType, wanted, func() error {
				state.Added++
				return reportProviderProgress(ctx, progress, state)
			})
			if err != nil {
				return err
			}
		}
		state.Processed++
		if err := reportProviderProgress(ctx, progress, state); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (s *Store) downloadProviderTaskItem(ctx context.Context, client *providers.Client, libraryID, itemID, itemType string, languages []string, added func() error) error {
	detail, existing, err := s.readProviderTaskItem(ctx, libraryID, itemID, true)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if detail.Type != itemType || detail.Type != "Movie" && detail.Type != "Episode" {
		return nil
	}
	query, err := s.resolvedProviderTaskQuery(ctx, detail)
	if err != nil {
		return err
	}
	ids := make(map[string]string)
	for key, value := range query.ProviderIDs {
		if strings.EqualFold(key, "Imdb") || strings.EqualFold(key, "Tmdb") {
			if strings.TrimSpace(value) != "" {
				ids[key] = value
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	query.Name = ""
	query.ProviderIDs = ids
	snapshot, err := s.readProviderSubtitleSnapshot(ctx, nil, itemID)
	if err != nil {
		return err
	}
	sourceTag := mediaSnapshotTag(snapshot.primary)
	for _, stream := range snapshot.primary.mediaFile.Item.Media.Streams {
		if stream.CodecType != "subtitle" {
			continue
		}
		if tag, err := language.Parse(stream.Language); err == nil {
			base, _ := tag.Base()
			if normalized, ok := providerTaskLanguage(base.String()); ok {
				existing[normalized] = true
			}
		}
	}
	for _, language := range languages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if existing[language] {
			continue
		}
		matches, err := client.SearchSubtitles(ctx, providers.SubtitleQuery{Query: query, Languages: []string{language}})
		if err != nil {
			return err
		}
		var selected providers.RemoteSubtitle
		candidates := 0
		for _, match := range matches {
			matchLanguage, ok := providerTaskLanguage(match.Language)
			if match.Provider == "opensubtitles" && ok && matchLanguage == language {
				selected = match
				candidates++
			}
		}
		if candidates != 1 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		download, err := client.DownloadSubtitle(ctx, selected)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.registerDownloadedSubtitleForSource(ctx, nil, itemID, sourceTag, download); err != nil {
			return err
		}
		existing[language] = true
		if err := added(); err != nil {
			return err
		}
	}
	return nil
}

func providerTaskConfiguration(client *providers.Client) map[string]bool {
	configured := make(map[string]bool)
	for _, status := range client.Status() {
		configured[status.ID] = status.Configured
	}
	return configured
}

func (s *Store) resolvedProviderTaskQuery(ctx context.Context, detail ItemMetadataDetail) (providers.Query, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return providers.Query{}, err
	}
	defer rollback(tx)
	query, err := resolveProviderQuery(ctx, tx, detail)
	if err != nil {
		return providers.Query{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return providers.Query{}, err
	}
	return query, nil
}

func reportProviderProgress(ctx context.Context, progress func(ProviderProgress), state ProviderProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if progress != nil {
		progress(state)
	}
	return ctx.Err()
}

func (s *Store) providerTaskSnapshot(ctx context.Context, libraryID string) ([]providerTaskItem, string, error) {
	if !metadataIdentifier(libraryID) {
		return nil, "", ErrInvalidInput
	}
	if !s.Available() {
		return nil, "", ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, "", err
	}
	defer rollback(tx)
	var collectionType string
	if err := tx.QueryRow(ctx, `SELECT collection_type FROM libraries WHERE id=$1`, libraryID).Scan(&collectionType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	rows, err := tx.Query(ctx, `SELECT i.id,i.type FROM items i WHERE i.library_id=$1 AND `+ordinaryItemSQL("i")+` ORDER BY i.id LIMIT $2`, libraryID, providerTaskItemLimit+1)
	if err != nil {
		return nil, "", err
	}
	items := make([]providerTaskItem, 0)
	for rows.Next() {
		var item providerTaskItem
		if err := rows.Scan(&item.id, &item.itemType); err != nil {
			rows.Close()
			return nil, "", err
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(items) > providerTaskItemLimit {
		return nil, "", fmt.Errorf("%w: online provider task exceeds the 10000-item library limit", ErrInvalidInput)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, "", err
	}
	return items, collectionType, nil
}

func (s *Store) readProviderTaskItem(ctx context.Context, libraryID, itemID string, includeSubtitles bool) (ItemMetadataDetail, map[string]bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ItemMetadataDetail{}, nil, err
	}
	defer rollback(tx)
	record, err := readMetadataRecord(ctx, tx, itemID, false)
	if err != nil {
		return ItemMetadataDetail{}, nil, err
	}
	if record.libraryID != libraryID {
		return ItemMetadataDetail{}, nil, ErrNotFound
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return ItemMetadataDetail{}, nil, err
	}
	languages := make(map[string]bool)
	if includeSubtitles {
		rows, err := tx.Query(ctx, `SELECT DISTINCT language FROM item_subtitles WHERE item_id=$1 AND active`, itemID)
		if err != nil {
			return ItemMetadataDetail{}, nil, err
		}
		for rows.Next() {
			var language string
			if err := rows.Scan(&language); err != nil {
				rows.Close()
				return ItemMetadataDetail{}, nil, err
			}
			if normalized, ok := providerTaskLanguage(language); ok {
				languages[normalized] = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return ItemMetadataDetail{}, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemMetadataDetail{}, nil, err
	}
	return detail, languages, nil
}

func providerTaskLanguage(value string) (string, bool) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "-"))
	if len(value) != 2 && len(value) != 5 {
		return "", false
	}
	for index, character := range value {
		if len(value) == 5 && index == 2 {
			if character != '-' {
				return "", false
			}
		} else if character < 'a' || character > 'z' {
			return "", false
		}
	}
	if len(value) == 5 && !strings.HasPrefix(value, "zh-") && !strings.HasPrefix(value, "pt-") {
		value = value[:2]
	}
	return value, true
}

// PruneOnlineCache removes only database-owned artwork, never media-root files.
func (s *Store) PruneOnlineCache(ctx context.Context, retentionDays, maxEntries int, progress func(ProviderProgress)) error {
	if retentionDays < 1 || retentionDays > 3650 || maxEntries < 1 || maxEntries > 1000000 {
		return ErrInvalidInput
	}
	state := ProviderProgress{}
	if err := reportProviderProgress(ctx, progress, state); err != nil {
		return err
	}
	if !s.Available() {
		return ErrUnavailable
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		removed, err := s.pruneProviderCacheBatch(ctx, cutoff, maxEntries)
		if err != nil {
			return err
		}
		if removed == 0 {
			return ctx.Err()
		}
		state.Processed += removed
		state.Updated += removed
		if err := reportProviderProgress(ctx, progress, state); err != nil {
			return err
		}
	}
}

func (s *Store) pruneProviderCacheBatch(ctx context.Context, cutoff time.Time, maxEntries int) (int64, error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	// Keep reads on the ownership context, just like other short owned transactions.
	writeCtx := tx.(*ownedTx).ctx
	rows, err := tx.Query(writeCtx, `WITH ranked AS (
		SELECT item_id,image_type,image_index,fetched_at,
		row_number() OVER (ORDER BY fetched_at DESC,item_id,image_type,image_index) AS position
		FROM item_provider_images
	), candidates AS (
		SELECT item_id,image_type,image_index FROM ranked WHERE fetched_at < $1 OR position > $2
		ORDER BY fetched_at,item_id,image_type,image_index LIMIT 1000
	)
	SELECT im.item_id,im.image_type,im.image_index,i.library_id,COALESCE(i.parent_id,''),i.is_folder
	FROM candidates c JOIN item_provider_images im USING(item_id,image_type,image_index)
	JOIN items i ON i.id=im.item_id ORDER BY im.item_id,im.image_type,im.image_index FOR UPDATE OF i,im`, cutoff, maxEntries)
	if err != nil {
		return 0, err
	}
	itemIDs := make([]string, 0)
	imageTypes := make([]string, 0)
	imageIndexes := make([]int32, 0)
	uniqueIDs := make([]string, 0)
	changes := make(map[string]CatalogChange)
	for rows.Next() {
		var change CatalogChange
		var imageType string
		var imageIndex int32
		change.Kind = CatalogUpdated
		if err := rows.Scan(&change.ItemID, &imageType, &imageIndex, &change.LibraryID, &change.ParentID, &change.IsFolder); err != nil {
			rows.Close()
			return 0, err
		}
		itemIDs = append(itemIDs, change.ItemID)
		imageTypes = append(imageTypes, imageType)
		imageIndexes = append(imageIndexes, imageIndex)
		if _, exists := changes[change.ItemID]; !exists {
			uniqueIDs = append(uniqueIDs, change.ItemID)
			changes[change.ItemID] = change
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(itemIDs) == 0 {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	before, err := readAuxiliaryCatalogSnapshot(ctx, tx, uniqueIDs)
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM item_provider_images im USING unnest($1::text[],$2::text[],$3::integer[]) AS selected(item_id,image_type,image_index)
		WHERE im.item_id=selected.item_id AND im.image_type=selected.image_type AND im.image_index=selected.image_index`, itemIDs, imageTypes, imageIndexes)
	if err != nil {
		return 0, err
	}
	for _, itemID := range uniqueIDs {
		change := changes[itemID]
		if item, exists := before.items[itemID]; exists && !item.ordinary {
			change.ParentID = ""
		}
		if err := recordCatalogChanges(tx, change); err != nil {
			return 0, err
		}
	}
	if err := before.record(ctx, tx, nil); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
