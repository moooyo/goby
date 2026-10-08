//go:build linux

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
)

const generationStartupRolePrefix = "goby_generation_"

// Production startup intentionally drops private error messages. Preserve its
// literal stage sentinels and SQLSTATE/type identities without formatting
// unknown errors, SQL, credentials, URI text, or connection configuration.
func generationStartupFailure(err error) string {
	allowed := map[string]bool{
		"application generation dependencies are invalid":                                        true,
		"active generation configuration is unavailable":                                         true,
		"application generation startup timeout is invalid":                                      true,
		"PostgreSQL connection failed; check database availability and deployment configuration": true,
		"database generation ownership could not be acquired":                                    true,
		"database generation identity check failed":                                              true,
		"database schema initialization failed":                                                  true,
		"database generation binding failed":                                                     true,
		"administrator initialization state is unavailable":                                      true,
		"GOBY_SETUP_TOKEN is required until the first administrator is created":                  true,
		"backup and recovery manager initialization failed":                                      true,
		"backup and recovery startup reconciliation failed":                                      true,
		"pending recovery transition could not be resolved":                                      true,
		"activated recovery generation validation failed":                                        true,
		"playback control database capacity is unavailable":                                      true,
		"administrator assets are unavailable":                                                   true,
		"application generation initialization failed":                                           true,
		"HTTP startup binding is unavailable":                                                    true,
		"HTTP listener reservation failed":                                                       true,
		"HTTP listener publication failed":                                                       true,
		"injected reserved startup connection failure":                                           true,
	}
	var facts []string
	errorString := reflect.TypeOf(errors.New(""))
	remaining := 32
	var visit func(error, int)
	visit = func(current error, depth int) {
		if current == nil || remaining == 0 || depth > 8 {
			return
		}
		remaining--
		facts = append(facts, "type="+fmt.Sprintf("%T", current)+",class="+diagnostics.ErrorClass(current))
		if reflect.TypeOf(current) == errorString && allowed[current.Error()] {
			facts = append(facts, "stage="+current.Error())
		}
		if postgres, ok := current.(*pgconn.PgError); ok {
			if len(postgres.Code) == 5 && strings.IndexFunc(postgres.Code, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z') }) < 0 {
				facts = append(facts, "sqlstate="+postgres.Code)
			}
		}
		switch wrapped := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range wrapped.Unwrap() {
				visit(child, depth+1)
			}
		case interface{ Unwrap() error }:
			visit(wrapped.Unwrap(), depth+1)
		}
	}
	visit(err, 0)
	return strings.Join(facts, " | ")
}

type generationStartupDatabase struct {
	name, role, uri              string
	databaseOID, roleOID         int64
	roleCreated, databaseCreated bool
}

// The owned bridge makes the existing socket-only verification cluster usable
// by strict recovery URIs and the real PostgreSQL command adapter. It never
// changes PostgreSQL listeners or accepts traffic outside loopback.
type generationStartupProxy struct {
	listener         net.Listener
	network, address string
	slots            chan struct{}
	mu               sync.Mutex
	closing          bool
	connections      map[net.Conn]bool
	acceptDone       chan struct{}
	workers          sync.WaitGroup
	once             sync.Once
}

func newGenerationStartupProxy(t *testing.T, connection *pgx.ConnConfig) *generationStartupProxy {
	t.Helper()
	if !filepath.IsAbs(connection.Host) {
		t.Skip("generation startup fixtures require the designated owned Unix-socket PostgreSQL URL")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve the private generation PostgreSQL bridge")
	}
	proxy := &generationStartupProxy{listener: listener, network: "unix",
		address: filepath.Join(connection.Host, ".s.PGSQL."+strconv.Itoa(int(connection.Port))),
		slots:   make(chan struct{}, 32), connections: make(map[net.Conn]bool), acceptDone: make(chan struct{})}
	go proxy.accept()
	return proxy
}

