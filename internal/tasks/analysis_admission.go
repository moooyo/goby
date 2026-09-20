package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/moooyo/goby/internal/library"
)

const analysisConcurrencyGroup = "media.analysis"

var analysisFingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// AnalysisAdmissionRequest is a detached admission snapshot. A nil Actor is
// supplied only for an explicit scheduler source, never for a manual caller.
type AnalysisAdmissionRequest struct {
	TaskKey   string
	Source    string
	Selection library.AnalysisSelection
	Actor     *Actor
}

// AnalysisAdmissionBinding captures a configuration under the caller's owned
// transaction. Prepare must not publish side effects. Bind persists the captured
// profile only for a new run, before SnapshotChildren creates bounded cohorts or
// source chunks. Every callback uses this same transaction and must be bounded.
type AnalysisAdmissionBinding struct {
	ConfigurationFingerprint string
	Bind                     func(library.OwnedTx, string) error
	SnapshotChildren         func(library.OwnedTx, string) (int64, error)
}

func isAnalysisTask(key string) bool {
	return key == library.TaskIntroAnalysisKey || key == library.TaskPreviewGenerationKey
}

func cloneAnalysisSelection(input *library.AnalysisSelection) *library.AnalysisSelection {
	if input == nil {
		return nil
	}
	return &library.AnalysisSelection{LibraryIDs: append([]string{}, input.LibraryIDs...), ItemIDs: append([]string{}, input.ItemIDs...), Force: input.Force}
}

func normalizedTaskAnalysis(key string, input *library.AnalysisSelection) (*library.AnalysisSelection, error) {
	if !isAnalysisTask(key) {
		if input != nil {
			return nil, &ValidationError{Fields: map[string]string{"AnalysisInput": "Only analysis tasks accept a target selection."}}
		}
		return nil, nil
	}
	value := library.AnalysisSelection{}
	if input != nil {
		value = *input
	}
	value, err := library.NormalizeAnalysisSelection(value)
	if err != nil {
		return nil, &ValidationError{Fields: map[string]string{"AnalysisInput": "Supply bounded, unique library and media item identifiers."}}
	}
	return &value, nil
}

func taskRequestFingerprint(request StartRequest, key, source string, input *library.AnalysisSelection) [32]byte {
	// Do not add a null field to the historical three-field byte contract.
	if input == nil {
		encoded, _ := json.Marshal(struct{ TaskID, Executor, Source string }{request.TaskID, key, source})
		return sha256.Sum256(encoded)
	}
	encoded, _ := json.Marshal(struct {
		TaskID        string
		Executor      string
		Source        string
		AnalysisInput *library.AnalysisSelection
	}{request.TaskID, key, source, input})
	return sha256.Sum256(encoded)
}

func sameAnalysisInput(first, second *library.AnalysisSelection) bool {
	left, _ := json.Marshal(first)
	right, _ := json.Marshal(second)
	return bytes.Equal(left, right)
}

func analysisDatabaseInput(input *library.AnalysisSelection) any {
	if input == nil {
		return nil
	}
	encoded, _ := json.Marshal(input)
	return encoded
}

type analysisLibrary struct{ ID, Name string }

