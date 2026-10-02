//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func TestMediaRevalidationPreservesDeliveryFactsAndCompleteOpenContract(t *testing.T) {
	fixture, _, _ := subtitleTestCatalog(t)
	ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
	libraryIntegrationFile(t, fixture.allowedRoot, "movies/Nested/Feature.nfo",
		`<movie><title>Projected delivery fixture</title><plot>Catalog overview</plot><genre>Drama</genre><tag>Allowed</tag><studio>Example</studio><actor><name>Fixture Person</name><role>Lead</role></actor></movie>`)
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	subject := Subject{UserID: fixture.userID}
	full, complete, err := fixture.store.OpenMediaFor(fixture.ctx, subject, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = full.Close()
	if complete.Item.Name == "" || complete.Item.Metadata == nil || len(complete.Item.Entities.Genres) == 0 || len(complete.Item.Entities.People) == 0 || len(complete.Item.Subtitles) != 2 {
		t.Fatalf("complete opening lost catalog projections: %+v", complete.Item)
	}
	for _, includeSubtitles := range []bool{false, true} {
		file, current, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, subject, fixture.item.ID, complete.SourceID, includeSubtitles)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil || string(data) != fixture.contents {
			t.Fatalf("revalidation did not return the indexed bytes from offset zero: %v", readErr)
		}
		if current.Item.ID != complete.Item.ID || current.Item.LibraryID != complete.Item.LibraryID ||
			current.Item.Type != complete.Item.Type || current.Item.Path != complete.Item.Path || !current.Item.CanPlay ||
			!reflect.DeepEqual(current.Item.Media, complete.Item.Media) || current.SourceID != complete.SourceID ||
			current.ETag != complete.ETag || current.Size != complete.Size || !current.ModifiedAt.Equal(complete.ModifiedAt) ||
			current.Container != complete.Container || current.MIMEType != complete.MIMEType {
			t.Fatalf("delivery revalidation changed current source facts: %+v", current)
		}
		if current.Item.Metadata != nil || !reflect.DeepEqual(current.Item.Entities, ItemEntities{}) || current.Item.Name != "" || current.Item.ParentID != "" || current.Item.Intro != nil {
			t.Fatal("delivery revalidation unexpectedly loaded catalog projections")
		}
		if includeSubtitles && !reflect.DeepEqual(current.Item.Subtitles, complete.Item.Subtitles) || !includeSubtitles && len(current.Item.Subtitles) != 0 {
			t.Fatal("optional current subtitle metadata did not preserve the complete source contract")
		}
	}
	// Removing a bound sidecar is observable on the next revalidation. Owned
	// captions remain independently bound to the complete primary source stamp.
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_subtitles SET active=false WHERE item_id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	file, current, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, subject, fixture.item.ID, complete.SourceID, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if len(current.Item.Subtitles) != 1 || !current.Item.Subtitles[0].Owned {
		t.Fatal("revalidation retained inactive sidecar facts or lost current owned caption facts")
	}
	for _, allowed := range []bool{true, false} {
		tag := "Allowed"
		if !allowed {
			tag = "Other"
		}
		if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=jsonb_build_object('EnableAllFolders',true,'IncludeTags',jsonb_build_array($2::text)) WHERE id=$1`, fixture.userID, tag); err != nil {
			t.Fatal(err)
		}
		file, _, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, subject, fixture.item.ID, complete.SourceID, false)
		if file != nil {
			_ = file.Close()
		}
		if allowed && (err != nil || file == nil) || !allowed && (!errors.Is(err, ErrNotFound) || file != nil) {
			t.Fatalf("omitting entity projection changed the current tag policy: allowed=%t error=%v", allowed, err)
		}
	}
}

func TestMediaRevalidationExecutesNarrowSQLAndOnlyRequestedSubtitleReads(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
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
	ctx := context.WithValue(fixture.ctx, mediaRevalidationPerformanceContextKey{}, true)
	subject := Subject{UserID: fixture.userID}
	file, _, err := fixture.store.OpenMediaFor(ctx, subject, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if trace.entities.Load() != 1 || trace.subtitles.Load() != 2 {
		t.Fatalf("the complete API lost entity or caption projection queries: entities=%d subtitles=%d", trace.entities.Load(), trace.subtitles.Load())
	}
	fullQueries := trace.queries.Load()
	for _, includeSubtitles := range []bool{false, true} {
		trace.reset()
		file, _, err := fixture.store.RevalidateMediaSourceFor(ctx, subject, fixture.item.ID, "", includeSubtitles)
		if err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		if trace.sources.Load() != 1 || trace.entities.Load() != 0 {
			t.Fatal("revalidation executed the full catalog entity projection")
		}
		for _, fragment := range []string{"jsonb_agg", "catalog_entities", "item_metadata_state", "album_ancestors", "tv_parent", "tv_series", "item_intro_state"} {
			if strings.Contains(trace.statement, fragment) {
				t.Fatalf("revalidation executed unused catalog projection %q", fragment)
			}
		}
		wantSubtitles, wantQueries := int64(0), fullQueries-2
		if includeSubtitles {
			wantSubtitles, wantQueries = 2, fullQueries
		}
		if trace.subtitles.Load() != wantSubtitles || trace.queries.Load() != wantQueries {
			t.Fatalf("revalidation read unnecessary caption queries: include=%t queries=%d subtitles=%d", includeSubtitles, trace.queries.Load(), trace.subtitles.Load())
		}
	}
}

func TestMediaRevalidationReadsCurrentOrdinaryAndApplicationAuthority(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	key := seedCatalogApplicationKey(t, fixture.ctx, fixture.pool, "media-revalidation-key", true)
	ordinary := Subject{UserID: fixture.userID}
	open := func(subject Subject, want error) {
		t.Helper()
		file, source, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, subject, fixture.item.ID, media.SourceID(fixture.item.ID), false)
		if file != nil {
			_ = file.Close()
		}
		if want == nil {
			if err != nil || file == nil || source.Item.ID != fixture.item.ID {
				t.Fatalf("authorized revalidation failed: subject=%+v error=%v", subject, err)
			}
		} else if !errors.Is(err, want) || file != nil || !reflect.DeepEqual(source, MediaFile{}) {
			t.Fatalf("revoked authority received a source: subject=%+v error=%v want=%v", subject, err, want)
		}
	}
	open(ordinary, nil)
	open(key, nil)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy='{"EnableAllFolders":true,"EnableMediaPlayback":false}' WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	open(ordinary, ErrForbidden)
	// An application key retains its own playback authority when the target
	// user's playback permission is disabled, while target visibility applies.
	key.UserID = fixture.userID
	open(key, nil)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id=$1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	open(key, ErrNotFound)
	key.UserID = ""
	open(key, nil)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	open(key, ErrForbidden)
}

func TestMediaRevalidationRetainsPathAndProbeSnapshotValidation(t *testing.T) {
	for _, test := range []struct {
		name, statement string
	}{
		{"item path", `UPDATE items SET path='/unrelated/movie.mkv' WHERE id=$1`},
		{"relative traversal", `UPDATE items SET relative_path='../movie.mkv' WHERE id=$1`},
		{"identity", `UPDATE items SET file_identity='' WHERE id=$1`},
		{"size", `UPDATE items SET file_size=0 WHERE id=$1`},
		{"modified", `UPDATE items SET modified_at=NULL WHERE id=$1`},
		{"probe version", `UPDATE items SET media=jsonb_set(media,'{ProbeVersion}','0') WHERE id=$1`},
		{"change time", `UPDATE items SET media=media-'FileChangeTimeNs' WHERE id=$1`},
		{"root", `UPDATE library_roots SET path='/unrelated/root' WHERE id=(SELECT root_id FROM items WHERE id=$1)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.item.ID); err != nil {
				t.Fatal(err)
			}
			file, _, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "", false)
			if file != nil {
				_ = file.Close()
			}
			if !errors.Is(err, ErrUnavailable) || file != nil {
				t.Fatalf("invalid source snapshot passed revalidation: %v", err)
			}
		})
	}
}

