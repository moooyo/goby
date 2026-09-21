package library

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/notificationjournal"
)

const analysisHierarchyRevisionSQL = `('analysis-hierarchy-v1-'||md5(jsonb_build_array(i.library_id,i.root_id,i.parent_id,i.type,i.index_number,i.parent_index_number,
 p.id,p.library_id,p.parent_id,p.type,p.is_folder,p.index_number,s.id,s.library_id,s.type,s.is_folder)::text))`
const analysisSourceJoins = `JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
 LEFT JOIN items p ON p.id=i.parent_id AND p.library_id=i.library_id
 LEFT JOIN items s ON s.id=p.parent_id AND s.library_id=i.library_id
 LEFT JOIN item_intro_state manual ON manual.item_id=i.id
 LEFT JOIN analysis_intro_decisions decision ON decision.item_id=i.id
 LEFT JOIN analysis_preview_state preview ON preview.item_id=i.id`

var analysisPhysicalSQL = `NOT i.is_folder AND i.type IN ('Movie','Episode','Video') AND i.file_size>0 AND i.file_size<=1099511627776
 AND i.file_identity<>'' AND i.modified_at IS NOT NULL AND i.media IS NOT NULL
 AND COALESCE((i.media->>'DurationTicks')::bigint,0) BETWEEN 1 AND 432000000000
 AND jsonb_typeof(i.media->'Streams')='array' AND i.media->'Streams'<>'[]'::jsonb
 AND COALESCE((i.media->>'FileChangeTimeNs')::bigint,0)>0
 AND COALESCE((i.media->>'ProbeVersion')::integer,0)>=` + strconv.Itoa(media.CurrentProbeVersion)

const analysisSourceColumns = `i.id,i.library_id,i.root_id,COALESCE(s.id,''),COALESCE(p.id,''),
 CASE WHEN i.type='Episode' AND p.type='Season' AND p.is_folder AND s.type='Series' AND s.is_folder
 AND i.index_number>0 AND p.index_number>0 AND (i.parent_index_number IS NULL OR i.parent_index_number=p.index_number)
 THEN jsonb_build_array(i.library_id,s.id,p.index_number,i.index_number)::text ELSE '' END,
	` + introSourceRevisionSQL + `,` + analysisHierarchyRevisionSQL + `,i.type,i.file_size,(i.media->>'DurationTicks')::bigint,
 COALESCE(manual.revision,0)::text,COALESCE(decision.revision,0)::text,COALESCE(preview.revision,0)::text`

func scanAnalysisSource(row rowScanner) (AnalysisSource, error) {
	var value AnalysisSource
	err := row.Scan(&value.ItemID, &value.LibraryID, &value.RootID, &value.SeriesID, &value.SeasonID, &value.EpisodeKey,
		&value.SourceRevision, &value.HierarchyRevision, &value.ItemType, &value.Size, &value.DurationTicks, &value.ManualRevision, &value.DecisionRevision, &value.PreviewRevision)
	if err != nil {
		return AnalysisSource{}, analysisReadError(err)
	}
	if value.DurationTicks <= 0 || value.DurationTicks > media.MaxAnalysisDurationTicks {
		return AnalysisSource{}, fmt.Errorf("%w: current media analysis source required", ErrUnavailable)
	}
	value.MediaSourceID = media.SourceID(value.ItemID)
	return value, nil
}

