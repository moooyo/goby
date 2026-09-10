package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Store reads committed task state through its pool and performs every write
// through the same fenced transaction owner used by the scanner.
type Store struct {
	pool  *pgxpool.Pool
	owner library.OwnedTransactions
}

func New(pool *pgxpool.Pool, owner library.OwnedTransactions) (*Store, error) {
	if pool == nil || owner == nil {
		return nil, fmt.Errorf("%w: task pool and transaction owner are required", ErrInvalidInput)
	}
	return &Store{pool: pool, owner: owner}, nil
}

// Reconcile registers only executable definitions and never creates a run or
// schedule. Administrator choices and existing definition identities survive.
func (s *Store) Reconcile(ctx context.Context) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	return s.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		_, err := tx.Exec(`INSERT INTO task_definitions
            (id, key, emby_key, name, description, category)
            VALUES ($1,$2,$3,'Scan media library','Scan all registered media libraries.','Library')
            ON CONFLICT (key) DO UPDATE SET emby_key = EXCLUDED.emby_key,
                name = EXCLUDED.name, description = EXCLUDED.description,
                category = EXCLUDED.category, updated_at = clock_timestamp()
            WHERE (task_definitions.emby_key, task_definitions.name,
                task_definitions.description, task_definitions.category)
                IS DISTINCT FROM (EXCLUDED.emby_key, EXCLUDED.name,
                    EXCLUDED.description, EXCLUDED.category)`, id, LibraryScanKey, LibraryScanEmbyKey)
		if err != nil {
			return fmt.Errorf("register library task: %w", err)
		}
		_, err = tx.Exec(`UPDATE task_definitions SET enabled = false,
            revision = revision + 1, updated_at = clock_timestamp()
            WHERE key <> $1 AND enabled`, LibraryScanKey)
		if err != nil {
			return fmt.Errorf("disable unavailable task definitions: %w", err)
		}
		return nil
	})
}

// Each projection is one database statement, so its definition, active run,
// last result, and triggers share one MVCC snapshot.
const definitionProjection = `to_jsonb(d) || jsonb_build_object(
    'triggers', COALESCE((SELECT jsonb_agg(to_jsonb(t) ORDER BY t.position, t.id)
        FROM task_triggers t WHERE t.task_id = d.id AND t.retired_at IS NULL), '[]'::jsonb),
    'current_run', (SELECT to_jsonb(r) - 'request_fingerprint' FROM task_runs r
        WHERE r.task_id = d.id AND r.state IN ('pending','running','stopping')),
    'last_run', (SELECT to_jsonb(r) - 'request_fingerprint' FROM task_runs r
        WHERE r.task_id = d.id AND r.finished_at IS NOT NULL
        ORDER BY r.finished_at DESC, r.id DESC LIMIT 1))`

