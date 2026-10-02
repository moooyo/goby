package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowBudgetHistoryFixture(t *testing.T, budget *hlsInitializationBudget, count int) (*hlsGeneratedWindowBudgetHistory,
	hlsGeneratedWindowArtifactSet, [transcode.MaxHLSRenditions][32]byte) {
	t.Helper()
	original, artifacts, digests := generatedWindowInitializationHistoryFixture(t, count)
	artifacts.plan.HLS.Window.NativeClockVersion = transcode.GeneratedWindowNativeClockV2
	period := int64(original.basePlan.SegmentSeconds) * media.TicksPerSecond
	entries := int((original.basePlan.DurationTicks-1)/period + 1)
	history, err := newHLSGeneratedWindowBudgetHistory(budget, "presentation", strings.Repeat("a", 64), original.basePlan,
		transcode.GeneratedWindowNativeClockV2, entries)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(history.release)
	return history, artifacts, digests
}

func generatedWindowBudgetArtifactsAt(artifacts hlsGeneratedWindowArtifactSet, number int) hlsGeneratedWindowArtifactSet {
	plan := artifacts.plan
	period := int64(plan.SegmentSeconds) * media.TicksPerSecond
	plan.StartTicks = int64(number) * period
	plan.HLS.Window.StartNumber = number
	plan.HLS.Window.EndTicks = min(plan.StartTicks+period, plan.DurationTicks)
	artifacts.plan = plan
	for variant := 0; variant < max(1, plan.HLS.RenditionCount); variant++ {
		for index := range artifacts.resources[variant] {
			resource := hlsGeneratedWindowResource{number: number, variant: variant, initialization: index == 1}
			artifacts.resources[variant][index].name = hlsGeneratedWindowExactResourceName(plan, resource)
			artifacts.resources[variant][index].identity = fmt.Sprintf("slot-%d-variant-%d-role-%d", number, variant, index)
		}
	}
	return artifacts
}

func generatedWindowBudgetBinding(artifacts hlsGeneratedWindowArtifactSet, digests [transcode.MaxHLSRenditions][32]byte) hlsGeneratedWindowBinding {
	plan := artifacts.plan
	count := max(1, plan.HLS.RenditionCount)
	binding := hlsGeneratedWindowBinding{plan: plan, artifacts: artifacts, producer: hlsProducer{id: artifacts.producerID},
		closure: transcode.GeneratedWindowClosure{StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks,
			Number: plan.HLS.Window.StartNumber, RenditionCount: count, NativeClockVersion: transcode.GeneratedWindowNativeClockV2},
		nativeEmission: transcode.GeneratedFMP4NativeEmission{StartTicks: plan.StartTicks,
			EndTicks: plan.HLS.Window.EndTicks, RenditionCount: count, InitializationSHA256: digests}}
	for variant := 0; variant < count; variant++ {
		binding.nativeEmission.TrackIDs[variant] = 1
		binding.nativeEmission.MediaTimeScales[variant] = 12288
		binding.nativeEmission.SegmentSHA256[variant][0] = byte(variant + 11)
		binding.closure.Output[variant].InitializationSHA256 = digests[variant]
		binding.closure.Output[variant].SegmentSHA256 = binding.nativeEmission.SegmentSHA256[variant]
	}
	return binding
}

