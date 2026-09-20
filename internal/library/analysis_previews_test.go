package library

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func analysisPreviewTestValue() (AnalysisPreview, int64) {
	return AnalysisPreview{ItemID: "preview-item", Revision: "1", SourceRevision: "source-revision",
		ProfileFingerprint: strings.Repeat("a1", 32), ProfileRevision: "2", PublicationEpoch: 3,
		CacheKey: strings.Repeat("b2", 32), Seal: strings.Repeat("c3", 32), SHA256: strings.Repeat("d4", 32),
		Width: 320, Height: 180, Bytes: 1024, FrameCount: 3, IntervalTicks: 10 * media.TicksPerSecond,
		NominalTicks: []int64{0, 10 * media.TicksPerSecond, 20 * media.TicksPerSecond},
		ActualTicks:  []int64{0, 9 * media.TicksPerSecond, 21 * media.TicksPerSecond},
		UpdatedAt:    time.Unix(1, 0).UTC()}, 25 * media.TicksPerSecond
}

func TestAnalysisPreviewEffectiveIntervalCoversLongSourcesWithoutTruncation(t *testing.T) {
	for _, example := range []struct {
		duration   int64
		configured int
		seconds    int64
	}{
		{25 * media.TicksPerSecond, 10, 10},
		{4096 * 2 * media.TicksPerSecond, 2, 2},
		{4096*2*media.TicksPerSecond + 1, 2, 3},
		{media.MaxAnalysisDurationTicks, 2, 11},
		{media.MaxAnalysisDurationTicks, 120, 120},
	} {
		interval, err := EffectiveAnalysisPreviewInterval(example.duration, example.configured)
		if err != nil || interval != example.seconds*media.TicksPerSecond || (example.duration-1)/interval+1 > 4096 {
			t.Fatalf("effective preview interval is incomplete: %+v interval%d: %v", example, interval, err)
		}
	}
	for _, example := range []struct {
		duration int64
		seconds  int
	}{
		{0, 10}, {-1, 10}, {media.MaxAnalysisDurationTicks + 1, 10}, {math.MaxInt64, 10},
		{media.TicksPerSecond, 0}, {media.TicksPerSecond, 121},
	} {
		if _, err := EffectiveAnalysisPreviewInterval(example.duration, example.seconds); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid interval inputs were admitted: %+v", example)
		}
	}
	value, _ := analysisPreviewTestValue()
	value.FrameCount, value.IntervalTicks = 4096, 2*media.TicksPerSecond
	value.Bytes = 72 + 12*int64(value.FrameCount)
	value.NominalTicks, value.ActualTicks = make([]int64, value.FrameCount), make([]int64, value.FrameCount)
	for index := range value.NominalTicks {
		value.NominalTicks[index] = int64(index) * value.IntervalTicks
		value.ActualTicks[index] = value.NominalTicks[index]
	}
	if err := ValidateStoredAnalysisPreview(value, 4096*value.IntervalTicks); err != nil {
		t.Fatalf("exact frame bound was rejected: %v", err)
	}
	payload, err := EncodeAnalysisPreviewTimeline(value.NominalTicks, value.ActualTicks)
	if err != nil || len(payload) != 65536 {
		t.Fatalf("maximum packed preview timeline size changed: %d: %v", len(payload), err)
	}
}

func TestAnalysisPreviewTimelineUsesExactBoundedTickPairs(t *testing.T) {
	nominal, actual := []int64{0, 0x01020304}, []int64{0x05, 0x05060708}
	want, err := hex.DecodeString("0000000000000000050000000000000004030201000000000807060500000000")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeAnalysisPreviewTimeline(nominal, actual)
	if err != nil || !bytes.Equal(encoded, want) {
		t.Fatalf("packed timeline differs: %x: %v", encoded, err)
	}
	decodedNominal, decodedActual, err := DecodeAnalysisPreviewTimeline(want)
	if err != nil || !reflect.DeepEqual(decodedNominal, nominal) || !reflect.DeepEqual(decodedActual, actual) {
		t.Fatalf("packed timeline lost exact ticks: %v %v: %v", decodedNominal, decodedActual, err)
	}
	for _, invalid := range [][]byte{nil, want[:len(want)-1], append(append([]byte(nil), want...), 0), make([]byte, analysisPreviewMaxTimelineBytes+16)} {
		if nominal, actual, err := DecodeAnalysisPreviewTimeline(invalid); !errors.Is(err, ErrInvalidInput) || nominal != nil || actual != nil {
			t.Fatal("malformed timeline exposed partial ticks")
		}
	}
	negative := append([]byte(nil), want...)
	binary.LittleEndian.PutUint64(negative[8:16], math.MaxUint64)
	if _, _, err := DecodeAnalysisPreviewTimeline(negative); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("unsigned-to-signed overflow became a source timestamp")
	}
}

