package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/systemevents"
)

type onlineImagesScheduleFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	owner      *library.Store
	store      *Store
	actor      Actor
	definition Definition
}

func newOnlineImagesScheduleFixture(t *testing.T, extra ...ExecutorRegistration) *onlineImagesScheduleFixture {
	t.Helper()
	ctx, pool, owner, _, actor, _ := taskRepository(t, 0)
	registrations := append([]ExecutorRegistration{{Key: library.TaskOnlineImagesKey, Name: "Fill missing online images", Category: "Images", Executor: libraryBatchTestExecutor{}}}, extra...)
	registry, err := NewExecutorRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	store, err := New(pool, owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	definition, err := store.GetByKey(ctx, library.TaskOnlineImagesKey)
	if err != nil {
		t.Fatal(err)
	}
	return &onlineImagesScheduleFixture{ctx: ctx, pool: pool, owner: owner, store: store, actor: actor, definition: definition}
}

func (f *onlineImagesScheduleFixture) addLibrary(t *testing.T, id, collectionType string, enabled bool) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES($1,$1,$2)`, id, collectionType); err != nil {
		t.Fatal(err)
	}
	if enabled {
		if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableOnlineImages":true}'::jsonb WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *onlineImagesScheduleFixture) addItem(t *testing.T, id, libraryID, parentID, itemType, tmdbID string) {
	t.Helper()
	providerIDs := map[string]string{}
	if tmdbID != "" {
		providerIDs["Tmdb"] = tmdbID
	}
	source, err := json.Marshal(map[string]any{"ProviderIDs": providerIDs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items
		(id,library_id,parent_id,name,sort_name,type,is_folder,index_number,parent_index_number,local_metadata)
		VALUES($1,$2,NULLIF($3,''),$1,$1,$4,$5,1,1,$6::jsonb)`, id, libraryID, parentID, itemType,
		itemType == "Series" || itemType == "Season" || itemType == "MusicAlbum", source); err != nil {
		t.Fatal(err)
	}
}

func (f *onlineImagesScheduleFixture) maskImageTypes(t *testing.T, itemID string, imageTypes ...string) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO artwork_state(item_id,revision,managed_types) VALUES($1,1,$2)`, itemID, imageTypes); err != nil {
		t.Fatal(err)
	}
}

func (f *onlineImagesScheduleFixture) addProviderImages(t *testing.T, itemID string, imageTypes ...string) {
	t.Helper()
	for _, imageType := range imageTypes {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO item_provider_images
			(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
			VALUES($1,$2,0,'tmdb','123','fixture-image',$3,'image/png',1,1,$4)`, itemID, imageType,
			[]byte{1}, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *onlineImagesScheduleFixture) addLocalImages(t *testing.T, libraryID, itemID string, imageTypes ...string) {
	t.Helper()
	rootID := libraryID + "-root"
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES($1,$2,'/fixture/'||$2,'/fixture',$2) ON CONFLICT(id) DO NOTHING`, rootID, libraryID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET root_id=$2,relative_path=$1||'.mkv' WHERE id=$1`, itemID, rootID); err != nil {
		t.Fatal(err)
	}
	for _, imageType := range imageTypes {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO item_images
			(item_id,root_id,image_type,image_index,relative_path,file_identity,source_hash,file_size,modified_at,width,height,mime_type)
			VALUES($1,$2,$3,0,$3||'.png','fixture-image',$4,1,clock_timestamp(),1,1,'image/png')`, itemID, rootID, imageType, strings.Repeat("b", 64)); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *onlineImagesScheduleFixture) childLibraries(t *testing.T, run Run) []string {
	t.Helper()
	children, err := f.store.ListChildren(f.ctx, run.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(children.Items))
	for _, child := range children.Items {
		ids = append(ids, child.LibraryID)
	}
	sort.Strings(ids)
	return ids
}

func (f *onlineImagesScheduleFixture) request(t *testing.T, count int) {
	t.Helper()
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		for range count {
			if err := systemevents.Record(tx.Exec, systemevents.OnlineImagesRequested); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineImagesDefaultSchedulePreservesAdministratorChoices(t *testing.T) {
	f := newOnlineImagesScheduleFixture(t)
	definition := f.definition
	if definition.Revision != 2 || len(definition.Triggers) != 2 {
		t.Fatalf("default image schedule was not installed exactly once: %+v", definition)
	}
	interval, event := definition.Triggers[0], definition.Triggers[1]
	if interval.Kind != string(ScheduleInterval) || interval.IntervalTicks == nil ||
		*interval.IntervalTicks != 86400*ScheduleTicksPerSecond || interval.AnchorAt == nil || interval.NextFireAt == nil ||
		interval.NextFireAt.Sub(*interval.AnchorAt) != 24*time.Hour || interval.MaxRuntimeTicks == nil ||
		*interval.MaxRuntimeTicks != 1800*ScheduleTicksPerSecond || event.Kind != string(ScheduleSystemEvent) ||
		event.SystemEvent == nil || *event.SystemEvent != string(systemevents.OnlineImagesRequested) ||
		event.LastEventSequence != 0 || event.MaxRuntimeTicks == nil || *event.MaxRuntimeTicks != 1800*ScheduleTicksPerSecond {
		t.Fatalf("image schedule lost its bounded daily/event policy: %+v", definition.Triggers)
	}
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := f.store.Get(f.ctx, definition.ID); err != nil || !reflect.DeepEqual(after, definition) {
		t.Fatalf("reconciliation rewrote the installed image schedule: %+v %v", after, err)
	}
	f.addLibrary(t, "default-library", "movies", false)
	var optedIn bool
	if err := f.pool.QueryRow(f.ctx, `SELECT options->'EnableOnlineImages'='true'::jsonb FROM libraries WHERE id='default-library'`).Scan(&optedIn); err != nil || optedIn {
		t.Fatalf("task installation opted a library into provider requests: %t %v", optedIn, err)
	}
	for _, scenario := range []string{"custom", "cleared", "retained_empty_choice", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOnlineImagesScheduleFixture(t)
			definition := f.definition
			switch scenario {
			case "custom", "cleared":
				rules := []ScheduleRule{}
				if scenario == "custom" {
					rules = append(rules, ScheduleRule{Kind: ScheduleStartup})
				}
				if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID,
					Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: rules}); err != nil {
					t.Fatal(err)
				}
			case "retained_empty_choice", "disabled":
				if _, err := f.pool.Exec(f.ctx, `DELETE FROM task_triggers WHERE task_id=$1`, definition.ID); err != nil {
					t.Fatal(err)
				}
				if scenario == "disabled" {
					if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=false,revision=1 WHERE id=$1`, definition.ID); err != nil {
						t.Fatal(err)
					}
				}
			}
			before, err := f.store.Get(f.ctx, definition.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.store.Reconcile(f.ctx); err != nil {
				t.Fatal(err)
			}
			if after, err := f.store.Get(f.ctx, definition.ID); err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("image schedule replaced an administrator decision: %+v %v", after, err)
			}
		})
	}
}

