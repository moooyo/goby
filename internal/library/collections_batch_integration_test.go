package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func catalogBatchTestStore(t *testing.T) (context.Context, *pgxpool.Pool, *Store, string, *itemCountTracer) {
	t.Helper()
	return catalogBatchTestStoreWithTracer(t, nil)
}

func catalogBatchTestStoreWithTracer(t *testing.T, queryTrace pgx.QueryTracer) (context.Context, *pgxpool.Pool, *Store, string, *itemCountTracer) {
	t.Helper()
	ctx, pool, original, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := original.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &itemCountTracer{}
	if queryTrace == nil {
		queryTrace = trace
	}
	config := pool.Config()
	config.ConnConfig.Tracer = queryTrace
	tracedPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, tracedPool)
	store, err := New(tracedPool, &libraryFixtureProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanupCtx); err != nil {
			t.Errorf("close traced catalog store: %v", err)
		}
	})
	return ctx, pool, store, userID, trace
}

func catalogBatchStatementCount(trace *itemCountTracer, fragment string) int {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	count := 0
	for _, statement := range trace.statements {
		if strings.Contains(statement, fragment) {
			count++
		}
	}
	return count
}

func TestCollectionMemberBatchPreservesOrderDuplicatesAndOrdinaryAccess(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	libraryIntegrationUser(t, ctx, pool, "collection-batch-reader", false, false, []string{"collection-source-a"})
	if _, err := pool.Exec(ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path)
		VALUES ('collection-batch-root','collection-source-a','/collection-batch','/collection-batch','');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,relative_path) VALUES
		('collection-batch-movie','collection-source-a','collection-batch-root',NULL,'Movie','movie','Movie','Film/Main.mp4'),
		('collection-batch-extra','collection-source-a','collection-batch-root','collection-batch-movie','Extra','extra','Video','Film/featurettes/Extra.mp4');
		INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory)
		VALUES ('collection-batch-root','Film/featurettes',true);
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active)
		VALUES ('collection-batch-extra','collection-batch-movie','clip',true)`); err != nil {
		t.Fatal(err)
	}
	owner := Subject{UserID: ownerID}
	source := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{
		Name: "Source order", MediaType: "Audio", ItemIDs: []string{"collection-track-b", "collection-track-a", "collection-track-a"},
	})
	box := collectionUserDataCreate(t, ctx, store, owner, BoxSetKind, CollectionInput{Name: "Source box"})
	target := CollectionInfo{ID: "collection-batch-target", Kind: PlaylistKind, MediaType: "Audio"}
	for _, test := range []struct {
		name      string
		userID    string
		requested []string
		want      []string
		wantErr   error
		queries   int
	}{
		{name: "folder and playlist order", userID: ownerID,
			requested: []string{"collection-track-b", "collection-album", "collection-track-b", source.ID},
			want:      []string{"collection-track-b", "collection-track-a", "collection-track-b", "collection-track-b", "collection-track-b", "collection-track-a", "collection-track-a"}, queries: 3},
		{name: "missing before self", userID: ownerID, requested: []string{"missing-member", target.ID}, wantErr: ErrNotFound, queries: 1},
		{name: "self before missing", userID: ownerID, requested: []string{target.ID, "missing-member"}, wantErr: ErrInvalidInput, queries: 1},
		{name: "incompatible before missing", userID: ownerID, requested: []string{"collection-batch-movie", "missing-member"}, wantErr: ErrInvalidInput, queries: 1},
		{name: "missing before incompatible", userID: ownerID, requested: []string{"missing-member", "collection-batch-movie"}, wantErr: ErrNotFound, queries: 1},
		{name: "box before missing", userID: ownerID, requested: []string{box.ID, "missing-member"}, wantErr: ErrInvalidInput, queries: 1},
		{name: "hidden before incompatible", userID: "collection-batch-reader", requested: []string{"collection-track-c", "collection-batch-movie"}, wantErr: ErrNotFound, queries: 1},
		{name: "auxiliary is not ordinary", userID: ownerID, requested: []string{"collection-batch-extra"}, wantErr: ErrNotFound, queries: 1},
		{name: "empty request", userID: ownerID, requested: []string{}, want: []string{}, queries: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, access, err := store.beginSubjectRead(ctx, Subject{UserID: test.userID})
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			observed := &collectionProjectionTx{Tx: tx}
			actual, err := resolveCollectionMembers(ctx, observed, access, target, test.requested)
			if !errors.Is(err, test.wantErr) || !reflect.DeepEqual(actual, test.want) {
				t.Fatalf("member resolution = %v, %v; want %v, %v", actual, err, test.want, test.wantErr)
			}
			if observed.queries != test.queries {
				t.Fatalf("member resolution used %d queries, want %d", observed.queries, test.queries)
			}
		})
	}
	for _, requested := range [][]string{{source.ID}, {box.ID}} {
		if _, err := store.AddCollectionItems(ctx, owner, box.ID, BoxSetKind, requested); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("BoxSet accepted a nested collection: %v", err)
		}
	}
	if added, err := store.AddCollectionItems(ctx, owner, box.ID, BoxSetKind, []string{"collection-album", "collection-album"}); err != nil || added != 1 {
		t.Fatalf("BoxSet lost ordinary folders or duplicate suppression: added=%d error=%v", added, err)
	}
}

func TestCollectionMemberBatchKeepsCumulativeFolderLimitErrorOrder(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type,is_folder)
		VALUES ('collection-large-album','collection-source-a','Large album','large','MusicAlbum',true);
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
		SELECT 'collection-large-'||n::text,'collection-source-a','collection-large-album',n::text,lpad(n::text,5,'0'),'Audio'
		FROM generate_series(1,10000)n`); err != nil {
		t.Fatal(err)
	}
	target := CollectionInfo{ID: "collection-limit-target", Kind: PlaylistKind, MediaType: "Audio"}
	for _, test := range []struct {
		name      string
		requested []string
		wantErr   error
		limit     bool
	}{
		{name: "folder overflow before missing", requested: []string{"collection-track-a", "collection-large-album", "missing-member"}, wantErr: ErrInvalidInput, limit: true},
		{name: "missing before folder overflow", requested: []string{"missing-member", "collection-track-a", "collection-large-album"}, wantErr: ErrNotFound},
		{name: "full folder then missing", requested: []string{"collection-large-album", "missing-member"}, wantErr: ErrNotFound},
		{name: "explicit overflow still checks next identity", requested: []string{"collection-large-album", "collection-track-a", "missing-member"}, wantErr: ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, access, err := store.beginSubjectRead(ctx, Subject{UserID: ownerID})
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			_, err = resolveCollectionMembers(ctx, tx, access, target, test.requested)
			if !errors.Is(err, test.wantErr) || test.limit && !strings.Contains(err.Error(), "collection entry limit exceeded") {
				t.Fatalf("cumulative member resolution = %v; want %v (limit=%t)", err, test.wantErr, test.limit)
			}
		})
	}
}

