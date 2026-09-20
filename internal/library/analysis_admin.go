package library

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func validateAnalysisItemQuery(input AnalysisItemQuery) (AnalysisItemQuery, error) {
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.StartIndex < 0 || int64(input.StartIndex) > math.MaxInt32 || input.Limit < 1 || input.Limit > 200 ||
		input.LibraryID != "" && !analysisOpaque(input.LibraryID, 128) || len(input.SearchTerm) > 256 ||
		!utf8.ValidString(input.SearchTerm) || strings.IndexFunc(input.SearchTerm, unicode.IsControl) >= 0 {
		return AnalysisItemQuery{}, ErrInvalidInput
	}
	return input, nil
}

func (s *Store) beginAnalysisAdminRead(ctx context.Context, actor identity.Principal) (pgx.Tx, error) {
	if err := analysisContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	if err := (&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}).check(ctx, tx, false); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

// Snapshot reads need repeatable source/result/count facts, while the final
// authority check must see revocation committed after that snapshot began.
// End the snapshot before opening this short independent read transaction.
func (s *Store) finishAnalysisAdminRead(ctx context.Context, actor identity.Principal) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := (&catalogAdministrator{actor: actor, audience: identity.AdministratorNative}).check(ctx, tx, false); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func analysisAdminLibrary(ctx context.Context, tx pgx.Tx, libraryID string) error {
	if libraryID == "" {
		return nil
	}
	var kind string
	if err := tx.QueryRow(ctx, `SELECT collection_type FROM libraries WHERE id=$1`, libraryID).Scan(&kind); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if libraryID == CollectionsLibraryID || kind != "movies" && kind != "tvshows" && kind != "mixed" {
		return ErrInvalidInput
	}
	return nil
}

// Inventory is a bounded indexed-source read. It never opens each media file
// or resolves storage paths merely to render an administrator page.
func (s *Store) ListAnalysisItems(ctx context.Context, actor identity.Principal, input AnalysisItemQuery) (AnalysisItemPage, error) {
	input, err := validateAnalysisItemQuery(input)
	if err != nil {
		return AnalysisItemPage{}, err
	}
	tx, err := s.beginAnalysisAdminRead(ctx, actor)
	if err != nil {
		return AnalysisItemPage{}, err
	}
	defer rollback(tx)
	if err := analysisAdminLibrary(ctx, tx, input.LibraryID); err != nil {
		return AnalysisItemPage{}, err
	}
	population := ` FROM items i ` + analysisSourceJoins + ` JOIN libraries l ON l.id=i.library_id
		WHERE ` + analysisPhysicalSQL + ` AND ` + ordinaryItemSQL("i") + `
		AND l.collection_type IN ('movies','tvshows','mixed') AND i.library_id<>$1
		AND ($2='' OR i.library_id=$2) AND ($3='' OR strpos(lower(i.name),lower($3))>0)
		AND jsonb_typeof(i.media->'Streams')='array' AND i.media->'Streams'<>'[]'::jsonb`
	result := AnalysisItemPage{Items: []AnalysisItem{}}
	if err := tx.QueryRow(ctx, `SELECT count(*)`+population, CollectionsLibraryID, input.LibraryID, input.SearchTerm).Scan(&result.TotalRecordCount); err != nil {
		return AnalysisItemPage{}, err
	}
	rows, err := tx.Query(ctx, `SELECT i.id`+population+` ORDER BY i.sort_name COLLATE "C",i.name COLLATE "C",i.id LIMIT $4 OFFSET $5`,
		CollectionsLibraryID, input.LibraryID, input.SearchTerm, input.Limit, input.StartIndex)
	if err != nil {
		return AnalysisItemPage{}, err
	}
	ids := make([]string, 0, input.Limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return AnalysisItemPage{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AnalysisItemPage{}, err
	}
	for _, id := range ids {
		item, err := readAnalysisAdminItem(ctx, tx, id)
		if err != nil {
			return AnalysisItemPage{}, err
		}
		// Only the detail/playback source readers validate supporting bytes.
		// Indexed audit evidence alone must not advertise a detected interval
		// as physically revalidated during a cheap inventory request.
		if item.Detection.Effective != nil && item.Detection.Effective.Provenance == "Detected" {
			item.Detection.Effective = nil
		}
		result.Items = append(result.Items, item)
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisItemPage{}, err
	}
	if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
		return AnalysisItemPage{}, err
	}
	return result, nil
}

