package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ThemeReservedItemSQL returns a complete boolean expression for an existing
// items alias in the caller's trusted schema search path. Path comparisons are
// exact, including case and literal LIKE metacharacters; a directory marker
// also hides its entire descendant subtree. The alias is an internal SQL alias,
// never an item identifier or a client-supplied fragment.
func ThemeReservedItemSQL(alias string) string {
	item, marker := pgx.Identifier{alias}.Sanitize(), pgx.Identifier{alias + "_theme_reserved"}.Sanitize()
	return `(EXISTS (SELECT 1 FROM theme_reserved_paths AS ` + marker +
		` WHERE ` + marker + `.root_id = ` + item + `.root_id AND (` + item + `.relative_path COLLATE "C" = ` + marker + `.relative_path
		OR (` + marker + `.is_directory AND left(` + item + `.relative_path, length(` + marker + `.relative_path) + 1) COLLATE "C"
		= ` + marker + `.relative_path || '/'))))`
}

// ThemeOrdinaryItemSQL excludes every resource association, including inactive
// ones, and every reserved path. A failed refresh must not republish a resource
// as an ordinary catalog item.
func ThemeOrdinaryItemSQL(alias string) string {
	item, link := pgx.Identifier{alias}.Sanitize(), pgx.Identifier{alias + "_theme_membership"}.Sanitize()
	return `(NOT EXISTS (SELECT 1 FROM item_theme_resources AS ` + link + ` WHERE ` + link + `.resource_item_id = ` + item + `.id)
		AND NOT ` + ThemeReservedItemSQL(alias) + `)`
}

// ThemeDirectItemSQL permits ordinary items or currently active, shape-valid
// theme resources. Authorization by the caller's user/library policy remains
// mandatory; this expression never grants an owner number a navigable alias.
func ThemeDirectItemSQL(alias string) string {
	return `(` + ThemeOrdinaryItemSQL(alias) + ` OR ` + themeResourceItemSQL(alias, true) + `)`
}

// Use one cross-row shape for direct reads and backup/restore validation.
// Inactive history retains its parent/library and reserved resource identity,
// even after its owner moves to another root or ceases to be ordinary. Those
// owner eligibility checks remain mandatory for every active association.
func themeResourceItemSQL(alias string, activeOnly bool) string {
	item := pgx.Identifier{alias}.Sanitize()
	link := pgx.Identifier{alias + "_theme_link"}.Sanitize()
	ownerAlias, rootAlias := alias+"_theme_owner", alias+"_theme_root"
	owner, root := pgx.Identifier{ownerAlias}.Sanitize(), pgx.Identifier{rootAlias}.Sanitize()
	active := ""
	if activeOnly {
		active = ` AND ` + link + `.active`
	}
	return `(EXISTS (SELECT 1 FROM item_theme_resources AS ` + link +
		` JOIN items AS ` + owner + ` ON ` + owner + `.id = ` + link + `.owner_item_id
		JOIN library_roots AS ` + root + ` ON ` + root + `.id = ` + item + `.root_id
		WHERE ` + link + `.resource_item_id = ` + item + `.id` + active +
		` AND ` + item + `.id <> ` + owner + `.id AND NOT ` + item + `.is_folder
		AND ((` + link + `.kind = 'song' AND ` + item + `.type = 'Audio')
		OR (` + link + `.kind = 'video' AND ` + item + `.type = 'Video'))
		AND ` + owner + `.library_id = ` + item + `.library_id AND ` + root + `.library_id = ` + item + `.library_id
		AND ` + item + `.parent_id = ` + owner + `.id
		AND (NOT ` + link + `.active OR ((` + owner + `.root_id = ` + item + `.root_id OR (` + owner + `.id = ` + item + `.library_id
		AND ` + owner + `.type = 'CollectionFolder' AND ` + owner + `.is_folder))
		AND ` + ThemeOrdinaryItemSQL(ownerAlias) + `)) AND ` + ThemeReservedItemSQL(alias) + `))`
}

// ErrThemeState means the schema's referential constraints do not suffice to
// prove complete owner mapping or valid cross-row resource classification.
var ErrThemeState = errors.New("incomplete or inconsistent theme state")

// ValidateThemeState reads the caller's transaction without allocating IDs or
// repairing rows. Callers must first verify the schema and trusted search path.
// Old archives retain their original schema semantics until migration reaches
// version 26; they must not query tables that did not exist in their source.
func ValidateThemeState(ctx context.Context, tx pgx.Tx, version int64) error {
	if version < 26 {
		return nil
	}
	if tx == nil {
		return errors.New("theme state validation requires a transaction")
	}
	var valid bool
	statement := `SELECT
		(SELECT count(*) FROM theme_owner_ids WHERE virtual_root AND item_id IS NULL) = 1
		AND (SELECT count(*) FROM theme_owner_ids) = (SELECT count(*) + 1 FROM items)
		AND NOT EXISTS (SELECT 1 FROM items AS mapped_item LEFT JOIN theme_owner_ids AS owner_id
		ON owner_id.item_id = mapped_item.id WHERE owner_id.id IS NULL)
		AND NOT EXISTS (SELECT 1 FROM item_theme_resources AS resource_link LEFT JOIN items AS resource_item
		ON resource_item.id = resource_link.resource_item_id WHERE resource_item.id IS NULL OR NOT ` + themeResourceItemSQL("resource_item", false) + `)`
	if err := tx.QueryRow(ctx, statement).Scan(&valid); err != nil {
		return fmt.Errorf("read theme semantic state: %w", err)
	}
	if !valid {
		return ErrThemeState
	}
	return nil
}
