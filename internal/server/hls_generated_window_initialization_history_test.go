package server

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowInitializationHistoryFixture(t *testing.T, count int) (hlsGeneratedWindowInitializationHistory, hlsGeneratedWindowArtifactSet, [transcode.MaxHLSRenditions][32]byte) {
	t.Helper()
	plan, lists, identities := generatedWindowArtifactSetFixture("fmp4", count)
	base := plan
	base.StartTicks, base.HLS.Window = 0, transcode.HLSWindow{}
	history, err := newHLSGeneratedWindowInitializationHistory("presentation", strings.Repeat("a", 64), base, plan.HLS.Window.NativeClockVersion)
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := hlsGeneratedWindowArtifactSetFromPlaylists("first-producer", plan, lists, identities)
	if err != nil {
		t.Fatal(err)
	}
	var digests [transcode.MaxHLSRenditions][32]byte
	for variant := 0; variant < max(1, count); variant++ {
		digests[variant][0], digests[variant][31] = byte(variant+1), 254
	}
	return history, artifacts, digests
}

func TestGeneratedWindowInitializationHistorySurvivesProducerAndDiskIdentityReplacement(t *testing.T) {
	for _, count := range []int{0, 2, transcode.MaxHLSRenditions} {
		history, artifacts, digests := generatedWindowInitializationHistoryFixture(t, count)
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err != nil {
			t.Fatal(err)
		}
		// Producer eviction drops readers and artifacts, while the immutable
		// logical MAP contract remains attached to the living presentation.
		replacement := artifacts
		replacement.producerID = "replacement-producer"
		for variant := 0; variant < max(1, count); variant++ {
			replacement.resources[variant][0].identity = fmt.Sprintf("replacement-media-file-identity-%d", variant)
			replacement.resources[variant][1].identity = fmt.Sprintf("replacement-init-file-identity-%d", variant)
		}
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), replacement, digests); err != nil || len(history.slots) != 1 {
			t.Fatal("identical initialization bytes could not preserve a cached logical MAP across producer replacement")
		}
		changed := digests
		changed[max(1, count)-1][31]--
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), replacement, changed); !errors.Is(err, transcode.ErrOutputUnavailable) {
			t.Fatal("replacement media was allowed behind another epoch's cached initialization bytes")
		}
		if history.slots[15].digests != digests {
			t.Fatal("a rejected sibling changed the initialization contract already observed by the client")
		}
	}
}

func TestGeneratedWindowInitializationHistoryRejectsIncompleteSetBeforeAnyCommit(t *testing.T) {
	for _, missing := range []int{0, 1} {
		history, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 2)
		digests[missing] = [32]byte{}
		if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err == nil || len(history.slots) != 0 {
			t.Fatal("one complete rendition committed a MAP while its sibling lacked measured initialization bytes")
		}
	}
	history, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 2)
	digests[2][0] = 1
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err == nil || len(history.slots) != 0 {
		t.Fatal("unnegotiated initialization bytes escaped the exact rendition set")
	}
}

func TestGeneratedWindowInitializationHistoryDoesNotBorrowAnotherGraphSourceOrSlot(t *testing.T) {
	for name, mutate := range map[string]func(*hlsGeneratedWindowArtifactSet){
		"different source position": func(set *hlsGeneratedWindowArtifactSet) { set.plan.StartTicks-- },
		"different slot number":     func(set *hlsGeneratedWindowArtifactSet) { set.plan.HLS.Window.StartNumber++ },
		"different endpoint":        func(set *hlsGeneratedWindowArtifactSet) { set.plan.HLS.Window.EndTicks-- },
		"different duration":        func(set *hlsGeneratedWindowArtifactSet) { set.plan.DurationTicks += media.TicksPerSecond },
		"different ladder":          func(set *hlsGeneratedWindowArtifactSet) { set.plan.HLS.Renditions[1].Width += 2 },
		"missing media sibling":     func(set *hlsGeneratedWindowArtifactSet) { set.resources[1][0].identity = "" },
		"missing map sibling":       func(set *hlsGeneratedWindowArtifactSet) { set.resources[1][1].identity = "" },
		"foreign map name":          func(set *hlsGeneratedWindowArtifactSet) { set.resources[1][1].name = "v0-init.mp4" },
	} {
		t.Run(name, func(t *testing.T) {
			history, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 2)
			mutate(&artifacts)
			if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); err == nil || len(history.slots) != 0 {
				t.Fatal("foreign or incomplete private resources acquired this logical MAP contract")
			}
		})
	}
	for _, identities := range [][2]string{{"other-presentation", strings.Repeat("a", 64)}, {"presentation", strings.Repeat("b", 64)}, {"", ""}} {
		history, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 2)
		if err := history.validateAndRemember(identities[0], identities[1], artifacts, digests); err == nil || len(history.slots) != 0 {
			t.Fatal("another graph or source lifetime borrowed retained initialization hashes")
		}
	}
}

func TestGeneratedWindowInitializationHistoryIsBoundedAndContainerSpecific(t *testing.T) {
	history, artifacts, digests := generatedWindowInitializationHistoryFixture(t, 0)
	for _, sourceIdentity := range []string{"", strings.Repeat("A", 64), strings.Repeat("z", 64)} {
		if _, err := newHLSGeneratedWindowInitializationHistory("presentation", sourceIdentity, history.basePlan, 0); err == nil {
			t.Fatal("an unknown source identity acquired a retained MAP history")
		}
	}
	other := history.basePlan
	other.HLS.SegmentType, other.Container = "mpegts", "ts"
	if _, err := newHLSGeneratedWindowInitializationHistory("presentation", strings.Repeat("a", 64), other, 0); err == nil {
		t.Fatal("TS acquired initialization retention")
	}
	history.slots = make(map[int]hlsGeneratedWindowInitializationEpoch)
	for number := 0; number < transcode.MaxTimelineSegments; number++ {
		history.slots[number+transcode.MaxTimelineSegments] = hlsGeneratedWindowInitializationEpoch{}
	}
	if err := history.validateAndRemember("presentation", strings.Repeat("a", 64), artifacts, digests); !errors.Is(err, transcode.ErrBusy) {
		t.Fatal("retained initialization history exceeded its fixed presentation bound")
	}
}
