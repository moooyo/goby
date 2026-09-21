package library

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

// AnalysisStoredResult is an audit fact, not worker authority. A library-level
// abstention has a Reason and deliberately has no invented content identity.
type AnalysisStoredResult struct {
	Version string
	Episode introdetect.EpisodeResult
	Reason  string
}
type AnalysisAuditEvidence struct {
	Result   *AnalysisStoredResult
	Decision *AnalysisDecision
}

func analysisAbstentionReason(reason string) bool {
	switch reason {
	case "insufficient_cohort", "unsupported_item_type", "unsupported_hierarchy", "source_unavailable", "analysis_limit", "comparison_budget_exceeded", "cancelled":
		return true
	}
	return false
}
func analysisMatcherReason(reason introdetect.Reason) bool {
	switch reason {
	case introdetect.DuplicateIdentity, introdetect.InsufficientEpisodes, introdetect.MissingAudio, introdetect.MissingVisual, introdetect.IncompatibleProfile, introdetect.NoRepeatedInterval, introdetect.LowAudioEntropy, introdetect.InsufficientAudio, introdetect.InsufficientVisual, introdetect.LowVisualDiversity, introdetect.AudioWithoutVisual, introdetect.InsufficientConsensus, introdetect.WeakAudioEvidence, introdetect.WeakVisualEvidence, introdetect.ShortInterval, introdetect.CompetingIntervals, introdetect.AnalysisBoundary, introdetect.OverlongRepeat, introdetect.CandidateSearchLimited, "source_timeline_unproven":
		return true
	case introdetect.InsufficientVisualAnchors, introdetect.PeriodicVisualEvidence, introdetect.InconsistentTimeAlignment:
		return true
	}
	return false
}
func validateAnalysisReasons(reasons []introdetect.Reason) bool {
	if reasons == nil {
		return false
	}
	seen := map[introdetect.Reason]bool{}
	for _, reason := range reasons {
		if !analysisMatcherReason(reason) || seen[reason] {
			return false
		}
		seen[reason] = true
	}
	return true
}
func analysisInterval(interval introdetect.Interval, duration int64) bool {
	return interval.StartTicks >= 0 && interval.EndTicks > interval.StartTicks && interval.EndTicks <= min(duration, 600*media.TicksPerSecond)
}
func validateAnalysisCandidate(candidate introdetect.Candidate) bool {
	options := introdetect.DefaultOptions()
	duration := candidate.Interval.EndTicks - candidate.Interval.StartTicks
	if !analysisInterval(candidate.Interval, 600*media.TicksPerSecond) || duration < options.MinDurationTicks || duration > options.MaxDurationTicks || !analysisOpaque(candidate.GroupID, 256) || candidate.Status != introdetect.Qualified && candidate.Status != introdetect.Review || !validateAnalysisReasons(candidate.Reasons) || !introdetect.ValidateCandidateEvidence(candidate, options) || len(candidate.Support) < options.MinSupport || len(candidate.Support) > 32 || candidate.Metrics.AudioDistinct < 12 || candidate.Metrics.AudioSamples < candidate.Metrics.AudioDistinct || candidate.Metrics.PairCount != len(candidate.Support)*(len(candidate.Support)-1)/2 {
		return false
	}
	episodes, sources, contents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, reason := range candidate.Reasons {
		if reason == "source_timeline_unproven" {
			return false
		}
	}
	for _, support := range candidate.Support {
		span := support.Interval.EndTicks - support.Interval.StartTicks
		if !analysisOpaque(support.EpisodeKey, 512) || !analysisOpaque(support.SourceKey, 256) || !analysisSHA(support.ContentIdentity) || !analysisInterval(support.Interval, 600*media.TicksPerSecond) || span < options.MinDurationTicks || span > options.MaxDurationTicks || (candidate.Status == introdetect.Qualified && span < options.AutoMinDurationTicks) || episodes[support.EpisodeKey] || sources[support.SourceKey] || contents[support.ContentIdentity] {
			return false
		}
		episodes[support.EpisodeKey] = true
		sources[support.SourceKey] = true
		contents[support.ContentIdentity] = true
	}
	return true
}

