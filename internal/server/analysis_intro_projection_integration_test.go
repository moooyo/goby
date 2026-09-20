//go:build linux

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introdetect"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type analysisProjectionProber struct{}

func (analysisProjectionProber) CacheVersion() int { return media.CurrentProbeVersion }
func (analysisProjectionProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (analysisHTTPProber{}).ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	var content [128]byte
	count, _ := file.ReadAt(content[:], 0)
	if strings.Contains(string(content[:count]), "chapter-source") {
		info.Chapters = []media.Chapter{{StartTicks: 5 * media.TicksPerSecond, Title: "IntroStart"}, {StartTicks: 15 * media.TicksPerSecond, Title: "IntroEnd"}}
	}
	return info, nil
}

type analysisProjectionFixture struct {
	*serverFixture
	root, libraryID string
	ids, paths      []string
	cookie          *http.Cookie
	csrf, token     string
	actor           identity.Principal
	tasks           *tasks.Store
}

func newAnalysisProjectionFixture(t *testing.T) analysisProjectionFixture {
	t.Helper()
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	catalog, err := library.New(f.pool, analysisProjectionProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	fixture := analysisProjectionFixture{serverFixture: f, root: root, cookie: cookie, csrf: csrf}
	fixture.actor, err = f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	fixture.token = stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	for index := 1; index <= 3; index++ {
		fixture.paths = append(fixture.paths, writeAPIMediaFile(t, root, fmt.Sprintf("tv/Projection Show/Season 01/Projection Show.S01E%02d.mp4", index)))
	}
	fixture.libraryID = createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "tv"), "tvshows")
	for _, path := range fixture.paths {
		var id string
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&id); err != nil {
			t.Fatal(err)
		}
		fixture.ids = append(fixture.ids, id)
	}
	// The fixture uses real task admission to obtain immutable source/cohort
	// facts, but never starts a worker or manufactures a publication fence.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	execution := library.AnalysisExecutionProfile{Version: library.AnalysisProfileVersion, Available: true,
		FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64),
		DetectorVersion: introdetect.Version, DetectorOptions: introdetect.DefaultOptions(), VisualIntervalTicks: media.TicksPerSecond / 2, IntroProfile: "projection-fixture-v1"}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskIntroAnalysisKey, Name: "Projection evidence fixture", Executor: analysisHTTPNoWorkExecutor{},
		AnalysisAdmission: func(tx library.OwnedTx, request tasks.AnalysisAdmissionRequest) (tasks.AnalysisAdmissionBinding, error) {
			binding, err := library.PrepareAnalysis(tx, request.TaskKey, request.Selection, execution)
			return tasks.AnalysisAdmissionBinding{ConfigurationFingerprint: binding.ConfigurationFingerprint, Bind: binding.Bind, SnapshotChildren: binding.SnapshotChildren}, err
		}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.tasks, err = tasks.New(f.pool, f.app.library, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.tasks.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f analysisProjectionFixture) subject() library.Subject {
	return library.Subject{UserID: f.actor.User.ID}
}

func (f analysisProjectionFixture) item(t *testing.T) library.Item {
	t.Helper()
	item, err := f.app.library.GetItemFor(f.ctx, f.subject(), f.ids[0])
	if err != nil || item.AnalysisSourceRevision == "" {
		t.Fatalf("capture real item source snapshot: %v", err)
	}
	return item
}

