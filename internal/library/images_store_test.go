//go:build linux

package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestImageListsEnforceUserPoliciesAndKeepBinaryPublic(t *testing.T) {
	ctx, pool, store, allowed, unrestricted := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible := imageStoreTestRoot(t, ctx, pool, store, allowed, "visible")
	hidden := imageStoreTestRoot(t, ctx, pool, store, allowed, "hidden")
	data := imageStoreTestPNG(t)
	primary := imageStoreTestInsert(t, ctx, pool, visible, "visible-item", "Primary", 0, "primary.png", data)
	backdrop := imageStoreTestInsert(t, ctx, pool, visible, "visible-item", "Backdrop", 3, "backdrop.png", data)
	secret := imageStoreTestInsert(t, ctx, pool, hidden, "hidden-item", "Primary", 0, "secret.png", data)
	libraryIntegrationUser(t, ctx, pool, "image-restricted", false, false, []string{visible.libraryID})
	libraryIntegrationUser(t, ctx, pool, "image-none", false, false, nil)
	libraryIntegrationUser(t, ctx, pool, "image-disabled", true, true, nil)
	libraryIntegrationUser(t, ctx, pool, "image-admin", false, false, nil)
	if _, err := pool.Exec(ctx, "UPDATE users SET is_administrator = true WHERE id = 'image-admin'"); err != nil {
		t.Fatal(err)
	}

	images, err := store.ListImages(ctx, "image-restricted", "visible-item")
	if err != nil || !reflect.DeepEqual(images, []Image{primary, backdrop}) {
		t.Fatalf("visible image list = %+v, %v, want primary then indexed backdrop", images, err)
	}
	for _, id := range []string{"hidden-item", "missing-item"} {
		if _, err := store.ListImages(ctx, "image-restricted", id); !errors.Is(err, ErrNotFound) {
			t.Errorf("image list for inaccessible item %q = %v, want ErrNotFound", id, err)
		}
	}
	if images, err := store.ListImages(ctx, "image-restricted", visible.libraryID); err != nil || images == nil || len(images) != 0 {
		t.Errorf("authorized item without images = %+v, %v, want an empty list", images, err)
	}
	if _, err := store.ListImages(ctx, "image-none", "visible-item"); !errors.Is(err, ErrNotFound) {
		t.Errorf("image list without folder permissions = %v, want ErrNotFound", err)
	}
	for _, user := range []string{"image-disabled", "missing-user"} {
		if _, err := store.ListImages(ctx, user, "visible-item"); !errors.Is(err, ErrForbidden) {
			t.Errorf("image list for blocked user %q = %v, want ErrForbidden", user, err)
		}
		if _, err := store.ImagesForItems(ctx, user, nil); !errors.Is(err, ErrForbidden) {
			t.Errorf("empty image batch for blocked user %q = %v, want ErrForbidden", user, err)
		}
	}
	ids := []string{"hidden-item", "visible-item", "missing-item", "visible-item", visible.libraryID}
	batch, err := store.ImagesForItems(ctx, "image-restricted", ids)
	if err != nil || !reflect.DeepEqual(batch, map[string][]Image{"visible-item": {primary, backdrop}}) {
		t.Fatalf("restricted image batch = %+v, %v, want only accessible images without duplicates", batch, err)
	}
	for _, user := range []string{unrestricted, "image-admin"} {
		batch, err := store.ImagesForItems(ctx, user, ids)
		if err != nil || len(batch) != 2 || !reflect.DeepEqual(batch["hidden-item"], []Image{secret}) {
			t.Errorf("unrestricted image batch for %q = %+v, %v", user, batch, err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":"false"}'::jsonb WHERE id = 'image-none'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImagesForItems(ctx, "image-none", ids); !errors.Is(err, ErrForbidden) {
		t.Errorf("image batch with malformed policy = %v, want ErrForbidden", err)
	}

	// Public binary reads intentionally require neither a user nor a token.
	file, public, err := store.OpenPublicImage(ctx, "hidden-item", "pRiMaRy", 0)
	if err != nil {
		t.Fatalf("public indexed image read = %v", err)
	}
	defer file.Close()
	opened, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(opened, data) || !reflect.DeepEqual(public, secret) {
		t.Errorf("public image descriptor did not return the indexed bytes from offset zero: image = %+v, error = %v", public, err)
	}
	for _, selector := range []struct {
		kind  string
		index int
	}{{"Backdrop", 0}, {"Primary", 1}, {"Disc", 0}, {"BoxRear", 31}, {"Screenshot", 1}} {
		if _, _, err := store.OpenPublicImage(ctx, "hidden-item", selector.kind, selector.index); !errors.Is(err, ErrNotFound) {
			t.Errorf("unindexed valid image selector %+v = %v, want ErrNotFound", selector, err)
		}
	}
}

func TestImageListsDoNotDecodeHiddenInvalidRecords(t *testing.T) {
	ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	visible := imageStoreTestRoot(t, ctx, pool, store, allowed, "visible")
	hidden := imageStoreTestRoot(t, ctx, pool, store, allowed, "hidden")
	want := imageStoreTestInsert(t, ctx, pool, visible, "visible-item", "Primary", 0, "visible.png", imageStoreTestPNG(t))
	imageStoreTestInsert(t, ctx, pool, hidden, "hidden-item", "Primary", 0, "hidden.png", imageStoreTestPNG(t))
	libraryIntegrationUser(t, ctx, pool, "image-reader", false, false, []string{visible.libraryID})
	if _, err := pool.Exec(ctx, "UPDATE library_roots SET path = '/unrelated/path' WHERE id = $1", hidden.id); err != nil {
		t.Fatal(err)
	}
	batch, err := store.ImagesForItems(ctx, "image-reader", []string{"hidden-item", "visible-item"})
	if err != nil || !reflect.DeepEqual(batch, map[string][]Image{"visible-item": {want}}) {
		t.Errorf("invalid hidden root affected visible image batch: %+v, %v", batch, err)
	}
	if _, err := store.ListImages(ctx, "image-reader", "hidden-item"); !errors.Is(err, ErrNotFound) {
		t.Errorf("invalid hidden record exposed its decoding error: %v", err)
	}
}

func TestImagesRejectMismatchedItemAndImageRoots(t *testing.T) {
	for _, corruption := range []string{"image-root", "item-root", "both-roots", "same-library-other-root"} {
		t.Run(corruption, func(t *testing.T) {
			ctx, pool, store, allowed, user := libraryIntegrationStore(t, &libraryFixtureProber{})
			visible := imageStoreTestRoot(t, ctx, pool, store, allowed, "visible")
			other := imageStoreTestRoot(t, ctx, pool, store, allowed, "other")
			imageStoreTestInsert(t, ctx, pool, visible, "item", "Primary", 0, "art.png", imageStoreTestPNG(t))
			if corruption == "same-library-other-root" {
				if _, err := pool.Exec(ctx, "UPDATE library_roots SET library_id = $1 WHERE id = $2", visible.libraryID, other.id); err != nil {
					t.Fatal(err)
				}
			}
			if corruption != "item-root" {
				if _, err := pool.Exec(ctx, "UPDATE item_images SET root_id = $1 WHERE item_id = 'item'", other.id); err != nil {
					t.Fatal(err)
				}
			}
			if corruption == "item-root" || corruption == "both-roots" {
				if _, err := pool.Exec(ctx, "UPDATE items SET root_id = $1 WHERE id = 'item'", other.id); err != nil {
					t.Fatal(err)
				}
			}
			if images, err := store.ListImages(ctx, user, "item"); err != nil || len(images) != 0 {
				t.Errorf("mismatched root entered image list: %+v, %v", images, err)
			}
			if batch, err := store.ImagesForItems(ctx, user, []string{"item"}); err != nil || len(batch) != 0 {
				t.Errorf("mismatched root entered image batch: %+v, %v", batch, err)
			}
			if file, _, err := store.OpenPublicImage(ctx, "item", "Primary", 0); !errors.Is(err, ErrNotFound) {
				if file != nil {
					file.Close()
				}
				t.Errorf("mismatched root opened a public image: %v", err)
			}
		})
	}
}

func TestPublicImageRejectsContentMutationWithRestoredMetadata(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		name := "same-inode-content-change"
		if replacement {
			name = "different-inode-same-content"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			root := imageStoreTestRoot(t, ctx, pool, store, allowed, "images")
			data := imageStoreTestPNG(t)
			indexed := imageStoreTestInsert(t, ctx, pool, root, "item", "Primary", 0, "art.png", data)
			before, err := os.Stat(indexed.Path)
			if err != nil {
				t.Fatal(err)
			}
			if replacement {
				if err := os.Rename(indexed.Path, indexed.Path+".original"); err != nil {
					t.Fatal(err)
				}
			} else {
				data[len(data)-1] ^= 0xff
			}
			if err := os.WriteFile(indexed.Path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(indexed.Path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(indexed.Path)
			if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || os.SameFile(before, after) == replacement {
				t.Fatalf("mutation fixture did not preserve the intended metadata: %v", err)
			}
			if file, _, err := store.OpenPublicImage(ctx, "item", "Primary", 0); !errors.Is(err, ErrUnavailable) {
				if file != nil {
					file.Close()
				}
				t.Errorf("changed image was returned despite matching size and mtime: %v", err)
			}
		})
	}
}

func TestPublicImageRejectsSymbolicLinksAtEveryLevel(t *testing.T) {
	for _, level := range []string{"leaf", "ancestor", "registered-root"} {
		t.Run(level, func(t *testing.T) {
			ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			root := imageStoreTestRoot(t, ctx, pool, store, allowed, "images")
			indexed := imageStoreTestInsert(t, ctx, pool, root, "item", "Primary", 0, "nested/art.png", imageStoreTestPNG(t))
			path := indexed.Path
			if level == "ancestor" {
				path = filepath.Dir(path)
			} else if level == "registered-root" {
				path = root.path
			}
			original := path + ".original"
			if err := os.Rename(path, original); err != nil {
				t.Fatal(err)
			}
			// The link still resolves to the exact original inode and bytes. A
			// hash-only or final-component-only guard would incorrectly accept it.
			if err := os.Symlink(original, path); err != nil {
				t.Fatal(err)
			}
			if file, _, err := store.OpenPublicImage(ctx, "item", "Primary", 0); !errors.Is(err, ErrUnavailable) {
				if file != nil {
					file.Close()
				}
				t.Errorf("symbolic link at %s opened an image: %v", level, err)
			}
		})
	}
}

func TestPublicImageRejectsMissingOversizedAndNonregularFiles(t *testing.T) {
	for _, kind := range []string{"missing", "oversized", "directory", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			ctx, pool, store, allowed, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			root := imageStoreTestRoot(t, ctx, pool, store, allowed, "images")
			indexed := imageStoreTestInsert(t, ctx, pool, root, "item", "Primary", 0, "art.png", imageStoreTestPNG(t))
			if kind == "oversized" {
				if err := os.Truncate(indexed.Path, storedImageReadLimit+1); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(indexed.Path); err != nil {
					t.Fatal(err)
				}
				if kind == "directory" {
					if err := os.Mkdir(indexed.Path, 0700); err != nil {
						t.Fatal(err)
					}
				} else if kind == "fifo" {
					if err := syscall.Mkfifo(indexed.Path, 0600); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if descriptor, err := syscall.Open(indexed.Path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
							_ = syscall.Close(descriptor)
						}
					})
				}
			}
			result := make(chan error, 1)
			go func() {
				file, _, err := store.OpenPublicImage(ctx, "item", "Primary", 0)
				if file != nil {
					file.Close()
				}
				result <- err
			}()
			select {
			case err := <-result:
				if !errors.Is(err, ErrUnavailable) {
					t.Errorf("%s image read = %v, want ErrUnavailable", kind, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("%s image blocked the request", kind)
			}
		})
	}
}

