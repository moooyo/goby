package tasks

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func taskCompatibilityActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, native Actor) Actor {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(id))
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id,user_id,token_hash,kind,expires_at)
		VALUES ($1,$2,$3,'emby',clock_timestamp()+interval '1 hour')`, id, native.Principal.User.ID, digest[:]); err != nil {
		t.Fatal(err)
	}
	actor := native
	actor.Audience = identity.AdministratorEmby
	actor.Principal.Kind, actor.Principal.SessionID = "emby", id
	return actor
}

func TestTaskCompatibilityStopSerializesConcurrentDefinitionRequests(t *testing.T) {
	for _, started := range []bool{false, true} {
		name := "pending"
		if started {
			name = "running"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, scanner, store, native, definition := taskRepository(t, 1)
			actor := taskCompatibilityActor(t, ctx, pool, native)
			admission, err := store.Start(ctx, native, StartRequest{TaskID: definition.ID})
			if err != nil {
				t.Fatal(err)
			}
			if started {
				if _, err := store.BeginRun(ctx, admission.Run.ID); err != nil {
					t.Fatal(err)
				}
				children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
				if err != nil {
					t.Fatal(err)
				}
				child := children.Items[0]
				if err := scanner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
					if _, err := tx.Exec(`UPDATE task_run_children SET state='running',
						scan_job_id='compatibility-stop-scan',started_at=clock_timestamp() WHERE id=$1`, child.ID); err != nil {
						return err
					}
					_, err := tx.Exec(`INSERT INTO scan_jobs(id,library_id,status,task_child_id,started_at)
						VALUES ('compatibility-stop-scan',$1,'Running',$2,clock_timestamp())`, child.LibraryID, child.ID)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			begin := make(chan struct{})
			results := make(chan error, 2)
			var workers sync.WaitGroup
			for range 2 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-begin
					_, err := store.StopByDefinition(ctx, actor, definition.ID)
					results <- err
				}()
			}
			close(begin)
			workers.Wait()
			close(results)
			succeeded, notRunning := 0, 0
			for err := range results {
				switch {
				case err == nil:
					succeeded++
				case errors.Is(err, ErrNotRunning):
					notRunning++
				default:
					t.Fatalf("compatibility stop returned an unexpected error: %v", err)
				}
			}
			if succeeded != 1 || notRunning != 1 {
				t.Fatalf("concurrent stops did not serialize: succeeded=%d not-running=%d", succeeded, notRunning)
			}
			current, err := store.GetRun(ctx, admission.Run.ID)
			if err != nil || current.StopReason != "administrator" {
				t.Fatalf("accepted stop was not persisted: %v", err)
			}
			if started {
				var flag bool
				if err := pool.QueryRow(ctx, `SELECT cancel_requested FROM scan_jobs WHERE id='compatibility-stop-scan'`).Scan(&flag); err != nil || !flag || current.State != RunStopping {
					t.Fatalf("running stop lost durable cancellation or terminal waiting: %v", err)
				}
			} else if current.State != RunCancelled {
				t.Fatalf("pending stop remained active: %s", current.State)
			}
			if _, err := store.Stop(ctx, native, admission.Run.ID); err != nil {
				t.Fatalf("compatibility stop changed native idempotency: %v", err)
			}
		})
	}
}

func TestTaskCompatibilityStopDistinguishesMissingIdleAndInvalidAuthority(t *testing.T) {
	ctx, pool, _, store, native, definition := taskRepository(t, 1)
	actor := taskCompatibilityActor(t, ctx, pool, native)
	if _, err := store.StopByDefinition(ctx, actor, definition.ID); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("idle definition returned %v", err)
	}
	if _, err := store.StopByDefinition(ctx, actor, "unknown-definition"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing definition returned %v", err)
	}
	if _, err := store.StopByDefinition(ctx, Actor{}, "unknown-definition"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("empty actor bypassed compatibility authorization: %v", err)
	}
	admission, err := store.Start(ctx, native, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StopByDefinition(ctx, actor, definition.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("stale compatibility credential stopped a task: %v", err)
	}
	unchanged, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || unchanged.State != RunPending || unchanged.StopRequestedAt != nil {
		t.Fatalf("rejected compatibility stop changed the run: %v", err)
	}
}

func TestTaskCompatibilityStopRollsBackWhenActorExpiresBeforeCommit(t *testing.T) {
	ctx, pool, scanner, store, native, definition := taskRepository(t, 2)
	actor := taskCompatibilityActor(t, ctx, pool, native)
	admission, err := store.Start(ctx, native, StartRequest{TaskID: definition.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	var reached atomic.Bool
	delayed, err := New(pool, hookedTransactions{owner: scanner, hook: func(tx library.OwnedTx, statement string) error {
		if strings.HasPrefix(statement, "UPDATE scan_jobs j SET cancel_requested") && reached.CompareAndSwap(false, true) {
			_, err := tx.Exec(`SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
				(expires_at-clock_timestamp())))+0.03)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID)
			return err
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := delayed.StopByDefinition(ctx, actor, definition.ID); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("expired actor committed compatibility stop: %v", err)
	}
	if !reached.Load() {
		t.Fatal("test did not reach its post-cancellation-write barrier")
	}
	current, err := store.GetRun(ctx, admission.Run.ID)
	if err != nil || current.State != RunPending || current.StopRequestedAt != nil {
		t.Fatalf("failed final authority check left the run stopped: %v", err)
	}
	children, err := store.ListChildren(ctx, admission.Run.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children.Items {
		if child.State != ChildWaiting || child.FinishedAt != nil {
			t.Fatal("failed final authority check left a cancelled child")
		}
	}
}
