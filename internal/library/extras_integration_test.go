package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func extraTestResource(t *testing.T, ctx context.Context, store *Store, id, owner, libraryID, relative, kind string) {
	t.Helper()
	themeTestItem(t, ctx, store, id, "Video", owner, libraryID, relative)
	if _, err := store.pool.Exec(ctx, `INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		VALUES($1,$2,$3,true)`, id, owner, kind); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE items SET media='{"Container":"mp4","DurationTicks":120000000,
		"Streams":[{"Index":0,"CodecType":"video","Codec":"h264"},{"Index":1,"CodecType":"audio","Codec":"aac"}]}'
		WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
}

func extraTestFixture(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx, store := themeTestFixture(t)
	for _, resource := range []struct{ id, relative, kind string }{
		{"extra-zeta", "Seed/featurettes/Zeta Bonus.mp4", ExtraKindClip},
		{"extra-alpha", "Seed/featurettes/Alpha Bonus.mp4", ExtraKindClip},
		{"extra-middle", "Seed/deleted scenes/Middle Deleted Scene.mp4", ExtraKindDeletedScene},
		{"extra-trailer", "Seed/trailers/Delta Local Trailer.mp4", ExtraKindTrailer},
	} {
		extraTestResource(t, ctx, store, resource.id, "theme-seed", "library-b", resource.relative, resource.kind)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE items SET name=names.name,sort_name=names.name FROM (VALUES
		('extra-zeta','Zeta Bonus'),('extra-alpha','Alpha Bonus'),('extra-middle','Middle Deleted Scene'),
		('extra-trailer','Delta Local Trailer'),('theme-seed','Feature Movie')) names(id,name)
		WHERE items.id=names.id`); err != nil {
		t.Fatal(err)
	}
	return ctx, store
}

func extraTestState(t *testing.T, ctx context.Context, store *Store) string {
	t.Helper()
	var extra string
	if err := store.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_extra_resources r),
		'reserved',(SELECT jsonb_agg(to_jsonb(p) ORDER BY root_id,relative_path) FROM extra_reserved_paths p))::text`).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	return themeTestState(t, ctx, store) + extra
}

func TestStoreExtraQueriesSeparateCompletePopulationsAndDirectAttributes(t *testing.T) {
	ctx, store := extraTestFixture(t)
	subject := Subject{UserID: "restricted"}
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "extra-alpha", PlayCount: 3, IsFavorite: true})
	before := extraTestState(t, ctx, store)
	features, err := store.QuerySpecialFeatures(ctx, "theme-seed", subject)
	if err != nil || !reflect.DeepEqual(queryItemIDs(features), []string{"extra-alpha", "extra-middle", "extra-zeta"}) {
		t.Fatalf("complete ordered features differ: %v", err)
	}
	trailers, err := store.QueryLocalTrailers(ctx, "theme-seed", subject)
	if err != nil || !reflect.DeepEqual(queryItemIDs(trailers), []string{"extra-trailer"}) {
		t.Fatalf("separate trailer population differs: %v", err)
	}
	for _, item := range append(features, trailers...) {
		direct, err := store.GetItemFor(ctx, subject, item.ID)
		if err != nil || item.Type != "Video" || item.ParentID != "theme-seed" || item.ExtraOwnerName != "Feature Movie" ||
			item.ExtraKind == "" || item.ThemeKind != "" || !reflect.DeepEqual(item, direct) || item.Media == nil || !item.CanPlay {
			t.Fatalf("list/direct extra identity or source projection differs for %s: %v", item.ID, err)
		}
	}
	if features[0].UserData == nil || features[0].UserData.PlayCount != 3 || !features[0].UserData.IsFavorite {
		t.Fatal("extra list borrowed or lost the resource's own user state")
	}
	for _, owner := range []string{"theme-all", "theme-series", "theme-episode1"} {
		for _, query := range []func(context.Context, string, Subject) ([]Item, error){store.QuerySpecialFeatures, store.QueryLocalTrailers} {
			items, err := query(ctx, owner, subject)
			if err != nil || items == nil || len(items) != 0 {
				t.Fatalf("authorized empty existing owner %s was not an empty array: %v", owner, err)
			}
		}
	}
	ordinary, err := store.QueryItems(ctx, Query{UserID: "restricted", Recursive: true, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range ordinary.Items {
		if strings.HasPrefix(item.ID, "extra-") {
			t.Fatal("ordinary browse exposed an attached resource")
		}
	}
	if extraTestState(t, ctx, store) != before {
		t.Fatal("extra reads wrote items, relationships, credentials, metadata, or user state")
	}
}

func TestStoreExtraQueriesApplyCurrentUserAndApplicationAuthority(t *testing.T) {
	ctx, store := extraTestFixture(t)
	for _, test := range []struct {
		owner, user string
		want        error
	}{
		{"theme-hidden", "restricted", ErrNotFound},
		{"theme-seed", "none", ErrNotFound}, {"theme-seed", "disabled", ErrForbidden},
	} {
		for _, query := range []func(context.Context, string, Subject) ([]Item, error){store.QuerySpecialFeatures, store.QueryLocalTrailers} {
			if _, err := query(ctx, test.owner, Subject{UserID: test.user}); !errors.Is(err, test.want) {
				t.Fatalf("extra owner/subject authorization differs: %v", err)
			}
		}
	}
	key := seedCatalogApplicationKey(t, ctx, store.pool, "extra-key", true)
	features, err := store.QuerySpecialFeatures(ctx, "theme-seed", key)
	if err != nil || len(features) != 3 || features[0].UserData != nil {
		t.Fatalf("userless application key acquired history or lost authorized extras: %v", err)
	}
	key.UserID = "none"
	if _, err := store.QueryLocalTrailers(ctx, "theme-seed", key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("application target library scope was ignored: %v", err)
	}
	key.UserID = "restricted"
	if _, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QuerySpecialFeatures(ctx, "theme-seed", key); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked application authority retained extras: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["library-b"],"EnableMediaPlayback":false}'
		WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	features, err = store.QuerySpecialFeatures(ctx, "theme-seed", Subject{UserID: "restricted"})
	if err != nil || len(features) != 3 || features[0].CanPlay {
		t.Fatalf("current playback denial was bypassed by the extra source: %v", err)
	}
}

