package introdetect

import (
	"context"
	"strings"
	"testing"
)

func TestCalibratedAuditBindsGeometryClockAndAnchor(t *testing.T) {
	cohort, bounds := calibratedSequenceFixture()
	result, err := DiscoverVisualSequences(context.Background(), cohort, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	group := requireCalibratedFixtureGroup(t, result, bounds)
	if group.Calibration == nil || group.Metrics.MeasurementPolicy != VisualMeasurementCalibrated || len(group.Metrics.CalibrationDigest) != 64 || group.Metrics.CalibrationDigest != visualCalibrationDigest(group) {
		t.Fatalf("calibrated audit missing or unbound: %+v", group)
	}
	for _, change := range []string{"geometry", "clock", "anchor"} {
		altered := group
		audit := *group.Calibration
		audit.Members = append([]calibratedMemberAudit(nil), audit.Members...)
		altered.Calibration = &audit
		switch change {
		case "geometry":
			audit.Members[1].Geometry.ShiftXPermille += 10
		case "clock":
			audit.Members[1].ClockOffsetTicks++
		case "anchor":
			audit.AnchorSourceKey = "different-anchor"
		}
		if visualCalibrationDigest(altered) == group.Metrics.CalibrationDigest {
			t.Fatalf("%s mutation preserved calibrated audit identity", change)
		}
	}
	candidate := Candidate{Interval: group.Members[0].Interval, Status: Qualified, Support: group.Members, VisualEvidence: &group.Metrics}
	if !ValidateCandidateEvidence(candidate, DefaultOptions()) {
		t.Fatal("current calibrated evidence was rejected")
	}
	for _, policy := range []string{"", "unknown-policy", VisualMeasurementCoarse} {
		metrics := group.Metrics
		metrics.MeasurementPolicy = policy
		candidate.VisualEvidence = &metrics
		if ValidateCandidateEvidence(candidate, DefaultOptions()) {
			t.Fatalf("invalid policy/digest combination accepted: %q", policy)
		}
	}
	for _, digest := range []string{"", strings.Repeat("g", 64), strings.Repeat("A", 64), strings.Repeat("0", 63)} {
		metrics := group.Metrics
		metrics.CalibrationDigest = digest
		candidate.VisualEvidence = &metrics
		if ValidateCandidateEvidence(candidate, DefaultOptions()) {
			t.Fatalf("invalid calibrated digest accepted: %q", digest)
		}
	}
}
