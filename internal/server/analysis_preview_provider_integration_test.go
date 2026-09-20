//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/analysiscache"
	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type analysisProviderFixture struct {
	stream        *streamHTTPFixture
	cache         *analysiscache.Store
	cacheConfig   analysiscache.Config
	viewer        identity.Principal
	administrator identity.Principal
	csrf          string
	source        library.AnalysisSource
	entry         analysiscache.Entry
	archives      map[int][]byte
}

// This fixture exercises real authenticated delivery, source containment and
// cache ownership. Its original bytes and deterministic probe are HTTP fixtures,
// not decodable media. Generation stays disabled and no executable is probed.
// Explicit SQL references below are a delivery fixture, not evidence that a
// worker acquired or passed the production publication fence.
func newAnalysisProviderFixture(t *testing.T, ready bool) *analysisProviderFixture {
	t.Helper()
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	catalog, err := library.New(f.pool, streamHTTPProber{}, []string{root})
	if err != nil {
		t.Fatal("create deterministic source catalog")
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	stream := &streamHTTPFixture{f: f, root: root, adminID: f.bootstrap(t)}
	stream.video = stream.addItem(t, "Preview Source.mp4", "movies", "Videos", "mp4", "video/mp4", bytes.Repeat([]byte("current-preview-source-identity"), 32))
	viewer, err := f.users.CreateUser(f.ctx, "Preview Viewer", "preview-viewer-password", false)
	if err != nil {
		t.Fatal("create preview viewer")
	}
	stream.viewerID = viewer.ID
	stream.setPolicy(t, viewer.ID, true, []string{stream.video.libraryID})
	stream.token = stringValue(t, f.embyLogin(t, "Preview Viewer", "preview-viewer-password"), "AccessToken")
	cookie, csrf := f.adminLogin(t)
	stream.cookie = cookie
	viewerPrincipal, err := f.users.ResolveWithPeer(f.ctx, stream.token, "emby", "192.0.2.1")
	if err != nil {
		t.Fatal("resolve real preview viewer login")
	}
	administrator, err := f.users.ResolveWithPeer(f.ctx, cookie.Value, "admin", "192.0.2.1")
	if err != nil {
		t.Fatal("resolve real native administrator login")
	}
	runtime := f.app.mediaAnalysis
	if runtime == nil || runtime.cache != nil || runtime.configuration.Enabled {
		t.Fatal("fixture unexpectedly has an execution-enabled analysis runtime")
	}
	configuration := analysiscache.Config{Root: filepath.Join(t.TempDir(), "cache"), MaxBytes: 16 << 20, MaxEntries: 4, MaxEntryBytes: 4 << 20, MaxFileBytes: 1 << 20, MaxTemporaryFiles: 16}
	cache, err := analysiscache.Open(f.ctx, configuration)
	if err != nil {
		t.Fatal("open owned sealed preview cache")
	}
	// Install only the real storage dependency before the first preview request.
	// The actual runtime, provider, watcher and shutdown code remain unchanged.
	runtime.cache = cache
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close real preview runtime and cache: %v", err)
		}
	})
	f.handler = f.app.Handler()
	fixture := &analysisProviderFixture{stream: stream, cache: cache, cacheConfig: configuration, viewer: viewerPrincipal, administrator: administrator, csrf: csrf, archives: make(map[int][]byte)}
	fixture.source, err = f.app.library.GetCurrentAnalysisSourceFor(f.ctx, librarySubject(viewerPrincipal, viewer.ID), stream.video.id, media.SourceID(stream.video.id))
	if err != nil || fixture.source.SourceRevision == "" {
		t.Fatal("read actual indexed source identity for preview delivery")
	}
	if ready {
		fixture.seedReady(t)
	}
	return fixture
}