func (s *Store) List(ctx context.Context, options ListOptions) ([]Definition, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(projection ORDER BY name, id), '[]'::jsonb)
        FROM (SELECT d.id, d.name, `+definitionProjection+` AS projection FROM task_definitions d
            WHERE ($1::boolean IS NULL OR d.is_hidden = $1)
                AND ($2::boolean IS NULL OR d.enabled = $2)) q`, options.IsHidden, options.IsEnabled).Scan(&raw)
	if err != nil {
		return nil, fmt.Errorf("list task definitions: %w", err)
	}
	var definitions []Definition
	if err := json.Unmarshal(raw, &definitions); err != nil {
		return nil, fmt.Errorf("decode task definitions: %w", err)
	}
	for index := range definitions {
		normalizeDefinition(&definitions[index])
	}
	return definitions, nil
}

func (s *Store) Get(ctx context.Context, id string) (Definition, error) {
	var definition Definition
	err := decodeRow(s.pool.QueryRow(ctx, "SELECT "+definitionProjection+" FROM task_definitions d WHERE d.id = $1", id), &definition)
	return definition, err
}

func (s *Store) GetByKey(ctx context.Context, key string) (Definition, error) {
	var definition Definition
	err := decodeRow(s.pool.QueryRow(ctx, "SELECT "+definitionProjection+" FROM task_definitions d WHERE d.key = $1", key), &definition)
	return definition, err
}

func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	var run Run
	err := decodeRow(s.pool.QueryRow(ctx, `SELECT to_jsonb(r) - 'request_fingerprint'
        FROM task_runs r WHERE id = $1`, id), &run)
	return run, err
}

func (s *Store) ListRuns(ctx context.Context, taskID string, page Page) (RunPage, error) {
	page, err := normalizePage(page)
	if err != nil {
		return RunPage{}, err
	}
	result := RunPage{Items: []Run{}, StartIndex: page.StartIndex, Limit: page.Limit}
	var exists bool
	var raw []byte
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_definitions WHERE id = $1),
        (SELECT count(*) FROM task_runs WHERE task_id = $1),
        COALESCE((SELECT jsonb_agg(to_jsonb(p) - 'request_fingerprint' ORDER BY p.created_at DESC, p.id DESC)
            FROM (SELECT * FROM task_runs WHERE task_id = $1 ORDER BY created_at DESC, id DESC
                LIMIT $2 OFFSET $3) p), '[]'::jsonb)`, taskID, page.Limit, page.StartIndex).
		Scan(&exists, &result.TotalRecordCount, &raw)
	if err != nil {
		return RunPage{}, fmt.Errorf("list task runs: %w", err)
	}
	if !exists {
		return RunPage{}, ErrNotFound
	}
	if err := json.Unmarshal(raw, &result.Items); err != nil {
		return RunPage{}, fmt.Errorf("decode task runs: %w", err)
	}
	normalizeRuns(result.Items)
	return result, nil
}

// ListActiveRuns provides a bounded page for an application-owned coordinator.
func (s *Store) ListActiveRuns(ctx context.Context, page Page) (RunPage, error) {
	page, err := normalizePage(page)
	if err != nil {
		return RunPage{}, err
	}
	result := RunPage{Items: []Run{}, StartIndex: page.StartIndex, Limit: page.Limit}
	var raw []byte
	err = s.pool.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM task_runs WHERE state IN ('pending','running','stopping')),
        COALESCE((SELECT jsonb_agg(to_jsonb(p) - 'request_fingerprint' ORDER BY p.created_at, p.id)
            FROM (SELECT * FROM task_runs WHERE state IN ('pending','running','stopping')
                ORDER BY created_at, id LIMIT $1 OFFSET $2) p), '[]'::jsonb)`, page.Limit, page.StartIndex).
		Scan(&result.TotalRecordCount, &raw)
	if err != nil {
		return RunPage{}, fmt.Errorf("list active task runs: %w", err)
	}
	if err := json.Unmarshal(raw, &result.Items); err != nil {
		return RunPage{}, fmt.Errorf("decode active task runs: %w", err)
	}
	normalizeRuns(result.Items)
	return result, nil
}

func (s *Store) ListChildren(ctx context.Context, runID string, page Page) (ChildPage, error) {
	return s.listChildren(ctx, runID, page, false)
}

func (s *Store) ListActiveChildren(ctx context.Context, runID string, page Page) (ChildPage, error) {
	return s.listChildren(ctx, runID, page, true)
}

func (s *Store) listChildren(ctx context.Context, runID string, page Page, activeOnly bool) (ChildPage, error) {
	page, err := normalizePage(page)
	if err != nil {
		return ChildPage{}, err
	}
	result := ChildPage{Items: []Child{}, StartIndex: page.StartIndex, Limit: page.Limit}
	var exists bool
	var raw []byte
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_runs WHERE id = $1),
        (SELECT count(*) FROM task_run_children WHERE run_id = $1
            AND (NOT $4 OR state IN ('waiting','queued','running'))),
        COALESCE((SELECT jsonb_agg(to_jsonb(p) ORDER BY p.ordinal, p.id)
            FROM (SELECT * FROM task_run_children WHERE run_id = $1
                AND (NOT $4 OR state IN ('waiting','queued','running'))
                ORDER BY ordinal, id LIMIT $2 OFFSET $3) p), '[]'::jsonb)`, runID, page.Limit, page.StartIndex, activeOnly).
		Scan(&exists, &result.TotalRecordCount, &raw)
	if err != nil {
		return ChildPage{}, fmt.Errorf("list task children: %w", err)
	}
	if !exists {
		return ChildPage{}, ErrNotFound
	}
	if err := json.Unmarshal(raw, &result.Items); err != nil {
		return ChildPage{}, fmt.Errorf("decode task children: %w", err)
	}
	for index := range result.Items {
		normalizeChild(&result.Items[index])
	}
	return result, nil
}