func TestCollectionMemberAndSharingBatchesHandleThousandInputs(t *testing.T) {
	ctx, pool, store, ownerID, trace := catalogBatchTestStore(t)
	collectionTestMedia(t, ctx, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO items(id,library_id,name,sort_name,type)
		SELECT 'collection-batch-'||lpad(n::text,4,'0'),'collection-source-a',n::text,lpad(n::text,4,'0'),'Audio'
		FROM generate_series(0,999)n;
		INSERT INTO users(id,name,normalized_name,password_hash,has_password,is_disabled,policy)
		SELECT 'collection-recipient-'||lpad(n::text,4,'0'),'Recipient '||n::text,'recipient '||n::text,'',false,false,'{"EnableAllFolders":true}'::jsonb
		FROM generate_series(0,999)n`); err != nil {
		t.Fatal(err)
	}
	ids, shares, expectedShares := make([]string, 1000), make([]CollectionShare, 1000), make([]CollectionShare, 1000)
	for index := range ids {
		ids[index] = fmt.Sprintf("collection-batch-%04d", 999-index)
		share := CollectionShare{UserID: fmt.Sprintf("collection-recipient-%04d", index), CanEdit: index%2 == 0}
		shares[999-index], expectedShares[index] = share, share
	}
	owner := Subject{UserID: ownerID}
	trace.reset()
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Thousand inputs", ItemIDs: ids})
	if count := catalogBatchStatementCount(trace, "SELECT i.id,i.type,i.is_folder FROM items"); count != 1 {
		t.Fatalf("create resolved its input facts in %d queries, want one", count)
	}
	entries, err := store.CollectionItems(ctx, owner, playlist.ID, PlaylistKind, 0, 1000)
	if err != nil || !reflect.DeepEqual(queryItemIDs(entries.Items), ids) {
		t.Fatalf("create changed the thousand-item request order: count=%d error=%v", len(entries.Items), err)
	}
	trace.reset()
	preview, err := store.PreviewCollectionItems(ctx, owner, playlist.ID, PlaylistKind, ids)
	if err != nil || preview.ItemCount != 1000 || !preview.ContainsDuplicates || catalogBatchStatementCount(trace, "SELECT i.id,i.type,i.is_folder FROM items") != 1 {
		t.Fatalf("thousand-item preview lost its batch or duplicates: %+v, %v", preview, err)
	}
	trace.reset()
	if added, err := store.AddCollectionItems(ctx, owner, playlist.ID, PlaylistKind, ids); err != nil || added != 1000 || catalogBatchStatementCount(trace, "SELECT i.id,i.type,i.is_folder FROM items") != 1 {
		t.Fatalf("thousand-item append lost its batch or duplicate entries: added=%d error=%v", added, err)
	}
	trace.reset()
	updated, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares})
	if err != nil || !reflect.DeepEqual(updated.Shares, expectedShares) {
		t.Fatalf("batched sharing changed normalized recipients or edit flags: count=%d error=%v", len(updated.Shares), err)
	}
	if count := catalogBatchStatementCount(trace, "AND NOT is_disabled"); count != 1 {
		t.Fatalf("sharing validated recipients in %d queries, want one", count)
	}
	if count := catalogBatchStatementCount(trace, "INSERT INTO media_collection_shares"); count != 1 {
		t.Fatalf("sharing inserted recipients in %d statements, want one", count)
	}
	if shares[0] != expectedShares[999] {
		t.Fatal("sharing normalized the caller's input slice in place")
	}
}

func TestCollectionSharingBatchKeepsNormalizedFirstErrorAndRollback(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	libraryIntegrationUser(t, ctx, pool, "collection-kept-recipient", false, true, nil)
	libraryIntegrationUser(t, ctx, pool, "aa-disabled-recipient", true, true, nil)
	libraryIntegrationUser(t, ctx, pool, "zz-disabled-recipient", true, true, nil)
	owner := Subject{UserID: ownerID}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Retained name"})
	retained := []CollectionShare{{UserID: "collection-kept-recipient", CanEdit: true}}
	if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &retained}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		recipient string
		wantErr   error
	}{
		{name: "missing before owner", recipient: "aa-missing-recipient", wantErr: ErrNotFound},
		{name: "owner before missing", recipient: "zz-missing-recipient", wantErr: ErrInvalidInput},
		{name: "disabled before owner", recipient: "aa-disabled-recipient", wantErr: ErrNotFound},
		{name: "owner before disabled", recipient: "zz-disabled-recipient", wantErr: ErrInvalidInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			shares := []CollectionShare{{UserID: ownerID}, {UserID: test.recipient}}
			name := "Uncommitted name"
			if _, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Name: &name, Shares: &shares}); !errors.Is(err, test.wantErr) {
				t.Fatalf("normalized recipient validation = %v; want %v", err, test.wantErr)
			}
			current, err := store.GetCollection(ctx, owner, playlist.ID, PlaylistKind)
			if err != nil || current.Name != playlist.Name || !reflect.DeepEqual(current.Shares, retained) {
				t.Fatalf("rejected sharing batch partially committed: %+v, %v", current, err)
			}
		})
	}
	shares := []CollectionShare{}
	current, err := store.UpdateCollection(ctx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares})
	if err != nil || len(current.Shares) != 0 {
		t.Fatalf("empty sharing batch failed to clear recipients: %+v, %v", current, err)
	}
}

func TestCollectionSharingBatchObservesRecipientDeletionWhileWaiting(t *testing.T) {
	ctx, pool, store, _, ownerID := libraryIntegrationStore(t, &libraryFixtureProber{})
	const recipientID = "collection-deleted-recipient"
	libraryIntegrationUser(t, ctx, pool, recipientID, false, true, nil)
	owner := Subject{UserID: ownerID}
	playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "Deletion race"})
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(holder)
	if _, err := holder.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, recipientID); err != nil {
		t.Fatal(err)
	}
	operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		shares := []CollectionShare{{UserID: recipientID, CanEdit: true}}
		_, err := store.UpdateCollection(operationCtx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares})
		finished <- err
	}()
	waitCatalogApplicationBlock(t, ctx, pool, holder.Conn().PgConn().PID())
	if _, err := holder.Exec(ctx, `DELETE FROM users WHERE id=$1`, recipientID); err != nil {
		t.Fatal(err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("sharing survived recipient deletion at its account lock: %v", err)
		}
	case <-operationCtx.Done():
		t.Fatal("sharing did not retire after recipient deletion")
	}
	current, err := store.GetCollection(ctx, owner, playlist.ID, PlaylistKind)
	if err != nil || len(current.Shares) != 0 {
		t.Fatalf("recipient deletion left a partial sharing batch: %+v, %v", current, err)
	}
}

type collectionRecipientLockQueryKey struct{}

type collectionRecipientLockTrace struct {
	armed            atomic.Bool
	admitted         chan error
	validated        chan error
	resumeAdmission  chan struct{}
	resumeValidation chan struct{}
}

func (trace *collectionRecipientLockTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.armed.Load() {
		return ctx
	}
	switch {
	case data.SQL == `SELECT id FROM users WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`:
		return context.WithValue(ctx, collectionRecipientLockQueryKey{}, "admission")
	case strings.HasPrefix(data.SQL, `SELECT id FROM users WHERE id=ANY($1::text[]) AND NOT is_disabled`):
		return context.WithValue(ctx, collectionRecipientLockQueryKey{}, "validation")
	default:
		return ctx
	}
}

func (trace *collectionRecipientLockTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var reached chan error
	var resume chan struct{}
	switch ctx.Value(collectionRecipientLockQueryKey{}) {
	case "admission":
		reached, resume = trace.admitted, trace.resumeAdmission
	case "validation":
		reached, resume = trace.validated, trace.resumeValidation
	default:
		return
	}
	select {
	case reached <- data.Err:
	case <-ctx.Done():
		return
	}
	select {
	case <-resume:
	case <-ctx.Done():
	}
}

func TestCollectionSharingBatchLocksRecipientsCreatedAfterAdmission(t *testing.T) {
	for _, mutation := range []string{"disable", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			trace := &collectionRecipientLockTrace{
				admitted: make(chan error, 1), validated: make(chan error, 1),
				resumeAdmission: make(chan struct{}, 1), resumeValidation: make(chan struct{}, 1),
			}
			ctx, pool, store, ownerID, _ := catalogBatchTestStoreWithTracer(t, trace)
			defer close(trace.resumeAdmission)
			defer close(trace.resumeValidation)
			owner := Subject{UserID: ownerID}
			playlist := collectionUserDataCreate(t, ctx, store, owner, PlaylistKind, CollectionInput{Name: "New recipient lock"})
			operationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			const recipientID = "collection-late-recipient"
			shares := []CollectionShare{{UserID: recipientID, CanEdit: true}}
			type updateResult struct {
				collection CollectionInfo
				err        error
			}
			updated := make(chan updateResult, 1)
			trace.armed.Store(true)
			go func() {
				collection, err := store.UpdateCollection(operationCtx, owner, playlist.ID, PlaylistKind, CollectionPatch{Shares: &shares})
				updated <- updateResult{collection: collection, err: err}
			}()
			select {
			case err := <-trace.admitted:
				if err != nil {
					t.Fatalf("initial account lock query failed: %v", err)
				}
			case <-operationCtx.Done():
				t.Fatal("sharing did not complete its initial account lock query")
			}
			// The initial query has exhausted its snapshot without this account.
			// Recipient validation must lock the newly committed row itself.
			libraryIntegrationUser(t, ctx, pool, recipientID, false, true, nil)
			trace.resumeAdmission <- struct{}{}
			select {
			case err := <-trace.validated:
				if err != nil {
					t.Fatalf("new recipient validation failed: %v", err)
				}
			case <-operationCtx.Done():
				t.Fatal("sharing did not validate the newly created recipient")
			}
			connection, err := pool.Acquire(operationCtx)
			if err != nil {
				t.Fatal(err)
			}
			mutationPID := int32(connection.Conn().PgConn().PID())
			statement := `UPDATE users SET is_disabled=true WHERE id=$1`
			if mutation == "delete" {
				statement = `DELETE FROM users WHERE id=$1`
			}
			mutated := make(chan error, 1)
			go func() {
				defer connection.Release()
				_, err := connection.Exec(operationCtx, statement, recipientID)
				mutated <- err
			}()
			ownedTransactionsWaitForBlock(t, operationCtx, pool, mutationPID, int32(store.ownership.conn.Conn().PgConn().PID()), mutated)
			trace.resumeValidation <- struct{}{}
			select {
			case result := <-updated:
				if result.err != nil || !reflect.DeepEqual(result.collection.Shares, shares) {
					t.Fatalf("protected sharing did not commit before recipient mutation: shares=%+v error=%v", result.collection.Shares, result.err)
				}
			case <-operationCtx.Done():
				t.Fatal("sharing did not retire after releasing recipient validation")
			}
			trace.armed.Store(false)
			select {
			case err := <-mutated:
				if err != nil {
					t.Fatalf("recipient mutation did not continue after sharing committed: %v", err)
				}
			case <-operationCtx.Done():
				t.Fatal("recipient mutation remained blocked after sharing committed")
			}
			var exists, disabled, shared bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1),
				EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_disabled),
				EXISTS(SELECT 1 FROM media_collection_shares WHERE collection_id=$2 AND user_id=$1)`, recipientID, playlist.ID).Scan(&exists, &disabled, &shared); err != nil {
				t.Fatal(err)
			}
			wantPresent := mutation == "disable"
			if exists != wantPresent || disabled != wantPresent || shared != wantPresent {
				t.Fatalf("recipient mutation lost its post-commit effect: exists=%t disabled=%t shared=%t", exists, disabled, shared)
			}
		})
	}
}