func (fixture *analysisProviderFixture) seedReady(t *testing.T) {
	t.Helper()
	f := fixture.stream.f
	keyDigest := sha256.Sum256([]byte("preview-provider-fixture:" + fixture.source.ItemID + ":" + fixture.source.SourceRevision))
	builder, err := fixture.cache.Begin(f.ctx, hex.EncodeToString(keyDigest[:]), 0)
	if err != nil {
		t.Fatal("begin owned preview cache generation")
	}
	t.Cleanup(func() { _ = builder.Abort(context.Background()) })
	artifacts := make(map[int]analysiscache.Artifact)
	timeline, err := library.EncodeAnalysisPreviewTimeline([]int64{0}, []int64{0})
	if err != nil {
		t.Fatal("encode one complete source timeline slot")
	}
	timelineDigest := sha256.Sum256(timeline)
	manifest := analysisPreviewBuildManifest{Version: 1, ItemID: fixture.source.ItemID, SourceRevision: fixture.source.SourceRevision,
		ProfileFingerprint: strings.Repeat("a", 64), ConfigurationRevision: "1", PublicationEpoch: 1,
		PreviewProfile: media.PreviewAnalysisProfile, FFmpegSHA256: strings.Repeat("b", 64), FFprobeSHA256: strings.Repeat("c", 64),
		Quality: 80, IntervalTicks: 100000000}
	for _, width := range []int{240, 320, 400} {
		height := width * 9 / 16
		raster := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				raster.SetRGBA(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 7), B: uint8(x + y), A: 255})
			}
		}
		var jpegBytes bytes.Buffer
		if err := jpeg.Encode(&jpegBytes, raster, &jpeg.Options{Quality: 80}); err != nil {
			t.Fatal("encode bounded Go JPEG preview")
		}
		archive, err := bif.Encode(f.ctx, []bif.Frame{{Timestamp: 0, JPEG: jpegBytes.Bytes()}}, 1000, bif.DefaultLimits())
		if err != nil {
			t.Fatal("encode real version-zero BIF bytes")
		}
		fixture.archives[width] = archive
		artifact, err := builder.WriteFile(f.ctx, strconv.Itoa(width)+".bif", func(ctx context.Context, writer io.Writer) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, err := writer.Write(archive)
			return err
		})
		if err != nil {
			t.Fatal("seal actual BIF artifact")
		}
		artifacts[width] = artifact
		manifest.Variants = append(manifest.Variants, analysisPreviewVariantManifest{Width: width, Height: height, FrameCount: 1,
			Bytes: artifact.Size, SHA256: artifact.SHA256, TimelineSHA256: hex.EncodeToString(timelineDigest[:])})
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteFile(f.ctx, "manifest.json", func(ctx context.Context, writer io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := writer.Write(encodedManifest)
		return err
	}); err != nil {
		t.Fatal("seal delivery metadata with the actual preview bytes")
	}
	publication, err := builder.Publish(f.ctx)
	if publication != nil {
		// A failed final directory sync can still return a published, pinned
		// entry. Conservatively resolve that pin even while reporting failure.
		t.Cleanup(func() { _ = publication.Keep() })
	}
	if err != nil {
		t.Fatal("publish physically sealed preview entry")
	}
	fixture.entry = publication.Entry
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin explicit preview delivery fixture references")
	}
	defer tx.Rollback(context.Background())
	for _, width := range []int{240, 320, 400} {
		artifact := artifacts[width]
		if _, err := tx.Exec(f.ctx, `INSERT INTO analysis_previews(item_id,width,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,
			child_id,cache_key,seal,height,content_sha256,bytes,frame_count,interval_ticks,timeline)
			VALUES($1,$2,1,$3,$4,1,1,'preview-delivery-sql-fixture',$5,$6,$7,$8,$9,1,100000000,$10)`,
			fixture.source.ItemID, width, fixture.source.SourceRevision, strings.Repeat("a", 64), publication.Entry.Key, publication.Entry.Seal, width*9/16, artifact.SHA256, artifact.Size, timeline); err != nil {
			t.Fatal("insert explicit source-bound delivery reference")
		}
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal("commit complete preview fixture references")
	}
	if err := publication.Keep(); err != nil {
		t.Fatal("release cache publication pin after committed fixture references")
	}
}

