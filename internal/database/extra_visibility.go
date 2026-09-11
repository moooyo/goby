package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ExtraReservedItemSQL matches exact registered-root reservations and their
// descendant directories. The alias is trusted internal SQL, never user input.
func ExtraReservedItemSQL(alias string) string {
	item, marker := pgx.Identifier{alias}.Sanitize(), pgx.Identifier{alias + "_extra_reserved"}.Sanitize()
	return `(EXISTS (SELECT 1 FROM extra_reserved_paths AS ` + marker +
		` WHERE ` + marker + `.root_id = ` + item + `.root_id AND (` + item + `.relative_path COLLATE "C" = ` + marker + `.relative_path
		OR (` + marker + `.is_directory AND left(` + item + `.relative_path, length(` + marker + `.relative_path) + 1) COLLATE "C"
		= ` + marker + `.relative_path || '/'))))`
}

// CatalogOrdinaryItemSQL excludes all permanent auxiliary associations and
// both reservation sets. Failed refreshes cannot expose historical resources
// through ordinary lists or folder aggregates.
func CatalogOrdinaryItemSQL(alias string) string {
	item, link := pgx.Identifier{alias}.Sanitize(), pgx.Identifier{alias + "_extra_membership"}.Sanitize()
	return `(` + ThemeOrdinaryItemSQL(alias) + ` AND NOT EXISTS (SELECT 1 FROM item_extra_resources AS ` + link +
		` WHERE ` + link + `.resource_item_id = ` + item + `.id) AND NOT ` + ExtraReservedItemSQL(alias) + `)`
}

// CatalogDirectItemSQL permits ordinary items and valid active resources.
// Callers must separately check current subject and library authorization.
func CatalogDirectItemSQL(alias string) string {
	return `(` + CatalogOrdinaryItemSQL(alias) + ` OR ` + ThemeResourceItemSQL(alias, true) +
		` OR ` + ExtraResourceItemSQL(alias, true) + `)`
}

// ThemeResourceItemSQL uses current-schema owner eligibility and permanently
// rejects resource IDs associated with extras, including inactive history.
func ThemeResourceItemSQL(alias string, activeOnly bool) string {
	return themeResourceItemSQLWithSemantics(alias, activeOnly, true)
}

// ExtraResourceItemSQL shares one shape between direct reads and archive
// validation. Inactive resources retain their parent, library and reserved
// identity while their owner may move or cease to be an ordinary Movie.
func ExtraResourceItemSQL(alias string, activeOnly bool) string {
	item := pgx.Identifier{alias}.Sanitize()
	link := pgx.Identifier{alias + "_extra_link"}.Sanitize()
	ownerAlias, rootAlias := alias+"_extra_owner", alias+"_extra_root"
	owner, root := pgx.Identifier{ownerAlias}.Sanitize(), pgx.Identifier{rootAlias}.Sanitize()
	theme := pgx.Identifier{alias + "_extra_theme_conflict"}.Sanitize()
	active := ""
	if activeOnly {
		active = ` AND ` + link + `.active`
	}
	return `(EXISTS (SELECT 1 FROM item_extra_resources AS ` + link +
		` JOIN items AS ` + owner + ` ON ` + owner + `.id = ` + link + `.owner_item_id
		JOIN library_roots AS ` + root + ` ON ` + root + `.id = ` + item + `.root_id
		WHERE ` + link + `.resource_item_id = ` + item + `.id` + active +
		` AND ` + item + `.id <> ` + owner + `.id AND NOT ` + item + `.is_folder AND ` + item + `.type = 'Video'
		AND ` + link + `.kind IN ('clip', 'deleted_scene', 'trailer')
		AND ` + item + `.parent_id = ` + owner + `.id
		AND ` + owner + `.library_id = ` + item + `.library_id AND ` + root + `.library_id = ` + item + `.library_id
		AND ` + item + `.relative_path <> '' AND position(chr(92) in ` + item + `.relative_path) = 0
		AND ` + item + `.relative_path !~ '^[A-Za-z]:'
		AND NOT (string_to_array(` + item + `.relative_path, '/') && ARRAY['', '.', '..'])
		AND (NOT ` + link + `.active OR (` + owner + `.root_id = ` + item + `.root_id
		AND ` + owner + `.type = 'Movie' AND NOT ` + owner + `.is_folder AND ` + CatalogOrdinaryItemSQL(ownerAlias) + `))
		AND ` + ExtraReservedItemSQL(alias) + ` AND NOT EXISTS (SELECT 1 FROM item_theme_resources AS ` + theme +
		` WHERE ` + theme + `.resource_item_id = ` + item + `.id)))`
}

// ErrExtraState reports an invalid cross-row attachment classification.
var ErrExtraState = errors.New("incomplete or inconsistent extra state")

// ValidateExtraState reads without repairing or allocating identities. Old
// archives must not query attachment tables before their schema reaches 27.
func ValidateExtraState(ctx context.Context, tx pgx.Tx, version int64) error {
	if version < 27 {
		return nil
	}
	if tx == nil {
		return errors.New("extra state validation requires a transaction")
	}
	var valid bool
	statement := `SELECT NOT EXISTS (SELECT 1 FROM item_extra_resources AS resource_link
		LEFT JOIN items AS resource_item ON resource_item.id = resource_link.resource_item_id
		WHERE resource_item.id IS NULL OR NOT ` + ExtraResourceItemSQL("resource_item", false) + `)`
	if err := tx.QueryRow(ctx, statement).Scan(&valid); err != nil {
		return fmt.Errorf("read extra semantic state: %w", err)
	}
	if !valid {
		return ErrExtraState
	}
	return nil
}
