//go:build linux

package library

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const scanQueryPlanRecordCapacity = 8192
const scanQueryPlanTemplateCapacity = 256

var scanQueryPlanOrder = [...]string{"D", "C", "C", "D", "C", "D", "D", "C"}

type scanQueryPlanQueryKey struct{}
type scanQueryPlanPrepareKey struct{}

type scanQueryPlanTemplate struct {
	sql, family string
}

type scanQueryPlanRecord struct {
	Kind                 string `json:"kind"`
	Family               string `json:"family"`
	TemplateSHA256       string `json:"template_sha256"`
	Ordinal              int    `json:"query_ordinal"`
	BackendPID           uint32 `json:"backend_pid"`
	Mode                 string `json:"mode,omitempty"`
	FirstObservedModeUse bool   `json:"first_observed_mode_use,omitempty"`
	StartedUnixNS        int64  `json:"started_unix_ns"`
	EndedUnixNS          int64  `json:"ended_unix_ns"`
	ElapsedNS            int64  `json:"elapsed_ns"`
	PathSHA256           string `json:"relative_path_sha256,omitempty"`
	ItemKeySHA256        string `json:"item_key_sha256,omitempty"`
	MediaKind            string `json:"media_kind,omitempty"`
	NamedPrepare         bool   `json:"named_prepare,omitempty"`
	AlreadyPrepared      bool   `json:"already_prepared,omitempty"`
	Error                bool   `json:"error"`
	template             int
	started              time.Time
	path, itemKey        string
}

type scanQueryPlanTemplateCount struct {
	SHA256 string `json:"sha256"`
	Family string `json:"family"`
	Count  int    `json:"count"`
}

type scanQueryPlanPhase struct {
	Phase             string                       `json:"phase"`
	Mode              string                       `json:"mode"`
	JobStartedUnixNS  int64                        `json:"job_started_unix_ns"`
	JobFinishedUnixNS int64                        `json:"job_finished_unix_ns"`
	JobElapsedNS      int64                        `json:"job_elapsed_ns"`
	ObservedElapsedNS int64                        `json:"observed_terminal_elapsed_ns"`
	Queries           int                          `json:"application_queries"`
	Prepares          int                          `json:"prepares"`
	Templates         []scanQueryPlanTemplateCount `json:"templates"`
	Records           []scanQueryPlanRecord        `json:"records"`
	PathOrderSHA256   string                       `json:"path_order_sha256"`
	Control           bool                         `json:"post_removal_control"`
	ResultQualified   bool                         `json:"result_qualified"`
	counts            [scanQueryPlanTemplateCapacity]int
	begin, end        int
}

type scanQueryPlanUse struct {
	pid          uint32
	family, mode string
}

// This observer changes no arguments or SQL. Only the test driver changes the
// Store's real query configuration, after the preceding worker has retired.
type scanQueryPlanControl struct {
	mu        sync.Mutex
	delegate  *scanRealImageTrace
	store     *Store
	active    bool
	mode      string
	problem   string
	records   []scanQueryPlanRecord
	phases    []scanQueryPlanPhase
	templates []scanQueryPlanTemplate
	bySQL     map[string]int
	firstUse  map[scanQueryPlanUse]bool
}

func TestScanQueryPlanReusePerformance(t *testing.T) {
	if os.Getenv("GOBY_TEST_SCAN_QUERY_PLAN_REUSE") != "1" {
		t.Skip("GOBY_TEST_SCAN_QUERY_PLAN_REUSE=1 enables the bounded query-plan crossover")
	}
	if os.Getenv("GOBY_TEST_SCAN_REAL_IMAGES") != "1" {
		t.Fatal("the query-plan crossover also requires GOBY_TEST_SCAN_REAL_IMAGES=1")
	}
	control := &scanQueryPlanControl{mode: "C", records: make([]scanQueryPlanRecord, 0, scanQueryPlanRecordCapacity),
		phases: make([]scanQueryPlanPhase, 0, 13), templates: make([]scanQueryPlanTemplate, 0, scanQueryPlanTemplateCapacity),
		bySQL: make(map[string]int, scanQueryPlanTemplateCapacity), firstUse: make(map[scanQueryPlanUse]bool, 64)}
	runScanRealImagesPerformance(t, control)
}

func (control *scanQueryPlanControl) configure(t *testing.T, config *pgxpool.Config, delegate *scanRealImageTrace) {
	t.Helper()
	if delegate.disabled || config.BeforeConnect != nil {
		t.Fatal("query-plan controls require the SQL tracer and homogeneous physical connection configuration")
	}
	// Match the production default rather than pgxpool.ParseConfig's default.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	config.ConnConfig.StatementCacheCapacity = 512
	config.ConnConfig.DescriptionCacheCapacity = 512
	config.ConnConfig.RuntimeParams["jit"] = "off"
	control.delegate = delegate
	config.ConnConfig.Tracer = control
}