func TestGeneratedWindowBudgetHistoryConstructorRequiresExactCapacityAndV2(t *testing.T) {
	original, _, _ := generatedWindowInitializationHistoryFixture(t, 2)
	type arguments struct {
		budget   *hlsInitializationBudget
		graphID  string
		sourceID string
		plan     transcode.Plan
		version  uint8
		entries  int
	}
	for name, mutate := range map[string]func(*arguments){
		"nil arena":        func(a *arguments) { a.budget = nil },
		"missing graph":    func(a *arguments) { a.graphID = "" },
		"missing source":   func(a *arguments) { a.sourceID = "" },
		"uppercase source": func(a *arguments) { a.sourceID = strings.Repeat("A", 64) },
		"nonhex source":    func(a *arguments) { a.sourceID = strings.Repeat("z", 64) },
		"nonzero start":    func(a *arguments) { a.plan.StartTicks = media.TicksPerSecond },
		"windowed base": func(a *arguments) {
			a.plan.HLS.Window = transcode.HLSWindow{EndTicks: 6 * media.TicksPerSecond}
		},
		"TS container":      func(a *arguments) { a.plan.HLS.SegmentType, a.plan.Container = "mpegts", "ts" },
		"zero duration":     func(a *arguments) { a.plan.DurationTicks = 0 },
		"zero period":       func(a *arguments) { a.plan.SegmentSeconds = 0 },
		"zero entries":      func(a *arguments) { a.entries = 0 },
		"negative entries":  func(a *arguments) { a.entries = -1 },
		"short capacity":    func(a *arguments) { a.entries = 16 },
		"extra capacity":    func(a *arguments) { a.entries = 18 },
		"timeline overflow": func(a *arguments) { a.entries = transcode.MaxTimelineSegments + 1 },
		"integer overflow":  func(a *arguments) { a.entries = int(^uint(0) >> 1) },
		"generic clock":     func(a *arguments) { a.version = 0 },
		"TS clock":          func(a *arguments) { a.version = transcode.GeneratedWindowNativeClockV1 },
		"unknown clock":     func(a *arguments) { a.version = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			budget := &hlsInitializationBudget{}
			a := arguments{budget: budget, graphID: "presentation", sourceID: strings.Repeat("a", 64), plan: original.basePlan,
				version: transcode.GeneratedWindowNativeClockV2, entries: 17}
			mutate(&a)
			if history, err := newHLSGeneratedWindowBudgetHistory(a.budget, a.graphID, a.sourceID, a.plan, a.version, a.entries); err == nil || history != nil {
				t.Fatal("invalid presentation acquired a metadata reservation")
			}
			if budget.reserved != 0 {
				t.Fatal("rejected constructor consumed metadata capacity")
			}
		})
	}
	budget := &hlsInitializationBudget{}
	history, _, _ := generatedWindowBudgetHistoryFixture(t, budget, 2)
	if history.lease.entries != 17 || budget.reserved != 1 {
		t.Fatal("full presentation capacity was not charged before any initialization record")
	}
}

func TestGeneratedWindowBudgetHistoryArenaExhaustionDoesNotFallback(t *testing.T) {
	if hlsInitializationPageBytes != 64<<10 || hlsInitializationPageCount != 256 || hlsInitializationEntryBytes != 129 {
		t.Fatal("metadata arena no longer has the fixed 16-MiB, 129-byte-record contract")
	}
	budget := &hlsInitializationBudget{}
	defer budget.close()
	var first *hlsInitializationReservation
	for index := 0; index < 256; index++ {
		lease, err := budget.reserve(1)
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = lease
		}
	}
	original, _, _ := generatedWindowInitializationHistoryFixture(t, 2)
	history, err := newHLSGeneratedWindowBudgetHistory(budget, "presentation", strings.Repeat("a", 64), original.basePlan,
		transcode.GeneratedWindowNativeClockV2, 17)
	if !errors.Is(err, transcode.ErrBusy) || history != nil || budget.reserved != 256 {
		t.Fatal("an exhausted arena admitted an uncharged presentation history")
	}
	if generatedWindowUnsupported(err) {
		t.Fatal("metadata exhaustion was classified as permission to fall back")
	}
	first.release()
	history, err = newHLSGeneratedWindowBudgetHistory(budget, "presentation", strings.Repeat("a", 64), original.basePlan,
		transcode.GeneratedWindowNativeClockV2, 17)
	if err != nil || history == nil || budget.reserved != 256 {
		t.Fatal("returned metadata capacity could not admit a new presentation")
	}
}

func TestGeneratedWindowBudgetHistoryPreservesHashesAcrossProducerReplacement(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(fmt.Sprintf("renditions-%d", count), func(t *testing.T) {
			budget := &hlsInitializationBudget{}
			history, artifacts, digests := generatedWindowBudgetHistoryFixture(t, budget, count)
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
				t.Fatal(err)
			}
			replacement := artifacts
			replacement.producerID = "replacement-producer"
			for variant := 0; variant < max(1, count); variant++ {
				for index := range replacement.resources[variant] {
					replacement.resources[variant][index].identity = fmt.Sprintf("replacement-variant-%d-role-%d", variant, index)
				}
			}
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), replacement, digests); err != nil {
				t.Fatal("identical MAP bytes did not survive producer and disk identity replacement")
			}
			changed := digests
			changed[max(1, count)-1][31]--
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), replacement, changed); !errors.Is(err, transcode.ErrOutputUnavailable) {
				t.Fatal("a changed sibling replaced retained initialization bytes")
			}
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), replacement, digests); err != nil || budget.reserved != 1 {
				t.Fatal("rejected replacement changed the full retained digest set or its reservation")
			}
		})
	}
}

