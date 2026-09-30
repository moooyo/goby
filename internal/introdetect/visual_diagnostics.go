package introdetect

import "sort"

// MaxVisualDiagnosticTraceEntries bounds the visual trace independently of the
// audio trace. Aggregate counts remain complete after trace truncation.
const MaxVisualDiagnosticTraceEntries = 128

// VisualBranchDiagnostics describes the actual branch admission. EligibleSources
// applies the calibrated readiness gate in that branch; coarse eligibility is
// its existing independent-source mask. HasQuorum requires one shared profile.
type VisualBranchDiagnostics struct {
	Attempted          bool
	Completed          bool
	IndependentSources int
	EligibleSources    int
	HasQuorum          bool
	Groups             int
	SearchLimited      bool
}

// VisualDiagnosticCounts counts completed stages, not semantic accuracy or
// unique measurements. PairLookups includes reused complete scans; PairCacheHits
// identifies that subset. No cached pair is scanned again for diagnostics.
type VisualDiagnosticCounts struct {
	CalibrationChecks         int
	CalibrationRejected       int
	PairLookups               int
	PairCacheHits             int
	PairsWithoutHypotheses    int
	AnchorsWithoutPairQuorum  int
	GroupSearches             int
	GroupSearchesWithoutGroup int
	GuardChecks               int
	GuardRejected             int
	DiscoveryGroups           int
	LimitedSourceRejected     int
	PublicationRejected       int
	OriginalAmbiguousSources  int
	RecoveryGroups            int
	RecoveredSources          int
	RetainedAmbiguousSources  int
	PublishedGroups           int
}

// VisualDiagnosticEntry records one completed production stage. A group-search
// rejection means that the existing full-clique search returned no witness;
// it does not infer a lower-level reason or nominate another candidate.
type VisualDiagnosticEntry struct {
	Stage           string
	Policy          string
	AnchorSourceKey string `json:",omitempty"`
	LeftSourceKey   string `json:",omitempty"`
	RightSourceKey  string `json:",omitempty"`
	Outcome         string
	Count           int
	CacheHit        bool `json:",omitempty"`
}

// VisualFallbackDiagnostics observes only the fallback invoked by Analyze.
// Considered means the audio-group decision was reached; Attempted means actual
// visual discovery began. Completed includes successful early abstentions.
// Branch completion and partial counts never authorize publication after an
// overall error. A separate DiscoverVisualSequences call cannot enter this trace.
type VisualFallbackDiagnostics struct {
	Considered     bool
	Attempted      bool
	Completed      bool
	SkipReason     string `json:",omitempty"`
	Calibrated     VisualBranchDiagnostics
	Coarse         VisualBranchDiagnostics
	Counts         VisualDiagnosticCounts
	TraceLimit     int
	TraceEntries   int
	TraceTruncated bool
	Entries        []VisualDiagnosticEntry
}

func (d *diagnosticsCollector) visualEntry(entry VisualDiagnosticEntry) {
	if d == nil {
		return
	}
	v := &d.value.Visual
	if v.TraceEntries >= v.TraceLimit {
		v.TraceTruncated = true
		return
	}
	v.Entries = append(v.Entries, entry)
	v.TraceEntries++
}

func (d *diagnosticsCollector) visualConsidered() {
	if d != nil {
		d.value.Visual.Considered = true
	}
}

func (d *diagnosticsCollector) visualSkipped(reason string) {
	if d != nil {
		d.value.Visual.Considered = true
		d.value.Visual.SkipReason = reason
		d.value.Visual.Completed = true
	}
}

func (d *diagnosticsCollector) visualStarted() {
	if d != nil {
		d.value.Visual.Attempted = true
	}
}

func (d *diagnosticsCollector) visualBranch(policy string) *VisualBranchDiagnostics {
	if d == nil {
		return nil
	}
	if policy == VisualMeasurementCalibrated {
		return &d.value.Visual.Calibrated
	}
	return &d.value.Visual.Coarse
}

func (d *diagnosticsCollector) visualBranchStarted(policy string) {
	if branch := d.visualBranch(policy); branch != nil {
		branch.Attempted = true
	}
}

