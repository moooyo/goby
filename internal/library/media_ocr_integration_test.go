//go:build linux

package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

type mediaOCRFixtureProber struct{}

func (mediaOCRFixtureProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (&libraryFixtureProber{}).ProbeFile(ctx, file)
	if err != nil {
		return info, err
	}
	info.Streams = append(info.Streams, media.Stream{Index: 7, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", Language: "eng"})
	return info, nil
}

// Persistence tests start at the engine result boundary. Real PGS/DVD decoding
// and Tesseract recognition have separate media and test-environment fixtures.
func mediaOCRReadyFixture(t *testing.T) (mediaSourceFixture, identity.Principal, MediaOperation) {
	t.Helper()
	fixture := mediaSourceTestCatalog(t, mediaOCRFixtureProber{})
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "ocr-review-administrator")
	target, err := fixture.store.GetMediaOperationTarget(fixture.ctx, actor, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := fixture.store.StartMediaOperation(fixture.ctx, actor, MediaOperationRequest{
		RequestID: "recognize-captions", Kind: MediaOperationOCR, ItemID: fixture.item.ID, MediaSourceID: target.MediaSourceID,
		SourceRevision: target.SourceRevision, StreamIndex: 7, MaxQueued: 4, ExecutionSnapshot: json.RawMessage(`{"Engine":"fixture"}`),
		Parameters: MediaOperationParameters{ModelIDs: []string{"eng", "chi_sim"}, OutputFormat: "vtt", Language: "zh-CN",
			Title: "Reviewed English and Chinese", IsForced: true, IsHearingImpaired: true},
	})
	if err != nil || !admission.Admitted {
		t.Fatalf("admit OCR persistence fixture: %v", err)
	}
	work, err := fixture.store.ClaimMediaOperation(fixture.ctx, admission.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded.Bytes())
	confidence := 42.5
	result := MediaOperationResult{Summary: json.RawMessage(`{"CueCount":2,"Warnings":["Review low confidence text."]}`), Cues: []MediaOperationCue{
		{StartTicks: media.TicksPerSecond, EndTicks: 3 * media.TicksPerSecond, Text: "Helo 中文", Included: true, Confidence: &confidence,
			ImagePNG: encoded.Bytes(), ImageSHA256: hex.EncodeToString(digest[:]), Warnings: []string{"Low confidence recognition."}},
		{StartTicks: 2 * media.TicksPerSecond, EndTicks: 4 * media.TicksPerSecond, Text: "", Included: false, Confidence: &confidence,
			ImagePNG: encoded.Bytes(), ImageSHA256: hex.EncodeToString(digest[:]), Warnings: []string{"No text was recognized."}},
	}}
	if err := fixture.store.ReadyMediaOperation(fixture.ctx, work, result); err != nil {
		t.Fatal(err)
	}
	operation, err := fixture.store.GetMediaOperation(fixture.ctx, actor, admission.Operation.ID)
	if err != nil || operation.State != "ready" || operation.ResultHash == "" {
		t.Fatalf("recognition did not persist a reviewable operation: %v", err)
	}
	return fixture, actor, operation
}

func mediaOCRAdmitApply(t *testing.T, fixture mediaSourceFixture, actor identity.Principal, operation MediaOperation) (MediaOperationApplyRequest, MediaOperationWork) {
	t.Helper()
	request := MediaOperationApplyRequest{Revision: operation.Revision, SourceRevision: operation.SourceRevision,
		ResultHash: operation.ResultHash, RequestID: "apply-reviewed-captions"}
	admission, err := fixture.store.ApplyMediaOperation(fixture.ctx, actor, operation.ID, request)
	if err != nil || !admission.Admitted {
		t.Fatalf("admit reviewed OCR application: %v", err)
	}
	work, err := fixture.store.ClaimMediaOperation(fixture.ctx, operation.ID)
	if err != nil || !work.Apply {
		t.Fatalf("claim reviewed OCR application: %v", err)
	}
	return request, work
}

func TestMediaOCRReviewApplyPublishesPreservedCaptionsAtomically(t *testing.T) {
	fixture, actor, original := mediaOCRReadyFixture(t)
	reviewed, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, original.ID, original.Revision, []MediaOperationCueEdit{
		{Ordinal: 0, StartTicks: media.TicksPerSecond, EndTicks: 3 * media.TicksPerSecond, Text: "Hello 中文", Included: true},
		{Ordinal: 1, StartTicks: 2 * media.TicksPerSecond, EndTicks: 4 * media.TicksPerSecond, Text: "Recovered caption", Included: true},
	})
	if err != nil || reviewed.ResultHash == original.ResultHash || reviewed.Revision <= original.Revision {
		t.Fatalf("correction did not produce a new review revision: %v", err)
	}
	if _, err := fixture.store.ApplyMediaOperation(fixture.ctx, actor, original.ID, MediaOperationApplyRequest{
		Revision: original.Revision, SourceRevision: original.SourceRevision, ResultHash: original.ResultHash, RequestID: "stale-review"}); !errors.Is(err, ErrMediaOperationConflict) {
		t.Fatalf("an earlier review revision was applied: %v", err)
	}
	notifications := make(chan CatalogNotification, 8)
	fixture.store.SetCatalogChangeListener(func(notification CatalogNotification) { notifications <- notification })
	request, work := mediaOCRAdmitApply(t, fixture, actor, reviewed)
	if err := fixture.store.ApplyMediaOCR(fixture.ctx, work); err != nil {
		t.Fatal(err)
	}
	completed, err := fixture.store.GetMediaOperation(fixture.ctx, actor, original.ID)
	if err != nil || completed.State != "completed" || !completed.Applied || completed.WorkerToken != "" {
		t.Fatalf("published OCR operation is not complete: %v", err)
	}
	var summary struct {
		AppliedStreamIndex                  int
		AppliedFormat, AppliedContentSHA256 string
	}
	if json.Unmarshal(completed.ResultSummary, &summary) != nil || summary.AppliedStreamIndex <= 7 || summary.AppliedFormat != "vtt" || len(summary.AppliedContentSHA256) != 64 {
		t.Fatal("durable OCR result does not identify the actual applied stream")
	}
	content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), summary.AppliedStreamIndex)
	if err != nil || !content.Info.Owned || !content.Info.IsForced || !content.Info.IsHearingImpaired || content.Info.Language != "zh-CN" || content.Info.Tag != summary.AppliedContentSHA256 {
		t.Fatalf("applied OCR subtitle metadata is not deliverable: %v", err)
	}
	document, err := subtitle.Parse(content.Data, subtitle.FormatWebVTT)
	if err != nil || len(document.Cues) != 2 || document.Cues[0].Text != "Hello 中文" || document.Cues[1].Text != "Recovered caption" ||
		document.Cues[1].StartTicks >= document.Cues[0].EndTicks {
		t.Fatalf("applied review lost corrected text or overlapping timing: %v", err)
	}
	page, err := fixture.store.GetMediaOperationReview(fixture.ctx, actor, original.ID, MediaOperationPageOptions{Limit: 20})
	if err != nil || len(page.Items) != 2 || page.Items[0].OriginalText != "Helo 中文" || page.Items[0].Text != "Hello 中文" ||
		page.Items[1].OriginalText != "" || page.Items[1].Text != "Recovered caption" {
		t.Fatalf("publication overwrote original recognition evidence: %v", err)
	}
	if imageBytes, err := fixture.store.GetMediaOperationCueImage(fixture.ctx, actor, original.ID, 0); err != nil || len(imageBytes) == 0 {
		t.Fatalf("publication lost its review image: %v", err)
	}
	select {
	case notice := <-notifications:
		if notice.Resync || len(notice.Changes) != 1 || notice.Changes[0].ItemID != fixture.item.ID || notice.Changes[0].Kind != CatalogUpdated {
			t.Fatal("OCR publication emitted an unrelated catalog notification")
		}
	default:
		t.Fatal("OCR publication committed without a catalog notification")
	}
	if replay, err := fixture.store.ApplyMediaOperation(fixture.ctx, actor, original.ID, request); err != nil || replay.Admitted || replay.Operation.State != "completed" {
		t.Fatalf("completed apply receipt was not idempotent: %v", err)
	}
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM item_owned_subtitles WHERE operation_id=$1`, original.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("apply receipt replay duplicated its subtitle: count=%d err=%v", count, err)
	}
	if err := fixture.store.Close(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(fixture.pool, mediaSourceTestProber{inner: mediaOCRFixtureProber{}}, []string{fixture.allowedRoot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close(context.Background()) })
	if err := reopened.RecoverMediaOperations(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	after, err := reopened.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), summary.AppliedStreamIndex)
	if err != nil || !bytes.Equal(after.Data, content.Data) || after.Info.Tag != content.Info.Tag {
		t.Fatalf("restart did not preserve the applied subtitle bytes: %v", err)
	}
	preserved, err := reopened.GetMediaOperationReview(fixture.ctx, actor, original.ID, MediaOperationPageOptions{Limit: 20})
	if err != nil || preserved.Operation.State != "completed" || len(preserved.Items) != 2 || preserved.Items[0].OriginalText != "Helo 中文" {
		t.Fatalf("restart changed completed review history: %v", err)
	}
}

func TestMediaOCRApplyRollbackPublishesNeitherBytesNorCompletion(t *testing.T) {
	fixture, actor, operation := mediaOCRReadyFixture(t)
	_, work := mediaOCRAdmitApply(t, fixture, actor, operation)
	notifications := make(chan CatalogNotification, 8)
	fixture.store.SetCatalogChangeListener(func(notification CatalogNotification) { notifications <- notification })
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION reject_owned_ocr_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'fixture commit rejection'; END $$;
		CREATE CONSTRAINT TRIGGER reject_owned_ocr_commit AFTER INSERT ON item_owned_subtitles
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_owned_ocr_commit()`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.ApplyMediaOCR(fixture.ctx, work); err == nil {
		t.Fatal("deferred commit rejection was ignored")
	}
	var count int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM item_owned_subtitles WHERE operation_id=$1`, operation.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed apply published owned bytes: count=%d err=%v", count, err)
	}
	current, err := fixture.store.GetMediaOperation(fixture.ctx, actor, operation.ID)
	if err != nil || current.State != "applying" || current.Applied || current.WorkerToken != work.Token {
		t.Fatalf("failed transaction persisted a completed operation: %v", err)
	}
	select {
	case <-notifications:
		t.Fatal("rolled-back OCR publication emitted a catalog update")
	default:
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DROP TRIGGER reject_owned_ocr_commit ON item_owned_subtitles; DROP FUNCTION reject_owned_ocr_commit()`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.ApplyMediaOCR(fixture.ctx, work); err != nil {
		t.Fatalf("confirmed rolled-back publication could not be retried: %v", err)
	}
}

func TestMediaOCRApplyRejectsChangedSourceCancelledWorkAndRevokedAuthority(t *testing.T) {
	for _, scenario := range []string{"source", "cancel", "authority"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, actor, operation := mediaOCRReadyFixture(t)
			_, work := mediaOCRAdmitApply(t, fixture, actor, operation)
			switch scenario {
			case "source":
				if err := os.WriteFile(fixture.path, []byte("video:replacement-before-ocr-apply"), 0600); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				if _, err := fixture.store.CancelMediaOperation(fixture.ctx, actor, operation.ID, work.Operation.Revision); err != nil {
					t.Fatal(err)
				}
			case "authority":
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			if err := fixture.store.ApplyMediaOCR(fixture.ctx, work); err == nil {
				t.Fatal("an invalidated OCR apply claim published a subtitle")
			}
			var count int
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM item_owned_subtitles WHERE operation_id=$1`, operation.ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invalidated apply retained derivative bytes: count=%d err=%v", count, err)
			}
		})
	}
}