func TestGeneratedWindowBudgetHistoryRejectsForeignOrIncompleteEntryBeforeCommit(t *testing.T) {
	for name, mutate := range map[string]func(*hlsGeneratedWindowArtifactSet, *[transcode.MaxHLSRenditions][32]byte, *string, *string){
		"different graph": func(_ *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, graph, _ *string) {
			*graph = "another-presentation"
		},
		"different source": func(_ *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, source *string) {
			*source = strings.Repeat("b", 64)
		},
		"different source start": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.StartTicks--
		},
		"different slot": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.HLS.Window.StartNumber++
		},
		"negative slot": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.HLS.Window.StartNumber = -1
		},
		"slot past capacity": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.HLS.Window.StartNumber = 17
		},
		"different end": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.HLS.Window.EndTicks--
		},
		"different duration": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.DurationTicks += media.TicksPerSecond
		},
		"different ladder": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.HLS.Renditions[1].Width += 2
		},
		"generic clock": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.plan.HLS.Window.NativeClockVersion = 0
		},
		"missing producer": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.producerID = ""
		},
		"missing media sibling": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.resources[1][0].identity = ""
		},
		"missing MAP sibling": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.resources[1][1].identity = ""
		},
		"foreign MAP name": func(a *hlsGeneratedWindowArtifactSet, _ *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			a.resources[1][1].name = "v0-init.mp4"
		},
		"missing primary digest": func(_ *hlsGeneratedWindowArtifactSet, d *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			d[0] = [32]byte{}
		},
		"missing sibling digest": func(_ *hlsGeneratedWindowArtifactSet, d *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			d[1] = [32]byte{}
		},
		"unused digest": func(_ *hlsGeneratedWindowArtifactSet, d *[transcode.MaxHLSRenditions][32]byte, _, _ *string) {
			d[2][31] = 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			budget := &hlsInitializationBudget{}
			history, artifacts, digests := generatedWindowBudgetHistoryFixture(t, budget, 2)
			invalid, incomplete := artifacts, digests
			graph, source := "presentation", strings.Repeat("a", 64)
			mutate(&invalid, &incomplete, &graph, &source)
			if err := history.validateAndRemember(graph, source, invalid, incomplete); err == nil {
				t.Fatal("foreign or incomplete evidence acquired a logical MAP record")
			}
			digests[0][31] ^= 128
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
				t.Fatal("a rejected entry partially committed initialization bytes")
			}
		})
	}
}

func TestGeneratedWindowBudgetHistoryKeepsAdjacentSlotsAndExactTailIndependent(t *testing.T) {
	budget := &hlsInitializationBudget{}
	original, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 2)
	original.basePlan.DurationTicks = 600 * 6 * media.TicksPerSecond
	artifacts.plan.DurationTicks = original.basePlan.DurationTicks
	artifacts.plan.HLS.Window.NativeClockVersion = transcode.GeneratedWindowNativeClockV2
	history, err := newHLSGeneratedWindowBudgetHistory(budget, "presentation", strings.Repeat("a", 64), original.basePlan,
		transcode.GeneratedWindowNativeClockV2, 600)
	if err != nil {
		t.Fatal(err)
	}
	defer history.release()
	for _, number := range []int{507, 508, 509, 599} {
		slotDigests := digests
		slotDigests[0][1], slotDigests[1][31] = byte(number), byte(number>>8)
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), generatedWindowBudgetArtifactsAt(artifacts, number), slotDigests); err != nil {
			t.Fatalf("write independent slot %d: %v", number, err)
		}
	}
	for _, number := range []int{507, 508, 509, 599} {
		slotDigests := digests
		slotDigests[0][1], slotDigests[1][31] = byte(number), byte(number>>8)
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), generatedWindowBudgetArtifactsAt(artifacts, number), slotDigests); err != nil {
			t.Fatalf("another slot overwrote retained record %d: %v", number, err)
		}
		slotDigests[1][31] ^= 128
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), generatedWindowBudgetArtifactsAt(artifacts, number), slotDigests); !errors.Is(err, transcode.ErrOutputUnavailable) {
			t.Fatalf("slot %d lost its complete digest record: %v", number, err)
		}
	}
	if budget.reserved != 2 {
		t.Fatal("600 source slots did not retain their two-page reservation")
	}
	tailBudget := &hlsInitializationBudget{}
	tailHistory, tailArtifacts, tailDigests := generatedWindowBudgetHistoryFixture(t, tailBudget, 2)
	tail := generatedWindowBudgetArtifactsAt(tailArtifacts, 16)
	if tail.plan.HLS.Window.EndTicks-tail.plan.StartTicks != 4*media.TicksPerSecond {
		t.Fatal("fixture lost the exact final source interval")
	}
	if err := tailHistory.validateAndRemember("presentation", strings.Repeat("a", 64), tail, tailDigests); err != nil {
		t.Fatal("last valid slot could not retain its exact shorter tail")
	}
}

