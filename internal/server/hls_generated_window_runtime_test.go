package server

import (
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowRuntimeBindingFixture(count int) hlsGeneratedWindowBinding {
	binding := hlsGeneratedWindowBinding{
		producer: hlsProducer{id: "closed-window", first: hlsGeneratedWindowProducer,
			ownership: &hlsProducerOwnership{}},
		plan: transcode.Plan{HLS: transcode.HLSPlan{SegmentType: "mpegts", RenditionCount: count,
			Window: transcode.HLSWindow{StartNumber: 15}}},
		closure: transcode.GeneratedWindowClosure{Number: 15, RenditionCount: max(1, count)},
	}
	for index := 0; index < max(1, count); index++ {
		prefix := ""
		if count != 0 {
			prefix = fmt.Sprintf("v%d-", index)
		}
		binding.artifactNames[index] = prefix + "segment-000015.ts"
		binding.artifactIdentities[index] = fmt.Sprintf("identity-%d", index)
		binding.redirectPins[index] = &transcode.ReadHandle{}
		binding.closure.Output[index].SegmentSHA256[0] = byte(index + 1)
	}
	binding.artifactName, binding.artifactIdentity = binding.artifactNames[0], binding.artifactIdentities[0]
	binding.redirectPin = binding.redirectPins[0]
	return binding
}

func TestGeneratedWindowVariantArtifactRejectsAliasesAndCrossRenditionNames(t *testing.T) {
	for _, count := range []int{0, 2, transcode.MaxHLSRenditions} {
		binding := generatedWindowRuntimeBindingFixture(count)
		for variant := 0; variant < max(1, count); variant++ {
			list := transcode.MediaPlaylist{Segments: []transcode.MediaSegment{{Name: binding.artifactNames[variant]}}}
			if !hlsGeneratedWindowVariantArtifact(binding.plan, list, variant) {
				t.Fatalf("canonical rendition artifact was rejected: count=%d, variant=%d", count, variant)
			}
			for _, name := range []string{
				"segment-000015.aac", "segment-000015.m4s", "segment-15.ts", "v01-segment-000015.ts",
				"segment-000016.ts", "../" + binding.artifactNames[variant], binding.artifactNames[variant] + "?variant=0",
				"%76" + binding.artifactNames[variant], "window-segment-000015.ts",
			} {
				list.Segments[0].Name = name
				if hlsGeneratedWindowVariantArtifact(binding.plan, list, variant) {
					t.Fatalf("an alias or another artifact type was accepted: count=%d, variant=%d, name=%q", count, variant, name)
				}
			}
			list.Segments[0].Name = binding.artifactNames[variant]
			list.InitName = "init.mp4"
			if hlsGeneratedWindowVariantArtifact(binding.plan, list, variant) {
				t.Fatal("a TS slot acquired an initialization resource")
			}
			if count > 0 {
				list.InitName = ""
				list.Segments[0].Name = binding.artifactNames[(variant+1)%count]
				if hlsGeneratedWindowVariantArtifact(binding.plan, list, variant) {
					t.Fatal("a different rendition's canonical name authorized the requested rendition")
				}
			}
		}
		for _, variant := range []int{-1, max(1, count), transcode.MaxHLSRenditions} {
			list := transcode.MediaPlaylist{Segments: []transcode.MediaSegment{{Name: binding.artifactNames[0]}}}
			if hlsGeneratedWindowVariantArtifact(binding.plan, list, variant) {
				t.Fatalf("a rendition outside the negotiated plan was accepted: count=%d, variant=%d", count, variant)
			}
		}
	}
	for _, count := range []int{-1, 1, transcode.MaxHLSRenditions + 1} {
		plan := transcode.Plan{HLS: transcode.HLSPlan{SegmentType: "mpegts", RenditionCount: count}}
		if _, valid := hlsGeneratedWindowVariantCount(plan); valid {
			t.Fatalf("an invalid negotiated rendition count was accepted: %d", count)
		}
	}
	for name, mutate := range map[string]func(*transcode.Plan){
		"fMP4":         func(plan *transcode.Plan) { plan.HLS.SegmentType = "fmp4" },
		"packed audio": func(plan *transcode.Plan) { plan.HLS.SegmentType = "packed" },
		"negative slot": func(plan *transcode.Plan) {
			plan.HLS.Window.StartNumber = -1
		},
		"oversized slot": func(plan *transcode.Plan) {
			plan.HLS.Window.StartNumber = transcode.MaxPlaylistSegments
		},
	} {
		t.Run(name, func(t *testing.T) {
			binding := generatedWindowRuntimeBindingFixture(2)
			mutate(&binding.plan)
			list := transcode.MediaPlaylist{Segments: []transcode.MediaSegment{{Name: binding.artifactNames[0]}}}
			if hlsGeneratedWindowVariantArtifact(binding.plan, list, 0) {
				t.Fatal("an unsupported artifact type or slot acquired a TS variant identity")
			}
		})
	}
}

func TestGeneratedWindowBindingVariantRetainsWholeProofAndExactIdentity(t *testing.T) {
	binding := generatedWindowRuntimeBindingFixture(transcode.MaxHLSRenditions)
	for variant := 0; variant < transcode.MaxHLSRenditions; variant++ {
		selected, valid := hlsGeneratedWindowBindingVariant(binding, variant)
		if !valid || selected.artifactName != binding.artifactNames[variant] || selected.artifactIdentity != binding.artifactIdentities[variant] ||
			selected.redirectPin != nil || selected.redirectPins != ([transcode.MaxHLSRenditions]*transcode.ReadHandle{}) ||
			selected.producer != binding.producer || selected.plan != binding.plan || selected.closure != binding.closure ||
			selected.artifactNames != binding.artifactNames || selected.artifactIdentities != binding.artifactIdentities {
			t.Fatalf("selection detached a rendition from its closed job and proof: variant=%d, selected=%+v", variant, selected)
		}
	}
	for name, mutate := range map[string]func(*hlsGeneratedWindowBinding){
		"missing identity":  func(binding *hlsGeneratedWindowBinding) { binding.artifactIdentities[1] = "" },
		"another rendition": func(binding *hlsGeneratedWindowBinding) { binding.artifactNames[1] = binding.artifactNames[0] },
		"another segment":   func(binding *hlsGeneratedWindowBinding) { binding.artifactNames[1] = "v1-segment-000016.ts" },
		"singular fallback": func(binding *hlsGeneratedWindowBinding) {
			binding.artifactNames, binding.artifactIdentities = [transcode.MaxHLSRenditions]string{}, [transcode.MaxHLSRenditions]string{}
			binding.artifactName, binding.artifactIdentity = "v1-segment-000015.ts", "primary-identity"
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := binding
			mutate(&mutated)
			if _, valid := hlsGeneratedWindowBindingVariant(mutated, 1); valid {
				t.Fatal("incomplete or substituted variant evidence authorized exact bytes")
			}
		})
	}
}

