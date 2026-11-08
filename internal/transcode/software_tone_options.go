package transcode

import "fmt"

// SoftwareToneMapOptions contains the native software tone mapping choices
// implemented by the CPU processing graph. Resolution limits affect planning;
// only the selected algorithm enters the immutable execution identity.
type SoftwareToneMapOptions struct {
	ToneMapSizeLimit         string `json:"ToneMapSizeLimit"`
	SoftwareToneMapAlgorithm string `json:"SoftwareToneMapAlgorithm"`
}

func DefaultSoftwareToneMapOptions() SoftwareToneMapOptions {
	return SoftwareToneMapOptions{ToneMapSizeLimit: "None", SoftwareToneMapAlgorithm: "Hable"}
}

// ValidateSoftwareToneMapOptions accepts complete settings objects. The empty
// value is reserved for historical or inactive execution snapshots and is
// handled by their versioned execution validator rather than by this function.
func ValidateSoftwareToneMapOptions(value SoftwareToneMapOptions) error {
	switch value.ToneMapSizeLimit {
	case "None", "Limit420", "Limit720", "Limit1080":
	default:
		return fmt.Errorf("%w: software tone mapping size limit", ErrInvalidExecution)
	}
	switch value.SoftwareToneMapAlgorithm {
	case "Hable", "Reinhard", "Mobius":
		return nil
	default:
		return fmt.Errorf("%w: software tone mapping algorithm", ErrInvalidExecution)
	}
}

// SoftwareToneMapHeightLimit returns the maximum encoded output height for an
// already validated CPU tone mapping policy. Zero leaves the existing server
// and client limits unchanged. Historical absent options also return zero.
func SoftwareToneMapHeightLimit(value SoftwareToneMapOptions) int {
	switch value.ToneMapSizeLimit {
	case "Limit420":
		return 420
	case "Limit720":
		return 720
	case "Limit1080":
		return 1080
	}
	return 0
}

func softwareToneMapFilter(plan Plan) string {
	algorithm := "hable"
	if plan.ExecutionVersion == ExecutionVersion {
		switch plan.Execution.SoftwareToneMapOptions.SoftwareToneMapAlgorithm {
		case "Reinhard":
			algorithm = "reinhard"
		case "Mobius":
			algorithm = "mobius"
		}
	}
	// Keep FFmpeg's per-algorithm parameter defaults. In particular, Reinhard
	// maps its external 0.5 default to an internal coefficient of 1.0; setting
	// an external value of 1.0 would produce a different transform. Hable does
	// not use that parameter. Desaturation preserves the existing graph value.
	return "tonemap=tonemap=" + algorithm + ":desat=2.0"
}
