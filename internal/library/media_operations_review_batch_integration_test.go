//go:build linux

package library

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

// Extend the existing ready fixture inside its private schema while retaining
// its real source snapshot, administrator grant, and original PNG evidence.
func mediaOperationReviewBatchFixture(t *testing.T, count int) (mediaSourceFixture, identity.Principal, MediaOperation, []MediaOperationCue) {
	t.Helper()
	fixture, actor, operation := mediaOCRReadyFixture(t)
	if count < 2 {
		t.Fatal("batch fixture requires at least two cues")
	}
	if count > 2 {
		_, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO media_operation_cues
			(operation_id,ordinal,original_start_ticks,original_end_ticks,original_text,start_ticks,end_ticks,text,
			 included,confidence,warnings,image_sha256,image_png,is_forced,is_hearing_impaired)
			SELECT cue.operation_id,extra.ordinal,cue.original_start_ticks+extra.ordinal*$3::bigint,
			 cue.original_end_ticks+extra.ordinal*$3::bigint,cue.original_text || ' #' || extra.ordinal,
			 cue.start_ticks+extra.ordinal*$3::bigint,cue.end_ticks+extra.ordinal*$3::bigint,
			 cue.text || ' #' || extra.ordinal,true,cue.confidence,cue.warnings,cue.image_sha256,cue.image_png,
			 extra.ordinal%2=0,extra.ordinal%3=0
			FROM media_operation_cues cue CROSS JOIN generate_series(2,$2::integer) extra(ordinal)
			WHERE cue.operation_id=$1 AND cue.ordinal=0`, operation.ID, count-1, 3*media.TicksPerSecond)
		if err != nil {
			t.Fatal(err)
		}
	}
	cues := mediaOperationReviewBatchCues(t, fixture, operation.ID)
	if len(cues) != count {
		t.Fatalf("batch fixture has %d cues, want %d", len(cues), count)
	}
	operation.ResultSummary = []byte(fmt.Sprintf(`{"CueCount":%d,"Warnings":["Review low confidence text."]}`, count))
	digest, err := mediaOperationReviewHash(operation, cues)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE media_operations SET result_summary=$2,result_hash=$3 WHERE id=$1`, operation.ID, operation.ResultSummary, digest); err != nil {
		t.Fatal(err)
	}
	operation, err = fixture.store.GetMediaOperation(fixture.ctx, actor, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Sequence increments survive rollback, so a failed statement cannot hide
	// query amplification by rolling back the counter with its edited rows.
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE SEQUENCE media_review_batch_updates;
		CREATE FUNCTION count_media_review_batch_updates() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM nextval('media_review_batch_updates'); RETURN NULL; END $$;
		CREATE TRIGGER count_media_review_batch_updates AFTER UPDATE ON media_operation_cues
		FOR EACH STATEMENT EXECUTE FUNCTION count_media_review_batch_updates()`); err != nil {
		t.Fatal(err)
	}
	return fixture, actor, operation, cues
}

func mediaOperationReviewBatchCues(t *testing.T, fixture mediaSourceFixture, id string) []MediaOperationCue {
	t.Helper()
	tx, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	cues, err := readMediaOperationCues(fixture.ctx, tx, id)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(fixture.ctx, `SELECT ordinal,image_png FROM media_operation_cues WHERE operation_id=$1 ORDER BY ordinal`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var ordinal int
		var image []byte
		if err := rows.Scan(&ordinal, &image); err != nil {
			t.Fatal(err)
		}
		if ordinal != count || count >= len(cues) {
			t.Fatal("batch evidence rows do not match the complete cue set")
		}
		cues[count].ImagePNG = image
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != len(cues) {
		t.Fatal("batch evidence read omitted a cue image")
	}
	return cues
}

func mediaOperationReviewBatchEdits(cues []MediaOperationCue, count int) []MediaOperationCueEdit {
	edits := make([]MediaOperationCueEdit, 0, count)
	// Reverse input order verifies that the typed relation joins by ordinal.
	for ordinal := count - 1; ordinal >= 0; ordinal-- {
		cue := cues[ordinal]
		edits = append(edits, MediaOperationCueEdit{
			Ordinal: ordinal, StartTicks: cue.StartTicks + 1, EndTicks: cue.EndTicks + 2,
			Text: fmt.Sprintf("Reviewed cue %d: 'quoted' text and \\ evidence.\nSecond line.", ordinal), Included: ordinal%3 != 0,
		})
	}
	return edits
}

func mediaOperationReviewBatchUpdateCount(t *testing.T, fixture mediaSourceFixture) int64 {
	t.Helper()
	var count int64
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM media_review_batch_updates`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertMediaOperationReviewBatchUnchanged(t *testing.T, fixture mediaSourceFixture, actor identity.Principal, operation MediaOperation, cues []MediaOperationCue) {
	t.Helper()
	current, err := fixture.store.GetMediaOperation(fixture.ctx, actor, operation.ID)
	if err != nil || !reflect.DeepEqual(current, operation) {
		t.Fatalf("failed review changed its durable operation: %v", err)
	}
	if currentCues := mediaOperationReviewBatchCues(t, fixture, operation.ID); !reflect.DeepEqual(currentCues, cues) {
		t.Fatal("failed review changed corrected fields or original evidence")
	}
}

func TestMediaOperationReviewBatchPreservesEvidenceAndUsesOneUpdate(t *testing.T) {
	for _, editCount := range []int{1, 200} {
		t.Run(fmt.Sprintf("%d edits", editCount), func(t *testing.T) {
			fixture, actor, original, cues := mediaOperationReviewBatchFixture(t, editCount+1)
			edits := mediaOperationReviewBatchEdits(cues, editCount)
			expected := append([]MediaOperationCue(nil), cues...)
			for _, edit := range edits {
				cue := &expected[edit.Ordinal]
				cue.StartTicks, cue.EndTicks, cue.Text, cue.Included = edit.StartTicks, edit.EndTicks, edit.Text, edit.Included
			}
			expectedHash, err := mediaOperationReviewHash(original, expected)
			if err != nil {
				t.Fatal(err)
			}
			reviewed, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, original.ID, original.Revision, edits)
			if err != nil || reviewed.Revision != original.Revision+1 || reviewed.ResultHash != expectedHash || reviewed.ResultHash == original.ResultHash {
				t.Fatalf("batch review lost its exact revision or whole-result hash: %v", err)
			}
			if count := mediaOperationReviewBatchUpdateCount(t, fixture); count != 1 {
				t.Fatalf("batch review executed %d cue UPDATE statements, want one", count)
			}
			actual := mediaOperationReviewBatchCues(t, fixture, original.ID)
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal("batch review changed an untouched cue, original timing/text, confidence, warnings, PNG, or flags")
			}
			current, err := fixture.store.GetMediaOperation(fixture.ctx, actor, original.ID)
			if err != nil || !reflect.DeepEqual(current, reviewed) {
				t.Fatalf("batch review receipt differs from its committed operation: %v", err)
			}
			cue := actual[0]
			unchanged, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, original.ID, reviewed.Revision, []MediaOperationCueEdit{{
				Ordinal: cue.Ordinal, StartTicks: cue.StartTicks, EndTicks: cue.EndTicks, Text: cue.Text, Included: cue.Included,
			}})
			if err != nil || unchanged.Revision != reviewed.Revision+1 || unchanged.ResultHash != reviewed.ResultHash {
				t.Fatalf("identical review edit changed its hash or revision contract: %v", err)
			}
			if count := mediaOperationReviewBatchUpdateCount(t, fixture); count != 2 {
				t.Fatalf("single identical edit executed an unexpected number of cue UPDATE statements: %d", count)
			}
		})
	}
}

