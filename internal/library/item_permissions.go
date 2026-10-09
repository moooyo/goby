package library

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

// ItemPermission is the minimal direct-item projection used by remote control.
// CanPlay preserves GetItemFor semantics, including visible folders and virtual
// expected episodes; it does not test media availability or begin playback.
type ItemPermission struct {
	CanPlay bool
}

// ItemPermissionAuthority records only the catalog authority actually observed
// by a completed permission query. It is not a credential or a reusable grant.
// Delivery must independently revalidate its original trusted credential.
type ItemPermissionAuthority struct {
	valid         bool
	userID        string
	credentialID  string
	administrator bool
	policy        identity.ManagedPolicy
}

// MatchesPrincipal compares a freshly revalidated controller with the item
// query's authority snapshot. A key with a selected account has separate target
// policy and cannot be compared with its userless principal through this API.
func (authority ItemPermissionAuthority) MatchesPrincipal(principal identity.Principal) bool {
	if !authority.valid {
		return false
	}
	if authority.credentialID != "" {
		return authority.userID == "" && principal.IsApplicationKey() && principal.SessionID == authority.credentialID
	}
	if (principal.Kind != "emby" && principal.Kind != "admin") || principal.User.IsDisabled ||
		principal.User.ID != authority.userID || principal.User.IsAdministrator != authority.administrator {
		return false
	}
	policy, err := identity.ParseRuntimePolicy(principal.User.Policy)
	return err == nil && reflect.DeepEqual(policy, authority.policy)
}

// ItemPermissionsFor returns visible identifiers in one current authorization
// snapshot. Callers must check coverage of every requested ID; missing entries
// are indistinguishable from inaccessible items.
func (s *Store) ItemPermissionsFor(ctx context.Context, subject Subject, ids []string) (map[string]ItemPermission, error) {
	return s.itemPermissionsFor(ctx, subject, ids, true, nil)
}

// ItemPermissionsWithAuthorityFor also returns the policy snapshot that selected
// the items, so a later delivery check can reject an intervening policy change.
func (s *Store) ItemPermissionsWithAuthorityFor(ctx context.Context, subject Subject, ids []string) (map[string]ItemPermission, ItemPermissionAuthority, error) {
	var authority ItemPermissionAuthority
	items, err := s.itemPermissionsFor(ctx, subject, ids, true, &authority)
	return items, authority, err
}

// StoredItemPermissionsFor applies the same direct-item predicate as
// GetItemsByIDFor without loading presentation fields. It queries only stored
// items, so a notification cannot turn a virtual expected episode into a
// currently existing catalog item. Expected-looking stored IDs remain literal.
func (s *Store) StoredItemPermissionsFor(ctx context.Context, subject Subject, ids []string) (map[string]ItemPermission, error) {
	return s.itemPermissionsFor(ctx, subject, ids, false, nil)
}

func (s *Store) itemPermissionsFor(ctx context.Context, subject Subject, ids []string, includeExpected bool, authority *ItemPermissionAuthority) (map[string]ItemPermission, error) {
	if len(ids) == 0 || len(ids) > 1000 {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]bool, len(ids))
	var physical, expected []string
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 256 || !utf8.ValidString(id) || strings.ContainsRune(id, '\x00') {
			return nil, ErrInvalidInput
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if includeExpected && IsExpectedEpisodeID(id) {
			expected = append(expected, id)
		} else {
			physical = append(physical, id)
		}
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	result := make(map[string]ItemPermission, len(seen))
	read := func(statement string, canPlay bool, args ...any) error {
		rows, err := tx.Query(ctx, statement, args...)
		if err != nil {
			return fmt.Errorf("query direct item permissions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return fmt.Errorf("scan direct item permission: %w", err)
			}
			result[id] = ItemPermission{CanPlay: canPlay}
		}
		return rows.Err()
	}
	if len(physical) != 0 {
		if err := read(`SELECT i.id FROM items i WHERE i.id=ANY($1::text[])
			AND ($2::boolean OR i.library_id=ANY($3::text[]) OR i.library_id=`+policySQLString(collectionLibraryID)+`)
			AND `+access.directSQL("i"), access.canPlay, physical, access.all, access.folders); err != nil {
			return nil, err
		}
	}
	if len(expected) != 0 {
		if err := read(`SELECT i.id FROM (`+expectedEpisodeItemsFilteredSQL(access, "e.id=ANY($1::text[])")+`) i
			WHERE i.id=ANY($1::text[]) AND `+access.itemPolicySQL("i"), false, expected); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete direct item permissions: %w", err)
	}
	if authority != nil {
		*authority = ItemPermissionAuthority{valid: true, userID: access.userID, credentialID: subject.ApplicationCredentialID,
			administrator: access.administrator, policy: access.policy}
	}
	return result, nil
}