func TestAnalysisPreviewStoredValidationRequiresCompleteSourceBoundSlots(t *testing.T) {
	value, duration := analysisPreviewTestValue()
	if err := ValidateStoredAnalysisPreview(value, duration); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AnalysisPreview){
		func(v *AnalysisPreview) { v.ItemID = "" },
		func(v *AnalysisPreview) { v.Revision = "01" },
		func(v *AnalysisPreview) { v.ProfileRevision = "0" },
		func(v *AnalysisPreview) { v.PublicationEpoch = 0 },
		func(v *AnalysisPreview) { v.ProfileFingerprint = strings.Repeat("A", 64) },
		func(v *AnalysisPreview) { v.SourceRevision = "" },
		func(v *AnalysisPreview) { v.CacheKey = strings.Repeat("g", 64) },
		func(v *AnalysisPreview) { v.Seal = "" },
		func(v *AnalysisPreview) { v.SHA256 = strings.Repeat("d", 63) },
		func(v *AnalysisPreview) { v.Width = 321 },
		func(v *AnalysisPreview) { v.Height = 2049 },
		func(v *AnalysisPreview) { v.Height = math.MaxInt },
		func(v *AnalysisPreview) { v.Bytes = 0 },
		func(v *AnalysisPreview) { v.Bytes = (128 << 20) + 1 },
		func(v *AnalysisPreview) {
			v.FrameCount = 2
			v.NominalTicks = v.NominalTicks[:2]
			v.ActualTicks = v.ActualTicks[:2]
		},
		func(v *AnalysisPreview) { v.FrameCount = 4097 },
		func(v *AnalysisPreview) { v.IntervalTicks = media.TicksPerSecond },
		func(v *AnalysisPreview) { v.IntervalTicks = 121 * media.TicksPerSecond },
		func(v *AnalysisPreview) { v.IntervalTicks++ },
		func(v *AnalysisPreview) { v.NominalTicks[1]++ },
		func(v *AnalysisPreview) { v.ActualTicks[2] = duration },
		func(v *AnalysisPreview) { v.ActualTicks[2] = v.ActualTicks[1] - 1 },
		func(v *AnalysisPreview) { v.UpdatedAt = time.Time{} },
	} {
		invalid, _ := analysisPreviewTestValue()
		mutate(&invalid)
		if err := ValidateStoredAnalysisPreview(invalid, duration); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid stored preview was accepted: %+v: %v", invalid, err)
		}
	}
	for _, duration := range []int64{0, -1, media.MaxAnalysisDurationTicks + 1, math.MaxInt64} {
		if err := ValidateStoredAnalysisPreview(value, duration); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid source duration %d was accepted", duration)
		}
	}
	value.ActualTicks = []int64{0, 0, 0}
	if err := ValidateStoredAnalysisPreview(value, duration); err != nil {
		t.Fatalf("repeated low-frame-rate presentation time was rejected: %v", err)
	}
	value.Bytes = 128 << 20
	if err := ValidateStoredAnalysisPreview(value, duration); err != nil {
		t.Fatalf("inclusive derivative byte boundary was rejected: %v", err)
	}
}

func TestAnalysisPreviewSizeMustContainEveryIndexAndJPEGMarker(t *testing.T) {
	for _, count := range []int{1, 3, 4096} {
		value, _ := analysisPreviewTestValue()
		value.FrameCount, value.IntervalTicks = count, 2*media.TicksPerSecond
		value.NominalTicks, value.ActualTicks = make([]int64, count), make([]int64, count)
		for index := range value.NominalTicks {
			value.NominalTicks[index] = int64(index) * value.IntervalTicks
			value.ActualTicks[index] = value.NominalTicks[index]
		}
		duration := int64(count) * value.IntervalTicks
		minimum := int64(72 + 12*count)
		for _, size := range []int64{1, 72, 64 + 8*int64(count+1), minimum - 1} {
			value.Bytes = size
			if err := ValidateStoredAnalysisPreview(value, duration); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("size %d cannot contain %d frame indexes and JPEG markers: %v", size, count, err)
			}
		}
		// The exact necessary bound is accepted as metadata, without claiming
		// that these declared bytes would decode as a JPEG or a complete BIF.
		value.Bytes = minimum
		if err := ValidateStoredAnalysisPreview(value, duration); err != nil {
			t.Fatalf("necessary structural boundary for %d frames was rejected: %v", count, err)
		}
	}
}