func readAnalysisAdminItem(ctx context.Context, tx pgx.Tx, itemID string) (AnalysisItem, error) {
	source, err := readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, false)
	if err != nil {
		return AnalysisItem{}, err
	}
	result := AnalysisItem{ID: source.ItemID, Type: source.ItemType, LibraryID: source.LibraryID,
		MediaSourceID: source.MediaSourceID, SourceRevision: source.SourceRevision, Previews: []AnalysisPreviewStatus{}}
	if err := analysisAdminLibrary(ctx, tx, source.LibraryID); err != nil {
		return AnalysisItem{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT name FROM items WHERE id=$1`, itemID).Scan(&result.Name); err != nil {
		return AnalysisItem{}, err
	}
	result.Detection, err = readAnalysisDetection(ctx, tx, unrestrictedLibraryAccess(), source)
	if err != nil {
		return AnalysisItem{}, err
	}
	rows, err := tx.Query(ctx, `SELECT p.width,p.height,p.bytes,p.frame_count,
		CASE WHEN p.source_revision=$2 AND p.profile_revision=settings.revision AND p.publication_epoch=settings.publication_epoch THEN 'ready' ELSE 'stale' END,
		CASE WHEN p.source_revision<>$2 THEN 'source_changed' WHEN p.profile_revision<>settings.revision OR p.publication_epoch<>settings.publication_epoch THEN 'configuration_changed' ELSE '' END,
		p.updated_at,p.cache_key,p.seal,p.content_sha256 FROM analysis_previews p CROSS JOIN analysis_settings settings
		WHERE p.item_id=$1 AND settings.id=1 ORDER BY p.width LIMIT 4`, itemID, source.SourceRevision)
	if err != nil {
		return AnalysisItem{}, err
	}
	for rows.Next() {
		var preview AnalysisPreviewStatus
		if err := rows.Scan(&preview.Width, &preview.Height, &preview.Size, &preview.FrameCount, &preview.Status, &preview.FailureCode, &preview.UpdatedAt,
			&preview.CacheKey, &preview.CacheSeal, &preview.CacheSHA256); err != nil {
			rows.Close()
			return AnalysisItem{}, err
		}
		preview.UpdatedAt = preview.UpdatedAt.UTC()
		result.Previews = append(result.Previews, preview)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return AnalysisItem{}, err
	}
	if len(result.Previews) > 3 {
		return AnalysisItem{}, ErrUnavailable
	}
	return result, nil
}

func (s *Store) GetAnalysisItem(ctx context.Context, actor identity.Principal, itemID string) (AnalysisItem, error) {
	if !analysisOpaque(itemID, 128) {
		return AnalysisItem{}, ErrInvalidInput
	}
	if err := analysisContext(ctx); err != nil {
		return AnalysisItem{}, err
	}
	if s == nil || s.pool == nil {
		return AnalysisItem{}, ErrUnavailable
	}
	// Bind the filesystem check to one monotonic logical detection revision.
	// A later candidate must not borrow an earlier candidate's source check.
	before, err := s.analysisAdminItemSnapshot(ctx, actor, itemID)
	if err != nil {
		return AnalysisItem{}, err
	}
	// Filesystem work ends before the result snapshot begins. A stale source
	// downgrades detection authority but does not erase the stored evidence.
	validation := s.validateAnalysisDetectionSources(ctx, nil, &actor, itemID)
	stale := errors.Is(validation, ErrAnalysisSourceChanged) || errors.Is(validation, ErrSourceChanged) || errors.Is(validation, ErrNotFound) || errors.Is(validation, ErrUnavailable)
	if validation != nil && !stale {
		return AnalysisItem{}, validation
	}
	result, err := s.analysisAdminItemSnapshot(ctx, actor, itemID)
	if err != nil {
		return AnalysisItem{}, err
	}
	stale = stale || before.SourceRevision != result.SourceRevision || before.Detection.Revision != result.Detection.Revision
	if stale && result.Detection.Candidate != nil {
		result.Detection.Status = "stale"
		result.Detection.Reasons = append(result.Detection.Reasons, "source_changed")
		if result.Detection.Effective != nil && result.Detection.Effective.Provenance == "Detected" {
			result.Detection.Effective = nil
		}
	}
	return result, nil
}

func (s *Store) analysisAdminItemSnapshot(ctx context.Context, actor identity.Principal, itemID string) (AnalysisItem, error) {
	tx, err := s.beginAnalysisAdminRead(ctx, actor)
	if err != nil {
		return AnalysisItem{}, err
	}
	defer rollback(tx)
	result, err := readAnalysisAdminItem(ctx, tx, itemID)
	if err != nil {
		return AnalysisItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisItem{}, err
	}
	if err := s.finishAnalysisAdminRead(ctx, actor); err != nil {
		return AnalysisItem{}, err
	}
	return result, nil
}

func (s *Store) GetAnalysisDetection(ctx context.Context, actor identity.Principal, itemID string) (AnalysisDetection, error) {
	item, err := s.GetAnalysisItem(ctx, actor, itemID)
	if err != nil {
		return AnalysisDetection{}, err
	}
	return item.Detection, nil
}

// writeItemIntroEditCAS is shared with UpdateItemIntro. The caller has opened
// the actual source outside the transaction and locked the current item after
// locking its administrator. The SQL also retains an exact manual-state CAS.
func writeItemIntroEditCAS(ctx context.Context, tx pgx.Tx, actor identity.Principal, record introRecord, edit IntroEdit, reset bool) error {
	revision, err := analysisRevision(edit.Revision)
	if err != nil || edit.SourceRevision == "" || !reset && edit.Provenance != "Manual" && edit.Provenance != "Import" {
		return ErrInvalidInput
	}
	if record.detail.Revision != edit.Revision || record.detail.SourceRevision != edit.SourceRevision || revision == math.MaxInt64 {
		return ErrIntroRevisionConflict
	}
	var start, end any
	source, provenance := "", ""
	if !reset {
		interval := &IntroInterval{StartTicks: edit.StartTicks, EndTicks: edit.EndTicks, Provenance: edit.Provenance}
		if !validIntroInterval(interval, record.info.DurationTicks) {
			return ErrInvalidInput
		}
		start, end, source, provenance = edit.StartTicks, edit.EndTicks, edit.SourceRevision, edit.Provenance
	}
	tag, err := tx.Exec(ctx, `INSERT INTO item_intro_state
		(item_id,revision,source_revision,start_ticks,end_ticks,provenance,last_edited_by,last_edited_at)
		SELECT $1,1,$2,$3,$4,$5,$6,clock_timestamp() WHERE $7::bigint=0 OR EXISTS(SELECT 1 FROM item_intro_state WHERE item_id=$1 AND revision=$7)
		ON CONFLICT (item_id) DO UPDATE SET revision=item_intro_state.revision+1,source_revision=EXCLUDED.source_revision,
		start_ticks=EXCLUDED.start_ticks,end_ticks=EXCLUDED.end_ticks,provenance=EXCLUDED.provenance,
		last_edited_by=EXCLUDED.last_edited_by,last_edited_at=EXCLUDED.last_edited_at WHERE item_intro_state.revision=$7`,
		record.detail.ItemID, source, start, end, provenance, actor.User.ID, revision)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrIntroRevisionConflict
	}
	return nil
}

func (s *Store) DecideAnalysisIntro(ctx context.Context, actor identity.Principal, itemID string, input AnalysisDecision) (AnalysisDetection, error) {
	if err := analysisContext(ctx); err != nil {
		return AnalysisDetection{}, err
	}
	if s == nil || s.pool == nil {
		return AnalysisDetection{}, ErrUnavailable
	}
	revision, err := analysisRevision(input.Revision)
	if err != nil || !analysisOpaque(itemID, 128) || !analysisOpaque(input.SourceRevision, 256) ||
		input.Action != "accept" && input.Action != "reject" && input.Action != "reset" {
		return AnalysisDetection{}, ErrInvalidInput
	}
	if _, err := analysisRevision(input.ManualRevision); err != nil {
		return AnalysisDetection{}, err
	}
	opened, err := s.analysisCurrentSourceAsAdministrator(ctx, actor, itemID)
	if err != nil {
		return AnalysisDetection{}, err
	}
	if opened.SourceRevision != input.SourceRevision {
		return AnalysisDetection{}, ErrAnalysisSourceChanged
	}
	if input.Action == "accept" {
		before, err := s.analysisAdminItemSnapshot(ctx, actor, itemID)
		if err != nil {
			return AnalysisDetection{}, err
		}
		if before.SourceRevision != input.SourceRevision || before.Detection.Revision != input.Revision || before.Detection.ManualRevision != input.ManualRevision {
			return AnalysisDetection{}, ErrAnalysisConflict
		}
		if err := s.validateAnalysisDetectionSources(ctx, nil, &actor, itemID); err != nil {
			return AnalysisDetection{}, err
		}
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return AnalysisDetection{}, err
	}
	defer rollback(tx)
	administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	if err := administrator.check(ctx, tx, true); err != nil {
		return AnalysisDetection{}, err
	}
	current, err := readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, true)
	if err != nil {
		return AnalysisDetection{}, err
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return AnalysisDetection{}, err
	}
	if !sameAnalysisSource(opened, current) || current.SourceRevision != input.SourceRevision {
		return AnalysisDetection{}, ErrAnalysisSourceChanged
	}
	if err := analysisAdminLibrary(ctx, tx, current.LibraryID); err != nil {
		return AnalysisDetection{}, err
	}
	detection, err := readAnalysisDetection(ctx, tx, unrestrictedLibraryAccess(), current)
	if err != nil {
		return AnalysisDetection{}, err
	}
	if detection.Revision != input.Revision || current.ManualRevision != input.ManualRevision || revision == math.MaxInt64 {
		return AnalysisDetection{}, ErrAnalysisConflict
	}
	if input.Action == "accept" {
		if current.ItemType != "Episode" || detection.Candidate == nil || detection.Status != "qualified" && detection.Status != "review" {
			return AnalysisDetection{}, ErrAnalysisConflict
		}
		record, err := readIntroRecord(ctx, tx, itemID, false)
		if err != nil {
			return AnalysisDetection{}, err
		}
		candidate := detection.Candidate.Interval
		err = writeItemIntroEditCAS(ctx, tx, actor, record, IntroEdit{Revision: input.ManualRevision, SourceRevision: input.SourceRevision,
			StartTicks: candidate.StartTicks, EndTicks: candidate.EndTicks, Provenance: "Manual"}, false)
		if errors.Is(err, ErrIntroRevisionConflict) {
			return AnalysisDetection{}, ErrAnalysisConflict
		}
		if err != nil {
			return AnalysisDetection{}, err
		}
	}
	// Decisions and worker publication share the maximum logical revision.
	// Keep a decision tombstone even when no detection has ever been written.
	if _, err := tx.Exec(ctx, `INSERT INTO analysis_intro_decisions(item_id,revision,source_revision,rejected,updated_by,updated_at)
		VALUES($1,$2,$3,$4,$5,clock_timestamp()) ON CONFLICT(item_id) DO UPDATE SET
		revision=EXCLUDED.revision,source_revision=EXCLUDED.source_revision,rejected=EXCLUDED.rejected,
		updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, itemID, revision+1, current.SourceRevision, input.Action == "reject", actor.User.ID); err != nil {
		return AnalysisDetection{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE analysis_detections SET revision=$2,auto_published=false WHERE item_id=$1`, itemID, revision+1); err != nil {
		return AnalysisDetection{}, err
	}
	evidence, err := json.Marshal(AnalysisAuditEvidence{Decision: &input})
	if err != nil {
		return AnalysisDetection{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence,actor_id)
		VALUES($1,$2,$3,COALESCE((SELECT profile_fingerprint FROM analysis_detections WHERE item_id=$1),''),$4,$5,$6)`,
		itemID, revision+1, current.SourceRevision, input.Action, evidence, actor.User.ID); err != nil {
		return AnalysisDetection{}, err
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: itemID, LibraryID: current.LibraryID, ParentID: current.SeasonID}); err != nil {
		return AnalysisDetection{}, err
	}
	current, err = readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, false)
	if err != nil {
		return AnalysisDetection{}, err
	}
	result, err := readAnalysisDetection(ctx, tx, unrestrictedLibraryAccess(), current)
	if err != nil {
		return AnalysisDetection{}, err
	}
	if result.Revision != strconv.FormatInt(revision+1, 10) {
		return AnalysisDetection{}, ErrUnavailable
	}
	if err := analysisContext(ctx); err != nil {
		return AnalysisDetection{}, err
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return AnalysisDetection{}, err
	}
	if err := analysisContext(ctx); err != nil {
		return AnalysisDetection{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AnalysisDetection{}, err
	}
	return result, nil
}
