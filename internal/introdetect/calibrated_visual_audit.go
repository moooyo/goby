package introdetect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// This canonical in-process audit is bound into publication group identity.
// Persisted metric names retain their v5 interpretation; this is not v4 data.
type calibratedMemberAudit struct {
	SourceKey        string
	Geometry         calibratedGeometry
	ClockOffsetTicks int64
}

type calibratedVisualAudit struct {
	Policy                   string
	AnchorSourceKey          string
	CropPermille             int
	ScaleYGrid               []int
	ShiftRangePermille       int
	ShiftStepPermille        int
	NominationStepTicks      int64
	GeometryScoringStepTicks int64
	MotionLagTicks           int64
	PhaseRadiusTicks         int64
	PhaseStepTicks           int64
	BoundaryGuardTicks       int64
	Members                  []calibratedMemberAudit
}

func calibratedAuditForGroup(group VisualSequenceGroup, anchor int, episodes []Episode, geometries map[int]calibratedGeometry) *calibratedVisualAudit {
	audit := &calibratedVisualAudit{Policy: "raster16-bilinear32-luma8-dhash9x8-global-geometry-v2", AnchorSourceKey: episodes[anchor].SourceKey, CropPermille: calibratedCropPermille, ScaleYGrid: []int{940, 1000, 1060}, ShiftRangePermille: 30, ShiftStepPermille: 10, NominationStepTicks: calibratedNominationStep, GeometryScoringStepTicks: TicksPerSecond, MotionLagTicks: TicksPerSecond, PhaseRadiusTicks: visualSequenceResidual, PhaseStepTicks: calibratedPhaseStep, BoundaryGuardTicks: visualSequenceResidual, Members: []calibratedMemberAudit{}}
	anchorStart := group.Members[0].Interval.StartTicks
	for _, member := range group.Members {
		for node, episode := range episodes {
			if episode.SourceKey == member.SourceKey {
				audit.Members = append(audit.Members, calibratedMemberAudit{member.SourceKey, geometries[node], member.Interval.StartTicks - anchorStart})
				break
			}
		}
	}
	return audit
}

func visualCalibrationDigest(group VisualSequenceGroup) string {
	if group.Calibration == nil {
		return ""
	}
	encoded, _ := json.Marshal(group.Calibration)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