func (proxy *generationStartupProxy) accept() {
	defer close(proxy.acceptDone)
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		select {
		case proxy.slots <- struct{}{}:
		default:
			_ = client.Close()
			continue
		}
		proxy.mu.Lock()
		if proxy.closing {
			proxy.mu.Unlock()
			<-proxy.slots
			_ = client.Close()
			return
		}
		proxy.connections[client] = true
		proxy.workers.Add(1)
		proxy.mu.Unlock()
		go proxy.forward(client)
	}
}

func (proxy *generationStartupProxy) forward(client net.Conn) {
	defer proxy.workers.Done()
	defer func() {
		_ = client.Close()
		proxy.mu.Lock()
		delete(proxy.connections, client)
		proxy.mu.Unlock()
		<-proxy.slots
	}()
	upstream, err := net.DialTimeout(proxy.network, proxy.address, 5*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	proxy.mu.Lock()
	if proxy.closing {
		proxy.mu.Unlock()
		return
	}
	proxy.connections[upstream] = true
	proxy.mu.Unlock()
	defer func() {
		proxy.mu.Lock()
		delete(proxy.connections, upstream)
		proxy.mu.Unlock()
	}()
	// The fixture itself has an eight-minute deadline. A transport that misses
	// application cancellation must still release its private forwarding owner.
	_ = client.SetDeadline(time.Now().Add(9 * time.Minute))
	_ = upstream.SetDeadline(time.Now().Add(9 * time.Minute))
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); done <- struct{}{} }()
	<-done
	_ = client.Close()
	_ = upstream.Close()
	<-done
}

func (proxy *generationStartupProxy) Close() {
	proxy.once.Do(func() {
		proxy.mu.Lock()
		proxy.closing = true
		connections := make([]net.Conn, 0, len(proxy.connections))
		for connection := range proxy.connections {
			connections = append(connections, connection)
		}
		proxy.mu.Unlock()
		_ = proxy.listener.Close()
		for _, connection := range connections {
			_ = connection.Close()
		}
		<-proxy.acceptDone
		proxy.workers.Wait()
	})
}

type generationStartupFixture struct {
	ctx             context.Context
	admin           *pgxpool.Pool
	proxy           *generationStartupProxy
	primary, target *generationStartupDatabase
	cfg             config.Config
	runtime         *recovery.Runtime
	owner           *generationOwner
	logger          *slog.Logger
}

func generationStartupID(t *testing.T) string {
	t.Helper()
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		t.Fatal("generate an owned startup resource identity")
	}
	return hex.EncodeToString(value[:])
}

