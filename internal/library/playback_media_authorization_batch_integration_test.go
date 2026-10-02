//go:build linux

package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestPlaybackMediaAuthorizationBatchRejectsSourceBeforeWaitingForPlay(t *testing.T) {
	for _, sourceState := range []string{"busy", "invalid-json", "invalid-probe", "invalid-source-id"} {
		t.Run(sourceState, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			want, sourceID := error(ErrUnavailable), play.MediaSourceID
			switch sourceState {
			case "busy":
				publicationReadTestReservation(t, fixture, "prepared")
				want = ErrBusy
			case "invalid-json":
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media='{"Container":17}' WHERE id=$1`, fixture.item.ID); err != nil {
					t.Fatal(err)
				}
			case "invalid-probe":
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET media=jsonb_set(media,'{ProbeVersion}','0') WHERE id=$1`, fixture.item.ID); err != nil {
					t.Fatal(err)
				}
			case "invalid-source-id":
				sourceID, want = "not-the-indexed-source", ErrNotFound
			}
			barrier, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(barrier)
			var locked string
			if err := barrier.QueryRow(fixture.ctx, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE", play.ID).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
			defer cancel()
			_, _, err = fixture.store.readPlaybackMediaAuthorization(ctx, principal, playbackMediaOwner(principal), play.ID, fixture.item.ID, sourceID, false)
			if !errors.Is(err, want) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				t.Fatalf("invalid/busy source waited on the play row or lost its primary error: error=%v want=%v", err, want)
			}
		})
	}
}

func TestPlaybackMediaAuthorizationBatchRejectsCredentialBeforeWaitingForItem(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	principal, play := playbackMediaTestPrincipal(t, fixture, false)
	if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", principal.SessionID); err != nil {
		t.Fatal(err)
	}
	barrier, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(barrier)
	var locked string
	if err := barrier.QueryRow(fixture.ctx, "SELECT id FROM items WHERE id=$1 FOR UPDATE", fixture.item.ID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
	defer cancel()
	_, _, err = fixture.store.readPlaybackMediaAuthorization(ctx, principal, playbackMediaOwner(principal), play.ID, fixture.item.ID, play.MediaSourceID, false)
	if !errors.Is(err, identity.ErrUnauthorized) || ctx.Err() != nil {
		t.Fatalf("revoked credential waited on the item before rejection: %v", err)
	}
}
