package settings

import (
	"strings"
	"testing"
)

func TestSortRemoveWordsRetainsLiteralTokensAndRejectsAmbiguousRules(t *testing.T) {
	for _, words := range [][]string{{}, {"The", "Ä", "Σ", "[literal]", "中文"}, {strings.Repeat("a", 128)}} {
		if err := ValidateStoredSorting(Sorting{SortRemoveWords: words}); err != nil {
			t.Fatalf("valid rule set: %v", err)
		}
	}
	for _, words := range [][]string{nil, {""}, {"The", "the"}, {"Ä", "ä"}, {"Σ", "σ"}, {"one two"}, {"the\u00a0"}, {"a\u0080"}, {"a\n"}, {string([]byte{0xff})}, {strings.Repeat("a", 129)}, make([]string, 33)} {
		if err := ValidateStoredSorting(Sorting{SortRemoveWords: words}); err == nil {
			t.Fatalf("accepted invalid rules: %#v", words)
		}
	}
	value := Sorting{SortRemoveWords: []string{"The"}}
	copy := cloneSorting(value)
	copy.SortRemoveWords[0] = "changed"
	if value.SortRemoveWords[0] != "The" {
		t.Fatal("sorting clone retained caller-owned array")
	}
}
