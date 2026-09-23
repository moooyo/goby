package database_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestOpenPreservesTypedQueriesWithoutNamedStatements(t *testing.T) {
	for _, test := range []struct {
		name, mode, capacity string
	}{
		{name: "default"},
		{name: "override_statement_mode", mode: "cache_statement"},
		{name: "disabled_descriptions", mode: "exec", capacity: "0"},
		{name: "small_description_cache", mode: "simple_protocol", capacity: "2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, fixture := migrationTestPool(t)
			schema := openJITSchema(t, ctx, fixture)
			if _, err := fixture.Exec(ctx, `CREATE TABLE query_mode_values (
				id bigint PRIMARY KEY, payload jsonb NOT NULL, binary_value bytea NOT NULL,
				tags text[], optional_number bigint, optional_time timestamptz)`); err != nil {
				t.Fatalf("create isolated typed-query fixture: %v", err)
			}
			dsn := openQueryModeDSN(t, schema, test.mode, test.capacity)
			incoming, err := pgx.ParseConfig(dsn)
			if err != nil {
				t.Fatal("parse the incoming typed-query configuration")
			}
			wantMode := pgx.QueryExecModeCacheDescribe
			if incoming.DescriptionCacheCapacity <= 0 {
				wantMode = pgx.QueryExecModeDescribeExec
			}
			pool := openJITPool(t, ctx, dsn)
			first := acquireOpenJIT(t, ctx, pool)
			second := acquireOpenJIT(t, ctx, pool)
			if first.Conn().PgConn().PID() == second.Conn().PgConn().PID() {
				t.Fatal("simultaneous typed-query checkouts reused a physical session")
			}
			for index, connection := range []*pgx.Conn{first.Conn(), second.Conn()} {
				config := connection.Config()
				if config.DefaultQueryExecMode != wantMode {
					t.Fatalf("physical query mode = %s, want %s", config.DefaultQueryExecMode, wantMode)
				}
				if config.DescriptionCacheCapacity != incoming.DescriptionCacheCapacity {
					t.Fatalf("description cache capacity changed: %d", config.DescriptionCacheCapacity)
				}
				requireOpenJITOff(t, ctx, connection, schema)
				for repeat := 0; repeat < 4; repeat++ {
					values := openQueryModeValues{
						document: openQueryModeDocument{Version: repeat, Text: "Quotes \" and apostrophe ' and slash \\ with a newline\n."},
						binary:   []byte{0, 1, 127, 128, 255},
						tags:     []string{"alpha", "comma,quote\"slash\\", ""},
					}
					if repeat%2 != 0 {
						values.number = pgtype.Int8{Int64: 1<<54 + int64(repeat), Valid: true}
						values.instant = pgtype.Timestamptz{Time: time.Date(2025, time.January, 2, 3, 4, 5, 678000000, time.UTC), Valid: true}
					}
					if repeat == 1 {
						values.tags = []string{}
					} else if repeat == 2 {
						values.tags = nil
					}
					raw, err := json.Marshal(values.document)
					if err != nil {
						t.Fatalf("encode typed-query JSON: %v", err)
					}
					// JSON and bytea deliberately share the []byte Go type. The
					// server's parameter OIDs must select their distinct codecs.
					id := int64(index + 1)
					assertOpenQueryModeValues(t, connection.QueryRow(ctx, `INSERT INTO query_mode_values
						(id,payload,binary_value,tags,optional_number,optional_time) VALUES ($1,$2,$3,$4,$5,$6)
						ON CONFLICT (id) DO UPDATE SET payload=EXCLUDED.payload,binary_value=EXCLUDED.binary_value,
						tags=EXCLUDED.tags,optional_number=EXCLUDED.optional_number,optional_time=EXCLUDED.optional_time
						RETURNING payload,binary_value,tags,optional_number,optional_time`,
						id, raw, values.binary, values.tags, values.number, values.instant), values)
					assertOpenQueryModeValues(t, connection.QueryRow(ctx, `SELECT payload,binary_value,tags,optional_number,optional_time
						FROM query_mode_values WHERE id=$1`, id), values)
					// A third statement also exercises eviction in the two-entry
					// description cache while retaining explicit-cast inference.
					assertOpenQueryModeValues(t, connection.QueryRow(ctx, `SELECT $1::jsonb,$2::bytea,$3::text[],$4::bigint,$5::timestamptz`,
						raw, values.binary, values.tags, values.number, values.instant), values)
					assertOpenQueryModeNoNamedStatements(t, ctx, connection)
				}
			}
		})
	}
}

type openQueryModeDocument struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
}

type openQueryModeValues struct {
	document openQueryModeDocument
	binary   []byte
	tags     []string
	number   pgtype.Int8
	instant  pgtype.Timestamptz
}

func assertOpenQueryModeValues(t *testing.T, row pgx.Row, want openQueryModeValues) {
	t.Helper()
	var raw []byte
	var got openQueryModeValues
	if err := row.Scan(&raw, &got.binary, &got.tags, &got.number, &got.instant); err != nil {
		t.Fatalf("execute and decode typed query: %v", err)
	}
	if err := json.Unmarshal(raw, &got.document); err != nil {
		t.Fatalf("decode JSON query result: %v", err)
	}
	if got.document != want.document || !bytes.Equal(got.binary, want.binary) ||
		!slices.Equal(got.tags, want.tags) || (got.tags == nil) != (want.tags == nil) ||
		got.number != want.number || got.instant.Valid != want.instant.Valid ||
		got.instant.Valid && !got.instant.Time.Equal(want.instant.Time) {
		t.Fatal("typed execution changed JSON, binary, array, or nullable values")
	}
}

func assertOpenQueryModeNoNamedStatements(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	var pid uint32
	var prepared int64
	// The observer must not prepare its own statement or alter the cache under
	// test. The PID check binds the count to this exact physical connection.
	if err := connection.QueryRow(ctx, `SELECT pg_backend_pid(),count(*) FROM pg_prepared_statements`,
		pgx.QueryExecModeExec).Scan(&pid, &prepared); err != nil {
		t.Fatalf("observe the typed-query backend: %v", err)
	}
	if pid != connection.PgConn().PID() || prepared != 0 {
		t.Fatalf("typed-query backend pid=%d retained %d named statements", pid, prepared)
	}
}

func openQueryModeDSN(t *testing.T, schema, mode, capacity string) string {
	t.Helper()
	raw := openJITDSN(t, schema, "")
	if strings.HasPrefix(raw, "postgres://") || strings.HasPrefix(raw, "postgresql://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal("parse the typed-query integration database URL")
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			t.Fatal("parse typed-query integration database options")
		}
		if mode != "" {
			query.Set("default_query_exec_mode", mode)
		}
		if capacity != "" {
			query.Set("description_cache_capacity", capacity)
		}
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if mode != "" {
		raw += " default_query_exec_mode=" + mode
	}
	if capacity != "" {
		raw += " description_cache_capacity=" + capacity
	}
	return raw
}
