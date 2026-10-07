//go:build linux

package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type scanRealImageTemplate struct {
	data          []byte
	hash          string
	width, height int
}

func scanRealImagePNG(t *testing.T, width, height int, seed uint8, variant ...string) scanRealImageTemplate {
	t.Helper()
	var signature []byte
	red, green, blue := seed, uint8(0), seed
	if len(variant) != 0 {
		digest := sha256.Sum256([]byte(variant[0]))
		signature = digest[:]
		red, green, blue = red+digest[0], digest[1], blue+digest[2]
	}
	frame := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			frame.SetNRGBA(x, y, color.NRGBA{R: uint8(x/8) + red, G: uint8(y/8) + green, B: blue, A: 255})
		}
	}
	// Keep the complete source signature in decoded pixels. Distinct content
	// must not depend on trailing bytes or changing only the file's metadata.
	for index, value := range signature {
		frame.SetNRGBA(index, 0, color.NRGBA{R: value, G: uint8(index), B: seed, A: 255})
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	return scanRealImageTemplate{data: data, hash: fmt.Sprintf("%x", sha256.Sum256(data)), width: width, height: height}
}

type scanRealImageTraceKey struct{}

type scanRealImageTrace struct {
	*scanPerformanceSQLTracer
	upserts, deletes, upsertRows, deletedRows, snapshots atomic.Int64
}

func (trace *scanRealImageTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.scanPerformanceSQLTracer.TraceQueryStart(ctx, conn, data)
	statement := strings.ToLower(strings.TrimSpace(data.SQL))
	if strings.HasPrefix(statement, "insert into item_images") {
		trace.upserts.Add(1)
		ctx = context.WithValue(ctx, scanRealImageTraceKey{}, "upsert")
	} else if strings.HasPrefix(statement, "delete from item_images") {
		trace.deletes.Add(1)
		ctx = context.WithValue(ctx, scanRealImageTraceKey{}, "delete")
	}
	if strings.Contains(data.SQL, imageCatalogSnapshotProjection) {
		trace.snapshots.Add(1)
	}
	return ctx
}

func (trace *scanRealImageTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.scanPerformanceSQLTracer.TraceQueryEnd(ctx, conn, data)
	if data.Err != nil {
		return
	}
	switch ctx.Value(scanRealImageTraceKey{}) {
	case "upsert":
		trace.upsertRows.Add(data.CommandTag.RowsAffected())
	case "delete":
		trace.deletedRows.Add(data.CommandTag.RowsAffected())
	}
}

func scanRealImageSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, libraryID string, expected map[string]string, sources map[string]scanRealImageTemplate) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT i.relative_path, im.image_type, im.image_index, im.relative_path,
		im.source_hash, im.file_size, im.width, im.height, im.mime_type, im.root_id=i.root_id, im.xmin::text
		FROM items i JOIN item_images im ON im.item_id=i.id WHERE i.library_id=$1`, libraryID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	versions := make(map[string]string, len(expected))
	for rows.Next() {
		var item, kind, path, hash, mime, version string
		var index, width, height int
		var size int64
		var currentRoot bool
		if err := rows.Scan(&item, &kind, &index, &path, &hash, &size, &width, &height, &mime, &currentRoot, &version); err != nil {
			t.Fatal(err)
		}
		key := fmt.Sprintf("%s:%s:%d", item, kind, index)
		source, exists := sources[path]
		if !exists || expected[key] != path || !currentRoot || hash != source.hash || size != int64(len(source.data)) ||
			width != source.width || height != source.height || mime != "image/png" || versions[key] != "" {
			t.Fatalf("image projection differs from the independent corpus: key=%s path=%s", key, path)
		}
		versions[key] = version
	}
	if err := rows.Err(); err != nil || len(versions) != len(expected) {
		t.Fatalf("image corpus is incomplete: actual=%d expected=%d error=%v", len(versions), len(expected), err)
	}
	return versions
}

// Copy this complete opt-in file unchanged to both measured revisions. Tuple
// changes are observations: an older scanner may rewrite unchanged valid rows.
func TestScanRealImagesPerformance(t *testing.T) {
	runScanRealImagesPerformance(t, nil)
}

// The optional test companion preserves this driver's standalone copy contract.
type scanRealImageQueryPlanControl interface {
	configure(*testing.T, *pgxpool.Config, *scanRealImageTrace)
	bind(*testing.T, *Store)
	begin(*testing.T, string)
	end(*testing.T, Job, time.Duration)
	observeResult(*testing.T, Job, int64, int, int, int, int, int)
	runControls(*testing.T, *Store, func(string, bool, int64))
	report(*testing.T)
}

func runScanRealImagesPerformance(t *testing.T, queryPlans scanRealImageQueryPlanControl) {
	t.Helper()
	if os.Getenv("GOBY_TEST_SCAN_REAL_IMAGES") != "1" {
		t.Skip("GOBY_TEST_SCAN_REAL_IMAGES=1 enables the bounded real image scan profile")
	}
	if os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("the enabled real image profile requires a PostgreSQL fixture")
	}
	contentMode := os.Getenv("GOBY_SCAN_REAL_IMAGES_CONTENT")
	if contentMode == "" {
		contentMode = "shared"
	}
	if contentMode != "shared" && contentMode != "diverse" {
		t.Fatal("GOBY_SCAN_REAL_IMAGES_CONTENT must be shared or diverse")
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	for _, tool := range []string{ffmpeg, ffprobe} {
		info, err := os.Stat(tool)
		if !filepath.IsAbs(tool) || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			t.Fatal("the enabled real image profile requires absolute executable media tools")
		}
	}
	prober := &scanProbeProfileProber{Prober: media.Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 30 * time.Second}}
	ctx, observer, original, root, userID := libraryIntegrationStoreWithTimeout(t, prober, 10*time.Minute)
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &scanRealImageTrace{scanPerformanceSQLTracer: scanPerformanceProfileTracer()}
	configuration := observer.Config()
	trace.configure(configuration)
	if !trace.disabled {
		configuration.ConnConfig.Tracer = trace
	}
	if queryPlans != nil {
		queryPlans.configure(t, configuration, trace)
	}
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if queryPlans != nil {
		queryPlans.bind(t, store)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	videoPath, audioPath := filepath.Join(root, "templates", "movie.mp4"), filepath.Join(root, "templates", "track.flac")
	if err := os.MkdirAll(filepath.Dir(videoPath), 0700); err != nil {
		t.Fatal(err)
	}
	catalogRescanGenerateMedia(t, ctx, ffmpeg, videoPath, "-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-threads", "1", "-movflags", "+faststart")
	catalogRescanGenerateMedia(t, ctx, ffmpeg, audioPath, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "0.25", "-ac", "1",
		"-c:a", "flac", "-threads", "1", "-metadata", "title=", "-metadata", "album=")
	video, videoErr := os.ReadFile(videoPath)
	audio, audioErr := os.ReadFile(audioPath)
	if videoErr != nil || audioErr != nil || len(video) == 0 || len(video) > 8<<10 || len(audio) == 0 || len(audio) > 16<<10 {
		t.Fatalf("invalid media templates: video=%d audio=%d errors=%v/%v", len(video), len(audio), videoErr, audioErr)
	}
	poster, backdrop, replacement := scanRealImagePNG(t, 320, 480, 17), scanRealImagePNG(t, 640, 360, 31), scanRealImagePNG(t, 320, 480, 93)
	const changedPath = "Group00/Movie0000-poster.png"
	if contentMode == "diverse" {
		replacement = scanRealImagePNG(t, 320, 480, 93, changedPath+":replacement")
	}
	corpus := filepath.Join(root, "mixed")
	sources, expected := make(map[string]scanRealImageTemplate), make(map[string]string)
	writeImage := func(path string, template scanRealImageTemplate) {
		if contentMode == "diverse" {
			seed := uint8(17)
			if template.hash == backdrop.hash {
				seed = 31
			}
			template = scanRealImagePNG(t, template.width, template.height, seed, path)
		}
		catalogCapacityWriteExclusive(t, filepath.Join(corpus, filepath.FromSlash(path)), template.data)
		sources[path] = template
	}
	for group := 0; group < 8; group++ {
		directory := fmt.Sprintf("Group%02d", group)
		writeImage(directory+"/poster.png", poster)
		expected[directory+":Primary:0"] = directory + "/poster.png"
		for index, name := range []string{"backdrop.png", "backdrop2.png", "backdrop10.png"} {
			writeImage(directory+"/"+name, backdrop)
			expected[fmt.Sprintf("%s:Backdrop:%d", directory, index)] = directory + "/" + name
		}
		for member := 0; member < 16; member++ {
			stem := fmt.Sprintf("%s/Movie%04d", directory, group*16+member)
			catalogCapacityWriteExclusive(t, filepath.Join(corpus, filepath.FromSlash(stem+".mp4")), video)
			writeImage(stem+"-poster.png", poster)
			writeImage(stem+"-backdrop.png", backdrop)
			expected[stem+".mp4:Primary:0"] = stem + "-poster.png"
			for index, path := range []string{stem + "-backdrop.png", directory + "/backdrop.png", directory + "/backdrop2.png", directory + "/backdrop10.png"} {
				expected[fmt.Sprintf("%s.mp4:Backdrop:%d", stem, index)] = path
			}
		}
	}
	for track := 0; track < 32; track++ {
		catalogCapacityWriteExclusive(t, filepath.Join(corpus, "Album", fmt.Sprintf("%02d Track.flac", track+1)), audio)
	}
	uniqueHashes := make(map[string]bool)
	for _, source := range sources {
		uniqueHashes[source.hash] = true
	}
	wantHashes := 2
	if contentMode == "diverse" {
		wantHashes = len(sources)
	}
	if len(uniqueHashes) != wantHashes || uniqueHashes[replacement.hash] {
		t.Fatalf("image content diversity differs from the selected corpus: hashes=%d want=%d replacement_duplicate=%t", len(uniqueHashes), wantHashes, uniqueHashes[replacement.hash])
	}
	var pngBytes, allocatedBytes int64
	var manifest []string
	if err := filepath.WalkDir(corpus, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
			return fmt.Errorf("corpus requires independent regular files")
		}
		allocatedBytes += stat.Blocks * 512
		if filepath.Ext(path) == ".png" {
			pngBytes += info.Size()
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(corpus, path)
		if err != nil {
			return err
		}
		manifest = append(manifest, fmt.Sprintf("%s %d %x", filepath.ToSlash(relative), info.Size(), sha256.Sum256(data)))
		return nil
	}); err != nil || len(sources) != 288 || len(expected) != 672 || pngBytes > 64<<20 || pngBytes-int64(len(sources[changedPath].data))+int64(len(replacement.data)) > 64<<20 {
		t.Fatalf("image corpus exceeds its fixed budget: files=%d rows=%d bytes=%d error=%v", len(sources), len(expected), pngBytes, err)
	}
	t.Logf("scan_real_images_corpus videos=128 tracks=32 png_files=288 expected_image_rows=672 png_bytes=%d corpus_allocated_bytes=%d corpus_sha256=%x video_bytes=%d audio_bytes=%d video_sha256=%x audio_sha256=%x poster_bytes=%d poster_sha256=%s backdrop_bytes=%d backdrop_sha256=%s replacement_bytes=%d replacement_sha256=%s",
		pngBytes, allocatedBytes, sha256.Sum256([]byte(strings.Join(manifest, "\n"))), len(video), len(audio), sha256.Sum256(video), sha256.Sum256(audio), len(poster.data), poster.hash, len(backdrop.data), backdrop.hash, len(replacement.data), replacement.hash)
	t.Logf("scan_real_images_content mode=%s unique_png_hashes=%d expected_generic_backdrop_reselections=384 template_scope=shared_mode_sources_or_diverse_mode_base_patterns", contentMode, len(uniqueHashes))
	library := libraryIntegrationCreate(t, ctx, store, "Real image profile", "mixed", corpus)
	observerCtx := context.WithValue(ctx, scanPerformanceObserverContextKey{}, true)
	taskOwned := os.Getenv("GOBY_TEST_SCAN_PROBE_TASK_OWNED") == "1"
	previous := map[string]string{}
	scan := func(phase string, force bool, wantProbes int64) {
		t.Helper()
		childID := ""
		if taskOwned {
			key := TaskLibraryScanKey
			if force {
				key = TaskLibraryRefreshMediaKey
			}
			_, children := taskScanFixtureForKey(t, ctx, observer, key, library)
			if len(children) != 1 {
				t.Fatal("image profile requires one task child")
			}
			childID = children[0]
		}
		trace.reset()
		for _, counter := range []*atomic.Int64{&trace.upserts, &trace.deletes, &trace.upsertRows, &trace.deletedRows, &trace.snapshots} {
			counter.Store(0)
		}
		prober.maximum.Store(0)
		if queryPlans != nil {
			queryPlans.begin(t, phase)
		}
		measurement := scanPerformanceBeginMeasurement(pool)
		calls, started := prober.calls.Load(), time.Now()
		var job Job
		if taskOwned {
			admission, err := store.AdmitTaskScan(ctx, childID)
			if err != nil || admission.Kind != ScanAdmitted {
				t.Fatalf("image task admission failed: %+v %v", admission, err)
			}
			job = admission.Job
		} else {
			job, err = store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: force})
			if err != nil {
				t.Fatal(err)
			}
		}
		job = libraryIntegrationWaitJobWithTimeout(t, observerCtx, store, job.ID, "Completed", 3*time.Minute)
		elapsed := time.Since(started)
		retirement := scanPerformanceWaitWorkerRetired(t, ctx, store, job.ID)
		observation := scanPerformanceEndMeasurement(measurement, pool)
		if queryPlans != nil {
			queryPlans.end(t, job, elapsed)
		}
		wantAdded, wantUpdated := 0, 0
		if phase == "cold" {
			wantAdded = 160
		}
		if force {
			wantUpdated = 160
		}
		probes := prober.calls.Load() - calls
		if job.Error != "" || job.Scanned != 160 || job.Added != wantAdded || job.Updated != wantUpdated || job.ForceProbe != force ||
			job.TaskChildID != childID || job.CancelRequested || job.StartedAt == nil || job.FinishedAt == nil || probes != wantProbes || prober.active.Load() != 0 {
			t.Fatalf("real image scan differs from its corpus: job=%+v probes=%d active=%d", job, probes, prober.active.Load())
		}
		observation.log(t, "real_images", phase, "mixed_real_images", job, elapsed, retirement, probes, trace.scanPerformanceSQLTracer)
		// All corpus reads and tuple comparisons follow the measured interval.
		current := scanRealImageSnapshot(t, ctx, observer, library.ID, expected, sources)
		inserted, changed, unchanged, removed := 0, 0, 0, 0
		for key, version := range current {
			if previous[key] == "" {
				inserted++
			} else if previous[key] != version {
				changed++
			} else {
				unchanged++
			}
		}
		for key := range previous {
			if current[key] == "" {
				removed++
			}
		}
		previous = current
		record := map[string]any{"phase": phase, "content_mode": contentMode, "image_rows": len(current), "png_files": len(sources), "new_keys": inserted, "removed_keys": removed,
			"changed_xmin": changed, "unchanged_xmin": unchanged, "image_upsert_commands": nil, "image_delete_commands": nil,
			"image_upsert_affected_rows": nil, "image_delete_affected_rows": nil, "image_catalog_snapshots": nil,
			"peak_probe_cohorts": prober.maximum.Load(), "sql_trace_enabled": !trace.disabled,
			"tuple_scope": "committed key and xmin differences outside measurement; changed xmin does not imply changed public content",
			"sql_scope":   "successful command-tag affected rows may include rolled-back attempts; upsert combines inserts and updates"}
		if !trace.disabled {
			record["image_upsert_commands"], record["image_delete_commands"] = trace.upserts.Load(), trace.deletes.Load()
			record["image_upsert_affected_rows"], record["image_delete_affected_rows"] = trace.upsertRows.Load(), trace.deletedRows.Load()
			record["image_catalog_snapshots"] = trace.snapshots.Load()
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("scan_real_images_observation=%s", encoded)
		if queryPlans != nil {
			queryPlans.observeResult(t, job, probes, len(current), inserted, removed, changed, unchanged)
		}
		if taskOwned {
			taskScanAssertChild(t, observerCtx, observer, childID, job)
		}
	}
	scan("cold", false, 160)
	scan("warm", false, 0)
	scan("force", true, 160)
	if err := os.WriteFile(filepath.Join(corpus, filepath.FromSlash(changedPath)), replacement.data, 0600); err != nil {
		t.Fatal(err)
	}
	sources[changedPath] = replacement
	scan("image_changed", false, 0)
	// Prefixed backdrops precede, rather than replace, the generic candidates.
	// Removing this one source shifts only this movie's three generic indexes.
	const removedPath = "Group00/Movie0000-backdrop.png"
	if err := os.Remove(filepath.Join(corpus, filepath.FromSlash(removedPath))); err != nil {
		t.Fatal(err)
	}
	delete(sources, removedPath)
	for index := 0; index < 4; index++ {
		delete(expected, fmt.Sprintf("Group00/Movie0000.mp4:Backdrop:%d", index))
	}
	for index, name := range []string{"backdrop.png", "backdrop2.png", "backdrop10.png"} {
		expected[fmt.Sprintf("Group00/Movie0000.mp4:Backdrop:%d", index)] = "Group00/" + name
	}
	scan("image_removed", false, 0)
	if queryPlans != nil {
		queryPlans.runControls(t, store, scan)
	}
	result, err := store.QueryItems(ctx, Query{UserID: userID, ParentID: library.ID, Recursive: true,
		IncludeItemTypes: []string{"Movie", "Audio"}, Limit: 160})
	if err != nil || result.TotalRecordCount != 160 || len(result.Items) != 160 {
		t.Fatalf("real image profile lost catalog items: total=%d items=%d error=%v", result.TotalRecordCount, len(result.Items), err)
	}
	scanPerformanceCloseMeasurementStore(t, store)
	if queryPlans != nil {
		queryPlans.report(t)
	}
}