func TestMediaRevalidationRejectsReplacedPathnameAndRestoredModificationTime(t *testing.T) {
	for _, change := range []string{"inode", "write", "symlink"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			file, _, err := fixture.store.OpenMedia(fixture.ctx, fixture.userID, fixture.item.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			before, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if change != "write" {
				if err := os.Rename(fixture.path, fixture.path+".original"); err != nil {
					t.Fatal(err)
				}
			}
			if change == "symlink" {
				if err := os.Symlink(fixture.path+".original", fixture.path); err != nil {
					t.Fatal(err)
				}
			} else {
				data := []byte(fixture.contents)
				data[len(data)-1] ^= 1
				if err := os.WriteFile(fixture.path, data, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(fixture.path, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
			defer cancel()
			verified, _, err := fixture.store.RevalidateMediaSourceFor(ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "", false)
			if verified != nil {
				_ = verified.Close()
			}
			if !errors.Is(err, ErrUnavailable) || verified != nil {
				t.Fatalf("revalidation trusted the retained descriptor instead of the current pathname: %v", err)
			}
		})
	}
}

func TestMediaRevalidationRetainsPublicationBarrierBeforeAndAfterOpening(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	snapshot, err := fixture.store.readMediaRevalidationFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "", false)
	if err != nil {
		t.Fatal(err)
	}
	opID := publicationReadTestReservation(t, fixture, "prepared")
	file, _, err := fixture.store.RevalidateMediaSourceFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, "", false)
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, ErrBusy) || file != nil {
		t.Fatalf("revalidation crossed an active publication: %v", err)
	}
	if file, err := fixture.store.openPublicMediaSource(fixture.ctx, snapshot); !errors.Is(err, ErrBusy) || file != nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("authorized revalidation snapshot crossed the post-open publication barrier: %v", err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE media_operations SET state='completed',publication_phase='done' WHERE id=$1`, opID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media,'{DurationTicks}',to_jsonb((media->>'DurationTicks')::bigint+1)) WHERE id=$1`, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if file, err := fixture.store.openPublicMediaSource(fixture.ctx, snapshot); !errors.Is(err, ErrSourceChanged) || file != nil {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("revalidation omitted the complete catalog publication revision: %v", err)
	}
	// Keep the expected registered location explicit, even for small fixture data.
	if snapshot.mediaFile.Item.Path != filepath.Join(snapshot.root.path, snapshot.relativePath) {
		t.Fatal("revalidation did not retain the registered primary pathname")
	}
}
