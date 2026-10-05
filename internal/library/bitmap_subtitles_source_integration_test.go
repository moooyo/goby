//go:build linux

package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func bitmapSubtitlePlaybackFixture(t *testing.T) (mediaSourceFixture, []BitmapSubtitle) {
	t.Helper()
	fixture := mediaSourceTestCatalog(t, nil)
	writeBitmapCatalogFiles(t, fixture)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	tracks := bitmapCatalogTracks(t, fixture)
	if len(tracks) != 3 {
		t.Fatalf("expected one SUP and two VobSub languages: %+v", tracks)
	}
	return fixture, tracks
}

func TestWithBitmapSubtitleBorrowsExactPairAndDemuxOrdinal(t *testing.T) {
	fixture, tracks := bitmapSubtitlePlaybackFixture(t)
	indexBytes, subBytes := bitmapCatalogTestPair()
	for _, track := range tracks {
		var held []*os.File
		calls := 0
		ctx := media.WithSourceReadPhase(fixture.ctx, func(context.Context, func(context.Context) error) error {
			return errors.New("inherited primary read phase must not govern the sidecar consumer")
		})
		err := fixture.store.WithBitmapSubtitleFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
			media.SourceID(fixture.item.ID), track.Index, func(ctx context.Context, info BitmapSubtitle, input media.ExternalSubtitleTimelineInput) error {
				calls++
				if !reflect.DeepEqual(info, track) || input.StreamIndex != track.Index || input.Codec != track.Codec || input.SourceStreamIndex != track.SourceStreamIndex {
					return errors.New("selected bitmap identity or demux ordinal changed")
				}
				if PrimaryRootIOFromContext(ctx) == nil {
					return errors.New("consumer lost its admitted source IO context")
				}
				if err := media.RunSourceReadPhase(ctx, func(context.Context) error { return nil }); err != nil {
					return err
				}
				held = append(held, input.Input)
				data, err := io.ReadAll(input.Input)
				if err != nil {
					return err
				}
				if info.Format == "sup" {
					if input.Companion != nil || !bytes.Equal(data, bitmapCatalogTestSUP()) {
						return errors.New("SUP input differs from its indexed bytes")
					}
				} else {
					if input.Companion == nil || !bytes.Equal(data, indexBytes) {
						return errors.New("IDX input or paired SUB descriptor is missing")
					}
					held = append(held, input.Companion)
					companion, err := io.ReadAll(input.Companion)
					if err != nil || !bytes.Equal(companion, subBytes) {
						return errors.New("VobSub companion differs from its indexed bytes")
					}
				}
				return nil
			})
		if err != nil || calls != 1 {
			t.Fatalf("bitmap track %d read: calls=%d error=%v", track.Index, calls, err)
		}
		for _, file := range held {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("borrowed descriptor was not closed after consume: %v", err)
			}
		}
		if err := fixture.store.WithBitmapSubtitleFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
			media.SourceID(fixture.item.ID), track.Index, nil); err != nil {
			t.Fatalf("bitmap-only revalidation failed: %v", err)
		}
	}
}

func TestWithBitmapSubtitleEnforcesPoliciesSelectorsAndApplicationRevocation(t *testing.T) {
	fixture, tracks := bitmapSubtitlePlaybackFixture(t)
	ctx, pool, store := fixture.ctx, fixture.pool, fixture.store
	libraryIntegrationUser(t, ctx, pool, "bitmap-reader", false, false, []string{fixture.library.ID})
	libraryIntegrationUser(t, ctx, pool, "bitmap-hidden", false, false, nil)
	libraryIntegrationUser(t, ctx, pool, "bitmap-disabled", true, true, nil)
	for _, test := range []struct {
		user, item, source string
		index              int
		want               error
	}{
		{"bitmap-reader", fixture.item.ID, media.SourceID(fixture.item.ID), tracks[0].Index, nil},
		{"bitmap-hidden", fixture.item.ID, media.SourceID(fixture.item.ID), tracks[0].Index, ErrNotFound},
		{"bitmap-disabled", fixture.item.ID, media.SourceID(fixture.item.ID), tracks[0].Index, ErrForbidden},
		{"missing-user", fixture.item.ID, media.SourceID(fixture.item.ID), tracks[0].Index, ErrForbidden},
		{"bitmap-reader", "missing-item", media.SourceID(fixture.item.ID), tracks[0].Index, ErrNotFound},
		{"bitmap-reader", fixture.item.ID, "https://example.invalid/subtitle.sup", tracks[0].Index, ErrNotFound},
		{"bitmap-reader", fixture.item.ID, "", tracks[0].Index, ErrNotFound},
		{"bitmap-reader", fixture.item.ID, media.SourceID(fixture.item.ID), 0, ErrNotFound},
		{"bitmap-reader", fixture.item.ID, media.SourceID(fixture.item.ID), tracks[2].Index + 1, ErrNotFound},
	} {
		called := false
		err := store.WithBitmapSubtitleFor(ctx, Subject{UserID: test.user}, test.item, test.source, test.index,
			func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error {
				called = true
				return nil
			})
		if test.want == nil && (err != nil || !called) || test.want != nil && (!errors.Is(err, test.want) || called) {
			t.Errorf("bitmap read %+v: called=%t error=%v", test, called, err)
		}
	}
	subject := seedCatalogApplicationKey(t, ctx, pool, "bitmap-source-key", true)
	if err := store.WithBitmapSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), tracks[1].Index, nil); err != nil {
		t.Fatalf("application key bitmap read failed: %v", err)
	}
	err := store.WithBitmapSubtitleFor(ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), tracks[1].Index,
		func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error {
			_, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", subject.ApplicationCredentialID)
			return err
		})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application credential survived a completed bitmap consume: %v", err)
	}
}

