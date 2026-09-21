package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func scanStagingTestBegin(t *testing.T, ctx context.Context, store *Store, scanID, libraryID string) *scanReconciliationStaging {
	t.Helper()
	staging, err := store.beginScanReconciliationStaging(ctx, scanID, libraryID)
	if err != nil {
		t.Fatalf("begin isolated Seen staging: %v", err)
	}
	generation, actualScan, actualLibrary := staging.Scope()
	if generation == "" || actualScan != scanID || actualLibrary != libraryID {
		t.Fatalf("staging did not bind its complete scope: %q, %q, %q", generation, actualScan, actualLibrary)
	}
	return staging
}

func scanStagingTestIDs(count, width int) []string {
	ids := make([]string, count)
	for index := range ids {
		ids[index] = fmt.Sprintf("%0*x", width, index+1)
	}
	return ids
}

func scanStagingTestRecord(t *testing.T, ctx context.Context, staging *scanReconciliationStaging, ids []string) {
	t.Helper()
	for _, id := range ids {
		if err := staging.Record(ctx, id); err != nil {
			t.Fatalf("record accepted identity: %v", err)
		}
	}
}

func scanStagingTestContains(t *testing.T, ctx context.Context, store *Store, staging *scanReconciliationStaging, present, absent []string) {
	t.Helper()
	requested := append(append([]string{}, present...), absent...)
	err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := staging.RequireSealed(tx); err != nil {
			return err
		}
		found, err := staging.Contains(tx, requested)
		if err != nil {
			return err
		}
		for _, id := range present {
			if !found[id] {
				return fmt.Errorf("accepted identity %q is absent from its sealed pass", id)
			}
		}
		for _, id := range absent {
			if found[id] {
				return fmt.Errorf("unseen identity %q borrowed another pass's membership", id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read exact sealed membership on the owner session: %v", err)
	}
}

func scanStagingTestStoredTotals(t *testing.T, ctx context.Context, store *Store, staging *scanReconciliationStaging) (int64, int64) {
	t.Helper()
	generation, scanID, libraryID := staging.Scope()
	var rows, serialized int64
	err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		return tx.QueryRow(`SELECT count(*), COALESCE(sum(4 + octet_length(convert_to(item_id,'UTF8'))), 0)
			FROM `+scanReconciliationSeenTable+`
			WHERE generation=$1 AND scan_id=$2 AND library_id=$3`, generation, scanID, libraryID).Scan(&rows, &serialized)
	})
	if err != nil {
		t.Fatalf("read the committed Seen accounting: %v", err)
	}
	return rows, serialized
}

func scanStagingTestOwnerPID(t *testing.T, ctx context.Context, store *Store) int32 {
	t.Helper()
	var pid int32
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		return tx.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid)
	}); err != nil {
		t.Fatalf("identify the reserved owner backend: %v", err)
	}
	return pid
}

func scanStagingTestRelationsAbsent(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	var seenAbsent, passAbsent bool
	err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		return tx.QueryRow(`SELECT to_regclass($1) IS NULL, to_regclass($2) IS NULL`,
			scanReconciliationSeenTable, scanReconciliationPassTable).Scan(&seenAbsent, &passAbsent)
	})
	if err != nil || !seenAbsent || !passAbsent {
		t.Fatalf("the last pass retained private temp relations: seen absent=%t, pass absent=%t, error=%v", seenAbsent, passAbsent, err)
	}
}

