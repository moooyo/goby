//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func playbackMediaPipelineTestDrain(t *testing.T, store *Store) {
	t.Helper()
	drained := make(chan struct{})
	go func() {
		store.mediaSourceOwners.owners.Wait()
		close(drained)
	}()
	mediaSourceAdmissionTestWait(t, drained, "completed fixture source owner cleanup")
}

func playbackMediaPipelineTestOwnerCounts(t *testing.T, root mediaSourceRootKey, wantIO, wantAuthorization int) {
	t.Helper()
	mediaSourceAdmission.mu.Lock()
	ioOwners := mediaSourceAdmission.roots[root].active
	mediaSourceAdmission.mu.Unlock()
	mediaSourceAuthorizationAdmission.mu.Lock()
	authorizationOwners := mediaSourceAuthorizationAdmission.roots[root].active
	mediaSourceAuthorizationAdmission.mu.Unlock()
	if ioOwners != wantIO || authorizationOwners != wantAuthorization {
		t.Fatalf("routed pipeline owners: io=%d authorization=%d; want %d/%d", ioOwners, authorizationOwners, wantIO, wantAuthorization)
	}
}

func playbackMediaPipelineTestProfile(t *testing.T, store *Store, before map[string]uint64, authorizations, immediate, misses, reauthorizations uint64) {
	t.Helper()
	after := store.MediaSourceAdmissionProfile()
	for field, want := range map[string]uint64{
		"authorizations": authorizations, "immediate_grants": immediate,
		"try_misses": misses, "reauthorizations": reauthorizations,
	} {
		if got := after[field] - before[field]; got != want {
			t.Errorf("pipeline profile %s delta=%d, want %d", field, got, want)
		}
	}
}

func playbackMediaPipelineTestRowLocked(t *testing.T, ctx context.Context, fixture mediaSourceFixture, statement, id string, locked bool) {
	t.Helper()
	tx, err := fixture.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	var observed string
	err = tx.QueryRow(ctx, statement, id).Scan(&observed)
	if locked {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "55P03" {
			t.Fatalf("authority row was not SHARE-locked at the memory-only IO grant: %v", err)
		}
	} else if err != nil || observed != id {
		t.Fatalf("authority row retained a lock after the authorization commit: %v", err)
	}
}

func TestPlaybackMediaSourcePipelineCheckerPanicRecoversAndReleasesOwners(t *testing.T) {
	for _, application := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("application=%t/canceled=%t", application, canceled), func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				principal, play := playbackMediaTestPrincipal(t, fixture, application)
				_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
				playbackMediaPipelineTestDrain(t, fixture.store)
				ctx, cancel := context.WithCancel(fixture.ctx)
				defer cancel()
				file, _, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false, func(PlaybackMediaAuthorization) error {
					if canceled {
						cancel()
					}
					panic("injected delivery checker panic")
				})
				want := error(ErrUnavailable)
				if canceled {
					want = context.Canceled
				}
				if file != nil || !errors.Is(err, want) {
					if file != nil {
						_ = file.Close()
					}
					t.Fatalf("delivery panic escaped its owned worker or hid cancellation: %v", err)
				}
				playbackMediaPipelineTestDrain(t, fixture.store)
				playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
				playbackMediaPipelineTestRowLocked(t, fixture.ctx, fixture, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID, false)
			})
		}
	}
}

func TestPlaybackMediaSourcePipelineAdmissionPanicUnwindsTransactionAndLease(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
	playbackMediaPipelineTestDrain(t, fixture.store)
	ctx := mediaSourceAuthorizationContext(fixture.ctx, root, domain, false)
	worker, finish, err := fixture.store.beginMediaSourceLifetime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ioRelease func()
	file, _, err := runAdmittedMediaSourceWorker(worker, func() {
		if ioRelease != nil {
			ioRelease()
		}
		finish()
	}, func() (*os.File, MediaFile, error) {
		_, _, readErr := fixture.store.readPlaybackMediaAuthorizationWithAdmission(worker, principal, playbackMediaOwner(principal), play.ID, fixture.item.ID, play.MediaSourceID, false, func(source indexedMediaSource) error {
			if err := validatePreparedPlaybackRoot(source, hint); err != nil {
				return err
			}
			release, granted, err := mediaSourceAdmission.tryAcquireRoot(worker, false, root, domain)
			if err != nil || !granted {
				return errors.New("test admission did not grant before its panic")
			}
			ioRelease = release
			panic("injected memory admission hook panic")
		})
		return nil, MediaFile{}, readErr
	})
	if file != nil || !errors.Is(err, ErrUnavailable) || ioRelease == nil {
		t.Fatalf("admission panic escaped its owned worker or did not grant: %v", err)
	}
	playbackMediaPipelineTestDrain(t, fixture.store)
	playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
	playbackMediaPipelineTestRowLocked(t, fixture.ctx, fixture, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID, false)
}