func TestMediaOperationReviewBatchRejectsInvalidEditsAndStaleState(t *testing.T) {
	for _, scenario := range []string{"duplicate ordinal", "missing ordinal", "out of bounds ordinal", "stale revision", "cancelled operation"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, actor, operation, cues := mediaOperationReviewBatchFixture(t, 2)
			edits := mediaOperationReviewBatchEdits(cues, 2)
			revision := operation.Revision
			want := ErrInvalidInput
			switch scenario {
			case "duplicate ordinal":
				edits[1].Ordinal = edits[0].Ordinal
			case "missing ordinal":
				edits[0].Ordinal = len(cues)
			case "out of bounds ordinal":
				edits[0].Ordinal = MaxMediaOperationCues
			case "stale revision":
				revision--
				want = ErrMediaOperationConflict
			case "cancelled operation":
				var err error
				operation, err = fixture.store.CancelMediaOperation(fixture.ctx, actor, operation.ID, operation.Revision)
				if err != nil {
					t.Fatal(err)
				}
				revision = operation.Revision
				want = ErrMediaOperationState
			}
			if _, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, operation.ID, revision, edits); !errors.Is(err, want) {
				t.Fatalf("rejected batch returned %v, want %v", err, want)
			}
			assertMediaOperationReviewBatchUnchanged(t, fixture, actor, operation, cues)
			if count := mediaOperationReviewBatchUpdateCount(t, fixture); count != 0 {
				t.Fatalf("invalid review executed %d cue UPDATE statements", count)
			}
		})
	}
}