func (d *diagnosticsCollector) visualQuorum(policy string, independent, eligible []bool, quorum bool) {
	if branch := d.visualBranch(policy); branch != nil {
		for i, value := range independent {
			if value {
				branch.IndependentSources++
			}
			if eligible[i] {
				branch.EligibleSources++
			}
		}
		branch.HasQuorum = quorum
	}
}

func (d *diagnosticsCollector) visualBranchCompleted(policy string, result VisualSequenceResult) {
	if branch := d.visualBranch(policy); branch != nil {
		branch.Completed = true
		branch.Groups = len(result.Groups)
		branch.SearchLimited = result.SearchLimited
	}
}

func (d *diagnosticsCollector) visualCalibration(anchor, source string, accepted bool) {
	if d == nil {
		return
	}
	d.value.Visual.Counts.CalibrationChecks++
	outcome := "accepted"
	if !accepted {
		d.value.Visual.Counts.CalibrationRejected++
		outcome = "no_calibration"
	}
	d.visualEntry(VisualDiagnosticEntry{Stage: "calibration", Policy: VisualMeasurementCalibrated, AnchorSourceKey: anchor, RightSourceKey: source, Outcome: outcome})
}

func (d *diagnosticsCollector) visualPair(policy, anchor, left, right string, count int, cached bool) {
	if d == nil {
		return
	}
	c := &d.value.Visual.Counts
	c.PairLookups++
	if cached {
		c.PairCacheHits++
	}
	outcome := "accepted"
	if count == 0 {
		c.PairsWithoutHypotheses++
		outcome = "no_hypothesis"
	}
	d.visualEntry(VisualDiagnosticEntry{Stage: "pair", Policy: policy, AnchorSourceKey: anchor, LeftSourceKey: left, RightSourceKey: right, Outcome: outcome, Count: count, CacheHit: cached})
}

func (d *diagnosticsCollector) visualGroupSearch(policy, anchor string, count int) {
	if d == nil {
		return
	}
	d.value.Visual.Counts.GroupSearches++
	outcome := "accepted"
	if count == 0 {
		d.value.Visual.Counts.GroupSearchesWithoutGroup++
		outcome = "no_full_group"
	}
	d.visualEntry(VisualDiagnosticEntry{Stage: "group", Policy: policy, AnchorSourceKey: anchor, Outcome: outcome, Count: count})
}

func (d *diagnosticsCollector) visualAnchorWithoutQuorum(anchor string, members int) {
	if d == nil {
		return
	}
	d.value.Visual.Counts.AnchorsWithoutPairQuorum++
	d.visualEntry(VisualDiagnosticEntry{Stage: "anchor", Policy: VisualMeasurementCalibrated, AnchorSourceKey: anchor, Outcome: "insufficient_pair_quorum", Count: members})
}

func (d *diagnosticsCollector) visualGuard(anchor string, accepted bool) {
	if d == nil {
		return
	}
	d.value.Visual.Counts.GuardChecks++
	outcome := "accepted"
	if !accepted {
		d.value.Visual.Counts.GuardRejected++
		outcome = "guard_rejected"
	}
	d.visualEntry(VisualDiagnosticEntry{Stage: "guard", Policy: VisualMeasurementCalibrated, AnchorSourceKey: anchor, Outcome: outcome})
}

func (d *diagnosticsCollector) visualOriginalSelection(selection map[string]string) map[string]bool {
	if d == nil {
		return nil
	}
	ambiguous := map[string]bool{}
	for source, group := range selection {
		if group == "" {
			ambiguous[source] = true
			d.value.Visual.Counts.OriginalAmbiguousSources++
		}
	}
	return ambiguous
}

func (d *diagnosticsCollector) visualCompleted(selection map[string]string, ambiguous map[string]bool, groups int) {
	if d == nil {
		return
	}
	v := &d.value.Visual
	v.Completed = true
	v.Counts.PublishedGroups = groups
	sources := make([]string, 0, len(ambiguous))
	for source := range ambiguous {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		outcome := "recovered"
		if selection[source] == "" {
			v.Counts.RetainedAmbiguousSources++
			outcome = "ambiguity_retained"
		} else {
			v.Counts.RecoveredSources++
		}
		d.visualEntry(VisualDiagnosticEntry{Stage: "selection", LeftSourceKey: source, Outcome: outcome})
	}
}
