//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func TestOwnedPrimaryReadProjectionPreservesPlanningFactsAndBoundedReads(t *testing.T) {
	fixture, _, _ := subtitleTestCatalog(t)
	fixture.contents = "video:" + strings.Repeat("projected primary source ", 4096)
	if err := os.WriteFile(fixture.path, []byte(fixture.contents), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationFile(t, fixture.allowedRoot, "movies/Nested/Feature.nfo",
		`<movie><title>Owned projection fixture</title><plot>Catalog overview</plot><genre>Drama</genre><tag>Allowed</tag><studio>Example</studio><actor><name>Fixture Person</name><role>Lead</role></actor></movie>`)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media ||
		'{"FormatStartTicks":17000,"FormatStartKnown":true,"PresentationOriginTicks":11000,"AudioDurationReason":"fixture_reason","Chapters":[{"StartTicks":0,"EndTicks":10000000,"Title":"Opening"}]}'::jsonb,
		'{Streams,0,Profile}','"High"'::jsonb) WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "owned-projection-intro-editor")
	intro, err := fixture.store.GetItemIntro(fixture.ctx, actor, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.UpdateItemIntro(fixture.ctx, actor, fixture.item.ID,
		IntroEdit{Revision: intro.Revision, SourceRevision: intro.SourceRevision, StartTicks: 2 * media.TicksPerSecond,
			EndTicks: 12 * media.TicksPerSecond, Provenance: "Manual"}, false); err != nil {
		t.Fatal(err)
	}
	trace := &mediaRevalidationPerformanceTrace{}
	configuration := fixture.pool.Config()
	configuration.ConnConfig.Tracer = trace
	tracedPool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	fixture.store.pool = tracedPool
	t.Cleanup(func() { fixture.store.pool = fixture.pool })
	ctx, cancel := context.WithTimeout(context.WithValue(fixture.ctx, mediaRevalidationPerformanceContextKey{}, true), 20*time.Second)
	defer cancel()
	fixture.ctx = ctx
	subject := Subject{UserID: fixture.userID}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	beforeIO := originalMediaReadGovernor.Stats()
	for _, purpose := range []string{"playback", "download"} {
		trace.reset()
		var planning MediaFile
		var file *os.File
		if purpose == "download" {
			file, planning, err = fixture.store.OpenDownloadFor(ctx, subject, fixture.item.ID, "")
		} else {
			file, planning, err = fixture.store.OpenMediaFor(ctx, subject, fixture.item.ID, "")
		}
		if file != nil {
			_ = file.Close()
		}
		if err != nil || file == nil || planning.Item.Metadata == nil || planning.Item.ParentID == "" ||
			len(planning.Item.Entities.Genres) == 0 || len(planning.Item.Entities.People) == 0 ||
			len(planning.Item.Subtitles) != 2 || planning.Item.Intro == nil || trace.entities.Load() != 1 {
			t.Fatalf("%s planning lost its complete catalog contract: source=%+v error=%v", purpose, planning, err)
		}
		if planning.Item.Media == nil || !planning.Item.Media.FormatStartKnown || len(planning.Item.Media.Chapters) != 1 ||
			planning.Item.Media.Streams[0].Profile != "High" {
			t.Fatalf("%s planning did not retain the rich primary facts", purpose)
		}
		trace.reset()
		result := primaryReadTestReceiveOpen(t, ctx, primaryProjectionTestStartOpen(t, ctx, fixture, subject, purpose, fixture.item.ID, planning.ETag))
		primaryProjectionTestRequireSource(t, result, planning)
		if trace.sources.Load() == 0 || trace.entities.Load() != 0 || trace.subtitles.Load() != 0 {
			t.Fatalf("%s owned reopen loaded unused catalog projections: sources=%d entities=%d subtitles=%d",
				purpose, trace.sources.Load(), trace.entities.Load(), trace.subtitles.Load())
		}
		for _, fragment := range []string{"jsonb_agg", "catalog_entities", "item_metadata_state", "album_ancestors", "tv_parent", "tv_series", "item_intro_state"} {
			if strings.Contains(trace.statement, fragment) {
				t.Fatalf("%s owned reopen queried unused catalog projection %q", purpose, fragment)
			}
		}
		read := primaryReadTestReceiveRead(t, ctx, primaryReadTestStartRead(result.reader, 64*1024))
		if read.err != nil || len(read.data) != 32*1024 || string(read.data) != fixture.contents[:32*1024] {
			t.Fatalf("%s projected source lost its bounded original read: bytes=%d error=%v", purpose, len(read.data), read.err)
		}
		if !primaryReadTestClose(t, result.reader) {
			return
		}
		if _, err := result.file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("%s projected consumer retired before descriptor closure: %v", purpose, err)
		}
		primaryReadTestWaitOwners(t, ctx, beforeOwners)
		if after := originalMediaReadGovernor.Stats(); after != beforeIO {
			t.Fatalf("%s projected consumer retained actual-read admission: before=%+v after=%+v", purpose, beforeIO, after)
		}
	}
	primaryProjectionTestJoinStore(t, fixture)
}