func TestOnlineImagesAdmissionRequiresOptInKnownIdentityAndMissingUnmanagedType(t *testing.T) {
	f := newOnlineImagesScheduleFixture(t)
	for _, fixture := range []struct {
		id, collectionType string
		enabled            bool
	}{
		{"eligible-movie", "movies", true}, {"eligible-inherited-episode", "tvshows", true},
		{"eligible-one-missing", "mixed", true}, {"disabled", "movies", false},
		{"unknown-identity", "movies", true}, {"complete-local", "movies", true},
		{"complete-provider", "movies", true}, {"managed-empty", "movies", true},
		{"unsupported-item", "mixed", true}, {"orphan-episode", "tvshows", true},
		{"empty", "movies", true}, {"music", "music", false}, {"legacy-null-identities", "tvshows", true},
	} {
		f.addLibrary(t, fixture.id, fixture.collectionType, fixture.enabled)
	}
	f.addItem(t, "eligible-movie-item", "eligible-movie", "", "Movie", "123")
	f.addItem(t, "eligible-series", "eligible-inherited-episode", "", "Series", "456")
	f.maskImageTypes(t, "eligible-series", "Primary", "Backdrop", "Logo")
	f.addItem(t, "eligible-season", "eligible-inherited-episode", "eligible-series", "Season", "")
	f.maskImageTypes(t, "eligible-season", "Primary")
	f.addItem(t, "eligible-episode", "eligible-inherited-episode", "eligible-season", "Episode", "")
	f.addItem(t, "one-missing-item", "eligible-one-missing", "", "Movie", "789")
	f.addProviderImages(t, "one-missing-item", "Primary", "Backdrop")
	f.addItem(t, "disabled-item", "disabled", "", "Movie", "123")
	f.addItem(t, "unknown-item", "unknown-identity", "", "Movie", "")
	f.addItem(t, "local-item", "complete-local", "", "Movie", "123")
	f.addLocalImages(t, "complete-local", "local-item", "Primary", "Backdrop", "Logo")
	f.addItem(t, "provider-item", "complete-provider", "", "Movie", "123")
	f.addProviderImages(t, "provider-item", "Primary", "Backdrop", "Logo")
	f.addItem(t, "deleted-images-item", "managed-empty", "", "Movie", "123")
	f.maskImageTypes(t, "deleted-images-item", "Primary", "Backdrop", "Logo")
	f.addItem(t, "audio-item", "unsupported-item", "", "Audio", "123")
	f.addItem(t, "orphan-episode-item", "orphan-episode", "", "Episode", "")
	f.addItem(t, "music-item", "music", "", "MusicAlbum", "123")
	f.addItem(t, "null-peer", "eligible-movie", "", "Movie", "")
	f.addItem(t, "null-movie", "legacy-null-identities", "", "Movie", "")
	f.addItem(t, "null-series", "legacy-null-identities", "", "Series", "")
	f.addItem(t, "null-season", "legacy-null-identities", "null-series", "Season", "")
	f.addItem(t, "null-episode", "legacy-null-identities", "null-season", "Episode", "")
	// Retained source JSON can represent an unknown provider map as JSON null.
	// It must neither invent a match nor abort valid work in the same or another library.
	if _, err := f.pool.Exec(f.ctx, `UPDATE item_metadata_state SET effective='{"ProviderIDs":null}'::jsonb
		WHERE item_id IN ('null-peer','null-movie','null-series','null-season','null-episode')`); err != nil {
		t.Fatal(err)
	}
	admission, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{"eligible-inherited-episode", "eligible-movie", "eligible-one-missing"}
	if ids := f.childLibraries(t, admission.Run); !reflect.DeepEqual(ids, wanted) || admission.Run.TotalChildren != int64(len(wanted)) {
		t.Fatalf("image admission included disabled, identified-as-missing, or manually suppressed artwork: children=%v run=%+v", ids, admission.Run)
	}
	var downloaded int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_provider_images`).Scan(&downloaded); err != nil || downloaded != 5 {
		t.Fatalf("admission changed stored image content: %d %v", downloaded, err)
	}
}

func TestOnlineImagesEventDeferralRetainsCursorAndIncludesLaterOptInExactlyOnce(t *testing.T) {
	f := newOnlineImagesScheduleFixture(t)
	f.addLibrary(t, "early-library", "movies", true)
	f.addItem(t, "early-item", "early-library", "", "Movie", "123")
	f.addLibrary(t, "later-library", "movies", false)
	f.addItem(t, "later-item", "later-library", "", "Movie", "456")
	admission, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.store.BeginRun(f.ctx, admission.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ids := f.childLibraries(t, run); !reflect.DeepEqual(ids, []string{"early-library"}) {
		t.Fatalf("initial image run included a library without opt-in: %v", ids)
	}
	f.request(t, 1)
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		if _, err := tx.Exec(`UPDATE libraries SET options=options||'{"EnableOnlineImages":true}'::jsonb,
			revision=revision+1 WHERE id='later-library'`); err != nil {
			return err
		}
		return systemevents.Record(tx.Exec, systemevents.OnlineImagesRequested)
	}); err != nil {
		t.Fatal(err)
	}
	eventTrigger := f.definition.Triggers[1]
	var before string
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(t)::text FROM task_triggers t WHERE id=$1`, eventTrigger.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	changed, err := f.store.DispatchSystemEvents(f.ctx, 10)
	var deferred *AnalysisDeferrals
	if changed || !errors.As(err, &deferred) || len(deferred.Items) != 1 || deferred.Items[0].TaskKey != library.TaskOnlineImagesKey {
		t.Fatalf("active image run swallowed a newer opt-in or poisoned event dispatch: changed=%t error=%v", changed, err)
	}
	var after string
	var receipts, occurrences int
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(t)::text,
		(SELECT count(*) FROM task_system_event_receipts WHERE task_id=$2),
		(SELECT count(*) FROM task_occurrences WHERE task_id=$2)
		FROM task_triggers t WHERE id=$1`, eventTrigger.ID, f.definition.ID).Scan(&after, &receipts, &occurrences); err != nil ||
		after != before || receipts != 0 || occurrences != 0 {
		t.Fatalf("deferred image request consumed its cursor or invented a receipt: receipts=%d occurrences=%d error=%v", receipts, occurrences, err)
	}
	children, err := f.store.ListChildren(f.ctx, run.ID, Page{})
	if err != nil || len(children.Items) != 1 {
		t.Fatalf("missing original image child: %+v %v", children, err)
	}
	token, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.claimExecution(f.ctx, run.ID, children.Items[0].ID, token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.executionWork(f.ctx, run, children.Items[0], token); err != nil {
		t.Fatal(err)
	}
	if err := f.store.finishExecution(f.ctx, run.ID, children.Items[0].ID, token, nil, false); err != nil {
		t.Fatal(err)
	}
	if completed, err := f.store.GetRun(f.ctx, run.ID); err != nil || completed.State != RunCompleted {
		t.Fatalf("original image run did not finish normally: %+v %v", completed, err)
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || !changed {
		t.Fatalf("retained image request did not resume: %t %v", changed, err)
	}
	current, err := f.store.Get(f.ctx, f.definition.ID)
	if err != nil || current.CurrentRun == nil || current.CurrentRun.ID == run.ID || current.CurrentRun.Source != "system_event" || current.CurrentRun.ActorKind != "system" {
		t.Fatalf("retained image event reused an old run or lost its authority: %+v %v", current, err)
	}
	if ids := f.childLibraries(t, *current.CurrentRun); !reflect.DeepEqual(ids, []string{"early-library", "later-library"}) {
		t.Fatalf("retained image event lost the newly opted-in library: %v", ids)
	}
	var first, last, sequence, cursor int64
	var disposition string
	if err := f.pool.QueryRow(f.ctx, `SELECT r.first_sequence,r.last_sequence,e.sequence,t.last_event_sequence,r.disposition
		FROM task_system_event_receipts r JOIN task_system_events e ON e.name=r.system_event
		JOIN task_triggers t ON t.id=r.trigger_id WHERE r.task_id=$1`, f.definition.ID).
		Scan(&first, &last, &sequence, &cursor, &disposition); err != nil || first != 1 || last != 2 || sequence != 2 || cursor != 2 || disposition != "admitted" {
		t.Fatalf("image event did not preserve its coalesced signal range: first=%d last=%d sequence=%d cursor=%d disposition=%s error=%v", first, last, sequence, cursor, disposition, err)
	}
	if changed, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil || changed {
		t.Fatalf("image event replayed its consumed range: %t %v", changed, err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_system_event_receipts WHERE task_id=$1`, f.definition.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("image event acquired more than one durable receipt: %d %v", receipts, err)
	}
}

