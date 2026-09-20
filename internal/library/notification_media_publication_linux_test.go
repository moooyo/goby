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
	"github.com/moooyo/goby/internal/notificationjournal"
)

// This uses the existing real filesystem exchange fixture. It does not repeat
// remux proof: it verifies that a new journal admission failure cannot publish
// half a catalog transaction or erase the already retained original inode.
func TestNotificationCapacityAfterMediaExchangeRetainsPreparedRecoveryBarrier(t *testing.T) {
	f, capture, journal := mediaEditTestCapture(t)
	actor := identity.Principal{Kind: "admin", User: identity.User{ID: f.userID}}
	if f.pool.QueryRow(f.ctx, `SELECT id FROM sessions WHERE user_id=$1 AND kind='admin' AND device_id='deletion-device'`, f.userID).Scan(&actor.SessionID) != nil {
		t.Fatal("read media edit actor")
	}
	original := *f.item.Media
	selected := highestEmbeddedStreamIndex(&original) + 1
	original.Streams = append(append([]media.Stream{}, original.Streams...), media.Stream{Index: selected, CodecType: "subtitle", Codec: "subrip", IsTextSubtitleStream: true})
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(f.ctx, `UPDATE items SET media=$2 WHERE id=$1`, f.item.ID, raw); err != nil {
		t.Fatal(err)
	}
	op := MediaOperation{ID: journal.OperationID, Kind: MediaOperationRemoveSubtitle, State: "applying", ItemID: f.item.ID, LibraryID: f.library.ID, RootID: journal.Target.Root.ID, MediaSourceID: media.SourceID(f.item.ID), StreamIndex: selected, RequestActor: actor, ApplyActor: actor, PublicationPhase: "none", TargetPresent: true}
	if f.pool.QueryRow(f.ctx, `SELECT `+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1`, op.ItemID).Scan(&op.SourceRevision) != nil {
		t.Fatal("read source revision")
	}
	journal.SourceRevision, journal.StreamIndex, journal.Container = op.SourceRevision, selected, "mkv"
	journal.Target.Target.SubtitleIndex = selected
	journal.Host, err = mediaDeletionHostIdentity()
	if err != nil {
		t.Fatal(err)
	}
	journal.CandidateMedia = *f.item.Media
	journal.CandidateMedia.Size, journal.CandidateMedia.FileChangeTimeNs = journal.Candidate.Size, journal.Candidate.ChangeTimeNs
	journal.ReadyHash, err = mediaEditReadyHash(journal)
	if err != nil {
		t.Fatal(err)
	}
	result, err := encodeMediaEditResult(journal)
	if err != nil {
		t.Fatal(err)
	}
	op.Journal, op.ResultHash = result.Journal, result.ResultHash
	work := MediaOperationWork{Operation: op, Token: strings.Repeat("b", 32), Apply: true}
	if _, err = f.pool.Exec(f.ctx, `INSERT INTO media_operations(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,parameters,source_snapshot,execution_snapshot,state,worker_token,result_summary,result_hash,journal,apply_actor_id,apply_credential_id)
	VALUES($1,$2,$3,$4,$5,$3,$4,$5,$6,$7,'notification-capacity-fixture',$8,$9,$10,$11,'{}','{}','{}','applying',$12,$13,$14,$15,$6,$7)`, op.ID, op.Kind, op.ItemID, op.LibraryID, op.RootID, actor.User.ID, actor.SessionID, make([]byte, 32), op.MediaSourceID, op.SourceRevision, op.StreamIndex, work.Token, result.Summary, result.ResultHash, result.Journal); err != nil {
		t.Fatal(err)
	}
	owner := playSessionOwnerFixture(t, f.ctx, f.pool, f.userID, "notification-media-capacity")
	seed, err := f.store.beginOwnedTx(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(seed)
	// pgx prepares parameterized statements individually. Keep the admission
	// state atomic without combining multiple SQL commands in one prepared call.
	if _, err = seed.Exec(f.ctx, `UPDATE notification_transport SET enabled=true,endpoint='https://receiver.invalid/events',credential_ciphertext=$1 WHERE id=1`, make([]byte, 48)); err != nil {
		t.Fatal(err)
	}
	if _, err = seed.Exec(f.ctx, `INSERT INTO notification_registrations(id,session_id,user_id,device_id,peer_ip,event_ids,token_ciphertext) VALUES($1,$2,$3,$4,'',ARRAY['CatalogInvalidated'],$5)`, strings.Repeat("c", 32), owner.SessionID, owner.UserID, owner.DeviceID, make([]byte, 48)); err != nil {
		t.Fatal(err)
	}
	if _, err = seed.Exec(f.ctx, `UPDATE notification_journal_state SET sequence=512`); err != nil {
		t.Fatal(err)
	}
	if _, err = seed.Exec(f.ctx, `INSERT INTO notification_source_events(id,sequence,kind,refs) SELECT md5('notification-media-capacity-'||n::text),n,'CatalogInvalidated',jsonb_build_array(jsonb_build_object('Kind','Item','Id',$1::text,'LibraryId',$2::text)) FROM generate_series(1,512)n`, op.ItemID, op.LibraryID); err != nil {
		t.Fatal(err)
	}
	if err = seed.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	var before string
	if f.pool.QueryRow(f.ctx, `SELECT file_identity FROM items WHERE id=$1`, op.ItemID).Scan(&before) != nil {
		t.Fatal("read original catalog identity")
	}
	if err = capture.Close(); err != nil {
		t.Fatal(err)
	}
	retired := false
	_, err = f.store.ApplyEmbeddedSubtitleRemoval(f.ctx, work, func(context.Context, string, string) error { retired = true; return nil })
	if !errors.Is(err, notificationjournal.ErrCapacity) || !errors.Is(err, ErrMediaOperationRecovery) || retired {
		t.Fatalf("capacity did not preserve recovery classification and retirement ordering: %v", err)
	}
	var phase, after string
	if f.pool.QueryRow(f.ctx, `SELECT o.publication_phase,i.file_identity FROM media_operations o JOIN items i ON i.id=o.item_id WHERE o.id=$1`, op.ID).Scan(&phase, &after) != nil || phase != "prepared" || after != before {
		t.Fatal("failed catalog publication lost the prepared barrier or changed indexed identity")
	}
	if content, err := os.ReadFile(filepath.Join(filepath.Dir(f.path), journal.StageName, "payload")); err != nil || string(content) != f.contents {
		t.Fatal("journal capacity failure erased the original backup")
	}
	if content, err := os.ReadFile(f.path); err != nil || string(content) != "candidate with one subtitle removed" {
		t.Fatal("fixture did not reach the actual exchange before failure")
	}
	file, _, err := f.store.OpenMedia(f.ctx, f.userID, op.ItemID, op.MediaSourceID)
	if file != nil {
		file.Close()
		t.Fatal("prepared source was opened")
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("failed publication no longer blocks new readers: %v", err)
	}
	if _, err = f.pool.Exec(f.ctx, `UPDATE notification_registrations SET source_cursor=512`); err != nil {
		t.Fatal(err)
	}
	if f.pool.QueryRow(f.ctx, `SELECT publication_phase FROM media_operations WHERE id=$1`, op.ID).Scan(&phase) != nil || phase != "prepared" {
		t.Fatal("freeing notification capacity implicitly replayed filesystem publication")
	}
}
