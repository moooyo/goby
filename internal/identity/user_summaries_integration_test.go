package identity_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestStoreListUserSummariesPreservesNativeFieldsAndCompleteLists(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	empty, err := store.ListUserSummaries(ctx)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty user summaries did not retain an empty array: %v", err)
	}
	for index, fixture := range []struct {
		name     string
		password string
		admin    bool
		disabled bool
	}{
		{"zulu Summary", "summary-password", true, false},
		{"ALPHA Summary", "", false, false},
		{"bravo Summary", "summary-password", false, true},
	} {
		user, err := store.CreateUser(ctx, fixture.name, fixture.password, fixture.admin)
		if err != nil {
			t.Fatal(err)
		}
		created := time.Date(2021, time.March, 12, 18, 25, index, 123456000, time.FixedZone("fixture", 8*60*60))
		if _, err := pool.Exec(ctx, `UPDATE users SET is_disabled=$2, created_at=$3,
			policy=jsonb_build_object('IsHidden',true,'SummaryMarker',name,'OpaquePayload',$4::text),
			configuration=jsonb_build_object('SummaryMarker',name,'OpaquePayload',$4::text),
			local_password_hash=password_hash, profile_pin_ciphertext=decode(repeat('01',36),'hex')
			WHERE id=$1`, user.ID, fixture.disabled, created, strings.Repeat(fixture.name, 1024)); err != nil {
			t.Fatal(err)
		}
	}
	complete, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"ALPHA Summary", "bravo Summary", "zulu Summary"}
	assertSummaries := func(want []identity.User) {
		t.Helper()
		summaries, err := store.ListUserSummaries(ctx)
		if err != nil || summaries == nil || len(summaries) != len(want) {
			t.Fatalf("user summary list omitted an account: %v", err)
		}
		for index, full := range want {
			if len(full.Policy) < 1024 || len(full.Configuration) < 1024 || !full.HasLocalPassword || !full.HasProfilePin {
				t.Fatal("complete user list lost policy, configuration or local credential state")
			}
			expected := identity.User{
				ID: full.ID, Name: full.Name, IsAdministrator: full.IsAdministrator,
				IsDisabled: full.IsDisabled, HasPassword: full.HasPassword, CreatedAt: full.CreatedAt,
			}
			if !reflect.DeepEqual(summaries[index], expected) {
				t.Fatalf("summary changed a native field or loaded an unused snapshot at index %d", index)
			}
		}
	}
	if len(complete) != len(wantNames) {
		t.Fatalf("complete user list returned %d accounts, want %d", len(complete), len(wantNames))
	}
	for index, user := range complete {
		if user.Name != wantNames[index] {
			t.Fatalf("user ordering changed at index %d", index)
		}
	}
	assertSummaries(complete)
	// An isolated schema can exercise the explicit ID tie-breaker even though
	// ordinary account admission enforces unique normalized names.
	if _, err := pool.Exec(ctx, `ALTER TABLE users DROP CONSTRAINT users_normalized_name_key;
		UPDATE users SET normalized_name='summary-tie'`); err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(complete, func(a, b identity.User) int { return strings.Compare(a.ID, b.ID) })
	assertSummaries(complete)
}
