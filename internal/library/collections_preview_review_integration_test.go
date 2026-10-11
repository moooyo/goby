package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type collectionPreviewOverlapObservation struct {
	statement  string
	collection string
	members    []string
	rows       int64
}

type collectionPreviewOverlapContextKey struct{}

type collectionPreviewReviewTrace struct {
	itemCountTracer
	overlaps []*collectionPreviewOverlapObservation
	failure  string
}

func (trace *collectionPreviewReviewTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.statements = append(trace.statements, data.SQL)
	overlap := strings.HasPrefix(data.SQL, "SELECT EXISTS (SELECT 1 FROM media_collection_entries WHERE collection_id=")
	if overlap {
		observation := &collectionPreviewOverlapObservation{statement: data.SQL}
		if len(data.Args) == 2 {
			observation.collection, _ = data.Args[0].(string)
			if members, ok := data.Args[1].([]string); ok {
				observation.members = append([]string{}, members...)
			}
		}
		trace.overlaps = append(trace.overlaps, observation)
		ctx = context.WithValue(ctx, collectionPreviewOverlapContextKey{}, observation)
	}
	if overlap && trace.failure == "overlap" || strings.EqualFold(strings.TrimSpace(data.SQL), "commit") && trace.failure == "commit" {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	return ctx
}

func (trace *collectionPreviewReviewTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	observation, _ := ctx.Value(collectionPreviewOverlapContextKey{}).(*collectionPreviewOverlapObservation)
	if observation == nil {
		return
	}
	trace.mu.Lock()
	defer trace.mu.Unlock()
	observation.rows = data.CommandTag.RowsAffected()
}

func (trace *collectionPreviewReviewTrace) resetPreview(failure string) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.statements = nil
	trace.overlaps = nil
	trace.failure = failure
}

func (trace *collectionPreviewReviewTrace) assertOverlap(t *testing.T, collection string, members []string) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if len(trace.overlaps) != 1 {
		t.Fatalf("preview used %d overlap queries, want one", len(trace.overlaps))
	}
	actual := trace.overlaps[0]
	if actual.statement != `SELECT EXISTS (SELECT 1 FROM media_collection_entries WHERE collection_id=$1 AND item_id=ANY($2::text[]))` ||
		actual.collection != collection || !reflect.DeepEqual(actual.members, members) || actual.rows != 1 {
		t.Fatalf("preview overlap observation = %+v; want collection=%q members=%v and one returned row", actual, collection, members)
	}
	for _, statement := range trace.statements {
		if strings.Contains(statement, "SELECT item_id FROM media_collection_entries WHERE collection_id=$1") {
			t.Fatal("preview loaded the complete existing member set")
		}
	}
}