func TestScanReconciliationStagingFlushesCompleteAndTailBatches(t *testing.T) {
	for _, count := range []int{511, 512, 513} {
		t.Run(fmt.Sprintf("ids_%d", count), func(t *testing.T) {
			ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			ownerPID := scanStagingTestOwnerPID(t, ctx, store)
			staging := scanStagingTestBegin(t, ctx, store, "tail-scan", "tail-library")
			ids := scanStagingTestIDs(count, 32)
			scanStagingTestRecord(t, ctx, staging, ids)
			if err := staging.Seal(ctx); err != nil {
				t.Fatalf("flush the complete accepted tail and seal: %v", err)
			}
			stats := staging.Stats()
			if stats.Rows != int64(count) || stats.SerializedBytes != int64(count)*36 || stats.BufferedIDs != 0 ||
				stats.Flushes != (count+511)/512 || stats.PeakBufferedIDs > 512 || stats.PeakBufferedBytes > 128<<10 {
				t.Fatalf("batching lost the tail or exceeded its finite buffer: %#v", stats)
			}
			rows, serialized := scanStagingTestStoredTotals(t, ctx, store, staging)
			if rows != stats.Rows || serialized != stats.SerializedBytes {
				t.Fatalf("published counters differ from committed identities: rows=%d, bytes=%d, stats=%#v", rows, serialized, stats)
			}
			scanStagingTestContains(t, ctx, store, staging, []string{ids[0], ids[count/2], ids[count-1]}, []string{"never-accepted"})
			if actual := scanStagingTestOwnerPID(t, ctx, store); actual != ownerPID {
				t.Fatalf("staging changed the reserved backend: got %d, want %d", actual, ownerPID)
			}
		})
	}
}

func TestScanReconciliationStagingCountsTheArrayHeaderInItsBuffer(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	staging := scanStagingTestBegin(t, ctx, store, "byte-buffer-scan", "byte-buffer-library")
	// Each identity costs 256 serialized bytes. All 512 identities would fill
	// 128 KiB before the additional 20-byte PostgreSQL array header is counted.
	ids := scanStagingTestIDs(512, 252)
	scanStagingTestRecord(t, ctx, staging, ids)
	if err := staging.Seal(ctx); err != nil {
		t.Fatalf("seal the byte-bounded tail: %v", err)
	}
	stats := staging.Stats()
	if stats.Rows != 512 || stats.SerializedBytes != 128<<10 || stats.Flushes != 2 ||
		stats.PeakBufferedIDs != 511 || stats.PeakBufferedBytes > 128<<10 || stats.BufferedIDs != 0 {
		t.Fatalf("the array header escaped the buffer bound: %#v", stats)
	}
	scanStagingTestContains(t, ctx, store, staging, []string{ids[0], ids[511]}, []string{"never-accepted"})
}

func TestScanReconciliationStagingChargesOnlyNewRowsAtExactLimits(t *testing.T) {
	for _, limitKind := range []string{"rows", "serialized_bytes"} {
		for _, overflow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_overflow_%t", limitKind, overflow), func(t *testing.T) {
				ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
				ownerPID := scanStagingTestOwnerPID(t, ctx, store)
				limits := defaultScanReconciliationStagingLimits()
				if limitKind == "rows" {
					limits.rows = 513
				} else {
					limits.serializedBytes = 513 * 36
				}
				staging, err := store.beginScanReconciliationStagingWithLimits(ctx, "quota-scan", "quota-library", limits)
				if err != nil {
					t.Fatalf("begin the lowered real PostgreSQL quota: %v", err)
				}
				ids := scanStagingTestIDs(514, 32)
				scanStagingTestRecord(t, ctx, staging, ids[:513])
				// The first identity has crossed a committed batch boundary; the
				// final identity is also repeated in the remaining small buffer.
				scanStagingTestRecord(t, ctx, staging, []string{ids[0], ids[512], ids[0]})
				if overflow {
					if err = staging.Record(ctx, ids[513]); err != nil && !errors.Is(err, errScanReconciliationStagingBudget) {
						t.Fatalf("record the one excess identity: %v", err)
					}
				}
				sealErr := staging.Seal(ctx)
				if overflow {
					if !errors.Is(sealErr, errScanReconciliationStagingBudget) {
						t.Fatalf("the exact quota admitted one additional identity: %v", sealErr)
					}
					if err := staging.Record(ctx, ids[0]); !errors.Is(err, errScanReconciliationStagingBudget) {
						t.Fatalf("a duplicate erased the sticky quota failure: %v", err)
					}
					ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
					return
				}
				if sealErr != nil {
					t.Fatalf("duplicates consumed the exact quota twice: %v", sealErr)
				}
				rows, serialized := scanStagingTestStoredTotals(t, ctx, store, staging)
				stats := staging.Stats()
				if rows != 513 || serialized != 513*36 || stats.Rows != rows || stats.SerializedBytes != serialized {
					t.Fatalf("duplicates inflated committed accounting: rows=%d, bytes=%d, stats=%#v", rows, serialized, stats)
				}
				scanStagingTestContains(t, ctx, store, staging, []string{ids[0], ids[512]}, []string{ids[513]})
			})
		}
	}
}

