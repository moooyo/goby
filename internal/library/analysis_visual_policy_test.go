package library

import (
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introdetect"
)

func TestStoredAnalysisVisualPolicyDoesNotReinterpretMeasurementSpace(t *testing.T) {
	value := analysisVisualDetectionTestValue()
	metrics := value.Episode.Candidates[0].VisualEvidence
	if metrics.MeasurementPolicy != "coarse-v1" || metrics.CalibrationDigest != "" {
		t.Fatal("fixture must retain the original measurement space")
	}
	start, end := analysisDetectionTestInterval(value)
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); err != nil {
		t.Fatal(err)
	}
	metrics.MeasurementPolicy = "calibrated-r16-v1"
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); err == nil {
		t.Fatal("calibrated evidence without its audit digest was accepted")
	}
	metrics.CalibrationDigest = strings.Repeat("a", 64)
	if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); err != nil {
		t.Fatal("complete current calibrated metric domain rejected", err)
	}
	for _, mutation := range []func(*introdetect.VisualSequenceMetrics){
		func(m *introdetect.VisualSequenceMetrics) { m.MeasurementPolicy = "" },
		func(m *introdetect.VisualSequenceMetrics) { m.MeasurementPolicy = "future-policy" },
		func(m *introdetect.VisualSequenceMetrics) { m.MeasurementPolicy = "coarse-v1" },
		func(m *introdetect.VisualSequenceMetrics) { m.CalibrationDigest = strings.Repeat("A", 64) },
		func(m *introdetect.VisualSequenceMetrics) { m.CalibrationDigest = strings.Repeat("f", 63) },
	} {
		original := *metrics
		mutation(metrics)
		if err := ValidateStoredAnalysisResult(analysisDetectionTestJSON(t, value), "qualified", start, end); err == nil {
			t.Fatal("invalid policy/digest pair accepted", *metrics)
		}
		*metrics = original
	}
}