func readAnalysisSource(tx OwnedTx, itemID string, lock bool) (AnalysisSource, error) {
	if !analysisOpaque(itemID, 128) {
		return AnalysisSource{}, ErrInvalidInput
	}
	if lock {
		var id string
		if err := tx.QueryRow(`SELECT id FROM items WHERE id=$1 FOR UPDATE`, itemID).Scan(&id); err != nil {
			return AnalysisSource{}, analysisReadError(err)
		}
	}
	return scanAnalysisSource(tx.QueryRow(`SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+` WHERE i.id=$1 AND `+analysisPhysicalSQL, itemID))
}

func readAnalysisSourceUsing(ctx context.Context, tx pgx.Tx, access libraryAccess, itemID string, lock bool) (AnalysisSource, error) {
	if !analysisOpaque(itemID, 128) {
		return AnalysisSource{}, ErrInvalidInput
	}
	if lock {
		var id string
		if err := tx.QueryRow(ctx, `SELECT i.id FROM items i WHERE i.id=$1 AND ($2 OR i.library_id=ANY($3::text[])) AND `+access.directSQL("i")+` FOR UPDATE`, itemID, access.all, access.folders).Scan(&id); err != nil {
			return AnalysisSource{}, analysisReadError(err)
		}
	}
	return scanAnalysisSource(tx.QueryRow(ctx, `SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+` WHERE i.id=$1 AND `+analysisPhysicalSQL+` AND ($2 OR i.library_id=ANY($3::text[])) AND `+access.directSQL("i"), itemID, access.all, access.folders))
}

func FindAnalysisSource(work AnalysisWork, itemID string) (AnalysisSource, bool) {
	for _, source := range work.Sources {
		if source.ItemID == itemID {
			return source, true
		}
	}
	return AnalysisSource{}, false
}

