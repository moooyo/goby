package media

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDolbyVisionInBandParameterSetsSurviveMediaCodec(t *testing.T) {
	for _, inBand := range []bool{false, true} {
		stream := backgroundClipDolbyVisionTestStream(5)
		stream.DolbyVision.InBandParameterSets = inBand
		original := Info{Container: "mov,mp4,m4a,3gp,3g2,mj2", Streams: []Stream{stream}}
		encoded, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"InBandParameterSets"`) != inBand {
			t.Fatalf("parameter-set evidence did not preserve legacy omission: %s", encoded)
		}
		var decoded Info
		if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(original, decoded) {
			t.Fatalf("media snapshot lost parameter-set evidence: %+v, %v", decoded, err)
		}
	}
	var legacy DolbyVisionMetadata
	if err := json.Unmarshal([]byte(`{"Profile":5,"RPUPresent":true,"BLPresent":true,"RPUVerified":true,"RPUProfile":5,"ResidualDisabled":true,"RPUFrameCount":2}`), &legacy); err != nil || legacy.InBandParameterSets {
		t.Fatalf("legacy media snapshot invented in-band parameter evidence: %+v, %v", legacy, err)
	}
}