func TestScanReconciliationStagingSealsAnEmptyCompletePass(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	staging := scanStagingTestBegin(t, ctx, store, "empty-scan", "empty-library")
	if err := staging.Seal(ctx); err != nil {
		t.Fatalf("a legitimate empty Seen set could not be sealed: %v", err)
	}
	scanStagingTestContains(t, ctx, store, staging, nil, []string{"absent-item"})
	stats := staging.Stats()
	if stats.Rows != 0 || stats.SerializedBytes != 0 || stats.BufferedIDs != 0 {
		t.Fatalf("empty staging invented accepted identities: %#v", stats)
	}
}

func TestScanReconciliationStagingCountsUTF8BytesWithoutMergingIdentities(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	limits := defaultScanReconciliationStagingLimits()
	limits.rows, limits.serializedBytes = 2, 13
	staging, err := store.beginScanReconciliationStagingWithLimits(ctx, "utf8-scan", "utf8-library", limits)
	if err != nil {
		t.Fatalf("begin the exact UTF-8 byte quota: %v", err)
	}
	// These distinct UTF-8 byte sequences contain two and three bytes. Their
	// four-byte serialized lengths make the exact total six plus seven bytes.
	ids := []string{"\u00e9", "e\u0301"}
	scanStagingTestRecord(t, ctx, staging, []string{ids[0], ids[1], ids[0], ids[1]})
	if err := staging.Seal(ctx); err != nil {
		t.Fatalf("seal distinct identities at the exact byte quota: %v", err)
	}
	rows, serialized := scanStagingTestStoredTotals(t, ctx, store, staging)
	if stats := staging.Stats(); rows != 2 || serialized != 13 || stats.Rows != rows || stats.SerializedBytes != serialized {
		t.Fatalf("UTF-8 identities were normalized or counted as runes: rows=%d, bytes=%d, stats=%#v", rows, serialized, stats)
	}
	scanStagingTestContains(t, ctx, store, staging, ids, []string{"e"})
}

