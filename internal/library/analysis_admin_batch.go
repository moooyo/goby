package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

type analysisAdminProjection struct {
	source       AnalysisSource
	item         AnalysisItem
	record       analysisDetectionRecord
	hasDetection bool
	current      bool
	allowEmpty   bool
}

type analysisAdminSourceScanner struct {
	row        rowScanner
	additional []any
}

func (scanner analysisAdminSourceScanner) Scan(values ...any) error {
	return scanner.row.Scan(append(values, scanner.additional...)...)
}

// The page has already been selected in this repeatable-read transaction. Read
// its source, marker and audit facts together, then replay the selected order.
// None of these indexed observations opens a physical media source.
func readAnalysisAdminItems(ctx context.Context, tx pgx.Tx, ids []string) ([]AnalysisItem, error) {
	result := make([]AnalysisItem, 0, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	access := unrestrictedLibraryAccess()
	rows, err := tx.Query(ctx, `SELECT `+analysisSourceColumns+`,i.name,l.collection_type,
		CASE WHEN i.type IN ('Movie','Episode') THEN i.media END,COALESCE(manual.source_revision,''),manual.start_ticks,manual.end_ticks,COALESCE(manual.provenance,''),
		COALESCE(decision.source_revision,''),COALESCE(decision.rejected,false),d.item_id IS NOT NULL,
		COALESCE(d.revision,0),COALESCE(d.source_revision,''),COALESCE(d.profile_fingerprint,''),
		COALESCE(d.profile_revision,0),COALESCE(d.publication_epoch,0),COALESCE(d.cohort_revision,''),
		COALESCE(d.status,''),d.result,d.start_ticks,d.end_ticks,COALESCE(d.auto_published,false),COALESCE(d.updated_at,'epoch'::timestamptz)
		FROM items i `+analysisSourceJoins+` JOIN libraries l ON l.id=i.library_id
		LEFT JOIN analysis_detections d ON d.item_id=i.id
		WHERE i.id=ANY($1::text[]) AND `+analysisPhysicalSQL+` AND `+access.directSQL("i"), ids)
	if err != nil {
		return nil, err
	}
	projections := make(map[string]*analysisAdminProjection, len(ids))
	for rows.Next() {
		projection := &analysisAdminProjection{}
		var name, kind, manualSource, provenance, decisionSource string
		var raw []byte
		var start, end *int64
		var suppressed bool
		record := &projection.record
		source, err := scanAnalysisSource(analysisAdminSourceScanner{row: rows, additional: []any{
			&name, &kind, &raw, &manualSource, &start, &end, &provenance, &decisionSource, &suppressed, &projection.hasDetection,
			&record.revision, &record.proof.source, &record.proof.profile, &record.proof.revision, &record.proof.epoch, &record.proof.cohort,
			&record.status, &record.raw, &record.start, &record.end, &record.proof.active, &record.updated,
		}})
		if err != nil {
			rows.Close()
			return nil, err
		}
		if !analysisOpaque(source.ItemID, 128) || source.LibraryID == CollectionsLibraryID || kind != "movies" && kind != "tvshows" && kind != "mixed" {
			rows.Close()
			return nil, ErrInvalidInput
		}
		projection.source = source
		projection.item = AnalysisItem{ID: source.ItemID, Name: name, Type: source.ItemType, LibraryID: source.LibraryID,
			MediaSourceID: source.MediaSourceID, SourceRevision: source.SourceRevision, Previews: []AnalysisPreviewStatus{}}
		detection := &projection.item.Detection
		*detection = AnalysisDetection{ItemID: source.ItemID, Revision: source.DecisionRevision, ManualRevision: source.ManualRevision,
			SourceRevision: source.SourceRevision, Status: "not_analyzed", Reasons: []string{"not_analyzed"},
			Suppressed: suppressed && decisionSource == source.SourceRevision}
		if source.ItemType == "Movie" || source.ItemType == "Episode" {
			var info media.Info
			if json.Unmarshal(raw, &info) != nil || info.DurationTicks <= 0 || len(info.Streams) == 0 {
				rows.Close()
				return nil, fmt.Errorf("%w: a current indexed media source is required", ErrUnavailable)
			}
			detection.Effective = ExplicitChapterIntro(&info)
			if start != nil && end != nil {
				interval := &IntroInterval{StartTicks: *start, EndTicks: *end, Provenance: provenance}
				if manualSource == source.SourceRevision && validIntroInterval(interval, info.DurationTicks) {
					detection.Effective = interval
				}
			}
		}
		projections[source.ItemID] = projection
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		projection := projections[id]
		if projection == nil {
			return nil, ErrNotFound
		}
		if !projection.hasDetection {
			continue
		}
		decisionRevision, err := strconv.ParseInt(projection.source.DecisionRevision, 10, 64)
		if err != nil {
			return nil, err
		}
		projection.current, projection.allowEmpty, err = projectAnalysisDetectionRecord(&projection.item.Detection, decisionRevision, projection.record)
		if err != nil {
			return nil, err
		}
	}
	var revision, epoch, maxBytes int64
	settingsErr := tx.QueryRow(ctx, `SELECT revision,publication_epoch,max_source_bytes FROM analysis_settings WHERE id=1`).Scan(&revision, &epoch, &maxBytes)
	if settingsErr != nil && !errors.Is(settingsErr, pgx.ErrNoRows) {
		return nil, settingsErr
	}
	current := make([]*analysisAdminProjection, 0, len(ids))
	for _, id := range ids {
		projection := projections[id]
		if !projection.current {
			continue
		}
		if settingsErr != nil {
			return nil, settingsErr
		}
		stale := analysisDetectionStaleReason(projection.source, projection.record.proof, revision, epoch)
		if stale != "" {
			projection.item.Detection.Status = "stale"
			projection.item.Detection.Reasons = append(projection.item.Detection.Reasons, stale)
		} else {
			current = append(current, projection)
		}
	}
	if err := readAnalysisAdminReferences(ctx, tx, current, maxBytes); err != nil {
		return nil, err
	}
	if settingsErr == nil {
		if err := readAnalysisAdminPreviews(ctx, tx, ids, projections, revision, epoch); err != nil {
			return nil, err
		}
	}
	for _, id := range ids {
		// Only detail and playback readers validate supporting bytes. Inventory
		// cannot promote indexed detection evidence to an effective interval.
		item := projections[id].item
		if item.Detection.Effective != nil && item.Detection.Effective.Provenance == "Detected" {
			item.Detection.Effective = nil
		}
		result = append(result, item)
	}
	return result, nil
}

type analysisAdminSeason struct {
	libraryID string
	seasonID  string
}

// Read support for all current results at once, then read each distinct season
// population once. The per-season bound and cohort hash match the detail path.
func readAnalysisAdminReferences(ctx context.Context, tx pgx.Tx, projections []*analysisAdminProjection, maxBytes int64) error {
	if len(projections) == 0 {
		return nil
	}
	ids := make([]string, len(projections))
	for index, projection := range projections {
		ids[index] = projection.source.ItemID
	}
	access := unrestrictedLibraryAccess()
	rows, err := tx.Query(ctx, `SELECT d.item_id,d.source_revision,d.hierarchy_revision,
		CASE WHEN i.id IS NULL OR NOT (`+access.directSQL("i")+`) THEN '' ELSE `+introSourceRevisionSQL+` END,
		CASE WHEN i.id IS NULL THEN '' ELSE `+analysisHierarchyRevisionSQL+` END
		FROM unnest($1::text[]) target(item_id) CROSS JOIN LATERAL (
			SELECT item_id,source_item_id,source_revision,hierarchy_revision FROM analysis_detection_sources
			WHERE item_id=target.item_id LIMIT 33
		) d LEFT JOIN items i ON i.id=d.source_item_id
		LEFT JOIN items p ON p.id=i.parent_id AND p.library_id=i.library_id
		LEFT JOIN items s ON s.id=p.parent_id AND s.library_id=i.library_id`, ids)
	if err != nil {
		return err
	}
	type supportFact struct {
		count   int
		invalid bool
	}
	support := make(map[string]supportFact, len(ids))
	for rows.Next() {
		var id, expected, hierarchy, now, currentHierarchy string
		if err := rows.Scan(&id, &expected, &hierarchy, &now, &currentHierarchy); err != nil {
			rows.Close()
			return err
		}
		fact := support[id]
		fact.count++
		fact.invalid = fact.invalid || expected != now || hierarchy != currentHierarchy
		support[id] = fact
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	seasons := map[analysisAdminSeason]bool{}
	neededCohorts := map[string]bool{}
	var seasonKeys []analysisAdminSeason
	for _, projection := range projections {
		fact := support[projection.source.ItemID]
		if fact.invalid || fact.count == 0 && !projection.allowEmpty || fact.count > 32 {
			continue
		}
		source := projection.source
		if source.EpisodeKey == "" {
			continue
		}
		neededCohorts[analysisCohortKey(source)] = true
		key := analysisAdminSeason{source.LibraryID, source.SeasonID}
		if !seasons[key] {
			seasons[key] = true
			seasonKeys = append(seasonKeys, key)
		}
	}
	counts, cohorts, err := readAnalysisAdminSeasonCohorts(ctx, tx, seasonKeys, neededCohorts, maxBytes)
	if err != nil {
		return err
	}
	policyIDs := []string{}
	policies := map[string]bool{}
	for _, projection := range projections {
		source := projection.source
		fact := support[source.ItemID]
		valid := !fact.invalid && (fact.count != 0 || projection.allowEmpty) && fact.count <= 32
		if valid {
			if source.EpisodeKey == "" {
				valid = analysisCohortHash([]AnalysisSource{source}) == projection.record.proof.cohort
			} else {
				cohort, exists := cohorts[analysisCohortKey(source)]
				if !exists {
					cohort = analysisCohortHash([]AnalysisSource{})
				}
				valid = counts[analysisAdminSeason{source.LibraryID, source.SeasonID}] <= analysisMaximumAdmissionSources && cohort == projection.record.proof.cohort
			}
		}
		if !valid {
			projection.item.Detection.Status = "stale"
			projection.item.Detection.Reasons = append(projection.item.Detection.Reasons, "support_changed")
		} else if analysisDetectionCanPublish(projection.item.Detection, projection.record) && !policies[source.LibraryID] {
			policies[source.LibraryID] = true
			policyIDs = append(policyIDs, source.LibraryID)
		}
	}
	if len(policyIDs) != 0 {
		// Retain policy validation for publishable audit records, but inventory
		// still cannot publish an interval without physical support validation.
		rows, err := tx.Query(ctx, `SELECT id,collection_type='tvshows' AND COALESCE((options->>'EnableIntroDetection')::boolean,false)
			FROM libraries WHERE id=ANY($1::text[])`, policyIDs)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var id string
			var enabled bool
			if err := rows.Scan(&id, &enabled); err != nil {
				rows.Close()
				return err
			}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count != len(policyIDs) {
			return pgx.ErrNoRows
		}
	}
	return nil
}

// Submit S distinct-season statements in one batch, retaining the original
// per-season SQL bound. Each rowset is consumed, hashed and released before
// the next one; neither PostgreSQL nor Go sorts a combined page population.
func readAnalysisAdminSeasonCohorts(ctx context.Context, tx pgx.Tx, seasons []analysisAdminSeason, neededCohorts map[string]bool, maxBytes int64) (map[analysisAdminSeason]int, map[string]string, error) {
	counts := make(map[analysisAdminSeason]int, len(seasons))
	cohorts := make(map[string]string, len(neededCohorts))
	if len(seasons) == 0 {
		return counts, cohorts, nil
	}
	batch := &pgx.Batch{}
	for _, season := range seasons {
		batch.Queue(`SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+`
			WHERE i.library_id=$1 AND i.parent_id=$2 AND `+analysisPhysicalSQL+` ORDER BY i.id LIMIT 100001`, season.libraryID, season.seasonID)
	}
	batchContext, cancel := context.WithCancel(ctx)
	results := tx.SendBatch(batchContext, batch)
	defer func() {
		// A failed rowset must not drain every remaining season. Cancel only
		// this batch's lifetime, retaining the caller's original error/context.
		cancel()
		_ = results.Close()
	}()
	for _, season := range seasons {
		rows, err := results.Query()
		if err != nil {
			return nil, nil, err
		}
		populations := map[string][]AnalysisSource{}
		for rows.Next() {
			source, err := scanAnalysisSource(rows)
			if err != nil {
				cancel()
				rows.Close()
				return nil, nil, err
			}
			counts[season]++
			if counts[season] > analysisMaximumAdmissionSources {
				populations = nil
				continue
			}
			cohort := analysisCohortKey(source)
			if source.Size <= maxBytes && neededCohorts[cohort] {
				populations[cohort] = append(populations[cohort], source)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
		for key, population := range populations {
			cohorts[key] = analysisCohortHash(population)
		}
	}
	if err := results.Close(); err != nil {
		return nil, nil, err
	}
	return counts, cohorts, nil
}

func readAnalysisAdminPreviews(ctx context.Context, tx pgx.Tx, ids []string, projections map[string]*analysisAdminProjection, revision, epoch int64) error {
	revisions := make([]string, len(ids))
	for index, id := range ids {
		revisions[index] = projections[id].source.SourceRevision
	}
	rows, err := tx.Query(ctx, `SELECT target.item_id,p.width,p.height,p.bytes,p.frame_count,
		CASE WHEN p.source_revision=target.source_revision AND p.profile_revision=$3 AND p.publication_epoch=$4 THEN 'ready' ELSE 'stale' END,
		CASE WHEN p.source_revision<>target.source_revision THEN 'source_changed' WHEN p.profile_revision<>$3 OR p.publication_epoch<>$4 THEN 'configuration_changed' ELSE '' END,
		p.updated_at,p.cache_key,p.seal,p.content_sha256
		FROM unnest($1::text[],$2::text[]) target(item_id,source_revision)
		CROSS JOIN LATERAL (SELECT * FROM analysis_previews WHERE item_id=target.item_id ORDER BY width LIMIT 4) p
		ORDER BY target.item_id,p.width`, ids, revisions, revision, epoch)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var preview AnalysisPreviewStatus
		if err := rows.Scan(&id, &preview.Width, &preview.Height, &preview.Size, &preview.FrameCount, &preview.Status, &preview.FailureCode, &preview.UpdatedAt,
			&preview.CacheKey, &preview.CacheSeal, &preview.CacheSHA256); err != nil {
			rows.Close()
			return err
		}
		preview.UpdatedAt = preview.UpdatedAt.UTC()
		item := &projections[id].item
		item.Previews = append(item.Previews, preview)
		if len(item.Previews) > 3 {
			rows.Close()
			return ErrUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	return err
}
