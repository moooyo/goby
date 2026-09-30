package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scoreFixture(t *testing.T) (evaluationReport, scoreLabels) {
	t.Helper()
	stamp := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	report := evaluationReport{Protocol: reportProtocol, SchemaVersion: 1, Mode: "full-prefix", ResultKind: "research-candidates-with-observed-bounds", Completed: true, Status: "completed-with-groups", Stage: "clique-closure-and-observed-boundaries", InputPath: "/unused/cohort.json", InputSHA256: digest([]byte("input")), Implementation: implementationIdentity(), Work: &budget{PatchComparisons: 100, ClockLookups: 100, RenderPixels: 100, RetainedHypotheses: 1}, Limits: map[string]any{}, StartedUTC: stamp, FinishedUTC: stamp.Add(time.Second)}
	report.Limits = map[string]any{"patchComparisons": maxPatchComparisons, "clockLookups": maxClockLookups, "renderPixels": maxRenderPixels, "retainedHypotheses": maxHypotheses, "inputManifestBytes": maxInputManifestBytes, "extractionManifestBytes": maxExtractionBytes, "gzipBytesPerSource": maxGzipBytes, "rawBytesPerSource": expectedFrames * frameBytes, "deadlineSeconds": 120}
	labels := scoreLabels{SchemaVersion: 1, SourceOnlyReview: true, ReviewerKind: "assistant-source-only-visual"}
	group := groupWitness{AnchorStart: 20, AnchorEnd: 32, SupportMask: (1 << patchCount) - 1, DynamicMask: (1 << patchCount) - 1, MinimumCoveragePermille: 1000, PassingWindows: 1, BoundaryPolicy: "observed-common-component-v2", ComponentAnchorBounds: [2]float64{20, 32}}
	for i, id := range []string{"A", "B", "C"} {
		hash := digest([]byte("media-" + id))
		episode := "series:" + strings.ToLower(id)
		report.AdmittedSources = append(report.AdmittedSources, admittedSource{Source: sourceBinding{ID: id, SourceSHA256: hash, GraySHA256: digest([]byte("gray-" + id)), PTSSHA256: digest([]byte("pts-" + id)), Frames: expectedFrames, FirstPTS: 0, LastPTS: 119.9}, EpisodeKey: episode, ManifestPath: "/unused/" + id + ".json", ManifestSHA256: digest([]byte("manifest-" + id)), GzipPath: "/unused/" + id + ".gz", GzipSHA256: digest([]byte("gzip-" + id)), GzipBytes: 64, OrchestratorSHA256: digest([]byte("orchestrator")), DriverSHA256: digest([]byte("driver")), IndividualFrameHashesVerified: true})
		report.AdmittedSources[i].Source.PTSSHA256 = report.AdmittedSources[i].ManifestSHA256
		labels.Cases = append(labels.Cases, scoreLabel{ID: id, EpisodeKey: episode, SourceSHA256: hash, LabelClass: "positive", TargetSeconds: []scoreDecimal{"20", "32"}, ProtectedRanges: []scoreProtected{{Kind: "narrative", Seconds: []scoreDecimal{"100", "600"}}}, SourceLabelEvidenceSHA256: digest([]byte("source-labels")), Variant: scoreVariant{Support: "unknown", EvidenceSHA256: digest([]byte("source-review"))}, Notes: "Synthetic protocol fixture, not media ground truth."})
		group.SourceIDs[i], group.Geometries[i], group.SourceBounds[i], group.ObservedEdgeBounds[i] = id, neutral(), [2]float64{20, 32}, [2]float64{20, 32}
		if i < 2 {
			group.Clocks[i] = clockHypothesis{Scale: 1, PrefixSupportMask: group.SupportMask, PrefixDynamicMask: group.DynamicMask}
		}
	}
	report.Prefix = map[string]any{"complete": true, "productionResult": false, "groups": []groupWitness{group}, "closure": closureSummary{}}
	return report, labels
}
func writeScoreInputs(t *testing.T, report any, labels any) (string, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	reportPath, labelsPath := filepath.Join(dir, "report.json"), filepath.Join(dir, "labels.json")
	r := writeFixtureJSON(t, reportPath, report)
	l := writeFixtureJSON(t, labelsPath, labels)
	return reportPath, digest(r), labelsPath, digest(l)
}
func scoreValues(t *testing.T, report any, labels any) sourceTargetScore {
	t.Helper()
	rp, rh, lp, lh := writeScoreInputs(t, report, labels)
	return scoreFrozenReport(context.Background(), rp, rh, lp, lh)
}
func scoreRawObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}
func TestScoreObservedInteriorIsNotACompleteTargetHit(t *testing.T) {
	report, labels := scoreFixture(t)
	groups := report.Prefix["groups"].([]groupWitness)
	groups[0].AnchorEnd = 31.9
	for i := range groups[0].SourceBounds {
		groups[0].SourceBounds[i][1] = 31.9
	}
	report.Prefix["groups"] = groups
	result := scoreValues(t, report, labels)
	if !result.Completed || !result.ScoreAvailable || result.Status != "scored" {
		t.Fatalf("score failed: %+v", result)
	}
	for _, row := range result.Cases {
		if row.Outcome != "miss" || row.MissReason != "incomplete_target" || row.FullTargetCoverageMatch || !row.LegacyEndpointRuleHit || len(row.Candidates) != 1 {
			t.Fatalf("interior was promoted to full target: %+v", row)
		}
		candidate := row.Candidates[0]
		if candidate.StrictFullTargetCovered || !candidate.StrictContainedInTarget || candidate.MissingTailSeconds.String() != "0.1" || candidate.EndpointErrorsSeconds[1].String() != "0.1" || candidate.TargetCoverageRatio.String() != "0.991666666666666667" {
			t.Fatalf("wrong target coverage: %+v", candidate)
		}
	}
}
func TestScoreProtectionUsesExactMicrosecondArithmetic(t *testing.T) {
	_, labels := scoreFixture(t)
	label := labels.Cases[0]
	label.TargetSeconds = []scoreDecimal{"20", "32"}
	label.ProtectedRanges = []scoreProtected{{Kind: "episode-title", Seconds: []scoreDecimal{"32", "40"}}}
	for _, tc := range []struct {
		end       string
		violation bool
	}{{"32", false}, {"32.000000999999", false}, {"32.000001", false}, {"32.000001000001", true}, {"32.1", true}} {
		t.Run(tc.end, func(t *testing.T) {
			candidate := scoreOneCandidate(0, []scoreDecimal{"20", scoreDecimal(tc.end)}, label)
			if candidate.ProtectedViolation != tc.violation || candidate.LegacyEndpointRuleMatch == tc.violation || !candidate.StrictFullTargetCovered {
				t.Fatalf("protection epsilon changed: %+v", candidate)
			}
			if tc.end != "32" && len(candidate.ProtectedOverlaps) != 1 {
				t.Fatal("raw overlap was rounded away")
			}
		})
	}
}
func TestScoreFullCoverageStillRequiresEndpointRule(t *testing.T) {
	report, labels := scoreFixture(t)
	for i := range labels.Cases {
		labels.Cases[i].TargetSeconds = []scoreDecimal{"26", "28"}
	}
	result := scoreValues(t, report, labels)
	if !result.Completed {
		t.Fatal(result.Error)
	}
	for _, row := range result.Cases {
		if row.FullTargetCoverageMatch || row.LegacyEndpointRuleHit || !row.Candidates[0].StrictFullTargetCovered || row.Candidates[0].OutsideTargetSeconds.String() != "10" {
			t.Fatalf("overlong envelope was accepted: %+v", row)
		}
	}
}
func TestScoreNoCandidateAndIncompleteHaveDifferentAccounting(t *testing.T) {
	report, labels := scoreFixture(t)
	report.Prefix["groups"] = []groupWitness{}
	report.Status = "completed-abstention"
	labels.Cases[1].LabelClass, labels.Cases[1].TargetSeconds, labels.Cases[1].Variant.Support = "negative", nil, "not-applicable"
	result := scoreValues(t, report, labels)
	if !result.ScoreAvailable || result.Cases[0].Outcome != "miss" || result.Cases[0].TargetCoverageRatio.String() != "0" || result.Cases[1].Outcome != "correct_abstention" || result.Cases[1].TargetCoverageRatio != nil {
		t.Fatalf("empty evaluation accounting changed: %+v", result)
	}
	for _, count := range []int{0, 2, 3} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			blocked := report
			blocked.Completed, blocked.Status, blocked.Error = false, "deadline-exceeded", "context deadline exceeded"
			blocked.Prefix = map[string]any{"complete": false, "productionResult": false, "groups": []groupWitness{}}
			blocked.AdmittedSources = append([]admittedSource{}, report.AdmittedSources[:count]...)
			result := scoreValues(t, blocked, labels)
			if !result.Completed || result.ScoreAvailable || result.Status != "evaluation-blocked" {
				t.Fatalf("incomplete evaluation was misclassified: %+v", result)
			}
			for _, row := range result.Cases {
				if row.Outcome != "blocked" || row.TargetCoverageRatio != nil || row.FullTargetCoverageMatch || row.LegacyEndpointRuleHit {
					t.Fatal("incomplete evaluation became score evidence")
				}
			}
		})
	}
	report.Completed, report.Status, report.Error = false, "budget-exhausted", "clock lookup budget exceeded"
	report.Prefix = map[string]any{"complete": false, "productionResult": false, "groups": []groupWitness{}, "closure": map[string]any{"cohortBoundaryAmbiguous": true}}
	if result := scoreValues(t, report, labels); !result.Completed || result.ScoreAvailable || result.Status != "evaluation-blocked" {
		t.Fatalf("partial ambiguity diagnostic invalidated a blocked report: %+v", result)
	}
}
func TestScoreRejectsSourceIdentityDriftAndDuplicateEncodes(t *testing.T) {
	for _, field := range []string{"id", "episodeKey", "sourceSha256"} {
		t.Run(field, func(t *testing.T) {
			report, labels := scoreFixture(t)
			object := scoreRawObject(t, labels)
			cases := object["cases"].([]any)
			cases[0].(map[string]any)[field] = cases[1].(map[string]any)[field]
			result := scoreValues(t, report, object)
			if result.Completed || len(result.Cases) != 0 {
				t.Fatalf("duplicate identity was admitted: %+v", result)
			}
		})
	}
	report, labels := scoreFixture(t)
	report.AdmittedSources[0].EpisodeKey = "series:another-original"
	if result := scoreValues(t, report, labels); result.Completed {
		t.Fatal("report identity drift accepted")
	}
}
func TestScoreRejectsMissingNullAndMalformedLabelContracts(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"missing-target":       func(x map[string]any) { delete(x["cases"].([]any)[0].(map[string]any), "targetSeconds") },
		"null-positive-target": func(x map[string]any) { x["cases"].([]any)[0].(map[string]any)["targetSeconds"] = nil },
		"null-endpoint":        func(x map[string]any) { x["cases"].([]any)[0].(map[string]any)["targetSeconds"] = []any{nil, 32} },
		"extra-endpoint":       func(x map[string]any) { x["cases"].([]any)[0].(map[string]any)["targetSeconds"] = []any{20, 32, 33} },
		"reversed":             func(x map[string]any) { x["cases"].([]any)[0].(map[string]any)["targetSeconds"] = []any{32, 20} },
		"missing-false":        func(x map[string]any) { delete(x, "detectorOutputsUsed") },
		"unknown-field":        func(x map[string]any) { x["forgiveProtectedSeconds"] = 5 },
		"null-protected":       func(x map[string]any) { x["cases"].([]any)[0].(map[string]any)["protectedRanges"] = nil },
		"unknown-variant-key": func(x map[string]any) {
			x["cases"].([]any)[0].(map[string]any)["variant"].(map[string]any)["key"] = "looks-same"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			report, labels := scoreFixture(t)
			object := scoreRawObject(t, labels)
			mutate(object)
			result := scoreValues(t, report, object)
			if result.Completed || len(result.Cases) != 0 {
				t.Fatalf("bad labels accepted: %+v", result)
			}
		})
	}
}
func TestScoreRejectsMalformedGroupsAndConflictingCompletion(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"missing-completed": func(x map[string]any) { delete(x, "completed") },
		"missing-groups":    func(x map[string]any) { delete(x["prefix"].(map[string]any), "groups") },
		"missing-closure":   func(x map[string]any) { delete(x["prefix"].(map[string]any), "closure") },
		"incomplete-with-groups": func(x map[string]any) {
			x["completed"] = false
			x["status"] = "deadline-exceeded"
			x["error"] = "deadline"
			x["prefix"].(map[string]any)["complete"] = false
		},
		"abstention-with-groups": func(x map[string]any) { x["status"] = "completed-abstention" },
		"ambiguous-with-groups":  func(x map[string]any) { x["ambiguous"] = true },
		"missing-input-hash":     func(x map[string]any) { delete(x, "inputSha256") },
		"duplicate-source": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			g["sourceIDs"] = []any{"A", "A", "C"}
		},
		"short-sources": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			g["sourceIDs"] = []any{"A", "B"}
		},
		"extra-source": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			g["sourceIDs"] = []any{"A", "B", "C", "D"}
		},
		"null-bound": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			g["sourceBounds"].([]any)[0] = []any{nil, 32}
		},
		"extra-bound": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			g["sourceBounds"].([]any)[0] = []any{20, 32, 33}
		},
		"missing-evidence": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			delete(g, "observedEdgeBounds")
		},
		"missing-zero-geometry": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			delete(g["geometries"].([]any)[0].(map[string]any), "shiftX")
		},
		"closure-ambiguity-conflict": func(x map[string]any) {
			x["prefix"].(map[string]any)["closure"] = map[string]any{"cohortBoundaryAmbiguous": true}
		},
		"source-pts-binding": func(x map[string]any) {
			x["admittedSources"].([]any)[0].(map[string]any)["source"].(map[string]any)["ptsSha256"] = digest([]byte("different"))
		},
		"source-prefix-hull": func(x map[string]any) {
			x["admittedSources"].([]any)[0].(map[string]any)["source"].(map[string]any)["firstPTS"] = 20
		},
		"weak-coverage": func(x map[string]any) {
			x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)["minimumCoveragePermille"] = 849
		},
		"weak-support": func(x map[string]any) {
			x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)["supportMask"] = 1
		},
		"weak-motion": func(x map[string]any) {
			x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)["dynamicMask"] = 1
		},
		"clock-bound-conflict": func(x map[string]any) {
			g := x["prefix"].(map[string]any)["groups"].([]any)[0].(map[string]any)
			g["clocks"].([]any)[0].(map[string]any)["offset"] = 1
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			report, labels := scoreFixture(t)
			object := scoreRawObject(t, report)
			mutate(object)
			result := scoreValues(t, object, labels)
			if result.Completed || len(result.Cases) != 0 {
				t.Fatalf("malformed report accepted: %+v", result)
			}
		})
	}
}
func TestScoreDoesNotUnionOrChooseBestCandidate(t *testing.T) {
	report, labels := scoreFixture(t)
	group := report.Prefix["groups"].([]groupWitness)[0]
	second := group
	group.AnchorEnd, second.AnchorStart = 29, 23
	for i := 0; i < 3; i++ {
		group.SourceBounds[i][1] = 29
		second.SourceBounds[i][0] = 23
	}
	report.Prefix["groups"] = []groupWitness{group, second}
	result := scoreValues(t, report, labels)
	if !result.Completed {
		t.Fatal(result.Error)
	}
	for _, row := range result.Cases {
		if row.CandidateCount != 2 || row.FullTargetCoverageMatch || row.LegacyEndpointRuleHit || row.TargetCoverageRatio != nil || row.Outcome != "miss" {
			t.Fatalf("candidate union or best-target selection occurred: %+v", row)
		}
	}
}
func TestScoreVariantDeclarationsDoNotRelabelUnknownPositives(t *testing.T) {
	report, labels := scoreFixture(t)
	report.Prefix["groups"] = []groupWitness{}
	report.Status = "completed-abstention"
	labels.Cases[0].Variant.Key, labels.Cases[0].Variant.Support = "variant:a", "partial-target"
	result := scoreValues(t, report, labels)
	if !result.Completed || result.SameFullTargetVariantDeclared {
		t.Fatalf("partial support was promoted: %+v", result)
	}
	for _, row := range result.Cases {
		if row.Outcome != "miss" || row.LabelClass != "positive" {
			t.Fatal("variant metadata changed positive labels")
		}
	}
	for i := range labels.Cases {
		labels.Cases[i].Variant.Key, labels.Cases[i].Variant.Support = "variant:a", "full-target"
	}
	if result := scoreValues(t, report, labels); !result.SameFullTargetVariantDeclared || result.IndependentHeldout || result.ProductionResult {
		t.Fatal("variant declaration changed evaluation authority")
	}
}
func TestScoreCLIIsReadOnlyAndBindsExactInputBytes(t *testing.T) {
	report, labels := scoreFixture(t)
	rp, rh, lp, lh := writeScoreInputs(t, report, labels)
	output := filepath.Join(filepath.Dir(rp), "score.json")
	args := []string{"-mode", "score", "-report", rp, "-report-sha256", rh, "-labels", lp, "-labels-sha256", lh, "-output", output}
	never := func(map[string]source, *budget) (map[string]any, error) {
		t.Fatal("score invoked matcher")
		return nil, nil
	}
	var stderr bytes.Buffer
	if code := runCLI(context.Background(), args, &stderr, never); code != 0 {
		t.Fatalf("score mode failed: %d %s", code, stderr.String())
	}
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var result sourceTargetScore
	if err := json.Unmarshal(before, &result); err != nil {
		t.Fatal(err)
	}
	if result.ReportSHA256 != rh || result.LabelsSHA256 != lh || result.MatcherExecutedByScoring || result.EvaluatedImplementation.SHA256 != report.Implementation.SHA256 || result.Cases[0].Outcome != "full_target_covered_with_legacy_bounds" {
		t.Fatalf("score lost identity: %+v", result)
	}
	if runCLI(context.Background(), args, &stderr, never) != 1 {
		t.Fatal("existing score was overwritten")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("score evidence changed")
	}
	if err := os.WriteFile(lp, append([]byte(" "), mustJSON(t, labels)...), 0600); err != nil {
		t.Fatal(err)
	}
	if result := scoreFrozenReport(context.Background(), rp, rh, lp, lh); result.Completed || !strings.Contains(result.Error, "SHA256 mismatch") {
		t.Fatal("exact label byte drift accepted")
	}
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestScoreJSONAliasesAndCanceledReadsFailClosed(t *testing.T) {
	report, labels := scoreFixture(t)
	data := mustJSON(t, labels)
	data = bytes.Replace(data, []byte(`"sourceOnlyReview":true`), []byte(`"sourceOnlyReview":false,"ſourceOnlyReview":true`), 1)
	if _, err := parseScoreLabels(data); err == nil {
		t.Fatal("Unicode duplicate label contract accepted")
	}
	rp, rh, lp, lh := writeScoreInputs(t, report, labels)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := scoreFrozenReport(ctx, rp, rh, lp, lh)
	if result.Completed || result.ScoreAvailable || len(result.Cases) != 0 {
		t.Fatal("canceled scoring reported outcomes")
	}
}

func TestScoreRawDecimalOverlapsRemainExact(t *testing.T) {
	_, labels := scoreFixture(t)
	label := labels.Cases[0]
	label.ProtectedRanges = []scoreProtected{{Kind: "protected-title", Seconds: []scoreDecimal{"32", "40"}}}
	for _, end := range []string{"32.000000000000000000000000000001", "32.00000100000000000000000000001"} {
		candidate := scoreOneCandidate(0, []scoreDecimal{"20", scoreDecimal(end)}, label)
		if len(candidate.ProtectedOverlaps) != 1 {
			t.Fatal("positive raw overlap disappeared")
		}
		expected := new(big.Rat).Sub(scoreDecimal(end).rat(), big.NewRat(32, 1))
		actual, _ := new(big.Rat).SetString(candidate.ProtectedOverlaps[0].RawOverlapSeconds.String())
		if actual.Cmp(expected) != 0 {
			t.Fatal("raw overlap was rounded")
		}
	}
}

func TestScoreProtectedMissTakesPriorityOverEndpointCompatibility(t *testing.T) {
	report, labels := scoreFixture(t)
	for i := range labels.Cases {
		labels.Cases[i].ProtectedRanges = []scoreProtected{{Kind: "frozen-protected-title", Seconds: []scoreDecimal{"31.9", "40"}}}
	}
	result := scoreValues(t, report, labels)
	if !result.Completed {
		t.Fatal(result.Error)
	}
	for _, row := range result.Cases {
		if row.Outcome != "miss" || row.MissReason != "protected_overlap" || row.FullTargetCoverageMatch || row.LegacyEndpointRuleHit || !row.Candidates[0].StrictFullTargetCovered {
			t.Fatalf("protection was hidden by target coverage: %+v", row)
		}
	}
}
