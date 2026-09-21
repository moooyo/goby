package introdetect

// ValidateCandidateEvidence validates the current-version metric domain for
// both review and qualified observations. Only qualified observations must
// satisfy the joint evidence policy. It does not validate identities, clique
// membership, group IDs or a caller's publication authority.
func ValidateCandidateEvidence(candidate Candidate, options Options) bool {
	o, err := normalizeOptions(options)
	if err != nil || candidate.Status != Qualified && candidate.Status != Review || !validEvidenceMetrics(candidate.Metrics) {
		return false
	}
	if candidate.Status == Review {
		return true
	}
	return len(candidate.Reasons) == 0 && candidate.Interval.EndTicks-candidate.Interval.StartTicks >= o.AutoMinDurationTicks && qualifiedEvidence(candidate.Metrics, o)
}

func validEvidenceMetrics(m Metrics) bool {
	for _, value := range []int{m.AudioAgreementPermille, m.AudioInformativePermille, m.AudioSimilarityPermille,
		m.VisualAgreementPermille, m.VisualSimilarityPermille, m.VisualCoveragePermille,
		m.VisualChangeCoveragePermille, m.VisualDominancePermille, m.VisualMinBandMatchedPermille,
		m.VisualMatchedTimePermille, m.VisualContradictedTimePermille, m.VisualUnobservableTimePermille,
		m.VisualDominantStatePermille} {
		if value < 0 || value > 1000 {
			return false
		}
	}
	for _, value := range []int64{m.VisualMaxUnconfirmedGapTicks, m.VisualStartAnchorGapTicks, m.VisualEndAnchorGapTicks} {
		if value < 0 || value > 600*TicksPerSecond {
			return false
		}
	}
	return m.AudioSamples >= 0 && m.AudioSamples <= 8192 && m.AudioDistinct >= 0 && m.AudioDistinct <= m.AudioSamples &&
		m.VisualSamples >= 0 && m.VisualSamples <= 4096 && m.VisualTransitions >= 0 && m.VisualTransitions <= 4096 &&
		m.VisualAnchorCount >= 0 && m.VisualAnchorCount <= 4096 && m.VisualDistinctStates >= 0 && m.VisualDistinctStates <= 4096 &&
		m.PairCount >= 0 && m.PairCount <= 496 && m.BoundaryUncertaintyTicks >= 0 && m.BoundaryUncertaintyTicks <= 30*TicksPerSecond
}

// qualifiedEvidence is shared by the matcher and persisted-result validator.
// The legacy adjacent-change diagnostics are deliberately absent from this gate.
func qualifiedEvidence(m Metrics, o Options) bool {
	return validEvidenceMetrics(m) && m.AudioDistinct >= 12 && m.AudioAgreementPermille >= o.MinAudioAgreement &&
		m.AudioInformativePermille >= o.MinAudioInformation && m.AudioSimilarityPermille >= o.MinAudioSimilarity &&
		m.VisualSamples >= o.MinVisualSamples && m.VisualCoveragePermille >= 700 &&
		m.VisualAgreementPermille >= o.MinVisualAgreement && m.VisualSimilarityPermille >= o.MinVisualSimilarity &&
		m.VisualMatchedTimePermille >= o.MinVisualAgreement && m.VisualMinBandMatchedPermille >= o.MinVisualBandMatchedPermille &&
		m.VisualAnchorCount > 0 && m.VisualMaxUnconfirmedGapTicks <= o.MaxVisualUnconfirmedGapTicks &&
		m.VisualStartAnchorGapTicks <= o.MaxVisualAnchorEdgeGapTicks && m.VisualEndAnchorGapTicks <= o.MaxVisualAnchorEdgeGapTicks &&
		m.VisualDistinctStates >= o.MinVisualStates && m.VisualDominantStatePermille <= o.MaxVisualStateDominancePermille
}