func readAnalysisWork(tx OwnedTx, childID string) (AnalysisWork, error) {
	var work AnalysisWork
	var profileRaw, executionRaw []byte
	var revision int64
	err := tx.QueryRow(`SELECT w.child_id,w.run_id,w.task_key,w.library_id,w.scope_key,w.cohort_revision,w.force,w.reason,
 p.configuration_revision,p.publication_epoch,p.fingerprint,p.profile,p.execution FROM analysis_work w JOIN analysis_run_profiles p ON p.run_id=w.run_id WHERE w.child_id=$1`, childID).Scan(&work.ChildID, &work.RunID, &work.TaskKey, &work.LibraryID, &work.ScopeKey, &work.CohortRevision, &work.Force, &work.Reason, &revision, &work.PublicationEpoch, &work.ConfigurationFingerprint, &profileRaw, &executionRaw)
	if err != nil {
		return AnalysisWork{}, analysisReadError(err)
	}
	if ValidateStoredAnalysisAdmission(profileRaw, executionRaw, revision, work.PublicationEpoch, work.ConfigurationFingerprint) != nil {
		return AnalysisWork{}, ErrUnavailable
	}
	// Historical admissions remain valid archive facts but are never upgraded
	// into worker authority by filling newly introduced fields with zero values.
	if analysisStrictJSON(profileRaw, &work.Profile) != nil || analysisStrictJSON(executionRaw, &work.Execution) != nil ||
		ValidateAnalysisExecutionProfile(work.Execution) != nil {
		return AnalysisWork{}, ErrUnavailable
	}
	work.ConfigurationRevision = strconv.FormatInt(revision, 10)
	rows, err := tx.Query(`SELECT item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,source_revision,hierarchy_revision,duration_ticks,size,manual_revision::text,decision_revision::text,preview_revision::text FROM analysis_work_sources WHERE child_id=$1 ORDER BY position`, childID)
	if err != nil {
		return AnalysisWork{}, err
	}
	work.Sources = []AnalysisSource{}
	for rows.Next() {
		var source AnalysisSource
		if err := rows.Scan(&source.ItemID, &source.Position, &source.Target, &source.LibraryID, &source.RootID, &source.SeriesID, &source.SeasonID, &source.EpisodeKey, &source.ItemType, &source.SourceRevision, &source.HierarchyRevision, &source.DurationTicks, &source.Size, &source.ManualRevision, &source.DecisionRevision, &source.PreviewRevision); err != nil {
			rows.Close()
			return AnalysisWork{}, err
		}
		source.MediaSourceID = media.SourceID(source.ItemID)
		work.Sources = append(work.Sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AnalysisWork{}, err
	}
	if len(work.Sources) == 0 || len(work.Sources) > 32 {
		return AnalysisWork{}, ErrUnavailable
	}
	return work, nil
}

func sameAnalysisSource(a, b AnalysisSource) bool {
	return a.ItemID == b.ItemID && a.LibraryID == b.LibraryID && a.RootID == b.RootID && a.SourceRevision == b.SourceRevision && a.HierarchyRevision == b.HierarchyRevision && a.EpisodeKey == b.EpisodeKey && a.DurationTicks == b.DurationTicks && a.Size == b.Size
}

func validateCurrentAnalysisWork(tx OwnedTx, work AnalysisWork) error {
	var revision string
	var epoch int64
	if err := tx.QueryRow(`SELECT revision::text,publication_epoch FROM analysis_settings WHERE id=1 FOR SHARE`).Scan(&revision, &epoch); err != nil {
		return err
	}
	if revision != work.ConfigurationRevision || epoch != work.PublicationEpoch {
		return ErrAnalysisConflict
	}
	ids := make([]string, len(work.Sources))
	for index, source := range work.Sources {
		ids[index] = source.ItemID
	}
	// The item lock precedes optional manual, decision and clear tombstones.
	rows, err := tx.Query(`SELECT id FROM items WHERE id=ANY($1::text[]) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(`SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+` WHERE i.id=ANY($1::text[]) AND `+analysisPhysicalSQL, ids)
	if err != nil {
		return err
	}
	current := map[string]AnalysisSource{}
	for rows.Next() {
		source, err := scanAnalysisSource(rows)
		if err != nil {
			rows.Close()
			return err
		}
		current[source.ItemID] = source
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, source := range work.Sources {
		now, exists := current[source.ItemID]
		if !exists || !sameAnalysisSource(source, now) {
			return ErrAnalysisSourceChanged
		}
		if source.Target && ((work.TaskKey == TaskIntroAnalysisKey && (source.ManualRevision != now.ManualRevision || source.DecisionRevision != now.DecisionRevision)) || (work.TaskKey == TaskPreviewGenerationKey && source.PreviewRevision != now.PreviewRevision)) {
			return ErrAnalysisConflict
		}
	}
	population := []AnalysisSource{work.Sources[0]}
	if work.TaskKey == TaskIntroAnalysisKey && work.Sources[0].EpisodeKey != "" {
		rows, err := tx.Query(`SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+` WHERE i.library_id=$1 AND i.parent_id=$2 AND `+analysisPhysicalSQL+` ORDER BY i.id LIMIT 100001`, work.LibraryID, work.Sources[0].SeasonID)
		if err != nil {
			return err
		}
		population = []AnalysisSource{}
		count := 0
		for rows.Next() {
			count++
			source, err := scanAnalysisSource(rows)
			if err != nil {
				rows.Close()
				return err
			}
			if source.Size <= work.Profile.MaxSourceBytes && analysisCohortKey(source) == analysisCohortKey(work.Sources[0]) {
				population = append(population, source)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if count > analysisMaximumAdmissionSources {
			return ErrAnalysisSourceChanged
		}
	}
	if analysisCohortHash(population) != work.CohortRevision {
		return ErrAnalysisSourceChanged
	}
	return nil
}

func (s *Store) withAnalysisWork(ctx context.Context, childID string, fence AnalysisFence, callback func(OwnedTx, AnalysisWork) error) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	if fence == nil || callback == nil || !analysisOpaque(childID, 128) {
		return ErrInvalidInput
	}
	return s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := analysisContext(ctx); err != nil {
			return err
		}
		if err := fence(tx); err != nil {
			return err
		}
		work, err := readAnalysisWork(tx, childID)
		if err != nil {
			return err
		}
		if err := validateCurrentAnalysisWork(tx, work); err != nil {
			return err
		}
		if err := callback(tx, work); err != nil {
			return err
		}
		if err := analysisFlush(tx); err != nil {
			return err
		}
		if err := analysisContext(ctx); err != nil {
			return err
		}
		if err := fence(tx); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
}

func (s *Store) GetAnalysisWork(ctx context.Context, childID string, fence AnalysisFence) (AnalysisWork, error) {
	var result AnalysisWork
	err := s.withAnalysisWork(ctx, childID, fence, func(_ OwnedTx, work AnalysisWork) error { result = work; return nil })
	if err != nil {
		return AnalysisWork{}, err
	}
	return result, nil
}

// OpenAnalysisSource returns a descriptor after the task capability, every
// cohort stamp and the actual contained source have been checked. The caller
// must revalidate its descriptor after reading and close it on every path.
func (s *Store) OpenAnalysisSource(ctx context.Context, childID, itemID string, fence AnalysisFence) (*os.File, MediaFile, error) {
	work, err := s.GetAnalysisWork(ctx, childID, fence)
	if err != nil {
		return nil, MediaFile{}, err
	}
	expected, ok := FindAnalysisSource(work, itemID)
	if !ok {
		return nil, MediaFile{}, ErrNotFound
	}
	file, mediaFile, err := runAnalysisSourceWorker(ctx, func() (*os.File, MediaFile, error) {
		tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, MediaFile{}, err
		}
		defer rollback(tx)
		current, err := readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, false)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if !sameAnalysisSource(expected, current) {
			return nil, MediaFile{}, ErrAnalysisSourceChanged
		}
		snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), itemID, expected.MediaSourceID)
		if err != nil {
			return nil, MediaFile{}, err
		}
		if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
			return nil, MediaFile{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, MediaFile{}, err
		}
		file, err := s.openPublicMediaSource(ctx, snapshot)
		return file, snapshot.mediaFile, err
	})
	if err != nil {
		return nil, MediaFile{}, err
	}
	if _, err := s.GetAnalysisWork(ctx, childID, fence); err != nil {
		_ = file.Close()
		return nil, MediaFile{}, err
	}
	return file, mediaFile, nil
}

func (s *Store) RevalidateAnalysisWork(ctx context.Context, childID string, fence AnalysisFence) (AnalysisWork, error) {
	work, err := s.GetAnalysisWork(ctx, childID, fence)
	if err != nil {
		return AnalysisWork{}, err
	}
	for _, source := range work.Sources {
		file, _, err := s.OpenAnalysisSource(ctx, childID, source.ItemID, fence)
		if err != nil {
			return AnalysisWork{}, err
		}
		if err := file.Close(); err != nil {
			return AnalysisWork{}, err
		}
	}
	return s.GetAnalysisWork(ctx, childID, fence)
}

func (s *Store) GetCurrentAnalysisSourceFor(ctx context.Context, subject Subject, itemID, sourceID string) (AnalysisSource, error) {
	return s.analysisCurrentSourceFor(ctx, subject, itemID, sourceID)
}

func (s *Store) analysisCurrentSourceFor(ctx context.Context, subject Subject, itemID, sourceID string) (AnalysisSource, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return AnalysisSource{}, err
	}
	defer rollback(tx)
	if !access.canPlay {
		return AnalysisSource{}, ErrForbidden
	}
	before, err := readAnalysisSourceUsing(ctx, tx, access, itemID, false)
	if err != nil {
		return AnalysisSource{}, err
	}
	if sourceID != "" && sourceID != before.MediaSourceID {
		return AnalysisSource{}, ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisSource{}, err
	}
	file, _, err := s.OpenMediaFor(ctx, subject, itemID, sourceID)
	if err != nil {
		return AnalysisSource{}, err
	}
	if err := file.Close(); err != nil {
		return AnalysisSource{}, err
	}
	tx, access, err = s.beginSubjectRead(ctx, subject)
	if err != nil {
		return AnalysisSource{}, err
	}
	defer rollback(tx)
	if !access.canPlay {
		return AnalysisSource{}, ErrForbidden
	}
	after, err := readAnalysisSourceUsing(ctx, tx, access, itemID, false)
	if err != nil {
		return AnalysisSource{}, err
	}
	if !sameAnalysisSource(before, after) {
		return AnalysisSource{}, ErrAnalysisSourceChanged
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisSource{}, err
	}
	return after, nil
}

func (s *Store) analysisCurrentSourceAsAdministrator(ctx context.Context, actor identity.Principal, itemID string) (AnalysisSource, error) {
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return AnalysisSource{}, err
	}
	defer rollback(tx)
	source, err := readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, false)
	if err != nil {
		return AnalysisSource{}, err
	}
	snapshot, err := readIndexedMediaSource(ctx, tx, unrestrictedLibraryAccess(), itemID, source.MediaSourceID)
	if err != nil {
		return AnalysisSource{}, err
	}
	if err := captureMediaPublicationRead(ctx, tx, &snapshot); err != nil {
		return AnalysisSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisSource{}, err
	}
	file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		file, err := s.openPublicMediaSource(ctx, snapshot)
		return file, snapshot.mediaFile, err
	})
	if err != nil {
		return AnalysisSource{}, err
	}
	if err := file.Close(); err != nil {
		return AnalysisSource{}, err
	}
	tx, err = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AnalysisSource{}, err
	}
	defer rollback(tx)
	after, err := readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, false)
	if err != nil {
		return AnalysisSource{}, err
	}
	if !sameAnalysisSource(source, after) {
		return AnalysisSource{}, ErrAnalysisSourceChanged
	}
	if err := (&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}).check(ctx, tx, false); err != nil {
		return AnalysisSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisSource{}, err
	}
	return after, nil
}

func analysisFlush(tx OwnedTx) error {
	view, ok := tx.(*ownedCallbackTx)
	if !ok || view == nil {
		return ErrInvalidInput
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.finished {
		return pgx.ErrTxClosed
	}
	return view.observeLocked(view.catalog.flushSystemEvent())
}

func analysisRecordChanges(tx OwnedTx, changes ...CatalogChange) error {
	view, ok := tx.(*ownedCallbackTx)
	if !ok || view == nil {
		return ErrInvalidInput
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.finished {
		return pgx.ErrTxClosed
	}
	if err := view.observeLocked(recordCatalogChanges(view.catalog, changes...)); err != nil {
		return err
	}
	return view.observeLocked(view.catalog.flushSystemEvent())
}

func analysisInvalidateCatalog(tx OwnedTx) error {
	rows, err := tx.Query(`SELECT id,library_id FROM items WHERE type='CollectionFolder' AND id=library_id ORDER BY id LIMIT 4097`)
	if err != nil {
		return err
	}
	refs := []notificationjournal.Reference{}
	for rows.Next() {
		var id, libraryID string
		if err := rows.Scan(&id, &libraryID); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, notificationjournal.Reference{Kind: "Library", ID: id, LibraryID: libraryID})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	view, ok := tx.(*ownedCallbackTx)
	if !ok {
		return ErrInvalidInput
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.finished {
		return pgx.ErrTxClosed
	}
	view.catalog.catalogChanges.requireResync()
	view.catalog.notificationReferences = append(view.catalog.notificationReferences, refs...)
	return view.observeLocked(view.catalog.flushSystemEvent())
}

func analysisRevision(value string) (int64, error) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 0 || strconv.FormatInt(number, 10) != value {
		return 0, ErrInvalidInput
	}
	return number, nil
}

// Task cancellation retains both this bounded source slot and the caller's task
// slot until actual filesystem work and undelivered descriptor cleanup finish.
// HTTP source reads deliberately retain their separate early-return behavior.
func runAnalysisSourceWorker(ctx context.Context, work func() (*os.File, MediaFile, error)) (*os.File, MediaFile, error) {
	if err := analysisContext(ctx); err != nil {
		return nil, MediaFile{}, err
	}
	select {
	case mediaSourceWorkers <- struct{}{}:
	case <-ctx.Done():
		return nil, MediaFile{}, ctx.Err()
	}
	defer func() { <-mediaSourceWorkers }()
	if err := ctx.Err(); err != nil {
		return nil, MediaFile{}, err
	}
	file, source, err := work()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		return nil, MediaFile{}, err
	}
	return file, source, nil
}