func newGenerationStartupFixture(t *testing.T) *generationStartupFixture {
	t.Helper()
	if os.Getenv("GOBY_GENERATION_STARTUP_DISPOSABLE_DATABASES") != "1" {
		t.Skip("GOBY_GENERATION_STARTUP_DISPOSABLE_DATABASES=1 explicitly enables owned startup databases and roles")
	}
	adminURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	dump, restore := os.Getenv("GOBY_TEST_PG_DUMP"), os.Getenv("GOBY_TEST_PG_RESTORE")
	if adminURL == "" || dump == "" || restore == "" {
		t.Skip("owned PostgreSQL admin URL and explicit PostgreSQL 17 dump/restore paths are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	adminConfig, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal("parse the designated startup PostgreSQL configuration")
	}
	adminConfig.MaxConns = 2
	admin, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatal("open the startup fixture administrator connection")
	}
	t.Cleanup(admin.Close)
	var superuser bool
	if err := admin.QueryRow(ctx, "SELECT rolsuper FROM pg_roles WHERE rolname=current_user").Scan(&superuser); err != nil || !superuser {
		t.Fatal("the explicit disposable startup fixture requires its owned cluster administrator")
	}
	proxy := newGenerationStartupProxy(t, adminConfig.ConnConfig)
	t.Cleanup(proxy.Close)
	root := t.TempDir()
	f := &generationStartupFixture{ctx: ctx, admin: admin, proxy: proxy, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Registered before creating either role, so partial provisioning failures
	// still clean only resources whose successful creation was recorded.
	t.Cleanup(func() { f.cleanup(t) })
	f.primary = f.createDatabase(t, "primary")
	f.target = f.createDatabase(t, "recovery")
	mediaRoot, webRoot := filepath.Join(root, "media"), filepath.Join(root, "web")
	for _, directory := range []string{mediaRoot, webRoot} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal("create the owned startup deployment directory")
		}
	}
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<!doctype html><title>Startup fixture</title>"), 0o600); err != nil {
		t.Fatal("write the owned startup dashboard")
	}
	f.cfg = config.Config{DatabaseURL: f.primary.uri, ListenAddress: "127.0.0.1:0", PublicURL: "http://127.0.0.1",
		ServerName: "Generation startup fixture", StartupTimeout: time.Minute, SetupToken: "generation-startup-token-with-more-than-thirty-two-bytes",
		APIKeyMasterKeyFile: filepath.Join(root, "master.key"), MediaRoots: []string{mediaRoot}, WebDirectory: webRoot,
		FFmpegPath: "/unused-startup-ffmpeg", FFprobePath: "/unused-startup-ffprobe",
		Transcoding: config.TranscodingConfig{MaxBitrate: config.DefaultMaxBitrate, MaxWidth: config.DefaultMaxWidth,
			MaxHeight: config.DefaultMaxHeight, MaxAudioChannels: config.DefaultMaxAudioChannels},
		Recovery: config.RecoveryConfig{Directory: filepath.Join(root, "lifecycle"), OperationsDirectory: filepath.Join(root, "operations"), DatabaseURL: f.target.uri,
			PGDumpPath: dump, PGRestorePath: restore, OperationTimeout: 3 * time.Minute,
			Backups: backupstore.Config{Directory: filepath.Join(root, "backups"), MaxObjectBytes: 16 << 20, MaxTotalBytes: 64 << 20, MaxObjects: 16, MinFreeBytes: 1 << 20}},
	}
	return f
}

func (f *generationStartupFixture) createDatabase(t *testing.T, slot string) *generationStartupDatabase {
	t.Helper()
	name := generationStartupRolePrefix + slot + "_" + generationStartupID(t)[:20]
	resource := &generationStartupDatabase{name: name, role: name}
	if slot == "primary" {
		f.primary = resource
	} else {
		f.target = resource
	}
	if _, err := f.admin.Exec(f.ctx, "CREATE ROLE "+pgx.Identifier{resource.role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS"); err != nil {
		t.Fatal("create the unique low-privilege startup role")
	}
	resource.roleCreated = true
	if err := f.admin.QueryRow(f.ctx, "SELECT oid::bigint FROM pg_roles WHERE rolname=$1", resource.role).Scan(&resource.roleOID); err != nil {
		t.Fatal("record the exact new startup role identity")
	}
	if _, err := f.admin.Exec(f.ctx, "CREATE DATABASE "+pgx.Identifier{resource.name}.Sanitize()+" OWNER "+pgx.Identifier{resource.role}.Sanitize()+" TEMPLATE template0 ENCODING 'UTF8'"); err != nil {
		t.Fatal("create the unique owned public startup database")
	}
	resource.databaseCreated = true
	var owner int64
	if err := f.admin.QueryRow(f.ctx, "SELECT oid::bigint, datdba::bigint FROM pg_database WHERE datname=$1", resource.name).Scan(&resource.databaseOID, &owner); err != nil || owner != resource.roleOID {
		t.Fatal("record the exact new startup database owner")
	}
	address := &url.URL{Scheme: "postgres", User: url.User(resource.role), Host: f.proxy.listener.Addr().String(), Path: "/" + resource.name, RawQuery: "sslmode=disable"}
	resource.uri = address.String()
	return resource
}

func (f *generationStartupFixture) verifyDatabase(ctx context.Context, resource *generationStartupDatabase) error {
	if resource == nil || !resource.roleCreated || !resource.databaseCreated || resource.roleOID <= 0 || resource.databaseOID <= 0 ||
		!strings.HasPrefix(resource.name, generationStartupRolePrefix) || !strings.HasPrefix(resource.role, generationStartupRolePrefix) {
		return errors.New("owned startup database creation record is incomplete")
	}
	var databaseOID, ownerOID, roleOID int64
	err := f.admin.QueryRow(ctx, `SELECT d.oid::bigint,d.datdba::bigint,r.oid::bigint FROM pg_database d JOIN pg_roles r ON r.rolname=$2 WHERE d.datname=$1`, resource.name, resource.role).
		Scan(&databaseOID, &ownerOID, &roleOID)
	if err != nil || databaseOID != resource.databaseOID || ownerOID != resource.roleOID || roleOID != resource.roleOID {
		return errors.New("owned startup database identity no longer matches its creation record")
	}
	return nil
}

func (f *generationStartupFixture) allowConnections(t *testing.T, resource *generationStartupDatabase, allow bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.verifyDatabase(ctx, resource); err != nil {
		t.Fatal("refuse to change an unverified startup database")
	}
	value := "false"
	if allow {
		value = "true"
	}
	if _, err := f.admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{resource.name}.Sanitize()+" WITH ALLOW_CONNECTIONS "+value); err != nil {
		t.Fatal("change connection admission only for the exact owned startup database")
	}
}

