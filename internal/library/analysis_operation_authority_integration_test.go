//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sync"
	"testing"
)

type analysisOperationAuthorityFixture struct {
	analysisWorkFixture
	workContext    context.Context
	childID        string
	work           AnalysisWork
	closeOperation func() error
}

func beginAnalysisOperationAuthorityFixture(t *testing.T, f analysisWorkFixture) analysisOperationAuthorityFixture {
	t.Helper()
	f.setIntroDetection(t, true)
	run, children := f.admit(t, TaskIntroAnalysisKey, nil)
	if len(children) != 1 {
		t.Fatalf("expected one analysis child, got %d", len(children))
	}
	child := children[0]
	f.claim(t, run, child)
	ctx, closeGrant, err := f.store.BeginAnalysisOperation(f.ctx, child, f.fence(child))
	if err != nil {
		t.Fatalf("begin claimed analysis operation: %v", err)
	}
	if ctx == nil || closeGrant == nil {
		t.Fatal("analysis operation omitted its context or cleanup")
	}
	var once sync.Once
	var closeErr error
	closeOperation := func() error {
		once.Do(func() { closeErr = closeGrant() })
		return closeErr
	}
	t.Cleanup(func() {
		if err := closeOperation(); err != nil {
			t.Errorf("close analysis operation: %v", err)
		}
	})
	work, err := f.store.GetAnalysisWork(ctx, child, f.fence(child))
	if err != nil {
		t.Fatalf("read authorized analysis work: %v", err)
	}
	return analysisOperationAuthorityFixture{f, ctx, child, work, closeOperation}
}

func analysisOperationAuthorityAssertRead(t *testing.T, f analysisOperationAuthorityFixture) {
	t.Helper()
	file, source, err := f.store.OpenAnalysisSource(f.workContext, f.childID, f.ids[0], f.fence(f.childID))
	if err != nil {
		t.Fatalf("open authorized analysis source: %v", err)
	}
	defer file.Close()
	if source.Item.ID != f.ids[0] {
		t.Fatal("analysis operation opened a different item")
	}
	read, err := f.store.PrepareMediaSourceIO(f.workContext, source)
	if err != nil {
		t.Fatalf("prepare authorized analysis source I/O: %v", err)
	}
	defer read.Close()
	var contents []byte
	if err := read.Run(f.workContext, func(context.Context) error {
		var readErr error
		contents, readErr = io.ReadAll(file)
		return readErr
	}); err != nil {
		t.Fatalf("read authorized analysis source: %v", err)
	}
	if string(contents) != "video:independent-analysis-episode-0" {
		t.Fatalf("analysis operation read unexpected content: %q", contents)
	}
}