func TestGeneratedWindowBudgetHistoryExecutionNormalizationIsLimitedToUnspecifiedBase(t *testing.T) {
	for _, capturedBase := range []bool{false, true} {
		t.Run(fmt.Sprintf("captured-base-%t", capturedBase), func(t *testing.T) {
			original, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 2)
			artifacts.plan.HLS.Window.NativeClockVersion = transcode.GeneratedWindowNativeClockV2
			captured, err := transcode.CaptureExecution(artifacts.plan, transcode.DefaultExecutionOptions(2))
			if err != nil {
				t.Fatal(err)
			}
			artifacts.plan = captured
			if capturedBase {
				original.basePlan.ExecutionVersion, original.basePlan.Execution = captured.ExecutionVersion, captured.Execution
			}
			budget := &hlsInitializationBudget{}
			history, err := newHLSGeneratedWindowBudgetHistory(budget, "presentation", strings.Repeat("a", 64), original.basePlan,
				transcode.GeneratedWindowNativeClockV2, 17)
			if err != nil {
				t.Fatal(err)
			}
			defer history.release()
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
				t.Fatal("valid captured execution could not match its presentation")
			}
			changed := artifacts
			changed.plan, err = transcode.CaptureExecution(changed.plan, transcode.DefaultExecutionOptions(3))
			if err != nil {
				t.Fatal(err)
			}
			err = history.validateAndRemember("presentation", strings.Repeat("a", 64), changed, digests)
			if capturedBase && !errors.Is(err, transcode.ErrInvalidTimeline) || !capturedBase && err != nil {
				t.Fatalf("execution normalization escaped the original base profile: %v", err)
			}
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
				t.Fatal("execution rejection changed retained initialization bytes")
			}
		})
	}
}

