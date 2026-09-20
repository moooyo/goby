package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestMediaOperationResultNormalizesOriginalObservations(t *testing.T) {
	confidence := 91.5
	image := []byte("bounded image witness")
	digest := sha256.Sum256(image)
	input := MediaOperationResult{
		Summary:    json.RawMessage(`{"CueCount":1}`),
		ResultHash: "untrusted producer hash",
		Cues: []MediaOperationCue{{
			Ordinal: 31, OriginalStartTicks: 500, OriginalEndTicks: 600, OriginalText: "replaced observation",
			StartTicks: 10, EndTicks: 30, Text: "Recognized text", Included: false,
			Confidence: &confidence, ImageSHA256: hex.EncodeToString(digest[:]), ImagePNG: image,
		}},
	}
	normalized, err := normalizeMediaOperationResult(MediaOperationOCR, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Cues) != 1 || normalized.ResultHash != "" || string(normalized.Journal) != "{}" {
		t.Fatalf("unexpected normalized result: %#v", normalized)
	}
	cue := normalized.Cues[0]
	if cue.Ordinal != 0 || cue.OriginalStartTicks != 10 || cue.OriginalEndTicks != 30 || cue.OriginalText != "Recognized text" || cue.Included {
		t.Fatalf("initial review differs from its observation: %#v", cue)
	}
	if cue.Warnings == nil || cue.ImagePNG == nil || cue.Confidence == nil || *cue.Confidence != 91.5 {
		t.Fatalf("nullable review evidence was not normalized: %#v", cue)
	}
	input.Cues[0].ImagePNG[0] = 'x'
	confidence = 5
	if string(cue.ImagePNG) != "bounded image witness" || *cue.Confidence != 91.5 {
		t.Fatal("the result retained mutable producer evidence")
	}

	op := MediaOperation{Parameters: MediaOperationParameters{OutputFormat: "srt"}, ResultSummary: normalized.Summary}
	hash, err := mediaOperationReviewHash(op, normalized.Cues)
	if err != nil || !mediaOperationHashValid(hash, false) {
		t.Fatalf("normalized review hash = %q, %v", hash, err)
	}
	normalized.Cues[0].Included = true
	changed, err := mediaOperationReviewHash(op, normalized.Cues)
	if err != nil || changed == hash {
		t.Fatal("a changed inclusion decision retained the previous review hash")
	}
}

func TestMediaOperationResultRejectsUnboundedOrInvalidEvidence(t *testing.T) {
	valid := func() MediaOperationResult {
		return MediaOperationResult{Summary: json.RawMessage(`{}`), Cues: []MediaOperationCue{{StartTicks: 0, EndTicks: 20, Text: "caption", Included: true}}}
	}
	tests := []struct {
		name string
		edit func(*MediaOperationResult)
	}{
		{"non-object summary", func(result *MediaOperationResult) { result.Summary = json.RawMessage(`[]`) }},
		{"oversized summary", func(result *MediaOperationResult) {
			result.Summary = json.RawMessage(`{"x":"` + strings.Repeat("x", MaxMediaOperationDocumentBytes) + `"}`)
		}},
		{"empty cues", func(result *MediaOperationResult) { result.Cues = nil }},
		{"too many cues", func(result *MediaOperationResult) { result.Cues = make([]MediaOperationCue, MaxMediaOperationCues+1) }},
		{"negative start", func(result *MediaOperationResult) { result.Cues[0].StartTicks = -1 }},
		{"empty time interval", func(result *MediaOperationResult) { result.Cues[0].EndTicks = 0 }},
		{"invalid UTF-8", func(result *MediaOperationResult) { result.Cues[0].Text = string([]byte{0xff}) }},
		{"NUL text", func(result *MediaOperationResult) { result.Cues[0].Text = "unsafe\x00text" }},
		{"oversized cue text", func(result *MediaOperationResult) {
			result.Cues[0].Text = strings.Repeat("x", MaxMediaOperationDocumentBytes+1)
		}},
		{"non-finite confidence", func(result *MediaOperationResult) { value := math.NaN(); result.Cues[0].Confidence = &value }},
		{"infinite confidence", func(result *MediaOperationResult) { value := math.Inf(1); result.Cues[0].Confidence = &value }},
		{"excess confidence", func(result *MediaOperationResult) { value := 100.1; result.Cues[0].Confidence = &value }},
		{"invalid warning", func(result *MediaOperationResult) { result.Cues[0].Warnings = []string{"line\nbreak"} }},
		{"oversized warning set", func(result *MediaOperationResult) {
			result.Cues[0].Warnings = make([]string, 9)
			for index := range result.Cues[0].Warnings {
				result.Cues[0].Warnings[index] = strings.Repeat("x", 2048)
			}
		}},
		{"oversized image", func(result *MediaOperationResult) { result.Cues[0].ImagePNG = make([]byte, (1<<20)+1) }},
		{"unhashed image", func(result *MediaOperationResult) { result.Cues[0].ImagePNG = []byte("image") }},
		{"missing image", func(result *MediaOperationResult) { result.Cues[0].ImageSHA256 = strings.Repeat("0", 64) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := valid()
			test.edit(&result)
			if _, err := normalizeMediaOperationResult(MediaOperationOCR, result); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid review evidence returned %v", err)
			}
		})
	}
	removal := MediaOperationResult{Summary: json.RawMessage(`{}`), Journal: json.RawMessage(`{"Version":1}`), ResultHash: strings.Repeat("a", 64)}
	if _, err := normalizeMediaOperationResult(MediaOperationRemoveSubtitle, removal); err != nil {
		t.Fatal(err)
	}
	removal.Cues = valid().Cues
	if _, err := normalizeMediaOperationResult(MediaOperationRemoveSubtitle, removal); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("a remux result accepted OCR observations")
	}
}