func TestImageReadInputsAreBounded(t *testing.T) {
	var store *Store
	ctx := context.Background()
	for _, id := range []string{"", " \t", "item\x00suffix"} {
		if _, err := store.ListImages(ctx, "user", id); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid list item identifier %q = %v", id, err)
		}
		if _, err := store.ImagesForItems(ctx, "user", []string{id}); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid batch item identifier %q = %v", id, err)
		}
		if _, _, err := store.OpenPublicImage(ctx, id, "Primary", 0); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid public item identifier %q = %v", id, err)
		}
	}
	if _, err := store.ImagesForItems(ctx, "user", make([]string, 1001)); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("oversized image batch = %v, want ErrInvalidInput", err)
	}
	for _, input := range []struct {
		kind  string
		index int
	}{{"", 0}, {"Backdrop", -1}, {"Backdrop", 32}, {"Chapter", 0}, {"Primary\x00", 0}} {
		if _, _, err := store.OpenPublicImage(ctx, "item", input.kind, input.index); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid image selector %+v = %v", input, err)
		}
	}
	for _, kind := range []string{"Primary", "Backdrop", "Thumb", "Banner", "Logo", "Art", "Disc", "Box", "BoxRear", "Menu", "Screenshot"} {
		if _, _, err := store.OpenPublicImage(ctx, "item", kind, 31); !errors.Is(err, ErrUnavailable) {
			t.Errorf("valid selector %s/31 = %v, want ErrUnavailable from the absent store", kind, err)
		}
	}
}

