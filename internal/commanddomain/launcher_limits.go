package commanddomain

// LimitsClass selects one fixed root-owned tool policy. It does not accept
// client resource values, grant an executable or prove storage retirement.
type LimitsClass uint8

const (
	LimitsDefault LimitsClass = iota
	LimitsAnalysis
	LauncherLimitsConfigVersion         = 2
	LauncherLimitsPolicyVersion         = 1
	launcherAnalysisAddressSpace uint64 = 2147483648
	launcherAnalysisOpenFiles    uint64 = 64
	launcherAnalysisFileSize     uint64 = 0
)

// LauncherLimits is the sole version-two sealed policy. The class fixes both
// soft and hard limits; arbitrary limit arrays and numerical overrides are not
// part of the wire schema. Version one omits this field completely.
type LauncherLimits struct {
	Version int    `json:"version"`
	Class   string `json:"class"`
}

func validateLimitsClass(class LimitsClass) error {
	if class != LimitsDefault && class != LimitsAnalysis {
		return ErrUnsafe
	}
	return nil
}

func launcherLimitsForClass(class LimitsClass) (int, *LauncherLimits, error) {
	switch class {
	case LimitsDefault:
		return LauncherConfigVersion, nil, nil
	case LimitsAnalysis:
		return LauncherLimitsConfigVersion, &LauncherLimits{Version: LauncherLimitsPolicyVersion, Class: "analysis-v1"}, nil
	default:
		return 0, nil, ErrUnsafe
	}
}

func validateLauncherLimits(version int, limits *LauncherLimits) error {
	switch version {
	case LauncherConfigVersion:
		if limits == nil {
			return nil
		}
	case LauncherLimitsConfigVersion:
		if limits != nil && limits.Version == LauncherLimitsPolicyVersion && limits.Class == "analysis-v1" {
			return nil
		}
	}
	return invalidLauncherConfig("unsupported limit policy or version")
}
