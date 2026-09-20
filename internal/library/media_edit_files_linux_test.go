//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestMediaEditCreateCandidateCompensatesPostOpenFailures(t *testing.T) {
	for _, point := range []string{"stat", "stage_sync", "parent_sync"} {
		t.Run(point, func(t *testing.T) {
			fixture, _, journal := mediaEditTestCapture(t)
			capture, err := fixture.store.openMediaEditCapture(fixture.ctx, journal.source().File, ".goby-edit-"+strings.Repeat("c", 32), "", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer capture.Close()
			injected := errors.New("injected candidate creation failure")
			stat := func(file *os.File) (os.FileInfo, error) {
				if point == "stat" {
					return nil, injected
				}
				return file.Stat()
			}
			syncCalls := 0
			syncDirectory := func(directory *os.Root) error {
				syncCalls++
				if point == "stage_sync" && syncCalls == 1 || point == "parent_sync" && syncCalls == 2 {
					return injected
				}
				return fileDeletionSyncDirectory(directory)
			}
			if file, err := capture.createCandidateWithIO(fixture.ctx, stat, syncDirectory); file != nil || !errors.Is(err, injected) || errors.Is(err, ErrMediaOperationRecovery) {
				t.Fatalf("creation failure was not safely compensated: file=%v err=%v", file, err)
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(fixture.path), capture.base.spec.StageName, "payload")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("post-open failure left candidate: %v", err)
			}
			if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
				t.Fatal("creation compensation modified source")
			}
		})
	}
}

func TestMediaEditCreateCandidateRefusesUnprovenReplacementAndRetainsWitness(t *testing.T) {
	fixture, _, journal := mediaEditTestCapture(t)
	operation := MediaOperation{ID: strings.Repeat("d", 32), Kind: MediaOperationRemoveSubtitle, State: "running", SourceRevision: journal.SourceRevision, ItemID: fixture.item.ID, LibraryID: fixture.library.ID, RootID: journal.Target.Root.ID, MediaSourceID: media.SourceID(fixture.item.ID)}
	capture, err := fixture.store.openMediaEditCapture(fixture.ctx, journal.source().File, ".goby-edit-"+operation.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	injected := errors.New("injected directory synchronization failure")
	stagePath := filepath.Join(filepath.Dir(fixture.path), capture.base.spec.StageName)
	syncDirectory := func(*os.Root) error {
		if err := os.Rename(filepath.Join(stagePath, "payload"), filepath.Join(stagePath, "retained-candidate")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stagePath, "payload"), []byte("unrelated replacement"), 0600); err != nil {
			t.Fatal(err)
		}
		return injected
	}
	if _, err := capture.createCandidateWithIO(fixture.ctx, func(file *os.File) (os.FileInfo, error) { return file.Stat() }, syncDirectory); !errors.Is(err, injected) || !errors.Is(err, ErrMediaOperationRecovery) {
		t.Fatalf("unproven cleanup was not retained: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(stagePath, "payload")); err != nil || string(data) != "unrelated replacement" {
		t.Fatal("cleanup unlinked a replacement")
	}
	if _, err := os.Stat(filepath.Join(stagePath, "retained-candidate")); err != nil {
		t.Fatal("cleanup removed its renamed object")
	}
	result, err := incompleteMediaEditResult(operation, journal.source(), strings.Repeat("a", 64), capture)
	if err != nil || len(result.ResultHash) != 64 {
		t.Fatalf("missing failed-creation witness: %v", err)
	}
	var witness map[string]any
	if json.Unmarshal(result.Journal, &witness) != nil || witness["Version"] != "media-edit-incomplete-staging-v1" || witness["StageIdentity"] != capture.base.stageIdentity || witness["CandidateIdentity"] != fileIdentity(capture.candidateInfo) {
		t.Fatal("failed-creation witness lost its captured identities")
	}
	if _, err := decodeMediaEditJournal(MediaOperation{ID: operation.ID, Journal: result.Journal, ResultHash: result.ResultHash}); !errors.Is(err, ErrMediaOperationRecovery) {
		t.Fatal("incomplete generation was accepted as a publishable candidate")
	}
	var credential string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT id FROM sessions WHERE user_id=$1 AND kind='admin' AND device_id='deletion-device'`, fixture.userID).Scan(&credential); err != nil {
		t.Fatal(err)
	}
	work := MediaOperationWork{Operation: operation, Token: strings.Repeat("e", 32)}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO media_operations
		(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,request_actor_id,request_credential_id,
		 request_id,request_fingerprint,media_source_id,source_revision,stream_index,parameters,source_snapshot,execution_snapshot,state,worker_token)
		VALUES($1,$2,$3,$4,$5,$3,$4,$5,$6,$7,'failed-create-fixture',$8,$9,$10,0,'{}','{}','{}','running',$11)`, operation.ID, operation.Kind, operation.ItemID, operation.LibraryID, operation.RootID, fixture.userID, credential, make([]byte, 32), operation.MediaSourceID, operation.SourceRevision, work.Token); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.retainIncompleteMediaEdit(fixture.ctx, work, result); err != nil {
		t.Fatal(err)
	}
	stored, err := readMediaOperation(fixture.ctx, fixture.pool, operation.ID, false)
	var summary struct{ RequiresManualInspection, GenerationComplete bool }
	if err != nil || stored.ResultHash != result.ResultHash || json.Unmarshal(stored.Journal, &witness) != nil || witness["StageIdentity"] != capture.base.stageIdentity || json.Unmarshal(stored.ResultSummary, &summary) != nil || !summary.RequiresManualInspection || summary.GenerationComplete {
		t.Fatalf("incomplete staging witness was not durable: %v", err)
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
		t.Fatal("failed creation modified source")
	}
}

