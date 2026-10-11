package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/notificationjournal"
)

type notificationJournalWrite struct {
	mutationID string
	refs       []notificationjournal.Reference
	resync     bool
}

type notificationJournalTestTx struct {
	pgx.Tx
	writes []notificationJournalWrite
	err    error
}

func (tx *notificationJournalTestTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	var refs []notificationjournal.Reference
	if err := json.Unmarshal(args[3].([]byte), &refs); err != nil {
		return pgconn.CommandTag{}, err
	}
	tx.writes = append(tx.writes, notificationJournalWrite{mutationID: args[0].(string), refs: refs, resync: args[5].(bool)})
	return pgconn.CommandTag{}, tx.err
}

func TestNotificationJournalUnchangedFlushSkipsEncoding(t *testing.T) {
	for _, count := range []int{0, 1024} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			driver := &notificationJournalTestTx{}
			tx := &ownedTx{Tx: driver, ctx: context.Background()}
			for index := 0; index < count; index++ {
				tx.rememberNotificationScope("library", fmt.Sprintf("item-%04d", index))
			}
			if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != 0 || tx.notificationMutationID != "" {
				t.Fatalf("empty catalog batch recorded a journal mutation: writes=%d, error=%v", len(driver.writes), err)
			}
			tx.catalogChanges.requireResync()
			if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != 1 {
				t.Fatalf("initial resync did not record its references: writes=%d, error=%v", len(driver.writes), err)
			}
			if !driver.writes[0].resync || !slices.Equal(driver.writes[0].refs, tx.notificationReferences) {
				t.Fatal("initial resync changed its ordered references")
			}
			tx.rememberNotificationReferences(tx.notificationReferences...)
			allocations := testing.AllocsPerRun(100, func() {
				if err := tx.recordNotificationJournal(); err != nil {
					t.Fatal(err)
				}
			})
			if allocations != 0 || len(driver.writes) != 1 {
				t.Fatalf("unchanged journal flush allocated or wrote again: allocations=%g, writes=%d", allocations, len(driver.writes))
			}
		})
	}
}

func TestNotificationJournalRetainsAppendAndResyncOnlyChanges(t *testing.T) {
	driver := &notificationJournalTestTx{}
	change := CatalogChange{Kind: CatalogUpdated, ItemID: "first", LibraryID: "library"}
	tx := &ownedTx{Tx: driver, ctx: context.Background(), catalogChanges: catalogChangeBatch{changes: []CatalogChange{change}}}
	tx.rememberNotificationScope("library", "first")
	for index, resync := range []bool{false, true, false} {
		// Auxiliary batch reconstruction can replace the change batch while the
		// transaction's append-only notification references remain intact.
		tx.catalogChanges = catalogChangeBatch{changes: []CatalogChange{change}, resync: resync}
		if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != index+1 {
			t.Fatalf("resync-only change was skipped: writes=%d, error=%v", len(driver.writes), err)
		}
		write := driver.writes[index]
		if write.resync != resync || write.mutationID != driver.writes[0].mutationID || !slices.Equal(write.refs, tx.notificationReferences) {
			t.Fatal("resync-only change lost its mutation identity or reference prefix")
		}
	}
	tx.rememberNotificationScope("library", "second")
	if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != 4 {
		t.Fatalf("new reference was not recorded: writes=%d, error=%v", len(driver.writes), err)
	}
	if write := driver.writes[3]; write.mutationID != driver.writes[0].mutationID || !slices.Equal(write.refs, tx.notificationReferences) {
		t.Fatal("appended reference lost its mutation identity or earlier source scope")
	}
}

func TestNotificationJournalRetriesFailedInitialAppendAndResyncWrites(t *testing.T) {
	for _, mode := range []string{"initial", "append", "resync"} {
		t.Run(mode, func(t *testing.T) {
			driver := &notificationJournalTestTx{}
			change := CatalogChange{Kind: CatalogUpdated, ItemID: "first", LibraryID: "library"}
			tx := &ownedTx{Tx: driver, ctx: context.Background(), catalogChanges: catalogChangeBatch{changes: []CatalogChange{change}}}
			tx.rememberNotificationScope("library", "first")
			if mode != "initial" {
				if err := tx.recordNotificationJournal(); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "append":
				tx.rememberNotificationScope("library", "second")
			case "resync":
				tx.catalogChanges.requireResync()
			}
			before := len(driver.writes)
			driver.err = errors.New("journal write failed")
			if err := tx.recordNotificationJournal(); !errors.Is(err, notificationjournal.ErrJournal) || len(driver.writes) != before+1 {
				t.Fatalf("journal failure was suppressed: writes=%d, error=%v", len(driver.writes), err)
			}
			failed := driver.writes[before]
			driver.err = nil
			if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != before+2 {
				t.Fatalf("failed journal write advanced the successful marker: writes=%d, error=%v", len(driver.writes), err)
			}
			retried := driver.writes[before+1]
			if retried.mutationID != failed.mutationID || retried.resync != failed.resync || !slices.Equal(retried.refs, failed.refs) {
				t.Fatal("journal retry changed the pending mutation")
			}
			if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != before+2 {
				t.Fatalf("successful retry did not suppress unchanged writes: writes=%d, error=%v", len(driver.writes), err)
			}
		})
	}
}

