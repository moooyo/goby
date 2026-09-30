package backuppg

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
)

func TestAnalysisArchiveIntroSkipperFormatsRespectSchemaBoundary(t *testing.T) {
	for _, schema := range []int64{50, 51, 52, 53, 54} {
		for _, execution := range []int{1, 2, 3, 4, 5, 6, 7} {
			raw, _ := json.Marshal(struct{ Version int }{execution})
			want := execution <= 5 || schema >= 54 && execution == 6
			if validAnalysisStateExecutionVersion(raw, schema) != want {
				t.Fatalf("schema %d changed its execution version boundary for %d", schema, execution)
			}
		}
		if validAnalysisStateResultVersion(introskipper.Version, schema) != (schema >= 54) {
			t.Fatalf("schema %d changed its native result boundary", schema)
		}
		if !validAnalysisStateResultVersion("introdetect-v5", schema) {
			t.Fatalf("schema %d lost the retained v5 result format", schema)
		}
		for codec := uint16(1); codec <= 5; codec++ {
			payload := make([]byte, 72)
			copy(payload, "GAFB")
			binary.LittleEndian.PutUint16(payload[4:6], codec)
			want := codec <= 2 || schema >= 53 && codec == 3 || schema >= 54 && codec == 4
			if validAnalysisStateFeatureVersion(payload, schema) != want {
				t.Fatalf("schema %d changed its feature codec boundary for %d", schema, codec)
			}
		}
	}
	for _, raw := range []string{"", "null", `{}`, `{"Version":0}`, `{"Version":-1}`, `{"Version":"6"}`} {
		if validAnalysisStateExecutionVersion([]byte(raw), 54) {
			t.Fatalf("invalid execution version was accepted: %s", raw)
		}
	}
	if validAnalysisStateFeatureVersion([]byte("GAFB"), 54) {
		t.Fatal("an incomplete feature header was accepted")
	}
}

func TestAnalysisArchiveIntroSkipperSettingsRequireExactNewFieldsOnlyAtSchema54(t *testing.T) {
	current := library.DefaultAnalysisProfile()
	legacy := current
	legacy.IntroSkipper = introskipper.Options{}
	options, err := json.Marshal(current.IntroSkipper)
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []int64{50, 51, 52, 53} {
		if err := validateAnalysisStateProfile(legacy, nil, schema); err != nil {
			t.Fatalf("schema %d lost its original profile: %v", schema, err)
		}
		if validateAnalysisStateProfile(current, nil, schema) == nil || validateAnalysisStateProfile(legacy, options, schema) == nil {
			t.Fatalf("schema %d silently accepted a future profile", schema)
		}
	}
	if err := validateAnalysisStateProfile(legacy, options, 54); err != nil {
		t.Fatalf("schema 54 rejected a complete explicit profile: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(options, &fields); err != nil {
		t.Fatal(err)
	}
	for field := range fields {
		original := fields[field]
		delete(fields, field)
		raw, _ := json.Marshal(fields)
		if validateAnalysisStateProfile(legacy, raw, 54) == nil {
			t.Fatalf("schema 54 invented a default for missing %s", field)
		}
		fields[field] = original
	}
	for _, raw := range [][]byte{nil, []byte("null"), []byte("{}"), append(append([]byte(nil), options[:len(options)-1]...), []byte(`,"Unknown":1}`)...)} {
		if validateAnalysisStateProfile(legacy, raw, 54) == nil {
			t.Fatalf("schema 54 accepted malformed options: %s", raw)
		}
	}
	legacy.PreviewIntervalSeconds = 1
	if validateAnalysisStateProfile(legacy, options, 54) == nil || validateAnalysisStateProfile(legacy, nil, 53) == nil {
		t.Fatal("new options bypassed existing profile bounds")
	}
}
