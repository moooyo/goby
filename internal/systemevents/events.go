// Package systemevents records bounded, committed task signals. It contains no
// transport or task-execution dependency and accepts only trusted transactions.
package systemevents

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

type Event string

const (
	ServerStarted        Event = "ServerStarted"
	LibraryChanged       Event = "LibraryChanged"
	ConfigurationChanged Event = "ConfigurationChanged"
)

func Valid(event Event) bool {
	return event == ServerStarted || event == LibraryChanged || event == ConfigurationChanged
}

type Exec func(string, ...any) (pgconn.CommandTag, error)

// Record changes only one of three durable counters. Coalescing needs no
// unbounded queue, item identity, user data, or notification payload. Rollback
// rolls back the signal together with its source mutation.
func Record(exec Exec, event Event) error {
	if !Valid(event) || event == ServerStarted {
		return fmt.Errorf("invalid committed system event")
	}
	tag, err := exec(`UPDATE task_system_events SET sequence=sequence+1,
        occurred_at=clock_timestamp() WHERE name=$1`, string(event))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("system event counter is missing")
	}
	return nil
}

// RecordStartup binds retries to the manager's fixed startup clock. The last
// lifecycle identity is enough because catalog ownership permits one manager.
func RecordStartup(exec Exec, identity string) error {
	if identity == "" || len(identity) > 128 {
		return fmt.Errorf("invalid startup identity")
	}
	tag, err := exec(`WITH changed AS (UPDATE task_system_events SET sequence=sequence+1,
        occurred_at=clock_timestamp(), lifecycle_key=$1
        WHERE name='ServerStarted' AND lifecycle_key<>$1 RETURNING name)
        SELECT name FROM task_system_events WHERE name='ServerStarted'`, identity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("startup event counter is missing")
	}
	return nil
}

type derivedKey struct{}

// WithDerived is reconstructed from durable task/scan ownership at worker
// creation, including recovered jobs. Task output must not recursively trigger
// another task; client catalog notifications remain unaffected.
func WithDerived(ctx context.Context) context.Context {
	return context.WithValue(ctx, derivedKey{}, true)
}
func IsDerived(ctx context.Context) bool { value, _ := ctx.Value(derivedKey{}).(bool); return value }