func (control *scanQueryPlanControl) bind(t *testing.T, store *Store) {
	t.Helper()
	if store.scanMediaFactsReadMode != pgx.QueryExecModeCacheStatement || store.scanBitmapPresenceReadMode != pgx.QueryExecModeCacheStatement {
		t.Fatal("the unchanged production constructor did not select both local statement-cache exceptions")
	}
	owner := store.ownership.conn.Conn().Config()
	if owner.DefaultQueryExecMode != pgx.QueryExecModeCacheDescribe || owner.StatementCacheCapacity != 512 ||
		owner.DescriptionCacheCapacity != 512 || owner.RuntimeParams["jit"] != "off" {
		t.Fatal("query-plan owner configuration differs from the selected production baseline")
	}
	control.store = store
}

func (control *scanQueryPlanControl) begin(t *testing.T, phase string) {
	t.Helper()
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.active || len(control.phases) == cap(control.phases) {
		t.Fatal("query-plan phase lifetime or fixed capacity differs")
	}
	control.phases = append(control.phases, scanQueryPlanPhase{Phase: phase, Mode: control.mode,
		Control: strings.HasPrefix(phase, "query_plan_"), begin: len(control.records)})
	control.active = true
}

func (control *scanQueryPlanControl) end(t *testing.T, job Job, elapsed time.Duration) {
	t.Helper()
	control.mu.Lock()
	defer control.mu.Unlock()
	control.active = false
	phase := &control.phases[len(control.phases)-1]
	phase.end = len(control.records)
	phase.ObservedElapsedNS = elapsed.Nanoseconds()
	if job.StartedAt != nil && job.FinishedAt != nil {
		phase.JobStartedUnixNS, phase.JobFinishedUnixNS = job.StartedAt.UnixNano(), job.FinishedAt.UnixNano()
		phase.JobElapsedNS = job.FinishedAt.Sub(*job.StartedAt).Nanoseconds()
	}
	if control.problem != "" {
		t.Fatalf("query-plan recorder: %s", control.problem)
	}
	for _, record := range control.records[phase.begin:phase.end] {
		if record.EndedUnixNS == 0 {
			t.Fatal("query-plan callback remained active after worker retirement")
		}
	}
}

func (control *scanQueryPlanControl) observeResult(t *testing.T, job Job, probes int64, rows, added, removed, changed, unchanged int) {
	t.Helper()
	phase := &control.phases[len(control.phases)-1]
	phase.ResultQualified = true
	if phase.Control {
		phase.ResultQualified = job.Scanned == 160 && job.Added == 0 && job.Updated == 0 && job.Error == "" &&
			!job.ForceProbe && probes == 0 && rows == 671 && added == 0 && removed == 0 && changed == 0 && unchanged == 671
		if !phase.ResultQualified {
			t.Fatal("query-plan control changed the fixed post-removal workload")
		}
	}
}

func (control *scanQueryPlanControl) runControls(t *testing.T, store *Store, scan func(string, bool, int64)) {
	t.Helper()
	// scan returns only after worker retirement and the original tuple guards.
	// Both caches remain populated. The first D use can prepare its description;
	// it is retained and labelled, not discarded or called a clean cold control.
	for index, mode := range scanQueryPlanOrder {
		selected := pgx.QueryExecModeCacheStatement
		if mode == "D" {
			selected = pgx.QueryExecModeCacheDescribe
		}
		store.scanMediaFactsReadMode, store.scanBitmapPresenceReadMode = selected, selected
		control.mode = mode
		scan(fmt.Sprintf("query_plan_%02d_%s", index+1, mode), false, 0)
	}
	store.scanMediaFactsReadMode = pgx.QueryExecModeCacheStatement
	store.scanBitmapPresenceReadMode = pgx.QueryExecModeCacheStatement
	control.mode = "C"
}

func scanQueryPlanFamily(sql string) string {
	switch {
	case sql == "SELECT EXISTS(SELECT 1 FROM item_bitmap_subtitles WHERE item_id=$1 AND active)":
		return "bitmap"
	case strings.HasPrefix(sql, "SELECT i.file_identity, i.file_size, i.modified_at, i.media,") && strings.HasSuffix(sql, "AND NOT i.is_folder AND i.media IS NOT NULL"):
		return "media"
	case strings.HasPrefix(sql, "SELECT "+storedFileColumns+", (") && strings.HasSuffix(sql, "FROM items WHERE root_id = $1 AND relative_path = $2"):
		return "lookup"
	case strings.Contains(sql, imageCatalogSnapshotProjection):
		return "images"
	default:
		return "other"
	}
}

