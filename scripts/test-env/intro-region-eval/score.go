package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/bits"
	"regexp"
	"sort"
	"strings"
)

const scoreProtocol = "intro-region-source-target-score-v1"
const maxScoreLabelsBytes = 1 << 20
const maxScoreCases = 3
const maxProtectedRanges = 256

// Arithmetic uses the exact decimal JSON values. In particular, binary float
// rounding cannot turn a one-microsecond serialization touch into a violation.
type scoreDecimal string

var scoreDecimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]{1,2})?$`)

func (d *scoreDecimal) UnmarshalJSON(data []byte) error {
	s := string(bytes.TrimSpace(data))
	if len(s) > 128 || !scoreDecimalPattern.MatchString(s) {
		return errors.New("seconds must be a bounded nonnegative decimal JSON number")
	}
	if _, ok := new(big.Rat).SetString(s); !ok {
		return errors.New("invalid decimal seconds")
	}
	*d = scoreDecimal(s)
	return nil
}

func (d scoreDecimal) MarshalJSON() ([]byte, error) { return []byte(d), nil }
func (d scoreDecimal) rat() *big.Rat                { r, _ := new(big.Rat).SetString(string(d)); return r }
func numberRat(r *big.Rat) json.Number {
	// Differences between decimal endpoints have a finite exact decimal form.
	// Only repeating ratios (coverage) need the documented 18-place rendering.
	d := new(big.Int).Set(r.Denom())
	twos, fives := 0, 0
	for d.Bit(0) == 0 {
		d.Rsh(d, 1)
		twos++
	}
	five, remainder := big.NewInt(5), new(big.Int)
	for {
		remainder.Mod(d, five)
		if remainder.Sign() != 0 {
			break
		}
		d.Quo(d, five)
		fives++
	}
	places := 18
	if d.Cmp(big.NewInt(1)) == 0 {
		places = max(twos, fives)
	}
	s := r.FloatString(places)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "" || s == "-0" {
		s = "0"
	}
	return json.Number(s)
}
func maxRat(a, b *big.Rat) *big.Rat {
	if a.Cmp(b) >= 0 {
		return new(big.Rat).Set(a)
	}
	return new(big.Rat).Set(b)
}
func minRat(a, b *big.Rat) *big.Rat {
	if a.Cmp(b) <= 0 {
		return new(big.Rat).Set(a)
	}
	return new(big.Rat).Set(b)
}
func positiveRat(r *big.Rat) *big.Rat    { return maxRat(new(big.Rat), r) }
func subtractRat(a, b *big.Rat) *big.Rat { return new(big.Rat).Sub(a, b) }

type scoreVariant struct {
	Key            string `json:"key"`
	Support        string `json:"support"`
	EvidenceSHA256 string `json:"evidenceSha256"`
}
type scoreProtected struct {
	Kind    string         `json:"kind"`
	Seconds []scoreDecimal `json:"seconds"`
}
type scoreLabel struct {
	ID                        string           `json:"id"`
	EpisodeKey                string           `json:"episodeKey"`
	SourceSHA256              string           `json:"sourceSha256"`
	LabelClass                string           `json:"labelClass"`
	TargetSeconds             []scoreDecimal   `json:"targetSeconds"`
	ProtectedRanges           []scoreProtected `json:"protectedRanges"`
	SourceLabelEvidenceSHA256 string           `json:"sourceLabelEvidenceSha256"`
	Variant                   scoreVariant     `json:"variant"`
	Notes                     string           `json:"notes"`
}
type scoreLabels struct {
	SchemaVersion       int          `json:"schemaVersion"`
	SourceOnlyReview    bool         `json:"sourceOnlyReview"`
	DetectorOutputsUsed bool         `json:"detectorOutputsUsed"`
	ReviewerKind        string       `json:"reviewerKind"`
	AudioReviewed       bool         `json:"audioReviewed"`
	IndependentHeldout  bool         `json:"independentHeldout"`
	Cases               []scoreLabel `json:"cases"`
}

func strictScoreJSON(data []byte, value any) error {
	if err := checkJSON(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(value)
}
func scoreInterval(value []scoreDecimal) error {
	if len(value) != 2 || value[0].rat().Cmp(value[1].rat()) >= 0 {
		return errors.New("interval requires exactly two increasing nonnegative seconds")
	}
	return nil
}
func rawScoreArray(data []byte, key string) ([]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(object[key], &items); err != nil {
		return nil, err
	}
	if items == nil {
		return nil, fmt.Errorf("%s must be an explicit array", key)
	}
	return items, nil
}
func parseScoreLabels(data []byte) (scoreLabels, error) {
	var labels scoreLabels
	if err := strictScoreJSON(data, &labels); err != nil {
		return labels, err
	}
	if err := requireFields(data, "schemaVersion", "sourceOnlyReview", "detectorOutputsUsed", "reviewerKind", "audioReviewed", "independentHeldout", "cases"); err != nil {
		return labels, err
	}
	if labels.SchemaVersion != 1 || !labels.SourceOnlyReview || labels.DetectorOutputsUsed || labels.ReviewerKind != "assistant-source-only-visual" || labels.AudioReviewed || labels.IndependentHeldout || len(labels.Cases) != maxScoreCases {
		return labels, errors.New("source-only development label contract is required for exactly three cases")
	}
	rawCases, err := rawScoreArray(data, "cases")
	if err != nil {
		return labels, err
	}
	ids, episodes, hashes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, label := range labels.Cases {
		if err := requireFields(rawCases[i], "id", "episodeKey", "sourceSha256", "labelClass", "protectedRanges", "sourceLabelEvidenceSha256", "variant.key", "variant.support", "variant.evidenceSha256", "notes"); err != nil {
			return labels, err
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(rawCases[i], &raw)
		if _, ok := raw["targetSeconds"]; !ok {
			return labels, errors.New("targetSeconds is required, including explicit null for negatives")
		}
		if !sourceIDPattern.MatchString(label.ID) || ids[strings.ToLower(label.ID)] || !episodeKeyPattern.MatchString(label.EpisodeKey) || episodes[label.EpisodeKey] || !validHash(label.SourceSHA256) || hashes[label.SourceSHA256] {
			return labels, errors.New("label cases require distinct source IDs, episode keys and media hashes")
		}
		ids[strings.ToLower(label.ID)], episodes[label.EpisodeKey], hashes[label.SourceSHA256] = true, true, true
		if !validHash(label.SourceLabelEvidenceSHA256) || !validHash(label.Variant.EvidenceSHA256) || len(label.Notes) == 0 || len(label.Notes) > 4096 {
			return labels, errors.New("bounded label provenance and notes are required")
		}
		switch label.LabelClass {
		case "positive":
			if err := scoreInterval(label.TargetSeconds); err != nil {
				return labels, err
			}
			if label.Variant.Support == "not-applicable" {
				return labels, errors.New("positive target cannot declare variant support not applicable")
			}
		case "negative":
			if !bytes.Equal(bytes.TrimSpace(raw["targetSeconds"]), []byte("null")) || label.Variant.Support != "not-applicable" {
				return labels, errors.New("negative requires null target and not-applicable variant support")
			}
		default:
			return labels, errors.New("unknown label class")
		}
		switch label.Variant.Support {
		case "full-target", "partial-target":
			if !episodeKeyPattern.MatchString(label.Variant.Key) {
				return labels, errors.New("reviewed variant requires a canonical variant key")
			}
		case "unknown", "not-applicable":
			if label.Variant.Key != "" {
				return labels, errors.New("unestablished variant must have an empty key")
			}
		default:
			return labels, errors.New("unknown variant support category")
		}
		if len(label.ProtectedRanges) > maxProtectedRanges {
			return labels, errors.New("protected range count exceeds bound")
		}
		ranges, err := rawScoreArray(rawCases[i], "protectedRanges")
		if err != nil {
			return labels, err
		}
		for j, zone := range label.ProtectedRanges {
			if err := requireFields(ranges[j], "kind", "seconds"); err != nil {
				return labels, err
			}
			if len(zone.Kind) == 0 || len(zone.Kind) > 128 {
				return labels, errors.New("protected range kind is required and bounded")
			}
			if err := scoreInterval(zone.Seconds); err != nil {
				return labels, err
			}
		}
	}
	return labels, nil
}

type scoreGroup struct {
	SourceIDs               []string          `json:"sourceIDs"`
	Geometries              []geometry        `json:"geometries"`
	Clocks                  []clockHypothesis `json:"clocks"`
	AnchorStart             scoreDecimal      `json:"anchorStart"`
	AnchorEnd               scoreDecimal      `json:"anchorEnd"`
	SupportMask             uint32            `json:"supportMask"`
	DynamicMask             uint32            `json:"dynamicMask"`
	MinimumCoveragePermille int               `json:"minimumCoveragePermille"`
	PassingWindows          int               `json:"passingWindows"`
	SourceBounds            [][]scoreDecimal  `json:"sourceBounds"`
	ObservedEdgeBounds      [][]scoreDecimal  `json:"observedEdgeBounds"`
	BoundaryPolicy          string            `json:"boundaryPolicy"`
	ComponentAnchorBounds   []scoreDecimal    `json:"componentAnchorBounds"`
}
type scoreReportInput struct {
	Report evaluationReport
	Groups []scoreGroup
}

func parseScoreReport(data []byte, labels scoreLabels) (scoreReportInput, error) {
	var input scoreReportInput
	if err := strictScoreJSON(data, &input.Report); err != nil {
		return input, err
	}
	if err := requireFields(data, "protocol", "schemaVersion", "mode", "resultKind", "productionResult", "independentHeldout", "completed", "status", "stage", "ambiguous", "inputPath", "implementation.sha256", "implementation.files", "implementation.hashEncoding", "admittedSources", "prefix.complete", "prefix.productionResult", "prefix.groups", "work", "limits", "startedUTC", "finishedUTC"); err != nil {
		return input, err
	}
	r := &input.Report
	if r.Protocol != reportProtocol || r.SchemaVersion != 1 || r.Mode != "full-prefix" || r.ResultKind != "research-candidates-with-observed-bounds" || r.ProductionResult || r.IndependentHeldout || r.Stage == "" || r.InputPath == "" || r.StartedUTC.IsZero() || r.FinishedUTC.Before(r.StartedUTC) {
		return input, errors.New("report is not a supported frozen research evaluation")
	}
	if !validHash(r.Implementation.SHA256) || len(r.Implementation.Files) < 8 || len(r.Implementation.Files) > 64 || r.Implementation.HashEncoding != implementationIdentity().HashEncoding {
		return input, errors.New("report implementation identity is incomplete")
	}
	for name, hash := range r.Implementation.Files {
		if !sourceIDPattern.MatchString(name) || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || !validHash(hash) {
			return input, errors.New("invalid reported implementation file identity")
		}
	}
	for _, name := range []string{"engine_core.go", "input.go", "local_prefix_cache3.go", "main.go", "observed_boundary.go", "posterior_cache.go", "secureio_linux.go", "secureio_unsupported.go"} {
		if _, ok := r.Implementation.Files[name]; !ok {
			return input, errors.New("missing reported implementation file identity")
		}
	}
	if r.InputSHA256 != "" && !validHash(r.InputSHA256) {
		return input, errors.New("invalid reported input identity")
	}
	if err := requireFields(data, "work.patchComparisons", "work.clockLookups", "work.renderPixels", "work.retainedHypotheses"); err != nil {
		return input, err
	}
	if r.Work == nil || r.Work.PatchComparisons < 0 || r.Work.ClockLookups < 0 || r.Work.RenderPixels < 0 || r.Work.RetainedHypotheses < 0 {
		return input, errors.New("invalid reported work counters")
	}
	if r.DerivedFrom != nil && (r.DerivedFrom.ManifestPath == "" || !validHash(r.DerivedFrom.SHA256)) {
		return input, errors.New("invalid reported upstream input identity")
	}
	var raw struct {
		Admitted []json.RawMessage `json:"admittedSources"`
		Prefix   json.RawMessage   `json:"prefix"`
	}
	_ = json.Unmarshal(data, &raw)
	if len(r.AdmittedSources) > 3 {
		return input, errors.New("report admission exceeds the fixed cohort")
	}
	byID := map[string]scoreLabel{}
	for _, label := range labels.Cases {
		byID[label.ID] = label
	}
	admitted := map[string]admittedSource{}
	episodes, hashes := map[string]bool{}, map[string]bool{}
	for i, source := range r.AdmittedSources {
		if err := requireFields(raw.Admitted[i], "source.id", "source.sourceSha256", "source.graySha256", "source.ptsSha256", "source.frames", "source.firstPTS", "source.lastPTS", "episodeKey", "manifestPath", "manifestSha256", "gzipPath", "gzipSha256", "gzipBytes", "orchestratorSha256", "driverSha256", "individualFrameHashesVerified"); err != nil {
			return input, err
		}
		label, ok := byID[source.Source.ID]
		if !ok || admitted[source.Source.ID].EpisodeKey != "" || episodes[source.EpisodeKey] || hashes[source.Source.SourceSHA256] || source.EpisodeKey != label.EpisodeKey || source.Source.SourceSHA256 != label.SourceSHA256 {
			return input, errors.New("report source identities do not match the frozen label population")
		}
		if source.Source.Frames != expectedFrames || source.Source.FirstPTS < 0 || source.Source.FirstPTS >= 0.1 || source.Source.LastPTS < 119.9 || source.Source.LastPTS >= 120 || source.Source.PTSSHA256 != source.ManifestSHA256 || !source.IndividualFrameHashesVerified || source.ManifestPath == "" || source.GzipPath == "" || source.GzipBytes <= 0 || source.GzipBytes > maxGzipBytes {
			return input, errors.New("incomplete reported source admission")
		}
		for _, hash := range []string{source.Source.GraySHA256, source.Source.PTSSHA256, source.ManifestSHA256, source.GzipSHA256, source.OrchestratorSHA256, source.DriverSHA256} {
			if !validHash(hash) {
				return input, errors.New("invalid reported extraction identity")
			}
		}
		admitted[source.Source.ID], episodes[source.EpisodeKey], hashes[source.Source.SourceSHA256] = source, true, true
	}
	var prefix struct {
		Complete         bool              `json:"complete"`
		ProductionResult bool              `json:"productionResult"`
		Groups           []json.RawMessage `json:"groups"`
	}
	if err := json.Unmarshal(raw.Prefix, &prefix); err != nil {
		return input, err
	}
	if prefix.Groups == nil || prefix.ProductionResult || len(prefix.Groups) > maxHypotheses {
		return input, errors.New("invalid research group container")
	}
	var prefixRaw map[string]json.RawMessage
	_ = json.Unmarshal(raw.Prefix, &prefixRaw)
	if _, present := prefixRaw["closure"]; r.Completed && (len(prefix.Groups) > 0 || r.Ambiguous) && !present {
		return input, errors.New("completed group or ambiguity requires closure evidence")
	}
	if closure, ok := prefixRaw["closure"]; ok {
		if err := requireFields(closure, "cohortBoundaryAmbiguous"); err != nil {
			return input, err
		}
		var summary struct {
			Ambiguous bool `json:"cohortBoundaryAmbiguous"`
		}
		if err := json.Unmarshal(closure, &summary); err != nil {
			return input, err
		}
		if r.Completed && summary.Ambiguous != r.Ambiguous {
			return input, errors.New("closure ambiguity disagrees with the result")
		}
	}
	if !r.Completed {
		if prefix.Complete || len(prefix.Groups) != 0 || r.Ambiguous || r.Error == "" {
			return input, errors.New("incomplete evaluation contains conflicting result claims")
		}
		switch r.Status {
		case "canceled", "deadline-exceeded", "input-rejected", "budget-exhausted", "evaluation-failed":
		default:
			return input, errors.New("invalid incomplete evaluation status")
		}
		return input, nil
	}
	if !prefix.Complete || r.Error != "" || len(admitted) != 3 || !validHash(r.InputSHA256) {
		return input, errors.New("completed evaluation lacks complete admitted evidence")
	}
	for name, expected := range map[string]int{"patchComparisons": maxPatchComparisons, "clockLookups": maxClockLookups, "renderPixels": maxRenderPixels, "retainedHypotheses": maxHypotheses, "inputManifestBytes": maxInputManifestBytes, "extractionManifestBytes": maxExtractionBytes, "gzipBytesPerSource": maxGzipBytes, "rawBytesPerSource": expectedFrames * frameBytes} {
		value, ok := r.Limits[name].(float64)
		if !ok || value != float64(expected) {
			return input, errors.New("completed report has missing or changed frozen limits")
		}
	}
	deadline, ok := r.Limits["deadlineSeconds"].(float64)
	if !ok || deadline <= 0 || deadline > maximumDeadline.Seconds() {
		return input, errors.New("invalid reported operational deadline")
	}
	if r.Work.PatchComparisons <= 0 || r.Work.PatchComparisons > maxPatchComparisons || r.Work.ClockLookups > maxClockLookups || r.Work.RenderPixels <= 0 || r.Work.RenderPixels > maxRenderPixels || r.Work.RetainedHypotheses > maxHypotheses {
		return input, errors.New("completed evaluation has impossible work counters")
	}
	if len(prefix.Groups) > 0 {
		if r.Status != "completed-with-groups" || r.Ambiguous {
			return input, errors.New("group output conflicts with evaluation status")
		}
	} else if r.Ambiguous {
		if r.Status != "completed-ambiguous-abstention" {
			return input, errors.New("ambiguous result status mismatch")
		}
	} else if r.Status != "completed-abstention" {
		return input, errors.New("empty result status mismatch")
	}
	for _, rawGroup := range prefix.Groups {
		var group scoreGroup
		if err := strictScoreJSON(rawGroup, &group); err != nil {
			return input, err
		}
		if err := requireFields(rawGroup, "sourceIDs", "geometries", "clocks", "anchorStart", "anchorEnd", "supportMask", "dynamicMask", "minimumCoveragePermille", "passingWindows", "sourceBounds", "observedEdgeBounds", "boundaryPolicy", "componentAnchorBounds"); err != nil {
			return input, err
		}
		if len(group.SourceIDs) != 3 || len(group.SourceBounds) != 3 || len(group.ObservedEdgeBounds) != 3 || len(group.Geometries) != 3 || len(group.Clocks) != 2 || group.BoundaryPolicy != "observed-common-component-v2" || group.PassingWindows <= 0 || group.SupportMask >= 1<<patchCount || !spatial(group.SupportMask) || bits.OnesCount32(group.DynamicMask) < 4 || group.DynamicMask & ^group.SupportMask != 0 || group.MinimumCoveragePermille < 850 || group.MinimumCoveragePermille > 1000 || r.Work.RetainedHypotheses == 0 || r.Work.ClockLookups == 0 {
			return input, errors.New("incomplete or unsupported group witness")
		}
		if err := scoreInterval(group.ComponentAnchorBounds); err != nil {
			return input, err
		}
		geometries, _ := rawScoreArray(rawGroup, "geometries")
		clocks, _ := rawScoreArray(rawGroup, "clocks")
		seen := map[string]bool{}
		for i, id := range group.SourceIDs {
			source, ok := admitted[id]
			if !ok || seen[id] {
				return input, errors.New("group does not contain each admitted source exactly once")
			}
			seen[id] = true
			if err := scoreInterval(group.SourceBounds[i]); err != nil {
				return input, err
			}
			if err := scoreInterval(group.ObservedEdgeBounds[i]); err != nil {
				return input, err
			}
			start, _ := group.SourceBounds[i][0].rat().Float64()
			end, _ := group.SourceBounds[i][1].rat().Float64()
			edgeStart, _ := group.ObservedEdgeBounds[i][0].rat().Float64()
			edgeEnd, _ := group.ObservedEdgeBounds[i][1].rat().Float64()
			if edgeStart < source.Source.FirstPTS-1e-9 || edgeEnd > source.Source.LastPTS+1e-9 {
				return input, errors.New("observed edge bounds exceed the admitted source")
			}
			if start < source.Source.FirstPTS-1e-9 || end > source.Source.LastPTS+1e-9 || end-start < 8-1e-9 || end-start > 90+1e-9 || group.SourceBounds[i][0].rat().Cmp(group.ObservedEdgeBounds[i][0].rat()) < 0 || group.SourceBounds[i][1].rat().Cmp(group.ObservedEdgeBounds[i][1].rat()) > 0 {
				return input, errors.New("group interval exceeds its admitted observed source envelope or duration limits")
			}
			if err := requireFields(geometries[i], "scaleX", "scaleY", "shiftX", "shiftY"); err != nil {
				return input, err
			}
			g := group.Geometries[i]
			validScale := func(v float64) bool { return v == .9 || v == 1 || v == 1.1 }
			validShift := func(v int) bool { return v == -3 || v == 0 || v == 3 }
			if !validScale(g.ScaleX) || !validScale(g.ScaleY) || !validShift(g.ShiftX) || !validShift(g.ShiftY) || i == 0 && g != neutral() {
				return input, errors.New("invalid frozen group geometry")
			}
		}
		if group.AnchorStart.rat().Cmp(group.SourceBounds[0][0].rat()) != 0 || group.AnchorEnd.rat().Cmp(group.SourceBounds[0][1].rat()) != 0 || group.AnchorStart.rat().Cmp(group.ComponentAnchorBounds[0].rat()) < 0 || group.AnchorEnd.rat().Cmp(group.ComponentAnchorBounds[1].rat()) > 0 {
			return input, errors.New("anchor bounds disagree with their source or component")
		}
		for i, clock := range group.Clocks {
			if err := requireFields(clocks[i], "scale", "offset", "prefixSupportMask", "prefixDynamicMask"); err != nil {
				return input, err
			}
			if (clock.Scale != .98 && clock.Scale != 1 && clock.Scale != 1.02) || math.Abs(clock.Offset) > 120 || math.Abs(clock.Offset*20-math.Round(clock.Offset*20)) > 1e-8 {
				return input, errors.New("invalid group clock")
			}
			if clock.PrefixSupportMask >= 1<<patchCount || !spatial(clock.PrefixSupportMask) || bits.OnesCount32(clock.PrefixDynamicMask) < 4 || clock.PrefixDynamicMask & ^clock.PrefixSupportMask != 0 {
				return input, errors.New("invalid frozen clock support")
			}
			for edge := 0; edge < 2; edge++ {
				anchor, _ := group.SourceBounds[0][edge].rat().Float64()
				source, _ := group.SourceBounds[i+1][edge].rat().Float64()
				if math.Abs(source-(anchor*clock.Scale+clock.Offset)) > 1e-9 {
					return input, errors.New("source interval disagrees with its fixed clock")
				}
			}
		}
		input.Groups = append(input.Groups, group)
	}
	return input, nil
}

type scoredOverlap struct {
	Kind                        string         `json:"kind"`
	ProtectedSeconds            []scoreDecimal `json:"protectedSeconds"`
	RawOverlapSeconds           json.Number    `json:"rawOverlapSeconds"`
	ExceedsSerializationEpsilon bool           `json:"exceedsSerializationEpsilon"`
}
type scoredCandidate struct {
	GroupIndex              int             `json:"groupIndex"`
	Seconds                 []scoreDecimal  `json:"seconds"`
	TargetCoverageRatio     *json.Number    `json:"targetCoverageRatio"`
	TargetOverlapSeconds    *json.Number    `json:"targetOverlapSeconds"`
	MissingHeadSeconds      *json.Number    `json:"missingHeadSeconds"`
	MissingTailSeconds      *json.Number    `json:"missingTailSeconds"`
	OutsideTargetSeconds    *json.Number    `json:"outsideTargetSeconds"`
	EndpointErrorsSeconds   []json.Number   `json:"endpointErrorsSeconds"`
	LegacyEndpointRuleMatch bool            `json:"legacyEndpointRuleMatch"`
	StrictFullTargetCovered bool            `json:"strictFullTargetCovered"`
	StrictContainedInTarget bool            `json:"strictContainedInTarget"`
	ProtectedOverlaps       []scoredOverlap `json:"protectedOverlaps"`
	ProtectedViolation      bool            `json:"protectedViolation"`
}
type scoredCase struct {
	ID                        string            `json:"id"`
	EpisodeKey                string            `json:"episodeKey"`
	SourceSHA256              string            `json:"sourceSha256"`
	LabelClass                string            `json:"labelClass"`
	TargetSeconds             []scoreDecimal    `json:"targetSeconds"`
	Variant                   scoreVariant      `json:"variant"`
	SourceLabelEvidenceSHA256 string            `json:"sourceLabelEvidenceSha256"`
	Admitted                  bool              `json:"admitted"`
	Outcome                   string            `json:"outcome"`
	MissReason                string            `json:"missReason,omitempty"`
	CandidateCount            int               `json:"candidateCount"`
	TargetCoverageRatio       *json.Number      `json:"targetCoverageRatio"`
	FullTargetCoverageMatch   bool              `json:"fullTargetCoverageMatch"`
	LegacyEndpointRuleHit     bool              `json:"legacyEndpointRuleHit"`
	Candidates                []scoredCandidate `json:"candidates"`
}
type sourceTargetScore struct {
	Protocol                      string                 `json:"protocol"`
	SchemaVersion                 int                    `json:"schemaVersion"`
	ProductionResult              bool                   `json:"productionResult"`
	IndependentHeldout            bool                   `json:"independentHeldout"`
	MatcherExecutedByScoring      bool                   `json:"matcherExecutedByScoring"`
	Completed                     bool                   `json:"completed"`
	ScoreAvailable                bool                   `json:"scoreAvailable"`
	Status                        string                 `json:"status"`
	Error                         string                 `json:"error,omitempty"`
	ReportSHA256                  string                 `json:"reportSha256,omitempty"`
	LabelsSHA256                  string                 `json:"labelsSha256,omitempty"`
	EvaluationCompleted           bool                   `json:"evaluationCompleted"`
	EvaluationStatus              string                 `json:"evaluationStatus,omitempty"`
	EvaluationInputSHA256         string                 `json:"evaluationInputSha256,omitempty"`
	EvaluatedImplementation       *implementationBinding `json:"evaluatedImplementation,omitempty"`
	ScorerImplementation          implementationBinding  `json:"scorerImplementation"`
	SameFullTargetVariantDeclared bool                   `json:"sameFullTargetVariantDeclared"`
	Policy                        map[string]any         `json:"policy"`
	Limitations                   []string               `json:"limitations"`
	Cases                         []scoredCase           `json:"cases"`
}

func scoreOneCandidate(group int, bounds []scoreDecimal, label scoreLabel) scoredCandidate {
	row := scoredCandidate{GroupIndex: group, Seconds: bounds, ProtectedOverlaps: []scoredOverlap{}}
	start, end := bounds[0].rat(), bounds[1].rat()
	epsilon := new(big.Rat).SetFrac64(1, 1_000_000)
	for _, zone := range label.ProtectedRanges {
		overlap := positiveRat(subtractRat(minRat(end, zone.Seconds[1].rat()), maxRat(start, zone.Seconds[0].rat())))
		if overlap.Sign() > 0 {
			violation := overlap.Cmp(epsilon) > 0
			row.ProtectedOverlaps = append(row.ProtectedOverlaps, scoredOverlap{zone.Kind, zone.Seconds, numberRat(overlap), violation})
			row.ProtectedViolation = row.ProtectedViolation || violation
		}
	}
	if label.LabelClass == "negative" {
		return row
	}
	a, b := label.TargetSeconds[0].rat(), label.TargetSeconds[1].rat()
	duration := subtractRat(b, a)
	overlap := positiveRat(subtractRat(minRat(end, b), maxRat(start, a)))
	coverage := numberRat(new(big.Rat).Quo(overlap, duration))
	overlapNumber := numberRat(overlap)
	head := numberRat(minRat(duration, positiveRat(subtractRat(start, a))))
	tail := numberRat(minRat(duration, positiveRat(subtractRat(b, end))))
	outside := numberRat(subtractRat(subtractRat(end, start), overlap))
	row.TargetCoverageRatio, row.TargetOverlapSeconds, row.MissingHeadSeconds, row.MissingTailSeconds, row.OutsideTargetSeconds = &coverage, &overlapNumber, &head, &tail, &outside
	startError := new(big.Rat).Abs(subtractRat(start, a))
	endError := new(big.Rat).Abs(subtractRat(end, b))
	row.EndpointErrorsSeconds = []json.Number{numberRat(startError), numberRat(endError)}
	row.LegacyEndpointRuleMatch = startError.Cmp(big.NewRat(5, 1)) <= 0 && endError.Cmp(big.NewRat(5, 1)) <= 0 && !row.ProtectedViolation
	row.StrictFullTargetCovered = subtractRat(start, a).Cmp(epsilon) <= 0 && subtractRat(b, end).Cmp(epsilon) <= 0
	row.StrictContainedInTarget = subtractRat(a, start).Cmp(epsilon) <= 0 && subtractRat(end, b).Cmp(epsilon) <= 0
	return row
}
func scoreFrozenReport(ctx context.Context, reportPath, reportHash, labelsPath, labelsHash string) sourceTargetScore {
	r := sourceTargetScore{Protocol: scoreProtocol, SchemaVersion: 1, Status: "input-rejected", ScorerImplementation: implementationIdentity(), Cases: []scoredCase{}, Policy: map[string]any{"endpointToleranceSeconds": 5, "serializationEpsilonSeconds": 0.000001, "strictFullTargetRule": "Exactly one candidate covers the complete frozen target within serialization epsilon, passes the legacy endpoint rule and has no protected overlap violation; no candidate union.", "legacyEndpointRule": "Exactly one candidate has both endpoint errors at most five seconds and no protected overlap violation. This is distinct from complete target coverage.", "protectionRule": "Every positive raw overlap is retained; overlap above one microsecond violates protection regardless of endpoint tolerance.", "emptyAndBlockedRule": "Completed positive abstention is a miss. Incomplete evaluation blocks every case and contributes no negative success or target coverage."}, Limitations: []string{"Scoring reads supplied frozen report bytes; it does not rerun or authenticate the matcher or upstream source review.", "Episode, variant and source-only review facts are caller assertions bound by hashes, not facts inferred from filenames or similarity.", "Variant declarations do not change semantic targets, remove misses or prove complete pairwise correspondence.", "This is research-candidate scoring, not publication authority, independent held-out accuracy or proof of continuous semantic content."}}
	fail := func(err error) sourceTargetScore {
		r.Error = err.Error()
		if errors.Is(err, context.Canceled) {
			r.Status = "canceled"
		} else if errors.Is(err, context.DeadlineExceeded) {
			r.Status = "deadline-exceeded"
		}
		r.Completed = false
		r.ScoreAvailable = false
		r.Cases = []scoredCase{}
		return r
	}
	if !validHash(reportHash) || !validHash(labelsHash) {
		return fail(errors.New("valid frozen report and label hashes are required"))
	}
	data, err := readRegular(ctx, reportPath, maxReportBytes)
	if err != nil {
		return fail(err)
	}
	r.ReportSHA256 = digest(data)
	if r.ReportSHA256 != reportHash {
		return fail(errors.New("frozen report SHA256 mismatch"))
	}
	labelData, err := readRegular(ctx, labelsPath, maxScoreLabelsBytes)
	if err != nil {
		return fail(err)
	}
	r.LabelsSHA256 = digest(labelData)
	if r.LabelsSHA256 != labelsHash {
		return fail(errors.New("frozen label SHA256 mismatch"))
	}
	labels, err := parseScoreLabels(labelData)
	if err != nil {
		return fail(err)
	}
	input, err := parseScoreReport(data, labels)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	r.EvaluationCompleted, r.EvaluationStatus, r.EvaluationInputSHA256, r.EvaluatedImplementation = input.Report.Completed, input.Report.Status, input.Report.InputSHA256, &input.Report.Implementation
	admitted := map[string]bool{}
	for _, source := range input.Report.AdmittedSources {
		admitted[source.Source.ID] = true
	}
	sameVariant := true
	variantKey := labels.Cases[0].Variant.Key
	for _, label := range labels.Cases {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		sameVariant = sameVariant && label.LabelClass == "positive" && label.Variant.Support == "full-target" && label.Variant.Key == variantKey
		row := scoredCase{ID: label.ID, EpisodeKey: label.EpisodeKey, SourceSHA256: label.SourceSHA256, LabelClass: label.LabelClass, TargetSeconds: label.TargetSeconds, Variant: label.Variant, SourceLabelEvidenceSHA256: label.SourceLabelEvidenceSHA256, Admitted: admitted[label.ID], Outcome: "blocked", Candidates: []scoredCandidate{}}
		if input.Report.Completed {
			for gi, group := range input.Groups {
				if err := ctx.Err(); err != nil {
					return fail(err)
				}
				for si, id := range group.SourceIDs {
					if id == label.ID {
						row.Candidates = append(row.Candidates, scoreOneCandidate(gi, group.SourceBounds[si], label))
					}
				}
			}
			row.CandidateCount = len(row.Candidates)
			if label.LabelClass == "negative" {
				row.Outcome = "correct_abstention"
				if row.CandidateCount > 0 {
					row.Outcome = "false_positive"
				}
			} else {
				row.Outcome = "miss"
				row.MissReason = "no_candidate"
				if row.CandidateCount == 0 {
					zero := json.Number("0")
					row.TargetCoverageRatio = &zero
				}
				if row.CandidateCount == 1 {
					candidate := row.Candidates[0]
					row.TargetCoverageRatio = candidate.TargetCoverageRatio
					row.FullTargetCoverageMatch = candidate.StrictFullTargetCovered && candidate.LegacyEndpointRuleMatch
					row.LegacyEndpointRuleHit = candidate.LegacyEndpointRuleMatch
					if row.FullTargetCoverageMatch {
						row.Outcome = "full_target_covered_with_legacy_bounds"
					}
				}
				if row.CandidateCount > 1 {
					row.MissReason = "multiple_candidates"
				} else if row.CandidateCount == 1 {
					if !row.Candidates[0].StrictFullTargetCovered {
						row.MissReason = "incomplete_target"
					} else if !row.Candidates[0].LegacyEndpointRuleMatch {
						row.MissReason = "boundary_error"
					} else {
						row.MissReason = ""
					}
				}
				for _, candidate := range row.Candidates {
					if candidate.ProtectedViolation {
						row.MissReason = "protected_overlap"
					}
				}
			}
		}
		r.Cases = append(r.Cases, row)
	}
	sort.Slice(r.Cases, func(i, j int) bool { return r.Cases[i].ID < r.Cases[j].ID })
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	r.SameFullTargetVariantDeclared = sameVariant
	r.Completed, r.ScoreAvailable = true, input.Report.Completed
	r.Status = "scored"
	if !input.Report.Completed {
		r.Status = "evaluation-blocked"
	}
	return r
}
func runScoreCLI(ctx context.Context, reportPath, reportHash, labelsPath, labelsHash, output string, stderr io.Writer) int {
	paths := []*string{&reportPath, &labelsPath, &output}
	for _, path := range paths {
		canonical, err := canonicalPath(".", *path)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		*path = canonical
	}
	writer, err := newReportWriter(output)
	if err != nil {
		fmt.Fprintln(stderr, "cannot reserve a new score report:", err)
		return 1
	}
	defer writer.Close()
	report := scoreFrozenReport(ctx, reportPath, reportHash, labelsPath, labelsHash)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil || len(data) >= maxReportBytes {
		fmt.Fprintln(stderr, "cannot encode bounded score report")
		return 1
	}
	if err := writer.Publish(append(data, '\n')); err != nil {
		fmt.Fprintln(stderr, "cannot publish score report:", err)
		return 1
	}
	fmt.Fprintf(stderr, "%s: score report=%s\n", report.Status, output)
	if !report.Completed || !report.ScoreAvailable {
		return 1
	}
	return 0
}