func TestStoreSpecialFeaturesMissingOwnerStillRequiresCurrentSubjectAuthority(t *testing.T) {
	ctx, store := extraTestFixture(t)
	missing, err := store.QuerySpecialFeatures(ctx, "missing", Subject{UserID: "restricted"})
	if err != nil || missing == nil || len(missing) != 0 {
		t.Fatalf("missing SpecialFeatures owner does not match the recorded empty array: %v", err)
	}
	if _, err := store.QuerySpecialFeatures(ctx, "missing", Subject{UserID: "disabled"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("absent owner bypassed the subject's current disablement: %v", err)
	}
	if _, err := store.QueryLocalTrailers(ctx, "missing", Subject{UserID: "restricted"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SpecialFeatures missing-owner observation was generalized to LocalTrailers: %v", err)
	}
	key := seedCatalogApplicationKey(t, ctx, store.pool, "missing-extra-key", true)
	if _, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QuerySpecialFeatures(ctx, "missing", key); !errors.Is(err, ErrForbidden) {
		t.Fatalf("absent owner bypassed application-key revocation: %v", err)
	}
}

func TestStoreExtraParentCountsUseOnlyValidActiveTrailersInTheAuthorizedSnapshot(t *testing.T) {
	ctx, store := extraTestFixture(t)
	owner, err := store.GetItem(ctx, "restricted", "theme-seed")
	if err != nil || owner.LocalTrailerCount == nil || *owner.LocalTrailerCount != 1 {
		t.Fatalf("positive Movie detail lost its real local trailer count: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE item_extra_resources SET active=false WHERE resource_item_id='extra-trailer'`); err != nil {
		t.Fatal(err)
	}
	owner, err = store.GetItem(ctx, "restricted", "theme-seed")
	if err != nil || owner.LocalTrailerCount != nil {
		t.Fatalf("retired trailer remained in the parent's positive count: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE item_extra_resources SET active=true WHERE resource_item_id='extra-trailer';
		UPDATE items SET parent_id='theme-all' WHERE id='extra-trailer'`); err != nil {
		t.Fatal(err)
	}
	owner, err = store.GetItem(ctx, "restricted", "theme-seed")
	if err != nil || owner.LocalTrailerCount != nil {
		t.Fatalf("invalid trailer association inflated the parent count: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE items SET parent_id='theme-seed' WHERE id='extra-trailer';
		UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, "restricted", "theme-seed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trailer-count projection bypassed owner ACL revocation: %v", err)
	}
}

func TestStoreExtraQueriesRetainInactiveIdentityAndRejectInvalidActivePopulation(t *testing.T) {
	ctx, store := extraTestFixture(t)
	subject := Subject{UserID: "restricted"}
	if _, err := store.pool.Exec(ctx, `UPDATE item_extra_resources SET active=false WHERE resource_item_id='extra-zeta'`); err != nil {
		t.Fatal(err)
	}
	features, err := store.QuerySpecialFeatures(ctx, "theme-seed", subject)
	if err != nil || len(features) != 2 {
		t.Fatalf("inactive resource was not retired independently: %v", err)
	}
	if _, err := store.GetItemFor(ctx, subject, "extra-zeta"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("inactive extra remained directly visible: %v", err)
	}
	for _, test := range []struct{ name, corrupt, repair string }{
		{"parent", `UPDATE items SET parent_id='theme-all' WHERE id='extra-alpha'`, `UPDATE items SET parent_id='theme-seed' WHERE id='extra-alpha'`},
		{"type", `UPDATE items SET type='Movie' WHERE id='extra-alpha'`, `UPDATE items SET type='Video' WHERE id='extra-alpha'`},
		{"library", `UPDATE items SET library_id='library-a' WHERE id='extra-alpha'`, `UPDATE items SET library_id='library-b' WHERE id='extra-alpha'`},
		{"root", `UPDATE items SET root_id='root-a' WHERE id='extra-alpha'`, `UPDATE items SET root_id='root-b' WHERE id='extra-alpha'`},
		{"owner type", `UPDATE items SET type='Series',is_folder=true WHERE id='theme-seed'`, `UPDATE items SET type='Movie',is_folder=false WHERE id='theme-seed'`},
		{"cross-role history", `INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES('extra-alpha','theme-seed','video',false)`,
			`DELETE FROM item_theme_resources WHERE resource_item_id='extra-alpha'`},
		{"reservation", `DELETE FROM extra_reserved_paths WHERE relative_path='Seed/featurettes/Alpha Bonus.mp4'`,
			`INSERT INTO extra_reserved_paths VALUES('root-b','Seed/featurettes/Alpha Bonus.mp4',false)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.pool.Exec(ctx, test.corrupt); err != nil {
				t.Fatal(err)
			}
			for _, query := range []func(context.Context, string, Subject) ([]Item, error){store.QuerySpecialFeatures, store.QueryLocalTrailers} {
				if items, err := query(ctx, "theme-seed", subject); !errors.Is(err, ErrUnavailable) || items != nil {
					t.Fatalf("invalid sibling produced a partial successful population: %v", err)
				}
			}
			if _, err := store.GetItemFor(ctx, subject, "extra-alpha"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("invalid extra remained directly available: %v", err)
			}
			if _, err := store.pool.Exec(ctx, test.repair); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoreExtraQueriesRejectCombinedOverflowWithoutEndpointBypass(t *testing.T) {
	ctx, store := extraTestFixture(t)
	if _, err := store.pool.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path,path)
		SELECT 'extra-bulk-'||n,'library-b','root-b','theme-seed','Bulk '||n,'Bulk '||n,'Video',false,
		'Seed/featurettes/bulk-'||n||'.mp4','/media/b/Seed/featurettes/bulk-'||n||'.mp4'
		FROM generate_series(1,$1::int) n`, MaxExtraResourcesPerOwner-4); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id LIKE 'extra-bulk-%';
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		SELECT id,'theme-seed','clip',true FROM items WHERE id LIKE 'extra-bulk-%'`); err != nil {
		t.Fatal(err)
	}
	features, err := store.QuerySpecialFeatures(ctx, "theme-seed", Subject{UserID: "restricted"})
	if err != nil || len(features) != MaxExtraResourcesPerOwner-1 {
		t.Fatalf("boundary population was silently truncated: %v", err)
	}
	extraTestResource(t, ctx, store, "extra-overflow", "theme-seed", "library-b", "Seed/trailers/Overflow.mp4", ExtraKindTrailer)
	for _, query := range []func(context.Context, string, Subject) ([]Item, error){store.QuerySpecialFeatures, store.QueryLocalTrailers} {
		if items, err := query(ctx, "theme-seed", Subject{UserID: "restricted"}); !errors.Is(err, ErrUnavailable) || items != nil {
			t.Fatalf("endpoint selection bypassed combined overflow: %v", err)
		}
	}
}

type extraSnapshotTraceKey struct{}

type extraSnapshotTracer struct {
	writer      *pgxpool.Pool
	afterPrefix string
	writeSQL    string
	once        sync.Once
	err         error
	begins      []string
}

func (trace *extraSnapshotTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(strings.ToLower(data.SQL), "begin") {
		trace.begins = append(trace.begins, strings.ToLower(data.SQL))
	}
	prefix := trace.afterPrefix
	if prefix == "" {
		prefix = "SELECT i.library_id, i.name FROM items i"
	}
	return context.WithValue(ctx, extraSnapshotTraceKey{}, strings.HasPrefix(data.SQL, prefix))
}

func (trace *extraSnapshotTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if selected, _ := ctx.Value(extraSnapshotTraceKey{}).(bool); selected {
		trace.once.Do(func() {
			statement := trace.writeSQL
			if statement == "" {
				statement = `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted';
				UPDATE item_extra_resources SET active=false WHERE resource_item_id='extra-alpha';
				UPDATE user_item_data SET play_count=99 WHERE user_id='restricted' AND item_id='extra-alpha'`
			}
			_, trace.err = trace.writer.Exec(ctx, statement)
		})
	}
}

func TestStoreExtraParentCountSharesTheOwnerAuthorizationSnapshot(t *testing.T) {
	ctx, store := extraTestFixture(t)
	trace := &extraSnapshotTracer{writer: store.pool, afterPrefix: "SELECT i.id, i.library_id, COALESCE",
		writeSQL: `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id='restricted';
		UPDATE item_extra_resources SET active=false WHERE resource_item_id='extra-trailer'`}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	owner, err := (&Store{pool: reader}).GetItem(ctx, "restricted", "theme-seed")
	if err != nil || trace.err != nil || owner.LocalTrailerCount == nil || *owner.LocalTrailerCount != 1 {
		t.Fatalf("trailer count escaped the owner's authorized snapshot: query=%v, writer=%v", err, trace.err)
	}
	if _, err := store.GetItem(ctx, "restricted", "theme-seed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the next parent read ignored committed policy revocation: %v", err)
	}
}

func TestStoreExtraQueriesAuthorizeOwnerResourcesAndUserDataInOneSnapshot(t *testing.T) {
	ctx, store := extraTestFixture(t)
	userDataSeed(t, ctx, store.pool, "restricted", UserData{ItemID: "extra-alpha", PlayCount: 3})
	trace := &extraSnapshotTracer{writer: store.pool}
	config := store.pool.Config()
	config.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	items, err := (&Store{pool: reader}).QuerySpecialFeatures(ctx, "theme-seed", Subject{UserID: "restricted"})
	if err != nil || trace.err != nil || len(trace.begins) != 1 || !strings.Contains(trace.begins[0], "repeatable read") ||
		!strings.Contains(trace.begins[0], "read only") || len(items) != 3 || items[0].UserData == nil || items[0].UserData.PlayCount != 3 {
		t.Fatalf("extra query escaped one read-only snapshot: query=%v, writer=%v, begins=%v", err, trace.err, trace.begins)
	}
	if _, err := store.QuerySpecialFeatures(ctx, "theme-seed", Subject{UserID: "restricted"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the next read failed to observe committed policy revocation: %v", err)
	}
}

func TestExtraQueriesRejectInvalidIdentifiersBeforeDatabaseAccess(t *testing.T) {
	for _, id := range []string{"", " seed", "seed ", "bad\x00seed", "bad\nseed", string([]byte{0xff}), strings.Repeat("x", 257)} {
		t.Run(fmt.Sprintf("%q", id), func(t *testing.T) {
			for _, query := range []func(context.Context, string, Subject) ([]Item, error){(&Store{}).QuerySpecialFeatures, (&Store{}).QueryLocalTrailers} {
				if _, err := query(context.Background(), id, Subject{UserID: "user"}); !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("invalid owner reached database access: %v", err)
				}
			}
		})
	}
}

func TestStoreExtraReadsRejectMissingControlStateInsteadOfGuessingAutomaticNames(t *testing.T) {
	ctx, store := extraTestFixture(t)
	if _, err := store.pool.Exec(ctx, "DELETE FROM item_metadata_state WHERE item_id='extra-trailer'"); err != nil {
		t.Fatal(err)
	}
	subject := Subject{UserID: "restricted"}
	if items, err := store.QueryLocalTrailers(ctx, "theme-seed", subject); !errors.Is(err, ErrUnavailable) || items != nil {
		t.Fatalf("list guessed a trailer name after losing metadata controls: %v", err)
	}
	if _, err := store.GetItemFor(ctx, subject, "extra-trailer"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("direct item guessed missing trailer control provenance: %v", err)
	}
	if _, err := store.QueryItems(ctx, Query{UserID: subject.UserID, Ids: []string{"extra-trailer"}}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("explicit IDs guessed missing trailer control provenance: %v", err)
	}
}
