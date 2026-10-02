package transcode

import (
	"errors"
	"strings"
	"testing"
)

func generatedSourceRangeProjectedFrame(data, projection string) string {
	return strings.Replace(data, `"pts":`, `"side_data_list":`+projection+`,"pts":`, 1)
}

func TestGeneratedSourceRangeEmptyFrameSideDataProjectionPreservesExactCoverage(t *testing.T) {
	plan := generatedSourceRangeTestPlan()
	plain := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92_000, 40, 150), "1/1000", "2.000000")
	want, err := parseGeneratedSourceRange([]byte(plain), plan, 2*ticksPerSecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, projection := range []string{`[]`, `[{}]`, `[{ },{}]`} {
		got, err := parseGeneratedSourceRange([]byte(generatedSourceRangeProjectedFrame(plain, projection)), plan, 2*ticksPerSecond)
		if err != nil || got != want {
			t.Fatalf("empty frame-side-data projection changed source coverage: projection=%s result=%+v error=%v", projection, got, err)
		}
	}
	tailPlan, certificate := generatedMP4SourceTailTestPlanAndCertificate()
	tail := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(96_000, 40, 150), "1/1000", "2.000000")
	plainTail, err := parseGeneratedMP4SourceTail([]byte(tail), tailPlan, certificate)
	if err != nil {
		t.Fatal(err)
	}
	projectedTail, err := parseGeneratedMP4SourceTail([]byte(generatedSourceRangeProjectedFrame(tail, `[{}]`)), tailPlan, certificate)
	if err != nil || projectedTail != plainTail {
		t.Fatal("empty projected side-data changed the absolute declared tail endpoint")
	}
}

func TestGeneratedSourceRangeRejectsNonemptyOrMalformedFrameSideDataProjection(t *testing.T) {
	plan := generatedSourceRangeTestPlan()
	plain := generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92_000, 40, 150), "1/1000", "2.000000")
	for name, projection := range map[string]string{
		"null array": "null", "object instead of array": `{}`, "string instead of array": `"[]"`,
		"null item": `[null]`, "array item": `[[]]`, "string item": `["{}"]`, "number item": `[1]`,
		"SEI content":    `[{"side_data_type":"H.26[45] User Data Unregistered SEI message"}]`,
		"timing content": `[{"pts":92000}]`, "unknown content": `[{"eof":true}]`,
		"duplicate item key": `[{"pts":92000,"pts":92000}]`,
	} {
		t.Run(name, func(t *testing.T) {
			data := generatedSourceRangeProjectedFrame(plain, projection)
			got, err := parseGeneratedSourceRange([]byte(data), plan, 2*ticksPerSecond)
			if got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineProbe) {
				t.Fatalf("side-data projection added unrequested evidence: result=%+v error=%v", got, err)
			}
		})
	}
	for _, data := range []string{
		strings.Replace(plain, `"pts":`, `"side_data_list":[],"side_data_list":[{}],"pts":`, 1),
		strings.Replace(plain, `"pts":`, `"Side_Data_List":[{}],"pts":`, 1),
	} {
		if got, err := parseGeneratedSourceRange([]byte(data), plan, 2*ticksPerSecond); got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineProbe) {
			t.Fatal("duplicate or case-folded frame projection entered the strict schema")
		}
	}
	for _, projection := range []string{
		`[` + strings.Repeat(`{},`, maxGeneratedSourceProjectedSideDataItems) + `{}` + `]`,
		`[{` + strings.Repeat(" ", maxGeneratedSourceProjectedSideDataBytes) + `}]`,
	} {
		if got, err := parseGeneratedSourceRange([]byte(generatedSourceRangeProjectedFrame(plain, projection)), plan, 2*ticksPerSecond); got != (GeneratedSourceRange{}) || !errors.Is(err, ErrTimelineLimit) {
			t.Fatal("empty side-data projection escaped its fixed count or byte budget")
		}
	}
	// An empty wrapper cannot repair a missing coverage frame or a clock.
	for _, data := range []string{
		generatedSourceRangeDocumentJSON(generatedSourceRangeFrames(92_000, 40, 149), "1/1000", "2.000000"),
		strings.Replace(plain, `"pts":92000,`, "", 1),
	} {
		if got, err := parseGeneratedSourceRange([]byte(generatedSourceRangeProjectedFrame(data, `[{}]`)), plan, 2*ticksPerSecond); err == nil || got != (GeneratedSourceRange{}) {
			t.Fatal("empty side-data projection authorized missing clock or partial source coverage")
		}
	}
}
