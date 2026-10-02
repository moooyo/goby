//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scanStagedPageMembershipTrace struct {
	scanShortProofSQLTracer
	pages, finalPages, membership, sealed int
}

func (trace *scanStagedPageMembershipTrace) Reset() {
	trace.scanShortProofSQLTracer.Reset()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.pages, trace.finalPages, trace.membership, trace.sealed = 0, 0, 0, 0
}

func (trace *scanStagedPageMembershipTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.scanShortProofSQLTracer.TraceQueryStart(ctx, conn, data)
	if conn.PgConn().PID() != trace.ownerPID.Load() {
		return ctx
	}
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if strings.Contains(statement, "and not exists (select 1 from pg_temp.goby_scan_reconciliation_seen seen") &&
		strings.Contains(statement, "for update of i") {
		trace.pages++
		if trace.ordinal == 2 {
			trace.finalPages++
		}
	}
	if strings.HasPrefix(statement, "select item_id from pg_temp.goby_scan_reconciliation_seen ") {
		trace.membership++
	}
	if strings.HasPrefix(statement, "select sealed,seen_rows,serialized_bytes,") {
		trace.sealed++
	}
	return ctx
}

func (trace *scanStagedPageMembershipTrace) counts() (pages, finalPages, membership, sealed int) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.pages, trace.finalPages, trace.membership, trace.sealed
}

// Reopen the same owned schema with a tracer before creating its Running scan.
// Only the reserved backend is counted; setup and independent pool work cannot
// masquerade as a removed catalog-owner membership query.
func scanStagedPageMembershipFixture(t *testing.T) (rootBindingScanFixture, *scanStagedPageMembershipTrace) {
	t.Helper()
	read := newRootBindingReadFixture(t)
	read.bind(t, 13)
	if err := read.store.Close(read.ctx); err != nil {
		t.Fatalf("retire initial staged-page owner: %v", err)
	}
	trace := &scanStagedPageMembershipTrace{}
	configuration := read.pool.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(read.ctx, configuration)
	if err != nil {
		t.Fatalf("create traced staged-page pool: %v", err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	store, err := New(pool, &libraryFixtureProber{}, []string{read.root.AllowedPath})
	if err != nil {
		t.Fatalf("acquire traced staged-page owner: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(ctx); err != nil {
			t.Errorf("retire traced staged-page owner: %v", err)
		}
	})
	store.ownership.mu.Lock()
	trace.ownerPID.Store(store.ownership.conn.Conn().PgConn().PID())
	store.ownership.mu.Unlock()
	read.pool, read.store = pool, store
	root := libraryRoot{id: read.root.RootID, libraryID: read.library.ID, path: read.root.Path,
		allowedPath: read.root.AllowedPath, relativePath: read.root.RelativePath}
	return rootBindingScanFixture{read, root, rootBindingScanOwnedTask(t, read.ctx, pool, store, read.library)}, trace
}

func TestScanReconciliationStagedPageMembershipUsesAntiJoin(t *testing.T) {
	fixture, trace := scanStagedPageMembershipFixture(t)
	const missing = scanReconciliationPageItems + 1
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT 'staged-missing-'||lpad(n::text,6,'0'),$1,$2,$1,'Missing','missing','Movie',false,
		$3||'/Missing-'||n::text||'.mkv','Missing-'||n::text||'.mkv'
		FROM generate_series(1,$4::integer) n`, fixture.library.ID, fixture.scanRoot.id, fixture.scanRoot.path, missing); err != nil {
		t.Fatal(err)
	}
	keptPath := libraryIntegrationFile(t, fixture.scanRoot.path, "Kept.mkv", "video:sealed-seen-source")
	presentPath := libraryIntegrationFile(t, fixture.scanRoot.path, "Present.mkv", "video:unseen-present-source")
	scanReconciliationCommitInsertItem(t, fixture, "staged-kept", "Kept.mkv", "Movie", fixture.library.ID, false)
	scanReconciliationCommitInsertItem(t, fixture, "staged-present", "Present.mkv", "Movie", fixture.library.ID, false)
	userDataSeed(t, fixture.ctx, fixture.pool, fixture.actor.User.ID, UserData{
		ItemID: "staged-kept", IsFavorite: true, PlayCount: 4, PlaybackPositionTicks: 19,
	})
	var protectedBefore string
	protectedSnapshot := `SELECT jsonb_build_object(
		'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id) FROM items i WHERE i.id=ANY($1::text[])),
		'user_data',(SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY u.user_id,u.item_id),'[]')
		FROM user_item_data u WHERE u.item_id=ANY($1::text[])))::text`
	protectedIDs := []string{"staged-kept", "staged-present"}
	if err := fixture.pool.QueryRow(fixture.ctx, protectedSnapshot, protectedIDs).Scan(&protectedBefore); err != nil {
		t.Fatal(err)
	}
	stage := scanReconciliationStageFixture(t, fixture, []string{"staged-kept"})
	adapter := &rootBindingScanTestCapture{snapshot: fixture.snapshot}
	capture := scanReconciliationCommitCapture(t, fixture, adapter)
	evidence := scanReconciliationCommitEvidence(t, capture)
	notifications := catalogChangesTestListener(t, fixture.store)
	checks := adapter.checks
	trace.Reset()
	albums, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage)
	if err != nil || len(albums) != 0 {
		t.Fatalf("reconcile two sealed candidate pages: albums=%v error=%v", albums, err)
	}
	pages, finalPages, membership, sealed := trace.counts()
	// Two nonempty final pages used to issue two additional membership SELECTs.
	// Closure and final membership still each need one 257-ID Contains batch.
	if pages != 3 || finalPages != 2 || membership != 2 || sealed != 6 {
		t.Fatalf("sealed page query consolidation changed its owner guards: pages=%d final_pages=%d membership=%d sealed=%d",
			pages, finalPages, membership, sealed)
	}
	if adapter.checks-checks != 3 {
		t.Fatalf("page consolidation changed complete filesystem proof count: %d", adapter.checks-checks)
	}
	owner := trace.Snapshot()
	if owner.OwnerTransactions != 2 || len(owner.OwnerTransactionSpans) != 2 ||
		owner.OwnerTransactionSpans[0].Finish != "commit" || owner.OwnerTransactionSpans[1].Finish != "commit" {
		t.Fatalf("two sealed authority transactions did not commit: %+v", owner)
	}
	var survivors int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM items
		WHERE library_id=$1 AND id LIKE 'staged-missing-%'`, fixture.library.ID).Scan(&survivors); err != nil || survivors != 0 {
		t.Fatalf("proven absent candidates survived: count=%d error=%v", survivors, err)
	}
	var protectedAfter string
	if err := fixture.pool.QueryRow(fixture.ctx, protectedSnapshot, protectedIDs).Scan(&protectedAfter); err != nil || protectedAfter != protectedBefore {
		t.Fatalf("sealed Seen, present source facts or UserData changed: error=%v", err)
	}
	for path, want := range map[string]string{keptPath: "video:sealed-seen-source", presentPath: "video:unseen-present-source"} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != want {
			t.Fatalf("reconciliation changed retained physical source: %v", err)
		}
	}
	facts := make([]CatalogChange, missing)
	for index := range facts {
		facts[index] = CatalogChange{Kind: CatalogRemoved, ItemID: fmt.Sprintf("staged-missing-%06d", index+1),
			LibraryID: fixture.library.ID, ParentID: fixture.library.ID}
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), facts)
	assertNoCatalogTestNotification(t, notifications)
	if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
		t.Fatalf("successful page consolidation lost its owner: %v", err)
	}
}

