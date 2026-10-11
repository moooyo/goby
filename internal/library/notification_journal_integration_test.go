package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/notificationjournal"
)

func notificationCollectorFixture(t *testing.T) (context.Context, *pgxpool.Pool, *Store, string) {
	t.Helper()
	ctx, pool, store, root, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	collection := libraryIntegrationCreate(t, ctx, store, "Notification collection", "movies", root)
	owner := playSessionOwnerFixture(t, ctx, pool, userID, "notification-collector")
	if _, err := pool.Exec(ctx, `UPDATE notification_transport SET enabled=true,endpoint='https://receiver.invalid/events',
		credential_ciphertext=decode(repeat('00',48),'hex') WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext)
		VALUES($1,$2,$3,$4,'',ARRAY['CatalogInvalidated'],decode(repeat('00',48),'hex'))`,
		strings.Repeat("a", 32), owner.SessionID, userID, owner.DeviceID); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, store, collection.ID
}

func TestNotificationReferenceAppendPathsShareOneTransactionIndex(t *testing.T) {
	ctx, pool, store, libraryID := notificationCollectorFixture(t)
	change := CatalogChange{Kind: CatalogUpdated, ItemID: libraryID, LibraryID: libraryID, IsFolder: true, IsCollectionFolder: true}
	rejected := errors.New("reject notification collector transaction")
	var rolledBackID string
	err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := analysisRecordChanges(tx, change); err != nil {
			return err
		}
		rolledBackID = tx.(*ownedCallbackTx).catalog.notificationMutationID
		return rejected
	})
	if !errors.Is(err, rejected) || rolledBackID == "" {
		t.Fatalf("failed collector transaction = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_source_events`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled-back notification escaped: count=%d, error=%v", count, err)
	}
	want := []notificationjournal.Reference{
		{Kind: "Item", ID: libraryID, LibraryID: libraryID, SourceID: libraryID},
		{Kind: "Library", ID: libraryID, LibraryID: libraryID},
	}
	var committedID string
	err = store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		view := tx.(*ownedCallbackTx)
		if len(view.catalog.notificationReferences) != 0 || len(view.catalog.notificationReferenceSet) != 0 ||
			view.catalog.notificationJournalRecorded || view.catalog.notificationJournalLength != 0 || view.catalog.notificationJournalResync {
			return errors.New("new transaction inherited rolled-back notification references or journal marker")
		}
		if err := analysisRecordChanges(tx, change); err != nil {
			return err
		}
		if err := analysisInvalidateCatalog(tx); err != nil {
			return err
		}
		if err := RebuildGeneratedSortNames(tx); err != nil {
			return err
		}
		if err := analysisInvalidateCatalog(tx); err != nil {
			return err
		}
		if err := analysisRecordChanges(tx, change); err != nil {
			return err
		}
		if !slices.Equal(view.catalog.notificationReferences, want) || len(view.catalog.notificationReferenceSet) != len(want) {
			return fmt.Errorf("append paths disagreed on ordered full references: %+v", view.catalog.notificationReferences)
		}
		committedID = view.catalog.notificationMutationID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if committedID == "" || committedID == rolledBackID {
		t.Fatal("a new transaction reused the rolled-back journal mutation")
	}
	var raw []byte
	var resync bool
	if err := pool.QueryRow(ctx, `SELECT refs,resync FROM notification_source_events WHERE id=$1`, committedID).Scan(&raw, &resync); err != nil {
		t.Fatal(err)
	}
	var actual []notificationjournal.Reference
	if err := json.Unmarshal(raw, &actual); err != nil || !resync || !slices.Equal(actual, want) {
		t.Fatalf("committed journal lost source scopes or global invalidation: %+v, resync=%t, error=%v", actual, resync, err)
	}
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { return analysisRecordChanges(tx, change) }); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_source_events`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("a later transaction suppressed its own repeated scope: count=%d, error=%v", count, err)
	}
}

func TestNotificationReferenceOverflowRetainsAtomicJournalFailure(t *testing.T) {
	ctx, pool, store, libraryID := notificationCollectorFixture(t)
	changes := make([]CatalogChange, 4097)
	for index := range changes {
		changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("item-%04d", index), LibraryID: libraryID}
	}
	err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`INSERT INTO server_settings(key,value) VALUES('notification-overflow','must roll back')`); err != nil {
			return err
		}
		return analysisRecordChanges(tx, changes...)
	})
	if !errors.Is(err, notificationjournal.ErrCapacity) {
		t.Fatalf("unscoped overflow changed its journal failure: %v", err)
	}
	var count int
	var leaked bool
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notification_source_events),
		EXISTS(SELECT 1 FROM server_settings WHERE key='notification-overflow')`).Scan(&count, &leaked); err != nil || count != 0 || leaked {
		t.Fatalf("overflow partially committed its business write or journal: count=%d, leaked=%t, error=%v", count, leaked, err)
	}
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { return analysisRecordChanges(tx, changes[:4096]...) }); err != nil {
		t.Fatalf("the exact reference limit inherited overflow from another transaction: %v", err)
	}
	var raw []byte
	var resync bool
	if err := pool.QueryRow(ctx, `SELECT refs,resync FROM notification_source_events`).Scan(&raw, &resync); err != nil {
		t.Fatal(err)
	}
	var refs []notificationjournal.Reference
	if err := json.Unmarshal(raw, &refs); err != nil || len(refs) != 4096 || !resync {
		t.Fatalf("exact-limit journal changed its bounded resync scopes: count=%d, resync=%t, error=%v", len(refs), resync, err)
	}
	for index, ref := range refs {
		want := notificationjournal.Reference{Kind: "Item", ID: changes[index].ItemID, LibraryID: libraryID, SourceID: changes[index].ItemID}
		if ref != want {
			t.Fatalf("exact-limit journal changed reference %d: %+v", index, ref)
		}
	}
}
