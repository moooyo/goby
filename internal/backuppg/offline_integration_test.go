//go:build linux

package backuppg

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
)

// Reserve an owned loopback TCP port without listening. It remains unavailable
// to source connections and cannot be claimed by a different listener during
// the test. This does not probe or modify any pre-existing endpoint.
func unavailableSourceURL(t *testing.T, sourceURL string) string {
	t.Helper()
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal("create unavailable source endpoint")
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 1}}); err != nil {
		t.Fatal("reserve unavailable source endpoint")
	}
	bound, err := syscall.Getsockname(fd)
	if err != nil {
		t.Fatal("read owned source endpoint")
	}
	address, ok := bound.(*syscall.SockaddrInet4)
	if !ok || address.Port == 0 {
		t.Fatal("owned source endpoint is invalid")
	}
	endpoint := net.JoinHostPort("127.0.0.1", strconv.Itoa(address.Port))
	connection, err := net.DialTimeout("tcp", endpoint, 500*time.Millisecond)
	if connection != nil {
		_ = connection.Close()
	}
	if err == nil {
		t.Fatal("owned source endpoint unexpectedly accepted a connection")
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		t.Fatal("parse deployment source fixture")
	}
	parsed.Host = endpoint
	return parsed.String()
}

func unchangedSourceWitness(t *testing.T, ctx context.Context, source *pgxpool.Pool, options Options) (backupformat.SourceFacts, map[string]sequenceState) {
	t.Helper()
	snapshot, err := OpenSnapshot(ctx, source, options)
	if err != nil {
		t.Fatalf("open source preservation witness: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(ctx)
	if err != nil {
		t.Fatal("read source preservation fingerprints")
	}
	sequences := make(map[string]sequenceState)
	for _, sequence := range snapshot.plan.catalog.Sequences {
		var state sequenceState
		if snapshot.Tx().QueryRow(snapshot.Context(), `SELECT last_value,is_called FROM `+qualified(options.Schema, sequence.Name)).Scan(&state.value, &state.called) != nil {
			t.Fatal("read source sequence preservation witness")
		}
		sequences[sequence.Name] = state
	}
	return facts, sequences
}

func assertSourceWitness(t *testing.T, ctx context.Context, source *pgxpool.Pool, options Options, before backupformat.SourceFacts, sequences map[string]sequenceState) {
	t.Helper()
	after, afterSequences := unchangedSourceWitness(t, ctx, source, options)
	if !equalJSON(before, after) {
		t.Fatal("offline recovery modified original source rows or schema facts")
	}
	if len(sequences) != len(afterSequences) {
		t.Fatal("offline recovery changed source sequence inventory")
	}
	for name, state := range sequences {
		if got, ok := afterSequences[name]; !ok || got != state {
			t.Fatal("offline recovery modified source sequence state")
		}
	}
}

func TestPostgreSQLOfflineRestoreWithUnavailableSource(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	file, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	offline := options
	offline.SourceURL = unavailableSourceURL(t, options.SourceURL)
	// Existing Restore retains its live-source contract. An unavailable source
	// must still fail before changing the target, rather than silently opting in
	// to the offline identity policy.
	unavailableConfig, err := pgxpool.ParseConfig(offline.SourceURL)
	if err != nil {
		t.Fatal("parse unavailable source pool")
	}
	unavailableConfig.ConnConfig.ConnectTimeout = 500 * time.Millisecond
	unavailable, err := pgxpool.NewWithConfig(ctx, unavailableConfig)
	if err != nil {
		t.Fatal("create unavailable source pool")
	}
	defer unavailable.Close()
	if _, err := Restore(ctx, unavailable, target, file, facts, options); !errors.Is(err, ErrDatabase) {
		t.Fatalf("existing live-source restore contract changed: %v", err)
	}
	var objects int
	if target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, options.Schema).Scan(&objects) != nil || objects != 0 {
		t.Fatal("failed live-source recovery modified the empty target")
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal("rewind offline archive")
	}
	result, err := RestoreOffline(ctx, target, file, facts, offline)
	if err != nil {
		t.Fatalf("restore without a reachable source: %v", err)
	}
	if result.SourceVersion != facts.SchemaVersion || !equalJSON(result.Tables, facts.Tables) {
		t.Fatal("offline restoration differs from authenticated source facts")
	}
	var name string
	if target.QueryRow(ctx, `SELECT name FROM users WHERE id='backup-admin'`).Scan(&name) != nil || name != "Before snapshot" {
		t.Fatal("offline restoration omitted source business rows")
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}

func TestPostgreSQLOfflineRestoreRejectsIdentityAndOverrides(t *testing.T) {
	ctx, source, target, options := recoveryFixture(t)
	file, facts := sourceArchive(t, ctx, source, options)
	before, sequences := unchangedSourceWitness(t, ctx, source, options)
	unavailable := unavailableSourceURL(t, options.SourceURL)
	for _, scenario := range []string{"same_database", "same_role", "both", "query_database_override", "query_user_override", "service_override", "multi_host", "populated_target"} {
		t.Run(scenario, func(t *testing.T) {
			if _, err := file.Seek(0, 0); err != nil {
				t.Fatal("rewind rejection archive")
			}
			parsed, err := url.Parse(unavailable)
			if err != nil {
				t.Fatal("parse offline rejection fixture")
			}
			expected := ErrTarget
			switch scenario {
			case "same_database":
				parsed.Path = "/" + target.Config().ConnConfig.Database
			case "same_role":
				parsed.User = url.User(target.Config().ConnConfig.User)
			case "both":
				parsed.Path = "/" + target.Config().ConnConfig.Database
				parsed.User = url.User(target.Config().ConnConfig.User)
			case "query_database_override":
				parsed.RawQuery += "&dbname=" + target.Config().ConnConfig.Database
				expected = ErrConfiguration
			case "query_user_override":
				parsed.RawQuery += "&user=" + target.Config().ConnConfig.User
				expected = ErrConfiguration
			case "service_override":
				parsed.RawQuery += "&service=unexpected"
				expected = ErrConfiguration
			case "multi_host":
				parsed.Host += " ,localhost:15432"
				expected = ErrConfiguration
			case "populated_target":
				if _, err := target.Exec(ctx, `CREATE TABLE offline_preservation_witness(id integer PRIMARY KEY); INSERT INTO offline_preservation_witness VALUES(42)`); err != nil {
					t.Fatal("create owned target preservation witness")
				}
			}
			offline := options
			offline.SourceURL = parsed.String()
			if _, err := RestoreOffline(ctx, target, file, facts, offline); !errors.Is(err, expected) {
				t.Fatalf("offline target or override rejection = %v, want %v", err, expected)
			}
			var count int
			if scenario == "populated_target" {
				if target.QueryRow(ctx, `SELECT id FROM offline_preservation_witness`).Scan(&count) != nil || count != 42 {
					t.Fatal("offline rejection changed existing target data")
				}
			} else if target.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1`, options.Schema).Scan(&count) != nil || count != 0 {
				t.Fatal("identity rejection changed the target schema")
			}
		})
	}
	assertSourceWitness(t, ctx, source, options, before, sequences)
}