func analysisPreviewPublicationFixture() (AnalysisWork, []AnalysisPreviewPublication) {
	value, duration := analysisPreviewTestValue()
	work := AnalysisWork{ChildID: "preview-child", TaskKey: TaskPreviewGenerationKey, ConfigurationFingerprint: value.ProfileFingerprint,
		ConfigurationRevision: value.ProfileRevision, PublicationEpoch: value.PublicationEpoch, Profile: DefaultAnalysisProfile(),
		Execution: AnalysisExecutionProfile{PreviewWidths: []int{240, 320}},
		Sources:   []AnalysisSource{{ItemID: value.ItemID, SourceRevision: value.SourceRevision, DurationTicks: duration, Target: true}}}
	values := []AnalysisPreviewPublication{}
	for _, width := range work.Execution.PreviewWidths {
		values = append(values, AnalysisPreviewPublication{ItemID: value.ItemID, CacheKey: value.CacheKey, Seal: value.Seal,
			SHA256: value.SHA256, Width: width, Height: value.Height, Bytes: value.Bytes, FrameCount: value.FrameCount,
			IntervalTicks: value.IntervalTicks, NominalTicks: append([]int64(nil), value.NominalTicks...), ActualTicks: append([]int64(nil), value.ActualTicks...)})
	}
	return work, values
}

func TestAnalysisPreviewPublicationRequiresExactWidthsAndOneCurrentTarget(t *testing.T) {
	work, values := analysisPreviewPublicationFixture()
	source, prepared, err := prepareAnalysisPreviewPublications(work, values)
	if err != nil || len(prepared) != 2 || source.ItemID != values[0].ItemID {
		t.Fatalf("complete preview publication was rejected: %v", err)
	}
	values[0].NominalTicks[0] = 123
	if prepared[0].NominalTicks[0] != 0 {
		t.Fatal("prepared publication retained caller-owned timeline slices")
	}
	for _, mutate := range []func(*AnalysisWork, *[]AnalysisPreviewPublication){
		func(w *AnalysisWork, _ *[]AnalysisPreviewPublication) { w.TaskKey = TaskIntroAnalysisKey },
		func(w *AnalysisWork, _ *[]AnalysisPreviewPublication) { w.Sources[0].Target = false },
		func(w *AnalysisWork, _ *[]AnalysisPreviewPublication) { w.Sources = append(w.Sources, w.Sources[0]) },
		func(w *AnalysisWork, _ *[]AnalysisPreviewPublication) { w.Execution.PreviewWidths = []int{240, 240} },
		func(w *AnalysisWork, _ *[]AnalysisPreviewPublication) { w.Execution.PreviewWidths = []int{240, 400} },
		func(w *AnalysisWork, _ *[]AnalysisPreviewPublication) { w.Profile.PreviewIntervalSeconds = 20 },
		func(_ *AnalysisWork, v *[]AnalysisPreviewPublication) { *v = (*v)[:1] },
		func(_ *AnalysisWork, v *[]AnalysisPreviewPublication) { (*v)[0].ItemID = "support-item" },
		func(_ *AnalysisWork, v *[]AnalysisPreviewPublication) { (*v)[1].Width = (*v)[0].Width },
		func(_ *AnalysisWork, v *[]AnalysisPreviewPublication) { (*v)[0].NominalTicks = make([]int64, 4097) },
	} {
		work, values := analysisPreviewPublicationFixture()
		mutate(&work, &values)
		if source, prepared, err := prepareAnalysisPreviewPublications(work, values); !errors.Is(err, ErrInvalidInput) || source != (AnalysisSource{}) || prepared != nil {
			t.Fatalf("partial or unbound preview set was accepted: %v", err)
		}
	}
}

func TestAnalysisPreviewPublicationUsesTheEffectiveInterval(t *testing.T) {
	work, values := analysisPreviewPublicationFixture()
	work.Sources[0].DurationTicks = media.MaxAnalysisDurationTicks
	work.Profile.PreviewIntervalSeconds = 2
	interval := int64(11) * media.TicksPerSecond
	for index := range values {
		value := &values[index]
		value.IntervalTicks = interval
		value.FrameCount = int((work.Sources[0].DurationTicks-1)/interval + 1)
		value.Bytes = 72 + 12*int64(value.FrameCount)
		value.NominalTicks, value.ActualTicks = make([]int64, value.FrameCount), make([]int64, value.FrameCount)
		for frame := range value.NominalTicks {
			value.NominalTicks[frame] = int64(frame) * interval
			value.ActualTicks[frame] = value.NominalTicks[frame]
		}
	}
	if _, _, err := prepareAnalysisPreviewPublications(work, values); err != nil {
		t.Fatalf("adaptive complete preview set was rejected: %v", err)
	}
	values[0].IntervalTicks = 2 * media.TicksPerSecond
	if _, _, err := prepareAnalysisPreviewPublications(work, values); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("configured interval was accepted in place of the bounded effective interval")
	}
}