func TestScanReconciliationStagingRequiresItsExactSealedControl(t *testing.T) {
	for _, fault := range []string{"unsealed", "generation", "library", "row_count", "byte_count", "missing_control", "missing_seen"} {
		t.Run(fault, func(t *testing.T) {
			ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
			staging := scanStagingTestBegin(t, ctx, store, "scope-scan", "scope-library")
			generation, scanID, libraryID := staging.Scope()
			if fault != "unsealed" {
				if err := staging.Seal(ctx); err != nil {
					t.Fatalf("seal the control before corrupting its scope: %v", err)
				}
				err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
					if fault == "missing_seen" {
						_, err := tx.Exec(`DROP TABLE ` + scanReconciliationSeenTable)
						return err
					}
					statement := `DELETE FROM ` + scanReconciliationPassTable + ` WHERE generation=$1 AND scan_id=$2 AND library_id=$3`
					arguments := []any{generation, scanID, libraryID}
					if fault == "generation" {
						other := strings.Repeat("f", len(generation))
						if other == generation {
							other = strings.Repeat("e", len(generation))
						}
						statement = `UPDATE ` + scanReconciliationPassTable + ` SET generation=$4 WHERE generation=$1 AND scan_id=$2 AND library_id=$3`
						arguments = append(arguments, other)
					} else if fault == "library" {
						statement = `UPDATE ` + scanReconciliationPassTable + ` SET library_id='different-library' WHERE generation=$1 AND scan_id=$2 AND library_id=$3`
					} else if fault == "row_count" {
						statement = `UPDATE ` + scanReconciliationPassTable + ` SET seen_rows=1 WHERE generation=$1 AND scan_id=$2 AND library_id=$3`
					} else if fault == "byte_count" {
						statement = `UPDATE ` + scanReconciliationPassTable + ` SET serialized_bytes=1 WHERE generation=$1 AND scan_id=$2 AND library_id=$3`
					}
					tag, err := tx.Exec(statement, arguments...)
					if err == nil && tag.RowsAffected() != 1 {
						return fmt.Errorf("control corruption affected %d rows", tag.RowsAffected())
					}
					return err
				})
				if err != nil {
					t.Fatalf("prepare the exact control fault: %v", err)
				}
			}
			err := store.WithOwnedTx(ctx, func(tx OwnedTx) error { return staging.RequireSealed(tx) })
			if fault == "missing_seen" {
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "42P01" || scanReconciliationObservationOnly(err) {
					t.Fatalf("a missing Seen relation became an empty successful pass: %v", err)
				}
				return
			}
			if !errors.Is(err, errScanReconciliationStagingState) {
				t.Fatalf("%s became an empty successful Seen set: %v", fault, err)
			}
		})
	}
}

func TestScanReconciliationStagingKeepsTwoActivePassesIsolated(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	first := scanStagingTestBegin(t, ctx, store, "first-scan", "first-library")
	second := scanStagingTestBegin(t, ctx, store, "second-scan", "second-library")
	if third, err := store.beginScanReconciliationStaging(ctx, "third-scan", "third-library"); err == nil || third != nil {
		t.Fatalf("the owner admitted an unbounded third active pass: %#v, %v", third, err)
	}
	for _, entry := range []struct {
		staging *scanReconciliationStaging
		id      string
	}{{first, "shared-item"}, {second, "shared-item"}, {first, "first-only"}, {second, "second-only"}} {
		if err := entry.staging.Record(ctx, entry.id); err != nil {
			t.Fatalf("interleave the two active pass buffers: %v", err)
		}
	}
	for _, staging := range []*scanReconciliationStaging{first, second} {
		if err := staging.Seal(ctx); err != nil {
			t.Fatalf("seal an independently scoped pass: %v", err)
		}
	}
	scanStagingTestContains(t, ctx, store, first, []string{"shared-item", "first-only"}, []string{"second-only"})
	scanStagingTestContains(t, ctx, store, second, []string{"shared-item", "second-only"}, []string{"first-only"})
	if err := first.Close(); err != nil {
		t.Fatalf("clean the first pass while its sibling remains active: %v", err)
	}
	if stats := first.Stats(); stats.SessionPhysicalBytes <= 0 {
		t.Fatalf("scoped row cleanup claimed the sibling's relation space was released: %#v", stats)
	}
	if rows, _ := scanStagingTestStoredTotals(t, ctx, store, first); rows != 0 {
		t.Fatalf("the first pass retained %d Seen rows after cleanup", rows)
	}
	scanStagingTestContains(t, ctx, store, second, []string{"shared-item", "second-only"}, []string{"first-only"})
	if err := second.Close(); err != nil {
		t.Fatalf("drop staging after the last active pass: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("repeated scoped cleanup was not idempotent: %v", err)
	}
	scanStagingTestRelationsAbsent(t, ctx, store)
}

func TestScanReconciliationStagingSQLFailureRollsBackWithoutLosingOwner(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := scanStagingTestOwnerPID(t, ctx, store)
	staging := scanStagingTestBegin(t, ctx, store, "failed-flush-scan", "failed-flush-library")
	err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`ALTER TABLE ` + scanReconciliationSeenTable + ` ADD CONSTRAINT reject_test_identity CHECK (item_id <> 'rejected-item')`)
		return err
	})
	if err != nil {
		t.Fatalf("install an ordinary server-side statement failure: %v", err)
	}
	scanStagingTestRecord(t, ctx, staging, []string{"accepted-before-error", "rejected-item"})
	err = staging.Seal(ctx)
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" || scanReconciliationObservationOnly(err) {
		t.Fatalf("a real SQL constraint failure was hidden or reclassified: %v", err)
	}
	if rows, serialized := scanStagingTestStoredTotals(t, ctx, store, staging); rows != 0 || serialized != 0 {
		t.Fatalf("a rejected flush partly committed: rows=%d, bytes=%d", rows, serialized)
	}
	if err := staging.Record(ctx, "later-item"); !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("later work erased the first failed flush: %v", err)
	}
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
}

