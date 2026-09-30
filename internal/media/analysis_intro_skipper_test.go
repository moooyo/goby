package media

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

const introSkipperTestMuxer = `Muxer chromaprint [Chromaprint]:
    Default audio codec: pcm_s16le.
chromaprint muxer AVOptions:
  -silence_threshold <int>        E.......... threshold for detecting silence (from -1 to 32767) (default -1)
  -algorithm         <int>        E.......... version of the fingerprint algorithm (from 0 to INT_MAX) (default 1)
  -fp_format         <int>        E.......... fingerprint format to write (from 0 to 2) (default base64)
     raw             0            E.......... binary raw fingerprint
`

func TestIntroSkipperRawFingerprintOrderAndBounds(t *testing.T) {
	wanted := []uint32{0, 0x80000000, 0xffffffff, 0x12345678}
	data := make([]byte, len(wanted)*4)
	for index, word := range wanted {
		binary.LittleEndian.PutUint32(data[index*4:], word)
	}
	actual, err := parseIntroSkipperFingerprint(data)
	if err != nil || !reflect.DeepEqual(actual, wanted) {
		t.Fatalf("raw sequence: %v %v", actual, err)
	}
	for _, empty := range [][]byte{nil, {}} {
		if _, err := parseIntroSkipperFingerprint(empty); !errors.Is(err, ErrIntroSkipperFingerprintUnavailable) {
			t.Fatalf("empty sequence did not retain per-episode unavailability: %v", err)
		}
	}
	for _, malformed := range [][]byte{{0}, {0, 0, 0}, {0, 0, 0, 0, 0}} {
		if _, err := parseIntroSkipperFingerprint(malformed); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("malformed sequence accepted: %v", err)
		}
	}
	if raw, err := parseIntroSkipperFingerprint(make([]byte, introskipper.MaxFingerprintPoints*4)); err != nil || len(raw) != introskipper.MaxFingerprintPoints {
		t.Fatalf("bounded sequence: %d %v", len(raw), err)
	}
	if _, err := parseIntroSkipperFingerprint(make([]byte, introskipper.MaxFingerprintPoints*4+4)); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("oversized sequence: %v", err)
	}
}

func TestIntroSkipperMuxerRequiresUnchangedDefaultsAndRawFormat(t *testing.T) {
	if !introSkipperMuxerSupported(introSkipperTestMuxer) {
		t.Fatal("native muxer contract rejected")
	}
	for _, mutation := range [][2]string{{"(default 1)", "(default 2)"}, {"(default -1)", "(default 0)"}, {"binary raw fingerprint", "text fingerprint"}, {"pcm_s16le.", "pcm_f32le."}, {"Muxer chromaprint", "Muxer alternate"}} {
		if introSkipperMuxerSupported(strings.ReplaceAll(introSkipperTestMuxer, mutation[0], mutation[1])) {
			t.Fatalf("changed muxer semantics accepted: %v", mutation)
		}
	}
}

func TestIntroSkipperEncoderAdmissionRequiresAnAudioEncoderEntry(t *testing.T) {
	if !introSkipperEncoderSupported("Encoders:\n A....D pcm_s16le PCM signed 16-bit little-endian\n") {
		t.Fatal("PCM audio encoder rejected")
	}
	for _, output := range []string{"Default audio codec: pcm_s16le.", " V....D pcm_s16le Invalid video encoder", " A....D pcm_s16le_planar Planar PCM", " A....D pcm_s16be Big-endian PCM"} {
		if introSkipperEncoderSupported(output) {
			t.Fatalf("nonmatching encoder description accepted: %q", output)
		}
	}
}

func TestIntroSkipperFinalSourceCheckRetainsAdmissionAndOverridesEmptyOnDeadline(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "successful-result"
		if empty {
			name = "empty-fingerprint"
		}
		t.Run(name, func(t *testing.T) {
			parent := context.Background()
			ctx, cancel := context.WithTimeout(parent, 20*time.Millisecond)
			defer cancel()
			entered, finishSource, released, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
			result := IntroSkipperFeatures{RawFingerprint: []uint32{1}}
			var resultErr error
			if empty {
				result, resultErr = IntroSkipperFeatures{}, ErrIntroSkipperFingerprintUnavailable
			}
			go func() {
				finalizeIntroSkipperAnalysis(ctx, func() error { close(entered); <-finishSource; return nil }, func() { close(released) }, &result, &resultErr)
				close(finished)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				close(finishSource)
				t.Fatal("final source check did not start")
			}
			<-ctx.Done()
			select {
			case <-released:
				t.Error("admission released before source I/O completed")
			default:
			}
			close(finishSource)
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Fatal("final source check did not complete")
			}
			if !errors.Is(resultErr, context.DeadlineExceeded) || parent.Err() != nil || errors.Is(resultErr, ErrIntroSkipperFingerprintUnavailable) || result.RawFingerprint != nil {
				t.Fatalf("internal deadline was classified as completed output: %+v %v", result, resultErr)
			}
			select {
			case <-released:
			default:
				t.Fatal("admission was retained after final checks completed")
			}
		})
	}
}