func (fixture *analysisProviderFixture) request(t *testing.T, method, path string, headers http.Header) *httptest.ResponseRecorder {
	t.Helper()
	values := headers.Clone()
	if values == nil {
		values = make(http.Header)
	}
	values.Set("X-Emby-Token", fixture.stream.token)
	return fixture.stream.f.request(t, method, path, nil, values)
}

func (fixture *analysisProviderFixture) bifPath() string {
	return "/emby/Videos/" + fixture.source.ItemID + "/index.bif?Width=400&MediaSourceId=" + fixture.source.MediaSourceID
}

func (fixture *analysisProviderFixture) open(t *testing.T) analysisPreviewLease {
	t.Helper()
	lease, err := fixture.stream.f.app.mediaAnalysis.OpenPreview(fixture.stream.f.ctx, fixture.viewer, fixture.source.ItemID, fixture.source.MediaSourceID, 400)
	if lease != nil {
		t.Cleanup(func() {
			if err := lease.Close(); err != nil {
				t.Errorf("close actual preview lease: %v", err)
			}
		})
	}
	if err != nil || lease == nil {
		t.Fatalf("open real source-bound preview lease: %v", err)
	}
	return lease
}

func assertAnalysisProviderIdle(t *testing.T, fixture *analysisProviderFixture) {
	t.Helper()
	stats := fixture.cache.Stats()
	if stats.Readers != 0 || stats.PendingPublications != 0 || stats.BuildingEntries != 0 || stats.ReservedBytes != 0 || len(fixture.stream.f.app.mediaAnalysis.previewSlots) != 0 {
		t.Fatalf("preview delivery retained owned resources: %+v", stats)
	}
}