func analysisOperationAuthorityAssertUnpublished(t *testing.T, f analysisWorkFixture) {
	t.Helper()
	var detections, audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_detections),
		(SELECT count(*) FROM analysis_intro_audit)`).Scan(&detections, &audits); err != nil {
		t.Fatal(err)
	}
	if detections != 0 || audits != 0 {
		t.Fatalf("rejected operation committed analysis state: detections=%d audits=%d", detections, audits)
	}
}

func analysisOperationAuthorityAssertPublication(t *testing.T, f analysisOperationAuthorityFixture) {
	t.Helper()
	result := analysisFixtureQualifiedResult(t, f.analysisWorkFixture, f.work)
	fence := f.fence(f.childID)
	rejection := errors.New("publication final fence rejected")
	lateFence := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		var written bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM analysis_detections)`).Scan(&written); err != nil {
			return err
		}
		if written {
			return rejection
		}
		return nil
	}
	if err := f.store.PublishIntroSkipperAnalysis(f.workContext, f.childID, lateFence, result); !errors.Is(err, rejection) {
		t.Fatalf("authorized publication bypassed its final fence: %v", err)
	}
	analysisOperationAuthorityAssertUnpublished(t, f.analysisWorkFixture)
	if err := f.store.PublishIntroSkipperAnalysis(f.workContext, f.childID, fence, result); err != nil {
		t.Fatalf("publish authorized analysis result: %v", err)
	}
	var detections, audits int
	var originalSources bool
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*),
		COALESCE(bool_and(d.source_revision=s.source_revision),false),
		(SELECT count(*) FROM analysis_intro_audit)
		FROM analysis_detections d JOIN analysis_work_sources s
		ON s.child_id=d.child_id AND s.item_id=d.item_id WHERE d.child_id=$1`,
		f.childID).Scan(&detections, &originalSources, &audits); err != nil {
		t.Fatal(err)
	}
	if detections != len(f.work.Sources) || audits != detections || !originalSources {
		t.Fatalf("publication lost its admitted source facts: detections=%d audits=%d original=%v", detections, audits, originalSources)
	}
}

func analysisOperationAuthorityAssertBeginRejected(t *testing.T, f analysisWorkFixture, child string) {
	t.Helper()
	ctx, closeOperation, err := f.store.BeginAnalysisOperation(f.ctx, child, f.fence(child))
	if closeOperation != nil {
		if closeErr := closeOperation(); closeErr != nil {
			t.Errorf("close unexpectedly authorized operation: %v", closeErr)
		}
	}
	if err == nil || ctx != nil {
		t.Fatalf("new analysis operation borrowed earlier root authority: context=%v error=%v", ctx != nil, err)
	}
}

func TestAnalysisOperationRetainsStartedRootApproval(t *testing.T) {
	for _, change := range []struct {
		name string
		sql  string
	}{
		{"revision", `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`},
		{"implicit_configured_approval", `UPDATE library_roots SET binding_revision=binding_revision+1,
			storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE library_id=$1`},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := beginAnalysisOperationAuthorityFixture(t, newAnalysisWorkFixture(t, 3))
			if _, err := f.pool.Exec(f.ctx, change.sql, f.library.ID); err != nil {
				t.Fatal(err)
			}
			current, err := f.store.GetAnalysisWork(f.workContext, f.childID, f.fence(f.childID))
			if err != nil || !reflect.DeepEqual(current, f.work) {
				t.Fatalf("started operation lost its admitted work after approval change: %v", err)
			}
			current, err = f.store.RevalidateAnalysisWork(f.workContext, f.childID, f.fence(f.childID))
			if err != nil || !reflect.DeepEqual(current, f.work) {
				t.Fatalf("started operation lost its current physical sources after approval change: %v", err)
			}
			analysisOperationAuthorityAssertRead(t, f)
			analysisOperationAuthorityAssertBeginRejected(t, f.analysisWorkFixture, f.childID)
			if _, err := f.store.GetAnalysisWork(f.ctx, f.childID, f.fence(f.childID)); !errors.Is(err, ErrAnalysisSourceChanged) {
				t.Fatalf("caller without an operation inherited the frozen source revision: %v", err)
			}
			// Both an explicit binding and the legacy configured-root approval
			// permit fresh work. Fresh admission must use the current source stamp.
			freshRun, freshChildren := f.admit(t, TaskPreviewGenerationKey, []string{f.ids[0]})
			if len(freshChildren) != 1 {
				t.Fatal("expected one freshly admitted analysis child")
			}
			fresh := freshChildren[0]
			f.claim(t, freshRun, fresh)
			if _, err := f.store.GetAnalysisWork(f.ctx, fresh, f.fence(fresh)); err != nil {
				t.Fatalf("fresh admission did not capture the current source stamp: %v", err)
			}
			ctx, closeOperation, err := f.store.BeginAnalysisOperation(f.ctx, fresh, f.fence(fresh))
			if err != nil {
				t.Fatalf("current root approval rejected fresh admission: %v", err)
			}
			if ctx == nil || closeOperation == nil {
				t.Fatal("fresh analysis operation omitted its authority or cleanup")
			}
			_, readErr := f.store.GetAnalysisWork(ctx, fresh, f.fence(fresh))
			if err := errors.Join(readErr, closeOperation()); err != nil {
				t.Fatalf("fresh operation did not retain its current approval: %v", err)
			}
			analysisOperationAuthorityAssertPublication(t, f)
		})
	}
}

func TestAnalysisOperationRetainsStartedConfiguredRoot(t *testing.T) {
	f := beginAnalysisOperationAuthorityFixture(t, newAnalysisWorkFixture(t, 3))
	f.store.mu.Lock()
	configured := f.store.roots
	f.store.roots = nil
	f.store.mu.Unlock()
	t.Cleanup(func() {
		f.store.mu.Lock()
		f.store.roots = configured
		f.store.mu.Unlock()
	})
	analysisOperationAuthorityAssertRead(t, f)
	analysisOperationAuthorityAssertBeginRejected(t, f.analysisWorkFixture, f.childID)
	freshRun, freshChildren := f.admit(t, TaskPreviewGenerationKey, []string{f.ids[0]})
	if len(freshChildren) != 1 {
		t.Fatal("expected one freshly admitted analysis child")
	}
	f.claim(t, freshRun, freshChildren[0])
	if _, err := f.store.GetAnalysisWork(f.ctx, freshChildren[0], f.fence(freshChildren[0])); err != nil {
		t.Fatalf("fresh admission did not retain current source facts: %v", err)
	}
	analysisOperationAuthorityAssertBeginRejected(t, f.analysisWorkFixture, freshChildren[0])
	file, _, err := f.store.OpenMediaFor(f.ctx, Subject{UserID: f.viewer}, f.ids[0], "")
	if file != nil {
		_ = file.Close()
	}
	if err == nil || file != nil {
		t.Fatalf("external media read inherited a task's configured root authority: %v", err)
	}
	analysisOperationAuthorityAssertPublication(t, f)
}

func TestAnalysisOperationRetainsSourceAndPublicationGuards(t *testing.T) {
	for _, change := range []struct {
		name   string
		mutate func(*testing.T, analysisOperationAuthorityFixture)
	}{
		{"file", func(t *testing.T, f analysisOperationAuthorityFixture) {
			if err := os.WriteFile(f.paths[0], []byte("video:replacement-analysis-source"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"index", func(t *testing.T, f analysisOperationAuthorityFixture) {
			if _, err := f.pool.Exec(f.ctx, `UPDATE items SET index_number=index_number+10 WHERE id=$1`, f.ids[0]); err != nil {
				t.Fatal(err)
			}
		}},
		{"manual", func(t *testing.T, f analysisOperationAuthorityFixture) {
			source := f.work.Sources[0]
			if _, err := f.store.UpdateItemIntro(f.ctx, f.actor, source.ItemID,
				IntroEdit{Revision: source.ManualRevision, SourceRevision: source.SourceRevision,
					StartTicks: 1, EndTicks: 2, Provenance: "Manual"}, false); err != nil {
				t.Fatal(err)
			}
		}},
		{"root_mapping", func(t *testing.T, f analysisOperationAuthorityFixture) {
			if _, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET path=path || '/Moved',
				relative_path=relative_path || '/Moved' WHERE library_id=$1`, f.library.ID); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			f := beginAnalysisOperationAuthorityFixture(t, newAnalysisWorkFixture(t, 3))
			result := analysisFixtureQualifiedResult(t, f.analysisWorkFixture, f.work)
			change.mutate(t, f)
			if work, err := f.store.RevalidateAnalysisWork(f.workContext, f.childID, f.fence(f.childID)); err == nil || !reflect.DeepEqual(work, AnalysisWork{}) {
				t.Fatalf("%s change retained operation source authority: %v", change.name, err)
			}
			if err := f.store.PublishIntroSkipperAnalysis(f.workContext, f.childID, f.fence(f.childID), result); err == nil {
				t.Fatalf("%s change retained operation publication authority", change.name)
			}
			analysisOperationAuthorityAssertUnpublished(t, f.analysisWorkFixture)
		})
	}
}

