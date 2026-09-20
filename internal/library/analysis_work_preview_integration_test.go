//go:build linux

package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func analysisWorkPreviewVariants(t *testing.T, work AnalysisWork) []AnalysisPreviewPublication {
	t.Helper()
	if len(work.Sources) != 1 || !work.Sources[0].Target {
		t.Fatal("preview fixture must have exactly one source target")
	}
	source := work.Sources[0]
	interval, err := EffectiveAnalysisPreviewInterval(source.DurationTicks, work.Profile.PreviewIntervalSeconds)
	if err != nil {
		t.Fatal(err)
	}
	count := int((source.DurationTicks-1)/interval + 1)
	values := make([]AnalysisPreviewPublication, 0, len(work.Execution.PreviewWidths))
	for _, width := range work.Execution.PreviewWidths {
		value := AnalysisPreviewPublication{ItemID: source.ItemID, Width: width, Height: width * 9 / 16,
			CacheKey: fmt.Sprintf("%064x", width), Seal: fmt.Sprintf("%064x", width+1000), SHA256: fmt.Sprintf("%064x", width+2000),
			Bytes: 72 + 12*int64(count), FrameCount: count, IntervalTicks: interval, NominalTicks: make([]int64, count), ActualTicks: make([]int64, count)}
		for index := range value.NominalTicks {
			value.NominalTicks[index] = int64(index) * interval
			value.ActualTicks[index] = value.NominalTicks[index]
		}
		values = append(values, value)
	}
	return values
}

// Admit force through the real immutable admission path. Updating a snapshot's
// force column would bypass the contract this test is meant to exercise.
func (f analysisWorkFixture) admitPreviewSelection(t *testing.T, selection AnalysisSelection) (string, string) {
	t.Helper()
	runID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	var child string
	err = f.store.WithOwnedTx(f.ctx, func(tx OwnedTx) error {
		binding, err := PrepareAnalysis(tx, TaskPreviewGenerationKey, selection, analysisAdmissionTestPreviewExecution())
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_definitions(id,key,name) VALUES($1,$2,'Preview selection fixture') ON CONFLICT(key) DO UPDATE SET name=EXCLUDED.name`, taskID, TaskPreviewGenerationKey); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO task_runs(id,task_id,state,source,actor_user_id,actor_session_id,actor_kind,task_key,task_name,analysis_input,analysis_config_fingerprint)
			VALUES($1,(SELECT id FROM task_definitions WHERE key=$2),'pending','manual',$3,$4,'admin',$2,'Preview selection fixture',$5,$6)`, runID, TaskPreviewGenerationKey, f.actor.User.ID, f.actor.SessionID, input, binding.ConfigurationFingerprint); err != nil {
			return err
		}
		if err := binding.Bind(tx, runID); err != nil {
			return err
		}
		count, err := binding.SnapshotChildren(tx, runID)
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("preview selection fixture admitted %d children", count)
		}
		return tx.QueryRow(`SELECT id FROM task_run_children WHERE run_id=$1`, runID).Scan(&child)
	})
	if err != nil {
		t.Fatal(err)
	}
	return runID, child
}

func analysisWorkPreviewFixture(t *testing.T) (analysisWorkFixture, string, AnalysisWork, []AnalysisPreviewPublication) {
	t.Helper()
	f := newAnalysisWorkFixture(t, 1)
	runID, children := f.admit(t, TaskPreviewGenerationKey, []string{f.ids[0]})
	if len(children) != 1 {
		t.Fatal("single preview selection did not produce one child")
	}
	child := children[0]
	f.claim(t, runID, child)
	work, err := f.store.GetAnalysisWork(f.ctx, child, f.fence(child))
	if err != nil {
		t.Fatal(err)
	}
	values := analysisWorkPreviewVariants(t, work)
	if err := f.store.PublishAnalysisPreview(f.ctx, child, f.fence(child), values); err != nil {
		t.Fatal(err)
	}
	return f, child, work, values
}

