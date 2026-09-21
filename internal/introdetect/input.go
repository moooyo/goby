package introdetect

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func normalizeOptions(o Options) (Options, error) {
	if o == (Options{}) {
		o = DefaultOptions()
	}
	if o.MaxEpisodes < 3 || o.MaxEpisodes > 32 || o.MinSupport < 3 || o.MinSupport > o.MaxEpisodes ||
		o.MaxAudioSamples < 1 || o.MaxAudioSamples > 8192 || o.MaxVisualSamples < 1 || o.MaxVisualSamples > 4096 ||
		o.MaxFeatureBytes < 1024 || o.MaxFeatureBytes > 256<<10 || o.MaxComparisons < 1 || o.MaxComparisons > 100_000_000 ||
		o.MaxOffsetCandidates < 1 || o.MaxOffsetCandidates > 12 || o.MaxCandidatesPerPair < 1 || o.MaxCandidatesPerPair > 32 ||
		o.MaxGroups < 1 || o.MaxGroups > 256 || o.WindowTicks < 15*TicksPerSecond || o.WindowTicks > 600*TicksPerSecond ||
		o.MinDurationTicks < 5*TicksPerSecond || o.MaxDurationTicks < o.MinDurationTicks || o.MaxDurationTicks > o.WindowTicks ||
		o.AutoMinDurationTicks < o.MinDurationTicks || o.AutoMinDurationTicks > o.MaxDurationTicks ||
		o.OffsetBinTicks < TicksPerSecond/20 || o.OffsetBinTicks > 2*TicksPerSecond ||
		o.AudioAlignmentTicks < 1 || o.AudioAlignmentTicks > 2*TicksPerSecond ||
		o.MaxAudioGapTicks < 0 || o.MaxAudioGapTicks > 2*TicksPerSecond ||
		o.VisualAlignmentTicks < 1 || o.VisualAlignmentTicks > 2*TicksPerSecond ||
		o.MaxVisualGapTicks < o.VisualAlignmentTicks || o.MaxVisualGapTicks > 5*TicksPerSecond ||
		o.BoundaryToleranceTicks < 1 || o.BoundaryToleranceTicks > 10*TicksPerSecond ||
		o.MaxAudioHamming < 0 || o.MaxAudioHamming > 8 || o.MaxVisualHamming < 0 || o.MaxVisualHamming > 24 ||
		o.MinAudioAgreement < 800 || o.MinAudioAgreement > 1000 || o.MinVisualAgreement < 650 || o.MinVisualAgreement > 1000 ||
		o.MinAudioInformation < 400 || o.MinAudioInformation > 1000 ||
		o.MinAudioSimilarity < 750 || o.MinAudioSimilarity > 1000 || o.MinVisualSimilarity < 750 || o.MinVisualSimilarity > 1000 ||
		o.MinVisualContrast < 1 || o.MinVisualContrast > 1000 || o.MinVisualSamples < 4 || o.MinVisualSamples > 128 ||
		o.MinVisualTransitions < 2 || o.MinVisualTransitions > 32 ||
		o.MinVisualChangeCoverage < 100 || o.MinVisualChangeCoverage > 1000 || o.MaxVisualDominance < 200 || o.MaxVisualDominance > 900 ||
		o.VisualBandTicks < TicksPerSecond || o.VisualBandTicks > 10*TicksPerSecond ||
		o.MinVisualBandMatchedPermille < 1 || o.MinVisualBandMatchedPermille > 1000 ||
		o.MinVisualAnchorTicks < TicksPerSecond/2 || o.MinVisualAnchorTicks > o.VisualBandTicks ||
		o.MaxVisualUnconfirmedGapTicks < 0 || o.MaxVisualUnconfirmedGapTicks > 5*TicksPerSecond ||
		o.MaxVisualAnchorEdgeGapTicks < 0 || o.MaxVisualAnchorEdgeGapTicks > 5*TicksPerSecond ||
		o.VisualStateRadius < 0 || o.VisualStateRadius > 16 || o.MinVisualStates < 4 || o.MinVisualStates > 32 ||
		o.MinVisualStateAnchorTicks < TicksPerSecond/2 || o.MinVisualStateAnchorTicks > 10*TicksPerSecond ||
		o.MaxVisualStateDominancePermille < 200 || o.MaxVisualStateDominancePermille > 900 {
		return Options{}, fmt.Errorf("%w: invalid options", ErrInvalidInput)
	}
	return o, nil
}

