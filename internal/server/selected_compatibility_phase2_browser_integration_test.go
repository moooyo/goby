//go:build linux && goby_embed_admin && goby_browser_integration

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
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
	adminassets "github.com/moooyo/goby/web/admin"
)

var selectedPhase2Phases = []string{"authentication", "removal-ready", "removal-applied", "ocr-ready", "ocr-reviewed",
	"ocr-applied", "cancel-ready", "cancelled", "artwork", "restart", "persisted", "cleanup"}

type selectedPhase2Execution struct {
	Marker                     string
	FFmpegPath, FFmpegSHA256   string
	FFprobePath, FFprobeSHA256 string
	PythonPath, PythonSHA256   string
	FontPath, FontSHA256       string
	OCR                        config.MediaOperationsOCRConfig
}

type selectedPhase2FileFact struct {
	Path                     string
	Device, Inode            uint64
	Bytes, Modified, Changed int64
	Mode                     uint32
	SHA256                   string
}

type selectedPhase2ExpectedCue struct {
	StartTicks int64  `json:"start_ticks"`
	EndTicks   int64  `json:"end_ticks"`
	Text       string `json:"text"`
	Forced     bool   `json:"forced"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RGBAHash   string `json:"rgba_sha256"`
}

type selectedPhase2BitmapManifest struct {
	Format        string `json:"format"`
	DurationTicks int64  `json:"duration_ticks"`
	Cases         []struct {
		Name      string                      `json:"name"`
		File      string                      `json:"pgs_matroska_file"`
		Origin    int64                       `json:"pgs_container_origin_ticks"`
		Models    []string                    `json:"ocr_models"`
		Intervals []selectedPhase2ExpectedCue `json:"pgs_intervals"`
	} `json:"cases"`
	Files map[string]struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

// Only this private temporary context contains passwords. No token or database
// URL is passed to the browser child or written into evidence summaries.
type selectedPhase2Context struct {
	Marker                                             string
	RunID                                              string `json:"RunId"`
	BaseURL                                            string
	AdminID                                            string `json:"AdminId"`
	AdminName, AdminPassword                           string
	ViewerID                                           string `json:"ViewerId"`
	ViewerName, ViewerPassword                         string
	LibraryID                                          string `json:"LibraryId"`
	LibraryName                                        string
	AudioLibraryID                                     string `json:"AudioLibraryId"`
	AudioLibraryName                                   string
	RemoveItemID                                       string `json:"RemoveItemId"`
	RemoveItemName                                     string
	OCRItemID                                          string `json:"OCRItemId"`
	OCRItemName                                        string
	AudioItemID                                        string `json:"AudioItemId"`
	AudioItemName                                      string
	SecondAudioItemID                                  string `json:"SecondAudioItemId"`
	RemoveStreamIndex, KeepStreamIndex, OCRStreamIndex int
	RemoveSubtitleTitle, KeepSubtitleTitle             string
	OCRModelIDs                                        []string `json:"ModelIds"`
	OCRLanguage, OCRTitle, ExpectedOCRPhrase           string
	OCRExpectedCues                                    []selectedPhase2ExpectedCue
	ReviewEdits                                        []library.MediaOperationCueEdit
	ArtifactsDir, ResultPath                           string
}

type selectedPhase2Request struct {
	RunID       string `json:"RunId"`
	Phase       string
	OperationID string `json:"OperationId"`
	Revision    string
	ResultHash  string
	CueEdits    []library.MediaOperationCueEdit
	HistoryIDs  []string `json:"HistoryIds"`
}

type selectedPhase2Result struct {
	Marker                      string
	RunID                       string `json:"RunId"`
	Complete                    bool
	Checks                      map[string]bool
	PageErrors, ForeignRequests *int
	Stages                      []struct{ Phase, State string }
}

func selectedPhase2Fact(path string, maximum int64) (selectedPhase2FileFact, error) {
	var result selectedPhase2FileFact
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return result, errors.New("phase 2 inventory paths must be canonical absolute paths")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum {
		return result, errors.New("phase 2 file is not a bounded regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	resolved, resolveErr := filepath.EvalSymlinks(path)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 || resolveErr != nil || resolved != path || info.Mode().Perm()&0o022 != 0 {
		return result, errors.New("phase 2 file identity or ownership is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return result, errors.New("phase 2 file could not be opened")
	}
	defer file.Close()
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(file, maximum+1))
	after, statErr := file.Stat()
	if err != nil || statErr != nil || n != info.Size() || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) || media.FileChangeTime(info) != media.FileChangeTime(after) {
		return result, errors.New("phase 2 file changed during its bounded hash observation")
	}
	return selectedPhase2FileFact{Path: path, Device: uint64(stat.Dev), Inode: stat.Ino, Bytes: n,
		Modified: info.ModTime().UnixNano(), Changed: stat.Ctim.Nano(), Mode: uint32(info.Mode().Perm()), SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func selectedPhase2ReadExecution(t *testing.T) (selectedPhase2Execution, []selectedPhase2FileFact) {
	t.Helper()
	path := refreshBrowserPath(t, "GOBY_SELECTED_PHASE2_EXECUTION_CONFIG", false)
	var raw json.RawMessage
	if featureWavePrivateJSON(path, 64<<10, &raw) != nil {
		t.Fatal("read private phase 2 execution inventory")
	}
	var execution selectedPhase2Execution
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&execution) != nil || execution.Marker != "goby-selected-phase2-execution-v1" {
		t.Fatal("phase 2 execution inventory has an invalid schema")
	}
	configuration := config.MediaOperationsConfig{Enabled: true, MaxConcurrent: 1, MaxQueued: 4, MaxRuntimeSeconds: 90,
		MaxScratchBytes: 512 << 20, ScratchDirectory: "/owned/phase2-scratch", WritableProfiles: []string{"matroska-v1"}, OCR: execution.OCR}
	if configuration.Validate() != nil {
		t.Fatal("phase 2 execution inventory does not contain a supported OCR configuration")
	}
	checks := []struct{ path, digest string }{{execution.FFmpegPath, execution.FFmpegSHA256}, {execution.FFprobePath, execution.FFprobeSHA256},
		{execution.PythonPath, execution.PythonSHA256}, {execution.FontPath, execution.FontSHA256}, {execution.OCR.Executable, execution.OCR.ToolSHA256}}
	models := map[string]bool{}
	for _, model := range execution.OCR.Models {
		models[model.ID] = true
		checks = append(checks, struct{ path, digest string }{filepath.Join(execution.OCR.TessdataDirectory, model.Filename), model.SHA256})
	}
	if !models["eng"] || !models["chi_sim"] {
		t.Fatal("phase 2 overlap OCR requires the explicit eng and chi_sim models")
	}
	facts := make([]selectedPhase2FileFact, 0, len(checks))
	for _, input := range checks {
		fact, err := selectedPhase2Fact(input.path, 256<<20)
		if err != nil || len(input.digest) != 64 || fact.SHA256 != input.digest {
			t.Fatal("phase 2 pinned tool, model, or font identity differs")
		}
		facts = append(facts, fact)
	}
	return execution, facts
}

func selectedPhase2PNG(t *testing.T, path string, shade color.NRGBA) []byte {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			canvas.SetNRGBA(x, y, shade)
		}
	}
	var encoded bytes.Buffer
	if png.Encode(&encoded, canvas) != nil || os.WriteFile(path, encoded.Bytes(), 0o600) != nil {
		t.Fatal("write owned phase 2 cover pixels")
	}
	return encoded.Bytes()
}

func selectedPhase2RGBAHash(data []byte) (string, int, int, error) {
	picture, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, errors.New("phase 2 image did not decode")
	}
	bounds := picture.Bounds()
	if bounds.Dx() < 1 || bounds.Dy() < 1 || bounds.Dx() > 4096 || bounds.Dy() > 4096 {
		return "", 0, 0, errors.New("phase 2 decoded image dimensions are invalid")
	}
	digest := sha256.New()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(picture.At(x, y)).(color.NRGBA)
			_, _ = digest.Write([]byte{pixel.R, pixel.G, pixel.B, pixel.A})
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), bounds.Dx(), bounds.Dy(), nil
}

func selectedPhase2GenerateVideo(t *testing.T, execution selectedPhase2Execution, output, bitmap, remove, keep, chapters string) {
	t.Helper()
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=navy:size=720x576:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=12"}
	if bitmap != "" {
		args = append(args, "-i", bitmap, "-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0", "-c:s", "copy", "-metadata:s:s:0", "language=eng", "-metadata:s:s:0", "title=PGS English Chinese")
	} else {
		args = append(args, "-i", remove, "-i", keep, "-f", "ffmetadata", "-i", chapters,
			"-map", "0:v:0", "-map", "1:a:0", "-map", "2:s:0", "-map", "3:s:0", "-map_metadata", "4", "-map_chapters", "4", "-c:s", "srt",
			"-metadata:s:s:0", "language=eng", "-metadata:s:s:0", "title=Remove English", "-disposition:s:0", "default",
			"-metadata:s:s:1", "language=fra", "-metadata:s:s:1", "title=Keep French", "-disposition:s:1", "0")
	}
	// Offset input audio by the AAC encoder's 1024-sample priming so muxing
	// leaves the independently authored PGS timeline at its actual zero origin.
	args = append(args, "-af", "asetpts=PTS+1024/SR/TB", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1", "-bf", "0", "-g", "24",
		"-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-b:a", "64000", "-t", "12", "-avoid_negative_ts", "disabled", "-f", "matroska", output)
	hlsHTTPMediaCommand(t, execution.FFmpegPath, args...)
	if os.Chmod(output, 0o600) != nil {
		t.Fatal("protect owned phase 2 video")
	}
}

func selectedPhase2Preservation(ctx context.Context, f *serverFixture, viewer string, items []string) (json.RawMessage, error) {
	var raw string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'Identity',(SELECT jsonb_agg(jsonb_build_object('Id',id,'LibraryId',library_id,'RootId',root_id,'ParentId',parent_id,'Name',name,'Type',type,'Path',path) ORDER BY id) FROM items WHERE id=ANY($2::text[])),
		'Metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m WHERE item_id=ANY($2::text[])),
		'UserData',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('RowVersion',d.xmin::text) ORDER BY item_id) FROM user_item_data d WHERE user_id=$1 AND item_id=ANY($2::text[]))
	)::text`, viewer, items).Scan(&raw)
	return json.RawMessage(raw), err
}

