package library

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

// Keep the published v1 formula independent of the optimized production SQL.
const legacyMediaOperationRevisionTestSQL = `('media-operation-source-v1-' || md5(jsonb_build_array(i.root_id,
	i.relative_path, i.file_identity, i.file_size, extract(epoch FROM i.modified_at), i.media,
	(SELECT ir.binding_revision FROM library_roots ir WHERE ir.id=i.root_id))::text))`

func mediaOperationRevisionFixture(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, store := libraryQueryTestStore(t)
	if _, err := store.pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('stamp-library','Stamp','movies');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('stamp-root-a','stamp-library','/stamp/a','/stamp','a'),('stamp-root-b','stamp-library','/stamp/b','/stamp','b');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,relative_path,file_identity,file_size,modified_at,media)
		VALUES('stamp-item','stamp-library','stamp-root-a','Stamp','stamp','Movie','Film.mkv','identity-1',123,
		'2026-01-02T03:04:05.123456Z','{"DurationTicks":600000000,"Container":"mkv"}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	return ctx, store.pool
}

func readMediaOperationRevisionTest(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, *string, *int64) {
	t.Helper()
	var current, legacy string
	var cached *string
	var binding *int64
	if err := pool.QueryRow(ctx, `SELECT `+MediaOperationSourceRevisionSQL+`,`+legacyMediaOperationRevisionTestSQL+`,
		i.media_operation_source_revision,i.media_operation_source_binding_revision FROM items i WHERE i.id='stamp-item'`).
		Scan(&current, &legacy, &cached, &binding); err != nil {
		t.Fatal(err)
	}
	if current != legacy {
		t.Fatal("cached source revision changed the published v1 stamp")
	}
	return current, cached, binding
}

func TestMediaOperationSourceRevisionCachePreservesV1ForEverySourceFact(t *testing.T) {
	ctx, pool := mediaOperationRevisionFixture(t)
	previous, cached, binding := readMediaOperationRevisionTest(t, ctx, pool)
	if cached == nil || *cached != previous || binding == nil || *binding != 1 {
		t.Fatal("new media source did not receive its exact v1 cache")
	}
	for _, assignment := range []string{
		`relative_path='Quoted '' Film.mkv'`, `file_identity='identity-2'`, `file_size=456`,
		`modified_at='2026-01-02T11:04:06.654321+08'`,
		`media='{"Container":"mkv","DurationTicks":600000001,"Nested":{"b":1.0,"a":"\\\""}}'::jsonb`,
		`root_id='stamp-root-b'`, `root_id=NULL`, `root_id='stamp-root-a'`,
		`modified_at=NULL`, `media=NULL`, `media='null'::jsonb`,
	} {
		if _, err := pool.Exec(ctx, "UPDATE items SET "+assignment+" WHERE id='stamp-item'"); err != nil {
			t.Fatal(err)
		}
		current, _, _ := readMediaOperationRevisionTest(t, ctx, pool)
		if current == previous && assignment != `media='null'::jsonb` {
			t.Fatalf("source mutation did not invalidate its stamp: %s", assignment)
		}
		if assignment == `media='null'::jsonb` && current != previous {
			t.Fatal("SQL NULL and JSON null no longer retain their identical v1 representation")
		}
		previous = current
	}
	// The INSERT and conflict UPDATE trigger paths must agree on NEW source facts.
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,name,sort_name,type,media)
		VALUES('stamp-item','stamp-library','stamp-root-b','Upsert','upsert','Movie','{"Container":"mp4"}')
		ON CONFLICT(id) DO UPDATE SET root_id=excluded.root_id,media=excluded.media`); err != nil {
		t.Fatal(err)
	}
	current, cached, binding := readMediaOperationRevisionTest(t, ctx, pool)
	if current == previous || cached == nil || current != *cached || binding == nil || *binding != 1 {
		t.Fatal("source upsert did not refresh its exact v1 cache")
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media_operation_source_revision=NULL WHERE id='stamp-item'`); err != nil {
		t.Fatal(err)
	}
	uncached, cached, _ := readMediaOperationRevisionTest(t, ctx, pool)
	if uncached != current || cached != nil {
		t.Fatal("an uncached row changed the v1 fallback")
	}
	if _, err := pool.CopyFrom(ctx, pgx.Identifier{"items"},
		[]string{"id", "library_id", "root_id", "name", "sort_name", "type", "relative_path", "media"},
		pgx.CopyFromRows([][]any{{"stamp-copied", "stamp-library", "stamp-root-a", "Copied", "copied", "Movie", "Copied.mkv", []byte(`{"Container":"mkv"}`)}})); err != nil {
		t.Fatal(err)
	}
	var copyValid bool
	if err := pool.QueryRow(ctx, `SELECT i.media_operation_source_revision IS NOT NULL
		AND i.media_operation_source_revision=`+legacyMediaOperationRevisionTestSQL+`
		FROM items i WHERE i.id='stamp-copied'`).Scan(&copyValid); err != nil || !copyValid {
		t.Fatalf("ordinary COPY skipped source cache maintenance: %v", err)
	}
}

