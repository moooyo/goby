package library

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

type ProviderSource struct {
	Provider   string    `json:"Provider"`
	ProviderID string    `json:"ProviderId"`
	SourceURL  string    `json:"SourceUrl"`
	Language   string    `json:"Language"`
	FetchedAt  time.Time `json:"FetchedAt"`
	Fields     []string  `json:"Fields"`
}

func mergeOnlineSource(base, online []byte) ([]byte, error) {
	values, err := metadataSourceObject(base)
	if err != nil {
		return nil, err
	}
	source, err := metadataSourceObject(online)
	if err != nil {
		return nil, err
	}
	for key, value := range source {
		values[key] = value
	}
	return json.Marshal(values)
}

func normalizeOnlineMetadata(result providers.Metadata) ([]byte, error) {
	if (result.Provider != "tmdb" && result.Provider != "musicbrainz") || !metadataIdentifier(result.ID) || len(result.SourceURL) > 2048 || len(result.Language) > 32 || len(result.Fields) > 32 {
		return nil, ErrInvalidInput
	}
	values := make(map[string]json.RawMessage, len(result.Fields))
	for key, value := range result.Fields {
		if key == "ParentIndexNumber" {
			continue
		}
		var canonical []byte
		var err error
		if key == "Artists" || key == "AlbumArtists" {
			var names []string
			names, err = metadataStringValues(value)
			if err == nil {
				canonical, err = json.Marshal(names)
			}
		} else {
			canonical, err = normalizeMetadataValue(key, value)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: unsupported provider metadata", ErrInvalidInput)
		}
		values[metadataInternalField(key)] = canonical
	}
	if len(values) == 0 {
		return nil, ErrInvalidInput
	}
	data, err := json.Marshal(values)
	if err != nil || len(data) > metadataEditMaxBytes {
		return nil, ErrInvalidInput
	}
	return data, nil
}