func TestMediaOperationFinishStatePreservesPublicationReservation(t *testing.T) {
	cancelled := time.Unix(123, 0)
	tests := []struct {
		name     string
		op       MediaOperation
		cause    error
		shutdown bool
		state    string
		code     string
	}{
		{"prepared cancellation", MediaOperation{PublicationPhase: "prepared", CancelRequestedAt: &cancelled}, context.Canceled, false, "recovery_required", "publication_recovery_required"},
		{"committed shutdown", MediaOperation{PublicationPhase: "catalog_committed"}, ErrSourceChanged, true, "recovery_required", "publication_recovery_required"},
		{"uncertain cleanup", MediaOperation{CancelRequestedAt: &cancelled}, errors.Join(context.Canceled, ErrMediaOperationRecovery), false, "recovery_required", "publication_recovery_required"},
		{"discard complete", MediaOperation{CancelRequestedAt: &cancelled}, nil, false, "cancelled", "cancelled"},
		{"cancelled process", MediaOperation{CancelRequestedAt: &cancelled}, context.Canceled, true, "cancelled", "cancelled"},
		{"failed cleanup", MediaOperation{CancelRequestedAt: &cancelled}, errors.New("private storage error"), false, "failed", "execution_failed"},
		{"shutdown", MediaOperation{}, context.Canceled, true, "interrupted", "interrupted"},
		{"source changed", MediaOperation{}, ErrSourceChanged, false, "stale", "source_changed"},
		{"missing final result", MediaOperation{}, nil, false, "failed", "incomplete_result"},
		{"private failure", MediaOperation{}, errors.New("secret source path and command"), false, "failed", "execution_failed"},
		{"OCR review survives invalid apply", MediaOperation{Kind: MediaOperationOCR, State: "applying", PublicationPhase: "none"}, ErrInvalidInput, false, "ready", "execution_failed"},
		{"OCR review survives revoked apply", MediaOperation{Kind: MediaOperationOCR, State: "applying", PublicationPhase: "none"}, ErrForbidden, false, "ready", "authority_changed"},
		{"OCR review survives shutdown", MediaOperation{Kind: MediaOperationOCR, State: "applying", PublicationPhase: "none"}, context.Canceled, true, "ready", "interrupted"},
		{"OCR review requires current source", MediaOperation{Kind: MediaOperationOCR, State: "applying", PublicationPhase: "none"}, ErrSourceChanged, false, "stale", "source_changed"},
		{"OCR callback must publish", MediaOperation{Kind: MediaOperationOCR, State: "applying", PublicationPhase: "none"}, nil, false, "ready", "incomplete_result"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state, code, message := mediaOperationFinishedState(test.op, test.cause, test.shutdown)
			if state != test.state || code != test.code || message == "" {
				t.Fatalf("finish = %q, %q, %q; want %q, %q", state, code, message, test.state, test.code)
			}
			if strings.Contains(message, "secret") || strings.Contains(message, "private") {
				t.Fatal("failure status disclosed a private executor error")
			}
		})
	}
}

