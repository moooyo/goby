package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func analysisGeometryTestMatrix(clockwise int) string {
	matrices := map[int][9]int64{
		0:   {65536, 0, 0, 0, 65536, 0, 0, 0, 1073741824},
		90:  {0, 65536, 0, -65536, 0, 0, 0, 0, 1073741824},
		180: {-65536, 0, 0, 0, -65536, 0, 0, 0, 1073741824},
		270: {0, -65536, 0, 65536, 0, 0, 0, 0, 1073741824},
	}
	values := matrices[clockwise]
	var text strings.Builder
	for row := 0; row < 3; row++ {
		fmt.Fprintf(&text, "\n%08d: %11d %11d %11d", row, values[row*3], values[row*3+1], values[row*3+2])
	}
	return text.String() + "\n"
}

func analysisGeometryTestDocument(t *testing.T, expected Stream, sar string, clockwise int) []byte {
	t.Helper()
	index := expected.Index
	ratio, err := json.Marshal(sar)
	if err != nil {
		t.Fatal(err)
	}
	stream := analysisGeometryStream{Index: &index, CodecType: "video", Width: expected.Width, Height: expected.Height,
		TimeBase: expected.TimeBase, SampleAspectRatio: ratio}
	if clockwise >= 0 {
		rotation := -clockwise
		if rotation < -180 {
			rotation += 360
		}
		stream.SideData = []analysisGeometrySideData{{Type: "Display Matrix", Matrix: analysisGeometryTestMatrix(clockwise), Rotation: &rotation}}
	}
	data, err := json.Marshal(analysisGeometryDocument{Streams: []analysisGeometryStream{stream}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAnalysisGeometryPreservesExactDARAcrossRotationsAndSAR(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	stream.Width, stream.Height = 1440, 1080
	for _, fixture := range []struct {
		clockwise, width, height, previewHeight int
		sarN, sarD                              int64
		filter                                  string
	}{
		{0, 1440, 1080, 180, 4, 3, ""},
		{90, 1080, 1440, 569, 3, 4, "transpose=clock,"},
		{180, 1440, 1080, 180, 4, 3, "hflip,vflip,"},
		{270, 1080, 1440, 569, 3, 4, "transpose=cclock,"},
	} {
		t.Run(fmt.Sprint(fixture.clockwise), func(t *testing.T) {
			geometry, err := parseAnalysisGeometry(bytes.NewReader(analysisGeometryTestDocument(t, stream, "4:3", fixture.clockwise)), stream, DefaultAnalysisLimits())
			if err != nil || geometry.sourceSARUnknown || geometry.width != fixture.width || geometry.height != fixture.height || geometry.sarNumerator != fixture.sarN || geometry.sarDenominator != fixture.sarD || geometry.filter() != fixture.filter {
				t.Fatalf("geometry = %+v, %v", geometry, err)
			}
			height, err := geometry.previewHeight(320, DefaultAnalysisLimits())
			if err != nil || height != fixture.previewHeight {
				t.Fatalf("display-ratio height = %d, %v", height, err)
			}
		})
	}
	stream.Width, stream.Height = 720, 576
	for _, fixture := range []struct {
		sar    string
		height int
	}{{"16:15", 240}, {"64:45", 180}} {
		geometry, err := parseAnalysisGeometry(bytes.NewReader(analysisGeometryTestDocument(t, stream, fixture.sar, -1)), stream, DefaultAnalysisLimits())
		if err != nil {
			t.Fatal(err)
		}
		if height, err := geometry.previewHeight(320, DefaultAnalysisLimits()); err != nil || height != fixture.height {
			t.Fatalf("SAR %s height = %d, %v", fixture.sar, height, err)
		}
	}
}

func TestAnalysisGeometryRejectsUnprovenMatricesAndStreamChanges(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	valid := analysisGeometryTestDocument(t, stream, "1:1", 0)
	for _, fixture := range []struct {
		name   string
		change func(*analysisGeometryDocument)
	}{
		{"flip", func(doc *analysisGeometryDocument) {
			doc.Streams[0].SideData[0].Matrix = strings.Replace(doc.Streams[0].SideData[0].Matrix, "65536", "-65536", 1)
		}},
		{"scaled", func(doc *analysisGeometryDocument) {
			doc.Streams[0].SideData[0].Matrix = strings.ReplaceAll(doc.Streams[0].SideData[0].Matrix, "65536", "131072")
		}},
		{"inexact_rotation", func(doc *analysisGeometryDocument) {
			doc.Streams[0].SideData[0].Matrix = strings.Replace(doc.Streams[0].SideData[0].Matrix, "65536", "65535", 1)
		}},
		{"perspective", func(doc *analysisGeometryDocument) {
			doc.Streams[0].SideData[0].Matrix = strings.Replace(doc.Streams[0].SideData[0].Matrix, "1073741824", "1073741823", 1)
		}},
		{"damaged_row", func(doc *analysisGeometryDocument) {
			doc.Streams[0].SideData[0].Matrix = strings.Replace(doc.Streams[0].SideData[0].Matrix, "00000001:", "00000003:", 1)
		}},
		{"missing_matrix", func(doc *analysisGeometryDocument) { doc.Streams[0].SideData[0].Matrix = "" }},
		{"missing_angle", func(doc *analysisGeometryDocument) { doc.Streams[0].SideData[0].Rotation = nil }},
		{"contradictory_angle", func(doc *analysisGeometryDocument) { value := 90; doc.Streams[0].SideData[0].Rotation = &value }},
		{"duplicate_matrix", func(doc *analysisGeometryDocument) {
			doc.Streams[0].SideData = append(doc.Streams[0].SideData, doc.Streams[0].SideData[0])
		}},
		{"wrong_side_data", func(doc *analysisGeometryDocument) { doc.Streams[0].SideData[0].Type = "unknown" }},
		{"unproven_legacy_tag", func(doc *analysisGeometryDocument) { doc.Streams[0].SideData = nil; doc.Streams[0].Tags.Rotate = "90" }},
		{"zero_denominator", func(doc *analysisGeometryDocument) { doc.Streams[0].SampleAspectRatio = json.RawMessage(`"1:0"`) }},
		{"negative_sar", func(doc *analysisGeometryDocument) { doc.Streams[0].SampleAspectRatio = json.RawMessage(`"-1:1"`) }},
		{"wrong_dimensions", func(doc *analysisGeometryDocument) { doc.Streams[0].Width++ }},
		{"wrong_time_base", func(doc *analysisGeometryDocument) { doc.Streams[0].TimeBase = "1/90000" }},
		{"wrong_index", func(doc *analysisGeometryDocument) { index := 1; doc.Streams[0].Index = &index }},
		{"duplicate_stream", func(doc *analysisGeometryDocument) { doc.Streams = append(doc.Streams, doc.Streams[0]) }},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var document analysisGeometryDocument
			if err := json.Unmarshal(valid, &document); err != nil {
				t.Fatal(err)
			}
			fixture.change(&document)
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseAnalysisGeometry(bytes.NewReader(data), stream, DefaultAnalysisLimits()); err == nil {
				t.Fatal("unproven geometry was accepted")
			}
		})
	}
	if _, err := parseAnalysisGeometry(bytes.NewReader(append(valid, []byte("{}")...)), stream, DefaultAnalysisLimits()); err == nil {
		t.Fatal("trailing geometry document was accepted")
	}
}

func TestAnalysisGeometryUnknownSARHasAnExplicitDisplayFallback(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	stream.Width, stream.Height = 320, 240
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`"N/A"`), json.RawMessage(`"0:1"`)} {
		for _, rotation := range []int{0, 90, 180, 270} {
			var document analysisGeometryDocument
			if err := json.Unmarshal(analysisGeometryTestDocument(t, stream, "1:1", rotation), &document); err != nil {
				t.Fatal(err)
			}
			document.Streams[0].SampleAspectRatio = raw
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			geometry, err := parseAnalysisGeometry(bytes.NewReader(data), stream, DefaultAnalysisLimits())
			if err != nil || !geometry.sourceSARUnknown || geometry.sarNumerator != 1 || geometry.sarDenominator != 1 {
				t.Fatalf("unknown SAR %s rotation %d lost its source/display distinction: %+v, %v", raw, rotation, geometry, err)
			}
			want := 180
			if rotation == 90 || rotation == 270 {
				want = 320
			}
			if height, err := geometry.previewHeight(240, DefaultAnalysisLimits()); err != nil || height != want {
				t.Fatalf("unknown SAR %s rotation %d height=%d want=%d: %v", raw, rotation, height, want, err)
			}
		}
	}
	if analysisSquareGeometry(stream).sourceSARUnknown {
		t.Fatal("known square geometry was reclassified as an unknown source")
	}
}