func selectedPhase2Database(ctx context.Context, f *serverFixture) (json.RawMessage, error) {
	var raw string
	err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object('Schema',current_schema(),
		'Operations',(SELECT COALESCE(jsonb_agg(jsonb_build_object('Id',id,'Kind',kind,'State',state,'Revision',revision::text,
			'ItemId',source_item_id,'StreamIndex',stream_index,'SourceRevision',source_revision,'Parameters',parameters,
			'ProgressStage',progress_stage,'Processed',processed,'Total',total,'ResultHash',result_hash,'ResultSummary',result_summary,
			'PublicationPhase',publication_phase,'WorkerPresent',worker_token<>'','CancelRequested',cancel_requested_at IS NOT NULL,
			'ErrorCode',error_code,'Started',started_at IS NOT NULL,'Finished',finished_at IS NOT NULL) ORDER BY created_at,id),'[]'::jsonb) FROM media_operations),
		'Cues',(SELECT COALESCE(jsonb_agg(jsonb_build_object('OperationId',operation_id,'Ordinal',ordinal,'OriginalStartTicks',original_start_ticks,
			'OriginalEndTicks',original_end_ticks,'OriginalText',original_text,'StartTicks',start_ticks,'EndTicks',end_ticks,'Text',text,
			'Included',included,'Confidence',confidence,'Warnings',warnings,'ImageSHA256',image_sha256,'ImageBytes',octet_length(image_png)) ORDER BY operation_id,ordinal),'[]'::jsonb) FROM media_operation_cues),
		'OwnedSubtitles',(SELECT COALESCE(jsonb_agg(jsonb_build_object('OperationId',operation_id,'ItemId',item_id,'StreamIndex',stream_index,
			'Codec',codec,'Language',language,'Title',title,'Active',active,'ContentSHA256',content_sha256,'Bytes',octet_length(content)) ORDER BY item_id,stream_index),'[]'::jsonb) FROM item_owned_subtitles),
		'EmbeddedArtwork',(SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemId',item_id,'Status',status,'SourceHash',source_hash,'Width',width,'Height',height) ORDER BY item_id),'[]'::jsonb) FROM item_embedded_artwork),
		'ActiveSessions',(SELECT count(*) FROM sessions WHERE revoked_at IS NULL),
		'ActiveOperations',(SELECT count(*) FROM media_operations WHERE state IN ('queued','running','applying') OR worker_token<>'' OR publication_phase IN ('prepared','catalog_committed') OR state='recovery_required')
	)::text`).Scan(&raw)
	return json.RawMessage(raw), err
}

type selectedPhase2Observer struct {
	runtime                      *phase3BrowserRuntime
	fixture                      selectedPhase2Context
	execution                    selectedPhase2Execution
	actor                        identity.Principal
	actorToken                   string
	files                        map[string]selectedPhase2FileFact
	removePath, ocrPath, scratch string
	originalRemove               selectedPhase2FileFact
	removeMedia                  media.Info
	coverBytes                   [][]byte
	preservation                 json.RawMessage
	removal, ocr, cancelled      string
	removeReady                  library.MediaOperation
	originalCues, reviewedCues   []library.MediaOperationCue
	reviewHash                   string
	reviewRevision               int64
	durable                      json.RawMessage
	imageObservations            []map[string]any
	completed                    int
}

func (observer *selectedPhase2Observer) idle(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		var active int
		err := observer.runtime.f.pool.QueryRow(ctx, `SELECT count(*) FROM media_operations WHERE state IN ('queued','running','applying') OR worker_token<>'' OR publication_phase IN ('prepared','catalog_committed') OR state='recovery_required'`).Scan(&active)
		if err != nil {
			return errors.New("phase 2 execution closure observation failed")
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("phase 2 execution workers did not close")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (observer *selectedPhase2Observer) filesVerified() ([]selectedPhase2FileFact, error) {
	paths := make([]string, 0, len(observer.files))
	for path := range observer.files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	facts := make([]selectedPhase2FileFact, 0, len(paths))
	for _, path := range paths {
		current, err := selectedPhase2Fact(path, 32<<20)
		if err != nil || current != observer.files[path] {
			return facts, errors.New("phase 2 source differs from its explicitly authorized file state")
		}
		facts = append(facts, current)
	}
	return facts, nil
}

func (observer *selectedPhase2Observer) snapshot(ctx context.Context, phase, suffix string) (map[string]any, error) {
	database, dbErr := selectedPhase2Database(ctx, observer.runtime.f)
	facts, fileErr := observer.filesVerified()
	value := map[string]any{"Marker": "goby-selected-phase2-stage-database-v1", "RunId": observer.fixture.RunID, "Phase": phase,
		"Complete": false, "Observed": dbErr == nil, "ExpectedFilesVerified": fileErr == nil, "Files": facts}
	if dbErr == nil {
		value["Database"] = database
	}
	if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-"+suffix+".json"), value); err != nil {
		return value, errors.New("phase 2 safe database observation could not be preserved")
	}
	if dbErr != nil {
		return value, errors.New("phase 2 safe database observation failed")
	}
	if fileErr != nil && suffix != "before-observation" {
		return value, fileErr
	}
	return value, nil
}

func (observer *selectedPhase2Observer) operation(ctx context.Context, id, expectedID, item, kind, state string) (library.MediaOperation, error) {
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) || expectedID != "" && id != expectedID {
		return library.MediaOperation{}, errors.New("phase 2 stage does not bind its owned operation")
	}
	op, err := observer.runtime.f.app.library.GetMediaOperation(ctx, observer.actor, id)
	wantError := ""
	if state == "cancelled" {
		wantError = "cancelled"
	}
	if err != nil || op.ItemID != item || op.Kind != kind || op.State != state || op.WorkerToken != "" || op.ErrorCode != wantError || op.Revision < 1 {
		return op, errors.New("phase 2 operation did not reach its exact expected durable state")
	}
	if err := observer.idle(ctx); err != nil {
		return op, err
	}
	return op, nil
}

func (observer *selectedPhase2Observer) removalApplied(ctx context.Context, op library.MediaOperation) error {
	var journal struct {
		StageName, SourceSHA256 string
		Candidate               struct {
			SHA256 string
			Size   int64
		}
		Published *struct {
			SHA256 string
			Size   int64
		}
	}
	var summary struct {
		BackupRetained       bool
		RemovedStreamIndex   int
		PreservedStreamCount int
	}
	if op.PublicationPhase != "done" || !op.Applied || json.Unmarshal(op.Journal, &journal) != nil || json.Unmarshal(op.ResultSummary, &summary) != nil ||
		!summary.BackupRetained || summary.RemovedStreamIndex != observer.fixture.RemoveStreamIndex || journal.StageName != ".goby-edit-"+op.ID ||
		journal.SourceSHA256 != observer.originalRemove.SHA256 || journal.Published == nil || journal.Published.SHA256 != journal.Candidate.SHA256 {
		return errors.New("phase 2 removal lacks the committed candidate and retained original witness")
	}
	backup := filepath.Join(filepath.Dir(observer.removePath), journal.StageName, "payload")
	original, err := selectedPhase2Fact(backup, 32<<20)
	if err != nil || original.SHA256 != observer.originalRemove.SHA256 || original.Bytes != observer.originalRemove.Bytes || original.Device != observer.originalRemove.Device || original.Inode != observer.originalRemove.Inode {
		return errors.New("phase 2 retained backup differs from the original media bytes and file identity")
	}
	current, err := selectedPhase2Fact(observer.removePath, 32<<20)
	if err != nil || current.SHA256 != journal.Candidate.SHA256 || current.SHA256 == observer.originalRemove.SHA256 {
		return errors.New("phase 2 source was not replaced by the confirmed candidate")
	}
	info, err := (media.Prober{FFprobePath: observer.execution.FFprobePath, FFmpegPath: observer.execution.FFmpegPath, Timeout: 20 * time.Second}).Probe(ctx, observer.removePath)
	if err != nil || len(info.Streams) != len(observer.removeMedia.Streams)-1 || len(info.Streams) != summary.PreservedStreamCount || !reflect.DeepEqual(info.Chapters, observer.removeMedia.Chapters) {
		return errors.New("phase 2 actual remux did not preserve chapters and the nonselected streams")
	}
	video, audio, kept, removed := 0, 0, 0, 0
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && stream.Codec == "h264" {
			video++
		}
		if stream.CodecType == "audio" && stream.Codec == "aac" {
			audio++
		}
		if stream.CodecType == "subtitle" && stream.Title == observer.fixture.KeepSubtitleTitle && stream.Language == "fra" && stream.Index == observer.fixture.KeepStreamIndex {
			kept++
		}
		if stream.CodecType == "subtitle" && stream.Title == observer.fixture.RemoveSubtitleTitle {
			removed++
		}
	}
	if video != 1 || audio != 1 || kept != 1 || removed != 0 {
		return errors.New("phase 2 removed or retained the wrong actual media stream")
	}
	observer.files[observer.removePath], observer.files[backup] = current, original
	return nil
}

func (observer *selectedPhase2Observer) ocrReady(ctx context.Context, op library.MediaOperation) error {
	if op.PublicationPhase != "none" || op.Applied || op.StreamIndex != observer.fixture.OCRStreamIndex ||
		!reflect.DeepEqual(op.Parameters.ModelIDs, observer.fixture.OCRModelIDs) || op.Parameters.OutputFormat != "srt" || op.Parameters.Language != observer.fixture.OCRLanguage || op.Parameters.Title != observer.fixture.OCRTitle ||
		op.Parameters.IsDefault || op.Parameters.IsForced || op.Parameters.IsHearingImpaired {
		return errors.New("phase 2 OCR did not use the selected source, models, and output settings")
	}
	page, err := observer.runtime.f.app.library.GetMediaOperationReview(ctx, observer.actor, op.ID, library.MediaOperationPageOptions{Limit: 200})
	if err != nil || int(page.TotalRecordCount) != len(observer.fixture.OCRExpectedCues) || len(page.Items) != len(observer.fixture.OCRExpectedCues) {
		return errors.New("phase 2 OCR cue count differs from the independently authored display intervals")
	}
	for index := range page.Items {
		cue := &page.Items[index]
		cue.ImagePNG, err = observer.runtime.f.app.library.GetMediaOperationCueImage(ctx, observer.actor, op.ID, cue.Ordinal)
		if err != nil {
			return errors.New("phase 2 original OCR bitmap could not be read under administrator authority")
		}
		expected := observer.fixture.OCRExpectedCues[index]
		digest := sha256.Sum256(cue.ImagePNG)
		rgba, width, height, decodeErr := selectedPhase2RGBAHash(cue.ImagePNG)
		if cue.Ordinal != index || cue.OriginalStartTicks != expected.StartTicks || cue.OriginalEndTicks != expected.EndTicks ||
			cue.StartTicks != expected.StartTicks || cue.EndTicks != expected.EndTicks || cue.OriginalText != cue.Text || cue.IsForced != expected.Forced ||
			cue.Confidence == nil || math.IsNaN(*cue.Confidence) || *cue.Confidence <= 0 || *cue.Confidence > 100 ||
			strings.Join(strings.Fields(cue.OriginalText), "") != strings.Join(strings.Fields(expected.Text), "") ||
			decodeErr != nil || width != expected.Width || height != expected.Height || rgba != expected.RGBAHash || hex.EncodeToString(digest[:]) != cue.ImageSHA256 {
			return errors.New("phase 2 OCR did not preserve real decoded pixels, authored timing, or recognition confidence")
		}
	}
	if strings.Join(strings.Fields(page.Items[0].OriginalText), " ") != observer.fixture.ExpectedOCRPhrase {
		return errors.New("phase 2 OCR did not recognize the authored English phrase")
	}
	var summary struct {
		EngineSHA256 string
		Models       []struct {
			ID     string `json:"Id"`
			SHA256 string
		}
		CueCount int
	}
	if json.Unmarshal(op.ResultSummary, &summary) != nil || summary.EngineSHA256 != observer.execution.OCR.ToolSHA256 || summary.CueCount != len(page.Items) || len(summary.Models) != 2 {
		return errors.New("phase 2 OCR result omitted the actual engine and model identity")
	}
	for _, used := range summary.Models {
		matched := false
		for _, model := range observer.execution.OCR.Models {
			matched = matched || model.ID == used.ID && model.SHA256 == used.SHA256
		}
		if !matched {
			return errors.New("phase 2 OCR used an undeclared trained model")
		}
	}
	observer.originalCues, observer.reviewHash, observer.reviewRevision = page.Items, op.ResultHash, op.Revision
	return nil
}

func (observer *selectedPhase2Observer) ocrReviewed(ctx context.Context, op library.MediaOperation, request selectedPhase2Request) error {
	page, err := observer.runtime.f.app.library.GetMediaOperationReview(ctx, observer.actor, op.ID, library.MediaOperationPageOptions{Limit: 200})
	if err != nil || len(page.Items) != len(observer.originalCues) || len(request.CueEdits) != 2 || request.Revision != strconv.FormatInt(op.Revision, 10) ||
		request.ResultHash != op.ResultHash || op.Revision <= observer.reviewRevision || op.ResultHash == observer.reviewHash {
		return errors.New("phase 2 reviewed OCR does not bind a new persisted CAS result")
	}
	first := observer.fixture.ReviewEdits[0]
	second := observer.originalCues[1]
	wantEdits := []library.MediaOperationCueEdit{first, {Ordinal: 1, StartTicks: second.StartTicks, EndTicks: second.EndTicks, Text: second.Text, Included: false}}
	if !reflect.DeepEqual(request.CueEdits, wantEdits) {
		return errors.New("phase 2 browser did not submit the exact reviewed cue edits")
	}
	for index := range page.Items {
		cue := &page.Items[index]
		cue.ImagePNG, err = observer.runtime.f.app.library.GetMediaOperationCueImage(ctx, observer.actor, op.ID, cue.Ordinal)
		if err != nil {
			return errors.New("phase 2 reviewed OCR bitmap could not be read under administrator authority")
		}
		before := observer.originalCues[index]
		if cue.OriginalStartTicks != before.OriginalStartTicks || cue.OriginalEndTicks != before.OriginalEndTicks || cue.OriginalText != before.OriginalText ||
			cue.ImageSHA256 != before.ImageSHA256 || !bytes.Equal(cue.ImagePNG, before.ImagePNG) || !reflect.DeepEqual(cue.Confidence, before.Confidence) {
			return errors.New("phase 2 review overwrote original OCR evidence")
		}
		if index == 0 {
			before.StartTicks, before.EndTicks, before.Text, before.Included = first.StartTicks, first.EndTicks, first.Text, true
		}
		if index == 1 {
			before.Included = false
		}
		if cue.StartTicks != before.StartTicks || cue.EndTicks != before.EndTicks || cue.Text != before.Text || cue.Included != before.Included {
			return errors.New("phase 2 reviewed cue text, timing, or inclusion differs")
		}
	}
	observer.reviewedCues, observer.reviewHash, observer.reviewRevision = page.Items, op.ResultHash, op.Revision
	return nil
}

func (observer *selectedPhase2Observer) viewerHTTP(ctx context.Context, action func(string, *http.Client) error) error {
	f := observer.runtime.f
	credentials, err := f.users.Authenticate(ctx, observer.fixture.ViewerName, observer.fixture.ViewerPassword,
		identity.Client{Name: "Phase 2 independent HTTP observer", DeviceID: "phase2-http-observer", Device: "Linux", Version: "1"}, "emby")
	if err != nil {
		return errors.New("phase 2 observer authentication failed")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = f.users.Revoke(cleanupCtx, credentials.Token)
	}()
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	actionErr := action(credentials.Token, client)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, observer.fixture.BaseURL+"/emby/Sessions/Logout", nil)
	if err != nil {
		return errors.New("phase 2 observer logout request failed")
	}
	request.Header.Set("X-Emby-Token", credentials.Token)
	response, err := client.Do(request)
	if err != nil {
		return errors.Join(actionErr, errors.New("phase 2 observer HTTP logout failed"))
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.Join(actionErr, errors.New("phase 2 observer HTTP logout was rejected"))
	}
	return actionErr
}

func selectedPhase2HTTP(ctx context.Context, client *http.Client, base, path, token string) ([]byte, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("X-Emby-Token", token)
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, errors.New("phase 2 owned HTTP read failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || response.StatusCode != http.StatusOK || len(data) > 8<<20 {
		return nil, nil, errors.New("phase 2 owned HTTP response is invalid")
	}
	return data, response.Header.Clone(), nil
}

func (observer *selectedPhase2Observer) ocrApplied(ctx context.Context, op library.MediaOperation) error {
	if op.PublicationPhase != "done" || !op.Applied || op.ResultHash != observer.reviewHash {
		return errors.New("phase 2 OCR publication did not bind the saved review")
	}
	var index int
	var content []byte
	var digest, codec, language, title string
	var active bool
	err := observer.runtime.f.pool.QueryRow(ctx, `SELECT stream_index,content,content_sha256,codec,language,title,active FROM item_owned_subtitles WHERE operation_id=$1 AND item_id=$2`, op.ID, observer.fixture.OCRItemID).
		Scan(&index, &content, &digest, &codec, &language, &title, &active)
	actual := sha256.Sum256(content)
	if err != nil || !active || codec != "srt" || language != observer.fixture.OCRLanguage || title != observer.fixture.OCRTitle || hex.EncodeToString(actual[:]) != digest {
		return errors.New("phase 2 OCR publication did not own its exact reviewed subtitle bytes")
	}
	document, err := subtitle.Parse(content, subtitle.FormatSRT)
	if err != nil {
		return errors.New("phase 2 published SRT did not parse")
	}
	want := []library.MediaOperationCue{}
	for _, cue := range observer.reviewedCues {
		if cue.Included {
			want = append(want, cue)
		}
	}
	if len(document.Cues) != len(want) {
		return errors.New("phase 2 excluded OCR cue was published")
	}
	for ordinal, cue := range document.Cues {
		if cue.StartTicks != want[ordinal].StartTicks || cue.EndTicks != want[ordinal].EndTicks || cue.Text != want[ordinal].Text {
			return errors.New("phase 2 published OCR text or timing differs from the reviewed document")
		}
	}
	return observer.viewerHTTP(ctx, func(token string, client *http.Client) error {
		path := "/emby/Videos/" + observer.fixture.OCRItemID + "/" + media.SourceID(observer.fixture.OCRItemID) + "/Subtitles/" + strconv.Itoa(index) + "/0/Stream.srt?GobySubtitleTag=" + url.QueryEscape(digest)
		data, header, err := selectedPhase2HTTP(ctx, client, observer.fixture.BaseURL, path, token)
		if err != nil || !bytes.Equal(data, content) || !strings.HasPrefix(header.Get("Content-Type"), "text/plain") {
			return errors.New("phase 2 viewer HTTP did not deliver the exact applied SRT")
		}
		return featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "published-subtitle-http.json"), map[string]any{"RunId": observer.fixture.RunID, "Status": 200, "ContentSHA256": digest, "Bytes": len(data), "CueCount": len(want), "TokenInURL": false})
	})
}

func (observer *selectedPhase2Observer) artwork(ctx context.Context) error {
	return observer.viewerHTTP(ctx, func(token string, client *http.Client) error {
		for index, itemID := range []string{observer.fixture.AudioItemID, observer.fixture.SecondAudioItemID, observer.fixture.AudioLibraryID} {
			path := "/emby/Items/" + itemID + "/Images/Primary"
			data, header, err := selectedPhase2HTTP(ctx, client, observer.fixture.BaseURL, path, token)
			if err != nil {
				return err
			}
			decoded, format, decodeErr := image.Decode(bytes.NewReader(data))
			if decodeErr != nil || format != "png" || header.Get("ETag") == "" {
				return errors.New("phase 2 artwork HTTP did not deliver a real PNG representation")
			}
			listed, err := observer.runtime.f.app.library.ListImagesFor(ctx, library.Subject{UserID: observer.fixture.ViewerID}, itemID)
			if err != nil || len(listed) != 1 || listed[0].Tag != strings.Trim(header.Get("ETag"), `"`) {
				return errors.New("phase 2 artwork HTTP differs from its actual source manifest")
			}
			if index < 2 {
				if listed[0].Source != "embedded" || !bytes.Equal(data, observer.coverBytes[index]) {
					return errors.New("phase 2 audio artwork is not the embedded owned cover bytes")
				}
			} else {
				if listed[0].Source != "generated" || decoded.Bounds().Dx() != 512 || decoded.Bounds().Dy() != 512 {
					return errors.New("phase 2 library artwork is not an automatic collage")
				}
				colors := map[color.NRGBA]bool{}
				for _, at := range [][2]int{{128, 128}, {384, 128}, {128, 384}, {384, 384}} {
					colors[color.NRGBAModel.Convert(decoded.At(at[0], at[1])).(color.NRGBA)] = true
				}
				if !colors[color.NRGBA{R: 224, G: 32, B: 48, A: 255}] || !colors[color.NRGBA{R: 32, G: 80, B: 224, A: 255}] || len(colors) != 2 {
					return errors.New("phase 2 collage did not contain the two distinct owned embedded covers")
				}
			}
			digest := sha256.Sum256(data)
			observer.imageObservations = append(observer.imageObservations, map[string]any{"Path": path, "Status": 200, "Source": listed[0].Source,
				"Width": decoded.Bounds().Dx(), "Height": decoded.Bounds().Dy(), "ETag": header.Get("ETag"), "ContentSHA256": hex.EncodeToString(digest[:]), "SHA256": hex.EncodeToString(digest[:]), "Complete": true, "Decoded": true, "Observation": "independent authorized HTTP"})
		}
		return nil
	})
}