func TestScanReconciliationStagingBackendLossFencesTheOldGeneration(t *testing.T) {
	ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	ownerPID := scanStagingTestOwnerPID(t, ctx, store)
	staging := scanStagingTestBegin(t, ctx, store, "lost-owner-scan", "lost-owner-library")
	ids := scanStagingTestIDs(513, 32)
	scanStagingTestRecord(t, ctx, staging, ids)
	generation, scanID, libraryID := staging.Scope()
	ownedTransactionsTerminateBackend(t, ctx, pool, ownerPID)
	if err := staging.Seal(ctx); !errors.Is(err, ErrUnavailable) || store.Available() {
		t.Fatalf("backend loss did not fence incomplete staging: %v", err)
	}
	if err := staging.Record(ctx, "late-old-worker"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("an old worker resumed staging after ownership loss: %v", err)
	}
	successor, err := New(pool, &libraryFixtureProber{}, []string{allowedRoot})
	if err != nil {
		t.Fatalf("acquire a fresh owner after confirmed backend termination: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := successor.Close(cleanup); err != nil {
			t.Errorf("close the successor owner: %v", err)
		}
	})
	replacement := scanStagingTestBegin(t, ctx, successor, scanID+"-successor", libraryID)
	if replacementGeneration, _, _ := replacement.Scope(); replacementGeneration == generation {
		t.Fatal("a replacement backend reused the lost owner generation")
	}
	if err := replacement.Seal(ctx); err != nil {
		t.Fatalf("seal a genuinely empty replacement pass: %v", err)
	}
	scanStagingTestContains(t, ctx, successor, replacement, nil, []string{ids[0], ids[512]})
	if err := successor.WithOwnedTx(ctx, func(tx OwnedTx) error {
		return staging.RequireSealed(tx)
	}); !errors.Is(err, errScanReconciliationStagingState) {
		t.Fatalf("the successor transaction accepted an old staging handle: %v", err)
	}
	called := false
	err = store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		called = true
		return staging.RequireSealed(tx)
	})
	if !errors.Is(err, ErrUnavailable) || called {
		t.Fatalf("a lost owner admitted a final reconciliation callback: called=%t, error=%v", called, err)
	}
}

func TestScanReconciliationStagingPhysicalLimitDiscardsTheOwner(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	limits := defaultScanReconciliationStagingLimits()
	// Even empty real PostgreSQL relations and their indexes exceed one byte.
	limits.physicalBytes = 1
	staging, err := store.beginScanReconciliationStagingWithLimits(ctx, "physical-limit-scan", "physical-limit-library", limits)
	if staging != nil || !errors.Is(err, errScanReconciliationStagingBudget) || !errors.Is(err, ErrUnavailable) || store.Available() {
		t.Fatalf("a physical quota failure retained a reusable owner: staging=%#v, error=%v", staging, err)
	}
	called := false
	err = store.WithOwnedTx(ctx, func(OwnedTx) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrUnavailable) || called {
		t.Fatalf("discarded physical staging admitted another owned callback: called=%t, error=%v", called, err)
	}
}

