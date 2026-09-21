package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPAdminRuntimeResourcesNativeAuthorityAndSafeBoundedProjection(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	path := "/admin/v1/runtime/resources"
	denied := f.request(t, http.MethodGet, path, nil, nil)
	expectStatus(t, denied, http.StatusUnauthorized)
	if denied.Header().Get("Cache-Control") != "no-store" || denied.Header().Get("Pragma") != "no-cache" {
		t.Fatal("unauthorized resource observations were cacheable")
	}
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	token := stringValue(t, emby, "AccessToken")
	for _, asCookie := range []bool{false, true} {
		headers := http.Header{"X-Emby-Token": {token}}
		var cookies []*http.Cookie
		if asCookie {
			headers = nil
			cookies = []*http.Cookie{{Name: sessionCookie, Value: token}}
		}
		expectStatus(t, f.request(t, http.MethodGet, path, nil, headers, cookies...), http.StatusUnauthorized)
	}
	actor := identity.Principal{User: identity.User{ID: "private-owner-marker"}, SessionID: "private-session-marker", Kind: "emby"}
	_, leave, err := f.app.originals.enterSource(actor, "item-runtime", "mediasource_item-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	response := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("authorized resource observations were cacheable")
	}
	body := jsonObject(t, response)
	if len(body) != 4 || len(objectValue(t, body, "StorageObservations")) != 2 || len(objectValue(t, body, "OriginalStreams")) != 11 {
		t.Fatal("runtime endpoint exposed an undocumented root or original-stream field")
	}
	pool := objectValue(t, body, "DatabasePool")
	if len(pool) != 10 {
		t.Fatal("pool snapshot exposed an undocumented field")
	}
	for _, field := range []string{"MaxConns", "TotalConns", "IdleConns", "AcquiredConns", "ConstructingConns"} {
		value, ok := pool[field].(float64)
		if !ok || value < 0 || float64(int32(value)) != value {
			t.Fatal("pool gauge was not a bounded JSON integer")
		}
	}
	for _, field := range []string{"AcquireCount", "AcquireDurationNanoseconds", "EmptyAcquireCount", "EmptyAcquireWaitNanoseconds", "CanceledAcquireCount"} {
		value, ok := pool[field].(string)
		if !ok {
			t.Fatal("pool cumulative measurement was not an exact decimal string")
		}
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil || number < 0 || strconv.FormatInt(number, 10) != value {
			t.Fatal("pool cumulative measurement was not canonical")
		}
	}
	originals := objectValue(t, body, "OriginalStreams")
	current, ok := originals["Current"].([]any)
	if !ok || len(current) != 1 || len(current[0].(map[string]any)) != 8 {
		t.Fatal("active source lease shape is not closed")
	}
	lease := current[0].(map[string]any)
	if lease["ItemId"] != "item-runtime" || lease["MediaSourceId"] != "mediasource_item-runtime" || lease["Active"] != true || lease["CompletedUnixNano"] != "0" {
		t.Fatal("endpoint did not project the actual registered lease")
	}
	for _, secret := range []string{"private-owner-marker", "private-session-marker", cookie.Value, token, f.cfg.DatabaseURL,
		"Directory", "URL", "Session", "Token", "Path", "Password"} {
		if secret != "" && strings.Contains(response.Body.String(), secret) {
			t.Fatal("runtime observation exposed request, credential or filesystem metadata")
		}
	}
	bad := f.request(t, http.MethodGet, path+"?private-query-marker=1", nil, nil, cookie)
	expectStatus(t, bad, http.StatusBadRequest)
	if strings.Contains(bad.Body.String(), "private-query-marker") {
		t.Fatal("resource endpoint reflected a rejected query")
	}
	leave()
	final := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, path, nil, nil, cookie)), "OriginalStreams")
	completed := final["Completed"].([]any)
	if len(completed) != 1 || completed[0].(map[string]any)["LeaseId"] != lease["LeaseId"] || completed[0].(map[string]any)["Active"] != false {
		t.Fatal("completed projection lost the exact active lease identity")
	}
	// Exercise missing deployment inventory through the real authorization
	// wrapper without mutating the concurrently running fixture's pool pointer.
	unavailableServer := &Server{cfg: f.cfg, identity: f.users, originals: f.app.originals, library: f.app.library}
	mux := http.NewServeMux()
	unavailableServer.registerAdminRuntimeResourcesRoutes(mux)
	request := httptest.NewRequest(http.MethodGet, path, nil).WithContext(f.ctx)
	request.AddCookie(cookie)
	unavailable := httptest.NewRecorder()
	mux.ServeHTTP(unavailable, request)
	expectStatus(t, unavailable, http.StatusServiceUnavailable)
	if unavailable.Header().Get("Cache-Control") != "no-store" || unavailable.Header().Get("Pragma") != "no-cache" {
		t.Fatal("unavailable pool snapshot was cacheable")
	}
}