// Validate physical membership before preparing a profile or coalescing a new
// request. These queries do not open storage or invent a cohort/source result.
func analysisLibraries(tx library.OwnedTx, input *library.AnalysisSelection) ([]analysisLibrary, error) {
	if input == nil {
		return nil, ErrInvalidInput
	}
	rows, err := tx.Query(`SELECT id,name,collection_type FROM libraries
		WHERE cardinality($1::text[])=0 OR id=ANY($1::text[]) ORDER BY id`, input.LibraryIDs)
	if err != nil {
		return nil, err
	}
	available := make(map[string]analysisLibrary)
	for rows.Next() {
		var entry analysisLibrary
		var kind string
		if err := rows.Scan(&entry.ID, &entry.Name, &kind); err != nil {
			rows.Close()
			return nil, err
		}
		eligible := entry.ID != library.CollectionsLibraryID && (kind == "movies" || kind == "tvshows" || kind == "mixed")
		if !eligible && len(input.LibraryIDs) != 0 {
			rows.Close()
			return nil, ErrInvalidInput
		}
		if eligible {
			available[entry.ID] = entry
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(input.LibraryIDs) != 0 && len(available) != len(input.LibraryIDs) {
		return nil, ErrInvalidInput
	}
	selected := available
	if len(input.ItemIDs) != 0 {
		rows, err := tx.Query(`SELECT i.id,i.library_id,i.type,i.is_folder,COALESCE(r.id,''),l.name,l.collection_type
			FROM items i JOIN libraries l ON l.id=i.library_id
			LEFT JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
			WHERE i.id=ANY($1::text[]) ORDER BY i.id`, input.ItemIDs)
		if err != nil {
			return nil, err
		}
		selected = make(map[string]analysisLibrary)
		count := 0
		for rows.Next() {
			var id, libraryID, kind, root, name, collection string
			var folder bool
			if err := rows.Scan(&id, &libraryID, &kind, &folder, &root, &name, &collection); err != nil {
				rows.Close()
				return nil, err
			}
			_, allowed := available[libraryID]
			if !allowed || folder || root == "" || (kind != "Movie" && kind != "Episode" && kind != "Video") {
				rows.Close()
				return nil, ErrInvalidInput
			}
			selected[libraryID] = analysisLibrary{ID: libraryID, Name: name}
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if count != len(input.ItemIDs) {
			return nil, ErrInvalidInput
		}
	}
	result := make([]analysisLibrary, 0, len(selected))
	for _, entry := range selected {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (s *Store) prepareAnalysisAdmission(tx library.OwnedTx, key, source string, input *library.AnalysisSelection, actor *Actor) (AnalysisAdmissionBinding, []analysisLibrary, error) {
	if !isAnalysisTask(key) {
		return AnalysisAdmissionBinding{}, nil, nil
	}
	libraries, err := analysisLibraries(tx, input)
	if err != nil {
		return AnalysisAdmissionBinding{}, nil, err
	}
	entry, found := s.executors.lookup(key)
	if !found || entry.AnalysisAdmission == nil {
		return AnalysisAdmissionBinding{}, nil, ErrUnavailable
	}
	var caller *Actor
	if actor != nil {
		copy := *actor
		caller = &copy
	}
	binding, err := entry.AnalysisAdmission(tx, AnalysisAdmissionRequest{TaskKey: key, Source: source, Selection: *cloneAnalysisSelection(input), Actor: caller})
	if err != nil {
		return AnalysisAdmissionBinding{}, nil, err
	}
	if !analysisFingerprintPattern.MatchString(binding.ConfigurationFingerprint) || binding.Bind == nil {
		return AnalysisAdmissionBinding{}, nil, fmt.Errorf("%w: analysis profile binding is incomplete", ErrInvalidInput)
	}
	return binding, libraries, nil
}

func (s *Store) bindAnalysisChildren(tx library.OwnedTx, runID, key string, input *library.AnalysisSelection, binding AnalysisAdmissionBinding, libraries []analysisLibrary) (int64, error) {
	if !isAnalysisTask(key) {
		return s.snapshotChildren(tx, runID, key)
	}
	if err := binding.Bind(tx, runID); err != nil {
		return 0, err
	}
	var count int64
	if binding.SnapshotChildren != nil {
		value, err := binding.SnapshotChildren(tx, runID)
		if err != nil {
			return 0, err
		}
		count = value
	} else {
		for index, entry := range libraries {
			if _, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key)
				VALUES(md5($1 || ':' || $2),$1,$2,$3,$4,$5)`, runID, entry.ID, entry.Name, index, "library:"+entry.ID); err != nil {
				return 0, err
			}
		}
		count = int64(len(libraries))
	}
	ids := make([]string, 0, len(libraries))
	for _, entry := range libraries {
		ids = append(ids, entry.ID)
	}
	var actual int64
	var invalid bool
	if err := tx.QueryRow(`SELECT count(*),COALESCE(bool_or(NOT (library_id=ANY($2::text[])) OR analysis_scope_key=''
		OR state<>'waiting' OR executor_token IS NOT NULL OR scan_job_id IS NOT NULL),false)
		FROM task_run_children WHERE run_id=$1`, runID, ids).Scan(&actual, &invalid); err != nil {
		return 0, err
	}
	if invalid || count < 0 || actual != count {
		return 0, ErrInconsistent
	}
	entry, _ := s.executors.lookup(key)
	if count == 0 && !entry.Executor.Available() {
		_, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key,state,error_code,error_message,finished_at)
			VALUES(md5($1 || ':unavailable'),$1,'','Executor availability',0,'availability','unavailable',
			'executor_unavailable','The configured executor is currently unavailable.',clock_timestamp())`, runID)
		return 1, err
	}
	return count, nil
}
