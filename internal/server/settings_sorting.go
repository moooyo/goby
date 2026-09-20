package server

import (
	"encoding/json"
	"github.com/moooyo/goby/internal/settings"
)

func decodeSortingSettings(raw json.RawMessage, invalid map[string]string) *settings.Sorting {
	fields, errors := adminTaskObject(raw, []string{"SortRemoveWords"}, []string{"SortRemoveWords"}, "Sorting")
	for field, message := range errors {
		invalid[field] = message
	}
	if errors != nil {
		return nil
	}
	var value settings.Sorting
	if json.Unmarshal(fields["SortRemoveWords"], &value.SortRemoveWords) != nil || settings.ValidateStoredSorting(value) != nil {
		invalid["Sorting.SortRemoveWords"] = "Supply at most 32 distinct case-insensitive words, each 1-128 UTF-8 bytes without whitespace or control characters."
		return nil
	}
	return &value
}
