package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowArtifactSetFixture(segmentType string, count int) (transcode.Plan, [transcode.MaxHLSRenditions]transcode.MediaPlaylist,
	[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
	plan, _, _ := generatedWindowGraphPlanFixture()
	plan.StartTicks = 90 * media.TicksPerSecond
	plan.HLS.Window = transcode.HLSWindow{EndTicks: 96 * media.TicksPerSecond, StartNumber: 15, RequireInputEvidence: true}
	plan.HLS.SegmentType, plan.HLS.RenditionCount = segmentType, count
	plan.VideoBitrate = 512000
	if segmentType == "fmp4" {
		plan.Container = "mp4"
	}
	for variant := 0; variant < count; variant++ {
		plan.HLS.Renditions[variant] = transcode.HLSRendition{Width: plan.Width >> variant, Height: plan.Height >> variant,
			VideoBitrate: plan.VideoBitrate >> variant}
	}
	var lists [transcode.MaxHLSRenditions]transcode.MediaPlaylist
	var identities [transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string
	for variant := 0; variant < max(1, count); variant++ {
		resource := hlsGeneratedWindowResource{number: 15, variant: variant}
		name := hlsGeneratedWindowExactResourceName(plan, resource)
		resource.initialization = true
		initialization := hlsGeneratedWindowExactResourceName(plan, resource)
		lists[variant] = transcode.MediaPlaylist{Version: 7, TargetDuration: 6, Sequence: 15, Type: "EVENT", Ended: true,
			InitName: initialization, Segments: []transcode.MediaSegment{{Number: 15, Name: name,
				DurationTicks: 6 * media.TicksPerSecond, Discontinuity: true}}}
		identities[variant][0] = fmt.Sprintf("observed-media-identity-%d", variant)
		if initialization != "" {
			identities[variant][1] = fmt.Sprintf("observed-init-identity-%d", variant)
		}
	}
	return plan, lists, identities
}

func TestGeneratedWindowArtifactSetBindsSeparateMediaAndInitializationIdentities(t *testing.T) {
	for _, segmentType := range []string{"mpegts", "fmp4"} {
		for _, count := range []int{0, 2, transcode.MaxHLSRenditions} {
			plan, lists, identities := generatedWindowArtifactSetFixture(segmentType, count)
			set, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities)
			if err != nil {
				t.Fatalf("build exact resource table: segment_type=%s, count=%d, error=%v", segmentType, count, err)
			}
			for variant := 0; variant < max(1, count); variant++ {
				for _, initialization := range []bool{false, true} {
					resource := hlsGeneratedWindowResource{number: 15, variant: variant, initialization: initialization}
					artifact, found := set.selectResource("closed-window", plan, resource)
					if initialization && segmentType == "mpegts" {
						if found {
							t.Fatal("a TS slot acquired an initialization identity")
						}
						continue
					}
					if !found || artifact.name != hlsGeneratedWindowExactResourceName(plan, resource) ||
						artifact.identity != identities[variant][hlsGeneratedWindowResourceIndex(resource)] {
						t.Fatalf("resource selection lost its independent exact identity: %+v, %+v", resource, artifact)
					}
					parsed, found := set.resourceForExactName("closed-window", plan, artifact.name)
					if !found || parsed != resource {
						t.Fatalf("exact resource lookup lost its slot, rendition or map role: %q, %+v", artifact.name, parsed)
					}
					resource.number++
					if _, found := set.selectResource("closed-window", plan, resource); found {
						t.Fatal("another source slot borrowed this producer's exact resource")
					}
				}
			}
			for _, name := range []string{"", "v0.m3u8", "window-segment-000015.ts", "window-init-000015.mp4",
				"v01-init.mp4", "../v0-init.mp4", "v0-init.mp4?window=15", "v0-segment-000016.m4s", "v3-segment-000016.ts"} {
				if _, found := set.resourceForExactName("closed-window", plan, name); found {
					t.Fatalf("an alias or another slot was treated as an exact resource: %q", name)
				}
			}
		}
	}
}

func TestGeneratedWindowArtifactSetRejectsIncompleteOrSubstitutedPrivateResources(t *testing.T) {
	for name, mutate := range map[string]func(*[transcode.MaxHLSRenditions]transcode.MediaPlaylist,
		*[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string){
		"missing media identity": func(_ *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, identities *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			identities[1][0] = ""
		},
		"missing init identity": func(_ *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, identities *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			identities[1][1] = ""
		},
		"cross rendition media": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].Segments[0].Name = lists[0].Segments[0].Name
		},
		"cross rendition init": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].InitName = lists[0].InitName
		},
		"another source slot": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].Segments[0].Name = "v1-segment-000016.m4s"
		},
		"unfinished private list": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].Ended = false
		},
		"complete source list": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].Type = "VOD"
		},
		"another private sequence": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].Sequence++
		},
		"wrong private duration": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[1].Segments[0].DurationTicks--
		},
		"hidden unused resource": func(_ *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, identities *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			identities[2][1] = "unnegotiated-init-identity"
		},
		"hidden unused playlist": func(lists *[transcode.MaxHLSRenditions]transcode.MediaPlaylist, _ *[transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) {
			lists[2].InitName = "v2-init.mp4"
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, lists, identities := generatedWindowArtifactSetFixture("fmp4", 2)
			mutate(&lists, &identities)
			if _, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities); !errors.Is(err, transcode.ErrInvalidTimeline) {
				t.Fatalf("incomplete or substituted resource table was accepted: %v", err)
			}
		})
	}
	plan, lists, identities := generatedWindowArtifactSetFixture("mpegts", 2)
	identities[1][1] = "foreign-initialization-identity"
	if _, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities); !errors.Is(err, transcode.ErrInvalidTimeline) {
		t.Fatal("a TS resource table retained an unadvertised initialization identity")
	}
}