func boundedKey(value string) bool {
	if len(value) == 0 || len(value) > 512 || !utf8.ValidString(value) || strings.TrimSpace(value) == "" {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validateInput(ctx context.Context, c Cohort, o Options) ([]Episode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !boundedKey(c.Key) || len(c.Episodes) == 0 {
		return nil, fmt.Errorf("%w: cohort identity and episodes are required", ErrInvalidInput)
	}
	if len(c.Episodes) > o.MaxEpisodes {
		return nil, fmt.Errorf("%w: complete cohort has %d episodes, maximum %d", ErrLimit, len(c.Episodes), o.MaxEpisodes)
	}
	sources := make(map[string]int)
	for index, e := range c.Episodes {
		if !boundedKey(e.EpisodeKey) || !boundedKey(e.SourceKey) || !boundedKey(e.ContentIdentity) || !boundedKey(e.AlgorithmProfile) ||
			e.DurationTicks <= 0 || e.AudioBoundaryUncertaintyTicks < 0 || e.AudioBoundaryUncertaintyTicks > 30*TicksPerSecond {
			return nil, fmt.Errorf("%w: invalid episode identity or duration", ErrInvalidInput)
		}
		// Budget the in-memory feature representation, including alignment.
		featureBytes := int64(len(e.Audio))*24 + int64(len(e.Visual))*24 + int64(len(e.EpisodeKey)+len(e.SourceKey)+len(e.ContentIdentity)+len(e.AlgorithmProfile))
		if len(e.Audio) > o.MaxAudioSamples || len(e.Visual) > o.MaxVisualSamples || featureBytes > int64(o.MaxFeatureBytes) {
			return nil, fmt.Errorf("%w: feature budget for episode %d", ErrLimit, index)
		}
		end := min(e.DurationTicks, o.WindowTicks)
		var previousEnd int64
		for i, sample := range e.Audio {
			if i%256 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			if sample.StartTicks < previousEnd || sample.StartTicks < 0 || sample.EndTicks <= sample.StartTicks ||
				sample.EndTicks > end || sample.EndTicks-sample.StartTicks > 2*TicksPerSecond {
				return nil, fmt.Errorf("%w: audio bins must be ordered, disjoint and within the explicit window", ErrInvalidInput)
			}
			previousEnd = sample.EndTicks
		}
		var previousTick int64 = -1
		for _, sample := range e.Visual {
			if sample.Ticks <= previousTick || sample.Ticks < 0 || sample.Ticks >= end || sample.Contrast > 1000 {
				return nil, fmt.Errorf("%w: invalid visual timeline or contrast", ErrInvalidInput)
			}
			previousTick = sample.Ticks
		}
		if other, exists := sources[e.SourceKey]; exists {
			p := c.Episodes[other]
			if p.ContentIdentity != e.ContentIdentity || p.AlgorithmProfile != e.AlgorithmProfile || p.DurationTicks != e.DurationTicks ||
				p.AudioBoundaryUncertaintyTicks != e.AudioBoundaryUncertaintyTicks || !reflect.DeepEqual(p.Audio, e.Audio) || !reflect.DeepEqual(p.Visual, e.Visual) {
				return nil, fmt.Errorf("%w: one source key has contradictory feature snapshots", ErrInvalidInput)
			}
		}
		sources[e.SourceKey] = index
	}
	values := append([]Episode(nil), c.Episodes...)
	sort.Slice(values, func(i, j int) bool {
		a, b := values[i], values[j]
		if a.EpisodeKey != b.EpisodeKey {
			return a.EpisodeKey < b.EpisodeKey
		}
		if a.SourceKey != b.SourceKey {
			return a.SourceKey < b.SourceKey
		}
		return a.ContentIdentity < b.ContentIdentity
	})
	return values, nil
}

// Union identities transitively: two aliases connected through a third record
// cannot become independent witnesses by using different content/source keys.
func independentEpisodes(episodes []Episode, o Options) []bool {
	parent := make([]int, len(episodes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	seen := make(map[string]int)
	for i, e := range episodes {
		for kind, key := range []string{e.EpisodeKey, e.SourceKey, e.ContentIdentity} {
			key = fmt.Sprintf("%d:%s", kind, key)
			if prior, exists := seen[key]; exists {
				a, b := find(i), find(prior)
				parent[max(a, b)] = min(a, b)
			} else {
				seen[key] = i
			}
		}
	}
	type quality struct {
		ready  bool
		usable int
		span   int64
	}
	qualities := make([]quality, len(episodes))
	profileComponents := make(map[string]map[int]bool)
	for i, e := range episodes {
		audio, visual := 0, 0
		for j := range e.Audio {
			if informativeAudio(e.Audio, j) {
				audio++
			}
		}
		for _, frame := range e.Visual {
			if frame.Contrast >= o.MinVisualContrast {
				visual++
			}
		}
		q := quality{ready: audio >= 12 && visual >= o.MinVisualSamples, usable: audio + visual}
		if len(e.Audio) > 0 && len(e.Visual) > 0 {
			q.span = min(e.Audio[len(e.Audio)-1].EndTicks-e.Audio[0].StartTicks, e.Visual[len(e.Visual)-1].Ticks-e.Visual[0].Ticks)
		}
		qualities[i] = q
		if q.ready {
			if profileComponents[e.AlgorithmProfile] == nil {
				profileComponents[e.AlgorithmProfile] = make(map[int]bool)
			}
			profileComponents[e.AlgorithmProfile][find(i)] = true
		}
	}
	best := make(map[int]int)
	for i, e := range episodes {
		component := find(i)
		previous, exists := best[component]
		if !exists {
			best[component] = i
			continue
		}
		a, b := qualities[i], qualities[previous]
		profileA, profileB := len(profileComponents[e.AlgorithmProfile]), len(profileComponents[episodes[previous].AlgorithmProfile])
		better := false
		switch {
		case a.ready != b.ready:
			better = a.ready
		case profileA != profileB:
			better = profileA > profileB
		case a.span != b.span:
			better = a.span > b.span
		case a.usable != b.usable:
			better = a.usable > b.usable
		}
		// Input is already canonical, so equal quality retains a stable tie.
		if better {
			best[component] = i
		}
	}
	result := make([]bool, len(episodes))
	for _, representative := range best {
		result[representative] = true
	}
	return result
}

type workBudget struct {
	ctx   context.Context
	limit int64
	used  int64
}

func (b *workBudget) spend() error {
	b.used++
	if b.used > b.limit {
		return fmt.Errorf("%w: comparison budget", ErrLimit)
	}
	if b.used%256 == 0 {
		return b.ctx.Err()
	}
	return nil
}

func absolute(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func addReason(values []Reason, reason Reason) []Reason {
	for _, existing := range values {
		if existing == reason {
			return values
		}
	}
	values = append(values, reason)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}