func TestScanReconciliationStagedPageClosedPassRollsBack(t *testing.T) {
	fixture, trace := scanStagedPageMembershipFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "closed-page-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
	stage := scanReconciliationStageFixture(t, fixture, nil)
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	before := scanReconciliationCommitSnapshot(t, fixture)
	closed := make(chan error, 1)
	trace.Reset()
	err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
		if err := stage.RequireSealed(tx); err != nil {
			return err
		}
		page, err := readScanReconciliationFinalPage(tx, fixture.library.ID, stage, scanReconciliationPreflight{})
		if err != nil || len(page) != 1 {
			return fmt.Errorf("admit fresh sealed first page: rows=%d error=%v", len(page), err)
		}
		if _, err := tx.Exec(`INSERT INTO server_settings(key,value) VALUES('staged-page-rollback','uncommitted')`); err != nil {
			return err
		}
		go func() { closed <- stage.Close() }()
		wait, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for !stage.closed.Load() {
			select {
			case <-wait.Done():
				return wait.Err()
			case <-ticker.C:
			}
		}
		_, err = collectScanReconciliationCandidates(tx, fixture.ctx, fixture.library.ID,
			map[string]*rootBindingScanCapture{fixture.scanRoot.id: capture}, evidence, stage,
			&scanReconciliationBudgetState{}, page)
		return err
	})
	if !errors.Is(err, errScanReconciliationStagingState) || scanReconciliationObservationOnly(err) {
		t.Fatalf("closed first page bypassed current sealed control: %v", err)
	}
	wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("retire closed sealed pass after protected rollback: %v", err)
		}
	case <-wait.Done():
		t.Fatal("closed pass did not retire after the owner rolled back")
	}
	owner := trace.Snapshot()
	if len(owner.OwnerTransactionSpans) == 0 || owner.OwnerTransactionSpans[0].Finish != "rollback" {
		t.Fatalf("closed page did not take protected rollback: %+v", owner)
	}
	var committed bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM server_settings WHERE key='staged-page-rollback')`).Scan(&committed); err != nil || committed {
		t.Fatalf("closed page committed earlier SQL: committed=%t error=%v", committed, err)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("closed first-page guard changed catalog or dependent records")
	}
	if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
		t.Fatalf("closed page rollback lost healthy ownership: %v", err)
	}
}

func TestScanReconciliationStagedPageGuardsPreservePass(t *testing.T) {
	for _, fault := range []string{"unsealed", "physical_budget", "source_reappeared"} {
		t.Run(fault, func(t *testing.T) {
			fixture, trace := scanStagedPageMembershipFixture(t)
			scanReconciliationCommitInsertItem(t, fixture, "guarded-page-missing", "Missing.mkv", "Movie", fixture.library.ID, false)
			stage := scanReconciliationStageFixture(t, fixture, nil)
			capture := scanReconciliationCommitCapture(t, fixture, nil)
			evidence := scanReconciliationCommitEvidence(t, capture)
			before := scanReconciliationCommitSnapshot(t, fixture)
			notifications := catalogChangesTestListener(t, fixture.store)
			trace.Reset()
			err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
				if err := stage.RequireSealed(tx); err != nil {
					return err
				}
				page, err := readScanReconciliationFinalPage(tx, fixture.library.ID, stage, scanReconciliationPreflight{})
				if err != nil || len(page) != 1 {
					return fmt.Errorf("admit guarded first page: rows=%d error=%v", len(page), err)
				}
				switch fault {
				case "unsealed":
					stage.sealed.Store(false)
				case "physical_budget":
					stage.limits.physicalBytes = 1
				case "source_reappeared":
					if err := os.WriteFile(filepath.Join(fixture.scanRoot.path, "Missing.mkv"), []byte("video:reappeared-source"), 0600); err != nil {
						return err
					}
				}
				_, err = collectScanReconciliationCandidates(tx, fixture.ctx, fixture.library.ID,
					map[string]*rootBindingScanCapture{fixture.scanRoot.id: capture}, evidence, stage,
					&scanReconciliationBudgetState{}, page)
				return err
			})
			want := errScanReconciliationStagingState
			if fault == "physical_budget" {
				want = errScanReconciliationStagingBudget
			} else if fault == "source_reappeared" {
				want = errScanReconciliationEvidenceUnavailable
			}
			if !errors.Is(err, want) || scanReconciliationObservationOnly(err) != (fault == "source_reappeared") {
				t.Fatalf("staged page changed guard error precedence: fault=%s error=%v want=%v", fault, err, want)
			}
			owner := trace.Snapshot()
			if len(owner.OwnerTransactionSpans) != 1 || owner.OwnerTransactionSpans[0].Finish != "rollback" {
				t.Fatalf("staged page guard did not roll back: %+v", owner)
			}
			if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
				t.Fatal("staged page guard changed catalog or dependent records")
			}
			assertNoCatalogTestNotification(t, notifications)
			if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
				t.Fatalf("staged page guard lost healthy ownership: %v", err)
			}
		})
	}
}

func TestScanReconciliationStagedPageSeenCascadeRetainsPass(t *testing.T) {
	fixture, trace := scanStagedPageMembershipFixture(t)
	scanReconciliationCommitInsertItem(t, fixture, "staged-missing-parent", "Gone", "Folder", fixture.library.ID, true)
	scanReconciliationCommitInsertItem(t, fixture, "staged-seen-child", "Gone/Child.mkv", "Movie", "staged-missing-parent", false)
	stage := scanReconciliationStageFixture(t, fixture, []string{"staged-seen-child"})
	capture := scanReconciliationCommitCapture(t, fixture, nil)
	evidence := scanReconciliationCommitEvidence(t, capture)
	before := scanReconciliationCommitSnapshot(t, fixture)
	notifications := catalogChangesTestListener(t, fixture.store)
	trace.Reset()
	_, err := fixture.store.reconcileMissingScanItems(fixture.task, fixture.library,
		[]*rootBindingScanCapture{capture}, evidence, nil, stage)
	if !errors.Is(err, errScanReconciliationEvidenceUnavailable) || !scanReconciliationObservationOnly(err) {
		t.Fatalf("anti-join bypassed Seen cascade protection: %v", err)
	}
	_, finalPages, membership, _ := trace.counts()
	if finalPages != 1 || membership != 1 {
		t.Fatalf("cascade discovery lost its necessary Contains: final_pages=%d membership=%d", finalPages, membership)
	}
	if after := scanReconciliationCommitSnapshot(t, fixture); after != before {
		t.Fatal("Seen descendant allowed its parent or dependent records to be removed")
	}
	assertNoCatalogTestNotification(t, notifications)
	if err := fixture.store.CheckOwnership(fixture.ctx); err != nil {
		t.Fatalf("Seen cascade rejection lost healthy ownership: %v", err)
	}
}
