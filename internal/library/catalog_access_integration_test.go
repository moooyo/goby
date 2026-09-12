package library

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"
)

func TestAllowedCatalogLibrariesUsesCurrentPolicyAndOnlyRequestedIDs(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	input := []string{"library-a", "library-b", "library-b", "historical-library"}
	all := map[string]bool{"library-a": true, "library-b": true, "historical-library": true}
	for _, userID := range []string{"default", "admin"} {
		assertAllowedCatalogLibraries(t, ctx, store, Subject{UserID: userID}, input, all)
	}
	subject := Subject{UserID: "restricted"}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, map[string]bool{"library-b": true})
	assertAllowedCatalogLibraries(t, ctx, store, Subject{UserID: "none"}, input, map[string]bool{})
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy =
		'{"EnableAllFolders":false,"EnabledFolders":["library-a","historical-library","policy-only","library-a"]}'
		WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, map[string]bool{"library-a": true, "historical-library": true})
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":[]}'
		WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, map[string]bool{})
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":true}' WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, all)
}

func TestAllowedCatalogLibrariesRetainsDeletedLibraryAuthority(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	subject := Subject{UserID: "restricted"}
	input := []string{"library-a", "library-b", "never-authorized-library"}
	want := map[string]bool{"library-b": true}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, want)
	if _, err := store.pool.Exec(ctx, "DELETE FROM libraries WHERE id = 'library-b'"); err != nil {
		t.Fatal(err)
	}
	var libraryExists, itemExists bool
	if err := store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM libraries WHERE id = 'library-b'),
		EXISTS(SELECT 1 FROM items WHERE library_id = 'library-b')`).Scan(&libraryExists, &itemExists); err != nil || libraryExists || itemExists {
		t.Fatalf("the deletion fixture still exists: library=%t, item=%t, error=%v", libraryExists, itemExists, err)
	}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, want)
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":["library-a"]}'
		WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	assertAllowedCatalogLibraries(t, ctx, store, subject, input, map[string]bool{"library-a": true})
}

func TestAllowedCatalogLibrariesEmptyInputStillChecksCurrentAuthority(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	for _, input := range [][]string{nil, {}, {"library-b"}} {
		for _, userID := range []string{"disabled", "missing-user", "malformed"} {
			if result, err := store.AllowedCatalogLibraries(ctx, Subject{UserID: userID}, input); !errors.Is(err, ErrForbidden) || result != nil {
				t.Fatalf("invalid user %q with input %v returned %+v, %v", userID, input, result, err)
			}
		}
	}
	for _, input := range [][]string{nil, {}} {
		assertAllowedCatalogLibraries(t, ctx, store, Subject{UserID: "default"}, input, map[string]bool{})
	}
	if _, err := store.pool.Exec(ctx, "UPDATE users SET is_disabled = true WHERE id = 'default'"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AllowedCatalogLibraries(ctx, Subject{UserID: "default"}, nil); !errors.Is(err, ErrForbidden) {
		t.Fatalf("empty input reused authority after user disablement: %v", err)
	}
}

func TestAllowedCatalogLibrariesUsesIndependentApplicationAuthorityAndTargetPolicy(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	key := seedCatalogApplicationKey(t, ctx, store.pool, "catalog-notification-key", true)
	input := []string{"library-a", "library-b", "deleted-library"}
	all := map[string]bool{"library-a": true, "library-b": true, "deleted-library": true}
	assertAllowedCatalogLibraries(t, ctx, store, key, input, all)
	target := Subject{UserID: "restricted", ApplicationCredentialID: key.ApplicationCredentialID}
	assertAllowedCatalogLibraries(t, ctx, store, target, input, map[string]bool{"library-b": true})
	libraryIntegrationUser(t, ctx, store.pool, "disabled-catalog-target", true, false, []string{"deleted-library", "not-requested"})
	disabledTarget := Subject{UserID: "disabled-catalog-target", ApplicationCredentialID: key.ApplicationCredentialID}
	assertAllowedCatalogLibraries(t, ctx, store, disabledTarget, input, map[string]bool{"deleted-library": true})
	assertAllowedCatalogLibraries(t, ctx, store, Subject{UserID: "admin", ApplicationCredentialID: key.ApplicationCredentialID}, input, all)
	if _, err := store.pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":["library-a"]}'
		WHERE id = 'restricted'`); err != nil {
		t.Fatal(err)
	}
	assertAllowedCatalogLibraries(t, ctx, store, target, input, map[string]bool{"library-a": true})
	assertAllowedCatalogLibraries(t, ctx, store, key, input, all)
	for _, requested := range [][]string{nil, input} {
		if result, err := store.AllowedCatalogLibraries(ctx,
			Subject{UserID: "missing-target", ApplicationCredentialID: key.ApplicationCredentialID}, requested); !errors.Is(err, ErrNotFound) || result != nil {
			t.Fatalf("missing application target returned %+v, %v", result, err)
		}
	}
	if _, err := store.pool.Exec(ctx, "UPDATE sessions SET revoked_at = clock_timestamp() WHERE id = $1", key.ApplicationCredentialID); err != nil {
		t.Fatal(err)
	}
	orphan := seedCatalogApplicationKey(t, ctx, store.pool, "orphan-catalog-notification-key", false)
	for _, subject := range []Subject{key, target, disabledTarget, orphan, {ApplicationCredentialID: "missing-key"}} {
		for _, requested := range [][]string{nil, {}, input} {
			if result, err := store.AllowedCatalogLibraries(ctx, subject, requested); !errors.Is(err, ErrForbidden) || result != nil {
				t.Fatalf("invalid application authority %+v with input %v returned %+v, %v", subject, requested, result, err)
			}
		}
	}
}