func validateAnalysisStoredResult(value AnalysisStoredResult) error {
	episode := value.Episode
	if value.Version != introdetect.Version || !analysisOpaque(episode.SourceKey, 256) || (episode.EpisodeKey != "" && !analysisOpaque(episode.EpisodeKey, 512)) || !validateAnalysisReasons(episode.Reasons) || episode.Candidates == nil || len(episode.Candidates) > 128 {
		return ErrInvalidInput
	}
	if value.Reason != "" {
		if !analysisAbstentionReason(value.Reason) || episode.Status != introdetect.NoResult || len(episode.Candidates) != 0 || len(episode.Reasons) != 0 || episode.ContentIdentity != "" {
			return ErrInvalidInput
		}
		return nil
	}
	if !analysisOpaque(episode.EpisodeKey, 512) || !analysisSHA(episode.ContentIdentity) {
		return ErrInvalidInput
	}
	switch episode.Status {
	case introdetect.Qualified:
		if len(episode.Candidates) != 1 || len(episode.Reasons) != 0 || episode.Candidates[0].Status != introdetect.Qualified {
			return ErrInvalidInput
		}
	case introdetect.Review:
		if len(episode.Candidates) == 0 {
			return ErrInvalidInput
		}
	case introdetect.NoResult:
		if len(episode.Candidates) != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	for _, reason := range episode.Reasons {
		if reason == "source_timeline_unproven" && (episode.Status != introdetect.NoResult || len(episode.Candidates) != 0) {
			return ErrInvalidInput
		}
	}
	groups := map[string]bool{}
	for _, candidate := range episode.Candidates {
		if !validateAnalysisCandidate(candidate) || groups[candidate.GroupID] {
			return ErrInvalidInput
		}
		groups[candidate.GroupID] = true
		found := false
		for _, support := range candidate.Support {
			if support.SourceKey == episode.SourceKey {
				found = support.EpisodeKey == episode.EpisodeKey && support.ContentIdentity == episode.ContentIdentity && support.Interval == candidate.Interval
			}
		}
		if !found {
			return ErrInvalidInput
		}
	}
	return nil
}

func analysisStoredInterval(value AnalysisStoredResult) (*int64, *int64) {
	if len(value.Episode.Candidates) == 0 {
		return nil, nil
	}
	candidate := value.Episode.Candidates[0]
	start, end := candidate.Interval.StartTicks, candidate.Interval.EndTicks
	return &start, &end
}

func ValidateStoredAnalysisResult(raw []byte, status string, start, end *int64) error {
	_, err := ReadStoredAnalysisResult(raw, status, start, end)
	return err
}

func ValidateStoredAnalysisAudit(raw []byte, action string) error {
	_, err := ReadStoredAnalysisAudit(raw, action)
	return err
}

func normalizeAnalysisEpisode(episode introdetect.EpisodeResult) introdetect.EpisodeResult {
	episode.Reasons = append([]introdetect.Reason{}, episode.Reasons...)
	episode.Candidates = append([]introdetect.Candidate{}, episode.Candidates...)
	for index := range episode.Candidates {
		episode.Candidates[index].Reasons = append([]introdetect.Reason{}, episode.Candidates[index].Reasons...)
		episode.Candidates[index].Support = append([]introdetect.Support{}, episode.Candidates[index].Support...)
	}
	return episode
}

func validateAnalysisResultForWork(work AnalysisWork, result introdetect.Result) (map[string]AnalysisStoredResult, error) {
	if work.TaskKey != TaskIntroAnalysisKey || !work.Execution.Available || result.Version != work.Execution.DetectorVersion || result.CohortKey != work.ScopeKey || result.Options != work.Execution.DetectorOptions || result.Comparisons < 0 || result.Comparisons > result.Options.MaxComparisons || len(result.Episodes) != len(work.Sources) || len(result.Groups) > result.Options.MaxGroups {
		return nil, ErrInvalidInput
	}
	expected := map[string]AnalysisSource{}
	for _, source := range work.Sources {
		expected[source.SourceRevision] = source
	}
	groups := map[string]introdetect.Group{}
	for _, group := range result.Groups {
		if _, exists := groups[group.ID]; exists {
			return nil, ErrInvalidInput
		}
		groups[group.ID] = group
	}
	values := map[string]AnalysisStoredResult{}
	for _, episode := range result.Episodes {
		source, exists := expected[episode.SourceKey]
		if !exists || episode.EpisodeKey != source.EpisodeKey {
			return nil, ErrInvalidInput
		}
		if _, duplicate := values[source.ItemID]; duplicate {
			return nil, ErrInvalidInput
		}
		value := AnalysisStoredResult{Version: result.Version, Episode: normalizeAnalysisEpisode(episode)}
		if validateAnalysisStoredResult(value) != nil {
			return nil, ErrInvalidInput
		}
		for _, candidate := range episode.Candidates {
			group, exists := groups[candidate.GroupID]
			if !exists || group.Status != candidate.Status || group.AlgorithmProfile != work.Execution.IntroProfile ||
				group.Metrics != candidate.Metrics || !reflect.DeepEqual(group.Reasons, candidate.Reasons) || !reflect.DeepEqual(group.Members, candidate.Support) {
				return nil, ErrInvalidInput
			}
			if !analysisInterval(candidate.Interval, source.DurationTicks) {
				return nil, ErrInvalidInput
			}
			for _, support := range candidate.Support {
				member, exists := expected[support.SourceKey]
				if !exists || member.EpisodeKey != support.EpisodeKey || !analysisInterval(support.Interval, member.DurationTicks) {
					return nil, ErrInvalidInput
				}
			}
		}
		values[source.ItemID] = value
	}
	return values, nil
}

func (s *Store) PublishIntroAnalysis(ctx context.Context, childID string, fence AnalysisFence, result introdetect.Result) error {
	work, err := s.RevalidateAnalysisWork(ctx, childID, fence)
	if err != nil {
		return err
	}
	values, err := validateAnalysisResultForWork(work, result)
	if err != nil {
		return err
	}
	return s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, current AnalysisWork) error { return publishAnalysisResults(tx, current, values) })
}

