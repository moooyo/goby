package transcode

import (
	"bytes"
	"encoding/json"
)

const (
	maxGeneratedSourceProjectedSideDataBytes = 512
	maxGeneratedSourceProjectedSideDataItems = 16
)

// FFprobe can retain side_data_list:[{}] when every frame-side-data field was
// explicitly excluded. Empty projected wrappers add no timing or completion
// evidence. Only bounded arrays of empty objects qualify; actual side-data
// fields, nulls and other JSON shapes remain outside the source-proof schema.
func decodeGeneratedSourceFrameProjection(data []byte, frame *generatedSourceRangeFrame) error {
	if err := generatedBoundsDecodeRecord(data, frame, "media_type", "stream_index", "pts", "duration", "side_data_list"); err != nil {
		return err
	}
	if len(frame.ProjectedSideData) == 0 {
		return nil
	}
	if len(frame.ProjectedSideData) > maxGeneratedSourceProjectedSideDataBytes {
		return ErrTimelineLimit
	}
	projected := bytes.TrimSpace(frame.ProjectedSideData)
	if len(projected) < 2 || projected[0] != '[' || projected[len(projected)-1] != ']' {
		return ErrTimelineProbe
	}
	var wrappers []json.RawMessage
	if err := json.Unmarshal(projected, &wrappers); err != nil {
		return ErrTimelineProbe
	}
	if len(wrappers) > maxGeneratedSourceProjectedSideDataItems {
		return ErrTimelineLimit
	}
	for _, raw := range wrappers {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil || len(object) != 0 {
			return ErrTimelineProbe
		}
	}
	return nil
}
