//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestOpenDownloadUsesOnlyContentDownloadingPermission(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	for _, administrator := range []bool{false, true} {
		for _, test := range []struct {
			name, policy string
			want         error
		}{
			{"missing-default", `{"EnableAllFolders":true}`, nil},
			{"explicit-true", `{"EnableAllFolders":true,"EnableContentDownloading":true}`, nil},
			{"explicit-false", `{"EnableAllFolders":true,"EnableContentDownloading":false}`, ErrForbidden},
			{"null", `{"EnableAllFolders":true,"EnableContentDownloading":null}`, ErrForbidden},
			{"string", `{"EnableAllFolders":true,"EnableContentDownloading":"true"}`, ErrForbidden},
			{"number", `{"EnableAllFolders":true,"EnableContentDownloading":1}`, ErrForbidden},
			{"array", `{"EnableAllFolders":true,"EnableContentDownloading":[]}`, ErrForbidden},
			{"object", `{"EnableAllFolders":true,"EnableContentDownloading":{}}`, ErrForbidden},
			{"playback-disabled", `{"EnableAllFolders":true,"EnableMediaPlayback":false}`, nil},
			{"malformed-playback-does-not-disable-downloading", `{"EnableAllFolders":true,"EnableMediaPlayback":"false"}`, nil},
			{"malformed-transcoding-does-not-disable-downloading", `{"EnableAllFolders":true,"EnableVideoPlaybackTranscoding":null}`, nil},
			{"remuxing-disabled", `{"EnableAllFolders":true,"EnablePlaybackRemuxing":false}`, nil},
			{"audio-transcoding-disabled", `{"EnableAllFolders":true,"EnableAudioPlaybackTranscoding":false}`, nil},
			{"video-transcoding-disabled", `{"EnableAllFolders":true,"EnableVideoPlaybackTranscoding":false}`, nil},
			{"all-playback-disabled", `{"EnableAllFolders":true,"EnableContentDownloading":true,"EnableMediaPlayback":false,"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":false}`, nil},
		} {
			t.Run(fmt.Sprintf("administrator-%t/%s", administrator, test.name), func(t *testing.T) {
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator = $2, policy = $3::jsonb WHERE id = $1`,
					fixture.userID, administrator, test.policy); err != nil {
					t.Fatal(err)
				}
				mediaDownloadTestOpenError(t, fixture.ctx, fixture.store, Subject{UserID: fixture.userID}, fixture.item.ID, "", test.want)
			})
		}
	}
}

func TestOpenDownloadReturnsOriginalSnapshotWhilePlaybackIsDisabled(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy = policy || '{"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	var original MediaFile
	for _, sourceID := range []string{"", media.SourceID(fixture.item.ID)} {
		file, source, err := fixture.store.OpenDownloadFor(fixture.ctx, Subject{UserID: fixture.userID}, fixture.item.ID, sourceID)
		if err != nil || file == nil {
			if file != nil {
				file.Close()
			}
			t.Fatalf("open original download source %q: %v", sourceID, err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 0 {
			t.Errorf("download descriptor offset = %d, %v; want zero", offset, err)
		}
		if source.Item.ID != fixture.item.ID || source.Item.Path != fixture.path || source.Item.Media == nil || source.Item.CanPlay ||
			source.SourceID != media.SourceID(fixture.item.ID) || source.Size != info.Size() ||
			!source.ModifiedAt.Equal(catalogModifiedTime(info)) || source.ETag == "" || source.Container == "" || source.MIMEType == "" {
			t.Errorf("download lost original source metadata or granted playback: %+v", source)
		}
		data, readErr := io.ReadAll(file)
		file.Close()
		if readErr != nil || string(data) != fixture.contents {
			t.Errorf("download bytes = %q, %v; want original content", data, readErr)
		}
		if sourceID == "" {
			original = source
		} else if source.SourceID != original.SourceID || source.ETag != original.ETag || source.Size != original.Size || !source.ModifiedAt.Equal(original.ModifiedAt) {
			t.Errorf("explicit source selector changed the download snapshot: first = %+v, second = %+v", original, source)
		}
	}
	mediaSourceTestOpenError(t, fixture.ctx, fixture.store, fixture.userID, fixture.item.ID, "", ErrForbidden)
}

func TestOpenDownloadRechecksCurrentAccountAndCatalogVisibility(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, pool, store := fixture.ctx, fixture.pool, fixture.store
	subject := Subject{UserID: fixture.userID}
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", nil)
	for _, test := range []struct {
		name   string
		policy map[string]any
	}{
		{"library-scope", map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{}}},
		{"excluded-item", map[string]any{"ExcludedSubFolders": []string{fixture.item.ID}}},
		{"excluded-ancestor", map[string]any{"ExcludedSubFolders": []string{fixture.item.ParentID}}},
		{"excluded-path", map[string]any{"ExcludedSubFolders": []string{filepath.Dir(fixture.path)}}},
		{"include-tags", map[string]any{"IncludeTags": []string{"Missing tag"}}},
		{"unrated-movie", map[string]any{"BlockUnratedItems": []string{"Movie"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := map[string]any{"EnableAllFolders": true, "EnableContentDownloading": true}
			for name, value := range test.policy {
				policy[name] = value
			}
			raw, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", fixture.userID, raw); err != nil {
				t.Fatal(err)
			}
			mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrNotFound)
		})
	}
	actor := metadataEditTestActor(t, ctx, pool, "download-policy-editor")
	before := metadataEditTestDetail(t, ctx, store, actor, fixture.item.ParentID)
	overrides := metadataEditTestCopy(before.Overrides)
	overrides["OfficialRating"] = metadataEditTestRaw(t, "R")
	overrides["Tags"] = metadataEditTestRaw(t, []string{"Adults"})
	updated := metadataEditTestUpdate(t, ctx, store, actor, before, overrides, before.LockedFields)
	if updated.Effective.OfficialRating != "R" || !reflect.DeepEqual(updated.Effective.Tags, []string{"Adults"}) {
		t.Fatalf("download fixture did not publish effective parent restrictions: %+v", updated.Effective)
	}
	for _, policy := range []string{
		`{"EnableAllFolders":true,"MaxParentalRating":5}`,
		`{"EnableAllFolders":true,"BlockedTags":["aDuLtS"]}`,
	} {
		if _, err := pool.Exec(ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", fixture.userID, policy); err != nil {
			t.Fatal(err)
		}
		mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrNotFound)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":true}'::jsonb WHERE id = $1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", nil)
	mediaDownloadTestOpenError(t, ctx, store, subject, "missing-item", "", ErrNotFound)
	mediaDownloadTestOpenError(t, ctx, store, Subject{UserID: "missing-user"}, fixture.item.ID, "", ErrForbidden)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_disabled = true, is_administrator = true WHERE id = $1", fixture.userID); err != nil {
		t.Fatal(err)
	}
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrForbidden)
}

func TestApplicationKeyDownloadHasIndependentAuthorityAndRevalidatesRevocation(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	ctx, pool, store := fixture.ctx, fixture.pool, fixture.store
	subject := seedCatalogApplicationKey(t, ctx, pool, "download-application-key", true)
	if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled = true, policy = policy ||
		'{"EnableContentDownloading":false,"EnableMediaPlayback":false,"EnablePlaybackRemuxing":false,"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":false}'::jsonb
		WHERE id = $1`, fixture.userID); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"", fixture.userID} {
		subject.UserID = target
		mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", nil)
		mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "unrelated-source", ErrNotFound)
	}
	for _, policy := range []string{
		`{"EnableAllFolders":false,"EnabledFolders":[],"EnableContentDownloading":false}`,
		fmt.Sprintf(`{"EnableAllFolders":true,"ExcludedSubFolders":[%q],"EnableContentDownloading":false}`, fixture.item.ID),
	} {
		if _, err := pool.Exec(ctx, "UPDATE users SET policy = $2::jsonb WHERE id = $1", fixture.userID, policy); err != nil {
			t.Fatal(err)
		}
		mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrNotFound)
	}
	subject.UserID = "missing-target"
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrNotFound)
	subject.UserID = ""
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", nil)
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrForbidden)
	if _, err := pool.Exec(ctx, "UPDATE sessions SET revoked_at = NULL WHERE id = $1", subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE items SET file_identity = 'stale-indexed-identity' WHERE id = $1", fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	mediaDownloadTestOpenError(t, ctx, store, subject, fixture.item.ID, "", ErrUnavailable)
	orphan := seedCatalogApplicationKey(t, ctx, pool, "download-orphan-key", false)
	mediaDownloadTestOpenError(t, ctx, store, orphan, fixture.item.ID, "", ErrForbidden)
	mediaDownloadTestOpenError(t, ctx, store, Subject{ApplicationCredentialID: "missing-key"}, fixture.item.ID, "", ErrForbidden)
}