func TestGeneratedWindowBindingVariantCannotAuthorizeScalarOrPartialArrays(t *testing.T) {
	binding := generatedWindowRuntimeBindingFixture(0)
	binding.artifactNames, binding.artifactIdentities = [transcode.MaxHLSRenditions]string{}, [transcode.MaxHLSRenditions]string{}
	binding.redirectPins = [transcode.MaxHLSRenditions]*transcode.ReadHandle{}
	if _, valid := hlsGeneratedWindowBindingVariant(binding, 0); valid {
		t.Fatal("scalar fields authorized an artifact without the minted variant array")
	}
	for _, field := range []string{"name", "identity"} {
		partial := binding
		if field == "name" {
			partial.artifactNames[0] = binding.artifactName
		} else {
			partial.artifactIdentities[0] = binding.artifactIdentity
		}
		if _, valid := hlsGeneratedWindowBindingVariant(partial, 0); valid {
			t.Fatalf("singular fields filled an incomplete array entry: %s", field)
		}
	}
}

func TestGeneratedWindowBindingLookupOnlyAuthorizesOwnedExactVariants(t *testing.T) {
	binding := generatedWindowRuntimeBindingFixture(2)
	session := &hlsSession{windowGraph: &hlsGeneratedWindowGraph{published: true,
		slots: map[int]hlsGeneratedWindowBinding{15: binding}}}
	runtime := &hlsRuntime{}
	lookup := func(id, name string) (hlsGeneratedWindowBinding, bool) {
		session.mu.Lock()
		defer session.mu.Unlock()
		return runtime.generatedWindowBindingLocked(session, id, name)
	}
	for variant := 0; variant < 2; variant++ {
		selected, owned := lookup(binding.producer.id, binding.artifactNames[variant])
		if !owned || selected.artifactIdentity != binding.artifactIdentities[variant] || selected.closure != binding.closure {
			t.Fatalf("owned variant lookup lost its exact identity or shared closure: variant=%d", variant)
		}
	}
	root, owned := lookup(binding.producer.id, "")
	if !owned || root.artifactName != binding.artifactNames[0] || root.closure != binding.closure {
		t.Fatal("owned producer lookup lost the root proof")
	}
	for _, name := range []string{"main.m3u8", "v0.m3u8", "init.mp4", "segment-000015.ts", "v2-segment-000015.ts", "v1-segment-000016.ts", "../v1-segment-000015.ts"} {
		if _, owned := lookup(binding.producer.id, name); owned {
			t.Fatalf("an unproved artifact acquired the private producer's ownership: %q", name)
		}
	}
	if _, owned := lookup("another-producer", binding.artifactNames[1]); owned {
		t.Fatal("another producer acquired a retained variant's identity")
	}
	for _, state := range []string{"closed", "unpublished", "fallback", "released", "other producer kind"} {
		session.closed = false
		session.windowGraph.published, session.windowGraph.fallback = true, false
		binding.producer.ownership.released = false
		binding.producer.first = hlsGeneratedWindowProducer
		switch state {
		case "closed":
			session.closed = true
		case "unpublished":
			session.windowGraph.published = false
		case "fallback":
			session.windowGraph.fallback = true
		case "released":
			binding.producer.ownership.released = true
		case "other producer kind":
			binding.producer.first = -1
		}
		session.windowGraph.slots[15] = binding
		if _, owned := lookup(binding.producer.id, binding.artifactNames[1]); owned {
			t.Fatalf("a revoked ownership state authorized a variant: %s", state)
		}
	}
}
