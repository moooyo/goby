//go:build linux

package library

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/media"
)

func TestCreditsOperationRetainsRootApprovalAndMarkerIsolation(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	setStoredCreditsPolicy(t, f, true)
	credits, err := f.store.GetItemCredits(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	credits, err = f.store.UpdateItemCredits(f.ctx, f.actor, f.ids[0], CreditsEdit{
		Revision: credits.Revision, SourceRevision: credits.SourceRevision,
		StartTicks: 5000 * media.TicksPerSecond, Provenance: "Manual"}, false)
	if err != nil {
		t.Fatal(err)
	}
	work, fence := admittedStoredCredits(t, f, []string{f.ids[0]})
	ctx, closeOperation, err := f.store.BeginAnalysisOperation(f.ctx, work.ChildID, fence)
	if err != nil {
		t.Fatalf("begin credits operation with an independent manual marker: %v", err)
	}
	t.Cleanup(func() {
		if err := closeOperation(); err != nil {
			t.Errorf("close credits operation: %v", err)
		}
	})
	source, found := FindAnalysisSource(work, f.ids[0])
	if !found || source.ManualRevision != credits.Revision || source.DecisionRevision != "0" {
		t.Fatal("credits operation lost its admitted marker revision")
	}
	intro, err := f.store.GetItemIntro(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateItemIntro(f.ctx, f.actor, f.ids[0], IntroEdit{
		Revision: intro.Revision, SourceRevision: intro.SourceRevision,
		StartTicks: 0, EndTicks: 20 * media.TicksPerSecond, Provenance: "Manual"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, f.library.ID); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.RevalidateAnalysisWork(ctx, work.ChildID, fence)
	if err != nil || !reflect.DeepEqual(current, work) {
		t.Fatalf("credits operation lost its source approval or borrowed intro revisions: %v", err)
	}
	if _, err := f.store.GetAnalysisWork(f.ctx, work.ChildID, fence); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("unscoped caller inherited retained credits authority: %v", err)
	}
	values := storedCreditsWorkValues(t, work, false)
	if err := f.store.PublishCreditsAnalysis(ctx, work.ChildID, fence, values, nil); err != nil {
		t.Fatalf("publish credits through the retained operation: %v", err)
	}
	var stored string
	if err := f.pool.QueryRow(f.ctx, `SELECT source_revision FROM analysis_credits_detections WHERE item_id=$1`, f.ids[0]).Scan(&stored); err != nil || stored != source.SourceRevision {
		t.Fatalf("credits publication replaced its admitted source stamp: %q %v", stored, err)
	}
	credits, err = f.store.GetItemCredits(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateItemCredits(f.ctx, f.actor, f.ids[0], CreditsEdit{
		Revision: credits.Revision, SourceRevision: credits.SourceRevision,
		StartTicks: 5100 * media.TicksPerSecond, Provenance: "Manual"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(ctx, work.ChildID, fence); !errors.Is(err, ErrAnalysisConflict) {
		t.Fatalf("retained approval bypassed a credits marker edit: %v", err)
	}
}

func TestCreditsOperationRejectsNewCohortMember(t *testing.T) {
	f := newAnalysisWorkFixture(t, 4)
	configuration, err := f.store.GetAnalysisConfiguration(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	var originalSize int64
	if err := f.pool.QueryRow(f.ctx, `SELECT file_size FROM items WHERE id=$1`, f.ids[3]).Scan(&originalSize); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET file_size=$2 WHERE id=$1`, f.ids[3], configuration.Profile.MaxSourceBytes+1); err != nil {
		t.Fatal(err)
	}
	work, fence := admittedStoredCredits(t, f, nil)
	if len(work.Sources) != 3 {
		t.Fatalf("ineligible episode entered admitted credits sources: %d", len(work.Sources))
	}
	ctx, closeOperation, err := f.store.BeginAnalysisOperation(f.ctx, work.ChildID, fence)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := closeOperation(); err != nil {
			t.Errorf("close credits operation: %v", err)
		}
	})
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET file_size=$2 WHERE id=$1`, f.ids[3], originalSize); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetAnalysisWork(ctx, work.ChildID, fence); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("new episode inherited the existing credits cohort authority: %v", err)
	}
	if err := f.store.PublishCreditsAnalysis(ctx, work.ChildID, fence, storedCreditsWorkValues(t, work, false), nil); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("changed credits cohort retained publication authority: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_credits_detections`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected credits cohort published detections: %d %v", count, err)
	}
}

func TestCreditsOperationPreservesImmutableTaskIdentity(t *testing.T) {
	f := newAnalysisWorkFixture(t, 2)
	work, fence := admittedStoredCredits(t, f, nil)
	_, err := f.pool.Exec(f.ctx, `UPDATE analysis_work SET task_key=$2 WHERE child_id=$1`, work.ChildID, TaskIntroAnalysisKey)
	var immutable *pgconn.PgError
	if !errors.As(err, &immutable) || immutable.Code != "P0001" || immutable.Message != "analysis admission snapshot is immutable" {
		t.Fatalf("credits admission did not reject a task identity rewrite: %v", err)
	}
	current, err := f.store.GetAnalysisWork(f.ctx, work.ChildID, fence)
	if err != nil || !reflect.DeepEqual(current, work) {
		t.Fatalf("rejected task rewrite damaged the original credits admission: %v", err)
	}
}
