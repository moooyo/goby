package transcode

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestExecutionOptionsValidateClosedControls(t *testing.T) {
	for name, mutate := range map[string]func(*ExecutionOptions){
		"zero threads":       func(v *ExecutionOptions) { v.Threads = 0 },
		"excess threads":     func(v *ExecutionOptions) { v.Threads = 65 },
		"preset fragment":    func(v *ExecutionOptions) { v.H264.Preset = "fast -f null" },
		"unselected preset":  func(v *ExecutionOptions) { v.HEVC.Preset = "faster" },
		"unbounded CRF mode": func(v *ExecutionOptions) { v.H264.RateControl = "crf" },
		"low CRF":            func(v *ExecutionOptions) { v.H264.CRF = 17 },
		"high CRF":           func(v *ExecutionOptions) { v.HEVC.CRF = 36 },
	} {
		t.Run(name, func(t *testing.T) {
			value := DefaultExecutionOptions(2)
			mutate(&value)
			if err := ValidateExecutionOptions(value); !errors.Is(err, ErrInvalidExecution) {
				t.Fatalf("unsupported execution value accepted: %v", err)
			}
		})
	}
	for _, threads := range []int{1, 64} {
		for _, preset := range []string{"veryfast", "fast", "medium", "slow"} {
			for _, crf := range []int{18, 35} {
				value := DefaultExecutionOptions(threads)
				value.H264, value.HEVC = CPUQuality{preset, "capped_crf", crf}, CPUQuality{preset, "bitrate", crf}
				if err := ValidateExecutionOptions(value); err != nil {
					t.Fatalf("closed boundary rejected: %v", err)
				}
			}
		}
	}
}

func TestExecutionCaptureKeepsOnlyOutputAffectingIdentity(t *testing.T) {
	plan := commandPlan()
	baseline, err := CaptureExecution(plan, DefaultExecutionOptions(2))
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*ExecutionOptions){
		"other codec":                 func(v *ExecutionOptions) { v.HEVC = CPUQuality{"slow", "capped_crf", 18} },
		"dormant CRF":                 func(v *ExecutionOptions) { v.H264.CRF = 35 },
		"inactive software tone gate": func(v *ExecutionOptions) { v.SoftwareToneMapping = false },
		"inactive Vulkan tone gate":   func(v *ExecutionOptions) { v.VulkanToneMapping = false },
	} {
		t.Run(name, func(t *testing.T) {
			options := DefaultExecutionOptions(2)
			change(&options)
			captured, err := CaptureExecution(plan, options)
			if err != nil || captured != baseline {
				t.Fatalf("inactive preference split output identity: %v", err)
			}
		})
	}
	for name, change := range map[string]func(*ExecutionOptions){
		"threads":    func(v *ExecutionOptions) { v.Threads = 3 },
		"preset":     func(v *ExecutionOptions) { v.H264.Preset = "fast" },
		"active CRF": func(v *ExecutionOptions) { v.H264.RateControl = "capped_crf" },
	} {
		t.Run(name, func(t *testing.T) {
			options := DefaultExecutionOptions(2)
			change(&options)
			captured, err := CaptureExecution(plan, options)
			if err != nil || captured == baseline {
				t.Fatalf("active preference failed to change output identity: %v", err)
			}
		})
	}
}

func TestExecutionCaptureGatesActualToneMapBackend(t *testing.T) {
	for _, backend := range []string{"", "vulkan"} {
		t.Run("backend="+backend, func(t *testing.T) {
			plan := commandPlan()
			plan.VideoFilters = VideoFilters{Backend: backend, ToneMap: "hdr10", SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
			options := DefaultExecutionOptions(2)
			if backend == "" {
				options.SoftwareToneMapping = false
			} else {
				options.VulkanToneMapping = false
			}
			if got, err := CaptureExecution(plan, options); !errors.Is(err, ErrToneMappingDisabled) || got != plan {
				t.Fatal("disabled filter was admitted or changed the rejected plan")
			}
			options.SoftwareToneMapping, options.VulkanToneMapping = !options.SoftwareToneMapping, !options.VulkanToneMapping
			if _, err := CaptureExecution(plan, options); err != nil {
				t.Fatalf("unrelated tone backend disabled an admitted graph: %v", err)
			}
		})
	}
}

func TestExecutionLegacyRecordsDoNotAcquireInventedDefaults(t *testing.T) {
	legacy := commandPlan()
	encoded, err := json.Marshal(legacy)
	if err != nil || strings.Contains(string(encoded), "Execution") {
		t.Fatalf("legacy plan acquired unproved execution fields: %s, %v", encoded, err)
	}
	var decoded Plan
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != legacy {
		t.Fatal("legacy plan did not round trip unchanged")
	}
	for _, version := range []int{-1, 2} {
		invalid := legacy
		invalid.ExecutionVersion = version
		if err := ValidatePlan(invalid); !errors.Is(err, ErrInvalidPlan) {
			t.Fatal("unsupported execution version accepted")
		}
	}
	legacy.Execution = DefaultExecutionOptions(2)
	if err := ValidatePlan(legacy); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("unversioned execution context accepted")
	}
}

func TestExecutionPlanRejectsHiddenInactiveIdentityAndUsesCapturedThreads(t *testing.T) {
	plan, err := CaptureExecution(commandPlan(), DefaultExecutionOptions(3))
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []int{0, 1, 64, 65} {
		threads, err := ExecutionThreads(plan, legacy)
		if err != nil || threads != 3 {
			t.Fatal("external runner argument reinterpreted captured threads")
		}
	}
	plan.Execution.HEVC.Preset = "slow"
	if err := ValidatePlan(plan); !errors.Is(err, ErrInvalidPlan) {
		t.Fatal("noncanonical inactive preference entered job identity")
	}
}
