package introdetect

import "context"

// MaxDiagnosticTraceEntries bounds the combined pair and offset trace. Aggregate
// counts remain complete when later trace entries are omitted.
const MaxDiagnosticTraceEntries = 512

// DiagnosticCounts describe completed matcher stages, not semantic accuracy.
// VisualAccepted includes witnesses that require review. Rejected audio runs
// exclude the separately counted raw-short and guarded-short outcomes.
type DiagnosticCounts struct {
	PairsConsidered          int
	OffsetCandidates         int
	RawAudioRuns             int
	RawShortAudioRuns        int
	RawRejectedAudioRuns     int
	GuardedShortAudioRuns    int
	GuardedRejectedAudioRuns int
	AcceptedAudioRuns        int
	VisualRejected           int
	VisualAccepted           int
	PairHypotheses           int
	Groups                   int
}

// OffsetDiagnostics contains the observations for one nominated audio offset.
// Reasons retain existing matcher meanings; short discovery failures are
// represented by their own counters instead of changing EpisodeResult reasons.
type OffsetDiagnostics struct {
	OffsetTicks   int64
	Counts        DiagnosticCounts
	AudioReasons  []Reason
	VisualReasons []Reason
}

type PairDiagnostics struct {
	LeftEpisodeKey   string
	RightEpisodeKey  string
	OffsetCandidates int
	SearchLimited    bool
	PairHypotheses   int
	Offsets          []OffsetDiagnostics
}

// Diagnostics is an optional bounded trace of the existing audio-led matcher.
// Completed is false after an error; partial counts never authorize publication.
// Diagnostics do not consume comparison budget or influence candidate selection.
type Diagnostics struct {
	Version        string
	Completed      bool
	Counts         DiagnosticCounts
	TraceLimit     int
	TraceEntries   int
	TraceTruncated bool
	Pairs          []PairDiagnostics
}

// AnalyzeWithDiagnostics runs exactly the Analyze pipeline and additionally
// returns bounded observations. On failure, Result has the same empty value as
// Analyze while Diagnostics retains only the stages completed before failure.
func AnalyzeWithDiagnostics(ctx context.Context, cohort Cohort, options Options) (Result, Diagnostics, error) {
	diagnostics := Diagnostics{Version: Version, TraceLimit: MaxDiagnosticTraceEntries, Pairs: []PairDiagnostics{}}
	collector := &diagnosticsCollector{value: &diagnostics}
	result, err := analyze(ctx, cohort, options, collector)
	diagnostics.Completed = err == nil
	return result, diagnostics, err
}

type diagnosticsCollector struct {
	value  *Diagnostics
	pair   *PairDiagnostics
	offset *OffsetDiagnostics
}

func (d *diagnosticsCollector) reserve() bool {
	if d.value.TraceEntries >= d.value.TraceLimit {
		d.value.TraceTruncated = true
		return false
	}
	d.value.TraceEntries++
	return true
}

func (d *diagnosticsCollector) beginPair(a, b Episode) {
	if d == nil {
		return
	}
	d.value.Counts.PairsConsidered++
	d.pair, d.offset = nil, nil
	if d.reserve() {
		d.value.Pairs = append(d.value.Pairs, PairDiagnostics{LeftEpisodeKey: a.EpisodeKey, RightEpisodeKey: b.EpisodeKey, Offsets: []OffsetDiagnostics{}})
		d.pair = &d.value.Pairs[len(d.value.Pairs)-1]
	}
}

func (d *diagnosticsCollector) nominatedOffsets(count int, limited bool) {
	if d == nil {
		return
	}
	d.value.Counts.OffsetCandidates += count
	if d.pair != nil {
		d.pair.OffsetCandidates, d.pair.SearchLimited = count, limited
	}
}

func (d *diagnosticsCollector) beginOffset(offset int64) {
	if d == nil {
		return
	}
	d.offset = nil
	if d.pair != nil && d.reserve() {
		d.pair.Offsets = append(d.pair.Offsets, OffsetDiagnostics{OffsetTicks: offset, AudioReasons: []Reason{}, VisualReasons: []Reason{}})
		d.offset = &d.pair.Offsets[len(d.pair.Offsets)-1]
	}
}

type diagnosticAudioStage int

const (
	diagnosticAudioRaw diagnosticAudioStage = iota
	diagnosticAudioRawShort
	diagnosticAudioRawRejected
	diagnosticAudioGuardedShort
	diagnosticAudioGuardedRejected
	diagnosticAudioAccepted
)

func (c *DiagnosticCounts) audioStage(stage diagnosticAudioStage) {
	switch stage {
	case diagnosticAudioRaw:
		c.RawAudioRuns++
	case diagnosticAudioRawShort:
		c.RawShortAudioRuns++
	case diagnosticAudioRawRejected:
		c.RawRejectedAudioRuns++
	case diagnosticAudioGuardedShort:
		c.GuardedShortAudioRuns++
	case diagnosticAudioGuardedRejected:
		c.GuardedRejectedAudioRuns++
	case diagnosticAudioAccepted:
		c.AcceptedAudioRuns++
	}
}

func (d *diagnosticsCollector) audioStage(stage diagnosticAudioStage, reason Reason) {
	if d == nil {
		return
	}
	d.value.Counts.audioStage(stage)
	if d.offset != nil {
		d.offset.Counts.audioStage(stage)
		if reason != "" {
			d.offset.AudioReasons = addReason(d.offset.AudioReasons, reason)
		}
	}
}

func (d *diagnosticsCollector) visualResult(match *pairMatch, reason Reason) {
	if d == nil {
		return
	}
	if match == nil {
		d.value.Counts.VisualRejected++
		if d.offset != nil {
			d.offset.Counts.VisualRejected++
		}
	} else {
		d.value.Counts.VisualAccepted++
		if d.offset != nil {
			d.offset.Counts.VisualAccepted++
			for _, retained := range match.reasons {
				d.offset.VisualReasons = addReason(d.offset.VisualReasons, retained)
			}
		}
	}
	if d.offset != nil && reason != "" {
		d.offset.VisualReasons = addReason(d.offset.VisualReasons, reason)
	}
}

func (d *diagnosticsCollector) pairHypotheses(count int) {
	if d == nil {
		return
	}
	d.value.Counts.PairHypotheses += count
	if d.pair != nil {
		d.pair.PairHypotheses = count
	}
}

func (d *diagnosticsCollector) groups(count int) {
	if d != nil {
		d.value.Counts.Groups = count
	}
}
