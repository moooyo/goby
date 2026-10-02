package transcode

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestGeneratedAVDiagnosticStageKeepsNativeReasonAndErrorChain(t *testing.T) {
	original := generatedAVTransportInvalid("PES declared extent")
	err := generatedAVDiagnosticStageFailure("baseline_transport", 0, 0, original)
	var stage *GeneratedAVDiagnosticStageError
	if !errors.As(err, &stage) || !errors.Is(err, ErrTimelineProbe) || stage.Stage != "baseline_transport" || stage.Variant != 0 || stage.Number != 0 || stage.Cause != "pes_declared_extent" {
		t.Fatal("bounded phase attribution lost the actual internal framing failure")
	}
}

func TestGeneratedAVDiagnosticStageDoesNotCopyPrivateErrorText(t *testing.T) {
	private := "private/path/credential?" + strings.Repeat("x", 4096)
	err := generatedAVDiagnosticStageFailure(private, MaxHLSRenditions, MaxPlaylistSegments, fmt.Errorf("%w: %s", ErrTimelineProbe, private))
	var stage *GeneratedAVDiagnosticStageError
	if !errors.As(err, &stage) || stage.Stage != "unknown" || stage.Variant != -1 || stage.Number != -1 || stage.Cause != "timeline_probe" || len(err.Error()) > 128 || strings.Contains(err.Error(), "credential") {
		t.Fatal("untrusted failure text escaped into a public diagnostic label")
	}
}
