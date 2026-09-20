//go:build linux

package library

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type embeddedArtworkFixtureProber struct {
	mu                  sync.Mutex
	probes, extractions int
	result              media.EmbeddedArtworkResult
	failure             error
	during              func()
}

func (p *embeddedArtworkFixtureProber) CacheVersion() int { return media.CurrentProbeVersion }
func (p *embeddedArtworkFixtureProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (mediaSourceTestProber{inner: &libraryFixtureProber{}}).ProbeFile(ctx, file)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.probes++
	for _, picture := range p.result.Pictures {
		info.Streams = append(info.Streams, media.Stream{Index: picture.StreamIndex, Codec: "png", CodecType: "video", IsAttachedPicture: true, Width: picture.Width, Height: picture.Height})
	}
	return info, err
}
func (p *embeddedArtworkFixtureProber) ExtractEmbeddedArtwork(ctx context.Context, file *os.File, info media.Info) (media.EmbeddedArtworkResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.extractions++
	if p.during != nil {
		p.during()
	}
	return p.result, p.failure
}
func (p *embeddedArtworkFixtureProber) set(result media.EmbeddedArtworkResult, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.result, p.failure = result, err
}
func (p *embeddedArtworkFixtureProber) counts() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.probes, p.extractions
}

func embeddedArtworkAssertOpen(t *testing.T, ctx context.Context, store *Store, userID, itemID string, want media.EmbeddedPicture) Image {
	t.Helper()
	reader, source, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, itemID, "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil || !bytes.Equal(data, want.Data) || source.Tag != want.Hash || source.Source != "embedded" || source.SourceRevision == "" {
		t.Fatalf("embedded bytes or provenance differed: %+v,%v", source, readErr)
	}
	return source
}

func TestStoreEmbeddedArtworkCachedScanStateFailureRetryAndRestart(t *testing.T) {
	picture := embeddedArtworkTestPicture(t, 4, "Front", color.NRGBA{R: 200, A: 255})
	result := media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}
	prober := &embeddedArtworkFixtureProber{result: result}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Album/Track.flac", "audio:embedded")
	collection := libraryIntegrationCreate(t, ctx, store, "Embedded artwork", "music", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	initial := embeddedArtworkAssertOpen(t, ctx, store, userID, item.ID, picture)
	actor := metadataEditTestActor(t, ctx, pool, "embedded-revision-editor")
	initialArtwork, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, ArtworkTarget{ItemID: item.ID})
	if err != nil {
		t.Fatal(err)
	}
	probes, extractions := prober.counts()
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if afterProbes, afterExtractions := prober.counts(); afterProbes != probes || afterExtractions != extractions {
		t.Fatal("unchanged source repeated probe or extraction")
	}
	// Simulate upgrading a catalog whose existing technical probe is cached,
	// but whose independently versioned artwork extraction never ran.
	if _, err := pool.Exec(ctx, "DELETE FROM item_embedded_artwork WHERE item_id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if afterProbes, afterExtractions := prober.counts(); afterProbes != probes || afterExtractions != extractions+1 {
		t.Fatal("cached technical probe skipped first artwork extraction")
	}
	if err := store.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	entitiesStoreCleanup(t, reopened)
	persisted := embeddedArtworkAssertOpen(t, ctx, reopened, userID, item.ID, picture)
	if persisted.Tag != initial.Tag || persisted.SourceRevision != initial.SourceRevision {
		t.Fatal("restart changed source-bound automatic artwork")
	}
	// A physical edit must reject old cached bytes before any scan updates DB.
	if err := os.WriteFile(path, []byte("audio:replacement-longer"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("unscanned replacement disclosed old bytes: %v", err)
	}
	prober.set(result, errors.New("damaged picture"))
	libraryIntegrationScan(t, ctx, reopened, collection.ID, "Completed")
	var status, failure string
	var noBytes bool
	if err := pool.QueryRow(ctx, "SELECT status,failure_code,content IS NULL FROM item_embedded_artwork WHERE item_id=$1", item.ID).Scan(&status, &failure, &noBytes); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || failure != "extraction_failed" || !noBytes {
		t.Fatalf("damage disguised as absence or retained bytes: %s/%s/%v", status, failure, noBytes)
	}
	if images, err := reopened.ListImages(ctx, userID, item.ID); err != nil || len(images) != 0 {
		t.Fatalf("failed replacement retained automatic image: %+v,%v", images, err)
	}
	prober.set(result, nil)
	libraryIntegrationScan(t, ctx, reopened, collection.ID, "Completed")
	repaired := embeddedArtworkAssertOpen(t, ctx, reopened, userID, item.ID, picture)
	if repaired.SourceRevision == initial.SourceRevision {
		t.Fatal("replaced source inherited original source revision")
	}
	repairedArtwork, err := reopened.GetArtwork(ctx, actor, identity.AdministratorNative, ArtworkTarget{ItemID: item.ID})
	if err != nil || repairedArtwork.Revision == initialArtwork.Revision {
		t.Fatalf("identical picture bytes inherited an obsolete source-bound artwork revision: %+v, %v", repairedArtwork, err)
	}
	prober.set(media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion}, nil)
	if err := os.WriteFile(path, []byte("audio:without-any-picture"), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, reopened, collection.ID, "Completed")
	if err := pool.QueryRow(ctx, "SELECT status,failure_code,content IS NULL FROM item_embedded_artwork WHERE item_id=$1", item.ID).Scan(&status, &failure, &noBytes); err != nil {
		t.Fatal(err)
	}
	if status != "none" || failure != "" || !noBytes {
		t.Fatalf("complete absence not explicit: %s/%s/%v", status, failure, noBytes)
	}
	_, emptyExtractions := prober.counts()
	libraryIntegrationScan(t, ctx, reopened, collection.ID, "Completed")
	if _, afterExtractions := prober.counts(); afterExtractions != emptyExtractions {
		t.Fatal("a successful cached absence repeated extraction")
	}
	if _, _, err := reopened.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed embedded picture remained readable: %v", err)
	}
	if err := reopened.DeleteLibrary(ctx, collection.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_embedded_artwork").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("removed library retained picture state: %d,%v", remaining, err)
	}
}