func TestRuntimeDatabasePoolSnapshotObservesRealWaitAndCancellationWithoutAcquiring(t *testing.T) {
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for actual pool wait observations")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse owned integration pool configuration")
	}
	configuration.MinConns, configuration.MaxConns = 0, 1
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal("open owned integration pool")
	}
	defer pool.Close()
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("acquire the sole actual connection")
	}
	defer held.Release()
	before := databasePoolResources(pool.Stat())
	if before.MaxConns != 1 || before.TotalConns != 1 || before.AcquiredConns != 1 || before.IdleConns != 0 || before.ConstructingConns != 0 {
		t.Fatal("test did not occupy the sole pool connection")
	}
	waiting, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	if connection, err := pool.Acquire(waiting); !errors.Is(err, context.DeadlineExceeded) || connection != nil {
		if connection != nil {
			connection.Release()
		}
		t.Fatalf("an acquire against the full pool did not actually time out: %v", err)
	}
	afterCancellation := databasePoolResources(pool.Stat())
	beforeCanceled, _ := strconv.ParseInt(before.CanceledAcquireCount, 10, 64)
	if afterCancellation.CanceledAcquireCount != strconv.FormatInt(beforeCanceled+1, 10) || afterCancellation.AcquireCount != before.AcquireCount {
		t.Fatal("canceled acquire was counted as a successful acquisition")
	}
	started, acquired := make(chan struct{}), make(chan error, 1)
	go func() {
		close(started)
		connection, err := pool.Acquire(ctx)
		if connection != nil {
			connection.Release()
		}
		acquired <- err
	}()
	<-started
	select {
	case err := <-acquired:
		t.Fatalf("waiter completed while the sole connection remained held: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	// A full pool cannot serve another query. Repeated statistics snapshots must
	// still finish and must not contribute successful acquisitions of their own.
	for index := 0; index < 3; index++ {
		observed := databasePoolResources(pool.Stat())
		if observed != afterCancellation {
			t.Fatal("read-only pool snapshot changed real counters or occupancy")
		}
	}
	held.Release()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal("waiting acquisition failed after release")
		}
	case <-ctx.Done():
		t.Fatal("waiting acquisition did not finish after the real connection release")
	}
	final := databasePoolResources(pool.Stat())
	beforeAcquire, _ := strconv.ParseInt(before.AcquireCount, 10, 64)
	beforeEmpty, _ := strconv.ParseInt(before.EmptyAcquireCount, 10, 64)
	beforeDuration, _ := strconv.ParseInt(before.AcquireDurationNanoseconds, 10, 64)
	beforeWait, _ := strconv.ParseInt(before.EmptyAcquireWaitNanoseconds, 10, 64)
	finalDuration, durationErr := strconv.ParseInt(final.AcquireDurationNanoseconds, 10, 64)
	finalWait, waitErr := strconv.ParseInt(final.EmptyAcquireWaitNanoseconds, 10, 64)
	if final.AcquireCount != strconv.FormatInt(beforeAcquire+1, 10) || final.EmptyAcquireCount != strconv.FormatInt(beforeEmpty+1, 10) ||
		final.CanceledAcquireCount != afterCancellation.CanceledAcquireCount || durationErr != nil || waitErr != nil ||
		finalDuration <= beforeDuration || finalWait <= beforeWait || final.AcquiredConns != 0 || final.IdleConns != 1 {
		t.Fatal("pool snapshot did not retain actual successful wait and cancellation counters")
	}
	raw, err := json.Marshal(final)
	if err != nil || len(raw) > 1024 || strings.Contains(string(raw), databaseURL) {
		t.Fatal("bounded pool DTO exposed configuration or failed encoding")
	}
}