func TestMediaOperationReviewBatchAffectedRowMismatchRollsBack(t *testing.T) {
	fixture, actor, operation, cues := mediaOperationReviewBatchFixture(t, 201)
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION skip_media_review_batch_row() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF OLD.ordinal=199 THEN RETURN NULL; END IF; RETURN NEW; END $$;
		CREATE TRIGGER skip_media_review_batch_row BEFORE UPDATE ON media_operation_cues
		FOR EACH ROW EXECUTE FUNCTION skip_media_review_batch_row()`); err != nil {
		t.Fatal(err)
	}
	edits := mediaOperationReviewBatchEdits(cues, 200)
	if _, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, operation.ID, operation.Revision, edits); !errors.Is(err, ErrMediaOperationState) {
		t.Fatalf("a suppressed cue update returned %v, want a state error", err)
	}
	assertMediaOperationReviewBatchUnchanged(t, fixture, actor, operation, cues)
	if count := mediaOperationReviewBatchUpdateCount(t, fixture); count != 1 {
		t.Fatalf("failed batch executed %d cue UPDATE statements, want one", count)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DROP TRIGGER skip_media_review_batch_row ON media_operation_cues`); err != nil {
		t.Fatal(err)
	}
	if retried, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, operation.ID, operation.Revision, edits); err != nil || retried.Revision != operation.Revision+1 {
		t.Fatalf("rolled-back batch could not retry its original revision: %v", err)
	}
}

func TestMediaOperationReviewBatchConcurrentRevisionAdmitsOneWriter(t *testing.T) {
	fixture, actor, operation, cues := mediaOperationReviewBatchFixture(t, 2)
	edits := mediaOperationReviewBatchEdits(cues, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, operation.ID, operation.Revision, edits)
			results <- err
		}()
	}
	close(start)
	accepted, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrMediaOperationConflict) {
			conflicts++
		} else {
			t.Fatalf("concurrent review failed outside its revision boundary: %v", err)
		}
	}
	current, err := fixture.store.GetMediaOperation(fixture.ctx, actor, operation.ID)
	if err != nil || accepted != 1 || conflicts != 1 || current.Revision != operation.Revision+1 || mediaOperationReviewBatchUpdateCount(t, fixture) != 1 {
		t.Fatalf("concurrent reviews did not admit exactly one revision: accepted=%d conflicts=%d error=%v", accepted, conflicts, err)
	}
}

func TestMediaOperationReviewBatchDeferredCommitFailureRollsBack(t *testing.T) {
	fixture, actor, operation, cues := mediaOperationReviewBatchFixture(t, 201)
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION reject_media_review_batch_commit() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'fixture review commit rejection'; END $$;
		CREATE CONSTRAINT TRIGGER reject_media_review_batch_commit AFTER UPDATE ON media_operations
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.revision<>OLD.revision)
		EXECUTE FUNCTION reject_media_review_batch_commit()`); err != nil {
		t.Fatal(err)
	}
	edits := mediaOperationReviewBatchEdits(cues, 200)
	if _, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, operation.ID, operation.Revision, edits); err == nil {
		t.Fatal("deferred review commit rejection was ignored")
	}
	assertMediaOperationReviewBatchUnchanged(t, fixture, actor, operation, cues)
	if count := mediaOperationReviewBatchUpdateCount(t, fixture); count != 1 {
		t.Fatalf("rejected commit executed %d cue UPDATE statements, want one", count)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `DROP TRIGGER reject_media_review_batch_commit ON media_operations`); err != nil {
		t.Fatal(err)
	}
	if retried, err := fixture.store.UpdateMediaOperationReview(fixture.ctx, actor, operation.ID, operation.Revision, edits); err != nil || retried.Revision != operation.Revision+1 {
		t.Fatalf("commit rollback could not retry its original revision: %v", err)
	}
}
