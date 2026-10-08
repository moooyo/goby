//go:build linux

package library

import (
	"context"
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

func holdSidecarAnchorPublicationGate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) pgx.Tx {
	t.Helper()
	statement := `CREATE FUNCTION sidecar_anchor_publication_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(hashtextextended(current_schema() || ':sidecar-anchor-gate', 0));
			RETURN NULL;
		END; $$;
		CREATE TRIGGER sidecar_anchor_publication_gate BEFORE INSERT OR UPDATE OR DELETE ON ` + pgx.Identifier{table}.Sanitize() + `
		FOR EACH STATEMENT EXECUTE FUNCTION sidecar_anchor_publication_gate()`
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatal(err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema() || ':sidecar-anchor-gate', 0))`); err != nil {
		rollback(gate)
		t.Fatal(err)
	}
	return gate
}

func sidecarAnchorCatalogSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, itemID string) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(entry)||jsonb_build_object('tuple_version',entry.xmin::text)
		ORDER BY to_jsonb(entry)::text),'[]'::jsonb)::text FROM `+pgx.Identifier{table}.Sanitize()+` entry WHERE item_id=$1`, itemID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestSidecarPublicationRetainsOriginalAnchorForInsertAndRetirement(t *testing.T) {
	for _, role := range []string{"images", "text subtitles", "bitmap subtitles", "embedded artwork"} {
		for _, retire := range []bool{false, true} {
			change := "insert"
			if retire {
				change = "retire"
			}
			for _, replaceAnchor := range []bool{true, false} {
				mutation := "approval only"
				if replaceAnchor {
					mutation = "replace original anchor"
				}
				t.Run(role+"/"+change+"/"+mutation, func(t *testing.T) {
					picture := embeddedArtworkTestPicture(t, 3, "Front", color.NRGBA{R: 180, A: 255})
					prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{
						Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}}
					ctx, pool, store, allowed, userID := libraryIntegrationStore(t, prober)
					kind, filename, contents := "movies", "Feature.mkv", "video:sidecar-original-anchor"
					if role == "embedded artwork" {
						kind, filename, contents = "music", "Track.flac", "audio:sidecar-original-anchor"
					}
					registered := filepath.Join(allowed, "bridge", "registered")
					path := libraryIntegrationFile(t, allowed, filepath.Join("bridge", "registered", filename), contents)
					sidecar, table := "", "item_embedded_artwork"
					writeSidecar := func() {
						t.Helper()
						switch role {
						case "images":
							sidecar, table = filepath.Join(registered, "Feature-poster.png"), "item_images"
							imageScanTestWrite(t, sidecar, color.NRGBA{G: 180, A: 255})
						case "text subtitles":
							sidecar, table = filepath.Join(registered, "Feature.en.srt"), "item_subtitles"
							if err := os.WriteFile(sidecar, []byte(subtitleTestSRT), 0600); err != nil {
								t.Fatal(err)
							}
						case "bitmap subtitles":
							sidecar, table = filepath.Join(registered, "Feature.en.sup"), "item_bitmap_subtitles"
							if err := os.WriteFile(sidecar, bitmapCatalogTestSUP(), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
					if retire {
						writeSidecar()
					}
					library := libraryIntegrationCreate(t, ctx, store, "Sidecar original anchor", kind, registered)
					job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
					if job.Error != "" {
						t.Fatalf("initial sidecar fixture reported a warning: %+v", job)
					}
					scanPerformanceWaitWorkerRetired(t, ctx, store, job.ID)
					item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
					if !retire {
						writeSidecar()
					} else if sidecar != "" {
						if err := os.Remove(sidecar); err != nil {
							t.Fatal(err)
						}
					}
					if role == "embedded artwork" {
						if retire {
							prober.set(media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion}, nil)
							streams := make([]media.Stream, 0, len(item.Media.Streams))
							for _, stream := range item.Media.Streams {
								if !stream.IsAttachedPicture {
									streams = append(streams, stream)
								}
							}
							item.Media.Streams = streams
							encoded, err := json.Marshal(item.Media)
							if err != nil {
								t.Fatal(err)
							}
							if _, err := pool.Exec(ctx, `UPDATE items SET media=$2::jsonb WHERE id=$1`, item.ID, encoded); err != nil {
								t.Fatal(err)
							}
						} else if _, err := pool.Exec(ctx, `DELETE FROM item_embedded_artwork WHERE item_id=$1`, item.ID); err != nil {
							t.Fatal(err)
						}
					}
					state := imageScanTestState(t, ctx, pool, store, library, ".")
					if _, err := state.readPrimaryScanAuthority(ctx); err != nil {
						t.Fatal(err)
					}
					if !replaceAnchor {
						if _, err := pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1`, state.root.id); err != nil {
							t.Fatal(err)
						}
					}
					before := sidecarAnchorCatalogSnapshot(t, ctx, pool, table, item.ID)
					beforeOwners, beforeIO := originalMediaReadOwners.Stats().RegisteredOwners, originalMediaReadGovernor.Stats()
					gate := holdSidecarAnchorPublicationGate(t, ctx, pool, table)
					defer rollback(gate)
					var embeddedFile *os.File
					if role == "embedded artwork" {
						var openErr error
						embeddedFile, openErr = os.Open(path)
						if openErr != nil {
							t.Fatal(openErr)
						}
						defer embeddedFile.Close()
					}
					done := make(chan struct{})
					var scanErr error
					t.Cleanup(func() {
						state.task.cancel()
						rollback(gate)
						mediaSourceAdmissionTestWait(t, done, "sidecar anchor proof cleanup")
					})
					go func() {
						scanErr = state.retrySidecarScan(func() error {
							switch role {
							case "images":
								return state.scanImagesAttempt(item.ID, item.Type, filename, false, false)
							case "text subtitles":
								return state.scanSubtitlesAttempt(item.ID, filename, item.Media)
							case "bitmap subtitles":
								return state.scanBitmapSubtitlesAttempt(item.ID, filename, item.Media)
							default:
								return state.scanEmbeddedArtworkAttempt(item.ID, item.Type, filename, embeddedFile, *item.Media)
							}
						})
						close(done)
					}()
					taskScanWaitOwnerBlocked(t, ctx, pool, store, gate.Conn().PgConn().PID())
					if replaceAnchor {
						paths := []string{registered, path}
						if sidecar != "" && !retire {
							paths = append(paths, sidecar)
						}
						stamps := make(map[string]os.FileInfo, len(paths))
						for _, current := range paths {
							stamp, err := os.Stat(current)
							if err != nil {
								t.Fatal(err)
							}
							stamps[current] = stamp
						}
						moved := filepath.Join(t.TempDir(), "old-anchor")
						if err := os.Rename(allowed, moved); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(allowed, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.Rename(filepath.Join(moved, "bridge"), filepath.Join(allowed, "bridge")); err != nil {
							t.Fatal(err)
						}
						for _, current := range paths {
							after, err := os.Stat(current)
							if err != nil || !themeSnapshotEqual(stamps[current], after) {
								t.Fatalf("bridge move changed a registered/source snapshot: path=%s error=%v", current, err)
							}
						}
					}
					if err := gate.Commit(ctx); err != nil {
						t.Fatal(err)
					}
					mediaSourceAdmissionTestWait(t, done, "sidecar original anchor proof")
					after := sidecarAnchorCatalogSnapshot(t, ctx, pool, table, item.ID)
					if replaceAnchor {
						if scanErr == nil && state.warnings == 0 || after != before {
							t.Fatalf("sidecar accepted a substituted original anchor: error=%v warnings=%d changed=%t", scanErr, state.warnings, after != before)
						}
					} else if scanErr != nil || state.warnings != 0 || after == before {
						t.Fatalf("approval-only change blocked legitimate sidecar publication: error=%v warnings=%d changed=%t", scanErr, state.warnings, after != before)
					}
					if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners || originalMediaReadGovernor.Stats() != beforeIO {
						t.Fatalf("sidecar anchor proof retained actual ownership: owners=%d/%d IO=%+v/%+v", owners, beforeOwners, originalMediaReadGovernor.Stats(), beforeIO)
					}
				})
			}
		}
	}
}
