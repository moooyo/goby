package introdetect

// intervalQualityReason is valid only for the interval that was measured.
// Projection recomputes these checks on every complete pair of the proposed
// final intersection. Every other reason is retained, including unknown future
// reasons, so a narrower interval cannot erase a boundary or search limitation.
func intervalQualityReason(reason Reason) bool {
	switch reason {
	case WeakAudioEvidence, WeakVisualEvidence, InsufficientVisualAnchors, LowVisualDiversity, ShortInterval:
		return true
	default:
		return false
	}
}