func (s *Store) PublishAnalysisAbstention(ctx context.Context, childID string, fence AnalysisFence, reason string) error {
	if !analysisAbstentionReason(reason) {
		return ErrInvalidInput
	}
	if _, err := s.RevalidateAnalysisWork(ctx, childID, fence); err != nil {
		return err
	}
	return s.withAnalysisWork(ctx, childID, fence, func(tx OwnedTx, work AnalysisWork) error {
		if work.TaskKey != TaskIntroAnalysisKey {
			return ErrInvalidInput
		}
		values := map[string]AnalysisStoredResult{}
		for _, source := range work.Sources {
			values[source.ItemID] = AnalysisStoredResult{Version: introdetect.Version, Episode: introdetect.EpisodeResult{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, Status: introdetect.NoResult, Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{}}, Reason: reason}
		}
		return publishAnalysisResults(tx, work, values)
	})
}

func publishAnalysisResults(tx OwnedTx, work AnalysisWork, values map[string]AnalysisStoredResult) error {
	if work.TaskKey != TaskIntroAnalysisKey || ValidateAnalysisExecutionProfile(work.Execution) != nil ||
		work.Execution.Available && work.Execution.DetectorVersion != introdetect.Version {
		return ErrInvalidInput
	}
	profileRevision, err := analysisRevision(work.ConfigurationRevision)
	if err != nil || profileRevision < 1 {
		return ErrInvalidInput
	}
	changes := []CatalogChange{}
	for _, source := range work.Sources {
		if !source.Target {
			continue
		}
		value, exists := values[source.ItemID]
		// An unavailable execution can record a library abstention in the
		// current result format, but cannot supply matcher qualification facts.
		if !exists || validateAnalysisStoredResult(value) != nil || !work.Execution.Available && value.Reason == "" {
			return ErrInvalidInput
		}
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > 131072 {
			return ErrInvalidInput
		}
		start, end := analysisStoredInterval(value)
		if ValidateStoredAnalysisResult(raw, string(value.Episode.Status), start, end) != nil {
			return ErrInvalidInput
		}
		var previous int64
		var suppressed bool
		if err := tx.QueryRow(`SELECT GREATEST(COALESCE((SELECT revision FROM analysis_detections WHERE item_id=$1),0),COALESCE((SELECT revision FROM analysis_intro_decisions WHERE item_id=$1),0)),
   COALESCE((SELECT rejected AND source_revision=$2 FROM analysis_intro_decisions WHERE item_id=$1),false)`, source.ItemID, source.SourceRevision).Scan(&previous, &suppressed); err != nil {
			return err
		}
		if previous == math.MaxInt64 {
			return ErrAnalysisConflict
		}
		auto := value.Episode.Status == introdetect.Qualified && work.Profile.AutoPublishIntros && !suppressed
		if _, err := tx.Exec(`INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
   VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(item_id) DO UPDATE SET revision=EXCLUDED.revision,source_revision=EXCLUDED.source_revision,
   profile_fingerprint=EXCLUDED.profile_fingerprint,profile_revision=EXCLUDED.profile_revision,publication_epoch=EXCLUDED.publication_epoch,child_id=EXCLUDED.child_id,
   cohort_revision=EXCLUDED.cohort_revision,status=EXCLUDED.status,result=EXCLUDED.result,start_ticks=EXCLUDED.start_ticks,end_ticks=EXCLUDED.end_ticks,auto_published=EXCLUDED.auto_published,updated_at=clock_timestamp()`, source.ItemID, previous+1, source.SourceRevision, work.ConfigurationFingerprint, profileRevision, work.PublicationEpoch, work.ChildID, work.CohortRevision, string(value.Episode.Status), raw, start, end, auto); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM analysis_detection_sources WHERE item_id=$1`, source.ItemID); err != nil {
			return err
		}
		facts := map[string]string{}
		if value.Episode.ContentIdentity != "" {
			facts[source.SourceRevision] = value.Episode.ContentIdentity
		}
		for _, candidate := range value.Episode.Candidates {
			for _, support := range candidate.Support {
				if prior, exists := facts[support.SourceKey]; exists && prior != support.ContentIdentity {
					return ErrInvalidInput
				}
				facts[support.SourceKey] = support.ContentIdentity
			}
		}
		for _, member := range work.Sources {
			content, exists := facts[member.SourceRevision]
			if !exists {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, source.ItemID, member.ItemID, member.LibraryID, member.RootID, member.SourceRevision, member.HierarchyRevision, member.EpisodeKey, content); err != nil {
				return err
			}
		}
		evidence, _ := json.Marshal(AnalysisAuditEvidence{Result: &value})
		if len(evidence) > 131072 {
			return ErrInvalidInput
		}
		if _, err := tx.Exec(`INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence) VALUES($1,$2,$3,$4,$5,$6)`, source.ItemID, previous+1, source.SourceRevision, work.ConfigurationFingerprint, string(value.Episode.Status), evidence); err != nil {
			return err
		}
		changes = append(changes, CatalogChange{Kind: CatalogUpdated, ItemID: source.ItemID, LibraryID: source.LibraryID})
	}
	return analysisRecordChanges(tx, changes...)
}

type analysisDetectionProof struct {
	source, profile, cohort string
	revision, epoch         int64
	active                  bool
}

func readAnalysisDetection(ctx context.Context, tx pgx.Tx, access libraryAccess, source AnalysisSource) (AnalysisDetection, error) {
	result := AnalysisDetection{ItemID: source.ItemID, Revision: "0", ManualRevision: source.ManualRevision, SourceRevision: source.SourceRevision, Status: "not_analyzed", Reasons: []string{"not_analyzed"}}
	if source.ItemType == "Movie" || source.ItemType == "Episode" {
		record, err := readIntroRecord(ctx, tx, source.ItemID, false)
		if err != nil {
			return result, err
		}
		result.Effective = record.detail.Effective
	}
	var decisionRevision int64
	var decisionSource string
	err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM analysis_intro_decisions WHERE item_id=$1),0),COALESCE((SELECT source_revision FROM analysis_intro_decisions WHERE item_id=$1),''),COALESCE((SELECT rejected FROM analysis_intro_decisions WHERE item_id=$1),false)`, source.ItemID).Scan(&decisionRevision, &decisionSource, &result.Suppressed)
	if err != nil {
		return result, err
	}
	result.Suppressed = result.Suppressed && decisionSource == source.SourceRevision
	result.Revision = strconv.FormatInt(decisionRevision, 10)
	var proof analysisDetectionProof
	var raw []byte
	var start, end *int64
	var revision int64
	err = tx.QueryRow(ctx, `SELECT revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,cohort_revision,status,result,start_ticks,end_ticks,auto_published,updated_at FROM analysis_detections WHERE item_id=$1`, source.ItemID).Scan(&revision, &proof.source, &proof.profile, &proof.revision, &proof.epoch, &proof.cohort, &result.Status, &raw, &start, &end, &proof.active, &result.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Revision = strconv.FormatInt(max(revision, decisionRevision), 10)
	facts, current, decodeErr := decodeAnalysisStoredResult(raw)
	if decodeErr != nil || !analysisStoredFactsMatch(facts, result.Status, start, end) {
		return result, ErrUnavailable
	}
	result.Reasons = []string{}
	for _, reason := range facts.Episode.Reasons {
		result.Reasons = append(result.Reasons, reason)
	}
	if facts.Reason != "" {
		result.Reasons = append(result.Reasons, facts.Reason)
	}
	// Retired algorithm claims remain auditable without inventing current metrics
	// or making old qualification effective under unchanged settings and sources.
	if current == nil {
		result.Status = "stale"
		result.Reasons = append(result.Reasons, "algorithm_changed")
		return result, nil
	}
	if len(current.Episode.Candidates) > 0 {
		candidate := current.Episode.Candidates[0]
		result.Candidate = &candidate
	}
	stale := ""
	if proof.source != source.SourceRevision {
		stale = "source_changed"
	}
	var currentRevision, currentEpoch int64
	var maxBytes int64
	if err := tx.QueryRow(ctx, `SELECT revision,publication_epoch,max_source_bytes FROM analysis_settings WHERE id=1`).Scan(&currentRevision, &currentEpoch, &maxBytes); err != nil {
		return result, err
	}
	if proof.revision != currentRevision || proof.epoch != currentEpoch {
		stale = "profile_changed"
	}
	if stale == "" {
		valid, err := analysisDetectionReferencesCurrent(ctx, tx, access, source, proof.cohort, maxBytes, facts.Reason != "")
		if err != nil {
			return result, err
		}
		if !valid {
			stale = "support_changed"
		}
	}
	if stale != "" {
		result.Status = "stale"
		result.Reasons = append(result.Reasons, stale)
	} else if proof.active && !result.Suppressed && result.Effective == nil && start != nil && end != nil {
		result.Effective = &IntroInterval{StartTicks: *start, EndTicks: *end, Provenance: "Detected"}
	}
	return result, nil
}