func TestGeneratedWindowArtifactSetSelectionCannotBypassIncompleteSiblingOrPlan(t *testing.T) {
	for name, mutate := range map[string]func(*hlsGeneratedWindowArtifactSet, *transcode.Plan){
		"missing sibling media": func(set *hlsGeneratedWindowArtifactSet, _ *transcode.Plan) { set.resources[1][0].identity = "" },
		"missing sibling map":   func(set *hlsGeneratedWindowArtifactSet, _ *transcode.Plan) { set.resources[1][1].identity = "" },
		"swapped resource roles": func(set *hlsGeneratedWindowArtifactSet, _ *transcode.Plan) {
			set.resources[1][0], set.resources[1][1] = set.resources[1][1], set.resources[1][0]
		},
		"unnegotiated identity":        func(set *hlsGeneratedWindowArtifactSet, _ *transcode.Plan) { set.resources[2][0].identity = "unused" },
		"another slot plan":            func(_ *hlsGeneratedWindowArtifactSet, plan *transcode.Plan) { plan.HLS.Window.StartNumber++ },
		"same name with another start": func(_ *hlsGeneratedWindowArtifactSet, plan *transcode.Plan) { plan.StartTicks-- },
		"same name with another end":   func(_ *hlsGeneratedWindowArtifactSet, plan *transcode.Plan) { plan.HLS.Window.EndTicks-- },
		"another container": func(_ *hlsGeneratedWindowArtifactSet, plan *transcode.Plan) {
			plan.HLS.SegmentType, plan.Container = "mpegts", "ts"
		},
		"another ladder": func(_ *hlsGeneratedWindowArtifactSet, plan *transcode.Plan) {
			plan.HLS.RenditionCount, plan.HLS.Renditions = 0, [transcode.MaxHLSRenditions]transcode.HLSRendition{}
		},
		"unproved plan": func(_ *hlsGeneratedWindowArtifactSet, plan *transcode.Plan) {
			plan.HLS.Window.RequireInputEvidence = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, lists, identities := generatedWindowArtifactSetFixture("fmp4", 2)
			set, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&set, &plan)
			if _, found := set.selectResource("closed-window", plan, hlsGeneratedWindowResource{number: 15}); found {
				t.Fatal("primary selection bypassed incomplete sibling evidence or changed plan")
			}
			if _, found := set.resourceForExactName("closed-window", plan, "v0-segment-000015.m4s"); found {
				t.Fatal("exact lookup bypassed incomplete sibling evidence or changed plan")
			}
		})
	}
}

func TestGeneratedWindowArtifactSetCannotBorrowAnotherProducerEpoch(t *testing.T) {
	plan, lists, identities := generatedWindowArtifactSetFixture("fmp4", 2)
	if _, err := hlsGeneratedWindowArtifactSetFromPlaylists("", plan, lists, identities); !errors.Is(err, transcode.ErrInvalidTimeline) {
		t.Fatal("an exact resource table was minted without a producer identity")
	}
	set, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities)
	if err != nil {
		t.Fatal(err)
	}
	for _, producerID := range []string{"", "another-window-with-the-same-plan"} {
		for _, initialization := range []bool{false, true} {
			if _, found := set.selectResource(producerID, plan, hlsGeneratedWindowResource{number: 15, variant: 1, initialization: initialization}); found {
				t.Fatal("another producer epoch borrowed this window's media or initialization identity")
			}
		}
		if _, found := set.resourceForExactName(producerID, plan, "v1-init.mp4"); found {
			t.Fatal("a reused initialization filename borrowed another producer's exact identity")
		}
	}
}

func TestGeneratedWindowArtifactSetRejectsObservedFilesystemAliasesAcrossResources(t *testing.T) {
	for _, segmentType := range []string{"mpegts", "fmp4"} {
		for name, roles := range map[string][4]int{
			"cross rendition media": {0, 0, 1, 0},
			"same rendition roles":  {0, 0, 0, 1},
			"cross rendition init":  {0, 1, 1, 1},
			"cross rendition roles": {0, 1, 1, 0},
		} {
			if segmentType == "mpegts" && (roles[1] == 1 || roles[3] == 1) {
				continue
			}
			t.Run(segmentType+"/"+name, func(t *testing.T) {
				plan, lists, identities := generatedWindowArtifactSetFixture(segmentType, 2)
				identities[roles[2]][roles[3]] = identities[roles[0]][roles[1]]
				if _, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities); !errors.Is(err, transcode.ErrInvalidTimeline) {
					t.Fatal("canonical names concealed an observed physical-resource alias")
				}

				plan, lists, identities = generatedWindowArtifactSetFixture(segmentType, 2)
				set, err := hlsGeneratedWindowArtifactSetFromPlaylists("closed-window", plan, lists, identities)
				if err != nil {
					t.Fatal(err)
				}
				set.resources[roles[2]][roles[3]].identity = set.resources[roles[0]][roles[1]].identity
				if _, found := set.selectResource("closed-window", plan, hlsGeneratedWindowResource{number: 15}); found {
					t.Fatal("primary selection bypassed a physical alias in another table resource")
				}
				if _, found := set.resourceForExactName("closed-window", plan, lists[0].Segments[0].Name); found {
					t.Fatal("exact lookup bypassed a physical alias in another table resource")
				}
			})
		}
	}
}