func (control *scanQueryPlanControl) template(sql string) int {
	if index, ok := control.bySQL[sql]; ok {
		return index
	}
	if len(control.templates) == cap(control.templates) {
		control.problem = "SQL template capacity exceeded"
		return -1
	}
	index := len(control.templates)
	control.templates = append(control.templates, scanQueryPlanTemplate{sql: sql, family: scanQueryPlanFamily(sql)})
	control.bySQL[sql] = index
	return index
}

func (control *scanQueryPlanControl) append(record scanQueryPlanRecord) int {
	if len(control.records) == cap(control.records) {
		control.problem = "record capacity exceeded"
		return -1
	}
	index := len(control.records)
	control.records = append(control.records, record)
	return index
}

func (control *scanQueryPlanControl) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = control.delegate.TraceQueryStart(ctx, conn, data)
	if ignored, _ := ctx.Value(scanPerformanceObserverContextKey{}).(bool); ignored {
		return ctx
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if !control.active {
		return ctx
	}
	index := control.template(data.SQL)
	if index < 0 {
		return ctx
	}
	phase := &control.phases[len(control.phases)-1]
	phase.Queries++
	phase.counts[index]++
	family := control.templates[index].family
	if family != "lookup" && family != "media" && family != "bitmap" {
		return ctx
	}
	started := time.Now()
	record := scanQueryPlanRecord{Kind: "query", Family: family, template: index, Ordinal: phase.Queries,
		BackendPID: conn.PgConn().PID(), started: started, StartedUnixNS: started.UnixNano()}
	args := data.Args
	if family == "media" || family == "bitmap" {
		if len(args) == 0 {
			control.problem = "target query omitted its arguments"
			return ctx
		}
		mode, ok := args[0].(pgx.QueryExecMode)
		if !ok || (mode != pgx.QueryExecModeCacheStatement && mode != pgx.QueryExecModeCacheDescribe) {
			control.problem = "target query omitted its explicit local mode"
			return ctx
		}
		args = args[1:]
		record.Mode = "C"
		if mode == pgx.QueryExecModeCacheDescribe {
			record.Mode = "D"
		}
		if record.Mode != control.mode {
			control.problem = "target query mode differs from the selected phase"
		}
		key := scanQueryPlanUse{pid: record.BackendPID, family: family, mode: record.Mode}
		record.FirstObservedModeUse = !control.firstUse[key]
		if record.FirstObservedModeUse && len(control.firstUse) == 64 {
			control.problem = "PID/family/mode first-use capacity exceeded"
			return ctx
		}
		control.firstUse[key] = true
	}
	pathIndex := -1
	switch family {
	case "lookup":
		if len(args) == 2 {
			pathIndex = 1
		}
	case "media":
		if len(args) == 4 {
			pathIndex = 3
			record.itemKey, _ = args[0].(string)
		}
	case "bitmap":
		if len(args) == 1 {
			record.itemKey, _ = args[0].(string)
		}
	}
	if pathIndex >= 0 {
		record.path, _ = args[pathIndex].(string)
	}
	if (family != "bitmap" && record.path == "") || (family != "lookup" && record.itemKey == "") {
		control.problem = "target or traversal parameter shape differs"
	}
	return context.WithValue(ctx, scanQueryPlanQueryKey{}, control.append(record))
}

func (control *scanQueryPlanControl) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	control.finish(ctx, scanQueryPlanQueryKey{}, data.Err != nil, false)
	control.delegate.TraceQueryEnd(ctx, conn, data)
}

func (control *scanQueryPlanControl) TracePrepareStart(ctx context.Context, conn *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	if ignored, _ := ctx.Value(scanPerformanceObserverContextKey{}).(bool); ignored {
		return ctx
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if !control.active {
		return ctx
	}
	index := control.template(data.SQL)
	if index < 0 {
		return ctx
	}
	phase := &control.phases[len(control.phases)-1]
	phase.Prepares++
	started := time.Now()
	record := scanQueryPlanRecord{Kind: "prepare", Family: control.templates[index].family, template: index,
		BackendPID: conn.PgConn().PID(), NamedPrepare: data.Name != "", started: started, StartedUnixNS: started.UnixNano()}
	if parent, ok := ctx.Value(scanQueryPlanQueryKey{}).(int); ok && parent >= 0 {
		record.Ordinal, record.Mode = control.records[parent].Ordinal, control.records[parent].Mode
	}
	return context.WithValue(ctx, scanQueryPlanPrepareKey{}, control.append(record))
}

func (control *scanQueryPlanControl) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareEndData) {
	control.finish(ctx, scanQueryPlanPrepareKey{}, data.Err != nil, data.AlreadyPrepared)
}