func (f analysisProjectionFixture) playback(t *testing.T) library.MediaFile {
	t.Helper()
	file, source, err := f.app.library.OpenMediaFor(f.ctx, f.subject(), f.ids[0], media.SourceID(f.ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if source.Item.AnalysisSourceRevision == "" {
		t.Fatal("opened playback source omitted its captured revision")
	}
	return source
}

func (f analysisProjectionFixture) rescan(t *testing.T) {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/libraries/"+f.libraryID+"/scan", map[string]any{"ForceProbe": true}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	jobID := stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	timer, ticker := time.NewTimer(15*time.Second), time.NewTicker(25*time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		job, err := f.app.library.GetJob(f.ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "Completed" {
			return
		}
		if job.Status == "Failed" || job.Status == "Cancelled" || job.Status == "Interrupted" {
			t.Fatal("replacement source scan did not complete")
		}
		select {
		case <-timer.C:
			t.Fatal("replacement scan exceeded its bounded fixture wait")
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func (f analysisProjectionFixture) manual(t *testing.T, reset bool) {
	t.Helper()
	path := "/admin/v1/items/" + f.ids[0] + "/intro"
	response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	current := jsonObject(t, response)
	body := map[string]any{"Revision": current["Revision"], "SourceRevision": current["SourceRevision"]}
	method := http.MethodDelete
	if !reset {
		method = http.MethodPut
		body["StartTicks"], body["EndTicks"], body["Provenance"] = int64(7*media.TicksPerSecond), int64(17*media.TicksPerSecond), "Manual"
	}
	expectStatus(t, f.request(t, method, path, body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie), http.StatusOK)
}

func (f analysisProjectionFixture) seedQualified(t *testing.T) {
	t.Helper()
	definition, err := f.tasks.GetByKey(f.ctx, library.TaskIntroAnalysisKey)
	if err != nil {
		t.Fatal(err)
	}
	actor := tasks.Actor{Principal: f.actor, Audience: identity.AdministratorNative}
	admission, err := f.tasks.Start(f.ctx, actor, tasks.StartRequest{TaskID: definition.ID, AnalysisInput: &library.AnalysisSelection{LibraryIDs: []string{f.libraryID}, ItemIDs: []string{f.ids[0]}}})
	if err != nil {
		t.Fatal(err)
	}
	var child, cohort, fingerprint string
	var profileRevision, epoch int64
	if err := f.pool.QueryRow(f.ctx, `SELECT w.child_id,w.cohort_revision,p.fingerprint,p.configuration_revision,p.publication_epoch
		FROM analysis_work w JOIN analysis_run_profiles p ON p.run_id=w.run_id WHERE w.run_id=$1`, admission.Run.ID).
		Scan(&child, &cohort, &fingerprint, &profileRevision, &epoch); err != nil {
		t.Fatal(err)
	}
	rows, err := f.pool.Query(f.ctx, `SELECT item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key FROM analysis_work_sources WHERE child_id=$1 ORDER BY position`, child)
	if err != nil {
		t.Fatal(err)
	}
	sources := []library.AnalysisSource{}
	for rows.Next() {
		var source library.AnalysisSource
		if err := rows.Scan(&source.ItemID, &source.LibraryID, &source.RootID, &source.SourceRevision, &source.HierarchyRevision, &source.EpisodeKey); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(sources) != 3 {
		t.Fatal("admission did not capture three independent real episode sources")
	}
	supports := make([]introdetect.Support, 0, len(sources))
	interval := introdetect.Interval{StartTicks: 10 * media.TicksPerSecond, EndTicks: 45 * media.TicksPerSecond}
	target := -1
	for index, source := range sources {
		var path string
		if err := f.pool.QueryRow(f.ctx, `SELECT path FROM items WHERE id=$1`, source.ItemID).Scan(&path); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		supports = append(supports, introdetect.Support{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hex.EncodeToString(digest[:]), Interval: interval})
		if source.ItemID == f.ids[0] {
			target = index
		}
	}
	if target < 0 {
		t.Fatal("admitted cohort omitted its actual publication target")
	}
	metrics := introdetect.Metrics{AudioAgreementPermille: 1000, AudioInformativePermille: 1000, AudioSimilarityPermille: 1000,
		AudioSamples: 100, AudioDistinct: 100, VisualAgreementPermille: 1000, VisualSimilarityPermille: 1000, VisualCoveragePermille: 1000,
		VisualSamples: 35, VisualTransitions: 34, VisualChangeCoveragePermille: 1000, VisualDominancePermille: 100, PairCount: 3}
	value := library.AnalysisStoredResult{Version: introdetect.Version, Episode: introdetect.EpisodeResult{
		EpisodeKey: supports[target].EpisodeKey, SourceKey: supports[target].SourceKey, ContentIdentity: supports[target].ContentIdentity,
		Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Candidates: []introdetect.Candidate{{Interval: interval,
			GroupID: "projection-qualified-evidence", Status: introdetect.Qualified, Reasons: []introdetect.Reason{}, Metrics: metrics, Support: supports}}}}
	raw, err := json.Marshal(value)
	if err != nil || library.ValidateStoredAnalysisResult(raw, "qualified", &interval.StartTicks, &interval.EndTicks) != nil {
		t.Fatal("qualified projection fixture is not valid stored evidence")
	}
	evidence, err := json.Marshal(library.AnalysisAuditEvidence{Result: &value})
	if err != nil || library.ValidateStoredAnalysisAudit(evidence, "qualified") != nil {
		t.Fatal("qualified projection fixture is not valid audit evidence")
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err := tx.Exec(f.ctx, `INSERT INTO analysis_detections(item_id,revision,source_revision,profile_fingerprint,profile_revision,publication_epoch,child_id,cohort_revision,status,result,start_ticks,end_ticks,auto_published)
		VALUES($1,1,$2,$3,$4,$5,$6,$7,'qualified',$8,$9,$10,true)`, f.ids[0], supports[target].SourceKey, fingerprint, profileRevision, epoch, child, cohort, raw, interval.StartTicks, interval.EndTicks); err != nil {
		t.Fatal(err)
	}
	for index, source := range sources {
		if _, err := tx.Exec(f.ctx, `INSERT INTO analysis_detection_sources(item_id,source_item_id,library_id,root_id,source_revision,hierarchy_revision,episode_key,content_sha256)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, f.ids[0], source.ItemID, source.LibraryID, source.RootID, source.SourceRevision, source.HierarchyRevision, source.EpisodeKey, supports[index].ContentIdentity); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(f.ctx, `INSERT INTO analysis_intro_audit(item_id,revision,source_revision,profile_fingerprint,action,evidence)
		VALUES($1,1,$2,$3,'qualified',$4)`, f.ids[0], supports[target].SourceKey, fingerprint, evidence); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.Stop(f.ctx, actor, admission.Run.ID); err != nil {
		t.Fatal(err)
	}
}

func analysisProjectionMarkers(t *testing.T, item map[string]any) map[string]int64 {
	t.Helper()
	chapters, ok := item["Chapters"].([]any)
	if !ok {
		t.Fatal("real response omitted the Chapters array")
	}
	markers := map[string]int64{}
	for _, raw := range chapters {
		chapter := raw.(map[string]any)
		kind, _ := chapter["MarkerType"].(string)
		if kind == "IntroStart" || kind == "IntroEnd" {
			if _, duplicate := markers[kind]; duplicate {
				t.Fatal("response duplicated one boundary of the intro interval")
			}
			markers[kind] = int64(chapter["StartPositionTicks"].(float64))
		}
	}
	return markers
}

func (f analysisProjectionFixture) assertHTTPMarkers(t *testing.T, want map[string]int64) {
	t.Helper()
	headers := http.Header{"X-Emby-Token": {f.token}}
	detail := f.request(t, http.MethodGet, "/emby/Users/"+f.actor.User.ID+"/Items/"+f.ids[0], nil, headers)
	expectStatus(t, detail, http.StatusOK)
	if strings.Contains(detail.Body.String(), "AnalysisSourceRevision") {
		t.Fatal("the private captured source revision leaked into item JSON")
	}
	if got := analysisProjectionMarkers(t, jsonObject(t, detail)); !reflect.DeepEqual(got, want) {
		t.Fatalf("item chapters: got %v, want %v", got, want)
	}
	playback := f.request(t, http.MethodGet, "/emby/Items/"+f.ids[0]+"/PlaybackInfo", nil, headers)
	expectStatus(t, playback, http.StatusOK)
	sources, ok := jsonObject(t, playback)["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("playback fixture did not describe one original source")
	}
	if got := analysisProjectionMarkers(t, sources[0].(map[string]any)); !reflect.DeepEqual(got, want) {
		t.Fatalf("playback chapters: got %v, want %v", got, want)
	}
}

func TestAnalysisIntroProjectionDoesNotBorrowAnIntervalFromAReplacementSource(t *testing.T) {
	for _, replacement := range []string{"manual", "chapter", "detected"} {
		t.Run(replacement, func(t *testing.T) {
			f := newAnalysisProjectionFixture(t)
			oldItem, oldPlayback := f.item(t), f.playback(t)
			if oldItem.Intro != nil || oldPlayback.Item.Intro != nil || oldItem.AnalysisSourceRevision != oldPlayback.Item.AnalysisSourceRevision {
				t.Fatal("old snapshot fixture already had an intro or mismatched source")
			}
			content := "replacement-media-source-with-new-bytes"
			if replacement == "chapter" {
				content += "-chapter-source"
			}
			if err := os.WriteFile(f.paths[0], []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			f.rescan(t)
			current := f.item(t)
			if current.AnalysisSourceRevision == oldItem.AnalysisSourceRevision {
				t.Fatal("replacement did not advance the actual indexed source stamp")
			}
			want := map[string]int64{"IntroStart": 5 * media.TicksPerSecond, "IntroEnd": 15 * media.TicksPerSecond}
			if replacement == "manual" {
				f.manual(t, false)
				want = map[string]int64{"IntroStart": 7 * media.TicksPerSecond, "IntroEnd": 17 * media.TicksPerSecond}
			} else if replacement == "detected" {
				f.seedQualified(t)
				want = map[string]int64{"IntroStart": 10 * media.TicksPerSecond, "IntroEnd": 45 * media.TicksPerSecond}
			}
			// The replacement is independently usable. Failure to enrich A must
			// therefore come from source binding, not an unavailable B fixture.
			f.assertHTTPMarkers(t, want)
			f.app.resolveItemAnalysisIntro(f.ctx, f.subject(), &oldItem, "")
			f.app.resolvePlaybackAnalysisIntro(f.ctx, f.subject(), &oldPlayback)
			if oldItem.Intro != nil || oldPlayback.Item.Intro != nil {
				t.Fatal("response snapshot A borrowed source B's interval")
			}
			if replacement == "detected" {
				unstamped := f.item(t)
				unstamped.AnalysisSourceRevision = ""
				f.app.resolveItemAnalysisIntro(f.ctx, f.subject(), &unstamped, "")
				if unstamped.Intro != nil {
					t.Fatal("a DTO without a captured source stamp gained detected authority")
				}
			}
		})
	}
}

func TestAnalysisIntroProjectionReachesHTTPChaptersRespectsManualAndWithdrawsChangedSupport(t *testing.T) {
	f := newAnalysisProjectionFixture(t)
	f.seedQualified(t)
	f.assertHTTPMarkers(t, map[string]int64{"IntroStart": 10 * media.TicksPerSecond, "IntroEnd": 45 * media.TicksPerSecond})
	f.manual(t, false)
	manual := map[string]int64{"IntroStart": 7 * media.TicksPerSecond, "IntroEnd": 17 * media.TicksPerSecond}
	f.assertHTTPMarkers(t, manual)
	if err := os.WriteFile(f.paths[1], []byte("unscanned-support-replacement-with-different-content"), 0600); err != nil {
		t.Fatal(err)
	}
	// A stale automatic source does not remove the independently valid manual
	// layer. Removing that manual layer must not revive the old detection.
	f.assertHTTPMarkers(t, manual)
	f.manual(t, true)
	f.assertHTTPMarkers(t, map[string]int64{})
	var revision, audits int
	if err := f.pool.QueryRow(f.ctx, `SELECT revision,(SELECT count(*) FROM analysis_intro_audit WHERE item_id=$1) FROM analysis_detections WHERE item_id=$1`, f.ids[0]).Scan(&revision, &audits); err != nil || revision != 1 || audits != 1 {
		t.Fatal("read-time source invalidation rewrote or erased the stored evidence")
	}
}