func (f *generationStartupFixture) openRuntime(t *testing.T) {
	t.Helper()
	runtime, err := recovery.Open(f.ctx, f.cfg)
	if err != nil {
		t.Fatalf("open the actual startup recovery runtime: error_type=%T", err)
	}
	f.runtime = runtime
	f.owner = &generationOwner{runtime: runtime, generations: make(map[*generation]bool)}
}

func (f *generationStartupFixture) prepare(t *testing.T, input *generation) *generation {
	t.Helper()
	g, err := f.owner.prepare(f.ctx, f.logger, nil, input)
	if err != nil || g == nil {
		if strings.Contains(generationStartupFailure(err), "stage=database generation binding failed") {
			f.bindingWitness(t)
		}
		t.Fatalf("prepare the actual application generation: cleanup_handle=%t failure={%s}", g != nil, generationStartupFailure(err))
	}
	return g
}

func (f *generationStartupFixture) bindingWitness(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg, state, err := f.runtime.ActiveConfig(ctx)
	if err != nil {
		t.Logf("startup_binding_witness active_config=false failure={%s}", generationStartupFailure(err))
		return
	}
	configuration, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		t.Log("startup_binding_witness configuration_parse=false")
		return
	}
	configuration.MaxConns, configuration.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Logf("startup_binding_witness connection=false failure={%s}", generationStartupFailure(err))
		return
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Logf("startup_binding_witness transaction=false failure={%s}", generationStartupFailure(err))
		return
	}
	defer tx.Rollback(ctx)
	var schemaVersion int64
	var databaseMatch, roleMatch, publicSchema bool
	if err := tx.QueryRow(ctx, `SELECT (SELECT COALESCE(max(version),0) FROM public.schema_migrations),
		current_database()=$1,current_user=$2,current_schema()='public'`, configuration.ConnConfig.Database, configuration.ConnConfig.User).
		Scan(&schemaVersion, &databaseMatch, &roleMatch, &publicSchema); err != nil {
		t.Logf("startup_binding_witness basic_facts=false failure={%s}", generationStartupFailure(err))
		return
	}
	_, inspectionErr := backuppg.InspectRecoveryTransaction(ctx, tx, "public")
	t.Logf("startup_binding_witness schema_version=%d database_match=%t role_match=%t public_schema=%t lifecycle_revision=%d lifecycle_slot=%s inspection_ok=%t unsupported=%t schema_mismatch=%t target_rejected=%t failure={%s}",
		schemaVersion, databaseMatch, roleMatch, publicSchema, state.Revision, state.DatabaseSlot, inspectionErr == nil,
		errors.Is(inspectionErr, backuppg.ErrUnsupported), errors.Is(inspectionErr, backuppg.ErrSchema), errors.Is(inspectionErr, backuppg.ErrTarget), generationStartupFailure(inspectionErr))
}

