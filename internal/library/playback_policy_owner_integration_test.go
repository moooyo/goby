//go:build linux

package library

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

func TestPlaybackSourcePlanReusesCurrentPrivateThemeOwnerUser(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	const ownerID = "playback-policy-box"
	users := []string{"playback-policy-owner", "playback-policy-shared", "playback-policy-denied"}
	for _, userID := range users {
		libraryIntegrationUser(t, fixture.ctx, fixture.pool, userID, false, false, []string{fixture.library.ID})
	}
	exec := func(statement string, arguments ...any) {
		t.Helper()
		if _, err := fixture.pool.Exec(fixture.ctx, statement, arguments...); err != nil {
			t.Fatalf("prepare the private theme owner: %v", err)
		}
	}
	// Keep the scanned source's complete file and probe snapshot. Only its
	// attachment role changes, so an allowed read still exercises completion.
	exec(`INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT $1,i.library_id,i.root_id,i.parent_id,$1,$1,'BoxSet',true,r.path||'/Policy owner','Policy owner'
		FROM items i JOIN library_roots r ON r.id=i.root_id WHERE i.id=$2`, ownerID, fixture.item.ID)
	exec(`INSERT INTO media_collections(item_id,owner_id,kind) VALUES($1,$2,'BoxSet')`, ownerID, users[0])
	exec(`INSERT INTO media_collection_shares(collection_id,user_id) VALUES($1,$2)`, ownerID, users[1])
	exec(`UPDATE items SET type='Video',parent_id=$2 WHERE id=$1`, fixture.item.ID, ownerID)
	exec(`INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id=$1`, fixture.item.ID)
	exec(`INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
		VALUES($1,$2,'video',true)`, fixture.item.ID, ownerID)
	baseline, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	expected, readErr := readIndexedPlaybackMedia(fixture.ctx, baseline, unrestrictedLibraryAccess(), fixture.item.ID, media.SourceID(fixture.item.ID), false)
	closeErr := baseline.Rollback(fixture.ctx)
	if readErr != nil || closeErr != nil || expected.mediaFile.Item.Type != "Video" {
		t.Fatalf("read the complete private theme source baseline: read=%v rollback=%v", readErr, closeErr)
	}
	baseAccess, err := parseLibraryPolicy([]byte(`{"EnableAllFolders":false}`))
	if err != nil {
		t.Fatal(err)
	}
	baseAccess.folders = []string{fixture.library.ID}
	for _, planPolicy := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(planPolicy, func(t *testing.T) {
			trace := &playbackSourcePlanTrace{}
			configuration := fixture.pool.Config().ConnConfig.Copy()
			configuration.DefaultQueryExecMode, configuration.StatementCacheCapacity = pgx.QueryExecModeCacheDescribe, 1
			configuration.Tracer = trace
			conn, err := pgx.ConnectConfig(fixture.ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(fixture.ctx)
			if _, err := conn.Exec(fixture.ctx, "SELECT set_config('plan_cache_mode',$1,false)", pgx.QueryExecModeExec, planPolicy); err != nil {
				t.Fatal(err)
			}
			var statement string
			var previous playbackSourcePlanObservation
			for index, userIndex := range []int{0, 2, 1, 0, 1, 2} {
				access := baseAccess
				access.userID = users[userIndex]
				snapshot, err := playbackSourcePlanRead(fixture.ctx, conn, access, fixture.item.ID, media.SourceID(fixture.item.ID))
				if userIndex == 2 {
					if !errors.Is(err, ErrNotFound) || !reflect.DeepEqual(snapshot, indexedMediaSource{}) {
						t.Fatalf("a reused source plan bypassed the private owner for %q: %v", access.userID, err)
					}
				} else if err != nil || !reflect.DeepEqual(snapshot, expected) {
					t.Fatalf("a reused source plan lost the owner's or shared user's complete source for %q: %v", access.userID, err)
				}
				trace.mu.Lock()
				actualSQL := trace.statement
				actualArgs := append([]any(nil), trace.sourceArgs...)
				lockReads, sourceReads, batches := trace.lockReads, trace.sourceReads, trace.batches
				trace.mu.Unlock()
				if index == 0 {
					statement = actualSQL
				}
				wantArgs := []any{pgx.QueryExecModeCacheStatement, fixture.item.ID, false, []string{fixture.library.ID}, access.userID}
				if statement == "" || actualSQL != statement || !reflect.DeepEqual(actualArgs, wantArgs) ||
					lockReads != index+1 || sourceReads != index+1 || batches != 0 {
					t.Fatal("changing the owner user changed the source SQL shape or failed to bind its current value")
				}
				for _, userID := range users {
					if strings.Contains(actualSQL, userID) {
						t.Fatal("the source statement retained a literal owner user")
					}
				}
				plan := playbackSourcePlanObserve(t, fixture.ctx, conn, statement)
				if plan.count != 1 || plan.total != 1 || plan.name == "" || plan.parameters != "{text,boolean,text[],text}" ||
					index > 0 && (plan.name != previous.name || plan.pid != previous.pid) ||
					plan.genericPlans+plan.customPlans != int64(index+1) {
					t.Fatalf("owner changes did not reuse one source plan: previous=%+v current=%+v", previous, plan)
				}
				if planPolicy == "force_custom_plan" && plan.genericPlans != 0 || planPolicy == "force_generic_plan" && plan.customPlans != 0 {
					t.Fatalf("the source did not exercise the requested plan policy: %+v", plan)
				}
				previous = plan
			}
		})
	}
}