func mediaEditTestCapture(t *testing.T) (mediaSourceFixture, *mediaEditCapture, mediaEditJournal) {
	t.Helper()
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET is_administrator=true WHERE id=$1`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET kind='admin' WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	actor.Kind = "admin"
	op := MediaOperation{ID: strings.Repeat("a", 32), ItemID: fixture.item.ID, MediaSourceID: media.SourceID(fixture.item.ID), RequestActor: actor, ApplyActor: actor}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT i.library_id,i.root_id,`+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1`, op.ItemID).Scan(&op.LibraryID, &op.RootID, &op.SourceRevision); err != nil {
		t.Fatal(err)
	}
	// The descriptor tests exercise filesystem publication without launching
	// media tools; the actual remux integration suite proves packet preservation.
	tx, err := fixture.store.beginMetadataRead(fixture.ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	source, err := readIndexedMediaSource(fixture.ctx, tx, unrestrictedLibraryAccess(), op.ItemID, op.MediaSourceID)
	if err != nil {
		rollback(tx)
		t.Fatal(err)
	}
	var bindingRevision int64
	if err := tx.QueryRow(fixture.ctx, `SELECT binding_revision FROM library_roots WHERE id=$1`, op.RootID).Scan(&bindingRevision); err != nil {
		rollback(tx)
		t.Fatal(err)
	}
	target := fileDeletionTarget{Kind: "media", ItemID: op.ItemID, LibraryID: op.LibraryID, ParentID: source.mediaFile.Item.ParentID, BindingRevision: bindingRevision, SourceTag: source.mediaFile.ETag, File: fileDeletionSpec{Root: source.root, RelativePath: source.relativePath, Identity: source.identity, Size: source.mediaFile.Size, ModifiedAt: source.mediaFile.ModifiedAt, ChangeTimeNs: source.mediaFile.Item.Media.FileChangeTimeNs}}
	target.BindingFingerprint, err = readDeletionBindingFingerprint(fixture.ctx, tx, target)
	rollback(tx)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := fixture.store.openMediaEditCapture(fixture.ctx, target.File, ".goby-edit-"+op.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capture.Close() })
	file, err := capture.createCandidate(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("candidate with one subtitle removed"); err != nil {
		t.Fatal(err)
	}
	attributes, err := mediaEditCopyAttributes(capture.base.source, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := mediaEditDigest(fixture.ctx, file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest, err := mediaEditDigest(fixture.ctx, capture.base.source, target.File.Size)
	if err != nil {
		t.Fatal(err)
	}
	r := target.File.Root
	journal := mediaEditJournal{Version: 1, OperationID: op.ID, StageName: capture.base.spec.StageName, StageIdentity: capture.base.stageIdentity, SourceRevision: op.SourceRevision, SourceSHA256: sourceDigest, AttributeSHA256: attributes, Candidate: mediaEditFileInfo(info, digest), Target: deletionTargetDocument{Target: target, Root: deletionRootDocument{r.id, r.libraryID, r.path, r.allowedPath, r.relativePath}}}
	return fixture, capture, journal
}

func TestMediaEditExchangeRetainsOriginalAndRecoveryOnlyObserves(t *testing.T) {
	fixture, capture, journal := mediaEditTestCapture(t)
	if _, err := capture.publish(fixture.ctx, journal.Candidate); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != "candidate with one subtitle removed" {
		t.Fatalf("published source=%q, error=%v", data, err)
	}
	backup := filepath.Join(filepath.Dir(fixture.path), journal.StageName, "payload")
	if data, err := os.ReadFile(backup); err != nil || string(data) != fixture.contents {
		t.Fatalf("original was not retained: %q, %v", data, err)
	}
	if err := capture.discardUnpublished(fixture.ctx); !errors.Is(err, ErrMediaOperationRecovery) {
		t.Fatalf("published backup could be discarded: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, published, err := fixture.store.openMediaEditPublication(fixture.ctx, journal, "prepared")
	if err != nil || !published {
		t.Fatalf("prepared observation=%t, %v", published, err)
	}
	defer recovered.Close()
	if err := verifyMediaEditDigests(fixture.ctx, recovered, journal, true); err != nil {
		t.Fatal(err)
	}
	if _, err := recovered.publish(fixture.ctx, journal.Candidate); err == nil {
		t.Fatal("recovery performed a second exchange")
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != fixture.contents {
		t.Fatal("read-only recovery changed original")
	}
}

func TestMediaEditRejectsCandidateTamperingBeforePublication(t *testing.T) {
	fixture, capture, journal := mediaEditTestCapture(t)
	if _, err := capture.candidate.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.publish(fixture.ctx, journal.Candidate); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("changed candidate accepted: %v", err)
	}
	if capture.attempted {
		t.Fatal("exchange attempted for changed candidate")
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
		t.Fatal("source changed on rejected candidate")
	}
}

func TestMediaEditCancellationAndPartialCandidateCleanupNeverModifySource(t *testing.T) {
	fixture, capture, journal := mediaEditTestCapture(t)
	ctx, cancel := context.WithCancel(fixture.ctx)
	cancel()
	if _, err := capture.publish(ctx, journal.Candidate); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publication=%v", err)
	}
	if capture.attempted {
		t.Fatal("cancelled publication attempted exchange")
	}
	if err := capture.discardUnpublished(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.path), journal.StageName, "payload")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("candidate cleanup=%v", err)
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
		t.Fatal("candidate cleanup modified source")
	}
}

func TestMediaEditReplacementAndMissingBackupAreNeverRecoveredAutomatically(t *testing.T) {
	fixture, capture, journal := mediaEditTestCapture(t)
	if _, err := capture.publish(fixture.ctx, journal.Candidate); err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(filepath.Dir(fixture.path), journal.StageName, "payload")
	if err := os.Rename(backup, backup+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("unrelated replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if recovered, _, err := fixture.store.openMediaEditPublication(fixture.ctx, journal, "prepared"); err == nil {
		recovered.Close()
		t.Fatal("replacement backup accepted")
	}
	if data, err := os.ReadFile(backup); err != nil || string(data) != "unrelated replacement" {
		t.Fatal("recovery modified replacement")
	}
	if data, err := os.ReadFile(backup + ".retained"); err != nil || string(data) != fixture.contents {
		t.Fatal("recovery modified retained original")
	}
}

func TestMediaEditSourceLookupRequiresNativeAdministrator(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	tx, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	for _, subject := range []identity.Principal{actor, {Kind: "admin", User: actor.User, SessionID: actor.SessionID}} {
		if _, _, err := readMediaEditTarget(fixture.ctx, tx, subject, MediaOperation{ItemID: fixture.item.ID}); !errors.Is(err, ErrForbidden) {
			t.Fatalf("non-admin media edit accepted: %v", err)
		}
	}
}
