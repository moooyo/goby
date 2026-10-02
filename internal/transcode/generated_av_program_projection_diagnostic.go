package transcode

import (
	"encoding/json"
	"reflect"
)

// The observed TS projection has one program containing only duplicate stream
// projections. It supplies no new clock authority. Each nested stream must have
// exactly the corresponding top-level keys and values, with unique indices.
// This contract belongs only to the new decoded observation domain.
func validateGeneratedAVObservedProgram(programs, groups, streams []json.RawMessage) error {
	if len(programs) != 1 || len(groups) != 0 || len(streams) != 2 {
		return ErrTimelineProbe
	}
	var program struct {
		Streams []json.RawMessage `json:"streams"`
	}
	if generatedBoundsDecodeRecord(programs[0], &program, "streams") != nil || len(program.Streams) != 2 {
		return ErrTimelineProbe
	}
	var top [2]map[string]json.RawMessage
	var indices [2]int64
	for index, raw := range streams {
		if !generatedAVObservationNoNullFields(raw) {
			return ErrTimelineProbe
		}
		if json.Unmarshal(raw, &top[index]) != nil {
			return ErrTimelineProbe
		}
		var selected struct {
			Index *int64 `json:"index"`
		}
		if json.Unmarshal(raw, &selected) != nil || selected.Index == nil || *selected.Index < 0 || *selected.Index > 7 {
			return ErrTimelineProbe
		}
		indices[index] = *selected.Index
	}
	if indices[0] == indices[1] {
		return ErrTimelineProbe
	}
	var seen [2]bool
	for _, raw := range program.Streams {
		if !generatedAVObservationNoNullFields(raw) {
			return ErrTimelineProbe
		}
		var nested map[string]json.RawMessage
		var selected struct {
			Index *int64 `json:"index"`
		}
		if json.Unmarshal(raw, &nested) != nil || json.Unmarshal(raw, &selected) != nil || selected.Index == nil {
			return ErrTimelineProbe
		}
		index := -1
		for candidate, value := range indices {
			if value == *selected.Index {
				index = candidate
			}
		}
		if index < 0 || seen[index] || len(nested) != len(top[index]) {
			return ErrTimelineProbe
		}
		seen[index] = true
		for key, left := range top[index] {
			right, present := nested[key]
			if !present {
				return ErrTimelineProbe
			}
			var leftValue, rightValue any
			if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil || !reflect.DeepEqual(leftValue, rightValue) {
				return ErrTimelineProbe
			}
		}
	}
	if !seen[0] || !seen[1] {
		return ErrTimelineProbe
	}
	return nil
}
