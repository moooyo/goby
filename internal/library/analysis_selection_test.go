package library

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalysisSelectionIsBoundedCanonicalAndDetached(t *testing.T) {
	input := AnalysisSelection{LibraryIDs: []string{"z", "a"}, ItemIDs: []string{"item-2", "item-1"}, Force: true}
	got, err := NormalizeAnalysisSelection(input)
	if err != nil || !reflect.DeepEqual(got.LibraryIDs, []string{"a", "z"}) || !reflect.DeepEqual(got.ItemIDs, []string{"item-1", "item-2"}) || !got.Force {
		t.Fatalf("selection: %+v %v", got, err)
	}
	input.LibraryIDs[0] = "changed"
	if got.LibraryIDs[1] != "z" {
		t.Fatal("normalized selection aliases caller input")
	}
	for _, invalid := range []AnalysisSelection{
		{LibraryIDs: []string{"same", "same"}}, {ItemIDs: []string{"same", "same"}}, {ItemIDs: []string{""}},
		{ItemIDs: []string{" leading"}}, {ItemIDs: []string{"embedded\x00nul"}}, {ItemIDs: []string{string([]byte{0xff})}},
		{ItemIDs: []string{strings.Repeat("x", 129)}}, {LibraryIDs: make([]string, 65)}, {ItemIDs: make([]string, 257)},
	} {
		if _, err := NormalizeAnalysisSelection(invalid); err == nil {
			t.Fatalf("invalid selection accepted: %+v", invalid)
		}
	}
}