func TestOnlineImagesClaimsDoNotOccupyMediaAnalysisConcurrencyGroup(t *testing.T) {
	if isMediaAnalysisExecution(library.TaskOnlineImagesKey) || isAnalysisTask(library.TaskOnlineImagesKey) {
		t.Fatal("image download was assigned an expensive media-analysis concurrency group")
	}
	for _, key := range mediaAnalysisExecutionKeys() {
		if key == library.TaskOnlineImagesKey {
			t.Fatal("media-analysis rotation retained an image-download task")
		}
	}
	for _, imageFirst := range []bool{false, true} {
		name := "media-first"
		if imageFirst {
			name = "images-first"
		}
		t.Run(name, func(t *testing.T) {
			f := newOnlineImagesScheduleFixture(t, ExecutorRegistration{Key: library.TaskBackgroundPreviewGenerationKey,
				Name: "Generate background clips", Category: "Media analysis", Executor: libraryBatchTestExecutor{}})
			f.addLibrary(t, "shared-library", "movies", true)
			f.addItem(t, "shared-item", "shared-library", "", "Movie", "123")
			if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableBackgroundPreviewGeneration":true}'::jsonb WHERE id='shared-library'`); err != nil {
				t.Fatal(err)
			}
			keys := []string{library.TaskBackgroundPreviewGenerationKey, library.TaskOnlineImagesKey}
			if imageFirst {
				keys[0], keys[1] = keys[1], keys[0]
			}
			type claim struct {
				run   Run
				child Child
				token string
			}
			claims := make([]claim, 0, 2)
			for _, key := range keys {
				definition, err := f.store.GetByKey(f.ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				admission, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: definition.ID})
				if err != nil {
					t.Fatal(err)
				}
				run, err := f.store.BeginRun(f.ctx, admission.Run.ID)
				if err != nil {
					t.Fatal(err)
				}
				children, err := f.store.ListChildren(f.ctx, run.ID, Page{})
				if err != nil || len(children.Items) != 1 {
					t.Fatalf("missing concurrency witness for %s: %+v %v", key, children, err)
				}
				token, err := randomID()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.claimExecution(f.ctx, run.ID, children.Items[0].ID, token); err != nil {
					t.Fatalf("image work blocked an independent media-analysis claim, or the reverse: key=%s error=%v", key, err)
				}
				claims = append(claims, claim{run: run, child: children.Items[0], token: token})
			}
			var running int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM task_run_children WHERE state='running' AND executor_token IS NOT NULL`).Scan(&running); err != nil || running != 2 {
				t.Fatalf("independent claims were not simultaneously active: count=%d error=%v", running, err)
			}
			for _, claimed := range claims {
				if err := f.store.finishExecution(f.ctx, claimed.run.ID, claimed.child.ID, claimed.token, nil, false); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