func TestAnalysisPreviewProviderHTTPAliasesUseRealSealedCacheAndMissingContracts(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, false)
	id, source := fixture.source.ItemID, fixture.source.MediaSourceID
	missing := fixture.request(t, http.MethodGet, fixture.bifPath(), nil)
	decoded, err := bif.Open(bytes.NewReader(missing.Body.Bytes()), int64(missing.Body.Len()), bif.DefaultLimits())
	if missing.Code != http.StatusOK || err != nil || decoded.Len() != 0 || missing.Body.Len() != 72 {
		t.Fatal("authorized source without cache did not produce a valid empty BIF")
	}
	for _, path := range []string{"/Items/" + id + "/ThumbnailSet?Width=400", "/Items/" + id + "/Images/Thumbnail?PositionTicks=0"} {
		expectStatus(t, fixture.request(t, http.MethodGet, path, nil), http.StatusNotFound)
	}
	assertAnalysisProviderIdle(t, fixture)
	fixture.seedReady(t)
	var tag, etag string
	for _, base := range []string{"/Items/", "/emby/Items/", "/EMBY/iTeMs/"} {
		response := fixture.request(t, http.MethodGet, base+id+"/tHuMbNaIlSeT?Width=400&MediaSourceId="+source, nil)
		expectStatus(t, response, http.StatusOK)
		var set analysisThumbnailSetDTO
		if json.Unmarshal(response.Body.Bytes(), &set) != nil || len(set.Thumbnails) != 1 || set.Thumbnails[0].PositionTicks != 0 || set.AspectRatio != float64(400)/225 {
			t.Fatal("real provider metadata differs from its sealed BIF")
		}
		tag = set.Thumbnails[0].ImageTag
	}
	for _, base := range []string{"/Videos/", "/emby/Videos/", "/EMBY/vIdEoS/"} {
		response := fixture.request(t, http.MethodGet, base+id+"/INDEX.BIF?Width=400&MediaSourceId="+source, nil)
		expectStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.Body.Bytes(), fixture.archives[400]) {
			t.Fatal("actual provider changed the sealed BIF bytes")
		}
		etag = response.Header().Get("ETag")
		if etag == "" || response.Header().Get("Cache-Control") != "private, no-cache, no-transform" {
			t.Fatal("sealed BIF lost its private source validator")
		}
	}
	unauthenticated := fixture.stream.f.request(t, http.MethodGet, "/Items/"+id+"/Images/Thumbnail?PositionTicks=0&tag="+tag, nil, http.Header{"If-None-Match": {"*"}})
	if unauthenticated.Code != http.StatusUnauthorized || unauthenticated.Header().Get("ETag") != "" {
		t.Fatal("a real sealed image tag became anonymous preview authority")
	}
	unbound := fixture.request(t, http.MethodGet, "/Items/"+id+"/ThumbnailSet?Width=400&MediaSourceId=unbound-source", http.Header{"If-None-Match": {"*"}})
	if unbound.Code != http.StatusNotFound || unbound.Header().Get("ETag") != "" {
		t.Fatal("an unbound media source received preview metadata or a cached validator")
	}
	for _, base := range []string{"/Items/", "/emby/Items/", "/EMBY/iTeMs/"} {
		response := fixture.request(t, http.MethodGet, base+id+"/iMaGeS/tHuMbNaIl?PositionTicks=10000000&MediaSourceId="+source+"&tag="+tag+"&maxWidth=800&quality=90", nil)
		expectStatus(t, response, http.StatusOK)
		info, err := jpeg.DecodeConfig(bytes.NewReader(response.Body.Bytes()))
		if err != nil || info.Width != 400 || info.Height != 225 {
			t.Fatal("DPR display bound selected a nonexistent cache variant or enlarged the preview")
		}
	}
	ranged := fixture.request(t, http.MethodGet, fixture.bifPath(), http.Header{"Range": {"bytes=0-7"}})
	if ranged.Code != 206 || !bytes.Equal(ranged.Body.Bytes(), fixture.archives[400][:8]) || ranged.Header().Get("Content-Range") != "bytes 0-7/"+strconv.Itoa(len(fixture.archives[400])) {
		t.Fatal("real cache reader did not honor a single BIF range")
	}
	conditional := fixture.request(t, http.MethodHead, fixture.bifPath(), http.Header{"If-None-Match": {etag}})
	if conditional.Code != 304 || conditional.Body.Len() != 0 {
		t.Fatal("real provider HEAD validator did not produce a bodyless conditional response")
	}
	head := fixture.request(t, http.MethodHead, fixture.bifPath(), nil)
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(fixture.archives[400])) {
		t.Fatal("real provider HEAD differs from the stored complete archive")
	}
	assertAnalysisProviderIdle(t, fixture)
}