func TestPlaybackMediaSourcePipelinePublicationPanicClosesUnhandedDescriptor(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	source, err := fixture.store.readMediaSourceFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the fixture's approved root descriptors but inject a failed query
	// boundary after the contained file has opened. No fixture pool is mutated.
	isolated := &Store{}
	fixture.store.mu.Lock()
	isolated.roots = append([]approvedRoot(nil), fixture.store.roots...)
	isolated.rootBindingAnchors = make(map[string]rootBindingAnchor, len(fixture.store.rootBindingAnchors))
	for id, anchor := range fixture.store.rootBindingAnchors {
		isolated.rootBindingAnchors[id] = anchor
	}
	fixture.store.mu.Unlock()
	openCount := func() int {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries {
			if path, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name())); err == nil && path == fixture.path {
				count++
			}
		}
		return count
	}
	before := openCount()
	file, _, err := executeMediaSourceWorker(fixture.ctx, func() (*os.File, MediaFile, error) {
		file, err := isolated.openPublicMediaSource(fixture.ctx, source)
		return file, source.mediaFile, err
	})
	if file != nil || !errors.Is(err, ErrUnavailable) || openCount() != before {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("publication panic leaked a descriptor before ownership handoff: %v", err)
	}
}

func TestPlaybackMediaSourcePipelineImmediateHookRetainsAuthorityUntilCommit(t *testing.T) {
	for _, application := range []bool{false, true} {
		t.Run(map[bool]string{false: "login", true: "application"}[application], func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, application)
			hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			playbackMediaPipelineTestDrain(t, fixture.store)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			ctx = mediaSourceAuthorizationContext(ctx, root, domain, false)
			entered, resume := make(chan struct{}), make(chan struct{})
			var resumeOnce sync.Once
			t.Cleanup(func() { resumeOnce.Do(func() { close(resume) }) })
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				var result PlaybackMediaAuthorization
				var readErr error
				func() {
					var ioRelease func()
					// Install cleanup before a transaction hook can obtain a lease.
					defer func() {
						if ioRelease != nil {
							ioRelease()
						}
					}()
					_, result, readErr = fixture.store.readPlaybackMediaAuthorizationWithAdmission(ctx, principal, playbackMediaOwner(principal),
						play.ID, fixture.item.ID, play.MediaSourceID, false, func(source indexedMediaSource) error {
							if err := validatePreparedPlaybackRoot(source, hint); err != nil {
								return err
							}
							release, granted, err := mediaSourceAdmission.tryAcquireRoot(ctx, false, root, domain)
							if err != nil {
								return err
							}
							if !granted {
								return errors.New("uncontended IO hook did not grant")
							}
							ioRelease = release
							close(entered)
							// This test barrier uses memory only. Database observers
							// below run on the main test goroutine and another checkout.
							select {
							case <-resume:
								return nil
							case <-ctx.Done():
								return ctx.Err()
							}
						})
				}()
				results <- playbackMediaTestResult{result: result, err: readErr}
			}()
			mediaSourceAdmissionTestWait(t, entered, "memory-only IO grant inside playback authority transaction")
			playbackMediaPipelineTestOwnerCounts(t, root, 1, 1)
			locks := []struct{ statement, id string }{
				{"SELECT id FROM sessions WHERE id=$1 FOR UPDATE NOWAIT", principal.SessionID},
				{"SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID},
			}
			if application {
				locks = append(locks, struct{ statement, id string }{"SELECT credential_id FROM application_keys WHERE credential_id=$1 FOR UPDATE NOWAIT", principal.SessionID},
					struct{ statement, id string }{"SELECT id FROM application_key_clients WHERE id=$1 FOR UPDATE NOWAIT", principal.ClientSessionID})
			} else {
				locks = append(locks, struct{ statement, id string }{"SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", principal.User.ID})
			}
			for _, lock := range locks {
				playbackMediaPipelineTestRowLocked(t, ctx, fixture, lock.statement, lock.id, true)
			}
			resumeOnce.Do(func() { close(resume) })
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.err != nil || result.result.Play.ID != play.ID || result.result.Principal.SessionID != principal.SessionID {
				t.Fatalf("immediate authority hook failed to commit canonical facts: %v", result.err)
			}
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			for _, lock := range locks {
				playbackMediaPipelineTestRowLocked(t, ctx, fixture, lock.statement, lock.id, false)
			}
		})
	}
}