func TestOpenDownloadRejectsUnsafeSourceSelectorsAndStoredPaths(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	subject := Subject{UserID: fixture.userID}
	for _, sourceID := range []string{
		media.SourceID("another-item"), fixture.item.ID, fixture.path, "../Feature.mkv",
		media.SourceID(fixture.item.ID) + ".mp4", "https://example.invalid/movie.mkv",
	} {
		mediaDownloadTestOpenError(t, fixture.ctx, fixture.store, subject, fixture.item.ID, sourceID, ErrNotFound)
	}
	mediaDownloadTestOpenError(t, fixture.ctx, fixture.store, subject, fixture.item.ID, "invalid\x00source", ErrInvalidInput)
	for _, test := range []struct {
		name, statement string
	}{
		{"stale-identity", "UPDATE items SET file_identity = 'stale-indexed-identity' WHERE id = $1"},
		{"relative-traversal", "UPDATE items SET relative_path = '../Feature.mkv' WHERE id = $1"},
		{"unrelated-item-path", "UPDATE items SET path = '/unrelated/Feature.mkv' WHERE id = $1"},
		{"mismatched-root-path", "UPDATE library_roots SET path = '/unrelated/root' WHERE id = (SELECT root_id FROM items WHERE id = $1)"},
		{"legacy-probe-version", "UPDATE items SET media = jsonb_set(media, '{ProbeVersion}', '0'::jsonb) WHERE id = $1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.item.ID); err != nil {
				t.Fatal(err)
			}
			mediaDownloadTestOpenError(t, fixture.ctx, fixture.store, Subject{UserID: fixture.userID}, fixture.item.ID, "", ErrUnavailable)
		})
	}
}

