package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providers"
)

var ErrSeriesOrderMappingIncomplete = errors.New("the selected group does not map every catalog and expected episode")

type SeriesOrderUnmappedEpisode struct {
	Kind          string `json:"Kind"`
	ID            string `json:"Id"`
	Name          string `json:"Name"`
	SeasonNumber  *int   `json:"SeasonNumber"`
	EpisodeNumber *int   `json:"EpisodeNumber"`
}

type SeriesOrderCoverage struct {
	PhysicalEpisodeCount int                          `json:"PhysicalEpisodeCount"`
	ExpectedEpisodeCount int                          `json:"ExpectedEpisodeCount"`
	UnmappedCount        int                          `json:"UnmappedCount"`
	Unmapped             []SeriesOrderUnmappedEpisode `json:"Unmapped"`
}

type SeriesDisplayOrder struct {
	SeriesID         string                          `json:"SeriesId"`
	SeriesName       string                          `json:"SeriesName"`
	Revision         string                          `json:"Revision"`
	DisplayOrder     string                          `json:"DisplayOrder"`
	ProviderSeriesID string                          `json:"ProviderSeriesId"`
	GroupID          string                          `json:"GroupId"`
	GroupName        string                          `json:"GroupName"`
	MappingSHA256    string                          `json:"MappingSHA256"`
	Entries          []providers.TVEpisodeGroupEntry `json:"Entries"`
	Coverage         SeriesOrderCoverage             `json:"Coverage"`
	SourceMatches    bool                            `json:"SourceMatches"`
	FetchedAt        *time.Time                      `json:"FetchedAt"`
	UpdatedAt        *time.Time                      `json:"UpdatedAt"`
}

type SeriesDisplayOrderPreview struct {
	Revision      string                          `json:"Revision"`
	DisplayOrder  string                          `json:"DisplayOrder"`
	GroupID       string                          `json:"GroupId"`
	GroupName     string                          `json:"GroupName"`
	MappingSHA256 string                          `json:"MappingSHA256"`
	Entries       []providers.TVEpisodeGroupEntry `json:"Entries"`
	Coverage      SeriesOrderCoverage             `json:"Coverage"`
}

type SeriesOrderMappingError struct{ Coverage SeriesOrderCoverage }

func (err *SeriesOrderMappingError) Error() string { return ErrSeriesOrderMappingIncomplete.Error() }
func (err *SeriesOrderMappingError) Unwrap() error { return ErrSeriesOrderMappingIncomplete }

func seriesDisplayOrderValue(value string) bool {
	return value == "Aired" || value == "Dvd" || value == "Absolute"
}

