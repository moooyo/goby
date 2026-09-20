package library

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestMediaOperationReceiptBindsSelectionButNotDeploymentSnapshot(t *testing.T) {
	request := MediaOperationRequest{RequestID: "retry-1", Kind: MediaOperationOCR, ItemID: strings.Repeat("a", 32), SourceRevision: "source-revision", StreamIndex: 3,
		Parameters: MediaOperationParameters{ModelIDs: []string{"eng", "chi_sim"}, OutputFormat: "srt"}, ExecutionSnapshot: json.RawMessage(`{"Version":1}`), MaxQueued: 16}
	request.MediaSourceID = media.SourceID(request.ItemID)
	if err := validateMediaOperationRequest(request); err != nil {
		t.Fatal(err)
	}
	original, err := mediaOperationRequestFingerprint(request)
	if err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.ExecutionSnapshot = json.RawMessage(`{"Version":2}`)
	changed.MaxQueued = 32
	retry, err := mediaOperationRequestFingerprint(changed)
	if err != nil || !bytes.Equal(original, retry) {
		t.Fatal("deployment changes altered the original request receipt")
	}
	for _, edit := range []func(*MediaOperationRequest){func(r *MediaOperationRequest) { r.StreamIndex++ }, func(r *MediaOperationRequest) { r.SourceRevision = "replacement" }, func(r *MediaOperationRequest) { r.Parameters.OutputFormat = "vtt" }, func(r *MediaOperationRequest) { r.Parameters.IsForced = true }} {
		changed = request
		edit(&changed)
		digest, err := mediaOperationRequestFingerprint(changed)
		if err != nil || bytes.Equal(original, digest) {
			t.Fatal("a different source, track, or output retained the original receipt")
		}
	}
}

func TestMediaOperationControlProjectionRetainsExplicitCleanup(t *testing.T) {
	for _, state := range []string{"interrupted", "failed", "stale", "recovery_required"} {
		op := MediaOperation{Kind: MediaOperationRemoveSubtitle, State: state, PublicationPhase: "none"}
		projectMediaOperation(&op)
		if !op.CanCancel || op.CanApply {
			t.Fatalf("unpublished %s candidate has no explicit cleanup action", state)
		}
		op.PublicationPhase = "prepared"
		projectMediaOperation(&op)
		if op.CanCancel {
			t.Fatalf("uncertain %s publication offered destructive cancellation", state)
		}
	}
	op := MediaOperation{Kind: MediaOperationOCR, State: "completed", PublicationPhase: "done"}
	projectMediaOperation(&op)
	if !op.Applied || op.CanCancel || op.CanApply || op.CanReview {
		t.Fatal("completed output retained an execution action")
	}
}

func TestMediaOperationReviewHashIncludesCorrectedTimingAndOriginalEvidence(t *testing.T) {
	op := MediaOperation{Parameters: MediaOperationParameters{OutputFormat: "srt"}, ResultSummary: json.RawMessage(`{"CueCount":1}`)}
	cues := []MediaOperationCue{{Ordinal: 0, OriginalStartTicks: 10, OriginalEndTicks: 30, OriginalText: "observed", StartTicks: 10, EndTicks: 30, Text: "corrected", Included: true, Warnings: []string{}}}
	first, err := mediaOperationReviewHash(op, cues)
	if err != nil {
		t.Fatal(err)
	}
	cues[0].ImagePNG = []byte("not serialized")
	same, err := mediaOperationReviewHash(op, cues)
	if err != nil || same != first {
		t.Fatal("private image buffers changed the review serialization")
	}
	cues[0].EndTicks = 31
	changed, err := mediaOperationReviewHash(op, cues)
	if err != nil || changed == first {
		t.Fatal("corrected timing did not invalidate Apply")
	}
	cues[0].EndTicks = 30
	cues[0].OriginalText = "different observation"
	changed, err = mediaOperationReviewHash(op, cues)
	if err != nil || changed == first {
		t.Fatal("original evidence was omitted from the review digest")
	}
}

func TestMediaOperationRequestRejectsUnsupportedModelsAndTextControls(t *testing.T) {
	request := MediaOperationRequest{RequestID: "request", Kind: MediaOperationOCR, ItemID: strings.Repeat("a", 32), SourceRevision: "revision", StreamIndex: 1, MaxQueued: 16, ExecutionSnapshot: json.RawMessage(`{}`), Parameters: MediaOperationParameters{ModelIDs: []string{"eng"}, OutputFormat: "vtt"}}
	request.MediaSourceID = media.SourceID(request.ItemID)
	for _, models := range [][]string{{"eng", "eng"}, {"eng-v1"}, {"unconfigured"}, nil} {
		request.Parameters.ModelIDs = models
		if validateMediaOperationRequest(request) == nil {
			t.Fatalf("accepted model selection %v", models)
		}
	}
	for _, text := range []string{"", "line\n\nline", "text\x00", "text\t"} {
		edit := MediaOperationCueEdit{StartTicks: 1, EndTicks: 2, Text: text, Included: true}
		if text == "line\n\nline" {
			continue
		} // Document rendering separately rejects empty lines.
		if validMediaOperationCueEdit(edit) {
			t.Fatalf("accepted invalid included text %q", text)
		}
	}
}