func TestPlaybackMediaSourcePipelineQueuedGrantReauthorizesChangedAuthority(t *testing.T) {
	for _, application := range []bool{false, true} {
		for _, change := range []string{"revoke", "stop"} {
			t.Run(fmt.Sprintf("%t/%s", application, change), func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				principal, play := playbackMediaTestPrincipal(t, fixture, application)
				_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
				playbackMediaPipelineTestDrain(t, fixture.store)
				releases := mediaSourceRootAdmissionTestHold(t, fixture.ctx, root, domain, mediaSourceRootOwnerLimit)
				ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
				defer cancel()
				before := fixture.store.MediaSourceAdmissionProfile()
				var checks atomic.Int32
				results := make(chan playbackMediaTestResult, 1)
				go func() {
					file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
						func(PlaybackMediaAuthorization) error { checks.Add(1); return nil })
					results <- playbackMediaTestResult{file: file, result: result, err: err}
				}()
				mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
				playbackMediaPipelineTestOwnerCounts(t, root, mediaSourceRootOwnerLimit, 0)
				if checks.Load() != 0 {
					t.Fatal("IO queueing invoked the delivery policy callback before fresh authorization")
				}
				playbackMediaPipelineTestProfile(t, fixture.store, before, 1, 0, 1, 0)
				mutation, mutationCancel := context.WithTimeout(ctx, 2*time.Second)
				statement, id, want := "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID, error(identity.ErrUnauthorized)
				if change == "stop" {
					statement, id, want = "UPDATE play_sessions SET state='Stopped',stopped_at=clock_timestamp() WHERE id=$1", play.ID, ErrNotFound
				}
				_, err := fixture.pool.Exec(mutation, statement, id)
				mutationCancel()
				if err != nil {
					t.Fatalf("IO queueing retained an authority transaction or mutation failed: %v", err)
				}
				if err := os.Remove(fixture.path); err != nil {
					t.Fatal(err)
				}
				for _, release := range releases {
					release()
				}
				result := awaitPlaybackMediaTest(t, ctx, results)
				if result.file != nil {
					_ = result.file.Close()
				}
				if result.file != nil || !errors.Is(result.err, want) || checks.Load() != 0 {
					t.Fatalf("queued IO reused authority or reached callback/storage: checks=%d error=%v, want %v", checks.Load(), result.err, want)
				}
				playbackMediaPipelineTestProfile(t, fixture.store, before, 2, 0, 1, 1)
			})
		}
	}
}

func TestPlaybackMediaSourcePipelineImmediateAndQueuedCallbacksRunOnceAfterCommit(t *testing.T) {
	for _, application := range []bool{false, true} {
		for _, queued := range []bool{false, true} {
			t.Run(fmt.Sprintf("application=%t/queued=%t", application, queued), func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				principal, play := playbackMediaTestPrincipal(t, fixture, application)
				_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
				playbackMediaPipelineTestDrain(t, fixture.store)
				var releases []func()
				if queued {
					releases = mediaSourceRootAdmissionTestHold(t, fixture.ctx, root, domain, mediaSourceRootOwnerLimit)
				}
				ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
				defer cancel()
				before := fixture.store.MediaSourceAdmissionProfile()
				var checks atomic.Int32
				results := make(chan playbackMediaTestResult, 1)
				go func() {
					file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
						func(current PlaybackMediaAuthorization) error {
							if checks.Add(1) != 1 || current.Play.ID != play.ID || current.Source.Item.ID != fixture.item.ID || current.Source.SourceID != play.MediaSourceID {
								return errors.New("delivery callback did not receive the single final canonical snapshot")
							}
							// This mutation would block if the delivery callback ran
							// before the authority transaction committed its SHARE lock.
							mutation, cancelMutation := context.WithTimeout(ctx, 2*time.Second)
							defer cancelMutation()
							_, err := fixture.pool.Exec(mutation, "UPDATE play_sessions SET state=state WHERE id=$1", play.ID)
							return err
						})
					results <- playbackMediaTestResult{file: file, result: result, err: err}
				}()
				if queued {
					mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
					if checks.Load() != 0 {
						t.Fatal("IO queueing invoked the final delivery callback")
					}
					for _, release := range releases {
						release()
					}
				}
				result := awaitPlaybackMediaTest(t, ctx, results)
				if result.err != nil || result.file == nil || checks.Load() != 1 {
					if result.file != nil {
						_ = result.file.Close()
					}
					t.Fatalf("final pipeline callback/open failed: checks=%d error=%v", checks.Load(), result.err)
				}
				contents, err := io.ReadAll(result.file)
				_ = result.file.Close()
				if err != nil || string(contents) != fixture.contents {
					t.Fatalf("pipeline returned an invalid source descriptor: %v", err)
				}
				if queued {
					playbackMediaPipelineTestProfile(t, fixture.store, before, 2, 0, 1, 1)
				} else {
					playbackMediaPipelineTestProfile(t, fixture.store, before, 1, 1, 0, 0)
				}
			})
		}
	}
}