func TestAnalysisGeometrySARFallbackRejectsMalformedEvidence(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	for _, raw := range []json.RawMessage{
		json.RawMessage(`null`), json.RawMessage(`""`), json.RawMessage(`0`), json.RawMessage(`true`), json.RawMessage(`{}`), json.RawMessage(`[]`),
		json.RawMessage(`"0/1"`), json.RawMessage(`"0:0"`), json.RawMessage(`"0:2"`), json.RawMessage(`"1:0"`),
		json.RawMessage(`"-1:1"`), json.RawMessage(`"1:-1"`), json.RawMessage(`"0:-1"`), json.RawMessage(`"-0:1"`),
		json.RawMessage(`"N/A "`), json.RawMessage(`" 0:1"`), json.RawMessage(`"1:2:3"`), json.RawMessage(`"2147483648:1"`),
	} {
		var document analysisGeometryDocument
		if err := json.Unmarshal(analysisGeometryTestDocument(t, stream, "1:1", 0), &document); err != nil {
			t.Fatal(err)
		}
		document.Streams[0].SampleAspectRatio = raw
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := parseAnalysisGeometry(bytes.NewReader(data), stream, DefaultAnalysisLimits()); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("malformed SAR %s returned %v", raw, err)
		}
	}
}

func TestAnalysisGeometryAcceptsOnlyExactOrthogonalLegacyAngles(t *testing.T) {
	for _, value := range []string{"0", "90.000000", "-180.0", "270", "-360.000"} {
		if _, err := analysisGeometryTagRotation(value); err != nil {
			t.Errorf("exact angle %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "90.5", "89.99999", "90/1", "NaN", "90e0", "450", strings.Repeat("0", 65)} {
		if _, err := analysisGeometryTagRotation(value); err == nil {
			t.Errorf("unsupported angle %q was accepted", value)
		}
	}
}

func TestAnalysisGeometryRejectsDuplicateEvidenceKeys(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	valid := string(analysisGeometryTestDocument(t, stream, "1:1", 0))
	for _, duplicate := range []string{
		strings.Replace(valid, `"sample_aspect_ratio":"1:1"`, `"sample_aspect_ratio":"4:3","sample_aspect_ratio":"1:1"`, 1),
		strings.Replace(valid, `"rotation":0`, `"rotation":90,"rotation":0`, 1),
	} {
		if duplicate == valid {
			t.Fatal("duplicate-key fixture did not change the document")
		}
		if _, err := parseAnalysisGeometry(strings.NewReader(duplicate), stream, DefaultAnalysisLimits()); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("duplicate geometry evidence returned %v", err)
		}
	}
}

func TestAnalysisGeometryMetadataBudgets(t *testing.T) {
	_, stream := visualAnalysisTestInfo()
	valid := analysisGeometryTestDocument(t, stream, "1:1", 0)
	for _, kind := range []string{"json", "matrix", "side_data"} {
		t.Run(kind, func(t *testing.T) {
			var document analysisGeometryDocument
			if err := json.Unmarshal(valid, &document); err != nil {
				t.Fatal(err)
			}
			data := valid
			if kind == "json" {
				data = append(append([]byte(nil), valid...), []byte(strings.Repeat(" ", maxAnalysisGeometryJSON))...)
			} else {
				if kind == "matrix" {
					document.Streams[0].SideData[0].Matrix = strings.Repeat("x", maxAnalysisGeometryMatrix+1)
				} else {
					document.Streams[0].SideData = make([]analysisGeometrySideData, maxAnalysisGeometrySideData+1)
				}
				var err error
				data, err = json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := parseAnalysisGeometry(bytes.NewReader(data), stream, DefaultAnalysisLimits()); !errors.Is(err, ErrAnalysisBudget) {
				t.Fatalf("geometry %s budget returned %v", kind, err)
			}
		})
	}
}

func TestAnalysisGeometryDistinguishesSourceAndFinalPixelBudgets(t *testing.T) {
	info, stream := visualAnalysisTestInfo()
	limits := DefaultAnalysisLimits()
	geometry, err := parseAnalysisGeometry(bytes.NewReader(analysisGeometryTestDocument(t, stream, "1:1", 90)), stream, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := analysisPreviewGeometryOptions(info, stream, geometry, PreviewAnalysisOptions{Width: 320}, limits)
	if err != nil || plan.width != 320 || plan.height != 569 {
		t.Fatalf("1080p intermediate raster was charged to the final pixel budget: %+v, %v", plan, err)
	}
	limits.MaxFramePixels = 320*569 - 1
	if _, err := analysisPreviewGeometryOptions(info, stream, geometry, PreviewAnalysisOptions{Width: 320}, limits); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("final pixel budget returned %v", err)
	}
	limits = DefaultAnalysisLimits()
	limits.MaxSourcePixels = int64(stream.Width*stream.Height - 1)
	if _, err := parseAnalysisGeometry(bytes.NewReader(analysisGeometryTestDocument(t, stream, "1:1", 0)), stream, limits); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("source pixel budget returned %v", err)
	}
	for _, ratio := range [][2]int64{{1, 2147483647}, {2147483647, 1}} {
		geometry := analysisDisplayGeometry{width: 1920, height: 1080, sarNumerator: ratio[0], sarDenominator: ratio[1]}
		if _, err := geometry.previewHeight(400, DefaultAnalysisLimits()); !errors.Is(err, ErrAnalysisBudget) {
			t.Fatalf("extreme SAR %v was not budget-rejected: %v", ratio, err)
		}
	}
}

func TestAnalysisGeometryBindsRotatedSourceSARAndExplicitFilter(t *testing.T) {
	info, stream := visualAnalysisTestInfo()
	limits := DefaultAnalysisLimits()
	geometry, err := parseAnalysisGeometry(bytes.NewReader(analysisGeometryTestDocument(t, stream, "4:3", 90)), stream, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := analysisVisualOptions(info, VisualAnalysisOptions{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	plan.geometry = geometry
	log, err := newAnalysisVisualLog(info, stream, plan, limits)
	if err != nil {
		t.Fatal(err)
	}
	text := visualAnalysisTestTranscript()
	text = strings.ReplaceAll(text, "s:1920x1080", "s:1080x1920")
	text = strings.ReplaceAll(text, "sar:1/1", "sar:3/4")
	if _, err := log.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	log.Close(nil)
	if err := readAnalysisVisualFrames(context.Background(), bytes.NewReader(make([]byte, 3*32*32)), log,
		func(analysisVisualFrame, []byte) error { return nil }); err != nil || log.result() != nil {
		t.Fatalf("proved rotated SAR was rejected: %v / %v", err, log.result())
	}
	args := strings.Join(buildAnalysisVisualArgs(info, stream, plan, limits), " ")
	for _, required := range []string{"-noautorotate", "transpose=clock,sidedata=mode=delete,showinfo@analysis_source", "scale=32:32:flags=area,setsar=1,format=gray"} {
		if !strings.Contains(args, required) {
			t.Errorf("missing explicit display transform %q", required)
		}
	}
}
