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

// The media package proves actual muxed packets. This fixture supplies a ready
// candidate directly so the catalog/publication transaction can independently
// prove user-state retention, stale intro projection and retirement ordering.
func TestMediaEditPublicationPreservesStateAndStalesSourceBoundIntro(t *testing.T) {
	fixture, capture, journal := mediaEditTestCapture(t)
	actor := identity.Principal{Kind: "admin", User: identity.User{ID: fixture.userID}}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT id FROM sessions WHERE user_id=$1 AND kind='admin' AND device_id='deletion-device'`, fixture.userID).Scan(&actor.SessionID); err != nil {
		t.Fatal(err)
	}
	original := *fixture.item.Media
	selected := highestEmbeddedStreamIndex(&original) + 1
	original.Streams = append(append([]media.Stream(nil), original.Streams...), media.Stream{Index: selected, CodecType: "subtitle", Codec: "subrip", IsTextSubtitleStream: true})
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=$2 WHERE id=$1`, fixture.item.ID, encoded); err != nil {
		t.Fatal(err)
	}
	op := MediaOperation{ID: journal.OperationID, Kind: MediaOperationRemoveSubtitle, State: "applying", ItemID: fixture.item.ID, LibraryID: fixture.library.ID, RootID: journal.Target.Root.ID, MediaSourceID: media.SourceID(fixture.item.ID), StreamIndex: selected, RequestActor: actor, ApplyActor: actor, PublicationPhase: "none", TargetPresent: true}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT `+MediaOperationSourceRevisionSQL+` FROM items i WHERE i.id=$1`, op.ItemID).Scan(&op.SourceRevision); err != nil {
		t.Fatal(err)
	}
	journal.SourceRevision, journal.StreamIndex, journal.Container = op.SourceRevision, selected, "mkv"
	journal.Target.Target.SubtitleIndex = selected
	journal.Host, err = mediaDeletionHostIdentity()
	if err != nil {
		t.Fatal(err)
	}
	journal.CandidateMedia = *fixture.item.Media
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
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO media_operations
		(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		 request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,stream_index,
		 parameters,source_snapshot,execution_snapshot,state,worker_token,result_summary,result_hash,journal,apply_actor_id,apply_credential_id)
		VALUES($1,$2,$3,$4,$5,$3,$4,$5,$6,$7,'publication-fixture',$8,$9,$10,$11,'{}','{}','{}','applying',$12,$13,$14,$15,$6,$7)`,
		op.ID, op.Kind, op.ItemID, op.LibraryID, op.RootID, actor.User.ID, actor.SessionID, make([]byte, 32), op.MediaSourceID, op.SourceRevision, op.StreamIndex, work.Token, result.Summary, result.ResultHash, result.Journal); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played) VALUES($1,$2,1234,7,true,true) ON CONFLICT(user_id,item_id) DO UPDATE SET playback_position_ticks=1234,play_count=7,is_favorite=true,played=true`, fixture.userID, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO item_intro_state(item_id,source_revision,start_ticks,end_ticks,provenance,last_edited_by) SELECT i.id,`+introSourceRevisionSQL+`,0,1,'Manual',$2 FROM items i WHERE i.id=$1`, fixture.item.ID, fixture.userID); err != nil {
		t.Fatal(err)
	}
	oldPlays := make(map[string]PlaybackOwner)
	retainedPlays := make(map[string]string)
	for _, state := range []string{"Prepared", "Playing", "Paused", "Stopped", "Expired", "OtherSource"} {
		owner := playSessionOwnerFixture(t, fixture.ctx, fixture.pool, fixture.userID, "edit-"+state)
		owner.PeerIP = "127.0.0.1"
		playID, playState, sourceID := "media-edit-play-"+state, state, op.MediaSourceID
		if state == "OtherSource" {
			playState = "Prepared"
			sourceID = "unrelated-source"
		}
		if _, err := fixture.pool.Exec(fixture.ctx, `INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,position_ticks,duration_ticks,expires_at,started_at,stopped_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,12,100,clock_timestamp()+interval '1 hour',
			CASE WHEN $7 IN ('Playing','Paused','Stopped','Expired') THEN clock_timestamp()-interval '10 minutes' ELSE NULL END,
			CASE WHEN $7 IN ('Paused','Stopped','Expired') THEN clock_timestamp()-interval '5 minutes' ELSE NULL END)`, playID, owner.UserID, owner.SessionID, owner.DeviceID, op.ItemID, sourceID, playState); err != nil {
			t.Fatal(err)
		}
		if state == "Prepared" || state == "Playing" || state == "Paused" {
			if _, err := fixture.store.GetPlaybackSession(fixture.ctx, owner, playID); err != nil {
				t.Fatalf("old live play was unusable before publication: %s %v", state, err)
			}
			oldPlays[playID] = owner
		} else {
			var snapshot string
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT to_jsonb(p)::text FROM play_sessions p WHERE id=$1`, playID).Scan(&snapshot); err != nil {
				t.Fatal(err)
			}
			retainedPlays[playID] = snapshot
		}
	}
	var pausedStop string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT stopped_at::text FROM play_sessions WHERE id='media-edit-play-Paused'`).Scan(&pausedStop); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, fixture.userID, fixture.item.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	retireCalls := 0
	result, err = fixture.store.ApplyEmbeddedSubtitleRemoval(fixture.ctx, work, func(ctx context.Context, item, source string) error {
		retireCalls++
		if item != op.ItemID || source != op.MediaSourceID {
			t.Fatal("retirement targeted another media source")
		}
		var phase, identity string
		if err := fixture.pool.QueryRow(ctx, `SELECT o.publication_phase,i.file_identity FROM media_operations o JOIN items i ON i.id=o.item_id WHERE o.id=$1`, op.ID).Scan(&phase, &identity); err != nil {
			return err
		}
		if phase != "catalog_committed" || identity != journal.Candidate.Identity {
			t.Fatal("retirement ran before catalog publication")
		}
		var active int
		if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM play_sessions WHERE item_id=$1 AND media_source_id=$2 AND state IN ('Prepared','Playing','Paused')`, op.ItemID, op.MediaSourceID).Scan(&active); err != nil {
			return err
		}
		if active != 0 {
			t.Fatal("old play IDs survived the source catalog transaction")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if retireCalls != 1 {
		t.Fatalf("retirement calls=%d", retireCalls)
	}
	for id, owner := range oldPlays {
		if _, err := fixture.store.GetPlaybackSession(fixture.ctx, owner, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("old play ID acquired the replacement source: %s %v", id, err)
		}
		var expired, stopped bool
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT state='Expired' AND expires_at<=clock_timestamp(),stopped_at IS NOT NULL FROM play_sessions WHERE id=$1`, id).Scan(&expired, &stopped); err != nil || !expired || !stopped {
			t.Fatalf("old play did not expire durably: %s %v", id, err)
		}
	}
	for id, before := range retainedPlays {
		var after string
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT to_jsonb(p)::text FROM play_sessions p WHERE id=$1`, id).Scan(&after); err != nil || before != after {
			t.Fatalf("terminal or unrelated-source play changed: %s %v", id, err)
		}
	}
	var stoppedAfter string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT stopped_at::text FROM play_sessions WHERE id='media-edit-play-Paused'`).Scan(&stoppedAfter); err != nil || stoppedAfter != pausedStop {
		t.Fatalf("existing stop timestamp was overwritten: %v", err)
	}
	var after, state, phase string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT to_jsonb(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id=$2`, fixture.userID, fixture.item.ID).Scan(&after); err != nil || before != after {
		t.Fatalf("user state changed: %v", err)
	}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT state,publication_phase FROM media_operations WHERE id=$1`, op.ID).Scan(&state, &phase); err != nil || state != "completed" || phase != "done" {
		t.Fatalf("publication state=%s/%s: %v", state, phase, err)
	}
	detail, err := fixture.store.GetItemIntro(fixture.ctx, actor, fixture.item.ID)
	if err != nil || !detail.OverrideStale || detail.Override == nil || detail.Effective != nil {
		t.Fatalf("source-bound intro inherited old source: %+v %v", detail, err)
	}
	stored, err := fixture.store.GetMediaOperation(fixture.ctx, actor, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeMediaEditJournal(stored); err != nil {
		t.Fatalf("JSONB roundtrip invalidated the ready witness: %v", err)
	}
	var summary mediaEditSummary
	if json.Unmarshal(result.Summary, &summary) != nil || !summary.BackupRetained {
		t.Fatal("published result omitted original retention")
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(fixture.path), journal.StageName, "payload")); err != nil || string(data) != fixture.contents {
		t.Fatal("successful catalog publication lost original bytes")
	}
}
