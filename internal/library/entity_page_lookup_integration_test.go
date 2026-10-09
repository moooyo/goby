package library

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type entityPageLookupTrace struct {
	statement string
	arguments []any
	captures  int
}

func (trace *entityPageLookupTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "SELECT entity.id,entity.name,entity.kind,0,") {
		trace.statement, trace.arguments = data.SQL, append([]any(nil), data.Args...)
		trace.captures++
	}
	return ctx
}

func (*entityPageLookupTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type collageTargetLookupTx struct {
	pgx.Tx
	statement string
	arguments []any
}

func (query *collageTargetLookupTx) Query(ctx context.Context, statement string, arguments ...any) (pgx.Rows, error) {
	if strings.Contains(statement, "UNION ALL SELECT entity.id::text,'entity'") {
		query.statement, query.arguments = statement, append([]any(nil), arguments...)
	}
	return query.Tx.Query(ctx, statement, arguments...)
}

func assertEntityPrimaryKeyLookup(t *testing.T, ctx context.Context, tx pgx.Tx, statement string, arguments []any) {
	t.Helper()
	if statement == "" {
		t.Fatal("entity lookup statement was not observed")
	}
	// Disable sequential scans only for this structural observation: even a
	// tiny fixture must retain a primary-key condition, not a full index scan.
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan=off`); err != nil {
		t.Fatal(err)
	}
	var encoded []byte
	if err := tx.QueryRow(ctx, `EXPLAIN (FORMAT JSON, COSTS OFF) `+statement, arguments...).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	type planNode struct {
		Index     string     `json:"Index Name"`
		Condition string     `json:"Index Cond"`
		Plans     []planNode `json:"Plans"`
	}
	var documents []struct{ Plan planNode }
	if err := json.Unmarshal(encoded, &documents); err != nil || len(documents) != 1 {
		t.Fatalf("decode entity lookup plan: %v", err)
	}
	found := false
	var visit func(planNode)
	visit = func(node planNode) {
		if node.Index == "catalog_entities_pkey" && strings.Contains(node.Condition, "ANY") {
			found = true
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(documents[0].Plan)
	if !found {
		t.Fatalf("selected entity IDs have no primary-key lookup condition: %s", encoded)
	}
}

func TestSearchHintEntityPageRetainsOrderAndPrimaryKeyLookup(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	searchHintTestMetadata(t, ctx, fixture, "movie-b", `{"Genres":["Lookup Alpha","Lookup Zulu"],"People":[{"Name":"Lookup Person","Type":"Actor"}]}`)
	searchHintTestMetadata(t, ctx, fixture, "movie-a", `{"Genres":["Lookup Hidden"]}`)
	query := SearchHintsQuery{SearchTerm: "Lookup", Limit: 1000, Projection: QueryProjection{ImagesDisabled: true}}
	subject := Subject{UserID: "restricted"}
	complete := searchHintTestQuery(t, ctx, fixture, subject, query)
	if complete.TotalRecordCount != 3 || len(complete.SearchHints) != 3 {
		t.Fatalf("unexpected entity page fixture: %+v", complete)
	}
	trace := &entityPageLookupTrace{}
	config := fixture.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	query.StartIndex, query.Limit = 1, 2
	page := searchHintTestQuery(t, ctx, &Store{pool: reader}, subject, query)
	if page.TotalRecordCount != complete.TotalRecordCount || !reflect.DeepEqual(page.SearchHints, complete.SearchHints[1:3]) {
		t.Fatal("typed entity lookup changed authorized page order or projection")
	}
	if trace.captures != 1 {
		t.Fatalf("entity page performed %d projections, want one", trace.captures)
	}
	tx, _, err := fixture.beginSubjectRead(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	assertEntityPrimaryKeyLookup(t, ctx, tx, trace.statement, trace.arguments)
}

func TestCollageTargetLookupKeepsCanonicalMixedIDsAndVisibility(t *testing.T) {
	ctx, fixture := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, fixture.pool)
	searchHintTestExec(t, ctx, fixture, `INSERT INTO catalog_entities(kind,name)
		SELECT 'Genre','Unreferenced lookup genre ' || n FROM generate_series(1,1024) n`)
	searchHintTestMetadata(t, ctx, fixture, "movie-b", `{"Genres":["Canonical visible","Canonical collision"]}`)
	searchHintTestMetadata(t, ctx, fixture, "movie-a", `{"Genres":["Canonical hidden"]}`)
	visible := searchHintTestEntity(t, ctx, fixture, "Genre", "Canonical visible")
	collision := searchHintTestEntity(t, ctx, fixture, "Genre", "Canonical collision")
	hidden := searchHintTestEntity(t, ctx, fixture, "Genre", "Canonical hidden")
	searchHintTestExec(t, ctx, fixture, `ANALYZE catalog_entities`)
	// Even a hidden physical owner keeps the generic image fallback from
	// treating the same wire ID as an entity. Typed SearchHints is separate.
	searchHintTestItem(t, ctx, fixture, collision.ID, "Hidden physical collision", "Movie", "library-a", "library-a")
	ids := []string{"library-b", "movie-b", visible.ID, visible.ID, collision.ID, hidden.ID,
		"0" + visible.ID, "+" + visible.ID, " " + visible.ID, "0", "-1", "9223372036854775808", "opaque"}
	tx, access, err := fixture.beginSubjectRead(ctx, Subject{UserID: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	observed := &collageTargetLookupTx{Tx: tx}
	images := make(map[string][]Image)
	if err := mergeCollageImageListing(ctx, observed, access, ids, images); err != nil {
		t.Fatal(err)
	}
	if len(images[visible.ID]) != 1 || len(images["library-b"]) != 1 || len(images) != 2 {
		t.Fatalf("mixed target lookup changed canonical identity, collision precedence or visibility: %+v", images)
	}
	aliases := []string{"0" + visible.ID, "+" + visible.ID, " " + visible.ID, "0", "-1", "9223372036854775808", "opaque"}
	aliasImages := make(map[string][]Image)
	if err := mergeCollageImageListing(ctx, tx, access, aliases, aliasImages); err != nil {
		t.Fatal(err)
	}
	if len(aliasImages) != 0 {
		t.Fatalf("noncanonical IDs selected canonical entity artwork: %+v", aliasImages)
	}
	assertEntityPrimaryKeyLookup(t, ctx, tx, observed.statement, observed.arguments)
}