func TestAnalysisWorkPreviewsRequireEveryCurrentVariant(t *testing.T) {
	f := newAnalysisWorkFixture(t, 1)
	runID, children := f.admit(t, TaskPreviewGenerationKey, []string{f.ids[0]})
	if len(children) != 1 {
		t.Fatal("single preview selection did not produce one child")
	}
	child := children[0]
	f.claim(t, runID, child)
	fence := f.fence(child)
	if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, fence); err != nil || values == nil || len(values) != 0 {
		t.Fatalf("absent previews were not a complete cache miss: %d %v", len(values), err)
	}
	work, err := f.store.GetAnalysisWork(f.ctx, child, fence)
	if err != nil {
		t.Fatal(err)
	}
	publications := analysisWorkPreviewVariants(t, work)
	if err := f.store.PublishAnalysisPreview(f.ctx, child, fence, publications); err != nil {
		t.Fatal(err)
	}
	assertComplete := func() {
		t.Helper()
		values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, fence)
		if err != nil || len(values) != len(work.Execution.PreviewWidths) {
			t.Fatalf("complete current previews were not reusable: %d %v", len(values), err)
		}
		for index, value := range values {
			if value.Width != work.Execution.PreviewWidths[index] || value.SourceRevision != work.Sources[0].SourceRevision || value.ProfileFingerprint != work.ConfigurationFingerprint || value.ProfileRevision != work.ConfigurationRevision || value.PublicationEpoch != work.PublicationEpoch ||
				!reflect.DeepEqual(value.NominalTicks, publications[index].NominalTicks) || !reflect.DeepEqual(value.ActualTicks, publications[index].ActualTicks) {
				t.Fatal("reusable preview set mixed source, profile, or timeline facts")
			}
		}
	}
	assertComplete()
	for _, statement := range []string{
		`UPDATE analysis_previews SET source_revision='previous-source' WHERE width=240`,
		`UPDATE analysis_previews SET profile_fingerprint=repeat('e',64) WHERE width=240`,
		`UPDATE analysis_previews SET profile_revision=profile_revision+1 WHERE width=240`,
		`UPDATE analysis_previews SET publication_epoch=publication_epoch+1 WHERE width=240`,
		`DELETE FROM analysis_previews WHERE width=240`,
	} {
		if _, err := f.pool.Exec(f.ctx, statement); err != nil {
			t.Fatal(err)
		}
		if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, fence); err != nil || values == nil || len(values) != 0 {
			t.Fatalf("a partial or differently bound variant set was reused: %d %v", len(values), err)
		}
		if err := f.store.PublishAnalysisPreview(f.ctx, child, fence, publications); err != nil {
			t.Fatal(err)
		}
		assertComplete()
	}
}