func TestCollectionPreviewUsesOnlyResolvedOverlap(t *testing.T) {
	trace := &collectionPreviewReviewTrace{}
	ctx, pool, store, ownerID, _ := catalogBatchTestStoreWithTracer(t, trace)
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES ('collection-preview-empty-album','collection-source-a','Empty album','empty','MusicAlbum',true)`); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationUser(t, ctx, pool, "collection-preview-editor", false, false, []string{"collection-source-a"})
	libraryIntegrationUser(t, ctx, pool, "collection-preview-reader", false, false, []string{"collection-source-a"})
	owner := Subject{UserID: ownerID}
	editor := Subject{UserID: "collection-preview-editor"}
	reader := Subject{UserID: "collection-preview-reader"}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{
		Name: "Existing repeats", ItemIDs: []string{"collection-track-a", "collection-track-a", "collection-track-c"},
	})
	empty := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Empty preview target"})
	locked := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Locked preview target", IsLocked: true})
	shares := []CollectionShare{{UserID: editor.UserID, CanEdit: true}, {UserID: reader.UserID}}
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		subject    Subject
		collection CollectionInfo
		requested  []string
		unique     []string
		count      int
		duplicates bool
		wantErr    error
	}{
		{name: "empty target", subject: owner, collection: empty, requested: []string{"collection-track-a"}, unique: []string{"collection-track-a"}, count: 1},
		{name: "local duplicates", subject: owner, collection: empty, requested: []string{"collection-track-a", "collection-track-a"}, unique: []string{"collection-track-a"}, count: 2, duplicates: true},
		{name: "existing overlap", subject: owner, collection: playlist, requested: []string{"collection-track-a"}, unique: []string{"collection-track-a"}, count: 1, duplicates: true},
		{name: "unrelated existing repeats", subject: owner, collection: playlist, requested: []string{"collection-track-b"}, unique: []string{"collection-track-b"}, count: 1},
		{name: "folder overlap", subject: owner, collection: playlist, requested: []string{"collection-album"}, unique: []string{"collection-track-a", "collection-track-b"}, count: 2, duplicates: true},
		{name: "resolved duplicates", subject: owner, collection: empty, requested: []string{"collection-album", "collection-track-a"}, unique: []string{"collection-track-a", "collection-track-b"}, count: 3, duplicates: true},
		{name: "zero expanded members", subject: owner, collection: playlist, requested: []string{"collection-preview-empty-album"}, unique: []string{}},
		{name: "shared editor overlap", subject: editor, collection: playlist, requested: []string{"collection-track-a"}, unique: []string{"collection-track-a"}, count: 1, duplicates: true},
		{name: "hidden proposal", subject: editor, collection: playlist, requested: []string{"collection-track-c"}, wantErr: ErrNotFound},
		{name: "reader cannot preview edits", subject: reader, collection: playlist, requested: []string{"collection-track-a"}, wantErr: ErrForbidden},
		{name: "locked collection", subject: owner, collection: locked, requested: []string{"collection-track-a", "collection-track-a"}, wantErr: ErrForbidden},
		{name: "missing after duplicates", subject: owner, collection: empty, requested: []string{"collection-track-a", "collection-track-a", "missing-preview-member"}, wantErr: ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.resetPreview("")
			result, err := store.PreviewCollectionItems(ctx, test.subject, test.collection.ID, test.collection.Kind, test.requested)
			if !errors.Is(err, test.wantErr) || result.ItemCount != test.count || result.ContainsDuplicates != test.duplicates {
				t.Fatalf("preview = %+v, %v; want count=%d duplicates=%t error=%v", result, err, test.count, test.duplicates, test.wantErr)
			}
			if test.wantErr != nil {
				if catalogBatchStatementCount(&trace.itemCountTracer, "SELECT EXISTS (SELECT 1 FROM media_collection_entries WHERE collection_id=") != 0 || catalogBatchStatementCount(&trace.itemCountTracer, "commit") != 0 {
					t.Fatal("rejected preview reached overlap or transaction commit")
				}
				return
			}
			trace.assertOverlap(t, test.collection.ID, test.unique)
			if catalogBatchStatementCount(&trace.itemCountTracer, "commit") != 1 {
				t.Fatal("successful preview did not commit its read transaction")
			}
		})
	}
	// The helper observes raw membership; the public entry above independently
	// rejects hidden proposals before it reaches this observation.
	tx, _, err := store.beginSubjectRead(ctx, editor)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	trace.resetPreview("")
	overlap, err := collectionMembersOverlap(ctx, tx, playlist.ID, []string{"collection-track-c"})
	if err != nil || !overlap {
		t.Fatalf("raw overlap lost a hidden existing member: %t, %v", overlap, err)
	}
	trace.assertOverlap(t, playlist.ID, []string{"collection-track-c"})
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var entries int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM media_collection_entries WHERE collection_id=$1`, playlist.ID).Scan(&entries); err != nil || entries != 3 {
		t.Fatalf("preview changed durable playlist entries: count=%d error=%v", entries, err)
	}
}

