package library

import (
	"errors"
	"testing"
)

func TestApplicationKeyStateWritesIgnoreTargetContentPolicyButReadsRetainIt(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	subject := seedCatalogApplicationKey(t, ctx, store.pool, "independent-state-policy-key", true)
	subject.UserID = "restricted"
	if _, err := store.pool.Exec(ctx, `UPDATE items SET local_metadata='{"OfficialRating":"TV-MA","Tags":["Adults"]}' WHERE id='series-b';
		SELECT sync_catalog_item_entities(id,local_metadata) FROM items WHERE id='series-b'`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, policy string
	}{
		{"excluded ancestor", `{"EnableAllFolders":true,"ExcludedSubFolders":["season-b"],"RestrictedFeatures":["goby_playback"]}`},
		{"inherited parental rating", `{"EnableAllFolders":true,"MaxParentalRating":5,"EnableMediaPlayback":false}`},
		{"inherited blocked tag", `{"EnableAllFolders":true,"BlockedTags":["Adults"]}`},
		{"no granted folders", `{"EnableAllFolders":false,"EnabledFolders":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.pool.Exec(ctx, `UPDATE users SET policy=$1::jsonb WHERE id=$2`, test.policy, subject.UserID); err != nil {
				t.Fatal(err)
			}
			if _, err := store.pool.Exec(ctx, `DELETE FROM user_item_data WHERE user_id=$1`, subject.UserID); err != nil {
				t.Fatal(err)
			}
			favorite, err := store.SetFavoriteFor(ctx, subject, "episode-b1", true)
			if err != nil || !favorite.IsFavorite {
				t.Fatalf("server credential lost independent favorite authority: %+v, %v", favorite, err)
			}
			played, err := store.SetPlayedFor(ctx, subject, "season-b", true, nil)
			if err != nil || !played.Played || played.UnplayedItemCount == nil || *played.UnplayedItemCount != 0 {
				t.Fatalf("server credential lost descendant write or response authority: %+v, %v", played, err)
			}
			var children, otherOwners int
			if err := store.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE user_id=$1 AND item_id IN ('episode-b1','episode-b2') AND played),
				count(*) FILTER (WHERE user_id<>$1) FROM user_item_data`, subject.UserID).Scan(&children, &otherOwners); err != nil || children != 2 || otherOwners != 0 {
				t.Fatalf("state mutation escaped ownership or omitted hidden children: children=%d other=%d error=%v", children, otherOwners, err)
			}
			if _, err := store.GetUserDataFor(ctx, subject, "episode-b1"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("state write widened later target-scoped reads: %v", err)
			}
			data, err := store.GetUserDataBatchFor(ctx, subject, []string{"episode-b1", "episode-b2"})
			if err != nil || len(data) != 0 {
				t.Fatalf("target-scoped batch disclosed restricted state: %+v, %v", data, err)
			}
			page, err := store.QueryItems(ctx, Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID,
				Recursive: true, IncludeItemTypes: []string{"Episode"}, Ids: []string{"episode-b1", "episode-b2"}})
			if err != nil || page.TotalRecordCount != 0 || len(page.Items) != 0 {
				t.Fatalf("target-scoped catalog ignored content policy: %+v, %v", page, err)
			}
			if _, err := store.UserDataNotificationPage(ctx, UserDataNotificationQuery{UserID: subject.UserID,
				ApplicationCredentialID: subject.ApplicationCredentialID, ItemID: "episode-b1"}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("state notification bypassed target visibility: %v", err)
			}
			if _, err := store.SetFavorite(ctx, subject.UserID, "episode-b1", false); !errors.Is(err, ErrNotFound) {
				t.Fatalf("ordinary account borrowed the key's write authority: %v", err)
			}
			if _, err := store.SetPlayed(ctx, subject.UserID, "season-b", false, nil); !errors.Is(err, ErrNotFound) {
				t.Fatalf("ordinary folder mutation bypassed content policy: %v", err)
			}
		})
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, subject.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetFavoriteFor(ctx, subject, "episode-b1", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked server credential retained independent write authority: %v", err)
	}
}

func TestApplicationKeyStateWritesDoNotParseTargetPolicyAsAuthority(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	subject := seedCatalogApplicationKey(t, ctx, store.pool, "malformed-target-state-key", true)
	subject.UserID = "malformed"
	if favorite, err := store.SetFavoriteFor(ctx, subject, "episode-b1", true); err != nil || !favorite.IsFavorite {
		t.Fatalf("target policy was used to authorize a server favorite write: %+v, %v", favorite, err)
	}
	if played, err := store.SetPlayedFor(ctx, subject, "season-b", true, nil); err != nil || !played.Played {
		t.Fatalf("target policy was reparsed before committing the folder mutation: %+v, %v", played, err)
	}
	if _, err := store.GetUserDataFor(ctx, subject, "episode-b1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("malformed target policy stopped failing closed for reads: %v", err)
	}
	subject.UserID = "missing-target"
	if _, err := store.SetFavoriteFor(ctx, subject, "episode-b1", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("independent authority invented a target account: %v", err)
	}
}