func TestAnalysisOperationRejectsNewCohortMember(t *testing.T) {
	base := newAnalysisWorkFixture(t, 4)
	configuration, err := base.store.GetAnalysisConfiguration(base.ctx, base.actor)
	if err != nil {
		t.Fatal(err)
	}
	var originalSize int64
	if err := base.pool.QueryRow(base.ctx, `SELECT file_size FROM items WHERE id=$1`, base.ids[3]).Scan(&originalSize); err != nil {
		t.Fatal(err)
	}
	if _, err := base.pool.Exec(base.ctx, `UPDATE items SET file_size=$2 WHERE id=$1`, base.ids[3], configuration.Profile.MaxSourceBytes+1); err != nil {
		t.Fatal(err)
	}
	f := beginAnalysisOperationAuthorityFixture(t, base)
	if len(f.work.Sources) != 3 {
		t.Fatalf("ineligible cohort member entered admission: %d", len(f.work.Sources))
	}
	if _, present := FindAnalysisSource(f.work, base.ids[3]); present {
		t.Fatal("cohort mutation must affect a source outside the admitted window")
	}
	result := analysisFixtureQualifiedResult(t, base, f.work)
	if _, err := base.pool.Exec(base.ctx, `UPDATE items SET file_size=$2 WHERE id=$1`, base.ids[3], originalSize); err != nil {
		t.Fatal(err)
	}
	if work, err := f.store.GetAnalysisWork(f.workContext, f.childID, f.fence(f.childID)); !errors.Is(err, ErrAnalysisSourceChanged) || !reflect.DeepEqual(work, AnalysisWork{}) {
		t.Fatalf("new cohort member inherited the original cohort's authority: %v", err)
	}
	if err := f.store.PublishIntroSkipperAnalysis(f.workContext, f.childID, f.fence(f.childID), result); !errors.Is(err, ErrAnalysisSourceChanged) {
		t.Fatalf("new cohort member retained old publication authority: %v", err)
	}
	analysisOperationAuthorityAssertUnpublished(t, base)
}

func TestAnalysisOperationContextIsBoundToChildAndLifetime(t *testing.T) {
	f := beginAnalysisOperationAuthorityFixture(t, newAnalysisWorkFixture(t, 3))
	run, children := f.admit(t, TaskPreviewGenerationKey, []string{f.ids[0]})
	if len(children) != 1 {
		t.Fatal("expected one independent analysis child")
	}
	other := children[0]
	f.claim(t, run, other)
	if _, err := f.store.GetAnalysisWork(f.ctx, other, f.fence(other)); err != nil {
		t.Fatalf("independent child lacks a valid durable claim: %v", err)
	}
	assertRejected := func(child string) {
		t.Helper()
		if work, err := f.store.GetAnalysisWork(f.workContext, child, f.fence(child)); err == nil || !reflect.DeepEqual(work, AnalysisWork{}) {
			t.Fatalf("out-of-scope context retained analysis work: %v", err)
		}
		file, _, err := f.store.OpenAnalysisSource(f.workContext, child, f.ids[0], f.fence(child))
		if file != nil {
			_ = file.Close()
		}
		if err == nil || file != nil {
			t.Fatalf("out-of-scope context retained a media descriptor: %v", err)
		}
	}
	assertRejected(other)
	analysisOperationAuthorityAssertRead(t, f)
	if err := f.closeOperation(); err != nil {
		t.Fatal(err)
	}
	assertRejected(f.childID)
	analysisOperationAuthorityAssertUnpublished(t, f.analysisWorkFixture)
}