func TestMediaOperationSourceRevisionCacheKeepsFreshAndRetainedRootBindings(t *testing.T) {
	ctx, pool := mediaOperationRevisionFixture(t)
	before, _, _ := readMediaOperationRevisionTest(t, ctx, pool)
	var itemVersion string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM items WHERE id='stamp-item'`).Scan(&itemVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id='stamp-root-a'`); err != nil {
		t.Fatal(err)
	}
	after, cached, binding := readMediaOperationRevisionTest(t, ctx, pool)
	var currentVersion string
	if err := pool.QueryRow(ctx, `SELECT xmin::text FROM items WHERE id='stamp-item'`).Scan(&currentVersion); err != nil {
		t.Fatal(err)
	}
	if after == before || cached == nil || *cached != before || binding == nil || *binding != 1 || itemVersion != currentVersion {
		t.Fatal("root binding change reused a stale stamp or rewrote its source item")
	}
	for _, selected := range []int64{1, 2, 3} {
		optimized := strings.ReplaceAll(MediaOperationSourceRevisionSQL, mediaOperationRootBindingRevisionSQL, "$1::bigint")
		legacy := strings.ReplaceAll(legacyMediaOperationRevisionTestSQL, mediaOperationRootBindingRevisionSQL, "$1::bigint")
		var actual, expected string
		if err := pool.QueryRow(ctx, "SELECT "+optimized+","+legacy+" FROM items i WHERE i.id='stamp-item'", selected).Scan(&actual, &expected); err != nil || actual != expected {
			t.Fatalf("retained binding %d changed its v1 stamp: %v", selected, err)
		}
	}
}

func TestMediaOperationSourceRevisionCacheDoesNotLockRootsAfterItems(t *testing.T) {
	ctx, pool := mediaOperationRevisionFixture(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id='stamp-root-a'`); err != nil {
		t.Fatal(err)
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := pool.Exec(writeCtx, `UPDATE items SET media=media||'{"DurationTicks":600000001}'::jsonb WHERE id='stamp-item'`); err != nil {
		t.Fatalf("source trigger waited for an unrelated root row lock: %v", err)
	}
	before, _, _ := readMediaOperationRevisionTest(t, ctx, pool)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	after, cached, _ := readMediaOperationRevisionTest(t, ctx, pool)
	if after == before || cached == nil || *cached != before {
		t.Fatal("a concurrent binding commit did not invalidate the captured cache")
	}
}

func TestMediaOperationSourceRevisionCacheLargeMediaCost(t *testing.T) {
	ctx, pool := mediaOperationRevisionFixture(t)
	index := media.VideoSeekIndex{Version: 1, Codec: "h264", DurationTicks: 8192 * media.TicksPerSecond,
		TimeBaseNumerator: 1, TimeBaseDenominator: 1000, SourceIdentity: strings.Repeat("1", 64),
		ToolIdentity: strings.Repeat("2", 64), ParameterSetsSHA256: strings.Repeat("3", 64),
		PacketSideDataChecked: true, NALScopeChecked: true, Width: 16, Height: 16, PixelFormat: "yuv420p", DecodedFrameBytes: 384,
		Entries: make([]media.VideoSeekPoint, media.MaxVideoSeekEntries)}
	for offset := range index.Entries {
		index.Entries[offset] = media.VideoSeekPoint{PTS: int64(offset) * 1000, DTS: int64(offset) * 1000,
			CodedSHA256: strings.Repeat("4", 64), DecodedSHA256: strings.Repeat("5", 64)}
	}
	payload, err := json.Marshal(media.Info{Container: "mkv", DurationTicks: index.DurationTicks, VideoSeekIndexes: []media.VideoSeekIndex{index}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE items SET media=$1 WHERE id='stamp-item'`, payload); err != nil {
		t.Fatal(err)
	}
	readMediaOperationRevisionTest(t, ctx, pool)
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, `SET jit=off`); err != nil {
		t.Fatal(err)
	}
	var legacyMS, cachedMS float64
	for range 3 {
		legacyMS += mediaOperationRevisionPlanTime(t, ctx, connection, legacyMediaOperationRevisionTestSQL)
		cachedMS += mediaOperationRevisionPlanTime(t, ctx, connection, MediaOperationSourceRevisionSQL)
	}
	updateMS := mediaOperationSQLPlanTime(t, ctx, connection, `UPDATE items SET media=media WHERE id='stamp-item'`)
	upsertMS := mediaOperationSQLPlanTime(t, ctx, connection, `INSERT INTO items
		(id,library_id,root_id,name,sort_name,type,relative_path,file_identity,file_size,modified_at,media)
		SELECT id,library_id,root_id,name,sort_name,type,relative_path,file_identity,file_size,modified_at,media
		FROM items WHERE id='stamp-item' ON CONFLICT(id) DO UPDATE SET media=excluded.media`)
	t.Logf("media bytes=%d reads per plan=128 samples=3 legacy_total_ms=%.3f cached_total_ms=%.3f update_ms=%.3f upsert_ms=%.3f",
		len(payload), legacyMS, cachedMS, updateMS, upsertMS)
}

func mediaOperationRevisionPlanTime(t *testing.T, ctx context.Context, connection *pgxpool.Conn, expression string) float64 {
	t.Helper()
	return mediaOperationSQLPlanTime(t, ctx, connection, `SELECT sum(length(`+expression+`))
		FROM items i CROSS JOIN generate_series(1,128) sample WHERE i.id='stamp-item'`)
}

func mediaOperationSQLPlanTime(t *testing.T, ctx context.Context, connection *pgxpool.Conn, statement string) float64 {
	t.Helper()
	var raw []byte
	if err := connection.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON) "+statement).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var document []struct {
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || len(document) != 1 {
		t.Fatalf("decode source stamp cost plan: %v", err)
	}
	return document[0].ExecutionTime
}