func TestOwnedPrimaryReadProjectionKeepsPurposeAuthorityAndFailureGuards(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, cancel := context.WithTimeout(fixture.ctx, 20*time.Second)
	defer cancel()
	fixture.ctx = ctx
	planning := primaryReadTestSnapshot(t, fixture)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	for _, policy := range []struct {
		name, encoded  string
		play, download bool
	}{
		{"download-only", `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false}`, false, true},
		{"playback-only", `{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":true}`, true, false},
	} {
		if _, err := fixture.pool.Exec(ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", fixture.userID, policy.encoded); err != nil {
			t.Fatal(err)
		}
		for _, purpose := range []string{"playback", "download"} {
			allowed := policy.play
			if purpose == "download" {
				allowed = policy.download
			}
			result := primaryReadTestReceiveOpen(t, ctx, primaryProjectionTestStartOpen(t, ctx, fixture,
				Subject{UserID: fixture.userID}, purpose, fixture.item.ID, planning.ETag))
			if !allowed {
				primaryReadTestOpenFailure(t, result, ErrForbidden)
			} else {
				expected := planning
				expected.Item.CanPlay = policy.play
				primaryProjectionTestRequireSource(t, result, expected)
				if !primaryReadTestClose(t, result.reader) {
					return
				}
			}
			// Root preparation fails before the source reader runs. Its failure
			// guard must still read current authority for the same delivery purpose.
			guard := primaryReadTestReceiveOpen(t, ctx, primaryProjectionTestStartOpen(t, ctx, fixture,
				Subject{UserID: fixture.userID}, purpose, "missing-projected-route", planning.ETag))
			want := ErrForbidden
			if allowed {
				want = ErrNotFound
			}
			primaryReadTestOpenFailure(t, guard, want)
			primaryReadTestWaitOwners(t, ctx, beforeOwners)
		}
	}
	key := seedCatalogApplicationKey(t, ctx, fixture.pool, "owned-projection-key", true)
	if _, err := fixture.pool.Exec(ctx, `UPDATE users SET is_disabled=true,policy='{"EnableAllFolders":true,"EnableContentDownloading":false,"EnableMediaPlayback":false}'::jsonb WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	key.UserID = fixture.userID
	for _, purpose := range []string{"playback", "download"} {
		result := primaryReadTestReceiveOpen(t, ctx, primaryProjectionTestStartOpen(t, ctx, fixture, key, purpose, fixture.item.ID, planning.ETag))
		primaryProjectionTestRequireSource(t, result, planning)
		if !primaryReadTestClose(t, result.reader) {
			return
		}
	}
	if _, err := fixture.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{"playback", "download"} {
		for _, itemID := range []string{fixture.item.ID, "missing-projected-route"} {
			result := primaryReadTestReceiveOpen(t, ctx, primaryProjectionTestStartOpen(t, ctx, fixture, key, purpose, itemID, planning.ETag))
			primaryReadTestOpenFailure(t, result, ErrForbidden)
		}
	}
	primaryReadTestWaitOwners(t, ctx, beforeOwners)
	primaryProjectionTestJoinStore(t, fixture)
}

func TestOwnedPrimaryReadProjectionRechecksQueuedAuthorityAndSourceProofs(t *testing.T) {
	for _, purpose := range []string{"playback", "download"} {
		for _, change := range []string{"permission", "binding", "snapshot", "publication"} {
			t.Run(purpose+"/"+change, func(t *testing.T) {
				fixture := mediaSourceTestCatalog(t, nil)
				ctx, cancel := context.WithTimeout(fixture.ctx, 15*time.Second)
				defer cancel()
				fixture.ctx = ctx
				planning := primaryReadTestSnapshot(t, fixture)
				_, root, domain := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
				releases := mediaSourceRootAdmissionTestHold(t, ctx, root, domain, mediaSourceRootOwnerLimit)
				beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
				call := primaryProjectionTestStartOpen(t, ctx, fixture, Subject{UserID: fixture.userID}, purpose, fixture.item.ID, planning.ETag)
				mediaSourceRootAdmissionTestQueue(t, ctx, mediaSourceAdmission, root, 1)
				want := ErrUnavailable
				switch change {
				case "permission":
					feature := "EnableMediaPlayback"
					if purpose == "download" {
						feature = "EnableContentDownloading"
					}
					if _, err := fixture.pool.Exec(ctx, `UPDATE users SET policy=jsonb_set(policy,ARRAY[$2::text],'false'::jsonb) WHERE id=$1`, fixture.userID, feature); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(fixture.path); err != nil {
						t.Fatal(err)
					}
					want = ErrForbidden
				case "binding":
					if _, err := fixture.pool.Exec(ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", root.id); err != nil {
						t.Fatal(err)
					}
				case "snapshot":
					if _, err := fixture.pool.Exec(ctx, "UPDATE items SET modified_at=modified_at+interval '1 second' WHERE id=$1", fixture.item.ID); err != nil {
						t.Fatal(err)
					}
				case "publication":
					publicationReadTestReservation(t, fixture, "prepared")
					want = ErrBusy
				}
				for _, release := range releases {
					release()
				}
				result := primaryReadTestReceiveOpen(t, ctx, call)
				primaryReadTestOpenFailure(t, result, want)
				if (change == "binding" || change == "snapshot") && !errors.Is(result.err, ErrSourceChanged) {
					t.Fatalf("queued projected %s lost its fresh source-change proof: %v", purpose, result.err)
				}
				primaryReadTestWaitOwners(t, ctx, beforeOwners)
				primaryProjectionTestJoinStore(t, fixture)
			})
		}
	}
}

func TestOwnedPrimaryReadProjectionRetainsPostOpenPublicationBarrier(t *testing.T) {
	for _, purpose := range []string{"playback", "download"} {
		t.Run(purpose, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			ctx, cancel := context.WithTimeout(fixture.ctx, 15*time.Second)
			defer cancel()
			fixture.ctx = ctx
			planning := primaryReadTestSnapshot(t, fixture)
			beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
			captured := make(chan indexedMediaSource, 1)
			allowOpen := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(allowOpen) }) }
			caller, cancelCaller := context.WithCancel(ctx)
			call := &primaryReadTestOpenCall{results: make(chan primaryReadTestOpenResult, 1)}
			t.Cleanup(func() {
				cancelCaller()
				release()
				cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancelCleanup()
				if !call.received {
					select {
					case result := <-call.results:
						call.received = true
						primaryReadTestRetireOpen(t, result)
					case <-cleanup.Done():
						t.Error("projected publication opener did not finish within bounded cleanup")
					}
				}
				if err := fixture.store.Close(cleanup); err != nil {
					t.Errorf("projected publication source owner did not join: %v", err)
				}
			})
			readSnapshot := fixture.store.readOriginalDownloadRevalidationFor
			if purpose == "playback" {
				readSnapshot = func(ctx context.Context, subject Subject, itemID, sourceID string) (indexedMediaSource, error) {
					return fixture.store.readMediaRevalidationFor(ctx, subject, itemID, sourceID, false)
				}
			}
			go func() {
				file, source, reader, err := fixture.store.openOriginalReadFor(caller, Subject{UserID: fixture.userID}, fixture.item.ID,
					planning.SourceID, planning.ETag, readSnapshot, func(worker context.Context, snapshot indexedMediaSource) (*os.File, error) {
						captured <- snapshot
						select {
						case <-allowOpen:
							return fixture.store.openPublicMediaSource(worker, snapshot)
						case <-worker.Done():
							return nil, worker.Err()
						}
					})
				call.results <- primaryReadTestOpenResult{file: file, source: source, reader: reader, err: err}
			}()
			select {
			case snapshot := <-captured:
				if snapshot.publicationRevision == "" || !reflect.DeepEqual(snapshot.mediaFile.Item.Media, planning.Item.Media) {
					t.Fatal("projected opener lost its complete facts or publication capture")
				}
			case <-ctx.Done():
				t.Fatalf("projected source did not reach actual opening: %v", ctx.Err())
			}
			publicationReadTestReservation(t, fixture, "prepared")
			blocked := primaryReadTestReceiveOpen(t, ctx, primaryProjectionTestStartOpen(t, ctx, fixture,
				Subject{UserID: fixture.userID}, purpose, fixture.item.ID, planning.ETag))
			primaryReadTestOpenFailure(t, blocked, ErrBusy)
			release()
			result := primaryReadTestReceiveOpen(t, ctx, call)
			primaryReadTestOpenFailure(t, result, ErrBusy)
			primaryReadTestWaitOwners(t, ctx, beforeOwners)
			primaryProjectionTestJoinStore(t, fixture)
		})
	}
}

func primaryProjectionTestStartOpen(t *testing.T, ctx context.Context, fixture mediaSourceFixture, subject Subject, purpose, itemID, expectedETag string) *primaryReadTestOpenCall {
	t.Helper()
	work, cancel := context.WithCancel(ctx)
	call := &primaryReadTestOpenCall{results: make(chan primaryReadTestOpenResult, 1)}
	t.Cleanup(func() {
		cancel()
		if call.received {
			return
		}
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		select {
		case result := <-call.results:
			primaryReadTestRetireOpen(t, result)
		case <-cleanup.Done():
			t.Error("projected owned caller did not finish within bounded cleanup")
		}
	})
	go func() {
		var result primaryReadTestOpenResult
		if purpose == "download" {
			result.file, result.source, result.reader, result.err = fixture.store.OpenOriginalDownloadFor(work, subject, itemID, "", expectedETag)
		} else {
			result.file, result.source, result.reader, result.err = fixture.store.OpenOriginalMediaFor(work, subject, itemID, "", expectedETag)
		}
		call.results <- result
	}()
	return call
}

func primaryProjectionTestRequireSource(t *testing.T, result primaryReadTestOpenResult, planning MediaFile) {
	t.Helper()
	expected := planning
	expected.Item = Item{ID: planning.Item.ID, LibraryID: planning.Item.LibraryID, Type: planning.Item.Type,
		Path: planning.Item.Path, CanPlay: planning.Item.CanPlay, Media: planning.Item.Media}
	if result.err != nil || result.file == nil || result.reader == nil || !reflect.DeepEqual(result.source, expected) {
		t.Fatalf("owned projection changed primary facts or retained catalog fields: source=%+v expected=%+v error=%v", result.source, expected, result.err)
	}
}

func primaryProjectionTestJoinStore(t *testing.T, fixture mediaSourceFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fixture.store.Close(ctx); err != nil {
		t.Fatalf("projected source owners did not finish actual Store cleanup: %v", err)
	}
}
