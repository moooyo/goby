package media

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProbeVersionEightRetainsHearingImpairedDisposition(t *testing.T) {
	var fixture map[string]any
	decoder := json.NewDecoder(bytes.NewReader(readProbeFixture(t)))
	decoder.UseNumber()
	if decoder.Decode(&fixture) != nil {
		t.Fatal("decode probe fixture")
	}
	streams := fixture["streams"].([]any)
	track := streams[2].(map[string]any)
	track["disposition"].(map[string]any)["hearing_impaired"] = 1
	raw, _ := json.Marshal(fixture)
	info, err := parseProbe(raw)
	if err != nil || info.ProbeVersion != CurrentProbeVersion || CurrentProbeVersion != 8 || !info.Streams[2].IsHearingImpaired || info.Streams[1].IsHearingImpaired {
		t.Fatalf("new probe did not retain measured hearing-impaired state: %v", err)
	}
	track["disposition"].(map[string]any)["hearing_impaired"] = 2
	raw, _ = json.Marshal(fixture)
	if _, err := parseProbe(raw); err == nil {
		t.Fatal("invalid hearing-impaired disposition was accepted")
	}
	var old Info
	if json.Unmarshal([]byte(`{"ProbeVersion":7,"Streams":[{"Index":2,"CodecType":"subtitle"}]}`), &old) != nil || old.ProbeVersion == (Prober{}).CacheVersion() {
		t.Fatal("a legacy cache was silently upgraded without probing the new disposition")
	}
}