func analysisDetectionReferencesCurrent(ctx context.Context, tx pgx.Tx, access libraryAccess, target AnalysisSource, cohort string, maxBytes int64, allowEmpty bool) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT d.source_item_id,d.source_revision,d.hierarchy_revision,d.episode_key,
  CASE WHEN i.id IS NULL OR NOT($2 OR i.library_id=ANY($3::text[])) OR NOT (`+access.directSQL("i")+`) THEN '' ELSE `+introSourceRevisionSQL+` END,
  CASE WHEN i.id IS NULL THEN '' ELSE `+analysisHierarchyRevisionSQL+` END
  FROM analysis_detection_sources d LEFT JOIN items i ON i.id=d.source_item_id LEFT JOIN items p ON p.id=i.parent_id AND p.library_id=i.library_id LEFT JOIN items s ON s.id=p.parent_id AND s.library_id=i.library_id WHERE d.item_id=$1`, target.ItemID, access.all, access.folders)
	if err != nil {
		return false, err
	}
	count := 0
	valid := true
	for rows.Next() {
		var id, expected, hierarchy, episode, now, currentHierarchy string
		if err := rows.Scan(&id, &expected, &hierarchy, &episode, &now, &currentHierarchy); err != nil {
			rows.Close()
			return false, err
		}
		count++
		valid = valid && expected == now && hierarchy == currentHierarchy
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if !valid || count == 0 && !allowEmpty || count > 32 {
		return false, nil
	}
	if target.EpisodeKey == "" {
		return analysisCohortHash([]AnalysisSource{target}) == cohort, nil
	}
	rows, err = tx.Query(ctx, `SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+` WHERE i.library_id=$1 AND i.parent_id=$2 AND `+analysisPhysicalSQL+` ORDER BY i.id LIMIT 100001`, target.LibraryID, target.SeasonID)
	if err != nil {
		return false, err
	}
	population := []AnalysisSource{}
	count = 0
	for rows.Next() {
		count++
		source, err := scanAnalysisSource(rows)
		if err != nil {
			rows.Close()
			return false, err
		}
		if source.Size <= maxBytes && analysisCohortKey(source) == analysisCohortKey(target) {
			population = append(population, source)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	return count <= analysisMaximumAdmissionSources && analysisCohortHash(population) == cohort, nil
}

func (s *Store) validateAnalysisDetectionSources(ctx context.Context, subject *Subject, actor *identity.Principal, itemID string) error {
	if subject == nil && actor == nil {
		return ErrInvalidInput
	}
	// Collect proof identities before filesystem work and compare each fresh source
	// afterward. No SQL connection remains held while network storage may block.
	rows, err := s.pool.Query(ctx, `SELECT source_item_id,source_revision,hierarchy_revision FROM analysis_detection_sources WHERE item_id=$1 ORDER BY source_item_id LIMIT 33`, itemID)
	if err != nil {
		return err
	}
	expected := []AnalysisSource{}
	for rows.Next() {
		var source AnalysisSource
		if err := rows.Scan(&source.ItemID, &source.SourceRevision, &source.HierarchyRevision); err != nil {
			rows.Close()
			return err
		}
		expected = append(expected, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(expected) == 0 || len(expected) > 32 {
		return ErrAnalysisSourceChanged
	}
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
		if source.SourceRevision != current.SourceRevision || source.HierarchyRevision != current.HierarchyRevision {
			return ErrAnalysisSourceChanged
		}
	}
	return nil
}

// ResolveAnalysisIntroFor adds qualified detection only after the target and all
// independent support sources pass actual containment, identity and current ACL
// checks. Explicit administrator and chapter markers retain precedence.
func (s *Store) ResolveAnalysisIntroFor(ctx context.Context, subject Subject, itemID, sourceID string) (*IntroInterval, error) {
	return s.resolveAnalysisIntro(ctx, subject, itemID, sourceID, "")
}

func (s *Store) ResolveAnalysisIntroForRevision(ctx context.Context, subject Subject, itemID, sourceID, expectedRevision string) (*IntroInterval, error) {
	if !analysisOpaque(expectedRevision, 256) {
		return nil, ErrInvalidInput
	}
	return s.resolveAnalysisIntro(ctx, subject, itemID, sourceID, expectedRevision)
}

func (s *Store) resolveAnalysisIntro(ctx context.Context, subject Subject, itemID, sourceID, expectedRevision string) (*IntroInterval, error) {
	source, err := s.analysisCurrentSourceFor(ctx, subject, itemID, sourceID)
	if err != nil {
		return nil, err
	}
	if expectedRevision != "" && source.SourceRevision != expectedRevision {
		return nil, ErrAnalysisSourceChanged
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	current, err := readAnalysisSourceUsing(ctx, tx, access, itemID, false)
	if err != nil {
		return nil, err
	}
	if !sameAnalysisSource(source, current) {
		return nil, ErrAnalysisSourceChanged
	}
	result, err := readAnalysisDetection(ctx, tx, access, current)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if result.Effective == nil || result.Effective.Provenance != "Detected" {
		return result.Effective, nil
	}
	if err := s.validateAnalysisDetectionSources(ctx, &subject, nil, itemID); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, nil
	}
	tx, access, err = s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	current, err = readAnalysisSourceUsing(ctx, tx, access, itemID, false)
	if err != nil {
		return nil, err
	}
	if !sameAnalysisSource(source, current) {
		return nil, ErrAnalysisSourceChanged
	}
	final, err := readAnalysisDetection(ctx, tx, access, current)
	if err != nil {
		return nil, err
	}
	if final.Revision != result.Revision {
		return nil, ErrAnalysisConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return final.Effective, nil
}