func TestAllowedCatalogLibrariesValidatesEveryBoundedIdentifier(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	subject := Subject{UserID: "default"}
	for _, invalid := range []string{"", " ", " leading", "trailing ", "edge\u00a0", "inner\x00value",
		"inner\tvalue", "inner\nvalue", "inner\x7fvalue", "inner\u0085value", string([]byte{0xff}), strings.Repeat("x", 257), strings.Repeat("\u754c", 86)} {
		if result, err := store.AllowedCatalogLibraries(ctx, subject, []string{"library-b", invalid}); !errors.Is(err, ErrInvalidInput) || result != nil {
			t.Fatalf("invalid library identifier %q returned %+v, %v", invalid, result, err)
		}
	}
	invalidSubjects := []Subject{{}, {UserID: " ", ApplicationCredentialID: "valid-key"}}
	for _, invalid := range []string{" ", " padded", "trailing\u00a0", "inner\x00value", "inner\nvalue", "inner\x7fvalue",
		string([]byte{0xff}), strings.Repeat("x", 257)} {
		invalidSubjects = append(invalidSubjects, Subject{UserID: invalid}, Subject{ApplicationCredentialID: invalid},
			Subject{UserID: invalid, ApplicationCredentialID: "valid-key"}, Subject{UserID: "default", ApplicationCredentialID: invalid})
	}
	for _, invalid := range invalidSubjects {
		if result, err := store.AllowedCatalogLibraries(ctx, invalid, nil); !errors.Is(err, ErrInvalidInput) || result != nil {
			t.Fatalf("invalid empty-input subject %+v returned %+v, %v", invalid, result, err)
		}
	}
	ids := make([]string, 4096)
	want := make(map[string]bool, len(ids))
	for index := range ids {
		ids[index] = fmt.Sprintf("historical-library-%04d", index)
	}
	ids[0], ids[len(ids)-1] = "library-\u754c", strings.Repeat("x", 256)
	for _, id := range ids {
		want[id] = true
	}
	assertAllowedCatalogLibraries(t, ctx, store, subject, ids, want)
	if result, err := store.AllowedCatalogLibraries(ctx, subject, append(ids, "one-too-many")); !errors.Is(err, ErrInvalidInput) || result != nil {
		t.Fatalf("over-limit library identifiers returned %d results, %v", len(result), err)
	}
	duplicates := make([]string, 4097)
	for index := range duplicates {
		duplicates[index] = "library-b"
	}
	if _, err := store.AllowedCatalogLibraries(ctx, subject, duplicates); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("deduplication bypassed the input count bound: %v", err)
	}
}

func assertAllowedCatalogLibraries(t *testing.T, ctx context.Context, store *Store, subject Subject, input []string, want map[string]bool) {
	t.Helper()
	got, err := store.AllowedCatalogLibraries(ctx, subject, input)
	if err != nil || got == nil || !maps.Equal(got, want) {
		t.Fatalf("allowed catalog libraries for %+v = %+v, %v; want %+v", subject, got, err, want)
	}
}
