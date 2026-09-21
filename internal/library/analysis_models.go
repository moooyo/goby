package library

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	TaskIntroAnalysisKey     = "media.intro_analysis"
	TaskPreviewGenerationKey = "media.preview_generation"
	AnalysisProfileVersion   = 1
	// Execution wire evolution is independent of managed configuration and CAS.
	AnalysisExecutionProfileVersion = 2
)

// AnalysisSelection is immutable admission input, not execution authority.
// Empty identifiers select all eligible libraries. Item selections restrict
// publication targets; intro matching may read other authorized cohort members
// as supporting evidence without publishing a result for those members.
type AnalysisSelection struct {
	LibraryIDs []string `json:"LibraryIds,omitempty"`
	ItemIDs    []string `json:"ItemIds,omitempty"`
	Force      bool     `json:"Force,omitempty"`
}

// NormalizeAnalysisSelection creates a deterministic, independently owned
// request snapshot. Membership and current administrator authority are checked
// in the admission transaction rather than inferred from opaque identifiers.
func NormalizeAnalysisSelection(input AnalysisSelection) (AnalysisSelection, error) {
	normalize := func(values []string, maximum int) ([]string, error) {
		if len(values) > maximum {
			return nil, fmt.Errorf("%w: too many analysis targets", ErrInvalidInput)
		}
		result := make([]string, 0, len(values))
		seen := make(map[string]bool, len(values))
		for _, value := range values {
			if value == "" || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
				return nil, fmt.Errorf("%w: invalid analysis target", ErrInvalidInput)
			}
			for _, character := range value {
				if character < 0x21 || character == 0x7f {
					return nil, fmt.Errorf("%w: invalid analysis target", ErrInvalidInput)
				}
			}
			if seen[value] {
				return nil, fmt.Errorf("%w: duplicate analysis target", ErrInvalidInput)
			}
			seen[value] = true
			result = append(result, value)
		}
		sort.Strings(result)
		return result, nil
	}
	libraries, err := normalize(input.LibraryIDs, 64)
	if err != nil {
		return AnalysisSelection{}, err
	}
	items, err := normalize(input.ItemIDs, 256)
	if err != nil {
		return AnalysisSelection{}, err
	}
	return AnalysisSelection{LibraryIDs: libraries, ItemIDs: items, Force: input.Force}, nil
}