func TestPlaybackSourcePlanReadsFreshClassificationAfterShareLockWait(t *testing.T) {
	for _, configuration := range []struct {
		name     string
		capacity int
	}{
		{name: "named", capacity: 1},
		{name: "batch", capacity: 0},
	} {
		for _, finish := range []string{"commit", "rollback"} {
			t.Run(configuration.name+"/"+finish, func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				connectionConfig := fixture.pool.Config().ConnConfig.Copy()
				connectionConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
				connectionConfig.StatementCacheCapacity = configuration.capacity
				if connectionConfig.RuntimeParams == nil {
					connectionConfig.RuntimeParams = make(map[string]string)
				}
				connectionConfig.RuntimeParams["default_transaction_isolation"] = "read committed"
				conn, err := pgx.ConnectConfig(fixture.ctx, connectionConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close(fixture.ctx)
				access := unrestrictedLibraryAccess()
				expected, err := playbackSourcePlanRead(fixture.ctx, conn, access, fixture.item.ID, media.SourceID(fixture.item.ID))
				if err != nil {
					t.Fatalf("read the original source classification: %v", err)
				}
				operation, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
				defer cancel()
				barrier, err := fixture.pool.Begin(operation)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(barrier)
				var locked string
				if err := barrier.QueryRow(operation, "SELECT id FROM items WHERE id=$1 FOR UPDATE", fixture.item.ID).Scan(&locked); err != nil {
					t.Fatal(err)
				}
				// Do not replace the item tuple: only a fresh statement snapshot can
				// observe the reservation committed in this separate relation.
				if _, err := barrier.Exec(operation, `INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
					SELECT root_id,relative_path,false FROM items WHERE id=$1`, fixture.item.ID); err != nil {
					t.Fatal(err)
				}
				type result struct {
					snapshot indexedMediaSource
					err      error
				}
				results := make(chan result, 1)
				done := make(chan struct{})
				go func() {
					defer close(done)
					snapshot, err := playbackSourcePlanRead(operation, conn, access, fixture.item.ID, media.SourceID(fixture.item.ID))
					results <- result{snapshot: snapshot, err: err}
				}()
				defer func() {
					cancel()
					rollback(barrier)
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("the source classification worker did not release its transaction")
					}
				}()
				waitPlaybackValidationBlock(t, operation, fixture.pool, barrier.Conn().PgConn().PID())
				select {
				case value := <-results:
					t.Fatalf("the source read crossed the held item lock: %v", value.err)
				default:
				}
				if finish == "commit" {
					err = barrier.Commit(operation)
				} else {
					err = barrier.Rollback(operation)
				}
				if err != nil {
					t.Fatal(err)
				}
				var value result
				select {
				case value = <-results:
				case <-operation.Done():
					t.Fatal("the source read did not finish after its classification barrier ended")
				}
				if finish == "commit" {
					if !errors.Is(value.err, ErrNotFound) || !reflect.DeepEqual(value.snapshot, indexedMediaSource{}) {
						t.Fatalf("the source reused visibility from before its SHARE lock wait: %v", value.err)
					}
				} else if value.err != nil || !reflect.DeepEqual(value.snapshot, expected) {
					t.Fatalf("a rolled-back reservation changed the complete source: %v", value.err)
				}
				if conn.PgConn().IsBusy() || conn.PgConn().TxStatus() != 'I' {
					t.Fatal("the source classification read retained a busy connection or transaction")
				}
			})
		}
	}
}