func TestWithBitmapSubtitleRejectsChangedAndUnsafeComponents(t *testing.T) {
	for _, mutation := range []string{"sup-removed", "idx-removed", "sub-removed", "sub-replaced", "sub-symlink", "sub-directory", "primary-changed"} {
		t.Run(mutation, func(t *testing.T) {
			fixture, tracks := bitmapSubtitlePlaybackFixture(t)
			track := tracks[1]
			path := filepath.Join(filepath.Dir(fixture.path), "Feature.sub")
			switch mutation {
			case "sup-removed":
				track, path = tracks[0], filepath.Join(filepath.Dir(fixture.path), tracks[0].Filename)
			case "idx-removed":
				path = filepath.Join(filepath.Dir(fixture.path), track.Filename)
			case "primary-changed":
				path = fixture.path
			}
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "sub-replaced", "primary-changed":
				data, err := os.ReadFile(path + ".saved")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "sub-symlink":
				if err := os.Symlink(path+".saved", path); err != nil {
					t.Fatal(err)
				}
			case "sub-directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			err := fixture.store.WithBitmapSubtitleFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
				media.SourceID(fixture.item.ID), track.Index, func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error {
					called = true
					return nil
				})
			if called || !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("unsafe %s reached bitmap consumer: called=%t error=%v", mutation, called, err)
			}
		})
	}
}

func TestWithBitmapSubtitleHashFencesCompanionWithMatchingCatalogStats(t *testing.T) {
	fixture, tracks := bitmapSubtitlePlaybackFixture(t)
	track := tracks[1]
	path := filepath.Join(filepath.Dir(fixture.path), track.Components[1].Name)
	_, changed := bitmapCatalogTestPair()
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	current := subtitleTimelineComponentFixture(t, path)
	current.SHA256 = track.Components[1].SHA256
	track.Components[1] = current
	encoded, err := json.Marshal(track.Components)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_bitmap_subtitles SET components=$3 WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, track.Index, encoded); err != nil {
		t.Fatal(err)
	}
	err = fixture.store.WithBitmapSubtitleFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
		media.SourceID(fixture.item.ID), track.Index, nil)
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("matching filesystem stats bypassed the paired SUB content hash: %v", err)
	}
}

func TestWithBitmapSubtitleRechecksConsumerMutationsAndRetiresOnError(t *testing.T) {
	for _, mutation := range []string{"sup", "companion", "primary", "parent", "catalog", "policy", "consumer-error"} {
		t.Run(mutation, func(t *testing.T) {
			fixture, tracks := bitmapSubtitlePlaybackFixture(t)
			track := tracks[1]
			if mutation == "sup" {
				track = tracks[0]
			}
			failure := errors.New("consumer rejected bitmap")
			var held []*os.File
			err := fixture.store.WithBitmapSubtitleFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
				media.SourceID(fixture.item.ID), track.Index, func(ctx context.Context, info BitmapSubtitle, input media.ExternalSubtitleTimelineInput) error {
					held = append(held, input.Input)
					if input.Companion != nil {
						held = append(held, input.Companion)
					}
					switch mutation {
					case "sup", "companion":
						component := len(info.Components) - 1
						path := filepath.Join(filepath.Dir(fixture.path), info.Components[component].Name)
						data, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						data[len(data)-1] ^= 1
						if err := os.WriteFile(path, data, 0600); err != nil {
							return err
						}
						// Consumer metadata is a copy and cannot rewrite the fence.
						stat, err := os.Stat(path)
						if err != nil {
							return err
						}
						digest := sha256.Sum256(data)
						info.Components[component] = BitmapSubtitleComponent{Name: filepath.Base(path), Identity: fileIdentity(stat),
							Size: stat.Size(), ModifiedNS: stat.ModTime().UnixNano(), ChangeTimeNS: media.FileChangeTime(stat), SHA256: hex.EncodeToString(digest[:])}
					case "catalog":
						_, err := fixture.pool.Exec(ctx, `UPDATE item_bitmap_subtitles SET title='Replacement' WHERE item_id=$1 AND stream_index=$2`, fixture.item.ID, track.Index)
						return err
					case "primary":
						if err := os.Rename(fixture.path, fixture.path+".saved"); err != nil {
							return err
						}
						return os.WriteFile(fixture.path, []byte(fixture.contents), 0600)
					case "parent":
						parent := filepath.Dir(fixture.path)
						if err := os.Rename(parent, parent+".saved"); err != nil {
							return err
						}
						return os.Mkdir(parent, 0700)
					case "policy":
						_, err := fixture.pool.Exec(ctx, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, fixture.userID)
						return err
					case "consumer-error":
						return failure
					}
					return nil
				})
			want := ErrSourceChanged
			if mutation == "policy" {
				want = ErrForbidden
			} else if mutation == "consumer-error" {
				want = failure
			}
			if !errors.Is(err, want) {
				t.Fatalf("mutation %s survived bitmap consumption: %v", mutation, err)
			}
			for _, file := range held {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("failed consumer retained descriptor: %v", err)
				}
			}
		})
	}
}

