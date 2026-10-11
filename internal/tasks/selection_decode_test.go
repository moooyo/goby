package tasks

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
)

type taskSelectionJSONRow string

func (row taskSelectionJSONRow) Scan(values ...any) error {
	*values[0].(*[]byte) = []byte(row)
	return nil
}

func TestTaskRunDecodeNormalizesSelectionsWithoutRetainingPreviousStorage(t *testing.T) {
	const timestamp = "2026-10-11T09:08:07+08:00"
	wantTime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		field string
		want  *library.AnalysisSelection
	}{
		{name: "absent"},
		{name: "null", field: `,"analysis_input":null`},
		{name: "empty object", field: `,"analysis_input":{}`, want: &library.AnalysisSelection{LibraryIDs: []string{}, ItemIDs: []string{}}},
		{name: "null lists", field: `,"analysis_input":{"LibraryIds":null,"ItemIds":null,"Force":true}`, want: &library.AnalysisSelection{LibraryIDs: []string{}, ItemIDs: []string{}, Force: true}},
		{name: "empty lists", field: `,"analysis_input":{"LibraryIds":[],"ItemIds":[]}`, want: &library.AnalysisSelection{LibraryIDs: []string{}, ItemIDs: []string{}}},
		{name: "stored order", field: `,"analysis_input":{"LibraryIds":["b","a"],"ItemIds":["y","x"],"Force":true}`, want: &library.AnalysisSelection{LibraryIDs: []string{"b", "a"}, ItemIDs: []string{"y", "x"}, Force: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := &library.AnalysisSelection{LibraryIDs: []string{"previous-library"}, ItemIDs: []string{"previous-item"}, Force: true}
			run := Run{AnalysisInput: previous}
			raw := `{"id":"decoded","created_at":"` + timestamp + `","scheduled_for":"` + timestamp +
				`","started_at":"` + timestamp + `","deadline_at":"` + timestamp + `","stop_requested_at":"` + timestamp +
				`","finished_at":"` + timestamp + `"` + test.field + `}`
			if err := decodeRow(taskSelectionJSONRow(raw), &run); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(run.AnalysisInput, test.want) || run.AnalysisInput == previous {
				t.Fatalf("decoded selection retained an old destination or changed its wire shape: got=%+v want=%+v", run.AnalysisInput, test.want)
			}
			if previous.LibraryIDs[0] != "previous-library" || previous.ItemIDs[0] != "previous-item" || !previous.Force {
				t.Fatal("reusing a decode destination mutated its previous selection")
			}
			for _, value := range []*time.Time{&run.CreatedAt, run.ScheduledFor, run.StartedAt, run.DeadlineAt, run.StopRequestedAt, run.FinishedAt} {
				if value == nil || value.Location() != time.UTC || !value.Equal(wantTime) {
					t.Fatalf("selection normalization changed timestamp normalization: %v", value)
				}
			}
		})
	}
}

func TestTaskRunListsNormalizeOwnedSelectionsWithoutCopyingOrAliasing(t *testing.T) {
	const rawRun = `{"id":"decoded","analysis_input":{"LibraryIds":["library"],"ItemIds":["item"]}}`
	var runs []Run
	if err := json.Unmarshal([]byte("["+rawRun+","+rawRun+"]"), &runs); err != nil {
		t.Fatal(err)
	}
	first, second := runs[0].AnalysisInput, runs[1].AnalysisInput
	libraryID, itemID := &first.LibraryIDs[0], &first.ItemIDs[0]
	normalizeRuns(runs)
	if runs[0].AnalysisInput != first || runs[1].AnalysisInput != second || &first.LibraryIDs[0] != libraryID || &first.ItemIDs[0] != itemID {
		t.Fatal("normalizing freshly decoded selections copied their owned storage")
	}
	first.LibraryIDs[0], first.ItemIDs[0] = "changed-library", "changed-item"
	if second.LibraryIDs[0] != "library" || second.ItemIDs[0] != "item" {
		t.Fatal("independently decoded run selections share mutable storage")
	}
	var definition Definition
	if err := decodeRow(taskSelectionJSONRow(`{"current_run":`+rawRun+`,"last_run":`+rawRun+`}`), &definition); err != nil {
		t.Fatal(err)
	}
	current, last := definition.CurrentRun.AnalysisInput, definition.LastRun.AnalysisInput
	current.LibraryIDs[0], current.ItemIDs[0] = "current-library", "current-item"
	if last.LibraryIDs[0] != "library" || last.ItemIDs[0] != "item" {
		t.Fatal("nested definition runs share mutable selection storage")
	}
	if err := decodeRow(taskSelectionJSONRow(`{"current_run":{"analysis_input":{}},"last_run":{"analysis_input":null}}`), &definition); err != nil {
		t.Fatal(err)
	}
	if input := definition.CurrentRun.AnalysisInput; input == nil || input.LibraryIDs == nil || input.ItemIDs == nil || len(input.LibraryIDs)+len(input.ItemIDs) != 0 {
		t.Fatalf("nested empty selection did not normalize: %+v", input)
	}
	if definition.LastRun.AnalysisInput != nil || last.LibraryIDs[0] != "library" || current.LibraryIDs[0] != "current-library" {
		t.Fatal("reusing a nested decode destination retained or mutated an earlier selection")
	}
}