func (f *generationStartupFixture) administrator(t *testing.T, g *generation) identity.Principal {
	t.Helper()
	users := identity.NewWithApplicationKeyVault(g.pool, identity.NewApplicationKeyVault(g.cfg.APIKeyMasterKeyFile))
	user, err := users.Bootstrap(f.ctx, "Startup administrator", "startup-administrator-password")
	if err != nil {
		t.Fatal("bootstrap the actual source administrator")
	}
	login, err := users.Authenticate(f.ctx, user.Name, "startup-administrator-password", identity.Client{Name: "Startup administrator"}, "admin")
	if err != nil {
		t.Fatal("authenticate the actual source administrator")
	}
	actor, err := users.Resolve(f.ctx, login.Token, "admin")
	if err != nil {
		t.Fatal("resolve the actual source administrator")
	}
	return actor
}

func (f *generationStartupFixture) assertReady(t *testing.T, g *generation) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	result := g.Start()
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	status, code, decoded := 0, "unrecognized", false
	assertActive := func() {
		t.Helper()
		select {
		case err := <-result:
			t.Fatalf("the actual prepared generation stopped before readiness: error_type=%T last_status=%d error_code=%s error_decoded=%t", err, status, code, decoded)
		default:
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("prepared generation readiness budget ended: error_type=%T last_status=%d error_code=%s error_decoded=%t", err, status, code, decoded)
		}
		if g.lease == nil || !g.lease.Protects(g.pool) {
			t.Fatalf("prepared generation lost its lease before readiness: last_status=%d error_code=%s error_decoded=%t", status, code, decoded)
		}
	}
	pending := false
	for attempts := 1; ; attempts++ {
		assertActive()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+g.listener.Addr().String()+"/readyz", nil)
		if err != nil {
			t.Fatal("create the actual readiness request")
		}
		response, err := client.Do(request)
		if err != nil {
			if response != nil {
				_ = response.Body.Close()
			}
			assertActive()
			t.Fatalf("read the actual prepared listener: error_type=%T last_status=%d error_code=%s error_decoded=%t", err, status, code, decoded)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		status, code, decoded = response.StatusCode, "unrecognized", false
		if status != http.StatusOK {
			var diagnostic struct {
				Error struct {
					Code string
				}
			}
			decoded = readErr == nil && json.Unmarshal(body, &diagnostic) == nil
			if decoded {
				switch diagnostic.Error.Code {
				case "not_ready", "catalog_not_ready", "tasks_not_ready", "diagnostics_not_ready":
					code = diagnostic.Error.Code
				}
			}
		}
		assertActive()
		if readErr != nil || closeErr != nil {
			t.Fatalf("prepared generation readiness response failed: read_error_type=%T close_error_type=%T last_status=%d error_code=%s error_decoded=%t", readErr, closeErr, status, code, decoded)
		}
		if status == http.StatusOK {
			if pending {
				t.Logf("prepared_generation_readiness tasks_pending_observed=true attempts=%d", attempts)
			}
			return
		}
		if status != http.StatusServiceUnavailable || !decoded || code != "tasks_not_ready" {
			t.Fatalf("prepared generation readiness status=%d error_code=%s error_decoded=%t", status, code, decoded)
		}
		pending = true
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			assertActive()
		case err := <-result:
			timer.Stop()
			t.Fatalf("the actual prepared generation stopped before readiness: error_type=%T last_status=%d error_code=%s error_decoded=%t", err, status, code, decoded)
		}
	}
}

