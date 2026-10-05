package library

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestBackgroundPreviewIntervalEditorialPriorityAndBounds(t *testing.T) {
	ticks := func(seconds int64) int64 { return seconds * backgroundTicksPerSecond }
	manual := ticks(17)
	cases := []struct {
		name          string
		total         int64
		manual        *int64
		intro         *IntroInterval
		credits       *CreditsPoint
		start, length int64
	}{
		{"movie fallback", 7200, nil, nil, nil, 300, 25},
		{"episode fallback", 2700, nil, nil, nil, 135, 25},
		{"minimum fallback", 300, nil, nil, nil, 30, 25},
		{"intro", 2700, nil, &IntroInterval{StartTicks: 0, EndTicks: ticks(80)}, nil, 83, 25},
		{"manual", 2700, &manual, &IntroInterval{StartTicks: 0, EndTicks: ticks(80)}, nil, 17, 25},
		{"short source", 12, nil, nil, nil, 0, 12},
		{"credits boundary", 90, nil, nil, &CreditsPoint{StartTicks: ticks(42)}, 30, 12},
		{"invalid intro ignored", 2700, nil, &IntroInterval{StartTicks: ticks(90), EndTicks: ticks(80)}, nil, 135, 25},
	}
	for _, entry := range cases {
		t.Run(entry.name, func(t *testing.T) {
			start, length, err := SelectBackgroundPreviewInterval(ticks(entry.total), entry.manual, entry.intro, entry.credits)
			if err != nil || start != ticks(entry.start) || length != ticks(entry.length) {
				t.Fatalf("got %d..%d: %v", start, start+length, err)
			}
		})
	}
	late := ticks(43)
	if _, _, err := SelectBackgroundPreviewInterval(ticks(90), &late, nil, &CreditsPoint{StartTicks: ticks(42)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("manual start after credits accepted")
	}
	if _, _, err := SelectBackgroundPreviewInterval(ticks(90), nil, nil, &CreditsPoint{StartTicks: 0}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("clip entered an all-credits source")
	}
	if _, _, err := SelectBackgroundPreviewInterval(ticks(90), nil, &IntroInterval{StartTicks: 0, EndTicks: ticks(80)}, &CreditsPoint{StartTicks: ticks(82)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("conflicting markers selected the intro as fallback")
	}
}

func TestBackgroundPreviewPolicyDefaultsAndStrictUpdates(t *testing.T) {
	if DefaultLibraryOptions().EnableBackgroundPreviewGeneration {
		t.Fatal("automatic backgrounds must default off")
	}
	var update LibraryOptionsUpdate
	if err := json.Unmarshal([]byte(`{"EnableBackgroundPreviewGeneration":true}`), &update); err != nil {
		t.Fatal(err)
	}
	options := applyLibraryOptions(DefaultLibraryOptions(), &update)
	if !options.EnableBackgroundPreviewGeneration || options.EnablePreviewGeneration || options.EnableIntroDetection {
		t.Fatalf("coupled policies: %+v", options)
	}
	if err := validateLibraryOptions("movies", options); err != nil {
		t.Fatal(err)
	}
	if err := validateLibraryOptions("music", options); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("music accepted: %v", err)
	}
	for _, raw := range []string{`{"EnableBackgroundPreviewGeneration":null}`, `{"EnableBackgroundPreviewGeneration":true,"enablebackgroundpreviewgeneration":false}`} {
		if json.Unmarshal([]byte(raw), &update) == nil {
			t.Fatalf("invalid update accepted: %s", raw)
		}
	}
	if err := ValidateBackgroundPreviewProfile(DefaultBackgroundPreviewProfile()); err != nil {
		t.Fatal(err)
	}
}