func TestStoredImagePreservesCancellationErrors(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if _, err := scanStoredImage(imageStoreErrorRow{err: cause}); !errors.Is(err, cause) || !errors.Is(err, ErrUnavailable) {
			t.Errorf("database cancellation cause was not preserved for HTTP handling: %v", err)
		}
	}
}

type imageStoreErrorRow struct{ err error }

func (row imageStoreErrorRow) Scan(...any) error { return row.err }

func TestStoredImageRejectsCorruptPathsAndMetadata(t *testing.T) {
	valid := storedImage{
		Image:        Image{ImageType: "Primary", Tag: strings.Repeat("a", 64), MIMEType: "image/png", Width: 2, Height: 2, Size: 123, ModifiedAt: time.Now()},
		root:         libraryRoot{path: "/srv/media", allowedPath: "/srv", relativePath: "media"},
		relativePath: "nested/art.png", identity: "1:2",
	}
	for _, path := range []string{"", ".", "../art.png", "nested/../../art.png", "/art.png", "nested/./art.png", "nested//art.png", "nested/../art.png", `nested\art.png`, "art.png\x00", "art.svg"} {
		t.Run("path-"+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
			invalid := valid
			invalid.relativePath = path
			if err := validateStoredImage(&invalid); !errors.Is(err, ErrUnavailable) {
				t.Errorf("corrupt stored path %q = %v", path, err)
			}
		})
	}
	for name, mutate := range map[string]func(*storedImage){
		"mismatched-root-path":   func(value *storedImage) { value.root.path = "/srv/other" },
		"absolute-root-relative": func(value *storedImage) { value.root.relativePath = "/media" },
		"traversing-root":        func(value *storedImage) { value.root.relativePath = "../media" },
		"empty-identity":         func(value *storedImage) { value.identity = "" },
		"oversized":              func(value *storedImage) { value.Size = storedImageReadLimit + 1 },
		"empty-file":             func(value *storedImage) { value.Size = 0 },
		"oversized-dimensions":   func(value *storedImage) { value.Width = 16385 },
		"oversized-pixel-count":  func(value *storedImage) { value.Width, value.Height = 6000, 6000 },
		"untrusted-mime":         func(value *storedImage) { value.MIMEType = "image/svg+xml" },
		"invalid-hash":           func(value *storedImage) { value.Tag = strings.Repeat("x", 64) },
		"uppercase-hash":         func(value *storedImage) { value.Tag = strings.Repeat("A", 64) },
		"invalid-index":          func(value *storedImage) { value.ImageIndex = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			mutate(&invalid)
			if err := validateStoredImage(&invalid); !errors.Is(err, ErrUnavailable) {
				t.Errorf("corrupt stored metadata %q = %v", name, err)
			}
		})
	}
	if err := validateStoredImage(&valid); err != nil || valid.Path != "/srv/media/nested/art.png" || valid.Filename != "art.png" {
		t.Errorf("valid record did not safely derive its display path: %+v, %v", valid, err)
	}
}