func TestAnalysisPreviewProviderRejectsRevokedAuthorityBeforeCachedResponses(t *testing.T) {
	for _, mode := range []string{"playback_policy", "viewer_credential"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newAnalysisProviderFixture(t, true)
			initial := fixture.request(t, http.MethodGet, fixture.bifPath(), nil)
			expectStatus(t, initial, http.StatusOK)
			lease := fixture.open(t)
			if !lease.Metadata().Ready {
				t.Fatal("current authorized viewer did not obtain the stored generation")
			}
			status := http.StatusForbidden
			if mode == "playback_policy" {
				fixture.stream.setPolicy(t, fixture.viewer.User.ID, false, []string{fixture.stream.video.libraryID})
			} else {
				status = http.StatusUnauthorized
				if err := fixture.stream.f.users.Revoke(fixture.stream.f.ctx, fixture.stream.token); err != nil {
					t.Fatal("revoke current viewer credential")
				}
			}
			if err := lease.Revalidate(fixture.stream.f.ctx); err == nil {
				t.Fatal("held preview lease retained revoked current authority")
			}
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				response := fixture.request(t, method, fixture.bifPath(), http.Header{"If-None-Match": {initial.Header().Get("ETag")}, "Range": {"bytes=0-7"}})
				if response.Code != status || response.Header().Get("ETag") != "" || response.Header().Get("Content-Range") != "" {
					t.Fatal("revoked authority received a conditional or ranged cached representation")
				}
			}
			if err := lease.Close(); err != nil {
				t.Fatal("release revoked preview lease")
			}
			assertAnalysisProviderIdle(t, fixture)
		})
	}
	fixture := newAnalysisProviderFixture(t, true)
	f := fixture.stream.f
	if err := f.users.Revoke(f.ctx, fixture.stream.cookie.Value); err != nil {
		t.Fatal("revoke current native administrator credential")
	}
	profile := library.DefaultAnalysisProfile()
	profile.PreviewQuality++
	response := f.request(t, http.MethodPut, "/admin/v1/media-analysis/configuration", map[string]any{"Revision": "1", "Profile": profile},
		http.Header{"X-CSRF-Token": {fixture.csrf}, "Origin": {f.cfg.PublicURL}}, fixture.stream.cookie)
	expectStatus(t, response, http.StatusUnauthorized)
	var revision, references int
	if f.pool.QueryRow(f.ctx, `SELECT revision,(SELECT count(*) FROM analysis_previews) FROM analysis_settings WHERE id=1`).Scan(&revision, &references) != nil || revision != 1 || references != 3 {
		t.Fatal("revoked native login changed configuration or invalidated another viewer's previews")
	}
	expectStatus(t, fixture.request(t, http.MethodGet, fixture.bifPath(), nil), http.StatusOK)
	assertAnalysisProviderIdle(t, fixture)
}

func TestAnalysisPreviewProviderRejectsActualSourceReplacementWithoutRescan(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, true)
	before := fixture.request(t, http.MethodGet, fixture.bifPath(), nil)
	expectStatus(t, before, http.StatusOK)
	lease := fixture.open(t)
	path := fixture.stream.video.path
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal("capture indexed source identity")
	}
	replacement := path + ".replacement"
	data := bytes.Clone(fixture.stream.video.data)
	data[0] ^= 0xff
	if err := os.WriteFile(replacement, data, 0600); err != nil {
		t.Fatal("write replacement source inode")
	}
	if err := os.Chtimes(replacement, original.ModTime(), original.ModTime()); err != nil {
		t.Fatal("retain indexed source mtime on replacement")
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal("atomically replace original source fixture")
	}
	current, err := os.Stat(path)
	if err != nil || os.SameFile(original, current) || current.Size() != original.Size() || !current.ModTime().Equal(original.ModTime()) {
		t.Fatal("replacement did not preserve size/mtime while changing the actual source identity")
	}
	if err := lease.Revalidate(fixture.stream.f.ctx); err == nil {
		t.Fatal("cached preview lease ignored an unscanned source replacement")
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		response := fixture.request(t, method, fixture.bifPath(), http.Header{"If-None-Match": {before.Header().Get("ETag")}, "Range": {"bytes=0-7"}})
		if response.Code != http.StatusServiceUnavailable || response.Header().Get("ETag") != "" || response.Header().Get("Content-Range") != "" {
			t.Fatal("source replacement was answered by an old BIF cache validator")
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatal("close source-replaced preview lease")
	}
	assertAnalysisProviderIdle(t, fixture)
}

