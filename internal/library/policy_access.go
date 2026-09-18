package library

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

func unrestrictedLibraryAccess() libraryAccess {
	return libraryAccess{all: true, administrator: true, folders: []string{}, canPlay: true}
}

func readUserLibraryAccess(ctx context.Context, tx pgx.Tx, userID string) (libraryAccess, error) {
	var administrator bool
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT is_administrator, policy FROM users WHERE id=$1", userID).Scan(&administrator, &raw); err != nil {
		return libraryAccess{}, fmt.Errorf("read current aggregate policy: %w", err)
	}
	access, err := parseLibraryPolicy(raw)
	if err != nil {
		return libraryAccess{}, err
	}
	access.userID, access.administrator = userID, administrator
	if administrator {
		access.all = true
	}
	return access, nil
}

// policySQLString encodes data as an explicit PostgreSQL escape string. Policy
// predicates never concatenate an unescaped account value into executable SQL.
func policySQLString(value string) string {
	return "E'" + strings.NewReplacer(`\`, `\\`, "'", "''").Replace(value) + "'"
}

func policySQLArray(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = policySQLString(value)
	}
	return "ARRAY[" + strings.Join(quoted, ",") + "]::text[]"
}

// itemPolicySQL is the single item authorization predicate used before counts,
// paging, projections, user-state writes, and opening any media resource. Only
// package-owned SQL aliases may be supplied. A collection's own ACL never grants
// access to the source items it references.
func (access libraryAccess) itemPolicySQL(alias string) string {
	conditions := []string{collectionAccessSQL(alias, access.userID, access.administrator)}
	if !access.policy.AllowsFeature(identity.FeaturePlaylists) {
		conditions = append(conditions, alias+".type <> 'Playlist'")
	}
	if !access.policy.AllowsFeature(identity.FeatureCollections) {
		conditions = append(conditions, alias+".type <> 'BoxSet'")
	}
	if !access.all {
		conditions = append(conditions, "("+alias+".library_id="+policySQLString(collectionLibraryID)+" OR "+alias+".library_id=ANY("+policySQLArray(access.folders)+"))")
	}
	policy := access.policy
	if len(policy.ExcludedSubFolders) == 0 && policy.MaxParentalRating == nil && len(policy.BlockUnratedItems) == 0 && len(policy.BlockedTags) == 0 && len(policy.IncludeTags) == 0 {
		return "(" + strings.Join(conditions, " AND ") + ")"
	}
	// The visited set makes corrupt cycles terminate. Auxiliary resources inherit
	// their ordinary owner's restrictions through the same parent relationship.
	ancestry := `WITH RECURSIVE policy_ancestors AS (
		SELECT ` + alias + `.id, ` + alias + `.parent_id, ` + alias + `.library_id, ` + alias + `.path, 0 AS depth, ARRAY[` + alias + `.id] AS visited
		UNION ALL SELECT parent.id, parent.parent_id, parent.library_id, parent.path, child.depth+1, child.visited||parent.id
		FROM policy_ancestors child JOIN items parent ON parent.id=child.parent_id AND parent.library_id=child.library_id
		WHERE NOT parent.id=ANY(child.visited)
	) `
	if len(policy.ExcludedSubFolders) != 0 {
		excluded := policySQLArray(policy.ExcludedSubFolders)
		conditions = append(conditions, "NOT EXISTS ("+ancestry+"SELECT 1 FROM policy_ancestors WHERE id=ANY("+excluded+") OR path=ANY("+excluded+"))")
	}
	tagMatch := func(tags []string) string {
		return "EXISTS (" + ancestry + `SELECT 1 FROM policy_ancestors ancestor
			JOIN item_entities association ON association.item_id=ancestor.id
			JOIN catalog_entities tag ON tag.id=association.entity_id AND tag.kind='Tag'
			WHERE tag.normalized_name IN (SELECT lower(btrim(value)) FROM unnest(` + policySQLArray(tags) + ") AS allowed(value)))"
	}
	// Explicit block lists are denials. Inclusive mode changes BlockedTags into
	// the allow list used by older clients; IncludeTags is the current allow list.
	if len(policy.BlockedTags) != 0 && !policy.IsTagBlockingModeInclusive {
		conditions = append(conditions, "NOT "+tagMatch(policy.BlockedTags))
	}
	allowTags := append([]string{}, policy.IncludeTags...)
	if policy.IsTagBlockingModeInclusive {
		allowTags = append(allowTags, policy.BlockedTags...)
	}
	ratingConditions := []string{}
	rating := "(" + ancestry + `SELECT upper(btrim(COALESCE(metadata.effective, item.local_metadata)->>'OfficialRating'))
		FROM policy_ancestors ancestor JOIN items item ON item.id=ancestor.id
		LEFT JOIN item_metadata_state metadata ON metadata.item_id=item.id
		WHERE NULLIF(btrim(COALESCE(metadata.effective,item.local_metadata)->>'OfficialRating'),'') IS NOT NULL
		ORDER BY ancestor.depth LIMIT 1)`
	if policy.MaxParentalRating != nil {
		// Values are pinned by metadata-m5b-metadata-editor.json. Unknown rating
		// vocabularies cannot silently bypass a configured parental ceiling.
		value := "CASE " + rating + ` WHEN 'TV-Y' THEN 1 WHEN 'APPROVED' THEN 1 WHEN 'G' THEN 1 WHEN 'E' THEN 1 WHEN 'EC' THEN 1 WHEN 'TV-G' THEN 1
			WHEN 'TV-Y7' THEN 3 WHEN 'TV-Y7-FV' THEN 4 WHEN 'PG' THEN 5 WHEN 'TV-PG' THEN 5 WHEN 'PG-13' THEN 7 WHEN 'T' THEN 7
			WHEN 'TV-14' THEN 8 WHEN 'R' THEN 9 WHEN 'M' THEN 9 WHEN 'TV-MA' THEN 9 WHEN 'NC-17' THEN 10
			WHEN 'AO' THEN 15 WHEN 'RP' THEN 15 WHEN 'UR' THEN 15 WHEN 'X' THEN 15 WHEN 'XXX' THEN 15 ELSE NULL END`
		ratingConditions = append(ratingConditions, "("+rating+" IS NULL OR "+value+fmt.Sprintf(" <= %d)", *policy.MaxParentalRating))
	}
	if len(policy.BlockUnratedItems) != 0 {
		category := "CASE WHEN " + alias + `.type='Movie' THEN 'Movie' WHEN ` + alias + `.type IN ('Series','Season','Episode') THEN 'Series'
			WHEN ` + alias + `.type IN ('Audio','MusicAlbum','MusicArtist','MusicVideo') THEN 'Music'
			WHEN EXISTS (SELECT 1 FROM item_extra_resources extra WHERE extra.resource_item_id=` + alias + `.id AND extra.active AND extra.kind='trailer') THEN 'Trailer' ELSE 'Other' END`
		ratingConditions = append(ratingConditions, "(("+rating+" IS NOT NULL AND "+rating+" NOT IN ('UR','NR','UNRATED','NOT RATED')) OR NOT ("+category+")=ANY("+policySQLArray(policy.BlockUnratedItems)+"))")
	}
	content := strings.Join(ratingConditions, " AND ")
	if len(allowTags) != 0 {
		tags := tagMatch(allowTags)
		if content == "" {
			content = tags
		} else if policy.AllowTagOrRating {
			content = "(" + content + ") OR " + tags
		} else {
			content = "(" + content + ") AND " + tags
		}
	}
	if content != "" {
		// Organizational folders stay navigable; their children are independently
		// filtered. A Series or MusicAlbum is itself subject to content policy.
		conditions = append(conditions, "("+alias+".type IN ('CollectionFolder','Folder','Playlist','BoxSet') OR ("+content+"))")
	}
	return "(" + strings.Join(conditions, " AND ") + ")"
}

func (access libraryAccess) ordinarySQL(alias string) string {
	return "(" + ordinaryItemSQL(alias) + " AND " + access.itemPolicySQL(alias) + ")"
}

func (access libraryAccess) directSQL(alias string) string {
	return "(" + directItemSQL(alias) + " AND " + access.itemPolicySQL(alias) + ` AND NOT EXISTS (
		SELECT 1 FROM item_extra_resources policy_extra JOIN items policy_owner ON policy_owner.id=policy_extra.owner_item_id
		WHERE policy_extra.resource_item_id=` + alias + `.id AND policy_extra.active AND NOT ` + access.itemPolicySQL("policy_owner") + `) AND NOT EXISTS (
		SELECT 1 FROM item_theme_resources policy_theme JOIN items policy_owner ON policy_owner.id=policy_theme.owner_item_id
		WHERE policy_theme.resource_item_id=` + alias + `.id AND policy_theme.active AND NOT ` + access.itemPolicySQL("policy_owner") + "))"
}

// scopeSQL also protects nested parent projections and folder count subqueries.
func (access libraryAccess) scopeSQL(statement string) string {
	for _, alias := range []string{"child", "parent", "tv_series", "tv_parent", "leaf"} {
		statement = strings.ReplaceAll(statement, ordinaryItemSQL(alias), access.ordinarySQL(alias))
	}
	return statement
}

func (access libraryAccess) itemColumnsSQL() string {
	return access.scopeSQL(itemColumns)
}

func parseLibraryPolicy(data []byte) (libraryAccess, error) {
	policy, err := identity.ParseRuntimePolicy(data)
	if err != nil {
		return libraryAccess{}, ErrForbidden
	}
	return libraryAccess{all: policy.EnableAllFolders, folders: append([]string{}, policy.EnabledFolders...), canPlay: policy.EnableMediaPlayback && policy.AllowsFeature(identity.FeaturePlayback), policy: policy}, nil
}
