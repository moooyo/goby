package library

const scanReconciliationDescendantPredicate = `(i.parent_id=ANY($1::text[])
	OR theme.owner_item_id=ANY($1::text[]) OR extra.owner_item_id=ANY($1::text[]))`

// Discover keys through each existing edge index before projecting complete
// item facts. Scope, visibility and active status must not filter a cascading
// edge. The original predicate remains after key discovery so PostgreSQL's
// lock-wait recheck still rejects a child whose parent edge moved away.
// Do not limit the key set before LockRows: a lock-wait recheck can remove rows,
// and the outer limit must continue to count matching, locked item facts.
func scanReconciliationDescendantQuery() string {
	return `WITH descendant_keys AS MATERIALIZED (
		SELECT id FROM items WHERE parent_id=ANY($1::text[])
		UNION SELECT resource_item_id FROM item_theme_resources WHERE owner_item_id=ANY($1::text[])
		UNION SELECT resource_item_id FROM item_extra_resources WHERE owner_item_id=ANY($1::text[])
	)
	SELECT ` + scanReconciliationItemColumns + `
	FROM descendant_keys keys JOIN items i ON i.id=keys.id
	LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
	LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
	WHERE ` + scanReconciliationDescendantPredicate + `
	ORDER BY i.id LIMIT $2 FOR UPDATE OF i`
}
