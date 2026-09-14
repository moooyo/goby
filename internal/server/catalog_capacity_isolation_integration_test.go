package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

// This adjustable baseline is a catalog-only profile, not a release capacity
// commitment. SQL creates synthetic metadata; no physical media, scanner,
// ffprobe, playback, kernel filesystem stall, or native service is exercised.
const (
	catalogCapacityMovies            = 2000
	catalogCapacitySeries            = 20
	catalogCapacitySeasons           = 5
	catalogCapacityEpisodesPerSeason = 20
	catalogCapacityAlbums            = 100
	catalogCapacityTracksPerAlbum    = 10
	catalogCapacityEpisodes          = catalogCapacitySeries * catalogCapacitySeasons * catalogCapacityEpisodesPerSeason
	catalogCapacityAudio             = catalogCapacityAlbums * catalogCapacityTracksPerAlbum
	catalogCapacityLeaves            = catalogCapacityMovies + catalogCapacityEpisodes + catalogCapacityAudio
	catalogCapacityPage              = 64
)

type catalogCapacitySubject struct {
	libraryID, userID, token string
}

func catalogCapacityLeafID(libraryID string, number int) string {
	return fmt.Sprintf("%s-leaf-%05d", libraryID, number)
}

func TestHTTPCatalogCapacityKeepsACLAndUserDataDuringOwnedTransactionBlock(t *testing.T) {
	started := time.Now()
	f, root := newLibraryServerFixture(t)
	adminID := f.bootstrap(t)
	subjects := make([]catalogCapacitySubject, 0, 2)
	seedStarted := time.Now()
	for _, name := range []string{"A", "B"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		collection, err := f.app.library.CreateLibrary(f.ctx, "Capacity "+name, "mixed", []string{path})
		if err != nil {
			t.Fatalf("create owned capacity library: %v", err)
		}
		user, err := f.users.CreateUser(f.ctx, "Capacity "+name, "capacity-fixture-password", false)
		if err != nil {
			t.Fatalf("create capacity fixture user: %v", err)
		}
		policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{collection.ID},
			"EnableMediaPlayback": true, "EnablePlaybackRemuxing": false,
			"EnableAudioPlaybackTranscoding": false, "EnableVideoPlaybackTranscoding": false})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", user.ID, policy); err != nil {
			t.Fatalf("set fixture library policy before login: %v", err)
		}
		catalogCapacitySeed(t, f, collection.ID, path)
		login := f.embyLogin(t, user.Name, "capacity-fixture-password")
		subjects = append(subjects, catalogCapacitySubject{collection.ID, user.ID, stringValue(t, login, "AccessToken")})
	}
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	admin := catalogCapacitySubject{userID: adminID, token: stringValue(t, adminLogin, "AccessToken")}
	// A shared persisted artist spans both ACL scopes. The two credit roles
	// must not duplicate Audio item results or grant the other library.
	var artistID int64
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO catalog_entities(kind,name)
		VALUES ('MusicArtist','Capacity Ensemble') RETURNING id`).Scan(&artistID); err != nil {
		t.Fatalf("create shared capacity artist: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO item_entities
		(item_id,entity_id,position,display_name,credit_type,credit_group)
		SELECT i.id,$1,1,'Capacity Ensemble',credit.kind,credit.group_number
		FROM items i CROSS JOIN (VALUES ('Artist',1),('AlbumArtist',2)) credit(kind,group_number)
		WHERE i.library_id=ANY($2::text[]) AND i.type IN ('Audio','MusicAlbum')`, artistID,
		[]string{subjects[0].libraryID, subjects[1].libraryID}); err != nil {
		t.Fatalf("index shared artist credit roles: %v", err)
	}
	seededNumbers := []int{1, catalogCapacityMovies + 1, catalogCapacityMovies + catalogCapacityEpisodes + 1,
		catalogCapacityLeaves - 1, catalogCapacityLeaves}
	expectedData := make(map[string]map[string]any)
	for _, subject := range subjects {
		for index, number := range seededNumbers {
			id := catalogCapacityLeafID(subject.libraryID, number)
			date := time.Date(2024, 2, 3, 4, index, 6, 7000, time.UTC)
			position, count := int64(index)*1_000_000, index+3
			favorite, played := index%2 == 0, index%2 == 1
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data
				(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7)`, subject.userID, id, position, count, favorite, played, date); err != nil {
				t.Fatalf("seed existing synthetic UserData: %v", err)
			}
			expectedData[id] = map[string]any{"PlaybackPositionTicks": float64(position), "PlayCount": float64(count),
				"IsFavorite": favorite, "Played": played, "LastPlayedDate": date.Format(time.RFC3339Nano)}
		}
	}
	var leaves, folders, schemaBytes int64
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER (WHERE NOT is_folder),
		count(*) FILTER (WHERE is_folder) FROM items`).Scan(&leaves, &folders); err != nil {
		t.Fatal(err)
	}
	wantFolders := int64(2 * (1 + catalogCapacitySeries + catalogCapacitySeries*catalogCapacitySeasons + catalogCapacityAlbums))
	if leaves != 2*catalogCapacityLeaves || folders != wantFolders {
		t.Fatalf("capacity seed counts = %d leaves/%d folders, want %d/%d", leaves, folders, 2*catalogCapacityLeaves, wantFolders)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(sum(pg_total_relation_size(c.oid)),0)::bigint
		FROM pg_class c WHERE c.relnamespace=current_schema()::regnamespace AND c.relkind IN ('r','p')`).Scan(&schemaBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("catalog_capacity_profile leaves=%d folders=%d libraries=2 synthetic_media_bytes=0 schema_allocated_bytes=%d seed_elapsed_ms=%d",
		leaves, folders, schemaBytes, time.Since(seedStarted).Milliseconds())

	readCtx, cancelReads := context.WithCancel(f.ctx)
	var reads sync.WaitGroup
	t.Cleanup(func() {
		cancelReads()
		joined := make(chan struct{})
		go func() { reads.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(5 * time.Second):
			t.Error("capacity HTTP readers did not close after their contexts and lock gate were released")
		}
	})
	request := func(ctx context.Context, subject catalogCapacitySubject, target string) (*httptest.ResponseRecorder, time.Duration) {
		before := time.Now()
		r := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		r.Header.Set("X-Emby-Token", subject.token)
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, r)
		return response, time.Since(before)
	}
	target := func(subject catalogCapacitySubject, offset, limit int) string {
		query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode,Audio"}, "SortBy": {"SortName"},
			"StartIndex": {strconv.Itoa(offset)}, "Limit": {strconv.Itoa(limit)}}
		return "/emby/Users/" + subject.userID + "/Items?" + query.Encode()
	}
	assertPage := func(response *httptest.ResponseRecorder, subject catalogCapacitySubject, offset, limit int) {
		t.Helper()
		if response.Body.Len() > 512<<10 {
			t.Fatal("bounded capacity page exceeded the fixture response-byte ceiling")
		}
		items, total := responseItems(t, response)
		if total != catalogCapacityLeaves || len(items) != limit {
			t.Fatalf("capacity page total/length = %d/%d, want %d/%d", total, len(items), catalogCapacityLeaves, limit)
		}
		for index, item := range items {
			number := offset + index + 1
			id := catalogCapacityLeafID(subject.libraryID, number)
			kind := "Movie"
			if number > catalogCapacityMovies {
				kind = "Episode"
			}
			if number > catalogCapacityMovies+catalogCapacityEpisodes {
				kind = "Audio"
			}
			if item["Id"] != id || item["Name"] != fmt.Sprintf("Item %05d", number) || item["Type"] != kind || item["IsFolder"] != false {
				t.Fatalf("capacity page lost its exact authorized sequence at item %d", number)
			}
			assertNoItemPaths(t, item, root)
			if expected, exists := expectedData[id]; exists {
				catalogCapacityAssertData(t, item["UserData"], expected)
			}
		}
	}
	var samples []time.Duration
	assertQueries := func(label string) {
		t.Helper()
		for _, subject := range subjects {
			for kind, count := range map[string]int{"Movie": catalogCapacityMovies, "Episode": catalogCapacityEpisodes, "Audio": catalogCapacityAudio} {
				query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {kind}, "Limit": {"0"}}
				response, _ := request(readCtx, subject, "/emby/Users/"+subject.userID+"/Items?"+query.Encode())
				items, total := responseItems(t, response)
				if len(items) != 0 || total != count {
					t.Fatalf("capacity %s count-only result = %d/%d, want %d/0", kind, total, len(items), count)
				}
			}
			for _, page := range [][2]int{{0, catalogCapacityPage}, {catalogCapacityLeaves / 2, catalogCapacityPage},
				{catalogCapacityLeaves - catalogCapacityPage, catalogCapacityPage}, {0, 0}} {
				ctx, cancel := context.WithTimeout(readCtx, 5*time.Second)
				response, elapsed := request(ctx, subject, target(subject, page[0], page[1]))
				cancel()
				assertPage(response, subject, page[0], page[1])
				samples = append(samples, elapsed)
			}
			for _, filter := range []string{"ArtistIds", "AlbumArtistIds"} {
				query := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Audio"}, "Limit": {"0"},
					filter: {strconv.FormatInt(artistID, 10)}}
				response, _ := request(readCtx, subject, "/emby/Users/"+subject.userID+"/Items?"+query.Encode())
				items, total := responseItems(t, response)
				if len(items) != 0 || total != catalogCapacityAudio {
					t.Fatalf("shared music filter %s exposed another library or duplicate credit roles", filter)
				}
			}
			catalogCapacityAssertHierarchy(t, f, subject, expectedData)
		}
		response, _ := request(readCtx, admin, target(admin, 0, 0))
		items, total := responseItems(t, response)
		if len(items) != 0 || total != 2*catalogCapacityLeaves {
			t.Fatalf("administrator count-only result = %d/%d", total, len(items))
		}
		for index, subject := range subjects {
			other := subjects[1-index]
			response, _ := request(readCtx, subject, "/emby/Users/"+subject.userID+"/Items/"+catalogCapacityLeafID(other.libraryID, 1))
			expectStatus(t, response, http.StatusNotFound)
			response, _ = request(readCtx, subject, target(other, 0, 0))
			expectStatus(t, response, http.StatusForbidden)
		}
		t.Logf("catalog_capacity_checkpoint=%s elapsed_ms=%d", label, time.Since(started).Milliseconds())
	}
	dataSnapshot := func() string {
		t.Helper()
		var snapshot string
		if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('row',to_jsonb(d),
			'xmin',d.xmin::text) ORDER BY d.user_id,d.item_id),'[]'::jsonb)::text FROM user_item_data d`).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	beforeData := dataSnapshot()
	assertQueries("before_database_block")

	const gateKey = "catalog-capacity-isolation-gate"
	if _, err := f.pool.Exec(f.ctx, "INSERT INTO server_settings(key,value) VALUES ($1,'original')", gateKey); err != nil {
		t.Fatal(err)
	}
	gate, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	caller, cancelCaller := context.WithCancel(f.ctx)
	finished := make(chan error, 1)
	ownerIDs := make(chan int32, 1)
	launched, joined, released := false, false, false
	releaseGate := func() error {
		if released {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := gate.Rollback(ctx)
		if err == nil || errors.Is(err, pgx.ErrTxClosed) {
			released = true
			return nil
		}
		return err
	}
	// This runs before the HTTP-reader and server fixture cleanup. Even an
	// assertion failure releases the gate before joining the owned transaction.
	t.Cleanup(func() {
		cancelCaller()
		if err := releaseGate(); err != nil {
			t.Errorf("release owned capacity row lock: %v", err)
		}
		if launched && !joined {
			select {
			case <-finished:
			case <-time.After(25 * time.Second):
				t.Error("capacity owned transaction did not finish within its protected transaction and cleanup bounds")
			}
		}
	})
	var gatePID int32
	if err := gate.QueryRow(f.ctx, "SELECT pg_backend_pid()").Scan(&gatePID); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Exec(f.ctx, "UPDATE server_settings SET value='gate-uncommitted' WHERE key=$1", gateKey); err != nil {
		t.Fatal(err)
	}
	launched = true
	go func() {
		finished <- f.app.library.WithOwnedTx(caller, func(tx library.OwnedTx) error {
			var pid int32
			if err := tx.QueryRow("SELECT pg_backend_pid()").Scan(&pid); err != nil {
				return err
			}
			ownerIDs <- pid
			if tag, err := tx.Exec(`UPDATE user_item_data SET is_favorite=NOT is_favorite,
				play_count=play_count+1,updated_at=clock_timestamp() WHERE user_id=$1 AND item_id=$2`,
				subjects[0].userID, catalogCapacityLeafID(subjects[0].libraryID, 1)); err != nil || tag.RowsAffected() != 1 {
				return fmt.Errorf("stage one uncommitted UserData row: count=%d error=%v", tag.RowsAffected(), err)
			}
			if tag, err := tx.Exec("UPDATE server_settings SET value='owner-uncommitted' WHERE key=$1", gateKey); err != nil || tag.RowsAffected() != 1 {
				return fmt.Errorf("pass the owned gate row: count=%d error=%v", tag.RowsAffected(), err)
			}
			// WithOwnedTx protects already-started SQL from caller cancellation.
			// Explicitly reject the callback after the gate opens so it rolls back.
			return caller.Err()
		})
	}()
	blockCtx, cancelBlock := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancelBlock()
	var ownerPID int32
	select {
	case ownerPID = <-ownerIDs:
	case err := <-finished:
		joined = true
		t.Fatalf("owned callback ended before exposing its backend: %v", err)
	case <-blockCtx.Done():
		t.Fatal("owned callback did not start within the bounded block profile")
	}
	waitBlocked := func() {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked bool
			if err := f.pool.QueryRow(blockCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
				WHERE pid=$1 AND wait_event_type='Lock' AND $2::integer=ANY(pg_blocking_pids(pid)))`, ownerPID, gatePID).Scan(&blocked); err != nil {
				t.Fatalf("observe this fixture's exact row-lock wait: %v", err)
			}
			if blocked {
				return
			}
			select {
			case err := <-finished:
				joined = true
				t.Fatalf("owned callback ended before the row-lock wait: %v", err)
			case <-ticker.C:
			case <-blockCtx.Done():
				t.Fatal("owned callback never reached the expected row-lock wait")
			}
		}
	}
	waitBlocked()
	blockedAt := time.Now()
	if dataSnapshot() != beforeData {
		t.Fatal("uncommitted owned UserData leaked into a concurrent read")
	}
	type pageResult struct {
		offset, limit int
		response      *httptest.ResponseRecorder
		elapsed       time.Duration
	}
	results := make(chan pageResult, 4)
	other := subjects[1]
	for _, page := range [][2]int{{0, catalogCapacityPage}, {catalogCapacityLeaves / 2, catalogCapacityPage},
		{catalogCapacityLeaves - catalogCapacityPage, catalogCapacityPage}, {0, 0}} {
		reads.Add(1)
		go func(offset, limit int) {
			defer reads.Done()
			response, elapsed := request(blockCtx, other, target(other, offset, limit))
			results <- pageResult{offset, limit, response, elapsed}
		}(page[0], page[1])
	}
	for range 4 {
		select {
		case result := <-results:
			assertPage(result.response, other, result.offset, result.limit)
			samples = append(samples, result.elapsed)
		case <-blockCtx.Done():
			t.Fatal("independent catalog reads did not finish while the other library's owned transaction was blocked")
		}
	}
	cancelCaller()
	waitBlocked()
	if dataSnapshot() != beforeData {
		t.Fatal("cancelling the blocked caller changed committed UserData")
	}
	if err := releaseGate(); err != nil {
		t.Fatal(err)
	}
	blockedFor := time.Since(blockedAt)
	select {
	case err := <-finished:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the explicitly rejected owned callback did not roll back with caller cancellation: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("released owned callback did not finish within its transaction/cleanup bounds")
	}
	cancelBlock()
	var currentPID int32
	if err := f.app.library.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		return tx.QueryRow("SELECT pg_backend_pid()").Scan(&currentPID)
	}); err != nil || currentPID != ownerPID {
		t.Fatalf("the same catalog owner connection was not reusable after rollback: %v", err)
	}
	var gateValue string
	var remainingGateWaits int
	if err := f.pool.QueryRow(f.ctx, "SELECT value FROM server_settings WHERE key=$1", gateKey).Scan(&gateValue); err != nil || gateValue != "original" {
		t.Fatalf("the two released transactions changed committed gate state: %v", err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock'
		AND $2::integer=ANY(pg_blocking_pids(pid))`, ownerPID, gatePID).Scan(&remainingGateWaits); err != nil || remainingGateWaits != 0 {
		t.Fatalf("the released gate still blocks the catalog owner: count=%d, error=%v", remainingGateWaits, err)
	}
	assertQueries("after_cancel_release_and_rollback")
	if dataSnapshot() != beforeData {
		t.Fatal("capacity queries or rollback rewrote an existing UserData field, timestamp or xmin")
	}
	for _, path := range []string{filepath.Join(root, "A"), filepath.Join(root, "B")} {
		entries, err := os.ReadDir(path)
		if err != nil || len(entries) != 0 {
			t.Fatalf("catalog-only work wrote media files: entries=%d, error=%v", len(entries), err)
		}
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
	err = f.app.Close(closeCtx)
	cancelClose()
	if err != nil {
		t.Fatalf("close capacity HTTP fixture and catalog owner: %v", err)
	}
	if dataSnapshot() != beforeData {
		t.Fatal("closing the HTTP fixture changed existing UserData or row versions")
	}
	if acquired := f.pool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("closed capacity fixture retained %d acquired PostgreSQL connections", acquired)
	}
	var totalRead, longest time.Duration
	for _, elapsed := range samples {
		totalRead += elapsed
		if elapsed > longest {
			longest = elapsed
		}
	}
	t.Logf("catalog_capacity_observed measured_page_requests=%d total_page_read_ms=%d max_page_read_ms=%d block_profile_ms=%d total_test_ms=%d",
		len(samples), totalRead.Milliseconds(), longest.Milliseconds(), blockedFor.Milliseconds(), time.Since(started).Milliseconds())
	t.Log("catalog_capacity_boundary=synthetic_SQL_catalog_and_real_PostgreSQL_row_lock; no_latency_SLO_scanner_throughput_or_kernel_IO_claim")
}

func catalogCapacityAssertData(t *testing.T, actual any, expected map[string]any) {
	t.Helper()
	left, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(expected)
	if err != nil || string(left) != string(right) {
		t.Fatal("capacity Item UserData differs from its existing synthetic row")
	}
}

func catalogCapacityAssertHierarchy(t *testing.T, f *serverFixture, subject catalogCapacitySubject, expectedData map[string]map[string]any) {
	t.Helper()
	headers := http.Header{"X-Emby-Token": {subject.token}}
	series := subject.libraryID + "-series-001"
	season := subject.libraryID + "-season-001-1"
	album := subject.libraryID + "-album-001"
	seasons, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Shows/"+series+"/Seasons?UserId="+subject.userID, nil, headers))
	if total != catalogCapacitySeasons || len(seasons) != catalogCapacitySeasons {
		t.Fatal("representative series lost its five persisted seasons")
	}
	for index, item := range seasons {
		if item["Id"] != fmt.Sprintf("%s-season-001-%d", subject.libraryID, index+1) || item["ParentId"] != series || item["SeriesId"] != series {
			t.Fatal("representative season order or parent identity changed")
		}
	}
	episodes, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Shows/"+series+"/Episodes?UserId="+subject.userID+"&SeasonId="+season, nil, headers))
	if total != catalogCapacityEpisodesPerSeason || len(episodes) != catalogCapacityEpisodesPerSeason {
		t.Fatal("representative season lost its twenty episodes")
	}
	for index, item := range episodes {
		if item["Id"] != catalogCapacityLeafID(subject.libraryID, catalogCapacityMovies+index+1) || item["ParentId"] != season || item["SeriesId"] != series || item["SeasonId"] != season {
			t.Fatal("representative episode hierarchy changed")
		}
	}
	albumResponse := f.request(t, http.MethodGet, "/emby/Users/"+subject.userID+"/Items/"+album, nil, headers)
	expectStatus(t, albumResponse, http.StatusOK)
	detail := jsonObject(t, albumResponse)
	if detail["Type"] != "MusicAlbum" || detail["ChildCount"] != float64(catalogCapacityTracksPerAlbum) {
		t.Fatal("representative album has the wrong direct-child count")
	}
	tracks, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Users/"+subject.userID+"/Items?ParentId="+album+"&SortBy=SortName", nil, headers))
	if total != catalogCapacityTracksPerAlbum || len(tracks) != catalogCapacityTracksPerAlbum {
		t.Fatal("representative album lost its ten physical-parent catalog tracks")
	}
	for index, item := range tracks {
		if item["Id"] != catalogCapacityLeafID(subject.libraryID, catalogCapacityMovies+catalogCapacityEpisodes+index+1) || item["ParentId"] != album || item["AlbumId"] != album {
			t.Fatal("representative track album identity changed")
		}
	}
	for _, number := range []int{1, catalogCapacityMovies + 1, catalogCapacityMovies + catalogCapacityEpisodes + 1} {
		id := catalogCapacityLeafID(subject.libraryID, number)
		response := f.request(t, http.MethodGet, "/emby/Users/"+subject.userID+"/Items/"+id, nil, headers)
		expectStatus(t, response, http.StatusOK)
		catalogCapacityAssertData(t, jsonObject(t, response)["UserData"], expectedData[id])
	}
}

func catalogCapacitySeed(t *testing.T, f *serverFixture, libraryID, path string) {
	t.Helper()
	var rootID string
	if err := f.pool.QueryRow(f.ctx, "SELECT id FROM library_roots WHERE library_id=$1", libraryID).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	insert := func(want int64, statement string, arguments ...any) {
		t.Helper()
		tag, err := f.pool.Exec(f.ctx, statement, arguments...)
		if err != nil || tag.RowsAffected() != want {
			t.Fatalf("seed bounded capacity rows: inserted=%d want=%d error=%v", tag.RowsAffected(), want, err)
		}
	}
	insert(catalogCapacitySeries, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT $1||'-series-'||lpad(n::text,3,'0'),$1,$2,$1,'Series '||n,'series '||lpad(n::text,3,'0'),'Series',true,
		$3||'/series/'||lpad(n::text,3,'0'),'series/'||lpad(n::text,3,'0') FROM generate_series(1,$4::integer) n`,
		libraryID, rootID, path, catalogCapacitySeries)
	insert(catalogCapacitySeries*catalogCapacitySeasons, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,index_number)
		SELECT $1||'-season-'||lpad(series_number::text,3,'0')||'-'||season,$1,$2,$1||'-series-'||lpad(series_number::text,3,'0'),
		'Season '||season,'season '||season,'Season',true,$3||'/series/'||lpad(series_number::text,3,'0')||'/season-'||season,
		'series/'||lpad(series_number::text,3,'0')||'/season-'||season,season
		FROM generate_series(1,$4::integer) series_number CROSS JOIN generate_series(1,$5::integer) season`,
		libraryID, rootID, path, catalogCapacitySeries, catalogCapacitySeasons)
	insert(catalogCapacityAlbums, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path)
		SELECT $1||'-album-'||lpad(n::text,3,'0'),$1,$2,$1,'Album '||n,'album '||lpad(n::text,3,'0'),'MusicAlbum',true,
		$3||'/albums/'||lpad(n::text,3,'0'),'albums/'||lpad(n::text,3,'0') FROM generate_series(1,$4::integer) n`,
		libraryID, rootID, path, catalogCapacityAlbums)
	insert(catalogCapacityLeaves, `WITH numbered AS (
		SELECT n, CASE WHEN n<=$5::integer THEN 'Movie' WHEN n<=$6::integer THEN 'Episode' ELSE 'Audio' END AS kind,
		(n-$5::integer-1)/$7::integer+1 AS show_number,
		((n-$5::integer-1)%$7::integer)/$8::integer+1 AS season_number,
		(n-$5::integer-1)%$8::integer+1 AS episode_number,
		(n-$6::integer-1)/$9::integer+1 AS album_number,(n-$6::integer-1)%$9::integer+1 AS track_number
		FROM generate_series(1,$4::integer) n
	), named AS (
		SELECT *, CASE kind WHEN 'Movie' THEN 'movies/'||lpad(n::text,5,'0')||'.mp4'
		WHEN 'Episode' THEN 'series/'||lpad(show_number::text,3,'0')||'/season-'||season_number||'/episode-'||episode_number||'.mp4'
		ELSE 'albums/'||lpad(album_number::text,3,'0')||'/track-'||track_number||'.flac' END AS relative
		FROM numbered
	) INSERT INTO items (id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path,
		index_number,parent_index_number,media)
		SELECT $1||'-leaf-'||lpad(n::text,5,'0'),$1,$2,
		CASE kind WHEN 'Movie' THEN $1 WHEN 'Episode' THEN $1||'-season-'||lpad(show_number::text,3,'0')||'-'||season_number
		ELSE $1||'-album-'||lpad(album_number::text,3,'0') END,
		'Item '||lpad(n::text,5,'0'),'item '||lpad(n::text,5,'0'),kind,false,$3||'/'||relative,relative,
		CASE kind WHEN 'Episode' THEN episode_number WHEN 'Audio' THEN track_number ELSE 0 END,
		CASE kind WHEN 'Episode' THEN season_number ELSE 0 END,
		jsonb_build_object('Container',CASE kind WHEN 'Audio' THEN 'flac' ELSE 'mp4' END,'DurationTicks',1250000000)
		FROM named`, libraryID, rootID, path, catalogCapacityLeaves, catalogCapacityMovies,
		catalogCapacityMovies+catalogCapacityEpisodes, catalogCapacitySeasons*catalogCapacityEpisodesPerSeason,
		catalogCapacityEpisodesPerSeason, catalogCapacityTracksPerAlbum)
}