func TestGeneratedWindowBindingInitializationRejectsInconsistentCompactProofBeforeCommit(t *testing.T) {
	for name, mutate := range map[string]func(*hlsGeneratedWindowBinding){
		"generic plan clock": func(b *hlsGeneratedWindowBinding) { b.plan.HLS.Window.NativeClockVersion = 0 },
		"unsupported native rate": func(b *hlsGeneratedWindowBinding) {
			b.plan.FrameRate = 30
			b.artifacts.plan = b.plan
		},
		"unproved plan":          func(b *hlsGeneratedWindowBinding) { b.plan.HLS.Window.RequireInputEvidence = false },
		"generic closure":        func(b *hlsGeneratedWindowBinding) { b.closure.NativeClockVersion = 0 },
		"closure start":          func(b *hlsGeneratedWindowBinding) { b.closure.StartTicks-- },
		"closure end":            func(b *hlsGeneratedWindowBinding) { b.closure.EndTicks-- },
		"closure slot":           func(b *hlsGeneratedWindowBinding) { b.closure.Number++ },
		"closure count":          func(b *hlsGeneratedWindowBinding) { b.closure.RenditionCount-- },
		"emission start":         func(b *hlsGeneratedWindowBinding) { b.nativeEmission.StartTicks-- },
		"emission end":           func(b *hlsGeneratedWindowBinding) { b.nativeEmission.EndTicks-- },
		"emission count":         func(b *hlsGeneratedWindowBinding) { b.nativeEmission.RenditionCount-- },
		"missing MAP sibling":    func(b *hlsGeneratedWindowBinding) { b.artifacts.resources[1][1].identity = "" },
		"foreign producer":       func(b *hlsGeneratedWindowBinding) { b.artifacts.producerID = "" },
		"missing track":          func(b *hlsGeneratedWindowBinding) { b.nativeEmission.TrackIDs[1] = 0 },
		"foreign native track":   func(b *hlsGeneratedWindowBinding) { b.nativeEmission.TrackIDs[1] = 2 },
		"missing timescale":      func(b *hlsGeneratedWindowBinding) { b.nativeEmission.MediaTimeScales[1] = 0 },
		"negative timescale":     func(b *hlsGeneratedWindowBinding) { b.nativeEmission.MediaTimeScales[1] = -1 },
		"missing init digest":    func(b *hlsGeneratedWindowBinding) { b.nativeEmission.InitializationSHA256[1] = [32]byte{} },
		"missing media digest":   func(b *hlsGeneratedWindowBinding) { b.nativeEmission.SegmentSHA256[1] = [32]byte{} },
		"closure init mismatch":  func(b *hlsGeneratedWindowBinding) { b.closure.Output[1].InitializationSHA256[31]-- },
		"closure media mismatch": func(b *hlsGeneratedWindowBinding) { b.closure.Output[1].SegmentSHA256[31] = 1 },
		"inactive init digest":   func(b *hlsGeneratedWindowBinding) { b.nativeEmission.InitializationSHA256[2][0] = 1 },
		"inactive media digest":  func(b *hlsGeneratedWindowBinding) { b.nativeEmission.SegmentSHA256[2][0] = 1 },
		"inactive track":         func(b *hlsGeneratedWindowBinding) { b.nativeEmission.TrackIDs[2] = 1 },
		"inactive timescale":     func(b *hlsGeneratedWindowBinding) { b.nativeEmission.MediaTimeScales[2] = 12288 },
		"inactive closure init":  func(b *hlsGeneratedWindowBinding) { b.closure.Output[2].InitializationSHA256[0] = 1 },
		"inactive closure media": func(b *hlsGeneratedWindowBinding) { b.closure.Output[2].SegmentSHA256[0] = 1 },
		"inactive input proof":   func(b *hlsGeneratedWindowBinding) { b.closure.Input[2].Rendition = 2 },
		"inactive packet proof":  func(b *hlsGeneratedWindowBinding) { b.closure.Output[2].Video.PacketCount = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			budget := &hlsInitializationBudget{}
			history, artifacts, digests := generatedWindowBudgetHistoryFixture(t, budget, 2)
			graph := &hlsGeneratedWindowGraph{initialization: history, endpoint: transcode.GeneratedSourceEndpointCertificate{SourceIdentity: strings.Repeat("a", 64)}}
			binding := generatedWindowBudgetBinding(artifacts, digests)
			invalid := binding
			mutate(&invalid)
			if err := graph.rememberBinding("presentation", invalid); err == nil {
				t.Fatal("inconsistent compact proof committed initialization bytes")
			}
			digests[0][31] ^= 128
			if err := graph.rememberBinding("presentation", generatedWindowBudgetBinding(artifacts, digests)); err != nil {
				t.Fatal("a rejected binding partially committed the first initialization epoch")
			}
		})
	}
}

func TestGeneratedWindowBindingInitializationRequiresLeaseAndPreservesSiblingEpoch(t *testing.T) {
	budget := &hlsInitializationBudget{}
	history, artifacts, digests := generatedWindowBudgetHistoryFixture(t, budget, 2)
	graph := &hlsGeneratedWindowGraph{initialization: history, endpoint: transcode.GeneratedSourceEndpointCertificate{SourceIdentity: strings.Repeat("a", 64)}}
	binding := generatedWindowBudgetBinding(artifacts, digests)
	var missing *hlsGeneratedWindowGraph
	if err := missing.rememberBinding("presentation", binding); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("nil graph admitted an fMP4 initialization epoch")
	}
	if err := (&hlsGeneratedWindowGraph{}).rememberBinding("presentation", binding); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("unreserved graph admitted an fMP4 initialization epoch")
	}
	if err := graph.rememberBinding("another-presentation", binding); err == nil {
		t.Fatal("foreign presentation borrowed a reserved initialization epoch")
	}
	graph.endpoint.SourceIdentity = strings.Repeat("b", 64)
	if err := graph.rememberBinding("presentation", binding); err == nil {
		t.Fatal("foreign source borrowed a reserved initialization epoch")
	}
	graph.endpoint.SourceIdentity = strings.Repeat("a", 64)
	if err := graph.rememberBinding("presentation", binding); err != nil {
		t.Fatal(err)
	}
	replacement := artifacts
	replacement.producerID = "replacement-producer"
	changed := digests
	changed[1][31]--
	if err := graph.rememberBinding("presentation", generatedWindowBudgetBinding(replacement, changed)); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatal("a closed replacement producer changed one retained MAP sibling")
	}
	if err := graph.rememberBinding("presentation", generatedWindowBudgetBinding(replacement, digests)); err != nil {
		t.Fatal("rejected replacement overwrote the original complete MAP set")
	}
	history.release()
	if err := graph.rememberBinding("presentation", binding); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("released graph retained access to recycled initialization pages")
	}
	if err := missing.rememberBinding("", hlsGeneratedWindowBinding{plan: transcode.Plan{HLS: transcode.HLSPlan{SegmentType: "mpegts"}}}); err != nil {
		t.Fatal("TS binding unexpectedly required initialization retention")
	}
}