func (f *generationStartupFixture) assertPublishedPools(t *testing.T, g *generation, resource *generationStartupDatabase) {
	t.Helper()
	if g == nil || g.pool == nil || g.playbackControlPool == nil || g.app == nil || g.server == nil || g.listener == nil ||
		g.pool == g.playbackControlPool || !g.lease.Protects(g.pool) || g.pool.Config().MaxConns != database.DataMaxConns ||
		g.playbackControlPool.Config().MaxConns != database.PlaybackControlMaxConns ||
		g.pool.Config().MaxConns+g.playbackControlPool.Config().MaxConns != database.ApplicationMaxConns ||
		g.cfg.DatabaseURL != resource.uri || g.pool.Config().ConnString() != resource.uri || g.playbackControlPool.Config().ConnString() != resource.uri {
		t.Fatal("the actual prepared application did not publish one bounded same-generation pool pair")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	var held []*pgxpool.Conn
	defer func() {
		for _, connection := range held {
			connection.Release()
		}
	}()
	for _, lane := range []struct {
		pool  *pgxpool.Pool
		count int32
	}{{g.pool, database.DataMaxConns - 1}, {g.playbackControlPool, database.PlaybackControlMaxConns}} {
		for range lane.count {
			connection, err := lane.pool.Acquire(ctx)
			if err != nil {
				t.Fatal("reserve every application connection around the real catalog owner")
			}
			held = append(held, connection)
			var databaseName, schema, role, jit string
			if err := connection.QueryRow(ctx, "SELECT current_database(),current_schema(),current_user,current_setting('jit')").Scan(&databaseName, &schema, &role, &jit); err != nil ||
				databaseName != resource.name || schema != "public" || role != resource.role || jit != "off" {
				t.Fatal("a published physical connection differs from its exact generation identity or JIT policy")
			}
		}
	}
	var total, dataAndLease, control, deploymentLeases int
	err := f.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE application_name='goby'),
		count(*) FILTER(WHERE application_name='goby-playback-control') FROM pg_stat_activity
		WHERE datid=$1::oid AND usename=$2`, resource.databaseOID, resource.role).Scan(&total, &dataAndLease, &control)
	if err != nil {
		t.Fatal("observe the actual published backend budget")
	}
	err = f.admin.QueryRow(ctx, `SELECT count(DISTINCT pid) FROM pg_locks WHERE locktype='advisory' AND granted AND database=$1::oid
		AND classid=$2::oid AND objid=$3::oid AND objsubid=1`, resource.databaseOID,
		uint32(uint64(4919415424202458201)>>32), uint32(uint64(4919415424202458201)&0xffffffff)).Scan(&deploymentLeases)
	if err != nil || total != int(database.ApplicationMaxConns)+1 || dataAndLease != int(database.DataMaxConns)+1 ||
		control != int(database.PlaybackControlMaxConns) || deploymentLeases != 1 {
		t.Fatalf("actual generation backend budget mismatch: total=%d data_and_lease=%d control=%d deployment_leases=%d", total, dataAndLease, control, deploymentLeases)
	}
	t.Logf("prepared_generation_budget total_backends=%d application_max=%d data_max=%d control_max=%d deployment_leases=%d", total, database.ApplicationMaxConns, database.DataMaxConns, database.PlaybackControlMaxConns, deploymentLeases)
}

func (f *generationStartupFixture) cleanup(t *testing.T) {
	t.Helper()
	if f.owner != nil {
		if err := f.owner.Close(); err != nil {
			t.Errorf("drain the actual startup generation owner: error_type=%T", err)
		}
	} else if f.runtime != nil {
		if err := f.runtime.Close(); err != nil {
			t.Errorf("close the actual startup runtime: error_type=%T", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// First diagnose application leaks while transport owners still exist.
	// Closing the bridge before this check could hide a failed generation drain.
	for _, resource := range []*generationStartupDatabase{f.target, f.primary} {
		if resource == nil || !resource.databaseCreated || f.verifyDatabase(ctx, resource) != nil {
			continue
		}
		var remaining int
		deadline := time.Now().Add(3 * time.Second)
		for {
			err := f.admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datid=$1::oid", resource.databaseOID).Scan(&remaining)
			if err != nil || remaining == 0 || time.Now().After(deadline) {
				if err != nil || remaining != 0 {
					t.Errorf("owned startup database retained backends after resource drain: remaining=%d error_type=%T", remaining, err)
				}
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	proxyDeadline := time.Now().Add(3 * time.Second)
	for {
		f.proxy.mu.Lock()
		active := len(f.proxy.connections)
		f.proxy.mu.Unlock()
		if active == 0 || time.Now().After(proxyDeadline) {
			if active != 0 {
				t.Errorf("owned startup bridge retained forwarding owners after generation drain: active=%d", active)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.proxy.Close()
	// Every forwarding owner has now joined. Identity is checked again before
	// dropping any exact creation record; other databases are never candidates.
	for _, resource := range []*generationStartupDatabase{f.target, f.primary} {
		if resource == nil {
			continue
		}
		if resource.databaseCreated {
			if err := f.verifyDatabase(ctx, resource); err != nil {
				t.Error("refuse to drop an unverified owned startup database")
				continue
			}
			if _, err := f.admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{resource.name}.Sanitize()+" WITH (FORCE)"); err != nil {
				t.Errorf("drop the exact owned startup database: error_type=%T", err)
				continue
			}
			resource.databaseCreated = false
		}
		if resource.roleCreated {
			var roleOID int64
			if resource.roleOID <= 0 || !strings.HasPrefix(resource.role, generationStartupRolePrefix) ||
				f.admin.QueryRow(ctx, "SELECT oid::bigint FROM pg_roles WHERE rolname=$1", resource.role).Scan(&roleOID) != nil || roleOID != resource.roleOID {
				t.Error("refuse to drop an unverified owned startup role")
				continue
			}
			if _, err := f.admin.Exec(ctx, "DROP ROLE "+pgx.Identifier{resource.role}.Sanitize()); err != nil {
				t.Errorf("drop the exact owned startup role: error_type=%T", err)
			} else {
				resource.roleCreated = false
			}
		}
	}
}

func (f *generationStartupFixture) waitOperation(t *testing.T, manager *recovery.Manager, actor identity.Principal, id, expected string) recovery.OperationView {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
	defer cancel()
	for {
		view, err := manager.Operation(ctx, actor, id)
		if err != nil {
			t.Fatalf("observe the actual recovery operation: error_type=%T", err)
		}
		if view.State == expected {
			return view
		}
		if view.State == "failed" || view.State == "cancelled" || view.State == "interrupted" {
			t.Fatalf("actual recovery operation ended before its expected receipt: state=%s code=%s", view.State, view.ErrorCode)
		}
		select {
		case <-ctx.Done():
			t.Fatal("the actual recovery operation exceeded the fixture deadline")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (f *generationStartupFixture) requestedRestore(t *testing.T, g *generation, actor identity.Principal) recovery.OperationView {
	t.Helper()
	passphrase := []byte("actual-startup-control-recovery-passphrase")
	defer clear(passphrase)
	created, err := g.manager.Create(f.ctx, actor, recovery.CreateRequest{RequestId: generationStartupID(t), Passphrase: passphrase})
	if err != nil {
		t.Fatalf("create the actual source archive: error_type=%T", err)
	}
	created = f.waitOperation(t, g.manager, actor, created.Id, "completed")
	archive, err := g.manager.Backup(f.ctx, actor, created.BackupId)
	if err != nil {
		t.Fatal("inspect the actual source archive receipt")
	}
	plan, err := g.manager.Plan(f.ctx, actor, recovery.PlanRequest{RequestId: generationStartupID(t), BackupId: archive.Id, SHA256: archive.SHA256,
		Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: "0"})
	if err != nil {
		t.Fatalf("stage the actual recovery database: error_type=%T", err)
	}
	plan = f.waitOperation(t, g.manager, actor, plan.Id, "ready")
	if _, err := g.manager.Apply(f.ctx, actor, plan.Id, recovery.ApplyRequest{Revision: plan.Revision, GenerationRevision: "0"}); err != nil {
		t.Fatalf("authorize the actual staged recovery transition: error_type=%T", err)
	}
	return plan
}
