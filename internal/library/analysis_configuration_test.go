package library

import (
	"errors"
	"testing"
)

func TestAnalysisProfileRequiresCompleteBoundedValues(t *testing.T) {
	defaults := DefaultAnalysisProfile()
	if !defaults.AutoPublishIntros || defaults.PreviewIntervalSeconds != 10 || defaults.PreviewQuality != 80 ||
		defaults.MaxSourceBytes != 128<<30 || defaults.MaxItemRuntimeSeconds != 1200 || defaults.FeatureCacheMaxBytes != 128<<20 {
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
		if err := ValidateAnalysisProfile(profile); err != nil {
			t.Fatalf("inclusive profile boundary was rejected: %+v: %v", profile, err)
		}
	}
	if err := ValidateAnalysisProfile(AnalysisProfile{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("an omitted profile silently selected defaults")
	}
}