func (observer *selectedPhase2Observer) stage(ctx context.Context, request selectedPhase2Request) error {
	f, fixture := observer.runtime.f, observer.fixture
	switch request.Phase {
	case "authentication":
		var operations int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM media_operations").Scan(&operations) != nil || operations != 0 {
			return errors.New("phase 2 did not start from an empty operation history")
		}
	case "removal-ready":
		op, err := observer.operation(ctx, request.OperationID, "", fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "ready")
		if err != nil {
			return err
		}
		if op.StreamIndex != fixture.RemoveStreamIndex || op.PublicationPhase != "none" || op.Applied || len(op.ResultHash) != 64 {
			return errors.New("phase 2 removal preparation selected the wrong original stream")
		}
		observer.removal, observer.removeReady = op.ID, op
	case "removal-applied":
		op, err := observer.operation(ctx, request.OperationID, observer.removal, fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "completed")
		if err != nil {
			return err
		}
		if err := observer.removalApplied(ctx, op); err != nil {
			return err
		}
	case "ocr-ready":
		op, err := observer.operation(ctx, request.OperationID, "", fixture.OCRItemID, library.MediaOperationOCR, "ready")
		if err != nil {
			return err
		}
		observer.ocr = op.ID
		if err := observer.ocrReady(ctx, op); err != nil {
			return err
		}
	case "ocr-reviewed":
		op, err := observer.operation(ctx, request.OperationID, observer.ocr, fixture.OCRItemID, library.MediaOperationOCR, "ready")
		if err != nil {
			return err
		}
		if err := observer.ocrReviewed(ctx, op, request); err != nil {
			return err
		}
	case "ocr-applied":
		op, err := observer.operation(ctx, request.OperationID, observer.ocr, fixture.OCRItemID, library.MediaOperationOCR, "completed")
		if err != nil {
			return err
		}
		if err := observer.ocrApplied(ctx, op); err != nil {
			return err
		}
	case "cancel-ready":
		op, err := observer.operation(ctx, request.OperationID, "", fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "ready")
		if err != nil {
			return err
		}
		if op.ID == observer.removal || op.StreamIndex != fixture.KeepStreamIndex || op.PublicationPhase != "none" || op.Applied {
			return errors.New("phase 2 cancellation preparation did not target the retained stream")
		}
		observer.cancelled = op.ID
	case "cancelled":
		op, err := observer.operation(ctx, request.OperationID, observer.cancelled, fixture.RemoveItemID, library.MediaOperationRemoveSubtitle, "cancelled")
		if err != nil {
			return err
		}
		if op.Applied || op.PublicationPhase != "none" {
			return errors.New("phase 2 cancelled operation published media")
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(observer.removePath), ".goby-edit-"+op.ID, "payload")); !errors.Is(err, os.ErrNotExist) {
			return errors.New("phase 2 cancellation retained its unpublished candidate bytes")
		}
	case "artwork":
		if err := observer.artwork(ctx); err != nil {
			return err
		}
	case "restart":
		if err := observer.idle(ctx); err != nil {
			return err
		}
		if err := observer.runtime.restart(ctx); err != nil {
			return errors.New("phase 2 complete application restart failed")
		}
	case "persisted":
		expected := []string{observer.removal, observer.ocr, observer.cancelled}
		actual := append([]string(nil), request.HistoryIDs...)
		slices.Sort(expected)
		slices.Sort(actual)
		if !reflect.DeepEqual(expected, actual) {
			return errors.New("phase 2 browser history does not bind all three real operations")
		}
	case "cleanup":
		if err := observer.idle(ctx); err != nil {
			return err
		}
		// The coordinator owns its worker map. Only inspect it after Close has
		// joined the real executor loop, processes, and pending storage work.
		if err := f.app.mediaOperations.Close(ctx); err != nil || len(f.app.mediaOperations.workers) != 0 {
			return errors.New("phase 2 actual media operation workers did not close")
		}
		if err := f.users.Revoke(ctx, observer.actorToken); err != nil {
			return errors.New("phase 2 fixture observer session could not be retired")
		}
		var sessions, playback, encodings, scans, tasks int
		if f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sessions WHERE revoked_at IS NULL),(SELECT count(*) FROM play_sessions),
			(SELECT count(*) FROM encoding_jobs),(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),
			(SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping'))`).Scan(&sessions, &playback, &encodings, &scans, &tasks) != nil || sessions != 0 || playback != 0 || encodings != 0 || scans != 0 || tasks != 0 {
			return errors.New("phase 2 left authentication or runtime work after cleanup")
		}
	}
	if _, err := observer.filesVerified(); err != nil {
		return err
	}
	preserved, err := selectedPhase2Preservation(ctx, f, fixture.ViewerID, []string{fixture.RemoveItemID, fixture.OCRItemID})
	if err != nil || !bytes.Equal(preserved, observer.preservation) {
		return errors.New("phase 2 changed catalog identity, metadata, or complete user data")
	}
	if request.Phase == "artwork" {
		observer.durable, err = selectedPhase2Database(ctx, f)
		if err != nil {
			return err
		}
	}
	if request.Phase == "restart" || request.Phase == "persisted" {
		current, err := selectedPhase2Database(ctx, f)
		if err != nil || !bytes.Equal(current, observer.durable) {
			return errors.New("phase 2 restart changed completed operation history or replayed publication")
		}
	}
	return nil
}

