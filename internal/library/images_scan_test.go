package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func imageScanTestWrite(t *testing.T, path string, pixel color.Color) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			frame.Set(x, y, pixel)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	data := append(encoded.Bytes(), 'a')
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func imageScanTestState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, library Library, directory string) *scanState {
	t.Helper()
	var root libraryRoot
	err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path FROM library_roots
		WHERE library_id = $1 ORDER BY path LIMIT 1`, library.ID).
		Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := store.openLibraryRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	info, err := opened.Lstat(directory)
	if err != nil {
		t.Fatal(err)
	}
	return &scanState{store: store, task: &scanTask{ctx: ctx}, library: library, root: root,
		opened: opened, directoryIdentities: map[string]os.FileInfo{filepath.Clean(directory): info}}
}

func imageScanTestList(t *testing.T, ctx context.Context, store *Store, userID, itemID, imageType string) []Image {
	t.Helper()
	images, err := store.ListImages(ctx, userID, itemID)
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]Image, 0)
	for _, image := range images {
		if image.ImageType == imageType {
			selected = append(selected, image)
		}
	}
	return selected
}

func TestScanImagesPriorityHashRefreshFailureRetentionAndRemoval(t *testing.T) {
	fixture := &libraryFixtureProber{}
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, fixture)
	mediaPath := libraryIntegrationFile(t, approved, "movies/Film.mp4", "video:artwork")
	directory := filepath.Dir(mediaPath)
	preferred := filepath.Join(directory, "Film-poster.png")
	fallback := filepath.Join(directory, "poster.jpg")
	preferredBytes := imageScanTestWrite(t, preferred, color.NRGBA{R: 255, A: 255})
	imageScanTestWrite(t, fallback, color.NRGBA{B: 255, A: 255})
	for _, name := range []string{"backdrop10.png", "backdrop2.png", "backdrop.png"} {
		imageScanTestWrite(t, filepath.Join(directory, name), color.NRGBA{G: 255, A: 255})
	}
	library := libraryIntegrationCreate(t, ctx, store, "Artwork movies", "movies", directory)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	item := libraryIntegrationItemByPath(t, items.Items, mediaPath)
	run := func() *scanState {
		state := imageScanTestState(t, ctx, pool, store, library, ".")
		if err := state.scanImages(item.ID, item.Type, "Film.mp4", false); err != nil {
			t.Fatal(err)
		}
		return state
	}
	firstState := run()
	if firstState.warnings != 0 {
		t.Fatalf("valid images produced %d warnings", firstState.warnings)
	}
	initialIndex := firstState.imageDirectories["."]
	if initialIndex == nil {
		t.Fatal("directory candidate index was not retained for this scan")
	}
	primary := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(primary) != 1 || primary[0].Filename != "Film-poster.png" || primary[0].MIMEType != "image/png" {
		t.Fatalf("wrong preferred image: %+v", primary)
	}
	backdrops := imageScanTestList(t, ctx, store, userID, item.ID, "Backdrop")
	for index, name := range []string{"backdrop.png", "backdrop2.png", "backdrop10.png"} {
		if len(backdrops) != 3 || backdrops[index].ImageIndex != index || backdrops[index].Filename != name {
			t.Fatalf("backdrops are not indexed in numeric order: %+v", backdrops)
		}
	}
	probeCalls := len(fixture.calls())
	before, err := os.Stat(preferred)
	if err != nil {
		t.Fatal(err)
	}
	// PNG permits trailing application bytes. Change only such a byte, keeping
	// valid decoded pixels, the inode, length, and timestamp exactly unchanged.
	preferredBytes[len(preferredBytes)-1] = 'b'
	if err := os.WriteFile(preferred, preferredBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(preferred, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := firstState.scanImages(item.ID, item.Type, "Film.mp4", false); err != nil {
		t.Fatal(err)
	}
	if firstState.imageDirectories["."] != initialIndex {
		t.Fatal("unchanged directory names were indexed again")
	}
	updated := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	digest := sha256.Sum256(preferredBytes)
	if len(updated) != 1 || updated[0].Tag == primary[0].Tag || updated[0].Tag != hex.EncodeToString(digest[:]) || len(fixture.calls()) != probeCalls {
		t.Fatalf("image hashing depended on cached media/size/mtime: before=%+v after=%+v", primary, updated)
	}
	if err := os.WriteFile(preferred, []byte("corrupt image replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "backdrop2.png")); err != nil {
		t.Fatal(err)
	}
	warningsBefore := firstState.warnings
	if err := firstState.scanImages(item.ID, item.Type, "Film.mp4", false); err != nil {
		t.Fatal(err)
	}
	if firstState.warnings <= warningsBefore || len(imageScanTestList(t, ctx, store, userID, item.ID, "Backdrop")) != 3 {
		t.Fatal("a changed directory reused stale candidates or cleared previous images")
	}
	if state := run(); state.warnings == 0 {
		t.Fatal("corrupt preferred image was not reported")
	}
	retained := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(retained) != 1 || retained[0].Tag != updated[0].Tag {
		t.Fatalf("corrupt preferred image replaced the previous type: %+v", retained)
	}
	backdrops = imageScanTestList(t, ctx, store, userID, item.ID, "Backdrop")
	if len(backdrops) != 2 || backdrops[1].Filename != "backdrop10.png" || backdrops[1].ImageIndex != 1 {
		t.Fatalf("another valid type did not update atomically: %+v", backdrops)
	}
	if err := os.Remove(preferred); err != nil {
		t.Fatal(err)
	}
	run()
	selectedFallback := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(selectedFallback) != 1 || selectedFallback[0].Filename != "poster.jpg" || selectedFallback[0].MIMEType != "image/png" {
		t.Fatalf("fallback or actual image format was not respected: %+v", selectedFallback)
	}
	if err := os.Remove(fallback); err != nil {
		t.Fatal(err)
	}
	run()
	if images := imageScanTestList(t, ctx, store, userID, item.ID, "Primary"); len(images) != 0 {
		t.Fatalf("authoritative absence did not clear the old type: %+v", images)
	}
}

func TestScanImagesRetainsOldTypesAfterDirectoryReplacement(t *testing.T) {
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	mediaPath := libraryIntegrationFile(t, approved, "movies/Film/Movie.mp4", "video:directory-artwork")
	imageScanTestWrite(t, filepath.Join(filepath.Dir(mediaPath), "poster.png"), color.White)
	library := libraryIntegrationCreate(t, ctx, store, "Directory artwork", "movies", filepath.Join(approved, "movies"))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	item := libraryIntegrationItemByPath(t, items.Items, mediaPath)
	state := imageScanTestState(t, ctx, pool, store, library, "Film")
	if err := state.scanImages(item.ID, item.Type, "Film/Movie.mp4", false); err != nil {
		t.Fatal(err)
	}
	before := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(before) != 1 {
		t.Fatalf("initial image was not indexed: %+v", before)
	}
	held, err := state.opened.OpenRoot("Film")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	oldDirectory := filepath.Dir(mediaPath)
	if err := os.Rename(oldDirectory, oldDirectory+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(oldDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := state.scanImages(item.ID, item.Type, "Film/Movie.mp4", false); err != nil {
		t.Fatal(err)
	}
	after := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if state.warnings == 0 || len(after) != 1 || after[0].Tag != before[0].Tag {
		t.Fatalf("a replacement directory cleared previous image metadata: %+v", after)
	}
}

func TestInspectLocalImageDetectsChangesAndRejectsUnsupportedSources(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "poster.png")
	imageScanTestWrite(t, path, color.White)
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	state := &scanState{task: &scanTask{ctx: context.Background()}, root: libraryRoot{path: directory}}
	source, err := state.inspectLocalImage(root, ".", "poster.png")
	if err != nil {
		t.Fatal(err)
	}
	defer source.file.Close()
	if err := os.Chtimes(path, time.Now(), source.info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := verifyScannedImage(root, source); err == nil {
		t.Fatal("a candidate changed after inspection was accepted")
	}
	if err := os.WriteFile(filepath.Join(directory, "poster.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := state.inspectLocalImage(root, ".", "poster.svg"); err == nil {
		t.Fatal("an SVG candidate was accepted")
	}
	if err := os.Truncate(path, maxLocalImageBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := state.inspectLocalImage(root, ".", "poster.png"); err == nil {
		t.Fatal("an oversized source reached image decoding")
	}
}
