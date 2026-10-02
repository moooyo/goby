package transcode

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

// GeneratedAVDiagnosticStageError carries only bounded public diagnostic labels.
// The original error remains available through Unwrap, but its private paths,
// arguments or diagnostics are never copied into Stage, Cause or Error().
type GeneratedAVDiagnosticStageError struct {
	Stage   string
	Variant int
	Number  int64
	Cause   string
	err     error
}

func (failure *GeneratedAVDiagnosticStageError) Error() string {
	return "generated A/V diagnostic failure: " + failure.Stage + ":" + failure.Cause
}

func (failure *GeneratedAVDiagnosticStageError) Unwrap() error { return failure.err }

func generatedAVDiagnosticStageFailure(stage string, variant int, number int64, err error) error {
	if err == nil {
		return nil
	}
	var existing *GeneratedAVDiagnosticStageError
	if errors.As(err, &existing) {
		return err
	}
	switch stage {
	case "options", "source_certificate", "source_effective", "source_pcm", "baseline_production", "baseline_resources", "observer_production", "observer_resources",
		"baseline_transport", "baseline_packets", "baseline_effective", "baseline_pcm", "baseline_continuous_effective", "baseline_continuous_pcm",
		"observer_transport", "observer_packets", "observer_effective", "observer_pcm", "observer_continuous_effective", "observer_continuous_pcm", "comparison", "final_fences", "cleanup":
	default:
		stage = "unknown"
	}
	if variant < -1 || variant >= MaxHLSRenditions {
		variant = -1
	}
	if number < -1 || number >= MaxPlaylistSegments {
		number = -1
	}
	return &GeneratedAVDiagnosticStageError{Stage: stage, Variant: variant, Number: number, Cause: generatedAVDiagnosticCause(err), err: err}
}

func generatedAVDiagnosticCause(err error) string {
	// Compare complete messages against a finite internal-reason allowlist.
	// Never emit a suffix parsed from an arbitrary wrapped error or pathname.
	if errors.Is(err, ErrTimelineProbe) {
		message := err.Error()
		for _, reason := range []string{
			"invalid explicit transport extent", "incomplete transport read", "unfinished PSI section", "missing complete program or access units", "missing selected PES track",
			"SDT program differs from final PAT", "sync, error, scrambling or control", "unmapped PID", "adaptation length", "unsupported adaptation field", "PCR field", "PCR extension",
			"adaptation stuffing", "mid-file discontinuity requires another contract", "adaptation-only continuity", "payload continuity", "empty advertised payload", "null payload",
			"payload before program mapping", "PES continuation without start", "PSI pointer", "PSI pointer does not finish previous section", "PSI prefix without previous section",
			"PSI continuation without section", "PSI trailing bytes", "partial PSI header", "PSI extent", "partial PSI section", "PSI syntax, version or CRC", "single-program PAT",
			"PAT program/PID", "changing PAT", "single AVC/AAC PMT", "PMT stream descriptor", "PMT elementary PID", "duplicate AVC", "duplicate AAC", "unsupported PMT stream type",
			"PMT track/PCR mapping", "changing PMT", "SDT shape", "SDT program differs", "SDT service descriptor", "SDT service strings", "changing SDT", "PES header", "PES stream ID",
			"PES declared extent", "PES timestamp fields", "PTS marker bits", "DTS marker bits", "partial ADTS header", "unsupported ADTS header", "partial or empty ADTS access unit",
			"changing ADTS layout", "effective frame projection", "partial or absent PCM samples", "decoder input copier did not reach complete EOF", "source frame and PCM sample counts differ",
			"held output observation association differs", "continuous frame and PCM sample counts differ",
		} {
			if message == generatedAVTransportInvalid(reason).Error() {
				replacer := strings.NewReplacer(" ", "_", "-", "_", "/", "_", ",", "")
				return strings.ToLower(replacer.Replace(reason))
			}
		}
	}
	switch {
	case errors.Is(err, media.ErrProcessRetirementUnknown):
		return "retirement_unknown"
	case errors.Is(err, ErrTimelineLimit):
		return "limit"
	case errors.Is(err, ErrInvalidInput):
		return "invalid_input"
	case errors.Is(err, ErrInvalidPlan):
		return "invalid_plan"
	case errors.Is(err, ErrInvalidOptions):
		return "invalid_options"
	case errors.Is(err, ErrUnsupportedTimeline), errors.Is(err, ErrUnsupported):
		return "unsupported"
	case errors.Is(err, ErrProgress):
		return "observer"
	case errors.Is(err, ErrTimelineProbe):
		return "timeline_probe"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, ErrOutputUnavailable):
		return "output_unavailable"
	case errors.Is(err, ErrStart):
		return "start"
	case errors.Is(err, ErrProcess):
		return "process"
	case errors.Is(err, os.ErrClosed):
		return "closed"
	default:
		return "other"
	}
}
