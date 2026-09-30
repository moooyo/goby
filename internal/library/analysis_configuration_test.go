package library

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
)

func TestAnalysisProfileRequiresCompleteBoundedValues(t *testing.T) {
	defaults := DefaultAnalysisProfile()
	if !defaults.AutoPublishIntros || defaults.PreviewIntervalSeconds != 10 || defaults.PreviewQuality != 80 ||
		defaults.MaxSourceBytes != 128<<30 || defaults.MaxItemRuntimeSeconds != 1200 || defaults.FeatureCacheMaxBytes != 128<<20 ||
		defaults.IntroSkipper != introskipper.DefaultOptions() {
		t.Fatal("analysis defaults changed without an explicit profile migration")
	}
	if err := ValidateAnalysisProfile(defaults); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AnalysisProfile){
		func(p *AnalysisProfile) { p.PreviewIntervalSeconds = 1 },
		func(p *AnalysisProfile) { p.PreviewIntervalSeconds = 121 },
		func(p *AnalysisProfile) { p.PreviewQuality = 39 },
		func(p *AnalysisProfile) { p.PreviewQuality = 96 },
		func(p *AnalysisProfile) { p.MaxSourceBytes = 0 },
		func(p *AnalysisProfile) { p.MaxSourceBytes = (1 << 40) + 1 },
		func(p *AnalysisProfile) { p.MaxItemRuntimeSeconds = 0 },
		func(p *AnalysisProfile) { p.MaxItemRuntimeSeconds = 7201 },
		func(p *AnalysisProfile) { p.FeatureCacheMaxBytes = (1 << 20) - 1 },
		func(p *AnalysisProfile) { p.FeatureCacheMaxBytes = (512 << 20) + 1 },
		func(p *AnalysisProfile) { p.IntroSkipper.AnalysisPercent = 0 },
		func(p *AnalysisProfile) {
			p.IntroSkipper.MaximumIntroDuration = p.IntroSkipper.MinimumIntroDuration - 1
		},
	} {
		profile := defaults
		mutate(&profile)
		if err := ValidateAnalysisProfile(profile); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unbounded profile was admitted: %+v: %v", profile, err)
		}
	}
	for _, profile := range []AnalysisProfile{
		{PreviewIntervalSeconds: 2, PreviewQuality: 40, MaxSourceBytes: 1, MaxItemRuntimeSeconds: 1, FeatureCacheMaxBytes: 1 << 20},
		{AutoPublishIntros: true, PreviewIntervalSeconds: 120, PreviewQuality: 95, MaxSourceBytes: 1 << 40, MaxItemRuntimeSeconds: 7200, FeatureCacheMaxBytes: 512 << 20},
	} {
		profile.IntroSkipper = introskipper.DefaultOptions()
		if err := ValidateAnalysisProfile(profile); err != nil {
			t.Fatalf("inclusive profile boundary was rejected: %+v: %v", profile, err)
		}
	}
	if err := ValidateAnalysisProfile(AnalysisProfile{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("an omitted profile silently selected defaults")
	}
}

func TestAnalysisIntroSkipperOptionsRequireExactStoredFields(t *testing.T) {
	encoded, err := json.Marshal(introskipper.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if value, err := DecodeAnalysisIntroSkipperOptions(encoded); err != nil || value != introskipper.DefaultOptions() {
		t.Fatalf("valid upstream defaults failed strict settings decoding: %+v %v", value, err)
	}
	for _, raw := range []string{
		`null`, `{}`, string(encoded) + `{}`,
		strings.Replace(string(encoded), `"MaximumTimeSkip":3.5,`, ``, 1),
		strings.Replace(string(encoded), `"MaximumTimeSkip":3.5`, `"maximumTimeSkip":3.5`, 1),
		strings.Replace(string(encoded), `"MaximumTimeSkip":3.5`, `"MaximumTimeSkip":null`, 1),
		strings.Replace(string(encoded), `"MaximumTimeSkip":3.5`, `"MaximumTimeSkip":3.5,"MaximumTimeSkip":0`, 1),
		strings.Replace(string(encoded), `"MaximumTimeSkip":3.5`, `"MaximumTimeSkip":31`, 1),
		strings.Replace(string(encoded), `"InvertedIndexShift":2`, `"InvertedIndexShift":2,"Offset":1`, 1),
	} {
		if _, err := DecodeAnalysisIntroSkipperOptions([]byte(raw)); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid settings JSON passed durable validation: %s: %v", raw, err)
		}
	}
	legacy := DefaultAnalysisProfile()
	if ValidateLegacyAnalysisProfile(legacy) == nil {
		t.Fatal("a current profile was accepted as historical wire data")
	}
	legacy.IntroSkipper = introskipper.Options{}
	if ValidateLegacyAnalysisProfile(legacy) != nil || ValidateAnalysisProfile(legacy) == nil {
		t.Fatal("historical validation injected current defaults or lost the old contract")
	}
}