func TestStoreEmbeddedArtworkAuthorityPrecedenceAndManagedTombstone(t *testing.T) {
	picture := embeddedArtworkTestPicture(t, 2, "Front", color.NRGBA{G: 210, A: 255})
	prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Track.mp3", "audio:precedence")
	collection := libraryIntegrationCreate(t, ctx, store, "Embedded precedence", "music", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	embeddedArtworkAssertOpen(t, ctx, store, userID, item.ID, picture)
	libraryIntegrationUser(t, ctx, pool, "embedded-hidden", false, false, nil)
	for _, id := range []string{item.ID, "missing-item"} {
		if _, _, err := store.OpenEmbeddedImageFor(ctx, Subject{UserID: "embedded-hidden"}, id, "Primary", 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unauthorized embedded image %s: %v", id, err)
		}
	}
	if batch, err := store.ImagesForItems(ctx, "embedded-hidden", []string{item.ID}); err != nil || len(batch) != 0 {
		t.Fatalf("batch disclosed hidden artwork: %+v,%v", batch, err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "embedded-editor")
	target := ArtworkTarget{ItemID: item.ID}
	before, err := store.GetArtwork(ctx, actor, identity.AdministratorNative, target)
	if err != nil {
		t.Fatal(err)
	}
	if managedArtworkImage(t, before, "Primary", 0).Source != "embedded" {
		t.Fatal("admin revision omitted automatic embedded artwork")
	}
	deleted, err := store.DeleteArtwork(ctx, actor, identity.AdministratorNative, target, before.Revision, "Primary", 0)
	if err != nil || len(deleted.Items) != 0 {
		t.Fatalf("managed deletion=%+v,%v", deleted, err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if _, _, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("managed tombstone fell through to embedded: %v", err)
	}
	restored, err := store.ResetArtwork(ctx, actor, identity.AdministratorNative, target, deleted.Revision, "Primary")
	if err != nil || managedArtworkImage(t, restored, "Primary", 0).Tag != picture.Hash {
		t.Fatalf("reset did not reveal embedded: %+v,%v", restored, err)
	}
	sidecarPath := filepath.Join(filepath.Dir(path), "Track-cover.png")
	sidecar := imageScanTestWrite(t, sidecarPath, color.NRGBA{B: 200, A: 255})
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	reader, source, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(data, sidecar) || source.Source == "embedded" {
		t.Fatal("embedded artwork overrode a sidecar")
	}
	provider := embeddedArtworkTestPicture(t, 8, "Other", color.NRGBA{R: 155, A: 255})
	if _, err := pool.Exec(ctx, `INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash)
		VALUES($1,'Primary',0,'tmdb','fixture-provider','fixture-image',$2,$3,$4,$5,$6)`, item.ID, provider.Data, provider.MIMEType, provider.Width, provider.Height, provider.Hash); err != nil {
		t.Fatal(err)
	}
	reader, source, err = store.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || !bytes.Equal(data, provider.Data) || source.Tag != provider.Hash {
		t.Fatal("sidecar or embedded artwork overrode provider selection")
	}
	if _, err := pool.Exec(ctx, "DELETE FROM item_provider_images WHERE item_id=$1", item.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	embeddedArtworkAssertOpen(t, ctx, store, userID, item.ID, picture)
	// Corrupt persisted cache bytes must fail integrity checks even when the
	// media source itself remains current and authorization still succeeds.
	corrupt := append([]byte(nil), picture.Data...)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := pool.Exec(ctx, "UPDATE item_embedded_artwork SET content=$2 WHERE item_id=$1", item.ID, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.OpenImageContentFor(ctx, Subject{UserID: userID}, item.ID, "Primary", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cache corruption was not rejected: %v", err)
	}
}

func TestStoreEmbeddedArtworkChangedDuringExtractionCannotPublish(t *testing.T) {
	picture := embeddedArtworkTestPicture(t, 1, "Front", color.NRGBA{R: 70, A: 255})
	prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}}
	ctx, pool, store, root, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, root, "music/Track.flac", "audio:changing")
	collection := libraryIntegrationCreate(t, ctx, store, "Changing embedded source", "music", filepath.Dir(path))
	prober.during = func() {
		if err := os.WriteFile(path, []byte("audio:concurrently-replaced"), 0600); err != nil {
			t.Error(err)
		}
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, collection.ID, path)
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM item_embedded_artwork WHERE item_id=$1", item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("changed source published automatic artwork: %d,%v", count, err)
	}
}
