package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/media"
)

const analysisMaximumAdmissionSources = 100000

func analysisOpaque(value string, maximum int) bool {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func analysisSHA(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// ValidateAnalysisExecutionProfile validates a closed, path-free tool inventory.
// Unavailable execution records contain no fabricated tool identities.
func ValidateAnalysisExecutionProfile(value AnalysisExecutionProfile) error {
	if value.Version != AnalysisProfileVersion {
		return ErrInvalidInput
	}
	if !value.Available {
		switch value.UnavailableReason {
		case "disabled", "not_configured", "dependencies_unavailable", "cache_unavailable", "unsupported_platform":
		default:
			return ErrInvalidInput
		}
		expected := AnalysisExecutionProfile{Version: AnalysisProfileVersion, UnavailableReason: value.UnavailableReason}
		if !reflect.DeepEqual(value, expected) {
			return ErrInvalidInput
		}
		return nil
	}
	if value.UnavailableReason != "" || !analysisSHA(value.FFmpegSHA256) || !analysisSHA(value.FFprobeSHA256) {
		return ErrInvalidInput
	}
	if value.IntroProfile != "" {
		if !analysisSHA(value.FingerprintSHA256) || !analysisOpaque(value.IntroProfile, 512) || value.DetectorVersion != introdetect.Version || value.DetectorOptions != introdetect.DefaultOptions() || value.VisualIntervalTicks != media.TicksPerSecond/2 || value.PreviewProfile != "" || len(value.PreviewWidths) != 0 {
			return ErrInvalidInput
		}
	} else if value.FingerprintSHA256 != "" || value.DetectorVersion != "" || value.DetectorOptions != (introdetect.Options{}) || value.VisualIntervalTicks != 0 || value.PreviewProfile != media.PreviewAnalysisProfile || !reflect.DeepEqual(value.PreviewWidths, []int{240, 320, 400}) {
		return ErrInvalidInput
	}
	return nil
}

// analysisStrictJSON checks exact field spelling, required fields and duplicate
// keys recursively, before typed decoding. Stored JSONB has no order contract.
func analysisStrictJSON(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var read func(reflect.Type) error
	read = func(kind reflect.Type) error {
		nullable := kind.Kind() == reflect.Pointer
		if nullable {
			kind = kind.Elem()
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if token == nil {
			if !nullable && kind.Kind() != reflect.Slice {
				return ErrInvalidInput
			}
			return nil
		}
		switch kind.Kind() {
		case reflect.Struct:
			if token != json.Delim('{') {
				return ErrInvalidInput
			}
			fields := map[string]reflect.Type{}
			for index := 0; index < kind.NumField(); index++ {
				field := kind.Field(index)
				if !field.IsExported() {
					continue
				}
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "-" {
					continue
				}
				if name == "" {
					name = field.Name
				}
				fields[name] = field.Type
			}
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				field, found := fields[name]
				if !ok || !found || seen[name] {
					return ErrInvalidInput
				}
				seen[name] = true
				if err := read(field); err != nil {
					return err
				}
			}
			if len(seen) != len(fields) {
				return ErrInvalidInput
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return ErrInvalidInput
			}
		case reflect.Slice, reflect.Array:
			if token != json.Delim('[') {
				return ErrInvalidInput
			}
			for decoder.More() {
				if err := read(kind.Elem()); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return ErrInvalidInput
			}
		default:
			if _, ok := token.(json.Delim); ok {
				return ErrInvalidInput
			}
		}
		return nil
	}
	if err := read(reflect.TypeOf(output).Elem()); err != nil {
		return ErrInvalidInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidInput
	}
	if err := json.Unmarshal(raw, output); err != nil {
		return ErrInvalidInput
	}
	return nil
}

func analysisAdmissionFingerprint(profile AnalysisProfile, execution AnalysisExecutionProfile, revision, epoch int64) string {
	raw, _ := json.Marshal(struct {
		Version         int
		Revision, Epoch int64
		Profile         AnalysisProfile
		Execution       AnalysisExecutionProfile
	}{AnalysisProfileVersion, revision, epoch, profile, execution})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// ValidateStoredAnalysisAdmission validates preserved historical profiles without
// consulting current tool paths or requiring an old worker to remain authorized.
func ValidateStoredAnalysisAdmission(profileRaw, executionRaw []byte, revision, epoch int64, fingerprint string) error {
	var profile AnalysisProfile
	var execution AnalysisExecutionProfile
	if len(profileRaw) > 32768 || len(executionRaw) > 32768 || revision < 1 || epoch < 1 || analysisStrictJSON(profileRaw, &profile) != nil || analysisStrictJSON(executionRaw, &execution) != nil || ValidateAnalysisProfile(profile) != nil || ValidateAnalysisExecutionProfile(execution) != nil || analysisAdmissionFingerprint(profile, execution, revision, epoch) != fingerprint {
		return ErrInvalidInput
	}
	return nil
}

// PrepareAnalysis performs only SQL and pure computation. Both returned closures
// must run inside the same task admission transaction that called this method.
func PrepareAnalysis(tx OwnedTx, taskKey string, selection AnalysisSelection, execution AnalysisExecutionProfile) (AnalysisAdmissionBinding, error) {
	if tx == nil || taskKey != TaskIntroAnalysisKey && taskKey != TaskPreviewGenerationKey {
		return AnalysisAdmissionBinding{}, ErrInvalidInput
	}
	selection, err := NormalizeAnalysisSelection(selection)
	if err != nil {
		return AnalysisAdmissionBinding{}, err
	}
	if err := ValidateAnalysisExecutionProfile(execution); err != nil {
		return AnalysisAdmissionBinding{}, err
	}
	if execution.Available && ((taskKey == TaskIntroAnalysisKey) != (execution.IntroProfile != "")) {
		return AnalysisAdmissionBinding{}, ErrInvalidInput
	}
	configuration, err := scanAnalysisConfiguration(tx.QueryRow(`SELECT ` + analysisConfigurationColumns + ` FROM analysis_settings WHERE id=1 FOR SHARE`))
	if err != nil {
		return AnalysisAdmissionBinding{}, err
	}
	revision, _ := strconv.ParseInt(configuration.Revision, 10, 64)
	var epoch int64
	if err := tx.QueryRow(`SELECT publication_epoch FROM analysis_settings WHERE id=1`).Scan(&epoch); err != nil {
		return AnalysisAdmissionBinding{}, err
	}
	execution.PreviewWidths = append([]int(nil), execution.PreviewWidths...)
	fingerprint := analysisAdmissionFingerprint(configuration.Profile, execution, revision, epoch)
	profileRaw, _ := json.Marshal(configuration.Profile)
	executionRaw, _ := json.Marshal(execution)
	binding := AnalysisAdmissionBinding{ConfigurationFingerprint: fingerprint}
	binding.Bind = func(target OwnedTx, runID string) error {
		if target != tx || !analysisOpaque(runID, 128) {
			return ErrInvalidInput
		}
		_, err := target.Exec(`INSERT INTO analysis_run_profiles(run_id,configuration_revision,publication_epoch,fingerprint,profile,execution) VALUES($1,$2,$3,$4,$5,$6)`, runID, revision, epoch, fingerprint, profileRaw, executionRaw)
		return err
	}
	binding.SnapshotChildren = func(target OwnedTx, runID string) (int64, error) {
		if target != tx {
			return 0, ErrInvalidInput
		}
		return snapshotAnalysisChildren(target, runID, taskKey, selection, configuration.Profile)
	}
	return binding, nil
}

func analysisCohortHash(sources []AnalysisSource) string {
	type fact struct{ ItemID, Source, Hierarchy, Episode string }
	facts := make([]fact, len(sources))
	for index, source := range sources {
		facts[index] = fact{source.ItemID, source.SourceRevision, source.HierarchyRevision, source.EpisodeKey}
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ItemID < facts[j].ItemID })
	raw, _ := json.Marshal(facts)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func analysisCohortKey(source AnalysisSource) string {
	if source.EpisodeKey == "" {
		return "item:" + source.ItemID
	}
	raw, _ := json.Marshal([]string{"season", source.LibraryID, source.SeriesID, source.SeasonID})
	return string(raw)
}

func snapshotAnalysisChildren(tx OwnedTx, runID, taskKey string, selection AnalysisSelection, profile AnalysisProfile) (total int64, resultErr error) {
	refresh, restore, err := analysisAdmissionSQLBudget(tx)
	if err != nil {
		return 0, err
	}
	defer func() {
		if resultErr == nil {
			resultErr = restore()
		}
	}()
	if err := refresh(); err != nil {
		return 0, err
	}
	// One bounded population read also includes support episodes for a leaf request.
	rows, err := tx.Query(`SELECT `+analysisSourceColumns+` FROM items i `+analysisSourceJoins+`
  JOIN libraries l ON l.id=i.library_id WHERE `+analysisPhysicalSQL+`
  AND l.collection_type IN ('movies','tvshows','mixed') AND (cardinality($1::text[])=0 OR i.library_id=ANY($1))
  ORDER BY i.library_id,COALESCE(p.parent_id,''),COALESCE(i.parent_id,''),i.index_number NULLS LAST,i.id LIMIT 100001`, selection.LibraryIDs)
	if err != nil {
		return 0, err
	}
	sources := make([]AnalysisSource, 0)
	count := 0
	for rows.Next() {
		count++
		source, err := scanAnalysisSource(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		if source.Size <= profile.MaxSourceBytes {
			sources = append(sources, source)
		}
		if count > analysisMaximumAdmissionSources {
			rows.Close()
			return 0, fmt.Errorf("%w: analysis admission population limit", ErrInvalidInput)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	selected := map[string]bool{}
	for _, id := range selection.ItemIDs {
		selected[id] = false
	}
	groups := map[string][]AnalysisSource{}
	order := []string{}
	for _, source := range sources {
		_, explicit := selected[source.ItemID]
		source.Target = len(selected) == 0 || explicit
		if explicit {
			selected[source.ItemID] = true
		}
		if taskKey == TaskIntroAnalysisKey && len(selected) == 0 && source.ItemType != "Episode" {
			continue
		}
		key := analysisCohortKey(source)
		if taskKey == TaskPreviewGenerationKey {
			key = "item:" + source.ItemID
		}
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], source)
	}
	for _, found := range selected {
		if !found {
			return 0, ErrNotFound
		}
	}
	ordinal := int64(0)
	for _, key := range order {
		population := groups[key]
		cohort := analysisCohortHash(population)
		targets := []int{}
		for index, source := range population {
			if source.Target {
				targets = append(targets, index)
			}
		}
		for first := 0; first < len(targets); {
			last := first + 1
			if taskKey == TaskIntroAnalysisKey {
				for last < len(targets) && last-first < 16 && targets[last]-targets[first] < 16 {
					last++
				}
			}
			start := max(0, targets[first]-8)
			end := min(len(population), start+32)
			start = max(0, end-32)
			if taskKey == TaskPreviewGenerationKey {
				start = targets[first]
				end = start + 1
			}
			window := append([]AnalysisSource(nil), population[start:end]...)
			targetSet := map[string]bool{}
			for _, index := range targets[first:last] {
				targetSet[population[index].ItemID] = true
			}
			for index := range window {
				window[index].Position = index
				window[index].Target = targetSet[window[index].ItemID]
			}
			reason := ""
			if taskKey == TaskIntroAnalysisKey {
				if population[targets[first]].ItemType != "Episode" {
					reason = "unsupported_item_type"
				} else if population[targets[first]].EpisodeKey == "" {
					reason = "unsupported_hierarchy"
				} else {
					independent := map[string]bool{}
					for _, source := range window {
						independent[source.EpisodeKey] = true
					}
					if len(independent) < 3 {
						reason = "insufficient_cohort"
					}
				}
			}
			scopeDigest := sha256.Sum256([]byte(key + ":" + strconv.Itoa(first)))
			scope := "analysis:" + hex.EncodeToString(scopeDigest[:])
			childDigest := sha256.Sum256([]byte(runID + ":" + scope))
			childID := hex.EncodeToString(childDigest[:16])
			libraryID := window[0].LibraryID
			if err := refresh(); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key)
    VALUES($1,$2,$3,(SELECT name FROM libraries WHERE id=$3),$4,$5)`, childID, runID, libraryID, ordinal, scope); err != nil {
				return 0, err
			}
			if err := refresh(); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(`INSERT INTO analysis_work(child_id,run_id,task_key,library_id,scope_key,cohort_revision,force,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, childID, runID, taskKey, libraryID, scope, cohort, selection.Force, reason); err != nil {
				return 0, err
			}
			if err := refresh(); err != nil {
				return 0, err
			}
			encoded, _ := json.Marshal(window)
			if _, err := tx.Exec(`INSERT INTO analysis_work_sources(child_id,item_id,position,target,library_id,root_id,series_id,season_id,episode_key,item_type,source_revision,hierarchy_revision,duration_ticks,size,manual_revision,decision_revision,preview_revision)
 SELECT $1,v."ItemId",v."Position",v."Target",v."LibraryId",v."RootId",v."SeriesId",v."SeasonId",v."EpisodeKey",v."ItemType",v."SourceRevision",v."HierarchyRevision",v."DurationTicks",v."Size",v."ManualRevision"::bigint,v."DecisionRevision"::bigint,v."PreviewRevision"::bigint
 FROM jsonb_to_recordset($2::jsonb) v("ItemId" text,"Position" integer,"Target" boolean,"LibraryId" text,"RootId" text,"SeriesId" text,"SeasonId" text,"EpisodeKey" text,"ItemType" text,"SourceRevision" text,"HierarchyRevision" text,"DurationTicks" bigint,"Size" bigint,"ManualRevision" text,"DecisionRevision" text,"PreviewRevision" text)`, childID, encoded); err != nil {
				return 0, err
			}
			ordinal++
			first = last
		}
	}
	return ordinal, nil
}

// Preserve rollback time on the reserved owner connection. Recalculate at every
// potentially waiting statement rather than reusing the initial time budget.
func analysisAdmissionSQLBudget(tx OwnedTx) (func() error, func() error, error) {
	view, ok := tx.(*ownedCallbackTx)
	if !ok || view == nil {
		return nil, nil, ErrInvalidInput
	}
	var previous string
	if err := tx.QueryRow(`SELECT current_setting('statement_timeout')`).Scan(&previous); err != nil {
		return nil, nil, err
	}
	refresh := func() error {
		budget := 5 * time.Second
		if deadline, ok := view.ctx.Deadline(); ok {
			budget = min(budget, time.Until(deadline)-2*time.Second)
		}
		if budget < time.Millisecond {
			return fmt.Errorf("%w: analysis admission SQL budget exhausted", ErrUnavailable)
		}
		_, err := tx.Exec(`SELECT set_config('statement_timeout',CASE WHEN setting::bigint=0 OR setting::bigint>$1::bigint THEN $2 ELSE current_setting('statement_timeout') END,true) FROM pg_settings WHERE name='statement_timeout'`, budget.Milliseconds(), strconv.FormatInt(budget.Milliseconds(), 10)+"ms")
		return err
	}
	restore := func() error {
		_, err := tx.Exec(`SELECT set_config('statement_timeout',$1,true)`, previous)
		return err
	}
	return refresh, restore, nil
}

func analysisReadError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
