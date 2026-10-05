package media

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/introskipper"
)

func TestCreditsSkipperArgumentsUseRelativeOutputEndAndNativeClock(t *testing.T) {
	for _, duration := range []int64{17*TicksPerSecond + 1, 450 * TicksPerSecond, 450*TicksPerSecond + 1, 12345678901, MaxAnalysisDurationTicks} {
		start := introskipper.CreditsFingerprintStartSeconds(duration)
		end := float64(duration) / float64(TicksPerSecond)
		args := creditsSkipperArgs(7, start, end)
		positions := make(map[string]int)
		for index, arg := range args {
			positions[arg] = index
		}
		if positions["-ss"] >= positions["-i"] || positions["-to"] <= positions["-i"] {
			t.Fatal("seek/output placement changed")
		}
		for _, value := range []struct {
			flag     string
			expected float64
		}{{"-ss", start}, {"-to", end - start}} {
			parsed, err := strconv.ParseFloat(args[positions[value.flag]+1], 64)
			if err != nil || math.Float64bits(parsed) != math.Float64bits(value.expected) {
				t.Fatalf("%s changed the binary64 clock: %v", value.flag, args)
			}
		}
		joined := " " + strings.Join(args, " ") + " "
		for _, required := range []string{" -i /proc/self/fd/3 ", " -map 0:7 ", " -ac 2 ", " -f chromaprint -fp_format raw pipe:1 "} {
			if !strings.Contains(joined, required) {
				t.Fatalf("missing upstream audio recipe: %s", required)
			}
		}
		for _, forbidden := range []string{" -copyts ", " -start_at_zero ", " -ar ", " -af ", " -vf ", " -hwaccel ", " -ss 0 -i "} {
			if strings.Contains(joined, forbidden) && (forbidden != " -ss 0 -i " || start != 0) {
				t.Fatalf("credits clock changed: %s", forbidden)
			}
		}
	}
}

func TestCreditsSkipperProfileIsDistinctPathFreeAndToolBound(t *testing.T) {
	available := AnalysisAvailability{IntroSkipperAvailable: true, IntroFFmpegSHA256: strings.Repeat("a", 64), IntroFFmpegPath: "/private/tool/ffmpeg"}
	profile, err := CreditsSkipperAlgorithmProfile(available)
	if err != nil || len(profile) > 512 || !strings.Contains(profile, introskipper.CreditsVersion) || !strings.Contains(profile, introskipper.UpstreamCommit) || !strings.Contains(profile, "tail-450-seconds") || strings.Contains(profile, "/private/") {
		t.Fatalf("credits profile %q %v", profile, err)
	}
	introProfile, err := IntroSkipperAlgorithmProfile(available)
	if err != nil || introProfile == profile {
		t.Fatal("credits reused the intro profile")
	}
	available.IntroFFmpegSHA256 = strings.Repeat("b", 64)
	changed, err := CreditsSkipperAlgorithmProfile(available)
	if err != nil || changed == profile {
		t.Fatal("changed muxer identity reused the profile")
	}
	available.IntroSkipperAvailable = false
	if _, err := CreditsSkipperAlgorithmProfile(available); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatal("unavailable credits muxer admitted")
	}
	available.IntroSkipperAvailable = true
	available.IntroFFmpegSHA256 = "invalid"
	if _, err := CreditsSkipperAlgorithmProfile(available); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatal("invalid muxer hash admitted")
	}
}

func TestCreditsSkipperFinalSourceAndCancellationChecksRetainTheSlot(t *testing.T) {
	for _, initial := range []error{nil, ErrIntroSkipperFingerprintUnavailable} {
		ctx, cancel := context.WithCancel(context.Background())
		result := CreditsSkipperFeatures{RawFingerprint: []uint32{1}, FingerprintStartSeconds: 12, FingerprintEndSeconds: 462}
		resultErr := initial
		released := false
		finalizeCreditsSkipperAnalysis(ctx, func() error {
			if released {
				t.Fatal("source check ran after release")
			}
			cancel()
			return nil
		}, func() { released = true }, &result, &resultErr)
		if !released || !errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, ErrIntroSkipperFingerprintUnavailable) || result.RawFingerprint != nil || result.FingerprintStartSeconds != 0 {
			t.Fatalf("partial result survived cancellation: %+v %v", result, resultErr)
		}
	}
	result := CreditsSkipperFeatures{RawFingerprint: []uint32{1}}
	var resultErr error
	ctx, cancel := context.WithCancel(context.Background())
	finalizeCreditsSkipperAnalysis(ctx, func() error { return nil }, cancel, &result, &resultErr)
	if resultErr != nil || len(result.RawFingerprint) != 1 {
		t.Fatal("release invalidated completed extraction")
	}
	resultErr = ErrIntroSkipperFingerprintUnavailable
	finalizeCreditsSkipperAnalysis(context.Background(), func() error { return ErrAnalysisUnproven }, nil, &result, &resultErr)
	if !errors.Is(resultErr, ErrAnalysisUnproven) || errors.Is(resultErr, ErrIntroSkipperFingerprintUnavailable) || result.RawFingerprint != nil {
		t.Fatal("source failure became an ordinary empty fingerprint")
	}
}