func TestNotificationJournalPreservesReferenceAndRawByteOverflow(t *testing.T) {
	for _, test := range []struct {
		name     string
		count    int
		longIDs  bool
		overflow bool
	}{
		{name: "exact reference limit", count: 4096},
		{name: "reference overflow", count: 4097, overflow: true},
		{name: "raw byte overflow", count: 1024, longIDs: true, overflow: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := &notificationJournalTestTx{}
			change := CatalogChange{Kind: CatalogUpdated, ItemID: "first", LibraryID: "library"}
			tx := &ownedTx{Tx: driver, ctx: context.Background(), catalogChanges: catalogChangeBatch{changes: []CatalogChange{change}}}
			for index := 0; index < test.count; index++ {
				id, libraryID := fmt.Sprintf("item-%04d", index), "library"
				if test.longIDs {
					id = fmt.Sprintf("%04d", index) + strings.Repeat("i", 252)
					libraryID = strings.Repeat("l", 256)
				}
				tx.rememberNotificationScope(libraryID, id)
			}
			if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != 1 {
				t.Fatalf("bounded journal was not recorded: writes=%d, error=%v", len(driver.writes), err)
			}
			write := driver.writes[0]
			if write.resync != test.overflow || test.overflow && len(write.refs) != 0 || !test.overflow && !slices.Equal(write.refs, tx.notificationReferences) {
				t.Fatalf("journal overflow conversion changed: references=%d, resync=%t", len(write.refs), write.resync)
			}
			if err := tx.recordNotificationJournal(); err != nil || len(driver.writes) != 1 {
				t.Fatalf("unchanged bounded journal was recorded again: writes=%d, error=%v", len(driver.writes), err)
			}
		})
	}
}

func TestNotificationReferencesPreserveFullScopeAndFirstInsertionOrder(t *testing.T) {
	tx := new(ownedTx)
	tx.rememberNotificationScope("library-a", "first", "parent", "parent")
	tx.rememberNotificationScope("library-a", "first", "parent")
	tx.rememberNotificationScope("library-a", "second", "parent")
	tx.rememberNotificationReferences(
		notificationjournal.Reference{Kind: "Library", ID: "parent", LibraryID: "library-a"},
		notificationjournal.Reference{Kind: "Item", ID: "parent", LibraryID: "library-b", SourceID: "first"},
		notificationjournal.Reference{Kind: "Item", ID: "parent", LibraryID: "library-a", SourceID: "first"},
	)
	want := []notificationjournal.Reference{
		{Kind: "Item", ID: "first", LibraryID: "library-a", SourceID: "first"},
		{Kind: "Item", ID: "parent", LibraryID: "library-a", SourceID: "first"},
		{Kind: "Item", ID: "second", LibraryID: "library-a", SourceID: "second"},
		{Kind: "Item", ID: "parent", LibraryID: "library-a", SourceID: "second"},
		{Kind: "Library", ID: "parent", LibraryID: "library-a"},
		{Kind: "Item", ID: "parent", LibraryID: "library-b", SourceID: "first"},
	}
	if !slices.Equal(tx.notificationReferences, want) {
		t.Fatalf("reference order or source scopes changed: %+v", tx.notificationReferences)
	}
	if len(tx.notificationReferenceSet) != len(want) {
		t.Fatal("the transaction reference index diverged from its ordered references")
	}
}

func TestNotificationReferencesRetainExactOverflowSentinel(t *testing.T) {
	tx := new(ownedTx)
	for index := 0; index < 4096; index++ {
		id := fmt.Sprintf("item-%04d", index)
		tx.rememberNotificationScope("library", id)
	}
	tx.rememberNotificationScope("library", "item-0000")
	if len(tx.notificationReferences) != 4096 {
		t.Fatal("an exact duplicate overflowed the legal reference limit")
	}
	overflow := notificationjournal.Reference{Kind: "Library", ID: "library", LibraryID: "library"}
	tx.rememberNotificationReferences(overflow)
	before := slices.Clone(tx.notificationReferences)
	changes := make([]CatalogChange, 32768)
	for index := range changes {
		changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("late-%05d", index), LibraryID: "library", ParentID: "parent"}
	}
	tx.rememberNotificationChanges(changes)
	tx.rememberNotificationReferences(notificationjournal.Reference{Kind: "Library", ID: "other", LibraryID: "other"})
	if len(tx.notificationReferenceSet) != 4097 || !slices.Equal(tx.notificationReferences, before) || tx.notificationReferences[4096] != overflow {
		t.Fatal("saturated collection changed its bounded overflow sentinel or retained later scopes")
	}
}

func TestNotificationReferencesBoundLargeCatalogBatches(t *testing.T) {
	for _, count := range []int{0, 1024, 32768} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			changes := make([]CatalogChange, count)
			for index := range changes {
				changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("item-%05d", index), LibraryID: "library", ParentID: "shared-parent"}
			}
			tx := new(ownedTx)
			tx.rememberNotificationChanges(changes)
			want := min(2*count, 4097)
			if len(tx.notificationReferences) != want || len(tx.notificationReferenceSet) != want {
				t.Fatalf("%d changes retained %d references and %d index entries, want %d", count, len(tx.notificationReferences), len(tx.notificationReferenceSet), want)
			}
			for index, ref := range tx.notificationReferences {
				source := changes[index/2].ItemID
				id := source
				if index%2 != 0 {
					id = "shared-parent"
				}
				if ref != (notificationjournal.Reference{Kind: "Item", ID: id, LibraryID: "library", SourceID: source}) {
					t.Fatalf("large batch lost first-insertion source order at reference %d", index)
				}
			}
		})
	}
}
