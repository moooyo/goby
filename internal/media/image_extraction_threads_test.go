package media

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

func TestImageExtractionThreadPolicyPreservesLegacyAndNativeProcessorThreshold(t *testing.T) {
	for _, test := range []struct {
		name       string
		options    ImageExtractionThreads
		processors int
		want       int
	}{
		{name: "legacy single processor", processors: 1, want: 1},
		{name: "legacy many processors", processors: 8, want: 1},
		{name: "automatic", options: ImageExtractionThreads{Configured: true}, processors: 8},
		{name: "explicit below processor count", options: ImageExtractionThreads{Count: 2, Configured: true}, processors: 8, want: 2},
		{name: "explicit equal to processor count", options: ImageExtractionThreads{Count: 8, Configured: true}, processors: 8},
		{name: "explicit above processor count", options: ImageExtractionThreads{Count: 16, Configured: true}, processors: 8},
		{name: "managed maximum below processor count", options: ImageExtractionThreads{Count: 16, Configured: true}, processors: 32, want: 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveImageExtractionDecoderThreads(test.options, test.processors)
			if err != nil || got != test.want {
				t.Fatalf("decoder limit = %d, %v; want %d", got, err, test.want)
			}
		})
	}
	for _, test := range []struct {
		options    ImageExtractionThreads
		processors int
	}{
		{options: ImageExtractionThreads{Count: 1}, processors: 8},
		{options: ImageExtractionThreads{Count: -1, Configured: true}, processors: 8},
		{options: ImageExtractionThreads{Count: 17, Configured: true}, processors: 8},
		{processors: 0},
		{processors: -1},
	} {
		if _, err := ResolveImageExtractionDecoderThreads(test.options, test.processors); !errors.Is(err, ErrInvalidImageExtractionThreads) {
			t.Fatalf("ambiguous decoder resource policy was accepted: %+v", test)
		}
	}
}

func TestImageExtractionThreadPolicyChangesOnlyPreviewInputDecoderArguments(t *testing.T) {
	_, stream, plan, limits, _ := previewHoldTestSetup(t, 4*TicksPerSecond, TicksPerSecond)
	proof := &analysisPreviewHoldProof{points: []analysisPreviewHoldPoint{{ordinal: 0, firstSlot: 0, slotCount: 4}}}
	policies := []ImageExtractionThreads{{}, {Configured: true}, {Count: 1, Configured: true}, {Count: 16, Configured: true}}
	if runtime.NumCPU() > 2 {
		policies = append(policies, ImageExtractionThreads{Count: 2, Configured: true})
	}
	for _, selected := range []*analysisPreviewHoldProof{nil, proof} {
		legacy, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, selected)
		if err != nil {
			t.Fatal(err)
		}
		legacyDecoder, legacyOutput := imageExtractionTestSplitCommand(t, legacy)
		for _, policy := range policies {
			args, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, selected, policy)
			if err != nil {
				t.Fatal(err)
			}
			decoder, output := imageExtractionTestSplitCommand(t, args)
			want, err := ResolveImageExtractionDecoderThreads(policy, runtime.NumCPU())
			if err != nil {
				t.Fatal(err)
			}
			if decoder != want || !reflect.DeepEqual(output, legacyOutput) || legacyDecoder != 1 {
				t.Fatalf("resource policy changed another preview command stage: decoder=%d want=%d", decoder, want)
			}
			if policy == (ImageExtractionThreads{}) && !reflect.DeepEqual(args, legacy) {
				t.Fatal("an absent image policy changed the historical command")
			}
		}
		for _, policy := range []ImageExtractionThreads{{Count: 1}, {Count: -1, Configured: true}, {Count: 17, Configured: true}} {
			if args, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, selected, policy); !errors.Is(err, ErrInvalidImageExtractionThreads) || args != nil {
				t.Fatal("invalid image decoder policy produced a partial command")
			}
		}
		if args, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, selected, policies[0], policies[1]); !errors.Is(err, ErrInvalidImageExtractionThreads) || args != nil {
			t.Fatal("more than one image decoder policy was accepted")
		}
	}
}

func TestImageExtractionThreadPolicyRejectsInvalidBeforePreviewInputs(t *testing.T) {
	for _, policy := range []ImageExtractionThreads{{Count: 1}, {Count: -1, Configured: true}, {Count: 17, Configured: true}} {
		extractor := AnalysisExtractor{ImageExtractionThreads: policy}
		callbacks := 0
		summary, err := extractor.ExtractPreviews(context.Background(), nil, Info{}, 0, PreviewAnalysisOptions{}, func(PreviewFrame) error {
			callbacks++
			return nil
		})
		if !errors.Is(err, ErrInvalidImageExtractionThreads) || summary != (PreviewAnalysisSummary{}) || callbacks != 0 {
			t.Fatalf("invalid resource policy reached preview input or emission: summary=%+v callbacks=%d error=%v", summary, callbacks, err)
		}
	}
}

// The returned command excludes only the input decoder resource argument.
// Its complete remaining stages must be identical under every resource policy.
func imageExtractionTestSplitCommand(t *testing.T, args []string) (int, []string) {
	t.Helper()
	decoder, inputSeen := 0, false
	other := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "-i" {
			inputSeen = true
		}
		if argument == "-threads" {
			if inputSeen || decoder != 0 || index+1 == len(args) {
				t.Fatal("decoder thread option was misplaced, repeated, or incomplete")
			}
			var err error
			decoder, err = strconv.Atoi(args[index+1])
			if err != nil || decoder < 1 {
				t.Fatal("decoder thread option did not contain an explicit positive limit")
			}
			index++
			continue
		}
		other = append(other, argument)
	}
	if !inputSeen {
		t.Fatal("preview command has no source input")
	}
	return decoder, other
}
