package library

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

type creditsDetectionRecord struct {
	Revision  string
	Status    string
	Reason    string
	Stale     bool
	Active    bool
	UpdatedAt *time.Time
	Result    AnalysisStoredCreditsResult
	Sources   []AnalysisSource
}

func (value creditsDetectionRecord) segments() []CreditsInterval {
	if !value.Active || value.Stale || value.Status != "qualified" {
		return nil
	}
	return append([]CreditsInterval{}, value.Result.Segments...)
}

func readCreditsDetection(ctx context.Context, tx pgx.Tx, access libraryAccess, source AnalysisSource) (creditsDetectionRecord, error) {
	result := creditsDetectionRecord{Revision: "0", Status: "not_analyzed"}
	var revision, profileRevision, epoch int64
	var storedSource, cohort, profile string
	var raw []byte
	var start, end *int64
	err := tx.QueryRow(ctx, `SELECT revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,cohort_revision,status,result,start_ticks,end_ticks,auto_published,updated_at
 FROM analysis_credits_detections WHERE item_id=$1`, source.ItemID).Scan(&revision, &storedSource, &profile, &profileRevision, &epoch, &cohort,
		&result.Status, &raw, &start, &end, &result.Active, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Revision = strconv.FormatInt(revision, 10)
	value, err := ReadStoredCreditsAnalysisResult(raw, result.Status, start, end)
	if err != nil || value.ItemID != source.ItemID || value.SourceRevision != storedSource || !analysisSHA(profile) || !analysisSHA(cohort) {
		result.Stale, result.Reason = true, "invalid_evidence"
		return result, nil
	}
	result.Result, result.Reason = value, value.Reason
	if storedSource != source.SourceRevision || value.DurationTicks != source.DurationTicks || value.ItemType != source.ItemType {
		result.Stale, result.Reason = true, "source_changed"
		return result, nil
	}
	var currentRevision, currentEpoch, maxBytes int64
	if err := tx.QueryRow(ctx, `SELECT revision,publication_epoch,max_source_bytes FROM analysis_settings WHERE id=1`).Scan(&currentRevision, &currentEpoch, &maxBytes); err != nil {
		return result, err
	}
	if profileRevision != currentRevision || epoch != currentEpoch {
		result.Stale, result.Reason = true, "profile_changed"
		return result, nil
	}
	valid, sources, err := readCurrentCreditsReferences(ctx, tx, access, source, value)
	if err != nil {
		return result, err
	}
	result.Sources = sources
	if valid {
		valid, err = creditsCohortCurrent(ctx, tx, source, cohort, maxBytes)
		if err != nil {
			return result, err
		}
	}
	if !valid {
		result.Stale, result.Reason = true, "support_changed"
		return result, nil
	}
	var enabled bool
	if err := tx.QueryRow(ctx, analysisCreditsLibraryPolicySQL, source.LibraryID).Scan(&enabled); err != nil {
		return result, err
	}
	result.Active = result.Active && enabled
	return result, nil
}

func readCurrentCreditsReferences(ctx context.Context, tx pgx.Tx, access libraryAccess, target AnalysisSource, value AnalysisStoredCreditsResult) (bool, []AnalysisSource, error) {
	rows, err := tx.Query(ctx, `SELECT d.source_item_id,d.library_id,d.root_id,d.source_revision,d.hierarchy_revision,d.episode_key,d.content_sha256,
 CASE WHEN i.id IS NULL OR NOT($2 OR i.library_id=ANY($3::text[])) OR NOT (`+access.directSQL("i")+`) THEN '' ELSE `+introSourceRevisionSQL+` END,
 CASE WHEN i.id IS NULL THEN '' ELSE `+analysisHierarchyRevisionSQL+` END,
 COALESCE(i.library_id,''),COALESCE(i.root_id,'')
 FROM analysis_credits_detection_sources d LEFT JOIN items i ON i.id=d.source_item_id
 LEFT JOIN items p ON p.id=i.parent_id AND p.library_id=i.library_id LEFT JOIN items s ON s.id=p.parent_id AND s.library_id=i.library_id
 WHERE d.item_id=$1 ORDER BY d.source_item_id LIMIT 33`, target.ItemID, access.all, access.folders)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	facts := map[string]string{value.SourceRevision: ""}
	for _, member := range value.AudioSources {
		facts[member.SourceKey] = member.ContentIdentity
	}
	valid := true
	expected := []AnalysisSource{}
	seen := map[string]bool{}
	for rows.Next() {
		var member AnalysisSource
		var hash, now, hierarchy, libraryID, rootID string
		if err := rows.Scan(&member.ItemID, &member.LibraryID, &member.RootID, &member.SourceRevision, &member.HierarchyRevision, &member.EpisodeKey,
			&hash, &now, &hierarchy, &libraryID, &rootID); err != nil {
			return false, nil, err
		}
		content, present := facts[member.SourceRevision]
		valid = valid && present && !seen[member.SourceRevision] && hash == content && member.SourceRevision == now && member.HierarchyRevision == hierarchy &&
			member.LibraryID == libraryID && member.RootID == rootID && member.LibraryID == target.LibraryID
		if member.SourceRevision == value.SourceRevision {
			valid = valid && member.ItemID == target.ItemID && member.EpisodeKey == target.EpisodeKey
		}
		for _, support := range value.AudioSources {
			if support.SourceKey == member.SourceRevision {
				valid = valid && member.EpisodeKey == support.EpisodeKey
			}
		}
		seen[member.SourceRevision] = true
		expected = append(expected, member)
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	return valid && len(expected) == len(facts) && len(expected) > 0 && len(expected) <= 2, expected, nil
}

func creditsCohortCurrent(ctx context.Context, tx pgx.Tx, target AnalysisSource, cohort string, maxBytes int64) (bool, error) {
	if target.EpisodeKey == "" {
		return analysisCohortHash([]AnalysisSource{target}) == cohort, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+`
 WHERE i.library_id=$1 AND i.parent_id=$2 AND `+analysisPhysicalSQL+` ORDER BY i.id LIMIT 100001`, target.LibraryID, target.SeasonID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	population := []AnalysisSource{}
	count := 0
	for rows.Next() {
		count++
		source, err := scanAnalysisSource(rows)
		if err != nil {
			return false, err
		}
		if source.Size <= maxBytes && analysisCohortKey(source) == analysisCohortKey(target) {
			population = append(population, source)
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return count <= analysisMaximumAdmissionSources && analysisCohortHash(population) == cohort, nil
}

func (s *Store) validateCreditsDetectionSources(ctx context.Context, subject *Subject, actor *identity.Principal, expected []AnalysisSource) error {
	if subject == nil && actor == nil || len(expected) == 0 || len(expected) > 2 {
		return ErrAnalysisSourceChanged
	}
	// Open every support source through its current authorization and containment
	// boundary. Full hashes prove independent matching at generation time; reads
	// use the current physical source stamp and never rehash a movie per request.
	for _, source := range expected {
		var current AnalysisSource
		var err error
		if subject != nil {
			current, err = s.analysisCurrentSourceFor(ctx, *subject, source.ItemID, "")
		} else {
			current, err = s.analysisCurrentSourceAsAdministrator(ctx, *actor, source.ItemID)
		}
		if err != nil {
			return err
		}
		if source.SourceRevision != current.SourceRevision || source.HierarchyRevision != current.HierarchyRevision ||
			source.LibraryID != current.LibraryID || source.RootID != current.RootID || source.EpisodeKey != current.EpisodeKey {
			return ErrAnalysisSourceChanged
		}
	}
	return nil
}

// ResolveDetectedCreditsForRevision supplies automatic intervals only after
// target and support sources pass current authorization and physical checks.
// Manual/import and reserved chapter points suppress automatic projection.
// Invalid or stale detection evidence is a missing optional result, not a 500.
func (s *Store) ResolveDetectedCreditsForRevision(ctx context.Context, subject Subject, itemID, sourceID, expectedRevision string) ([]CreditsInterval, error) {
	if !analysisOpaque(expectedRevision, 256) {
		return nil, ErrInvalidInput
	}
	source, err := s.analysisCurrentSourceFor(ctx, subject, itemID, sourceID)
	if err != nil {
		return nil, err
	}
	if source.SourceRevision != expectedRevision {
		return nil, ErrAnalysisSourceChanged
	}
	load := func() (creditsDetectionRecord, error) {
		tx, access, err := s.beginSubjectRead(ctx, subject)
		if err != nil {
			return creditsDetectionRecord{}, err
		}
		defer rollback(tx)
		current, err := readAnalysisSourceUsing(ctx, tx, access, itemID, false)
		if err != nil {
			return creditsDetectionRecord{}, err
		}
		if !sameAnalysisSource(source, current) {
			return creditsDetectionRecord{}, ErrAnalysisSourceChanged
		}
		marker, err := readCreditsRecord(ctx, tx, itemID, false)
		if err != nil {
			return creditsDetectionRecord{}, err
		}
		if marker.detail.Effective != nil {
			return creditsDetectionRecord{}, tx.Commit(ctx)
		}
		result, err := readCreditsDetection(ctx, tx, access, current)
		if err != nil {
			return result, err
		}
		return result, tx.Commit(ctx)
	}
	result, err := load()
	if err != nil || len(result.segments()) == 0 {
		return nil, err
	}
	if err := s.validateCreditsDetectionSources(ctx, &subject, nil, result.Sources); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	final, err := load()
	if err != nil {
		return nil, err
	}
	if result.Revision != final.Revision {
		return nil, nil
	}
	return final.segments(), nil
}

func (s *Store) creditsDetectionAsAdministrator(ctx context.Context, actor identity.Principal, itemID, expectedRevision string) (creditsDetectionRecord, error) {
	load := func() (creditsDetectionRecord, error) {
		tx, err := s.beginMetadataRead(ctx, actor)
		if err != nil {
			return creditsDetectionRecord{}, err
		}
		defer rollback(tx)
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM analysis_credits_detections WHERE item_id=$1)`, itemID).Scan(&exists); err != nil {
			return creditsDetectionRecord{}, err
		}
		if !exists {
			return creditsDetectionRecord{Revision: "0", Status: "not_analyzed"}, tx.Commit(ctx)
		}
		source, err := readAnalysisSourceUsing(ctx, tx, unrestrictedLibraryAccess(), itemID, false)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) {
				result, readErr := readStaleCreditsDetection(ctx, tx, itemID)
				if readErr != nil {
					return result, readErr
				}
				return result, tx.Commit(ctx)
			}
			return creditsDetectionRecord{}, err
		}
		if source.SourceRevision != expectedRevision {
			return creditsDetectionRecord{}, ErrCreditsRevisionConflict
		}
		result, err := readCreditsDetection(ctx, tx, unrestrictedLibraryAccess(), source)
		if err != nil {
			return result, err
		}
		return result, tx.Commit(ctx)
	}
	result, err := load()
	if err != nil || result.Revision == "0" || result.Stale {
		return result, err
	}
	if err := s.validateCreditsDetectionSources(ctx, nil, &actor, result.Sources); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		result.Stale, result.Reason = true, "support_changed"
		return result, nil
	}
	final, err := load()
	if err != nil {
		return final, err
	}
	if final.Revision != result.Revision {
		final.Stale, final.Reason = true, "detection_changed"
	}
	return final, nil
}

func readStaleCreditsDetection(ctx context.Context, tx pgx.Tx, itemID string) (creditsDetectionRecord, error) {
	result := creditsDetectionRecord{Stale: true, Reason: "source_changed"}
	var raw []byte
	var start, end *int64
	err := tx.QueryRow(ctx, `SELECT revision::text,status,result,start_ticks,end_ticks,updated_at FROM analysis_credits_detections WHERE item_id=$1`, itemID).
		Scan(&result.Revision, &result.Status, &raw, &start, &end, &result.UpdatedAt)
	if err != nil {
		return result, err
	}
	value, err := ReadStoredCreditsAnalysisResult(raw, result.Status, start, end)
	if err != nil {
		result.Reason = "invalid_evidence"
	} else {
		result.Result = value
	}
	return result, nil
}

func applyCreditsDetection(detail *CreditsDetail, detection creditsDetectionRecord) {
	detail.Detected = append([]CreditsInterval{}, detection.Result.Segments...)
	detail.DetectedStale = detection.Stale
	detail.DetectedRevision = detection.Revision
	detail.DetectedStatus = detection.Status
	detail.DetectedReason = detection.Reason
	detail.DetectedUpdatedAt = detection.UpdatedAt
	if intervals := detection.segments(); detail.Automatic == nil && len(intervals) != 0 {
		detail.Automatic = &CreditsPoint{StartTicks: intervals[0].StartTicks, Provenance: "Detected"}
	}
	if detail.Effective == nil {
		detail.Effective = detail.Automatic
	}
}