func imageStoreTestRoot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, approvedPath, name string) libraryRoot {
	t.Helper()
	path := filepath.Join(approvedPath, name)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	library := libraryIntegrationCreate(t, ctx, store, name, "movies", path)
	var root libraryRoot
	if err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path
		FROM library_roots WHERE library_id = $1`, library.ID).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
		t.Fatal(err)
	}
	return root
}

func imageStoreTestInsert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, root libraryRoot, itemID, imageType string, index int, relative string, data []byte) Image {
	t.Helper()
	path := filepath.Join(root.path, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	image := Image{ImageType: imageType, ImageIndex: index, Path: path, Filename: filepath.Base(path),
		Tag: hex.EncodeToString(digest[:]), MIMEType: "image/png", Width: 2, Height: 2,
		Size: info.Size(), ModifiedAt: catalogModifiedTime(info)}
	if _, err := pool.Exec(ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, relative_path)
		VALUES ($1, $2, $3, $2, $1, $1, 'Movie', $4) ON CONFLICT (id) DO NOTHING`,
		itemID, root.libraryID, root.id, itemID+".mp4"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_images
		(item_id, root_id, image_type, image_index, relative_path, file_identity,
		 source_hash, file_size, modified_at, width, height, mime_type)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, itemID, root.id, imageType, index,
		filepath.ToSlash(relative), fileIdentity(info), image.Tag, image.Size, image.ModifiedAt,
		image.Width, image.Height, image.MIMEType); err != nil {
		t.Fatal(err)
	}
	return image
}

func imageStoreTestPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 220, G: 80, B: 30, A: 255})
	picture.Set(1, 1, color.RGBA{R: 20, G: 150, B: 230, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestPublicImageWorkersKeepSlotsUntilCancelledWorkFinishes(t *testing.T) {
	directory := t.TempDir()
	slots := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	var startedCount atomic.Int32
	started := make(chan struct{}, 4)
	results := make(chan imageStoreWorkerResult, 4)
	files := make([]*os.File, 0, 4)
	cancels := make([]context.CancelFunc, 0, 4)
	t.Cleanup(func() {
		for _, cancel := range cancels {
			cancel()
		}
		releaseOnce.Do(func() { close(release) })
		if !imageStoreWaitWorkerCleanup(slots) {
			t.Error("cancelled image workers did not release their slots during cleanup")
		}
		for _, file := range files {
			_ = file.Close()
		}
	})
	for range 4 {
		file, err := os.CreateTemp(directory, "late-image-*.png")
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		go func() {
			opened, image, err := runPublicImageWorker(ctx, slots, func() (*os.File, Image, error) {
				startedCount.Add(1)
				started <- struct{}{}
				<-release
				return file, Image{ImageType: "Primary"}, nil
			})
			results <- imageStoreWorkerResult{file: opened, image: image, err: err}
		}()
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("the four image workers did not enter their blocking operations")
		}
	}
	for _, cancel := range cancels {
		cancel()
	}
	for range 4 {
		result := imageStoreReceiveWorkerResult(t, results)
		if result.file != nil {
			_ = result.file.Close()
			t.Error("cancelled image request received a descriptor")
		}
		if !errors.Is(result.err, context.Canceled) {
			t.Errorf("cancelled image request error = %v, want context.Canceled", result.err)
		}
	}
	if len(slots) != 4 || startedCount.Load() != 4 {
		t.Fatalf("cancelled callers released blocked workers: slots = %d, started = %d", len(slots), startedCount.Load())
	}

	for round := range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		queued := make(chan imageStoreWorkerResult, 4)
		for range 4 {
			go func() {
				file, image, err := runPublicImageWorker(ctx, slots, func() (*os.File, Image, error) {
					startedCount.Add(1)
					return nil, Image{}, nil
				})
				queued <- imageStoreWorkerResult{file: file, image: image, err: err}
			}()
		}
		for range 4 {
			result := imageStoreReceiveWorkerResult(t, queued)
			if result.file != nil {
				_ = result.file.Close()
				t.Errorf("queued request in round %d unexpectedly received a descriptor", round)
			}
			if !errors.Is(result.err, context.DeadlineExceeded) {
				t.Errorf("queued request in round %d error = %v, want context.DeadlineExceeded", round, result.err)
			}
		}
		cancel()
		if len(slots) != 4 || startedCount.Load() != 4 {
			t.Fatalf("queued requests in round %d bypassed retained slots: slots = %d, started = %d", round, len(slots), startedCount.Load())
		}
	}

	releaseOnce.Do(func() { close(release) })
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("released image workers did not finish descriptor cleanup")
	}
	if startedCount.Load() != 4 {
		t.Errorf("expired queued work started after capacity became available: started = %d", startedCount.Load())
	}
	for _, file := range files {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("late image descriptor remained open after its worker released the slot: %v", err)
		}
	}
}

