package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"testing"
	"time"
)

func makeLongBoundarySources() map[string]source {
	sources := makeSyntheticSources("different-content")
	seed := uint32(0x12b0a1d5)
	common := make([]byte, 120*frameBytes)
	for i := range common {
		common[i] = syntheticRandom(&seed)
	}
	for index, id := range []string{"SYN-A", "SYN-B", "SYN-C"} {
		s := sources[id]
		start := 200 + index*70
		copy(s.Raw[start*frameBytes:(start+120)*frameBytes], common)
		hash := sha256.Sum256(s.Raw)
		s.Info.SourceSHA256, s.GraySHA256 = hex.EncodeToString(hash[:]), hex.EncodeToString(hash[:])
		sources[id] = s
	}
	return sources
}

func TestBoundaryPipelineFrozenFixtures(t *testing.T) {
	if os.Getenv("GOBY_INTRO_REGION_FULL_PIPELINE") != "1" {
		t.Skip("set GOBY_INTRO_REGION_FULL_PIPELINE=1 and a new GOBY_PRIVATE_BOUNDARY_OUTPUT path to run the full-prefix fixtures")
	}
	output := os.Getenv("GOBY_PRIVATE_BOUNDARY_OUTPUT")
	if output == "" {
		t.Fatal("explicit private boundary output path is required")
	}
	report := map[string]any{
		"protocol":                            reportProtocol,
		"productionResult":                    false,
		"independentHeldout":                  false,
		"implementation":                      implementationIdentity(),
		"scope":                               "Private development synthetic boundary evaluation; no real media accuracy claim.",
		"boundaryPolicy":                      "observed-common-component-v2",
		"originalEightSecondFixtureUnchanged": true,
		"twelveSecondFixture":                 "120 deterministic random 96x96 shared frames inserted into independent source-specific content at 20, 27 and 34 seconds.",
		"strictContainmentToleranceSeconds":   0,
		"expectedEightSecondResult":           "Abstention is required because the observation hull lasts less than eight seconds.",
		"requiredNonemptyPositive":            "The twelve-second shared moving case must retain at least one fully remeasured contained witness.",
		"startedUTC":                          time.Now().UTC(),
	}
	cases := []map[string]any{}
	defer func() {
		report["cases"], report["finishedUTC"], report["testPassed"] = cases, time.Now().UTC(), !t.Failed()
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		writer, err := newReportWriter(output)
		if err != nil {
			t.Error(err)
			return
		}
		defer writer.Close()
		if err := writer.Publish(append(data, '\n')); err != nil {
			t.Error(err)
		}
	}()
	for _, kind := range []string{"original-eight-second-moving", "twelve-second-moving", "shared-static", "different-content"} {
		started := time.Now()
		var sources map[string]source
		switch kind {
		case "original-eight-second-moving":
			sources = makeSyntheticSources("shared-moving")
		case "twelve-second-moving":
			sources = makeLongBoundarySources()
		default:
			sources = makeSyntheticSources(kind)
		}
		work := &budget{}
		prefix, err := completePrefix(sources, work)
		groups, _ := prefix["groups"].([]groupWitness)
		complete, _ := prefix["complete"].(bool)
		entry := map[string]any{"kind": kind, "elapsedSeconds": time.Since(started).Seconds(), "work": work, "prefix": prefix, "groupCount": len(groups), "complete": complete && err == nil}
		if err != nil {
			entry["error"] = err.Error()
			t.Errorf("%s failed closed: %v", kind, err)
		}
		if !complete {
			t.Errorf("%s did not complete", kind)
		}
		crossing := 0
		for _, group := range groups {
			for source := 0; source < 3; source++ {
				begin := float64(20 + source*7)
				if group.SourceBounds[source][0] < begin || group.SourceBounds[source][1] > begin+12 || group.SourceBounds[source][1]-group.SourceBounds[source][0] < 8 {
					crossing++
				}
				if group.SourceBounds[source][0] < group.ObservedEdgeBounds[source][0] || group.SourceBounds[source][1] > group.ObservedEdgeBounds[source][1] {
					t.Error("projected boundary escaped its observed source hull")
				}
			}
			if group.BoundaryPolicy != "observed-common-component-v2" || !spatial(group.SupportMask) || bits.OnesCount32(group.DynamicMask) < 4 || group.DynamicMask&^group.SupportMask != 0 || group.MinimumCoveragePermille < 850 {
				t.Error("group lost fixed-support audit evidence")
			}
		}
		entry["crossingSourceIntervals"] = crossing
		if kind == "twelve-second-moving" {
			if err == nil && (len(groups) == 0 || crossing > 0) {
				t.Errorf("required contained positive failed: %d groups, %d crossing source intervals", len(groups), crossing)
			}
		} else if len(groups) != 0 {
			t.Errorf("%s must abstain, emitted %d groups", kind, len(groups))
		}
		cases = append(cases, entry)
		t.Log(fmt.Sprintf("%s: complete=%v groups=%d patch=%d lookups=%d", kind, complete && err == nil, len(groups), work.PatchComparisons, work.ClockLookups))
	}
}

func TestBoundarySourceRejectsIncompleteOrUncertainPTS(t *testing.T) {
	s := source{PTS: make([]float64, 1200), Raw: make([]byte, 1200*frameBytes)}
	for i := range s.PTS {
		s.PTS[i] = float64(i) / 10
	}
	if err := validateBoundarySource(s, &budget{}); err != nil {
		t.Fatal(err)
	}
	s.PTS[20] = 2.1
	if validateBoundarySource(s, &budget{}) == nil {
		t.Fatal("uncertain slot PTS accepted")
	}
	s.PTS = s.PTS[:1199]
	if validateBoundarySource(s, &budget{}) == nil {
		t.Fatal("missing source slot accepted")
	}
}
