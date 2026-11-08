package transcode

import (
	"errors"
	"strings"
	"testing"
)

func TestSoftwareToneMapOptionsAcceptOnlyImplementedNativePolicies(t *testing.T) {
	for _, limit := range []struct {
		name   string
		height int
	}{{"None", 0}, {"Limit420", 420}, {"Limit720", 720}, {"Limit1080", 1080}} {
		for _, algorithm := range []string{"Hable", "Reinhard", "Mobius"} {
			value := SoftwareToneMapOptions{ToneMapSizeLimit: limit.name, SoftwareToneMapAlgorithm: algorithm}
			if err := ValidateSoftwareToneMapOptions(value); err != nil {
				t.Fatalf("implemented native policy was rejected: %+v: %v", value, err)
			}
			if got := SoftwareToneMapHeightLimit(value); got != limit.height {
				t.Fatalf("output height limit = %d, want %d", got, limit.height)
			}
		}
	}
	for _, value := range []SoftwareToneMapOptions{
		{},
		{ToneMapSizeLimit: "Limit480", SoftwareToneMapAlgorithm: "Hable"},
		{ToneMapSizeLimit: "None", SoftwareToneMapAlgorithm: "Bt2390"},
		{ToneMapSizeLimit: "None", SoftwareToneMapAlgorithm: "hable"},
		{ToneMapSizeLimit: "None", SoftwareToneMapAlgorithm: "hable:desat=0"},
	} {
		if err := ValidateSoftwareToneMapOptions(value); !errors.Is(err, ErrInvalidExecution) {
			t.Fatalf("unimplemented or malformed policy was accepted: %+v: %v", value, err)
		}
	}
	if SoftwareToneMapHeightLimit(SoftwareToneMapOptions{}) != 0 {
		t.Fatal("historical absent options acquired an output limit")
	}
}

func TestSoftwareToneMapCaptureTracksOnlyActiveAlgorithm(t *testing.T) {
	plan := videoFilterHDRPlan()
	options := DefaultExecutionOptions(2)
	baseline, err := CaptureExecution(plan, options)
	if err != nil {
		t.Fatal(err)
	}
	options.SoftwareToneMapOptions.ToneMapSizeLimit = "Limit720"
	withSizePreference, err := CaptureExecution(plan, options)
	if err != nil || withSizePreference != baseline {
		t.Fatalf("already captured dimensions acquired a second size identity: %v", err)
	}
	options.SoftwareToneMapOptions.SoftwareToneMapAlgorithm = "Reinhard"
	withAlgorithm, err := CaptureExecution(plan, options)
	if err != nil || withAlgorithm == baseline || withAlgorithm.Execution.SoftwareToneMapOptions.ToneMapSizeLimit != "None" {
		t.Fatalf("active algorithm was lost or a planning preference entered execution: %+v: %v", withAlgorithm.Execution, err)
	}
	if got := softwareVideoProcessingFilter(withAlgorithm); !strings.Contains(got, "tonemap=tonemap=reinhard:desat=2.0") || strings.Contains(got, "param=") {
		t.Fatalf("selected native algorithm did not reach its CPU transform: %s", got)
	}
	plan.VideoFilters = VideoFilters{}
	inactive, err := CaptureExecution(plan, options)
	if err != nil || inactive.Execution.SoftwareToneMapOptions != (SoftwareToneMapOptions{}) {
		t.Fatalf("inactive tone mapping preference entered output identity: %+v: %v", inactive.Execution, err)
	}
	plan.VideoFilters = VideoFilters{Backend: "vulkan", ToneMap: "hdr10"}
	hardware, err := CaptureExecution(plan, options)
	if err != nil || hardware.Execution.SoftwareToneMapOptions != (SoftwareToneMapOptions{}) {
		t.Fatalf("a CPU algorithm entered hardware output identity: %+v: %v", hardware.Execution, err)
	}
}

func TestSoftwareToneMapLegacyGraphKeepsHableDefaults(t *testing.T) {
	for _, version := range []int{0, LegacyExecutionVersion} {
		plan := videoFilterHDRPlan()
		plan.ExecutionVersion = version
		if version != 0 {
			plan.Execution = legacyExecutionDefaults(2)
		}
		if err := ValidatePlan(plan); err != nil {
			t.Fatalf("historical software tone mapping plan became invalid: %v", err)
		}
		if got := softwareVideoProcessingFilter(plan); !strings.Contains(got, "tonemap=tonemap=hable:desat=2.0") || strings.Contains(got, "param=") {
			t.Fatalf("historical tone mapping graph changed: %s", got)
		}
	}
}