func TestMediaOperationClaimAndWorkerFences(t *testing.T) {
	cancelled := time.Unix(123, 0)
	tests := []struct {
		name    string
		op      MediaOperation
		apply   bool
		discard bool
		err     error
	}{
		{"queued", MediaOperation{State: "queued"}, false, false, nil},
		{"apply", MediaOperation{State: "applying"}, true, false, nil},
		{"ready cleanup", MediaOperation{State: "ready", CancelRequestedAt: &cancelled}, false, true, nil},
		{"interrupted cleanup", MediaOperation{State: "interrupted", CancelRequestedAt: &cancelled}, false, true, nil},
		{"cancel before apply", MediaOperation{State: "applying", CancelRequestedAt: &cancelled}, true, true, nil},
		{"live publication cancellation", MediaOperation{State: "applying", PublicationPhase: "prepared", CancelRequestedAt: &cancelled}, false, false, ErrMediaOperationRecovery},
		{"already claimed", MediaOperation{State: "queued", WorkerToken: "claim"}, false, false, ErrMediaOperationConflict},
		{"ready is not automatic", MediaOperation{State: "ready"}, false, false, ErrMediaOperationState},
		{"interrupted is not automatic", MediaOperation{State: "interrupted"}, false, false, ErrMediaOperationState},
		{"completed is not automatic", MediaOperation{State: "completed"}, false, false, ErrMediaOperationState},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			apply, discard, err := mediaOperationClaimMode(test.op)
			if apply != test.apply || discard != test.discard || !errors.Is(err, test.err) {
				t.Fatalf("claim mode = %v, %v, %v", apply, discard, err)
			}
		})
	}
	work := MediaOperationWork{Operation: MediaOperation{ID: strings.Repeat("a", 32), Kind: MediaOperationOCR}, Token: strings.Repeat("b", 32)}
	op := work.Operation
	op.State, op.WorkerToken = "running", work.Token
	if !validMediaOperationWork(work) || checkMediaOperationWork(op, work, false) != nil {
		t.Fatal("a current claim was rejected")
	}
	op.WorkerToken = strings.Repeat("c", 32)
	if !errors.Is(checkMediaOperationWork(op, work, false), ErrMediaOperationConflict) {
		t.Fatal("a superseded worker retained authority")
	}
	op.WorkerToken, op.CancelRequestedAt = work.Token, &cancelled
	if !errors.Is(checkMediaOperationWork(op, work, false), context.Canceled) {
		t.Fatal("a cancelled worker could publish a result")
	}
	op.State, work.Apply = "applying", true
	if err := checkMediaOperationWork(op, work, true); err != nil {
		t.Fatalf("post-commit completion was blocked by cancellation: %v", err)
	}
	work.Token = ""
	if validMediaOperationWork(work) {
		t.Fatal("an empty token passed the worker boundary")
	}
}

func TestMediaOperationProgressBounds(t *testing.T) {
	for _, progress := range []MediaOperationProgress{
		{Stage: "decoding"}, {Stage: "recognizing", Processed: 2}, {Stage: "validating", Processed: 100, Total: 100},
	} {
		if !validMediaOperationProgress(progress) {
			t.Fatalf("valid progress rejected: %#v", progress)
		}
	}
	for _, progress := range []MediaOperationProgress{
		{Stage: "source/private/path"}, {Stage: strings.Repeat("x", 65)}, {Processed: -1}, {Total: -1}, {Processed: 2, Total: 1},
	} {
		if validMediaOperationProgress(progress) {
			t.Fatalf("invalid progress accepted: %#v", progress)
		}
	}
}
