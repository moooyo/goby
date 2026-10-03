package library

import (
	"fmt"

	"github.com/moooyo/goby/internal/storagebinding"
)

// The materialized dependencies retain the existing row-lock order. Initial
// statement-snapshot lookups choose rows only; every accepting predicate uses
// values returned by FOR UPDATE. At Read Committed, PostgreSQL rechecks a row
// changed while its lock was awaited. Missing rows, changed associations, and
// unsuccessful observations fall back after this implicit transaction ends.
const primaryScanManualAuthoritySQL = `/* primary_scan_authority */
WITH job_route AS MATERIALIZED (
	SELECT id, COALESCE(task_child_id, '') AS child_id
	FROM scan_jobs WHERE id = $1
), locked_job AS MATERIALIZED (
	SELECT j.id, j.library_id, j.status, j.force_probe, j.cancel_requested,
		COALESCE(j.task_child_id, '') AS child_id
	FROM scan_jobs j JOIN job_route route ON j.id = route.id
	WHERE route.child_id = $3 AND $3 = ''
	FOR UPDATE OF j
), authorized_job AS MATERIALIZED (
	SELECT library_id FROM locked_job
	WHERE id = $1 AND library_id = $2 AND child_id = $3
		AND status = 'Running' AND force_probe = $4 AND NOT cancel_requested
)
` + primaryScanAuthorityRootSQL

const primaryScanTaskAuthoritySQL = `/* primary_scan_authority */
WITH job_route AS MATERIALIZED (
	SELECT id, COALESCE(task_child_id, '') AS child_id
	FROM scan_jobs WHERE id = $1
), child_route AS MATERIALIZED (
	SELECT c.id, c.run_id, route.id AS job_id
	FROM task_run_children c JOIN job_route route ON c.id = route.child_id
	WHERE route.child_id = $3 AND $3 <> ''
), locked_run AS MATERIALIZED (
	SELECT r.id, r.state, r.task_key, route.id AS child_id, route.job_id
	FROM task_runs r JOIN child_route route ON r.id = route.run_id
	FOR UPDATE OF r
), locked_child AS MATERIALIZED (
	SELECT c.id, c.library_id, c.state, COALESCE(c.scan_job_id, '') AS scan_id,
		r.state AS run_state, r.task_key AS run_key, r.job_id
	FROM task_run_children c JOIN locked_run r ON c.run_id = r.id AND c.id = r.child_id
	FOR UPDATE OF c
), locked_job AS MATERIALIZED (
	SELECT j.id, j.library_id, j.status, j.force_probe, j.cancel_requested,
		COALESCE(j.task_child_id, '') AS child_id, c.id AS locked_child_id,
		c.library_id AS child_library_id, c.state AS child_state, c.scan_id,
		c.run_state, c.run_key
	FROM scan_jobs j JOIN locked_child c ON j.id = c.job_id
	FOR UPDATE OF j
), authorized_job AS MATERIALIZED (
	SELECT library_id FROM locked_job
	WHERE id = $1 AND library_id = $2 AND child_id = $3
		AND status = 'Running' AND force_probe = $4 AND NOT cancel_requested
		AND locked_child_id = child_id AND child_library_id = library_id AND scan_id = id
		AND run_state IN ('pending', 'running') AND child_state = 'running'
		AND ((run_key = 'library.scan' AND NOT force_probe)
			OR (run_key = 'library.refresh_media' AND force_probe))
)
` + primaryScanAuthorityRootSQL

// Keep this projection identical to readRootBindingScanRow, including bounded
// persisted documents and metadata. The root lock follows all authority locks.
const primaryScanAuthorityRootSQL = `SELECT ` + rootBindingMetadataColumns + `,
	r.storage_binding IS NOT NULL,
	CASE WHEN octet_length(r.storage_binding::text) <= $6 THEN r.storage_binding::text END,
	r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
	FROM library_roots r JOIN authorized_job authority ON r.library_id = authority.library_id
	WHERE r.id = $5 FOR UPDATE OF r`

func (input *primaryScanRead) readAuthorityInOneRequest() (rootBindingRow, bool, error) {
	state := input.state
	statement := primaryScanManualAuthoritySQL
	if state.task.job.TaskChildID != "" {
		statement = primaryScanTaskAuthoritySQL
	}
	rows, err := state.store.pool.Query(input.work, statement, state.task.job.ID,
		state.task.job.LibraryID, state.task.job.TaskChildID, state.task.job.ForceProbe,
		state.root.id, storagebinding.MaxDocumentBytes)
	if err != nil {
		return rootBindingRow{}, false, fmt.Errorf("read scan authority: %w", err)
	}
	defer rows.Close()
	var row rootBindingRow
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
			&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy); err != nil {
			return rootBindingRow{}, false, fmt.Errorf("read scan root binding: %w", err)
		}
	}
	// Exhausting pgx rows consumes ReadyForQuery and releases the pooled
	// connection. Neither I/O nor the fallback may begin with these locks held.
	if err := rows.Err(); err != nil {
		return rootBindingRow{}, false, fmt.Errorf("complete scan authority: %w", err)
	}
	if err := input.work.Err(); err != nil {
		return rootBindingRow{}, false, err
	}
	if count != 1 || row.root != state.root || row.validateMapping() != nil {
		return rootBindingRow{}, false, nil
	}
	return row, true, nil
}