func TestCollectionPreviewKeepsExpandedLimitAndNarrowParameters(t *testing.T) {
	trace := &collectionPreviewReviewTrace{}
	ctx, pool, store, ownerID, _ := catalogBatchTestStoreWithTracer(t, trace)
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES ('collection-preview-large-album','collection-source-a','Large album','large','MusicAlbum',true);
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
		SELECT 'collection-preview-expanded-'||n::text,'collection-source-a','collection-preview-large-album',n::text,lpad(n::text,5,'0'),'Audio'
		FROM generate_series(1,10000)n`); err != nil {
		t.Fatal(err)
	}
	owner := Subject{UserID: ownerID}
	small := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Small existing playlist", ItemIDs: []string{"collection-track-a"}})
	large := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Full existing playlist", ItemIDs: []string{"collection-preview-large-album"}})
	want := make([]string, collectionEntryLimit)
	for index := range want {
		want[index] = fmt.Sprintf("collection-preview-expanded-%d", index+1)
	}
	trace.resetPreview("")
	result, err := store.PreviewCollectionItems(ctx, owner, small.ID, PlaylistKind, []string{"collection-preview-large-album"})
	if err != nil || result.ItemCount != collectionEntryLimit || result.ContainsDuplicates {
		t.Fatalf("legal ten-thousand-member expansion was restricted by the input limit: %+v, %v", result, err)
	}
	trace.assertOverlap(t, small.ID, want)
	trace.resetPreview("")
	result, err = store.PreviewCollectionItems(ctx, owner, large.ID, PlaylistKind, []string{want[5000]})
	if err != nil || result.ItemCount != 1 || !result.ContainsDuplicates {
		t.Fatalf("small proposal into full playlist lost overlap: %+v, %v", result, err)
	}
	trace.assertOverlap(t, large.ID, []string{want[5000]})
	trace.resetPreview("")
	if _, err := store.PreviewCollectionItems(ctx, owner, small.ID, PlaylistKind, want[:collectionInputLimit+1]); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("preview accepted too many original IDs: %v", err)
	}
	if catalogBatchStatementCount(&trace.itemCountTracer, "") != 0 {
		t.Fatal("oversized original input reached database admission")
	}
	trace.resetPreview("")
	if _, err := store.PreviewCollectionItems(ctx, owner, small.ID, PlaylistKind, []string{"collection-preview-large-album", "collection-track-a"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("preview accepted an oversized expansion: %v", err)
	}
	if catalogBatchStatementCount(&trace.itemCountTracer, "SELECT EXISTS (SELECT 1 FROM media_collection_entries WHERE collection_id=") != 0 {
		t.Fatal("oversized expansion reached the overlap query")
	}
}

func TestCollectionPreviewRetainsOverlapAndCommitErrors(t *testing.T) {
	trace := &collectionPreviewReviewTrace{}
	ctx, pool, store, ownerID, _ := catalogBatchTestStoreWithTracer(t, trace)
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES ('collection-preview-error-album','collection-source-a','Empty album','empty','MusicAlbum',true)`); err != nil {
		t.Fatal(err)
	}
	owner := Subject{UserID: ownerID}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Preview error target"})
	for _, failure := range []string{"overlap", "commit"} {
		for _, proposal := range []struct {
			name string
			ids  []string
		}{
			{name: "ordinary", ids: []string{"collection-track-a"}},
			{name: "known duplicates", ids: []string{"collection-track-a", "collection-track-a"}},
			{name: "empty expansion", ids: []string{"collection-preview-error-album"}},
		} {
			t.Run(failure+"/"+proposal.name, func(t *testing.T) {
				trace.resetPreview(failure)
				defer trace.resetPreview("")
				result, err := store.PreviewCollectionItems(ctx, owner, playlist.ID, PlaylistKind, proposal.ids)
				if !errors.Is(err, context.Canceled) || result != (CollectionPreview{}) {
					t.Fatalf("preview masked its %s error: result=%+v error=%v", failure, result, err)
				}
				if count := catalogBatchStatementCount(&trace.itemCountTracer, "SELECT EXISTS (SELECT 1 FROM media_collection_entries WHERE collection_id="); count != 1 {
					t.Fatalf("preview bypassed overlap after %s resolution: queries=%d", proposal.name, count)
				}
				commits := catalogBatchStatementCount(&trace.itemCountTracer, "commit")
				if failure == "overlap" && commits != 0 || failure == "commit" && commits != 1 {
					t.Fatalf("preview changed commit ordering after %s failure: commits=%d", failure, commits)
				}
			})
		}
	}
}