func TestPlaybackMediaSourcePipelineFailedHookOrCommitReleasesGrantedOwners(t *testing.T) {
	for _, failure := range []string{"hook", "commit"} {
		t.Run(failure, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			hint, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			playbackMediaPipelineTestDrain(t, fixture.store)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			ctx = mediaSourceAuthorizationContext(ctx, root, domain, false)
			hookFailure := errors.New("injected admission hook failure")
			var granted bool
			var readErr error
			func() {
				var ioRelease func()
				// The owner wrapper must exist before the hook, including when a
				// successful memory grant is followed by a failed SQL commit.
				defer func() {
					if ioRelease != nil {
						ioRelease()
					}
				}()
				_, _, readErr = fixture.store.readPlaybackMediaAuthorizationWithAdmission(ctx, principal, playbackMediaOwner(principal),
					play.ID, fixture.item.ID, play.MediaSourceID, false, func(source indexedMediaSource) error {
						if err := validatePreparedPlaybackRoot(source, hint); err != nil {
							return err
						}
						release, acquired, err := mediaSourceAdmission.tryAcquireRoot(ctx, false, root, domain)
						if err != nil || !acquired {
							return fmt.Errorf("test IO grant failed: acquired=%t error=%v", acquired, err)
						}
						ioRelease, granted = release, true
						if failure == "hook" {
							return hookFailure
						}
						cancel()
						return nil
					})
			}()
			if !granted || readErr == nil || failure == "hook" && !errors.Is(readErr, hookFailure) ||
				failure == "commit" && !errors.Is(readErr, ErrUnavailable) {
				t.Fatalf("failure did not follow a successful IO grant: granted=%t error=%v", granted, readErr)
			}
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			// Both transaction rollback and the outer owner defer must have run.
			playbackMediaPipelineTestRowLocked(t, fixture.ctx, fixture, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID, false)
		})
	}
}

func TestPlaybackMediaSourcePipelineQueuedBindingChangeRejectsBeforeCallbackOrIO(t *testing.T) {
	for _, change := range []string{"revision", "mapping"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			playbackMediaPipelineTestDrain(t, fixture.store)
			releases := mediaSourceRootAdmissionTestHold(t, fixture.ctx, root, domain, mediaSourceRootOwnerLimit)
			ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer cancel()
			before := fixture.store.MediaSourceAdmissionProfile()
			var checks atomic.Int32
			results := make(chan playbackMediaTestResult, 1)
			go func() {
				file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
					func(PlaybackMediaAuthorization) error { checks.Add(1); return nil })
				results <- playbackMediaTestResult{file: file, result: result, err: err}
			}()
			mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
			if change == "revision" {
				if _, err := fixture.pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", root.id); err != nil {
					t.Fatal(err)
				}
			} else {
				tx, err := fixture.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer rollback(tx)
				if _, err := tx.Exec(ctx, `UPDATE library_roots SET path=path || '/Replacement',
					relative_path=relative_path || '/Replacement', binding_revision=binding_revision+1 WHERE id=$1`, root.id); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, "UPDATE items SET path=$2 WHERE id=$1", fixture.item.ID,
					filepath.Join(filepath.Dir(filepath.Dir(fixture.path)), "Replacement", "Nested", filepath.Base(fixture.path))); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(fixture.path); err != nil {
				t.Fatal(err)
			}
			for _, release := range releases {
				release()
			}
			result := awaitPlaybackMediaTest(t, ctx, results)
			if result.file != nil {
				_ = result.file.Close()
			}
			if result.file != nil || !errors.Is(result.err, ErrUnavailable) || !errors.Is(result.err, ErrSourceChanged) || checks.Load() != 0 {
				t.Fatalf("queued binding replacement reached final callback or filesystem: checks=%d error=%v", checks.Load(), result.err)
			}
			playbackMediaPipelineTestProfile(t, fixture.store, before, 2, 0, 1, 1)
		})
	}
}