func TestAnalysisWorkPreviewsForceSkipsAnOtherwiseReusableSet(t *testing.T) {
	f := newAnalysisWorkFixture(t, 1)
	runID, child := f.admitPreviewSelection(t, AnalysisSelection{LibraryIDs: []string{f.library.ID}, ItemIDs: []string{f.ids[0]}, Force: true})
	f.claim(t, runID, child)
	work, err := f.store.GetAnalysisWork(f.ctx, child, f.fence(child))
	if err != nil || !work.Force {
		t.Fatalf("force was not frozen in admitted work: %v", err)
	}
	if err := f.store.PublishAnalysisPreview(f.ctx, child, f.fence(child), analysisWorkPreviewVariants(t, work)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_previews WHERE item_id=$1`, f.ids[0]).Scan(&count); err != nil || count != 3 {
		t.Fatalf("force fixture omitted a reusable variant: %d %v", count, err)
	}
	if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, f.fence(child)); err != nil || values == nil || len(values) != 0 {
		t.Fatalf("forced work reused prior extraction: %d %v", len(values), err)
	}
}

func TestAnalysisWorkPreviewsDoNotExposePartialResultsAfterErrors(t *testing.T) {
	f, child, work, publications := analysisWorkPreviewFixture(t)
	fence := f.fence(child)
	calls := 0
	lateFailure := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		calls++
		if calls == 2 {
			return ErrForbidden
		}
		return nil
	}
	if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, lateFailure); !errors.Is(err, ErrForbidden) || values != nil || calls != 2 {
		t.Fatalf("final fence failure exposed reusable references: %d calls%d %v", len(values), calls, err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE analysis_previews SET timeline=set_byte(timeline,0,1) WHERE width=320`); err != nil {
		t.Fatal(err)
	}
	if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, fence); !errors.Is(err, ErrUnavailable) || values != nil {
		t.Fatalf("invalid current preview exposed a partial variant set: %d %v", len(values), err)
	}
	if err := f.store.PublishAnalysisPreview(f.ctx, child, fence, publications); err != nil {
		t.Fatal(err)
	}
	// The row is internally well-formed but contradicts the bound profile's
	// effective interval. Source and profile stamps alone must not admit it.
	interval := publications[1].IntervalTicks * 2
	countFrames := int((work.Sources[0].DurationTicks-1)/interval + 1)
	nominal, actual := make([]int64, countFrames), make([]int64, countFrames)
	for index := range nominal {
		nominal[index] = int64(index) * interval
		actual[index] = nominal[index]
	}
	timeline, err := EncodeAnalysisPreviewTimeline(nominal, actual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE analysis_previews SET interval_ticks=$1,frame_count=$2,timeline=$3 WHERE width=320`, interval, countFrames, timeline); err != nil {
		t.Fatal(err)
	}
	if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, fence); !errors.Is(err, ErrUnavailable) || values != nil {
		t.Fatalf("a different effective interval was reused: %d %v", len(values), err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_previews`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("read-only reuse check modified preview references: %d %v", count, err)
	}
}

func TestAnalysisWorkPreviewsHonorSourceConfigurationClearAndClaimFences(t *testing.T) {
	for _, mutation := range []string{"source", "configuration", "clear", "claim"} {
		t.Run(mutation, func(t *testing.T) {
			f, child, work, _ := analysisWorkPreviewFixture(t)
			want := ErrAnalysisConflict
			switch mutation {
			case "source":
				_, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, f.library.ID)
				if err != nil {
					t.Fatal(err)
				}
				want = ErrAnalysisSourceChanged
			case "configuration":
				configuration, err := f.store.GetAnalysisConfiguration(f.ctx, f.actor)
				if err != nil {
					t.Fatal(err)
				}
				configuration.Profile.PreviewQuality++
				if _, err := f.store.UpdateAnalysisConfiguration(f.ctx, f.actor, AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: configuration.Profile}); err != nil {
					t.Fatal(err)
				}
			case "clear":
				if err := f.store.ClearAnalysisPreviews(f.ctx, f.actor, f.ids[0], work.Sources[0].SourceRevision); err != nil {
					t.Fatal(err)
				}
			case "claim":
				if _, err := f.pool.Exec(f.ctx, `UPDATE task_run_children SET executor_token=repeat('2',32) WHERE id=$1`, child); err != nil {
					t.Fatal(err)
				}
				want = ErrForbidden
			}
			if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, f.fence(child)); !errors.Is(err, want) || values != nil {
				t.Fatalf("%s invalidation retained work previews: %d %v", mutation, len(values), err)
			}
		})
	}
}

func TestAnalysisWorkPreviewsRejectIntroTaskAuthority(t *testing.T) {
	f := newAnalysisWorkFixture(t, 3)
	runID, children := f.admit(t, TaskIntroAnalysisKey, []string{f.ids[0]})
	if len(children) != 1 {
		t.Fatal("leaf intro selection did not produce one child")
	}
	child := children[0]
	f.claim(t, runID, child)
	if values, err := f.store.GetAnalysisWorkPreviews(f.ctx, child, f.fence(child)); !errors.Is(err, ErrInvalidInput) || values != nil {
		t.Fatalf("intro task borrowed preview reuse authority: %d %v", len(values), err)
	}
}
