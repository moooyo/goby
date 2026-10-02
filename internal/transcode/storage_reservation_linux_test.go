//go:build linux

package transcode

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func storageTestJournalRoot(t *testing.T) (string, string) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("the optional storage journal belongs to the root broker")
	}
	root := filepath.Join(t.TempDir(), "ledger")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	token := storageTestID(900)
	if err := os.WriteFile(filepath.Join(root, storageJournalMarker), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, token
}

func TestStorageReservationJournalOwnsExclusiveDurableRecovery(t *testing.T) {
	root, token := storageTestJournalRoot(t)
	journal, err := openStorageReservationJournal(root, token)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newStorageReservationLedger(storageTestLimits(2), journal)
	if err != nil {
		t.Fatal(err)
	}
	storageTestLease(t, ledger, 1)
	if second, err := openStorageReservationJournal(root, token); second != nil || !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("a second owner bypassed the broker journal lock: %v", err)
	}
	if err := ledger.close(); err != nil {
		t.Fatal(err)
	}
	journal, err = openStorageReservationJournal(root, token)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := newStorageReservationLedger(storageTestLimits(2), journal)
	if err != nil {
		_ = journal.close()
		t.Fatal(err)
	}
	defer reopened.close()
	if snapshot := reopened.snapshot(); snapshot.Leases != 1 || !snapshot.RecoveryRequired {
		t.Fatalf("durable reservations disappeared after lock handoff: %+v", snapshot)
	}
	if _, err := reopened.reserve(storageTestID(2), storageTestID(999)); !errors.Is(err, ErrStorageRecoveryRequired) {
		t.Fatalf("opening the ledger bypassed recovery: %v", err)
	}
}

func TestStorageReservationJournalRejectsStagedOrUnknownOwnedContent(t *testing.T) {
	for _, name := range []string{storageJournalNext, "foreign-payload"} {
		t.Run(name, func(t *testing.T) {
			root, token := storageTestJournalRoot(t)
			if err := os.WriteFile(filepath.Join(root, name), []byte("incomplete"), 0o600); err != nil {
				t.Fatal(err)
			}
			journal, err := openStorageReservationJournal(root, token)
			if journal != nil {
				_ = journal.close()
				t.Fatal("an incomplete or foreign inode was accepted")
			}
			if name == storageJournalNext && !errors.Is(err, ErrStorageRecoveryRequired) {
				t.Fatalf("staged transaction did not require recovery: %v", err)
			}
			if name != storageJournalNext && !errors.Is(err, ErrStorageUnsafe) {
				t.Fatalf("foreign content did not fail closed: %v", err)
			}
		})
	}
}

func TestStorageReservationJournalRejectsLinksOwnershipAndTokenChanges(t *testing.T) {
	for _, mutation := range []string{"root-mode", "marker-hardlink", "marker-symlink", "wrong-token"} {
		t.Run(mutation, func(t *testing.T) {
			root, token := storageTestJournalRoot(t)
			marker := filepath.Join(root, storageJournalMarker)
			switch mutation {
			case "root-mode":
				if err := os.Chmod(root, 0o770); err != nil {
					t.Fatal(err)
				}
			case "marker-hardlink":
				if err := os.Link(marker, filepath.Join(t.TempDir(), "linked-marker")); err != nil {
					t.Fatal(err)
				}
			case "marker-symlink":
				target := filepath.Join(t.TempDir(), "marker-target")
				if err := os.Rename(marker, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, marker); err != nil {
					t.Fatal(err)
				}
			case "wrong-token":
				token = storageTestID(901)
			}
			journal, err := openStorageReservationJournal(root, token)
			if journal != nil {
				_ = journal.close()
				t.Fatal("unsafe owner boundary was accepted")
			}
			if !errors.Is(err, ErrStorageUnsafe) {
				t.Fatalf("unsafe ownership did not fail closed: %v", err)
			}
		})
	}
}

func TestStorageReservationJournalRejectsOversizedAndUnrecognizedSchema(t *testing.T) {
	for _, payload := range [][]byte{[]byte("{\"unrecognized\":1}"), []byte("{} {}"), make([]byte, maxStorageJournalBytes+1)} {
		root, token := storageTestJournalRoot(t)
		if err := os.WriteFile(filepath.Join(root, storageJournalName), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		journal, err := openStorageReservationJournal(root, token)
		if err != nil {
			t.Fatal(err)
		}
		_, _, loadErr := journal.load()
		if err := journal.close(); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(loadErr, ErrStorageUnsafe) {
			t.Fatalf("malformed durable ownership was accepted: %v", loadErr)
		}
	}
}

func TestStorageReservationJournalDoesNotInitializeMissingMarker(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the optional storage journal belongs to the root broker")
	}
	root := filepath.Join(t.TempDir(), "unowned")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := openStorageReservationJournal(root, storageTestID(900))
	if journal != nil {
		_ = journal.close()
		t.Fatal("an unmarked directory was initialized automatically")
	}
	if !errors.Is(err, ErrStorageUnsafe) {
		t.Fatalf("missing explicit setup did not fail closed: %v", err)
	}
	names, err := os.ReadDir(root)
	if err != nil || len(names) != 0 {
		t.Fatalf("the failed prerequisite created owned state: entries=%d error=%v", len(names), err)
	}
}

func TestStorageReservationJournalRejectsRootAndLockIdentityDrift(t *testing.T) {
	for _, mutation := range []string{"root", "lock", "marker"} {
		t.Run(mutation, func(t *testing.T) {
			root, token := storageTestJournalRoot(t)
			journal, err := openStorageReservationJournal(root, token)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.close()
			switch mutation {
			case "root":
				moved := filepath.Join(filepath.Dir(root), "original-ledger")
				if err := os.Rename(root, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(root, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, storageJournalMarker), []byte(token+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "lock":
				if err := os.Rename(filepath.Join(root, storageJournalLock), filepath.Join(t.TempDir(), "original-lock")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, storageJournalLock), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "marker":
				if err := os.WriteFile(filepath.Join(root, storageJournalMarker), []byte(storageTestID(901)+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := journal.load(); !errors.Is(err, ErrStorageUnsafe) {
				t.Fatalf("owner identity drift bypassed the held journal authority: %v", err)
			}
		})
	}
}

func TestStorageReservationJournalCannotBackTwoCachedLedgers(t *testing.T) {
	root, token := storageTestJournalRoot(t)
	journal, err := openStorageReservationJournal(root, token)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := newStorageReservationLedger(storageTestLimits(1), journal)
	if err != nil {
		_ = journal.close()
		t.Fatal(err)
	}
	defer ledger.close()
	if second, err := newStorageReservationLedger(storageTestLimits(1), journal); second != nil || !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("one locked journal supplied two independent capacity caches: %v", err)
	}
	storageTestLease(t, ledger, 1)
	if snapshot := ledger.snapshot(); snapshot.Leases != 1 || snapshot.AvailableBytes != 0 {
		t.Fatalf("rejected reuse disturbed the original ledger: %+v", snapshot)
	}
}
