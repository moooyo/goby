package library

// Television references follow at most two actual parent edges in the same
// authorized catalog snapshot. A valid direct Season survives an invalid
// Series edge. Ordinary folders are never traversed or guessed from indexes.
var itemTVParentsColumn = `CASE WHEN (i.type = 'Episode' AND NOT i.is_folder)
	OR (i.type = 'Season' AND i.is_folder) THEN (
	SELECT jsonb_build_object(
		'Season', CASE WHEN i.type = 'Episode' AND tv_parent.type = 'Season'
			THEN jsonb_build_object('ID', tv_parent.id, 'Name', tv_parent.name) END,
		'Series', CASE WHEN tv_parent.type = 'Series'
			THEN jsonb_build_object('ID', tv_parent.id, 'Name', tv_parent.name)
			WHEN tv_series.id IS NOT NULL
			THEN jsonb_build_object('ID', tv_series.id, 'Name', tv_series.name) END)
	FROM items tv_parent
	LEFT JOIN items tv_series ON i.type = 'Episode' AND tv_parent.type = 'Season'
		AND tv_series.id = tv_parent.parent_id AND tv_series.library_id = i.library_id
		AND tv_series.id <> i.id AND tv_series.id <> tv_parent.id
		AND tv_series.type = 'Series' AND tv_series.is_folder
		AND ` + ordinaryItemSQL("tv_series") + `
	WHERE tv_parent.id = i.parent_id AND tv_parent.library_id = i.library_id
		AND tv_parent.id <> i.id AND tv_parent.is_folder
		AND (tv_parent.type = 'Series' OR (i.type = 'Episode' AND tv_parent.type = 'Season'))
		AND ` + ordinaryItemSQL("tv_parent") + `
) END`