func (observer *selectedPhase2Observer) run(ctx context.Context) error {
	for _, phase := range selectedPhase2Phases {
		if err := phase3BrowserWaitRequest(ctx, phase3BrowserContext{RunID: observer.fixture.RunID, ArtifactsDir: observer.fixture.ArtifactsDir}, phase); err != nil {
			return errors.New("phase 2 browser stage request failed")
		}
		var request selectedPhase2Request
		if featureWavePrivateJSON(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-request.json"), 64<<10, &request) != nil || request.RunID != observer.fixture.RunID || request.Phase != phase {
			return errors.New("phase 2 request does not bind the expected stage")
		}
		_, stageErr := observer.snapshot(ctx, phase, "before-observation")
		if stageErr == nil {
			stageErr = observer.stage(ctx, request)
		}
		value, snapshotErr := observer.snapshot(ctx, phase, "after-observation")
		if stageErr == nil {
			stageErr = snapshotErr
		}
		if stageErr != nil {
			value["Failed"], value["ErrorCode"], value["FailureDetail"] = true, "database_stage_verification_failed", stageErr.Error()
			_ = featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json"), value)
			return stageErr
		}
		value["Complete"], value["ExpectedFilesVerified"] = true, true
		if phase == "artwork" {
			value["ImageObservations"] = observer.imageObservations
		}
		if err := featureWaveWriteCheckpoint(filepath.Join(observer.fixture.ArtifactsDir, "stage-"+phase+"-database.json"), value); err != nil {
			return errors.New("phase 2 stage acknowledgement could not be preserved")
		}
		observer.completed++
	}
	return nil
}