func TestIntroSkipperFinalChecksPrecedeReleaseAndSourceFailureOverridesEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := IntroSkipperFeatures{RawFingerprint: []uint32{1}}
	var resultErr error
	finalizeIntroSkipperAnalysis(ctx, func() error { return nil }, cancel, &result, &resultErr)
	if resultErr != nil || len(result.RawFingerprint) != 1 || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("release cancellation invalidated a completed operation: %+v %v", result, resultErr)
	}
	resultErr = ErrIntroSkipperFingerprintUnavailable
	finalizeIntroSkipperAnalysis(context.Background(), func() error { return ErrAnalysisUnproven }, nil, &result, &resultErr)
	if !errors.Is(resultErr, ErrAnalysisUnproven) || errors.Is(resultErr, ErrIntroSkipperFingerprintUnavailable) || result.RawFingerprint != nil {
		t.Fatalf("source failure retained per-episode empty sentinel: %+v %v", result, resultErr)
	}
}

func TestIntroSkipperProfileAndToolFallbackAreIndependent(t *testing.T) {
	available := AnalysisAvailability{IntroSkipperAvailable: true, IntroFFmpegSHA256: strings.Repeat("a", 64)}
	profile, err := IntroSkipperAlgorithmProfile(available)
	if err != nil || len(profile) > 512 || !strings.Contains(profile, introskipper.UpstreamCommit) {
		t.Fatalf("profile: %q %v", profile, err)
	}
	available.IntroFFmpegSHA256 = strings.Repeat("b", 64)
	other, err := IntroSkipperAlgorithmProfile(available)
	if err != nil || other == profile {
		t.Fatal("changed extraction binary reused the same profile")
	}
	available.IntroSkipperAvailable = false
	if _, err := IntroSkipperAlgorithmProfile(available); !errors.Is(err, ErrAnalysisUnavailable) {
		t.Fatalf("unavailable muxer: %v", err)
	}
	e := AnalysisExtractor{FFmpegPath: "legacy", ExpectedFFmpegSHA256: "legacy-hash"}
	if path, hash := e.introSkipperTool(); path != "legacy" || hash != "legacy-hash" {
		t.Fatalf("fallback: %q %q", path, hash)
	}
	e.IntroFFmpegPath, e.ExpectedIntroFFmpegSHA256 = "intro", "intro-hash"
	if path, hash := e.introSkipperTool(); path != "intro" || hash != "intro-hash" {
		t.Fatalf("independent binary: %q %q", path, hash)
	}
}

func TestIntroSkipperStreamSelectionUsesChannelsAndLanguageFallback(t *testing.T) {
	info := Info{Streams: []Stream{
		{Index: 4, CodecType: "audio", Channels: 6, Language: "jpn"},
		{Index: 2, CodecType: "audio", Channels: 6, Language: "ENG"},
		{Index: 1, CodecType: "audio", Channels: 2, Language: "eng", IsDefault: true},
		{Index: 0, CodecType: "video", Channels: 64},
		{Index: 9, CodecType: "audio", Channels: 64, IsExternal: true},
	}}
	for _, test := range []struct {
		language string
		most     bool
		index    int
	}{
		{"", true, 2}, {"eng", true, 2}, {" jPn ", true, 4}, {"missing", true, 2}, {"", false, 1}, {"jpn", false, 4}, {"missing", false, 1},
	} {
		if index, ok := SelectIntroSkipperAudioStream(info, test.language, test.most); !ok || index != test.index {
			t.Fatalf("selection %+v: %d %v", test, index, ok)
		}
	}
	info.Streams = append(info.Streams, Stream{Index: 2, CodecType: "video"})
	if _, ok := SelectIntroSkipperAudioStream(info, "", true); ok {
		t.Fatal("ambiguous original index accepted")
	}
	if _, ok := SelectIntroSkipperAudioStream(Info{}, "", true); ok {
		t.Fatal("missing audio accepted")
	}
}

func TestIntroSkipperExtractionArgumentsKeepTheNativeClock(t *testing.T) {
	args := introSkipperArgs(3, 373.09216025)
	joined := " " + strings.Join(args, " ") + " "
	for _, required := range []string{" -ss 0 -i /proc/self/fd/3 -to 373.09216025 -map 0:3 ", " -ac 2 ", " -f chromaprint -fp_format raw pipe:1 "} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing native argument sequence %q: %s", required, joined)
		}
	}
	for _, forbidden := range []string{" -ar ", " -af ", " -copyts ", " -start_at_zero ", " -itsoffset "} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unexpected clock or PCM rewrite: %s", joined)
		}
	}
}