func TestScanReconciliationStagingFlushPhysicalLimitAlsoFencesItsSibling(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	first := scanStagingTestBegin(t, ctx, store, "physical-sibling-scan", "physical-sibling-library")
	if err := first.Seal(ctx); err != nil {
		t.Fatalf("seal the independently empty sibling: %v", err)
	}
	limits := defaultScanReconciliationStagingLimits()
	// Admit the second control row, then let a real bounded INSERT cross the
	// session's observed allocation plus 16 KiB. Long unique identities force
	// more storage than that allowance without allocating a large test corpus.
	limits.physicalBytes = first.Stats().SessionPhysicalBytes + 16<<10
	second, err := store.beginScanReconciliationStagingWithLimits(ctx, "physical-flush-scan", "physical-flush-library", limits)
	if err != nil {
		t.Fatalf("admit the second pass before its first bounded INSERT: %v", err)
	}
	for _, id := range scanStagingTestIDs(512, 252) {
		err = second.Record(ctx, id)
		if err != nil {
			break
		}
	}
	if !errors.Is(err, errScanReconciliationStagingBudget) || !errors.Is(err, ErrUnavailable) || store.Available() {
		t.Fatalf("a growing physical relation escaped the shared ceiling: %v", err)
	}
	if err := second.Seal(ctx); !errors.Is(err, errScanReconciliationStagingBudget) {
		t.Fatalf("a failed physical flush later acquired sealing authority: %v", err)
	}
	if err := first.Seal(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("the disposed shared backend left its sibling authoritative: %v", err)
	}
	if rows := second.Stats().Rows; rows != 0 {
		t.Fatalf("the over-budget batch published %d committed rows", rows)
	}
}

func TestScanReconciliationStagingDoesNotAdoptAnExistingTempRelation(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`CREATE TEMP TABLE ` + scanReconciliationPassTable + ` (unrelated integer) ON COMMIT PRESERVE ROWS`)
		return err
	}); err != nil {
		t.Fatalf("prepare a same-named unrelated session relation: %v", err)
	}
	staging, err := store.beginScanReconciliationStaging(ctx, "collision-scan", "collision-library")
	var databaseError *pgconn.PgError
	if staging != nil || !errors.As(err, &databaseError) || databaseError.Code != "42P07" ||
		!errors.Is(err, ErrUnavailable) || store.Available() {
		t.Fatalf("initialization adopted or returned a colliding private session: staging=%#v, error=%v", staging, err)
	}
}

func TestScanReconciliationStagingConfirmedDDLRollbackKeepsTheOwner(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	ownerPID := scanStagingTestOwnerPID(t, ctx, store)
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	published := false
	err := store.withScanStagingTx(cancelled, false, func(transaction *scanStagingTx) (func(), error) {
		if err := transaction.create(); err != nil {
			return nil, err
		}
		// This is the cancellation boundary after actual DDL and before the
		// commit acknowledgement can publish any staging authority.
		cancel()
		return func() { published = true }, nil
	})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnavailable) || published {
		t.Fatalf("confirmed DDL rollback lost the owner or published state: published=%t, error=%v", published, err)
	}
	scanStagingTestRelationsAbsent(t, ctx, store)
	ownedTransactionsAssertReusable(t, ctx, store, ownerPID)
	replacement := scanStagingTestBegin(t, ctx, store, "after-ddl-cancel-scan", "after-ddl-cancel-library")
	if err := replacement.Seal(ctx); err != nil {
		t.Fatalf("initialize fresh staging after confirmed DDL rollback: %v", err)
	}
	scanStagingTestContains(t, ctx, store, replacement, nil, []string{"not-accepted"})
}

