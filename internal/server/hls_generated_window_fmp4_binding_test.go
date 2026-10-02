package server

import (
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

// This structural fixture exercises retained proof selection. It does not
// establish source coverage, measured bytes or actual native media playback.
func generatedWindowFMP4BindingFixture(t *testing.T, count int) hlsGeneratedWindowBinding {
	t.Helper()
	plan, lists, identities := generatedWindowArtifactSetFixture("fmp4", count)
	plan.HLS.Window.NativeClockVersion = transcode.GeneratedWindowNativeClockV2
	artifacts, err := hlsGeneratedWindowArtifactSetFromPlaylists("fmp4-closed-window", plan, lists, identities)
	if err != nil {
		t.Fatal(err)
	}
	binding := hlsGeneratedWindowBinding{
		producer: hlsProducer{id: artifacts.producerID, first: hlsGeneratedWindowProducer, ownership: &hlsProducerOwnership{}},
		plan:     plan, artifacts: artifacts,
		closure: transcode.GeneratedWindowClosure{StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks,
			Number: planNumber(plan), RenditionCount: max(1, count), NativeClockVersion: transcode.GeneratedWindowNativeClockV2},
		nativeEmission: transcode.GeneratedFMP4NativeEmission{StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks, RenditionCount: max(1, count)},
	}
	for variant := 0; variant < max(1, count); variant++ {
		binding.artifactNames[variant], binding.artifactIdentities[variant] = artifacts.resources[variant][0].name, artifacts.resources[variant][0].identity
		binding.nativeEmission.TrackIDs[variant], binding.nativeEmission.MediaTimeScales[variant] = 1, 12288
		binding.nativeEmission.InitializationSHA256[variant][0] = byte(variant + 1)
		binding.nativeEmission.SegmentSHA256[variant][0] = byte(variant + 11)
		binding.closure.Output[variant].InitializationSHA256 = binding.nativeEmission.InitializationSHA256[variant]
		binding.closure.Output[variant].SegmentSHA256 = binding.nativeEmission.SegmentSHA256[variant]
		binding.redirectPins[variant], binding.initializationPins[variant] = &transcode.ReadHandle{}, &transcode.ReadHandle{}
	}
	binding.artifactName, binding.artifactIdentity = binding.artifactNames[0], binding.artifactIdentities[0]
	return binding
}

func TestGeneratedWindowFMP4BindingSelectsIndependentRolesWithoutBorrowingPins(t *testing.T) {
	for _, count := range []int{0, 2} {
		binding := generatedWindowFMP4BindingFixture(t, count)
		runtime := &hlsRuntime{}
		session := &hlsSession{windowGraph: &hlsGeneratedWindowGraph{published: true, slots: map[int]hlsGeneratedWindowBinding{15: binding}}}
		for variant := 0; variant < max(1, count); variant++ {
			for _, initialization := range []bool{false, true} {
				resource := hlsGeneratedWindowResource{number: 15, variant: variant, initialization: initialization}
				selected, valid := hlsGeneratedWindowBindingResource(binding, resource)
				artifact := binding.artifacts.resources[variant][hlsGeneratedWindowResourceIndex(resource)]
				if !valid || selected.artifactName != artifact.name || selected.artifactIdentity != artifact.identity ||
					selected.producer != binding.producer || selected.plan != binding.plan || selected.closure != binding.closure ||
					selected.artifacts != binding.artifacts || selected.nativeEmission != binding.nativeEmission || selected.redirectPin != nil ||
					selected.redirectPins != ([transcode.MaxHLSRenditions]*transcode.ReadHandle{}) ||
					selected.initializationPins != ([transcode.MaxHLSRenditions]*transcode.ReadHandle{}) {
					t.Fatal("one resource detached its independent identity from the complete proof or borrowed a retained reader")
				}
				session.mu.Lock()
				lookedUp, owned := runtime.generatedWindowBindingLocked(session, binding.producer.id, artifact.name)
				session.mu.Unlock()
				if !owned || lookedUp.artifactIdentity != artifact.identity {
					t.Fatal("exact MAP or media lookup lost its closed producer ownership")
				}
				resource.number++
				if _, valid := hlsGeneratedWindowBindingResource(binding, resource); valid {
					t.Fatal("another source slot acquired the held MAP or media identity")
				}
			}
		}
		for _, name := range []string{"window-init-000015.mp4", "window-segment-000015.m4s", "v01-init.mp4", "../init.mp4", "init.mp4?slot=15", "segment-000016.m4s", "main.m3u8"} {
			session.mu.Lock()
			_, owned := runtime.generatedWindowBindingLocked(session, binding.producer.id, name)
			session.mu.Unlock()
			if owned {
				t.Fatal("an admission URI, alias or private playlist acquired raw artifact authority")
			}
		}
	}
}

func TestGeneratedWindowFMP4BindingRejectsWholeSiblingAndClockSubstitution(t *testing.T) {
	for name, mutate := range map[string]func(*hlsGeneratedWindowBinding){
		"missing sibling initialization": func(binding *hlsGeneratedWindowBinding) { binding.artifacts.resources[1][1].identity = "" },
		"aliased roles": func(binding *hlsGeneratedWindowBinding) {
			binding.artifacts.resources[1][1].identity = binding.artifacts.resources[1][0].identity
		},
		"unpaired sibling initialization": func(binding *hlsGeneratedWindowBinding) { binding.nativeEmission.InitializationSHA256[1][0]++ },
		"unpaired sibling media":          func(binding *hlsGeneratedWindowBinding) { binding.closure.Output[1].SegmentSHA256[0]++ },
		"different source interval":       func(binding *hlsGeneratedWindowBinding) { binding.closure.StartTicks++ },
		"different slot":                  func(binding *hlsGeneratedWindowBinding) { binding.closure.Number++ },
		"different native track":          func(binding *hlsGeneratedWindowBinding) { binding.nativeEmission.TrackIDs[1] = 2 },
		"zero scale":                      func(binding *hlsGeneratedWindowBinding) { binding.nativeEmission.MediaTimeScales[1] = 0 },
		"unused output":                   func(binding *hlsGeneratedWindowBinding) { binding.closure.Output[2].Video.Present = true },
		"unused input":                    func(binding *hlsGeneratedWindowBinding) { binding.closure.Input[2].Frames = 1 },
		"different producer table":        func(binding *hlsGeneratedWindowBinding) { binding.artifacts.producerID = "another-producer" },
	} {
		t.Run(name, func(t *testing.T) {
			binding := generatedWindowFMP4BindingFixture(t, 2)
			mutate(&binding)
			for _, initialization := range []bool{false, true} {
				if _, valid := hlsGeneratedWindowBindingResource(binding, hlsGeneratedWindowResource{number: 15, initialization: initialization}); valid {
					t.Fatal("a primary resource authorized incomplete or substituted sibling proof")
				}
			}
		})
	}
}