func validateSeriesOrderMapping(order string, mapping *providers.TVEpisodeGroupDetail) (string, error) {
	if !seriesDisplayOrderValue(order) {
		return "", ErrInvalidInput
	}
	if order == "Aired" {
		if mapping != nil {
			return "", ErrInvalidInput
		}
		return "", nil
	}
	if mapping == nil || mapping.DisplayOrder != order || !episodeRosterText(mapping.ID, 128, false) ||
		!episodeRosterText(mapping.Name, 512, false) || len(mapping.Entries) < 1 || len(mapping.Entries) > providers.MaxTVEpisodeGroupEntries ||
		mapping.EpisodeCount != len(mapping.Entries) {
		return "", ErrInvalidInput
	}
	series, err := strconv.ParseInt(mapping.SeriesID, 10, 64)
	if err != nil || series <= 0 || strconv.FormatInt(series, 10) != mapping.SeriesID {
		return "", ErrInvalidInput
	}
	pairs, ids, ranks := map[[2]int]bool{}, map[string]bool{}, map[[2]int]bool{}
	for index, entry := range mapping.Entries {
		id, err := strconv.ParseInt(entry.ProviderEpisodeID, 10, 64)
		pair, rank := [2]int{entry.SeasonNumber, entry.EpisodeNumber}, [2]int{entry.GroupOrder, entry.EpisodeOrder}
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != entry.ProviderEpisodeID ||
			entry.SeasonNumber < 0 || entry.SeasonNumber > 9999 || entry.EpisodeNumber < 0 || entry.EpisodeNumber > 9999 ||
			entry.GroupOrder < 0 || entry.GroupOrder > 2000 || entry.EpisodeOrder < 0 || entry.EpisodeOrder > 2000 ||
			!episodeRosterText(entry.GroupName, 512, true) || entry.Ordinal != index || pairs[pair] || ids[entry.ProviderEpisodeID] || ranks[rank] {
			return "", ErrInvalidInput
		}
		if index > 0 && (mapping.Entries[index-1].GroupOrder > entry.GroupOrder ||
			mapping.Entries[index-1].GroupOrder == entry.GroupOrder && mapping.Entries[index-1].EpisodeOrder >= entry.EpisodeOrder) {
			return "", ErrInvalidInput
		}
		pairs[pair], ids[entry.ProviderEpisodeID], ranks[rank] = true, true, true
	}
	encoded, err := json.Marshal(mapping)
	if err != nil || len(encoded) > MaxEpisodeRosterBytes {
		return "", ErrInvalidInput
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (s *Store) GetSeriesDisplayOrder(ctx context.Context, actor identity.Principal, seriesID string) (SeriesDisplayOrder, error) {
	if !metadataIdentifier(seriesID) {
		return SeriesDisplayOrder{}, ErrInvalidInput
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	defer rollback(tx)
	detail, err := readSeriesDisplayOrder(ctx, tx, seriesID)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	if err := authorizeMetadataActor(ctx, tx, actor); err != nil {
		return SeriesDisplayOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SeriesDisplayOrder{}, err
	}
	return detail, nil
}

func readSeriesDisplayOrder(ctx context.Context, tx pgx.Tx, seriesID string) (SeriesDisplayOrder, error) {
	if owned, ok := tx.(*ownedTx); ok {
		ctx = owned.ctx
	}
	detail := SeriesDisplayOrder{SeriesID: seriesID, Revision: "0", DisplayOrder: "Aired", SourceMatches: true, Entries: []providers.TVEpisodeGroupEntry{}}
	record, err := readMetadataRecord(ctx, tx, seriesID, false)
	if err != nil {
		return detail, err
	}
	if record.itemType != "Series" || !record.isFolder {
		return detail, ErrNotFound
	}
	detail.SeriesName = record.name
	metadata, err := metadataDetail(record)
	if err != nil {
		return detail, err
	}
	detail.ProviderSeriesID = metadata.Effective.ProviderIDs["Tmdb"]
	var savedProviderSeries string
	err = tx.QueryRow(ctx, `SELECT revision::text,display_order,provider_series_id,provider_group_id,group_name,mapping_sha256,fetched_at,updated_at
		FROM series_display_orders WHERE series_id=$1`, seriesID).
		Scan(&detail.Revision, &detail.DisplayOrder, &savedProviderSeries, &detail.GroupID, &detail.GroupName, &detail.MappingSHA256, &detail.FetchedAt, &detail.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return detail, fmt.Errorf("read series display order: %w", err)
	}
	detail.SourceMatches = detail.DisplayOrder == "Aired" || savedProviderSeries == detail.ProviderSeriesID
	rows, err := tx.Query(ctx, `SELECT provider_episode_id,season_number,episode_number,group_order,episode_order,ordinal,group_name
		FROM series_display_order_entries WHERE series_id=$1 ORDER BY ordinal`, seriesID)
	if err != nil {
		return detail, err
	}
	for rows.Next() {
		var entry providers.TVEpisodeGroupEntry
		if err := rows.Scan(&entry.ProviderEpisodeID, &entry.SeasonNumber, &entry.EpisodeNumber, &entry.GroupOrder, &entry.EpisodeOrder, &entry.Ordinal, &entry.GroupName); err != nil {
			rows.Close()
			return detail, err
		}
		detail.Entries = append(detail.Entries, entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return detail, err
	}
	coverageEntries := detail.Entries
	if !detail.SourceMatches {
		coverageEntries = nil
	}
	detail.Coverage, err = seriesDisplayOrderCoverage(ctx, tx, seriesID, coverageEntries, detail.DisplayOrder == "Aired")
	return detail, err
}

func (s *Store) PreviewSeriesDisplayOrder(ctx context.Context, actor identity.Principal, seriesID string, mapping providers.TVEpisodeGroupDetail) (SeriesDisplayOrderPreview, error) {
	if !metadataIdentifier(seriesID) {
		return SeriesDisplayOrderPreview{}, ErrInvalidInput
	}
	digest, err := validateSeriesOrderMapping(mapping.DisplayOrder, &mapping)
	if err != nil {
		return SeriesDisplayOrderPreview{}, err
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return SeriesDisplayOrderPreview{}, err
	}
	defer rollback(tx)
	detail, err := readSeriesDisplayOrder(ctx, tx, seriesID)
	if err != nil {
		return SeriesDisplayOrderPreview{}, err
	}
	if detail.ProviderSeriesID != mapping.SeriesID {
		return SeriesDisplayOrderPreview{}, ErrRevisionConflict
	}
	coverage, err := seriesDisplayOrderCoverage(ctx, tx, seriesID, mapping.Entries, false)
	if err != nil {
		return SeriesDisplayOrderPreview{}, err
	}
	if err := authorizeMetadataActor(ctx, tx, actor); err != nil {
		return SeriesDisplayOrderPreview{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SeriesDisplayOrderPreview{}, err
	}
	return SeriesDisplayOrderPreview{Revision: detail.Revision, DisplayOrder: mapping.DisplayOrder, GroupID: mapping.ID, GroupName: mapping.Name,
		MappingSHA256: digest, Entries: mapping.Entries, Coverage: coverage}, nil
}

func SeriesOrderMappingSHA256(mapping providers.TVEpisodeGroupDetail) (string, error) {
	return validateSeriesOrderMapping(mapping.DisplayOrder, &mapping)
}

// SetSeriesDisplayOrder changes ordering only. Canonical numbers, physical
// parents, item identities, roster facts, and user playback rows are untouched.
func (s *Store) SetSeriesDisplayOrder(ctx context.Context, actor identity.Principal, seriesID, revision, order string, mapping *providers.TVEpisodeGroupDetail) (SeriesDisplayOrder, error) {
	if !validMetadataActor(actor) || !metadataIdentifier(seriesID) {
		return SeriesDisplayOrder{}, ErrForbidden
	}
	currentRevision, err := episodeRosterRevision(revision)
	if err != nil || currentRevision == math.MaxInt64 {
		return SeriesDisplayOrder{}, ErrInvalidInput
	}
	digest, err := validateSeriesOrderMapping(order, mapping)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	defer rollback(tx)
	if err := lockMetadataActor(ctx, tx, actor); err != nil {
		return SeriesDisplayOrder{}, err
	}
	record, err := readMetadataRecord(ctx, tx, seriesID, true)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	if record.itemType != "Series" || !record.isFolder {
		return SeriesDisplayOrder{}, ErrNotFound
	}
	metadata, err := metadataDetail(record)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	if mapping != nil && metadata.Effective.ProviderIDs["Tmdb"] != mapping.SeriesID {
		return SeriesDisplayOrder{}, ErrRevisionConflict
	}
	var savedRevision int64
	err = tx.QueryRow(ctx, `SELECT revision FROM series_display_orders WHERE series_id=$1 FOR UPDATE`, seriesID).Scan(&savedRevision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return SeriesDisplayOrder{}, err
	}
	if savedRevision != currentRevision {
		return SeriesDisplayOrder{}, ErrRevisionConflict
	}
	entries := []providers.TVEpisodeGroupEntry{}
	if mapping != nil {
		entries = mapping.Entries
	}
	coverage, err := seriesDisplayOrderCoverage(ctx, tx, seriesID, entries, order == "Aired")
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	if coverage.UnmappedCount != 0 {
		return SeriesDisplayOrder{}, &SeriesOrderMappingError{Coverage: coverage}
	}
	providerSeries, groupID, groupName := "", "", ""
	var fetchedAt *time.Time
	if mapping != nil {
		providerSeries, groupID, groupName = mapping.SeriesID, mapping.ID, mapping.Name
		now := time.Now().UTC()
		fetchedAt = &now
	}
	_, err = tx.Exec(ctx, `INSERT INTO series_display_orders(series_id,revision,display_order,provider_series_id,provider_group_id,group_name,mapping_sha256,entry_count,last_edited_by,fetched_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(series_id) DO UPDATE SET
		revision=EXCLUDED.revision,display_order=EXCLUDED.display_order,provider_series_id=EXCLUDED.provider_series_id,
		provider_group_id=EXCLUDED.provider_group_id,group_name=EXCLUDED.group_name,mapping_sha256=EXCLUDED.mapping_sha256,
		entry_count=EXCLUDED.entry_count,last_edited_by=EXCLUDED.last_edited_by,fetched_at=EXCLUDED.fetched_at,updated_at=clock_timestamp()`,
		seriesID, currentRevision+1, order, providerSeries, groupID, groupName, digest, len(entries), actor.User.ID, fetchedAt)
	if err != nil {
		return SeriesDisplayOrder{}, fmt.Errorf("write series display order: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM series_display_order_entries WHERE series_id=$1`, seriesID); err != nil {
		return SeriesDisplayOrder{}, err
	}
	if len(entries) != 0 {
		seasons, episodes, groups, episodeOrders, ordinals := []int32{}, []int32{}, []int32{}, []int32{}, []int32{}
		names, ids := []string{}, []string{}
		for _, entry := range entries {
			seasons, episodes = append(seasons, int32(entry.SeasonNumber)), append(episodes, int32(entry.EpisodeNumber))
			groups, episodeOrders, ordinals = append(groups, int32(entry.GroupOrder)), append(episodeOrders, int32(entry.EpisodeOrder)), append(ordinals, int32(entry.Ordinal))
			names, ids = append(names, entry.GroupName), append(ids, entry.ProviderEpisodeID)
		}
		_, err = tx.Exec(ctx, `INSERT INTO series_display_order_entries(series_id,season_number,episode_number,group_order,episode_order,ordinal,group_name,provider_episode_id)
			SELECT $1,entry.* FROM unnest($2::integer[],$3::integer[],$4::integer[],$5::integer[],$6::integer[],$7::text[],$8::text[])
			AS entry(season_number,episode_number,group_order,episode_order,ordinal,group_name,provider_episode_id)`, seriesID, seasons, episodes, groups, episodeOrders, ordinals, names, ids)
		if err != nil {
			return SeriesDisplayOrder{}, fmt.Errorf("write series episode order mapping: %w", err)
		}
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: seriesID, LibraryID: record.libraryID, ParentID: record.parentID, IsFolder: true, ChildrenAdded: true}); err != nil {
		return SeriesDisplayOrder{}, err
	}
	detail, err := readSeriesDisplayOrder(ctx, tx, seriesID)
	if err != nil {
		return SeriesDisplayOrder{}, err
	}
	if err := authorizeMetadataActor(ctx, tx, actor); err != nil {
		return SeriesDisplayOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SeriesDisplayOrder{}, err
	}
	return detail, nil
}

// Coverage includes explicit expected facts as well as physical episodes. A
// group cannot silently change missing-episode semantics by omitting a pair.
func seriesDisplayOrderCoverage(ctx context.Context, tx pgx.Tx, seriesID string, entries []providers.TVEpisodeGroupEntry, aired bool) (SeriesOrderCoverage, error) {
	seasons, episodes := make([]int32, len(entries)), make([]int32, len(entries))
	for index, entry := range entries {
		seasons[index], episodes[index] = int32(entry.SeasonNumber), int32(entry.EpisodeNumber)
	}
	var coverage SeriesOrderCoverage
	var unmappedJSON []byte
	err := tx.QueryRow(ctx, `WITH RECURSIVE tree AS (
		SELECT i.id,i.library_id,0 AS depth,ARRAY[i.id] AS visited,NULL::integer AS season_number FROM items i WHERE i.id=$1 AND i.type='Series' AND i.is_folder
		UNION ALL SELECT child.id,child.library_id,parent.depth+1,parent.visited||child.id,
		CASE WHEN child.type='Season' THEN child.index_number ELSE parent.season_number END FROM tree parent
		JOIN items child ON child.parent_id=parent.id AND child.library_id=parent.library_id
		WHERE parent.depth<128 AND child.type<>'Series' AND NOT child.id=ANY(parent.visited) AND `+ordinaryItemSQL("child")+`
	), declared AS (SELECT * FROM unnest($2::integer[],$3::integer[]) AS pair(season_number,episode_number)), physical AS (
		SELECT episode.id,episode.name,COALESCE(episode.parent_index_number,tree.season_number) AS season_number,
		episode.index_number AS episode_number FROM tree JOIN items episode ON episode.id=tree.id
		WHERE episode.type='Episode' AND NOT episode.is_folder AND `+ordinaryItemSQL("episode")+`
	), expected AS (
		SELECT e.id,e.name,e.season_number,e.episode_number FROM expected_episodes e JOIN series_episode_rosters roster ON roster.series_id=e.series_id
		WHERE e.series_id=$1 AND e.active AND roster.state='active' AND e.source_key=roster.source_key
	), unmapped AS (
		SELECT 'physical' AS kind,p.* FROM physical p WHERE NOT $4 AND NOT EXISTS(SELECT 1 FROM declared d WHERE d.season_number=p.season_number AND d.episode_number=p.episode_number)
		UNION ALL SELECT 'expected' AS kind,e.* FROM expected e WHERE NOT $4 AND NOT EXISTS(SELECT 1 FROM declared d WHERE d.season_number=e.season_number AND d.episode_number=e.episode_number)
	) SELECT (SELECT count(*) FROM physical),(SELECT count(*) FROM expected),(SELECT count(*) FROM unmapped),
		COALESCE((SELECT jsonb_agg(jsonb_build_object('Kind',kind,'Id',id,'Name',name,'SeasonNumber',season_number,'EpisodeNumber',episode_number) ORDER BY kind,id)
		FROM (SELECT * FROM unmapped ORDER BY kind,id LIMIT 50) limited),'[]'::jsonb)`, seriesID, seasons, episodes, aired).
		Scan(&coverage.PhysicalEpisodeCount, &coverage.ExpectedEpisodeCount, &coverage.UnmappedCount, &unmappedJSON)
	if err != nil {
		return coverage, fmt.Errorf("check series order coverage: %w", err)
	}
	if err := json.Unmarshal(unmappedJSON, &coverage.Unmapped); err != nil {
		return coverage, err
	}
	return coverage, nil
}