func (control *scanQueryPlanControl) finish(ctx context.Context, key any, failed, alreadyPrepared bool) {
	index, ok := ctx.Value(key).(int)
	if !ok || index < 0 {
		return
	}
	ended := time.Now()
	control.mu.Lock()
	defer control.mu.Unlock()
	record := &control.records[index]
	record.EndedUnixNS, record.ElapsedNS = ended.UnixNano(), ended.Sub(record.started).Nanoseconds()
	record.Error, record.AlreadyPrepared = failed, alreadyPrepared
}

func (control *scanQueryPlanControl) report(t *testing.T) {
	t.Helper()
	// Digesting, comparison and serialization follow all measurements and Close.
	if len(control.phases) != 13 {
		t.Fatal("query-plan profile did not retain the original five phases and all eight controls")
	}
	var firstTemplates []scanQueryPlanTemplateCount
	firstOrder := ""
	var issues []string
	for phaseIndex := range control.phases {
		phase := &control.phases[phaseIndex]
		for index, count := range phase.counts {
			if count != 0 {
				template := control.templates[index]
				digest := fmt.Sprintf("%x", sha256.Sum256([]byte(template.sql)))
				if (template.family == "media" && digest != "6bf1992ebd49df413f0061c62e606ae2ffcdc57858ff2a8d68de7dbeb35c800a") ||
					(template.family == "bitmap" && digest != "2b0fe30f97645400db3f9a82e6d46ae2e4097f9ff072981a32cf68549f893219") {
					issues = append(issues, phase.Phase+" target SQL text differs from the selected production query")
				}
				phase.Templates = append(phase.Templates, scanQueryPlanTemplateCount{SHA256: digest, Family: template.family, Count: count})
			}
		}
		sort.Slice(phase.Templates, func(i, j int) bool { return phase.Templates[i].SHA256 < phase.Templates[j].SHA256 })
		phase.Records = control.records[phase.begin:phase.end]
		paths := make([]string, 0, 160)
		familyCounts := make(map[string]int)
		for index := range phase.Records {
			record := &phase.Records[index]
			record.TemplateSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(control.templates[record.template].sql)))
			if record.path != "" {
				record.PathSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(record.path)))
				record.MediaKind = filepath.Ext(record.path)
			}
			if record.itemKey != "" {
				record.ItemKeySHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(record.itemKey)))
			}
			if record.Kind == "query" {
				familyCounts[record.Family]++
				if record.Family == "lookup" {
					paths = append(paths, record.PathSHA256)
				}
			}
		}
		phase.PathOrderSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(paths, "\n"))))
		if phase.Control {
			if familyCounts["lookup"] != 160 || familyCounts["media"] != 160 || familyCounts["bitmap"] != 128 {
				issues = append(issues, fmt.Sprintf("%s changed traversal or target counts: %v", phase.Phase, familyCounts))
			}
			if firstTemplates == nil {
				firstTemplates, firstOrder = phase.Templates, phase.PathOrderSHA256
			} else if !reflect.DeepEqual(firstTemplates, phase.Templates) || firstOrder != phase.PathOrderSHA256 {
				issues = append(issues, phase.Phase+" changed application SQL multiset or path traversal order")
			}
			for _, record := range phase.Records {
				if record.Error {
					issues = append(issues, phase.Phase+" has a failed query or Prepare callback")
					break
				}
			}
		}
	}
	control.store.mu.Lock()
	closed := control.store.closed
	control.store.mu.Unlock()
	if !closed {
		issues = append(issues, "Store.Close did not complete")
	}
	encoded, err := json.Marshal(struct {
		Version          int                  `json:"version"`
		Order            [8]string            `json:"fixed_order"`
		DefaultQueryMode string               `json:"default_query_mode"`
		CacheCapacities  [2]int               `json:"statement_description_capacities"`
		JIT              string               `json:"jit"`
		RecordCapacity   int                  `json:"record_capacity"`
		StoreClosed      bool                 `json:"store_closed"`
		Scope            string               `json:"scope"`
		CacheScope       string               `json:"cache_scope"`
		Issues           []string             `json:"issues"`
		Phases           []scanQueryPlanPhase `json:"phases"`
	}{1, scanQueryPlanOrder, "cache_describe", [2]int{512, 512}, "off", scanQueryPlanRecordCapacity, closed,
		"one Store/schema/fixture; callbacks from scan request through worker retirement; job endpoints separate; post-removal pairs only",
		"original five phases use production C; both caches persist and D does not deallocate C plans; first observed PID/family/mode and Prepare retained; no clean-cold or retained-memory mode comparison",
		issues, control.phases})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("scan_query_plan_crossover=%s", encoded)
	if len(issues) != 0 {
		t.Fatalf("query-plan comparison qualification failed: %s", strings.Join(issues, "; "))
	}
}