func TestGeneratedWindowBudgetHistoryReleaseCannotAlterRecycledLease(t *testing.T) {
	budget := &hlsInitializationBudget{}
	history, artifacts, digests := generatedWindowBudgetHistoryFixture(t, budget, 2)
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
		t.Fatal(err)
	}
	oldPage := &budget.pages[0][0]
	history.release()
	replacement, replacementArtifacts, changed := generatedWindowBudgetHistoryFixture(t, budget, 2)
	changed[1][31]--
	if err := replacement.validateAndRemember("presentation", strings.Repeat("a", 64), replacementArtifacts, changed); err != nil {
		t.Fatal("new presentation inherited initialization bytes from a released lease")
	}
	history.release()
	if &budget.pages[0][0] != oldPage || budget.reserved != 1 || budget.owners[0] != replacement.lease {
		t.Fatal("stale release altered the recycled page owner or its reservation")
	}
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("stale history wrote to a replacement presentation's metadata pages")
	}
	if err := replacement.validateAndRemember("presentation", strings.Repeat("a", 64), replacementArtifacts, changed); err != nil {
		t.Fatal("stale history changed the replacement initialization epoch")
	}
}

func TestGeneratedWindowInitializationLifecyclePauseRetainsAndRetireReleasesHistory(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "initialization-lifetime", false)
	history, artifacts, digests := generatedWindowBudgetHistoryFixture(t, &h.initializationBudget, 2)
	delete(h.byKey, session.key)
	session.key.plan = history.basePlan
	h.byKey[session.key] = session
	session.windowGraph = &hlsGeneratedWindowGraph{initialization: history, basePlan: history.basePlan,
		endpoint: transcode.GeneratedSourceEndpointCertificate{SourceIdentity: strings.Repeat("a", 64)}}
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
		t.Fatal(err)
	}
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Paused", artifacts.plan.StartTicks))
	if !session.demand.paused || h.initializationBudget.reserved != 1 || session.closed {
		t.Fatal("committed pause retired the living presentation's initialization history")
	}
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
		t.Fatal("paused presentation lost its retained MAP bytes")
	}
	h.retire(session)
	if !session.closed || session.ctx.Err() == nil || h.initializationBudget.reserved != 0 {
		t.Fatal("retirement did not fence the presentation and return its metadata reservation")
	}
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatal("retired presentation retained access to released initialization metadata")
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.ensured) != 0 {
		t.Fatal("metadata lifecycle started production")
	}
}

func TestGeneratedWindowInitializationLifecycleCloseReleasesAttachedAndUnattachedHistory(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "initialization-close", false)
	attached, artifacts, digests := generatedWindowBudgetHistoryFixture(t, &h.initializationBudget, 2)
	session.windowGraph = &hlsGeneratedWindowGraph{initialization: attached}
	unattached, unattachedArtifacts, unattachedDigests := generatedWindowBudgetHistoryFixture(t, &h.initializationBudget, 2)
	if h.initializationBudget.reserved != 2 {
		t.Fatal("fixture did not retain both attached and pending presentation reservations")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if !session.closed || !h.initializationBudget.closed || h.initializationBudget.reserved != 0 {
		t.Fatal("runtime close left a presentation metadata reservation active")
	}
	for _, retained := range []struct {
		history   *hlsGeneratedWindowBudgetHistory
		artifacts hlsGeneratedWindowArtifactSet
		digests   [transcode.MaxHLSRenditions][32]byte
	}{{attached, artifacts, digests}, {unattached, unattachedArtifacts, unattachedDigests}} {
		if err := retained.history.validateAndRemember("presentation", strings.Repeat("a", 64), retained.artifacts, retained.digests); !errors.Is(err, transcode.ErrJobNotFound) {
			t.Fatal("runtime close left attached or unpublished metadata writable")
		}
	}
	if _, err := h.initializationBudget.reserve(1); !errors.Is(err, transcode.ErrManagerClosed) {
		t.Fatal("closed runtime admitted another metadata reservation")
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.ensured) != 0 {
		t.Fatal("metadata shutdown started production")
	}
}