func TestAnalysisPreviewProviderTreatsEvictedCacheAsMissingDespiteDatabaseReferences(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, true)
	fixture.assertNativeResidency(t, "ready", "")
	lease := fixture.open(t)
	if !lease.Metadata().Ready || fixture.cache.Stats().Readers != 2 {
		t.Fatal("ready provider did not pin its actual cache reader")
	}
	blocked, err := fixture.cache.PruneUnreferencedResult(fixture.stream.f.ctx, map[string]bool{})
	if err != nil || blocked.RemovedEntries != 0 || blocked.BusyEntries < 1 {
		t.Fatal("cache pruning removed an actively leased generation")
	}
	if err := lease.Close(); err != nil {
		t.Fatal("release cache pin before eviction")
	}
	// This deliberately models physical LRU eviction while SQL references remain.
	// It is not the administrator's reference-aware pruning workflow.
	pruned, err := fixture.cache.PruneUnreferencedResult(fixture.stream.f.ctx, map[string]bool{})
	if err != nil || pruned.RemovedEntries != 1 || fixture.cache.Stats().ReadyEntries != 0 {
		t.Fatal("fixture did not evict its real sealed cache entry")
	}
	var references int
	if fixture.stream.f.pool.QueryRow(fixture.stream.f.ctx, `SELECT count(*) FROM analysis_previews WHERE item_id=$1`, fixture.source.ItemID).Scan(&references) != nil || references != 3 {
		t.Fatal("eviction test accidentally removed SQL references")
	}
	fixture.assertNativeResidency(t, "missing", "cache_not_resident")
	missing := fixture.open(t)
	if missing.Metadata().Ready || missing.Reader() != nil {
		t.Fatal("database references fabricated a ready preview after physical eviction")
	}
	if err := missing.Close(); err != nil {
		t.Fatal("release authorized missing-result lease")
	}
	response := fixture.request(t, http.MethodGet, fixture.bifPath(), nil)
	decoded, err := bif.Open(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()), bif.DefaultLimits())
	if response.Code != 200 || err != nil || decoded.Len() != 0 {
		t.Fatal("evicted derivative did not use the authenticated empty BIF contract")
	}
	expectStatus(t, fixture.request(t, http.MethodGet, "/Items/"+fixture.source.ItemID+"/ThumbnailSet?Width=400", nil), http.StatusNotFound)
	assertAnalysisProviderIdle(t, fixture)
}

func (fixture *analysisProviderFixture) assertNativeResidency(t *testing.T, status, failure string) {
	t.Helper()
	f := fixture.stream.f
	response := f.request(t, http.MethodGet, "/admin/v1/media-analysis/items/"+fixture.source.ItemID, nil, nil, fixture.stream.cookie)
	value := analysisHTTPObject(t, response, http.StatusOK)
	previews, ok := value["Previews"].([]any)
	if !ok || len(previews) != 3 {
		t.Fatal("native detail lost its complete registered preview family")
	}
	for _, entry := range previews {
		preview, ok := entry.(map[string]any)
		if !ok || len(preview) != 7 || preview["Status"] != status || preview["FailureCode"] != failure {
			t.Fatal("native preview status differs from physical cache residency or exposes extra fields")
		}
	}
	for _, private := range []string{fixture.entry.Key, fixture.entry.Seal, fixture.cacheConfig.Root} {
		if private != "" && strings.Contains(response.Body.String(), private) {
			t.Fatal("native preview inventory exposed private cache identity or storage")
		}
	}
	for _, artifact := range fixture.entry.Artifacts {
		if strings.Contains(response.Body.String(), artifact.SHA256) {
			t.Fatal("native preview inventory exposed a private artifact digest")
		}
	}
}