func TestSelectedCompatibilityPhase2BrowserIntegration(t *testing.T) {
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("phase 2 browser admission requires root and an owned integration database")
	}
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	playwright := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_MODULE", false)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	execution, inventory := selectedPhase2ReadExecution(t)
	runID := os.Getenv("GOBY_SELECTED_PHASE2_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("an explicit phase 2 browser run identity is required")
	}
	if info, err := os.Stat(artifacts); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("phase 2 artifact parent must be private")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read phase 2 source directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate phase 2 source root")
	}
	for _, input := range []string{node, playwright, filepath.Join(sourceRoot, "scripts", "test-env", "bitmap-subtitle-fixtures.py"),
		filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase2-browser.mjs")} {
		fact, err := selectedPhase2Fact(input, 256<<20)
		if err != nil {
			t.Fatal("capture phase 2 browser and authoring source identities")
		}
		inventory = append(inventory, fact)
	}
	output, err := os.MkdirTemp(artifacts, "selected-phase2-browser-")
	if err != nil || os.Chmod(output, 0o700) != nil {
		t.Fatal("create phase 2 private artifact directory")
	}
	driver := map[string]any{"Marker": "goby-selected-phase2-browser-driver-v1", "RunId": runID, "Complete": false, "ArtifactDirectory": output,
		"CredentialsWrittenToSummary": false, "RealProber": true, "OCRRecognitionExercised": false, "OriginalClientUsed": false}
	var schema, mediaRoot string
	t.Cleanup(func() {
		if schema != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			pool, err := pgxpool.New(ctx, os.Getenv("GOBY_TEST_DATABASE_URL"))
			if err != nil {
				t.Error("observe phase 2 schema cleanup")
			} else {
				defer pool.Close()
				var remains bool
				if pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)", schema).Scan(&remains) != nil || remains {
					t.Error("phase 2 owned schema was not removed")
				} else {
					driver["OwnedSchemaRemoved"] = true
				}
			}
		}
		if mediaRoot != "" {
			if _, err := os.Lstat(mediaRoot); !errors.Is(err, os.ErrNotExist) {
				t.Error("phase 2 owned media root was not removed")
			} else {
				driver["OwnedMediaRootRemoved"] = true
			}
		}
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver) != nil {
			t.Error("preserve phase 2 driver result")
		}
	})
	if refreshBrowserWriteJSON(filepath.Join(output, "execution-inventory.json"), inventory) != nil {
		t.Fatal("preserve pinned phase 2 execution identities")
	}
	f := newServerFixtureWithTimeout(t, 12*time.Minute)
	if f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema) != nil {
		t.Fatal("identify phase 2 owned schema")
	}
	mediaRoot = t.TempDir()
	inputDir := filepath.Join(mediaRoot, "authoring")
	movieDir := filepath.Join(mediaRoot, "movies")
	musicDir := filepath.Join(mediaRoot, "music")
	for _, dir := range []string{inputDir, movieDir, filepath.Join(musicDir, "First Album"), filepath.Join(musicDir, "Second Album")} {
		if os.MkdirAll(dir, 0o700) != nil {
			t.Fatal("create phase 2 owned source directories")
		}
	}
	bitmapDir := filepath.Join(inputDir, "bitmap")
	generator := filepath.Join(sourceRoot, "scripts", "test-env", "bitmap-subtitle-fixtures.py")
	hlsHTTPMediaCommand(t, execution.PythonPath, "-I", generator, "--font-file", execution.FontPath, "--font-sha256", execution.FontSHA256, "--output-dir", bitmapDir)
	manifestPath := filepath.Join(bitmapDir, "manifest.json")
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil || len(manifestRaw) > 1<<20 {
		t.Fatal("read bounded phase 2 authored bitmap manifest")
	}
	var manifest selectedPhase2BitmapManifest
	if json.Unmarshal(manifestRaw, &manifest) != nil || manifest.Format != "goby-bitmap-subtitle-fixtures-v1" || manifest.DurationTicks != 97_280_000 {
		t.Fatal("phase 2 bitmap authoring manifest identity differs")
	}
	var expected []selectedPhase2ExpectedCue
	bitmapPath := ""
	for _, item := range manifest.Cases {
		if item.Name == "overlap" {
			expected = item.Intervals
			bitmapPath = filepath.Join(bitmapDir, item.File)
			if item.Origin != 0 || !reflect.DeepEqual(item.Models, []string{"eng", "chi_sim"}) {
				t.Fatal("phase 2 authored overlap origin or models differ")
			}
		}
	}
	if len(expected) != 4 || bitmapPath != filepath.Join(bitmapDir, "overlap-pgs.mks") {
		t.Fatal("phase 2 authored PGS overlap display intervals are missing")
	}
	starts := []int64{10_240_000, 25_600_000, 40_960_000, 66_560_000}
	ends := []int64{25_600_000, 40_960_000, 56_320_000, 87_040_000}
	texts := []string{"Hello world", "Hello world\n\u4e2d\u6587\u6d4b\u8bd5", "\u4e2d\u6587\u6d4b\u8bd5", "Hello world"}
	for index, cue := range expected {
		if cue.StartTicks != starts[index] || cue.EndTicks != ends[index] || cue.Text != texts[index] || cue.Forced || cue.Width < 1 || cue.Height < 1 || len(cue.RGBAHash) != 64 {
			t.Fatal("phase 2 bitmap generator changed the fixed independent overlap ground truth")
		}
	}
	for name, recorded := range manifest.Files {
		if filepath.Base(name) != name {
			t.Fatal("phase 2 authored fixture file escaped its directory")
		}
		fact, err := selectedPhase2Fact(filepath.Join(bitmapDir, name), 32<<20)
		if err != nil || fact.Bytes != recorded.Bytes || fact.SHA256 != recorded.SHA256 {
			t.Fatal("phase 2 authored fixture file differs from its manifest")
		}
	}
	if refreshBrowserWriteJSON(filepath.Join(output, "bitmap-authoring-manifest.json"), json.RawMessage(manifestRaw)) != nil {
		t.Fatal("preserve independent phase 2 authored bitmap evidence")
	}
	removeText := filepath.Join(inputDir, "remove.srt")
	keepText := filepath.Join(inputDir, "keep.srt")
	chapterPath := filepath.Join(inputDir, "chapters.ffmetadata")
	for path, contents := range map[string]string{removeText: "1\n00:00:01,000 --> 00:00:03,000\nRemove this English track\n", keepText: "1\n00:00:02,000 --> 00:00:04,000\nConserver cette piste\n",
		chapterPath: ";FFMETADATA1\ntitle=Selected Phase Two Source\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=3000\ntitle=Opening\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=3000\nEND=12000\ntitle=Main\n"} {
		if os.WriteFile(path, []byte(contents), 0o600) != nil {
			t.Fatal("write owned phase 2 subtitle and chapter sources")
		}
	}
	removePath := filepath.Join(movieDir, "Phase Two Remove.mkv")
	ocrPath := filepath.Join(movieDir, "Phase Two OCR.mkv")
	selectedPhase2GenerateVideo(t, execution, removePath, "", removeText, keepText, chapterPath)
	selectedPhase2GenerateVideo(t, execution, ocrPath, bitmapPath, "", "", "")
	coverPaths := []string{filepath.Join(inputDir, "cover-red.png"), filepath.Join(inputDir, "cover-blue.png")}
	coverBytes := [][]byte{selectedPhase2PNG(t, coverPaths[0], color.NRGBA{R: 224, G: 32, B: 48, A: 255}), selectedPhase2PNG(t, coverPaths[1], color.NRGBA{R: 32, G: 80, B: 224, A: 255})}
	audioPaths := []string{filepath.Join(musicDir, "First Album", "First Track.flac"), filepath.Join(musicDir, "Second Album", "Second Track.flac")}
	for index, path := range audioPaths {
		hlsHTTPMediaCommand(t, execution.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=800:sample_rate=48000", "-i", coverPaths[index],
			"-map", "0:a:0", "-map", "1:v:0", "-af", "atrim=end_sample=9600,asetpts=PTS-STARTPTS", "-c:a", "flac", "-c:v", "copy", "-disposition:v:0", "attached_pic",
			"-metadata:s:v:0", "comment=Cover (front)", "-metadata:s:v:0", "title=Embedded source cover", path)
		if os.Chmod(path, 0o600) != nil {
			t.Fatal("protect phase 2 real audio source")
		}
	}
	files := map[string]selectedPhase2FileFact{}
	for _, path := range append([]string{removePath, ocrPath, removeText, keepText, chapterPath, bitmapPath, manifestPath, coverPaths[0], coverPaths[1]}, audioPaths...) {
		fact, err := selectedPhase2Fact(path, 32<<20)
		if err != nil {
			t.Fatal("capture phase 2 owned source fingerprints")
		}
		files[path] = fact
	}
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("close initial phase 2 application")
	}
	scratch := t.TempDir()
	f.cfg.FFmpegPath, f.cfg.FFprobePath, f.cfg.MediaRoots = execution.FFmpegPath, execution.FFprobePath, []string{mediaRoot}
	f.cfg.MediaOperations = config.MediaOperationsConfig{Enabled: true, MaxConcurrent: 1, MaxQueued: 4, MaxRuntimeSeconds: 90, MaxScratchBytes: 512 << 20, ScratchDirectory: scratch, WritableProfiles: []string{"matroska-v1"}, OCR: execution.OCR}
	assets, err := adminassets.Files()
	if err != nil {
		t.Fatal("open phase 2 embedded native administrator assets")
	}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "selected-phase2-browser-integration", WithDashboardAssets(assets))
	if err != nil {
		t.Fatal("construct phase 2 actual media operation application")
	}
	f.app, f.handler = app, app.Handler()
	runtime := &phase3BrowserRuntime{f: f, assets: assets, addr: "127.0.0.1:0"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var active int
		if f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active) != nil {
			t.Error("observe phase 2 residual sessions")
		}
		driver["ActiveSessionsBeforeFallback"] = active
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("retire phase 2 residual owned sessions")
		} else {
			driver["FallbackSessionRevocations"] = result.RowsAffected()
			if driver["Complete"] == true && result.RowsAffected() != 0 {
				t.Error("successful phase 2 browser required fallback session revocation")
			}
		}
		if runtime.close(ctx) != nil {
			t.Error("close phase 2 actual workers")
		} else {
			driver["ServerWorkersClosed"], driver["HTTPListenerClosed"] = true, true
		}
	})
	if !app.mediaOperations.Available() || !app.mediaOperations.ocrReady {
		t.Fatal("phase 2 pinned real media and OCR execution inventory is unavailable")
	}
	adminPassword, viewerPassword := featureWavePassword(t), featureWavePassword(t)
	admin, err := f.users.Bootstrap(f.ctx, "Selected phase 2 administrator", adminPassword)
	if err != nil {
		t.Fatal("bootstrap phase 2 native administrator")
	}
	viewer, err := f.users.CreateUser(f.ctx, "Selected phase 2 viewer", viewerPassword, false)
	if err != nil {
		t.Fatal("create phase 2 independent HTTP viewer")
	}
	credentials, err := f.users.Authenticate(f.ctx, admin.Name, adminPassword, identity.Client{Name: "Phase 2 fixture observer", DeviceID: "phase2-fixture-observer", Device: "Linux", Version: "1"}, "admin")
	if err != nil {
		t.Fatal("authenticate phase 2 fixture observer")
	}
	actor, err := f.users.Resolve(f.ctx, credentials.Token, "admin")
	if err != nil {
		t.Fatal("resolve phase 2 fixture observer")
	}
	movies, err := app.library.CreateLibrary(f.ctx, "Selected Phase Two Movies", "movies", []string{movieDir})
	if err != nil {
		t.Fatal("create phase 2 real movie library")
	}
	music, err := app.library.CreateLibrary(f.ctx, "Selected Phase Two Music", "music", []string{musicDir})
	if err != nil {
		t.Fatal("create phase 2 real music library")
	}
	selectedPhase1Scan(t, f, movies.ID, 2)
	selectedPhase1Scan(t, f, music.ID, 2)
	readItem := func(path string) library.Item {
		var id string
		if f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE path=$1", path).Scan(&id) != nil {
			t.Fatal("read phase 2 scanned item identity")
		}
		item, err := app.library.GetItem(f.ctx, viewer.ID, id)
		if err != nil || item.Media == nil || item.Media.ProbeVersion != media.CurrentProbeVersion {
			t.Fatal("phase 2 item lacks real current media probe facts")
		}
		return item
	}
	removeItem, ocrItem, firstAudio, secondAudio := readItem(removePath), readItem(ocrPath), readItem(audioPaths[0]), readItem(audioPaths[1])
	if !ocrItem.Media.FormatStartKnown || ocrItem.Media.FormatStartTicks != 0 || ocrItem.Media.DurationTicks < 110_000_000 || len(removeItem.Media.Chapters) != 2 {
		t.Fatal("phase 2 actual media does not preserve authored timing and chapters")
	}
	removeIndex, keepIndex, ocrIndex := -1, -1, -1
	for _, stream := range removeItem.Media.Streams {
		if stream.CodecType == "subtitle" && stream.Title == "Remove English" {
			removeIndex = stream.Index
		}
		if stream.CodecType == "subtitle" && stream.Title == "Keep French" {
			keepIndex = stream.Index
		}
	}
	for _, stream := range ocrItem.Media.Streams {
		if stream.CodecType == "subtitle" && stream.Codec == "hdmv_pgs_subtitle" {
			ocrIndex = stream.Index
		}
	}
	if removeIndex < 0 || keepIndex < 0 || ocrIndex < 0 {
		t.Fatal("phase 2 actual scans did not expose the authored subtitle streams")
	}
	if keepIndex > removeIndex {
		keepIndex--
	}
	for _, id := range []string{removeItem.ID, ocrItem.ID} {
		detail, err := app.library.GetItemMetadata(f.ctx, actor, id)
		if err != nil {
			t.Fatal("read phase 2 metadata baseline")
		}
		value, _ := json.Marshal("Phase 2 preserved manual overview")
		if _, err := app.library.UpdateItemMetadata(f.ctx, actor, id, library.MetadataEdit{Revision: detail.Revision, Overrides: map[string]json.RawMessage{"Overview": value}}); err != nil {
			t.Fatal("seed phase 2 preserved manual metadata")
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at) VALUES($1,$2,50000000,3,true,false,'2026-01-02T03:04:05Z')`, viewer.ID, id); err != nil {
			t.Fatal("seed phase 2 persistent owned user data")
		}
	}
	preservation, err := selectedPhase2Preservation(f.ctx, f, viewer.ID, []string{removeItem.ID, ocrItem.ID})
	if err != nil {
		t.Fatal("capture phase 2 user data and metadata preservation baseline")
	}
	embedded, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		t.Fatal("inventory phase 2 native assets")
	}
	sourceAssets, err := refreshBrowserAssetInventory(os.DirFS(filepath.Join(sourceRoot, "web", "admin", "dist")))
	if err != nil || !reflect.DeepEqual(embedded, sourceAssets) {
		t.Fatal("phase 2 embedded administrator assets differ from source")
	}
	if refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), embedded) != nil {
		t.Fatal("preserve phase 2 embedded asset inventory")
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	if runtime.listen() != nil {
		t.Fatal("start phase 2 owned TCP4 listener")
	}
	fixture := selectedPhase2Context{Marker: "goby-selected-phase2-browser-fixture-v1", RunID: runID, BaseURL: "http://" + runtime.addr,
		AdminID: admin.ID, AdminName: admin.Name, AdminPassword: adminPassword, ViewerID: viewer.ID, ViewerName: viewer.Name, ViewerPassword: viewerPassword,
		LibraryID: movies.ID, LibraryName: movies.Name, AudioLibraryID: music.ID, AudioLibraryName: music.Name, RemoveItemID: removeItem.ID, RemoveItemName: removeItem.Name,
		OCRItemID: ocrItem.ID, OCRItemName: ocrItem.Name, AudioItemID: firstAudio.ID, AudioItemName: firstAudio.Name, SecondAudioItemID: secondAudio.ID,
		RemoveStreamIndex: removeIndex, KeepStreamIndex: keepIndex, OCRStreamIndex: ocrIndex, RemoveSubtitleTitle: "Remove English", KeepSubtitleTitle: "Keep French",
		OCRModelIDs: []string{"eng", "chi_sim"}, OCRLanguage: "eng", OCRTitle: "Phase 2 reviewed subtitles", ExpectedOCRPhrase: "Hello world", OCRExpectedCues: expected,
		ReviewEdits:  []library.MediaOperationCueEdit{{Ordinal: 0, StartTicks: 12_500_000, EndTicks: 24_000_000, Text: "Phase 2 reviewed subtitle", Included: true}},
		ArtifactsDir: output, ResultPath: filepath.Join(output, "browser-result.json")}
	contextPath := filepath.Join(output, "private-context.json")
	t.Cleanup(func() {
		if err := os.Remove(contextPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Error("remove phase 2 private browser credentials")
		} else {
			driver["PrivateContextRemoved"] = true
		}
	})
	if refreshBrowserWriteJSON(contextPath, fixture) != nil {
		t.Fatal("write private phase 2 browser context")
	}
	observer := &selectedPhase2Observer{runtime: runtime, fixture: fixture, execution: execution, actor: actor, actorToken: credentials.Token, files: files,
		removePath: removePath, ocrPath: ocrPath, scratch: scratch, originalRemove: files[removePath], removeMedia: *removeItem.Media, coverBytes: coverBytes, preservation: preservation}
	if _, err := observer.snapshot(f.ctx, "seeded", "database"); err != nil {
		t.Fatal("preserve phase 2 real source seed evidence")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if os.Mkdir(filepath.Join(output, name), 0o700) != nil {
			t.Fatal("create phase 2 private browser runtime directories")
		}
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1", "HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1", "GOBY_TEST_PLAYWRIGHT_MODULE=" + playwright, "GOBY_SELECTED_PHASE2_RUN_ID=" + runID, "GOBY_SELECTED_PHASE2_CONTEXT=" + contextPath}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Minute)
	defer cancel()
	observerCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- observer.run(observerCtx) }()
	command, commandErr := refreshBrowserCommand(ctx, node, []string{filepath.Join(sourceRoot, "scripts", "test-env", "selected-compatibility-phase2-browser.mjs")}, environment, sourceRoot, output)
	stop()
	observerErr := <-done
	driver["BrowserCommand"], driver["CompletedDatabaseStages"] = command, observer.completed
	driver["OCRRecognitionExercised"] = len(observer.originalCues) > 0
	if observerErr != nil && observer.completed < len(selectedPhase2Phases) {
		driver["FailedDatabaseStage"], driver["DatabaseObservationError"] = selectedPhase2Phases[observer.completed], observerErr.Error()
	}
	var result selectedPhase2Result
	readErr := featureWavePrivateJSON(fixture.ResultPath, 1<<20, &result)
	if commandErr != nil || observerErr != nil || readErr != nil || observer.completed != len(selectedPhase2Phases) {
		t.Fatal("phase 2 browser or independent observer failed; inspect retained private artifacts")
	}
	checks := []string{"Authentication", "RemovalPrepared", "RemovalApplied", "OCRRecognized", "OCRReviewed", "OCRApplied", "CancelPrepared", "Cancelled", "ArtworkObserved", "RestartPersisted", "HistoryPersisted", "Cleanup"}
	if result.Marker != "goby-selected-phase2-browser-result-v1" || result.RunID != runID || !result.Complete || len(result.Checks) != len(checks) || len(result.Stages) != len(selectedPhase2Phases) || result.PageErrors == nil || *result.PageErrors != 0 || result.ForeignRequests == nil || *result.ForeignRequests != 0 {
		t.Fatal("phase 2 result does not bind the complete real scenario")
	}
	for index, phase := range selectedPhase2Phases {
		if result.Stages[index].Phase != phase || result.Stages[index].State != "complete" {
			t.Fatal("phase 2 browser stages differ from independently observed states")
		}
	}
	for _, name := range checks {
		if !result.Checks[name] {
			t.Fatal("phase 2 browser check did not complete")
		}
	}
	driver["Complete"], driver["ExpectedFilesVerified"], driver["OwnedSessionsRetired"] = true, true, true
	driver["BrowserChecks"], driver["ImageObservations"], driver["ServerRestarts"] = result.Checks, observer.imageObservations, 1
	t.Log("selected_phase2_browser_verified=true stages=12 real_media_operations=3 real_ocr=true server_restarts=1")
}