func TestScanReconciliationStagingCleanupFailureDiscardsAndRetainsItsError(t *testing.T) {
	ctx, _, store, _, _ := libraryIntegrationStore(t, &libraryFixtureProber{}, ErrUnavailable)
	staging := scanStagingTestBegin(t, ctx, store, "failed-cleanup-scan", "failed-cleanup-library")
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		_, err := tx.Exec(`DROP TABLE ` + scanReconciliationSeenTable)
		return err
	}); err != nil {
		t.Fatalf("prepare a missing owned relation before cleanup: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		err := staging.Close()
		var databaseError *pgconn.PgError
		if !errors.Is(err, ErrUnavailable) || !errors.As(err, &databaseError) || databaseError.Code != "42P01" || store.Available() {
			t.Fatalf("cleanup attempt %d lost its failure or reused the poisoned session: %v", attempt, err)
		}
	}
}

func TestScanReconciliationStagingDropsPhysicalStateAndDiscardsThePooledSession(t *testing.T) {
	ctx, sourcePool, initial, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := initial.Close(ctx); err != nil {
		t.Fatalf("release the initial fixture owner: %v", err)
	}
	configuration := sourcePool.Config()
	configuration.MaxConns = 1
	singlePool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatalf("create the single-session reuse witness: %v", err)
	}
	libraryIntegrationPoolCleanup(t, singlePool)
	store, err := New(singlePool, &libraryFixtureProber{}, []string{allowedRoot})
	if err != nil {
		t.Fatalf("acquire the single reserved session: %v", err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Errorf("close the single-session owner: %v", err)
		}
	})
	ownerPID := scanStagingTestOwnerPID(t, ctx, store)
	for iteration := 0; iteration < 3; iteration++ {
		staging := scanStagingTestBegin(t, ctx, store, fmt.Sprintf("reuse-scan-%d", iteration), "reuse-library")
		if stats := staging.Stats(); stats.Rows != 0 || stats.SerializedBytes != 0 {
			t.Fatalf("a new pass inherited previously retired counters: %#v", stats)
		}
		scanStagingTestRecord(t, ctx, staging, scanStagingTestIDs(513, 32))
		if err := staging.Seal(ctx); err != nil {
			t.Fatalf("seal a pass after recreating its temp relations: %v", err)
		}
		if stats := staging.Stats(); stats.SessionPhysicalBytes <= 0 || stats.PeakSessionPhysicalBytes < stats.SessionPhysicalBytes {
			t.Fatalf("real temp relations were omitted from physical accounting: %#v", stats)
		}
		if err := staging.Close(); err != nil {
			t.Fatalf("retire the last pass's physical relations: %v", err)
		}
		if err := staging.Close(); err != nil {
			t.Fatalf("repeat a completed pass cleanup: %v", err)
		}
		if stats := staging.Stats(); stats.SessionPhysicalBytes != 0 || stats.PeakSessionPhysicalBytes <= 0 {
			t.Fatalf("dropping the last pass lost its physical cleanup or peak accounting: %#v", stats)
		}
		scanStagingTestRelationsAbsent(t, ctx, store)
		if actual := scanStagingTestOwnerPID(t, ctx, store); actual != ownerPID {
			t.Fatalf("normal scoped cleanup replaced the healthy owner: got %d, want %d", actual, ownerPID)
		}
	}
	if err := store.Close(ctx); err != nil {
		t.Fatalf("dispose of the session that held private state: %v", err)
	}
	borrower, err := singlePool.Acquire(ctx)
	if err != nil {
		t.Fatalf("borrow from the pool after owner shutdown: %v", err)
	}
	defer borrower.Release()
	var borrowerPID int32
	var seenAbsent, passAbsent bool
	if err := borrower.QueryRow(ctx, `SELECT pg_backend_pid(), to_regclass($1) IS NULL, to_regclass($2) IS NULL`,
		scanReconciliationSeenTable, scanReconciliationPassTable).Scan(&borrowerPID, &seenAbsent, &passAbsent); err != nil {
		t.Fatalf("inspect the subsequent general pool borrower: %v", err)
	}
	if borrowerPID == ownerPID || !seenAbsent || !passAbsent {
		t.Fatalf("a session that held temp staging returned to the pool: owner=%d, borrower=%d, seen absent=%t, pass absent=%t",
			ownerPID, borrowerPID, seenAbsent, passAbsent)
	}
}
