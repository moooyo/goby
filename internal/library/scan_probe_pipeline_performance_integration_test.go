//go:build linux

package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type scanProbeProfileProber struct {
	media.Prober
	calls, active, maximum atomic.Int64
}

func (prober *scanProbeProfileProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	prober.calls.Add(1)
	active := prober.active.Add(1)
	defer prober.active.Add(-1)
	for maximum := prober.maximum.Load(); active > maximum; maximum = prober.maximum.Load() {
		if prober.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	return prober.Prober.ProbeFile(ctx, file)
}

type scanProbeProfileResources struct {
	childCPU  time.Duration
	inBlocks  int64
	outBlocks int64
}

func scanProbeProfileResourceSnapshot(t *testing.T) scanProbeProfileResources {
	t.Helper()
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &usage); err != nil {
		t.Fatal(err)
	}
	return scanProbeProfileResources{
		childCPU: time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second +
			time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond,
		inBlocks: int64(usage.Inblock), outBlocks: int64(usage.Oublock),
	}
}

// This complete, bounded real-media profile can be copied unchanged into the
// accepted baseline. It measures scanner work through terminal completion,
// unlike an HTTP load fixture that cancels its scan when the GET wave ends.
// Process peak counts ProbeFile cohorts, not individual subprocesses. Child
// block-I/O counters are kernel observations, not logical source-read bytes.
func TestScanRealProbePipelinePerformance(t *testing.T) {
	if os.Getenv("GOBY_TEST_SCAN_PROBE_PERFORMANCE") != "1" {
		t.Skip("GOBY_TEST_SCAN_PROBE_PERFORMANCE=1 enables the complete cold probe profile")
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for the real probe profile")
	}
	taskOwned := os.Getenv("GOBY_TEST_SCAN_PROBE_TASK_OWNED") == "1"
	mode := "independent"
	if taskOwned {
		mode = "task-owned"
	}
	prober := &scanProbeProfileProber{Prober: media.Prober{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Timeout: 30 * time.Second}}
	ctx, observer, original, root, userID := libraryIntegrationStoreWithTimeout(t, prober, 8*time.Minute)
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &scanPerformanceSQLTracer{}
	configuration := observer.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store, err := New(pool, prober, []string{root})
	if err != nil {
		t.Fatal(err)
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
	catalogRescanGenerateMedia(t, ctx, ffmpeg, videoPath,
		"-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-threads", "1", "-movflags", "+faststart")
	catalogRescanGenerateMedia(t, ctx, ffmpeg, audioPath,
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "0.25", "-ac", "1",
		"-c:a", "flac", "-threads", "1", "-metadata", "title=", "-metadata", "album=")
	video, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	audio, err := os.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(video) == 0 || len(video) > 8<<10 || len(audio) == 0 || len(audio) > 16<<10 {
		t.Fatalf("profile media is outside its fixture budget: video=%d audio=%d", len(video), len(audio))
	}
	const videos, tracks = 128, 32
	for i := 0; i < videos; i++ {
		catalogCapacityWriteExclusive(t, filepath.Join(root, "mixed", fmt.Sprintf("Movie%04d.mp4", i)), video)
	}
	for i := 0; i < tracks; i++ {
		catalogCapacityWriteExclusive(t, filepath.Join(root, "mixed", "Album", fmt.Sprintf("%02d Track.flac", i+1)), audio)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Real probe profile", "mixed", filepath.Join(root, "mixed"))
	observerCtx := context.WithValue(ctx, scanPerformanceObserverContextKey{}, true)
	rowsSnapshot := func() string {
		t.Helper()
		var value string
		if err := observer.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_array(i.id,i.xmin::text,
			COALESCE(ms.xmin::text,'')) ORDER BY i.id),'[]'::jsonb)::text
			FROM items i LEFT JOIN item_metadata_state ms ON ms.item_id=i.id WHERE i.library_id=$1`, library.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	scan := func(phase string, force bool, wantAdded, wantUpdated int, wantProbes int64) {
		t.Helper()
		childID := ""
		if taskOwned {
			key := TaskLibraryScanKey
			if force {
				key = TaskLibraryRefreshMediaKey
			}
			// Fixture preparation is outside the measured scanner interval.
			// Admission, scanning and terminal completion remain inside it.
			_, children := taskScanFixtureForKey(t, ctx, observer, key, library)
			if len(children) != 1 {
				t.Fatalf("task-owned profile requires exactly one child: %v", children)
			}
			childID = children[0]
		}
		trace.reset()
		prober.maximum.Store(0)
		calls, resources, started := prober.calls.Load(), scanProbeProfileResourceSnapshot(t), time.Now()
		var job Job
		if taskOwned {
			admission, err := store.AdmitTaskScan(ctx, childID)
			if err != nil || admission.Kind != ScanAdmitted || admission.Job.TaskChildID != childID {
				t.Fatalf("admit task-owned profile: admission=%+v error=%v", admission, err)
			}
			job = admission.Job
		} else {
			job, err = store.StartScanWithOptions(ctx, library.ID, ScanOptions{ForceProbe: force})
			if err != nil {
				t.Fatal(err)
			}
		}
		job = libraryIntegrationWaitJobWithTimeout(t, observerCtx, store, job.ID, "Completed", 3*time.Minute)
		after := scanProbeProfileResourceSnapshot(t)
		probes := prober.calls.Load() - calls
		if job.Error != "" || job.Scanned != videos+tracks || job.Added != wantAdded || job.Updated != wantUpdated ||
			job.ForceProbe != force || job.TaskChildID != childID || job.CancelRequested || probes != wantProbes || job.StartedAt == nil || job.FinishedAt == nil || prober.active.Load() != 0 {
			t.Fatalf("complete real scan facts differ: job=%+v probes=%d active=%d", job, probes, prober.active.Load())
		}
		if wantProbes == 0 && (trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0) {
			t.Fatalf("warm scan rewrote primary facts: items=%d metadata=%d", trace.itemRows.Load(), trace.metadataRows.Load())
		}
		t.Logf("scan_probe_performance phase=%s elapsed=%s job_elapsed=%s probes=%d peak_probe_cohorts=%d sql=%d begins=%d commits=%d direct_item_rows=%d direct_metadata_rows=%d child_cpu=%s child_inblock=%d child_oublock=%d mode=%s",
			phase, time.Since(started), job.FinishedAt.Sub(*job.StartedAt), probes, prober.maximum.Load(),
			trace.queries.Load(), trace.begins.Load(), trace.commits.Load(), trace.itemRows.Load(), trace.metadataRows.Load(),
			after.childCPU-resources.childCPU, after.inBlocks-resources.inBlocks, after.outBlocks-resources.outBlocks, mode)
		if taskOwned {
			// This observer assertion follows the measurement. It does not add
			// fixture or child-snapshot SQL to the scanner's traced interval.
			taskScanAssertChild(t, observerCtx, observer, childID, job)
		}
	}
	scan("cold", false, videos+tracks, 0, videos+tracks)
	before := rowsSnapshot()
	scan("warm", false, 0, 0, 0)
	if rowsSnapshot() != before {
		t.Fatal("warm scan changed catalog or automatic metadata row versions")
	}
	scan("force", true, 0, videos+tracks, videos+tracks)
	result, err := store.QueryItems(ctx, Query{UserID: userID, ParentID: library.ID, Recursive: true,
		IncludeItemTypes: []string{"Movie", "Audio"}, Limit: videos + tracks})
	if err != nil || result.TotalRecordCount != videos+tracks || len(result.Items) != videos+tracks {
		t.Fatalf("profile corpus did not remain complete: count=%d items=%d error=%v", result.TotalRecordCount, len(result.Items), err)
	}
}