func (s *Store) ProviderSources(ctx context.Context, actor identity.Principal, itemID string) ([]ProviderSource, error) {
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	if _, err := readMetadataRecord(ctx, tx, itemID, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT provider, provider_id, source_url, language, fetched_at, fields FROM item_provider_sources WHERE item_id = $1 ORDER BY provider`, itemID)
	if err != nil {
		return nil, fmt.Errorf("read online provider sources: %w", err)
	}
	result := make([]ProviderSource, 0)
	for rows.Next() {
		var source ProviderSource
		var fields []byte
		if err := rows.Scan(&source.Provider, &source.ProviderID, &source.SourceURL, &source.Language, &source.FetchedAt, &fields); err != nil {
			rows.Close()
			return nil, err
		}
		values, err := metadataSourceObject(fields)
		if err != nil {
			rows.Close()
			return nil, err
		}
		source.Fields = make([]string, 0, len(values))
		for key := range values {
			if key == "ProviderIDs" {
				key = "ProviderIds"
			}
			source.Fields = append(source.Fields, key)
		}
		sort.Strings(source.Fields)
		result = append(result, source)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// ApplyOnlineMetadata updates automatic facts; administrator overrides and
// captured locks are composed last using the same rules as local scanning.
func (s *Store) ApplyOnlineMetadata(ctx context.Context, actor identity.Principal, itemID, revision string, result providers.Metadata) (ItemMetadataDetail, error) {
	if !validMetadataActor(actor) {
		return ItemMetadataDetail{}, ErrForbidden
	}
	return s.applyOnlineMetadata(ctx, &actor, itemID, revision, result)
}

func (s *Store) applyOnlineMetadata(ctx context.Context, actor *identity.Principal, itemID, revision string, result providers.Metadata) (ItemMetadataDetail, error) {
	if !metadataIdentifier(itemID) {
		return ItemMetadataDetail{}, ErrInvalidInput
	}
	online, err := normalizeOnlineMetadata(result)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	defer rollback(tx)
	if actor != nil {
		if err := lockMetadataActor(ctx, tx, *actor); err != nil {
			return ItemMetadataDetail{}, err
		}
	}
	record, err := readMetadataRecord(ctx, tx, itemID, true)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	if result.Type != record.itemType || (actor != nil && revision == "") || (revision != "" && revision != strconv.FormatInt(record.revision, 10)) {
		return ItemMetadataDetail{}, ErrRevisionConflict
	}
	var base, local, music []byte
	if err := tx.QueryRow(ctx, `SELECT CASE WHEN ms.online_type=i.type THEN COALESCE(ms.online_base, ms.automatic) ELSE ms.automatic END, i.local_metadata, ms.music_source FROM item_metadata_state ms JOIN items i ON i.id = ms.item_id WHERE i.id=$1`, itemID).Scan(&base, &local, &music); err != nil {
		return ItemMetadataDetail{}, err
	}
	// Independent identifiers from the accepted baseline remain available.
	baseValues, err := metadataSourceObject(base)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	onlineValues, err := metadataSourceObject(online)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	baseIDs := map[string]string{}
	_ = json.Unmarshal(baseValues["ProviderIDs"], &baseIDs)
	if baseIDs == nil {
		baseIDs = map[string]string{}
	}
	var onlineIDs map[string]string
	_ = json.Unmarshal(onlineValues["ProviderIDs"], &onlineIDs)
	for key, value := range onlineIDs {
		baseIDs[key] = value
	}
	onlineValues["ProviderIDs"], _ = json.Marshal(baseIDs)
	if record.itemType != "Episode" {
		delete(onlineValues, "IndexNumber")
	}
	online, err = json.Marshal(onlineValues)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	mergedLocal, err := mergeAcceptedMusicSource(local, music)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	record.localSource, err = mergeOnlineSource(mergedLocal, online)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	automatic, err := mergeOnlineSource(base, online)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	overrides, err := metadataSourceObject(record.overrides)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	locks, err := metadataSourceObject(record.locked)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	overrides, locks = activeMetadataControls(record.itemType, overrides), activeMetadataControls(record.itemType, locks)
	effective, _, err := composeMetadataValues(automatic, overrides, locks)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	projection, err := buildMetadataProjection(record.localSource, overrides, locks, effective)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	before, err := readAuxiliaryCatalogSnapshot(ctx, tx, []string{itemID})
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	var beforeAlbum scanCatalogSnapshot
	if record.itemType == "MusicAlbum" && record.isFolder {
		beforeAlbum, err = readScanCatalogItem(ctx, tx, itemID)
		if err != nil {
			return ItemMetadataDetail{}, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM item_provider_sources WHERE item_id=$1 AND provider<>$2`, itemID, result.Provider); err != nil {
		return ItemMetadataDetail{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO item_provider_sources(item_id,provider,provider_id,source_url,language,fields) VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(item_id,provider) DO UPDATE SET provider_id=excluded.provider_id,source_url=excluded.source_url,language=excluded.language,fields=excluded.fields,fetched_at=clock_timestamp()`, itemID, result.Provider, result.ID, result.SourceURL, result.Language, online); err != nil {
		return ItemMetadataDetail{}, err
	}
	if err := tx.QueryRow(ctx, `UPDATE item_metadata_state SET online_base=$2,online_source=$3,automatic=$4,effective=$5,online_type=$6,revision=revision+1,updated_at=clock_timestamp() WHERE item_id=$1 RETURNING revision`, itemID, base, online, automatic, projection, record.itemType).Scan(&record.revision); err != nil {
		return ItemMetadataDetail{}, err
	}
	if err := applyEffectiveMetadata(ctx, tx, itemID, effective, projection); err != nil {
		return ItemMetadataDetail{}, err
	}
	record.automatic, record.name = automatic, effective.Name
	change := CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: record.libraryID, ParentID: record.parentID, IsFolder: record.isFolder}
	if item, exists := before.items[itemID]; exists && !item.ordinary {
		change.ParentID = ""
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		return ItemMetadataDetail{}, err
	}
	if err := before.record(ctx, tx, nil); err != nil {
		return ItemMetadataDetail{}, err
	}
	if beforeAlbum.present {
		afterAlbum, err := readScanCatalogItem(ctx, tx, itemID)
		if err != nil {
			return ItemMetadataDetail{}, err
		}
		if err := recordMusicAlbumReferenceChanges(ctx, tx, record.libraryID, itemID, beforeAlbum, afterAlbum); err != nil {
			return ItemMetadataDetail{}, err
		}
	}
	if actor != nil {
		administrator := &catalogAdministrator{actor: *actor, audience: identity.AdministratorNative}
		event := administrator.event(activity.ActionMetadataUpdated, activity.Resource{Kind: activity.ResourceItem, ID: itemID})
		event.Revision = record.revision
		for key := range onlineValues {
			if key == "ProviderIDs" {
				key = "ProviderIds"
			}
			if field, ok := metadataActivityField(key); ok {
				event.ChangedFields = append(event.ChangedFields, field)
			}
		}
		sort.Slice(event.ChangedFields, func(i, j int) bool { return event.ChangedFields[i] < event.ChangedFields[j] })
		if err := activity.RecordOwned(catalogActivityTx{tx: tx}, event); err != nil {
			return ItemMetadataDetail{}, err
		}
		if err := administrator.check(ctx, tx, false); err != nil {
			return ItemMetadataDetail{}, err
		}
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return ItemMetadataDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemMetadataDetail{}, fmt.Errorf("commit online metadata: %w", err)
	}
	return detail, nil
}

func providerQuery(detail ItemMetadataDetail) providers.Query {
	query := providers.Query{Type: detail.Type, Name: detail.Effective.Name, ProviderIDs: detail.Effective.ProviderIDs}
	if detail.Effective.ProductionYear != nil {
		query.Year = *detail.Effective.ProductionYear
	}
	if detail.Effective.IndexNumber != nil {
		if detail.Type == "Season" {
			query.Season = *detail.Effective.IndexNumber
		} else {
			query.Episode = *detail.Effective.IndexNumber
		}
	}
	if detail.Effective.ParentIndexNumber != nil {
		query.Season = *detail.Effective.ParentIndexNumber
	}
	return query
}

// OnlineQuery exposes only the facts required for a remote search.
func OnlineQuery(detail ItemMetadataDetail) providers.Query { return providerQuery(detail) }

func resolveProviderQuery(ctx context.Context, tx pgx.Tx, detail ItemMetadataDetail) (providers.Query, error) {
	query := providerQuery(detail)
	if detail.Type != "Season" && detail.Type != "Episode" {
		return query, nil
	}
	var name string
	var effective []byte
	err := tx.QueryRow(ctx, `SELECT series.name,ms.effective FROM items i
		JOIN items parent ON parent.id=i.parent_id AND parent.library_id=i.library_id
		JOIN items series ON series.id=CASE WHEN i.type='Season' THEN parent.id ELSE parent.parent_id END AND series.library_id=i.library_id AND series.type='Series'
		JOIN item_metadata_state ms ON ms.item_id=series.id WHERE i.id=$1`, detail.ItemID).Scan(&name, &effective)
	if err == pgx.ErrNoRows {
		return query, nil
	}
	if err != nil {
		return providers.Query{}, err
	}
	query.Name = name
	var values MetadataValues
	if effective != nil {
		decoded, err := decodeMetadataValues(effective)
		if err != nil {
			return providers.Query{}, err
		}
		values = decoded
	}
	query.Year = 0
	if values.ProductionYear != nil {
		query.Year = *values.ProductionYear
	}
	if providerIDFor(detail.Effective, "tmdb") == "" {
		if seriesID := providerIDFor(values, "tmdb"); seriesID != "" {
			ids := make(map[string]string, len(query.ProviderIDs)+1)
			for key, value := range query.ProviderIDs {
				ids[key] = value
			}
			ids["Tmdb"] = seriesID + ":" + strconv.Itoa(query.Season)
			if detail.Type == "Episode" {
				ids["Tmdb"] += ":" + strconv.Itoa(query.Episode)
			}
			query.ProviderIDs = ids
		}
	}
	return query, nil
}

func (s *Store) OnlineMetadataQuery(ctx context.Context, actor identity.Principal, itemID string) (providers.Query, error) {
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return providers.Query{}, err
	}
	defer rollback(tx)
	record, err := readMetadataRecord(ctx, tx, itemID, false)
	if err != nil {
		return providers.Query{}, err
	}
	detail, err := metadataDetail(record)
	if err != nil {
		return providers.Query{}, err
	}
	query, err := resolveProviderQuery(ctx, tx, detail)
	if err != nil {
		return providers.Query{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return providers.Query{}, err
	}
	return query, nil
}

func onlineProviderForType(itemType string) string {
	switch itemType {
	case "Movie", "Video", "Series", "Season", "Episode":
		return "tmdb"
	case "MusicAlbum", "MusicArtist", "Audio":
		return "musicbrainz"
	}
	return ""
}

func providerIDFor(values MetadataValues, provider string) string {
	for key, value := range values.ProviderIDs {
		if strings.EqualFold(key, provider) {
			return value
		}
	}
	return ""
}