func TestWithBitmapSubtitleCancellationRetainsBorrowedDescriptorUntilConsumerExits(t *testing.T) {
	fixture, tracks := bitmapSubtitlePlaybackFixture(t)
	ctx, cancel := context.WithCancel(fixture.ctx)
	defer cancel()
	started := make(chan *os.File, 1)
	release := make(chan struct{})
	finished := make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	go func() {
		finished <- fixture.store.WithBitmapSubtitleFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
			media.SourceID(fixture.item.ID), tracks[0].Index, func(_ context.Context, _ BitmapSubtitle, input media.ExternalSubtitleTimelineInput) error {
				started <- input.Input
				<-release
				return nil
			})
	}()
	var file *os.File
	select {
	case file = <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("bitmap consumer did not start")
	}
	cancel()
	select {
	case err := <-finished:
		t.Fatalf("canceled call returned before its borrowed consumer retired: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if _, err := file.Stat(); err != nil || len(subtitleSourceWorkers) == 0 {
		t.Fatalf("cancellation retired a descriptor or slot still borrowed by consumer: %v", err)
	}
	unblock()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled bitmap source returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled bitmap call did not finish after consumer retirement")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("bitmap call returned before its borrowed descriptor closed: %v", err)
	}
	if !imageStoreWaitWorkerCleanup(subtitleSourceWorkers) {
		t.Fatal("bitmap worker did not finish descriptor cleanup")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("bitmap descriptor remained open after consumer retirement: %v", err)
	}
}

func TestWithBitmapSubtitleQueuedAuthorityAndCancellationNeverLendDescriptors(t *testing.T) {
	for _, scenario := range []string{"policy", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, tracks := bitmapSubtitlePlaybackFixture(t)
			wait, stopWait := context.WithTimeout(fixture.ctx, 10*time.Second)
			defer stopWait()
			ctx, cancel := context.WithCancel(wait)
			defer cancel()
			hint, _, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			route, err := fixture.store.primaryReadRoute(hint)
			if err != nil {
				t.Fatal(err)
			}
			beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
			beforeIO := originalMediaReadGovernor.Stats()
			releases := primaryReadTestHold(t, wait, route, mediaSourceRootOwnerLimit)
			var called atomic.Bool
			finished := make(chan error, 1)
			go func() {
				finished <- fixture.store.WithBitmapSubtitleFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID,
					media.SourceID(fixture.item.ID), tracks[1].Index, func(context.Context, BitmapSubtitle, media.ExternalSubtitleTimelineInput) error {
						called.Store(true)
						return nil
					})
			}()
			primaryReadTestWaitQueued(t, wait, beforeIO.Queued+1)
			if scenario == "policy" {
				if _, err := fixture.pool.Exec(wait, `UPDATE users SET policy=policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
				select {
				case err := <-finished:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("queued cancellation returned %v", err)
					}
				case <-wait.Done():
					t.Fatal("queued cancellation waited for an unused consumer")
				}
			}
			for _, release := range releases {
				release()
			}
			if scenario == "policy" {
				select {
				case err := <-finished:
					if !errors.Is(err, ErrForbidden) {
						t.Fatalf("queued policy revocation returned %v", err)
					}
				case <-wait.Done():
					t.Fatal("revoked bitmap request did not finish after admission")
				}
			}
			primaryReadTestWaitOwners(t, wait, beforeOwners)
			if called.Load() {
				t.Fatal("a canceled or unauthorized queued request lent source descriptors")
			}
		})
	}
}
