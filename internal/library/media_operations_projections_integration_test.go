//go:build linux

package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mediaOperationProjectionTrace struct {
	mu         sync.Mutex
	statements []string
}

func (trace *mediaOperationProjectionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, " FROM media_operations o ") {
		trace.mu.Lock()
		trace.statements = append(trace.statements, data.SQL)
		trace.mu.Unlock()
	}
	return ctx
}

func (*mediaOperationProjectionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *mediaOperationProjectionTrace) assertNarrow(t *testing.T, count int) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if len(trace.statements) != count {
		t.Fatalf("operation projections issued %d reads, want %d", len(trace.statements), count)
	}
	for _, statement := range trace.statements {
		for _, field := range []string{"source_snapshot", "execution_snapshot", "journal", "request_fingerprint", "apply_fingerprint"} {
			if strings.Contains(statement, field) {
				t.Fatalf("narrow operation projection loaded private field %q", field)
			}
		}
	}
	trace.statements = nil
}

func mediaOperationProjectionReader(t *testing.T, fixture mediaSourceFixture) (*Store, *mediaOperationProjectionTrace) {
	t.Helper()
	trace := &mediaOperationProjectionTrace{}
	configuration := fixture.pool.Config().Copy()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	return &Store{pool: pool}, trace
}

func insertMediaOperationProjection(t *testing.T, fixture mediaSourceFixture, original MediaOperation, kind, state, token string, parameters json.RawMessage) string {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if parameters == nil {
		parameters, err = json.Marshal(original.Parameters)
		if err != nil {
			t.Fatal(err)
		}
	}
	private, err := json.Marshal(map[string]string{"Private": strings.Repeat("x", 128<<10)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.pool.Exec(fixture.ctx, `INSERT INTO media_operations
		(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,
		source_revision,stream_index,parameters,source_snapshot,execution_snapshot,state,worker_token,
		result_summary,result_hash,journal,apply_request_id,apply_fingerprint,apply_revision)
		SELECT $2,$3,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		request_actor_id,request_credential_id,$2,request_fingerprint,media_source_id,
		source_revision,stream_index,$6::jsonb,$7::jsonb,$7::jsonb,$4,$5,
		result_summary,result_hash,$7::jsonb,'retained-apply',request_fingerprint,revision
		FROM media_operations WHERE id=$1`, original.ID, id, kind, state, token, parameters, private)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func assertMediaOperationPublicEquivalent(t *testing.T, full, summary MediaOperation) {
	t.Helper()
	fullJSON, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil || !bytes.Equal(fullJSON, summaryJSON) || full.RootID != summary.RootID || full.WorkerToken != summary.WorkerToken {
		t.Fatalf("summary changed public state or worker-dependent capabilities: %v", err)
	}
	if len(summary.SourceSnapshot) != 0 || len(summary.ExecutionSnapshot) != 0 || len(summary.Journal) != 0 ||
		len(summary.RequestFingerprint) != 0 || len(summary.ApplyFingerprint) != 0 || summary.RequestActor.User.ID != "" || summary.ApplyActor.User.ID != "" {
		t.Fatal("summary retained unused execution evidence or actor credentials")
	}
}

func TestMediaOperationSummaryProjectionsPreservePublicState(t *testing.T) {
	fixture, actor, original := mediaOCRReadyFixture(t)
	reader, trace := mediaOperationProjectionReader(t, fixture)
	for _, kind := range []string{MediaOperationOCR, MediaOperationRemoveSubtitle} {
		for _, state := range []string{"ready", "interrupted", "failed", "stale", "recovery_required"} {
			for _, token := range []string{"", strings.Repeat("a", 32)} {
				id := insertMediaOperationProjection(t, fixture, original, kind, state, token, nil)
				full, err := fixture.store.GetMediaOperation(fixture.ctx, actor, id)
				if err != nil {
					t.Fatal(err)
				}
				if len(full.SourceSnapshot) < 128<<10 || len(full.ExecutionSnapshot) < 128<<10 || len(full.Journal) < 128<<10 ||
					len(full.RequestFingerprint) != 32 || len(full.ApplyFingerprint) != 32 || full.ApplyRequestID != "retained-apply" {
					t.Fatal("the default operation read lost its full-record contract")
				}
				summary, err := reader.GetMediaOperationSummary(fixture.ctx, actor, id)
				if err != nil {
					t.Fatal(err)
				}
				assertMediaOperationPublicEquivalent(t, full, summary)
				trace.assertNarrow(t, 1)
			}
		}
	}
	options := MediaOperationPageOptions{Limit: 200}
	full, err := fixture.store.ListMediaOperations(fixture.ctx, actor, options)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := reader.ListMediaOperationSummaries(fixture.ctx, actor, options)
	if err != nil || len(full.Items) != len(summary.Items) || full.TotalRecordCount != summary.TotalRecordCount {
		t.Fatalf("summary page changed pagination: %v", err)
	}
	for index := range full.Items {
		assertMediaOperationPublicEquivalent(t, full.Items[index], summary.Items[index])
	}
	trace.assertNarrow(t, 1)
	fullReview, err := fixture.store.GetMediaOperationReview(fixture.ctx, actor, original.ID, options)
	if err != nil {
		t.Fatal(err)
	}
	review, err := reader.GetMediaOperationReviewSummary(fixture.ctx, actor, original.ID, options)
	if err != nil || !reflect.DeepEqual(fullReview.Items, review.Items) || fullReview.TotalRecordCount != review.TotalRecordCount {
		t.Fatalf("summary review changed cue evidence or pagination: %v", err)
	}
	assertMediaOperationPublicEquivalent(t, fullReview.Operation, review.Operation)
	trace.assertNarrow(t, 1)
	if _, err = fixture.pool.Exec(fixture.ctx, `UPDATE items SET modified_at=modified_at+interval '1 second' WHERE id=$1`, original.ItemID); err != nil {
		t.Fatal(err)
	}
	current, err := fixture.store.GetMediaOperation(fixture.ctx, actor, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := reader.GetMediaOperationSummary(fixture.ctx, actor, original.ID)
	if err != nil || changed.CanApply || current.CanApply {
		t.Fatalf("summary offered application after the current source changed: %v", err)
	}
	assertMediaOperationPublicEquivalent(t, current, changed)
	trace.assertNarrow(t, 1)
	if _, err = fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = reader.GetMediaOperationSummary(fixture.ctx, actor, original.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("summary accepted revoked administrator authority: %v", err)
	}
}

func TestMediaOperationCandidateIDsValidateWholePage(t *testing.T) {
	fixture, _, original := mediaOCRReadyFixture(t)
	reader, trace := mediaOperationProjectionReader(t, fixture)
	good := insertMediaOperationProjection(t, fixture, original, MediaOperationOCR, "ready", "", nil)
	bad := insertMediaOperationProjection(t, fixture, original, MediaOperationOCR, "ready", "", json.RawMessage(`{"ModelIds":123}`))
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE media_operations SET cancel_requested_at=clock_timestamp(),
		created_at=CASE WHEN id=$1 THEN '2000-01-01'::timestamptz ELSE '2000-01-02'::timestamptz END
		WHERE id=ANY($2::text[])`, good, []string{good, bad}); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func(context.Context, int) ([]string, error){reader.PendingMediaOperationIDs, reader.PendingMediaOperationCancellationIDs} {
		page, err := read(fixture.ctx, 1)
		if err != nil || !reflect.DeepEqual(page, []string{good}) {
			t.Fatalf("candidate ordering changed: %v, %v", page, err)
		}
		page, err = read(fixture.ctx, 2)
		if !errors.Is(err, ErrUnavailable) || page != nil {
			t.Fatalf("corrupt later candidate returned a partial or successful page: %v, %v", page, err)
		}
	}
	trace.assertNarrow(t, 4)
}

func TestMediaOperationStatusPreservesOwnershipAuthorityAndFullCompensationRead(t *testing.T) {
	fixture, actor, original := mediaOCRReadyFixture(t)
	_, work := mediaOCRAdmitApply(t, fixture, actor, original)
	reader, trace := mediaOperationProjectionReader(t, fixture)
	for _, cancelled := range []bool{false, true} {
		if cancelled {
			if _, err := fixture.store.CancelMediaOperation(fixture.ctx, actor, original.ID, work.Operation.Revision); err != nil {
				t.Fatal(err)
			}
		}
		full, err := fixture.store.ReadMediaOperationWork(fixture.ctx, work)
		if err != nil {
			t.Fatal(err)
		}
		status, err := reader.MediaOperationWorkStatus(fixture.ctx, work)
		if err != nil || status.ID != full.ID || status.State != full.State || status.WorkerToken != full.WorkerToken ||
			status.PublicationPhase != full.PublicationPhase || !reflect.DeepEqual(status.CancelRequestedAt, full.CancelRequestedAt) {
			t.Fatalf("status projection changed current worker facts or authority: %v", err)
		}
		if len(full.Journal) == 0 || len(full.SourceSnapshot) == 0 || len(full.ExecutionSnapshot) == 0 {
			t.Fatal("compensation did not retain its complete work record")
		}
	}
	corrupt := work
	corrupt.Operation.ID = insertMediaOperationProjection(t, fixture, original, MediaOperationOCR, "ready", work.Token, json.RawMessage(`{"ModelIds":123}`))
	if _, err := reader.MediaOperationWorkStatus(fixture.ctx, corrupt); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("status accepted corrupt stored parameters: %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.MediaOperationWorkStatus(fixture.ctx, work); !errors.Is(err, ErrForbidden) {
		t.Fatalf("polling accepted revoked execution authority: %v", err)
	}
	work.Discard = true
	if _, err := reader.MediaOperationWorkStatus(fixture.ctx, work); err != nil {
		t.Fatalf("revocation prevented already-owned compensation status: %v", err)
	}
	work.Token = strings.Repeat("b", 32)
	if _, err := reader.MediaOperationWorkStatus(fixture.ctx, work); !errors.Is(err, ErrMediaOperationState) {
		t.Fatalf("polling accepted a replaced worker token: %v", err)
	}
	trace.assertNarrow(t, 6)
}