func TestAnalysisPreviewProviderActiveLeaseObservesConfigurationAndClear(t *testing.T) {
	for _, mode := range []string{"configuration", "preview_clear"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newAnalysisProviderFixture(t, true)
			f := fixture.stream.f
			prior := fixture.request(t, http.MethodGet, fixture.bifPath(), nil)
			expectStatus(t, prior, http.StatusOK)
			lease := fixture.open(t)
			if mode == "configuration" {
				profile := library.DefaultAnalysisProfile()
				profile.PreviewQuality++
				response := f.request(t, http.MethodPut, "/admin/v1/media-analysis/configuration", map[string]any{"Revision": "1", "Profile": profile},
					http.Header{"X-CSRF-Token": {fixture.csrf}, "Origin": {f.cfg.PublicURL}}, fixture.stream.cookie)
				expectStatus(t, response, http.StatusOK)
			} else {
				if err := f.app.library.ClearAnalysisPreviews(f.ctx, fixture.administrator, fixture.source.ItemID, fixture.source.SourceRevision); err != nil {
					t.Fatal("clear previews under the real native administrator authority")
				}
				var revision int64
				if f.pool.QueryRow(f.ctx, `SELECT revision FROM analysis_preview_state WHERE item_id=$1`, fixture.source.ItemID).Scan(&revision) != nil || revision != 1 {
					t.Fatal("preview clear did not persist its new admission tombstone")
				}
			}
			if err := lease.Revalidate(f.ctx); !errors.Is(err, library.ErrAnalysisSourceChanged) {
				t.Fatalf("old lease did not observe withdrawn generation: %v", err)
			}
			// Observe the production watcher rather than calling its cancellation hook.
			watchBudget := time.NewTimer(8 * time.Second)
			defer watchBudget.Stop()
			select {
			case <-lease.Context().Done():
			case <-watchBudget.C:
				t.Fatal("preview watcher retained a withdrawn generation")
			}
			if fixture.cache.Stats().Readers != 2 {
				t.Fatal("watcher cancellation silently released a caller-owned reader")
			}
			missing := fixture.request(t, http.MethodGet, fixture.bifPath(), http.Header{"If-None-Match": {prior.Header().Get("ETag")}})
			if missing.Code != 200 || missing.Body.Len() != 72 || missing.Header().Get("ETag") == prior.Header().Get("ETag") {
				t.Fatal("withdrawn generation reused a cached 304 or old BIF")
			}
			if err := lease.Close(); err != nil {
				t.Fatal("release withdrawn generation reader")
			}
			assertAnalysisProviderIdle(t, fixture)
		})
	}
}

func TestAnalysisPreviewProviderShutdownWaitsForActualCacheLeaseRelease(t *testing.T) {
	fixture := newAnalysisProviderFixture(t, true)
	runtime := fixture.stream.f.app.mediaAnalysis
	lease := fixture.open(t)
	reader, ok := lease.Reader().(*os.File)
	if !ok || fixture.cache.Stats().Readers != 2 {
		t.Fatal("provider did not hold an actual cache descriptor")
	}
	runtime.BeginClose()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Close(expired); !errors.Is(err, context.Canceled) {
		t.Fatal("bounded shutdown reported completion while its cache reader remained owned")
	}
	select {
	case <-runtime.done:
		t.Fatal("runtime completed before the actual lease was released")
	default:
	}
	if fixture.cache.Stats().Readers != 2 {
		t.Fatal("runtime cancellation prematurely released its reader pin")
	}
	if _, err := reader.Stat(); err != nil {
		t.Fatal("runtime cancellation closed a descriptor still owned by the caller")
	}
	if extra, err := runtime.OpenPreview(context.Background(), fixture.viewer, fixture.source.ItemID, fixture.source.MediaSourceID, 400); err == nil || extra != nil {
		if extra != nil {
			_ = extra.Close()
		}
		t.Fatal("closing runtime admitted another preview reader")
	}
	if err := lease.Close(); err != nil {
		t.Fatal("release final actual preview reader")
	}
	joined, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := runtime.Close(joined); err != nil {
		t.Fatal("shutdown did not join the reader release and cache owner")
	}
	if _, err := reader.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("joined shutdown left the cache descriptor open")
	}
	assertAnalysisProviderIdle(t, fixture)
	// A new real cache owner can enter only after the old runtime has finished.
	reopened, err := analysiscache.Open(joined, fixture.cacheConfig)
	if err != nil {
		t.Fatal("joined shutdown retained the old filesystem owner lock")
	}
	if err := reopened.Close(joined); err != nil {
		t.Fatal("close replacement cache ownership fixture")
	}
}