func TestPublicImageWorkerDeliversOpenDescriptorWithoutChangingOffset(t *testing.T) {
	slots := make(chan struct{}, 4)
	file, err := os.CreateTemp(t.TempDir(), "delivered-image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("indexed-image"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	want := Image{ImageType: "Primary", ImageIndex: 0, Tag: "fixture-tag", Size: 13}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opened, got, err := runPublicImageWorker(ctx, slots, func() (*os.File, Image, error) {
		return file, want, nil
	})
	if opened != nil && opened != file {
		defer opened.Close()
	}
	if err != nil || opened != file || !reflect.DeepEqual(got, want) {
		t.Fatalf("successful worker result = %v, %+v, %v; want the original descriptor and metadata", opened, got, err)
	}
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("successful image worker did not release its slot")
	}
	if _, err := opened.Stat(); err != nil {
		t.Fatalf("successful worker closed its delivered descriptor: %v", err)
	}
	if offset, err := opened.Seek(0, io.SeekCurrent); err != nil || offset != 3 {
		t.Fatalf("successful worker changed descriptor offset: offset = %d, error = %v", offset, err)
	}
	if data, err := io.ReadAll(opened); err != nil || string(data) != "exed-image" {
		t.Errorf("delivered image descriptor is not readable from its original offset: %q, %v", data, err)
	}
}

func TestPublicImageWorkerClosesDescriptorsReturnedWithErrors(t *testing.T) {
	slots := make(chan struct{}, 4)
	file, err := os.CreateTemp(t.TempDir(), "failed-image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cause := errors.New("image fixture read failed")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opened, _, err := runPublicImageWorker(ctx, slots, func() (*os.File, Image, error) {
		return file, Image{ImageType: "Primary"}, cause
	})
	if opened != nil {
		_ = opened.Close()
		t.Error("failed image worker delivered its descriptor")
	}
	if !errors.Is(err, cause) {
		t.Errorf("failed image worker error = %v, want the original error", err)
	}
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("failed image worker did not release its slot")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("failed image worker leaked its descriptor: %v", err)
	}
}

func TestPublicImageWorkerClosesResultsCancelledBeforeDelivery(t *testing.T) {
	slots := make(chan struct{}, 4)
	file, err := os.CreateTemp(t.TempDir(), "cancelled-image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan imageStoreWorkerResult, 1)
	go func() {
		opened, image, err := runPublicImageWorker(ctx, slots, func() (*os.File, Image, error) {
			cancel()
			return file, Image{ImageType: "Primary"}, nil
		})
		results <- imageStoreWorkerResult{file: opened, image: image, err: err}
	}()
	result := imageStoreReceiveWorkerResult(t, results)
	if result.file != nil {
		_ = result.file.Close()
		t.Error("a result cancelled before delivery exposed its descriptor")
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Errorf("result cancelled before delivery error = %v, want context.Canceled", result.err)
	}
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("cancelled image delivery did not finish cleanup")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("cancelled image delivery leaked its descriptor: %v", err)
	}
}

type imageStoreWorkerResult struct {
	file  *os.File
	image Image
	err   error
}

func imageStoreReceiveWorkerResult(t *testing.T, results <-chan imageStoreWorkerResult) imageStoreWorkerResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("image worker caller did not return within the bounded wait")
		return imageStoreWorkerResult{}
	}
}

// Acquiring every slot is a barrier after the workers' deferred cleanup. The
// temporary test tokens are released before returning, including timeout paths.
func imageStoreWaitWorkerCleanup(slots chan struct{}) bool {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	held := 0
	defer func() {
		for range held {
			<-slots
		}
	}()
	for held < cap(slots) {
		select {
		case slots <- struct{}{}:
			held++
		case <-timer.C:
			return false
		}
	}
	return true
}
