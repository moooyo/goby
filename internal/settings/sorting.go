package settings

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Sorting struct{ SortRemoveWords []string }

const FieldSorting Field = "Sorting"

func DefaultSorting() Sorting { return Sorting{SortRemoveWords: []string{}} }

// ValidateStoredSorting is shared by runtime loading and archive validation.
// Words are literal single tokens; spelling/order are retained in the DTO while
// matching and duplicate detection are case-insensitive. Empty disables removal.
func ValidateStoredSorting(value Sorting) error {
	if value.SortRemoveWords == nil || len(value.SortRemoveWords) > 32 {
		return sortingValidationError()
	}
	seen := map[string]bool{}
	for _, word := range value.SortRemoveWords {
		key := strings.ToLower(word)
		if len(word) == 0 || len(word) > 128 || !utf8.ValidString(word) || strings.IndexFunc(word, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 || seen[key] {
			return sortingValidationError()
		}
		seen[key] = true
	}
	return nil
}
func sortingValidationError() error {
	return &ValidationError{Fields: map[string]string{"Sorting.SortRemoveWords": "Supply at most 32 distinct case-insensitive words, each 1-128 UTF-8 bytes without whitespace or control characters."}}
}
func cloneSorting(value Sorting) Sorting {
	return Sorting{SortRemoveWords: slices.Clone(value.SortRemoveWords)}
}
func equalSorting(first, second Sorting) bool {
	return slices.Equal(first.SortRemoveWords, second.SortRemoveWords)
}