func TestOpenDownloadRejectsChangedFilesAndSymbolicLinks(t *testing.T) {
	for _, change := range []string{"size", "mtime", "inode", "missing", "leaf-link", "ancestor-link", "root-link"} {
		t.Run(change, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			before, err := os.Stat(fixture.path)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "size":
				err = os.Truncate(fixture.path, before.Size()+1)
			case "mtime":
				modified := before.ModTime().Add(2 * time.Second)
				err = os.Chtimes(fixture.path, modified, modified)
			case "inode":
				if err = os.Rename(fixture.path, fixture.path+".original"); err == nil {
					err = os.WriteFile(fixture.path, []byte(fixture.contents), 0600)
				}
				if err == nil {
					err = os.Chtimes(fixture.path, before.ModTime(), before.ModTime())
				}
			case "missing":
				err = os.Remove(fixture.path)
			default:
				path := fixture.path
				if change == "ancestor-link" {
					path = filepath.Dir(path)
				} else if change == "root-link" {
					path = fixture.library.Paths[0]
				}
				if err = os.Rename(path, path+".original"); err == nil {
					err = os.Symlink(path+".original", path)
				}
			}
			if err != nil {
				t.Fatalf("create %s download source replacement: %v", change, err)
			}
			mediaDownloadTestOpenError(t, fixture.ctx, fixture.store, Subject{UserID: fixture.userID}, fixture.item.ID, "", ErrUnavailable)
		})
	}
}

func mediaDownloadTestOpenError(t *testing.T, ctx context.Context, store *Store, subject Subject, itemID, sourceID string, want error) {
	t.Helper()
	var file *os.File
	var source MediaFile
	var err error
	if subject.ApplicationCredentialID == "" {
		file, source, err = store.OpenDownload(ctx, subject.UserID, itemID, sourceID)
	} else {
		file, source, err = store.OpenDownloadFor(ctx, subject, itemID, sourceID)
	}
	if file != nil {
		defer file.Close()
	}
	if want == nil {
		if err != nil || file == nil || source.Item.ID != itemID || source.SourceID != media.SourceID(itemID) {
			t.Errorf("open download for %+v, item %q, source %q = %v, %+v, %v; want an indexed source", subject, itemID, sourceID, file, source, err)
		}
		return
	}
	if !errors.Is(err, want) || file != nil || !reflect.DeepEqual(source, MediaFile{}) {
		t.Errorf("open download for %+v, item %q, source %q = %v, %+v, %v; want no descriptor or source and %v", subject, itemID, sourceID, file, source, err, want)
	}
}