type rowScanner interface{ Scan(...any) error }

func decodeRow(row rowScanner, value any) error {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read task resource: %w", err)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return fmt.Errorf("decode task resource: %w", err)
	}
	switch decoded := value.(type) {
	case *Definition:
		normalizeDefinition(decoded)
	case *Run:
		normalizeRun(decoded)
	case *Child:
		normalizeChild(decoded)
	}
	return nil
}

func utcPointer(value **time.Time) {
	if *value != nil {
		normalized := (*value).UTC()
		*value = &normalized
	}
}

func normalizeDefinition(value *Definition) {
	value.CreatedAt, value.UpdatedAt = value.CreatedAt.UTC(), value.UpdatedAt.UTC()
	if value.Triggers == nil {
		value.Triggers = []Trigger{}
	}
	for index := range value.Triggers {
		trigger := &value.Triggers[index]
		trigger.CreatedAt, trigger.UpdatedAt = trigger.CreatedAt.UTC(), trigger.UpdatedAt.UTC()
		utcPointer(&trigger.AnchorAt)
		utcPointer(&trigger.NextFireAt)
		utcPointer(&trigger.LastDueAt)
		utcPointer(&trigger.RetiredAt)
	}
	if value.CurrentRun != nil {
		normalizeRun(value.CurrentRun)
	}
	if value.LastRun != nil {
		normalizeRun(value.LastRun)
	}
}

func normalizeRun(value *Run) {
	value.CreatedAt = value.CreatedAt.UTC()
	utcPointer(&value.ScheduledFor)
	utcPointer(&value.StartedAt)
	utcPointer(&value.DeadlineAt)
	utcPointer(&value.StopRequestedAt)
	utcPointer(&value.FinishedAt)
}

func normalizeRuns(values []Run) {
	for index := range values {
		normalizeRun(&values[index])
	}
}

func normalizeChild(value *Child) {
	value.CreatedAt = value.CreatedAt.UTC()
	utcPointer(&value.StartedAt)
	utcPointer(&value.FinishedAt)
}

// The explicit adapter changes only the query signature. It cannot expose a
// connection or replace the owned transaction's protected context.
type authorizationTx struct{ tx library.OwnedTx }

func (adapter authorizationTx) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	return adapter.tx.QueryRow(statement, args...)
}

var _ identity.AuthorizationTx = authorizationTx{}

func checkActor(tx library.OwnedTx, actor Actor, lock bool) error {
	return identity.CheckAdministrator(context.Background(), authorizationTx{tx: tx}, actor.Principal, actor.Audience, lock)
}

func normalizePage(page Page) (Page, error) {
	if page.StartIndex < 0 || page.StartIndex > MaxStartIndex {
		return Page{}, &ValidationError{Fields: map[string]string{"StartIndex": "start index must be between 0 and 2147483647"}}
	}
	if page.Limit == 0 {
		page.Limit = DefaultPageLimit
	}
	if page.Limit < 1 || page.Limit > MaxPageLimit {
		return Page{}, &ValidationError{Fields: map[string]string{"Limit": "limit must be between 1 and 200"}}
	}
	return page, nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create task identity: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
