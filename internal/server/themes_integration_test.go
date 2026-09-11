package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type themeHTTPFixture struct {
	*serverFixture
	viewerID, otherID     string
	headers, otherHeaders http.Header
}

func newThemeHTTPFixture(t *testing.T) *themeHTTPFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("indexed theme HTTP fixtures use Linux catalog paths")
	}
	f := &themeHTTPFixture{serverFixture: newServerFixture(t)}
	f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Theme viewer", "theme-viewer-password", false)
	if err != nil {
		t.Fatal("create theme viewer")
	}
	other, err := f.users.CreateUser(f.ctx, "Other theme viewer", "other-theme-password", false)
	if err != nil {
		t.Fatal("create independent theme viewer")
	}
	f.viewerID, f.otherID = viewer.ID, other.ID
	// These are indexed metadata fixtures only. No scan, file probe, image
	// download, playback negotiation, or media delivery occurs in this suite.
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('theme-visible','Visible Themes','mixed'),('theme-hidden','Hidden Themes','mixed');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('theme-root-visible','theme-visible','/theme-http/visible','/theme-http','visible'),
		('theme-root-hidden','theme-hidden','/theme-http/hidden','/theme-http','hidden');
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path,path) VALUES
		('th-movie','theme-visible','theme-root-visible',NULL,'Theme Movie','theme movie','Movie',false,'Film/movie.mp4','/theme-http/visible/Film/movie.mp4'),
		('th-series','theme-visible','theme-root-visible',NULL,'Theme Series','theme series','Series',true,'Series','/theme-http/visible/Series'),
		('th-season','theme-visible','theme-root-visible','th-series','Season','season','Season',true,'Series/Season 01','/theme-http/visible/Series/Season 01'),
		('th-episode','theme-visible','theme-root-visible','th-season','Episode','episode','Episode',false,'Series/Season 01/episode.mp4','/theme-http/visible/Series/Season 01/episode.mp4'),
		('th-empty','theme-visible','theme-root-visible',NULL,'Empty','empty','Movie',false,'Empty/movie.mp4','/theme-http/visible/Empty/movie.mp4'),
		('th-private','theme-hidden','theme-root-hidden',NULL,'Private','private','Movie',false,'Private/movie.mp4','/theme-http/hidden/Private/movie.mp4'),
		('th-song','theme-visible','theme-root-visible','th-movie','theme','theme','Audio',false,'Film/theme.mp3','/theme-http/visible/Film/theme.mp3'),
		('th-video','theme-visible','theme-root-visible','th-movie','video','video','Video',false,'Film/backdrops/video.mp4','/theme-http/visible/Film/backdrops/video.mp4'),
		('th-series-song','theme-visible','theme-root-visible','th-series','theme','theme','Audio',false,'Series/theme.mp3','/theme-http/visible/Series/theme.mp3'),
		('th-season-video','theme-visible','theme-root-visible','th-season','video','video','Video',false,'Series/Season 01/backdrops/video.mp4','/theme-http/visible/Series/Season 01/backdrops/video.mp4'),
		('th-private-song','theme-hidden','theme-root-hidden','th-private','private theme','private theme','Audio',false,'Private/theme.mp3','/theme-http/hidden/Private/theme.mp3'),
		('th-retired','theme-visible','theme-root-visible','th-movie','retired theme','retired theme','Audio',false,'Film/theme-music/retired.mp3','/theme-http/visible/Film/theme-music/retired.mp3'),
		('th-reserved','theme-visible','theme-root-visible','th-movie','pending theme','pending theme','Audio',false,'Film/theme-music/pending.mp3','/theme-http/visible/Film/theme-music/pending.mp3');
		INSERT INTO theme_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id IN
		('th-song','th-video','th-series-song','th-season-video','th-private-song','th-retired','th-reserved');
		INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('th-song','th-movie','song',true),('th-video','th-movie','video',true),
		('th-series-song','th-series','song',true),('th-season-video','th-season','video',true),
		('th-private-song','th-private','song',true),('th-retired','th-movie','song',false);
		INSERT INTO item_images(item_id,root_id,image_type,image_index,relative_path,file_identity,source_hash,
		file_size,modified_at,width,height,mime_type) VALUES
		('th-song','theme-root-visible','Primary',0,'Film/song-poster.jpg','theme-image-fixture',repeat('a',64),20,clock_timestamp(),320,180,'image/jpeg'),
		('th-song','theme-root-visible','Backdrop',0,'Film/song-backdrop.jpg','theme-image-fixture',repeat('b',64),20,clock_timestamp(),320,180,'image/jpeg')`); err != nil {
		t.Fatalf("seed theme HTTP catalog: %v", err)
	}
	for userID, libraryID := range map[string]string{viewer.ID: "theme-visible", other.ID: "theme-hidden"} {
		policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{libraryID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", userID, policy); err != nil {
			t.Fatal("set theme fixture library policy")
		}
	}
	for _, item := range []struct {
		id    string
		video bool
	}{
		{"th-song", false}, {"th-video", true}, {"th-series-song", false}, {"th-season-video", true},
		{"th-private-song", false}, {"th-retired", false}, {"th-reserved", false},
	} {
		info := media.Info{Container: "mp3", DurationTicks: 12 * media.TicksPerSecond, Size: 192620, Bitrate: 128000,
			Streams: []media.Stream{{Index: 0, CodecType: "audio", Codec: "mp3", Channels: 2, SampleRate: 48000}}}
		if item.video {
			info.Container = "mp4"
			info.Streams = []media.Stream{{Index: 0, CodecType: "video", Codec: "h264", Width: 320, Height: 180},
				{Index: 1, CodecType: "audio", Codec: "aac", Channels: 2, SampleRate: 48000}}
		}
		raw, err := json.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE items SET media=$2::jsonb WHERE id=$1", item.id, raw); err != nil {
			t.Fatal("seed indexed theme media facts")
		}
	}
	f.headers = http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, viewer.Name, "theme-viewer-password"), "AccessToken")}}
	f.otherHeaders = http.Header{"X-Emby-Token": {stringValue(t, f.embyLogin(t, other.Name, "other-theme-password"), "AccessToken")}}
	return f
}

type themeHTTPGroup struct {
	owner int64
	items []map[string]any
}

func themeHTTPGroups(t *testing.T, response *httptest.ResponseRecorder) map[string]themeHTTPGroup {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatal("theme response is not JSON")
	}
	var wire map[string]struct {
		OwnerID          json.RawMessage  `json:"OwnerId"`
		Items            []map[string]any `json:"Items"`
		TotalRecordCount int              `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil || len(wire) != 3 {
		t.Fatal("theme response omitted its three result groups")
	}
	result := make(map[string]themeHTTPGroup, 3)
	for _, name := range []string{"ThemeSongsResult", "ThemeVideosResult", "SoundtrackSongsResult"} {
		group, exists := wire[name]
		owner, err := strconv.ParseInt(string(group.OwnerID), 10, 64)
		if !exists || err != nil || owner < 0 || strconv.FormatInt(owner, 10) != string(group.OwnerID) ||
			group.Items == nil || group.TotalRecordCount != len(group.Items) {
			t.Fatalf("theme group %s lost an integer OwnerId, non-null Items, or complete count", name)
		}
		result[name] = themeHTTPGroup{owner: owner, items: group.Items}
	}
	if result["SoundtrackSongsResult"].owner != 0 || len(result["SoundtrackSongsResult"].items) != 0 {
		t.Fatal("unindexed soundtrack resources acquired an invented positive result")
	}
	return result
}

func (f *themeHTTPFixture) get(t *testing.T, seed, query string) map[string]themeHTTPGroup {
	t.Helper()
	path := "/emby/Items/" + seed + "/ThemeMedia?UserId=" + f.viewerID
	if query != "" {
		path += "&" + query
	}
	return themeHTTPGroups(t, f.request(t, http.MethodGet, path, nil, f.headers))
}

func (f *themeHTTPFixture) owner(t *testing.T, itemID string) int64 {
	t.Helper()
	if itemID == "" {
		return 0
	}
	var owner int64
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM theme_owner_ids WHERE item_id=$1
		OR ($1=$2 AND virtual_root)`, itemID, library.VirtualRootItemID).Scan(&owner); err != nil || owner <= 0 {
		t.Fatal("read the fixture's durable theme owner")
	}
	return owner
}

func themeHTTPItemIDs(t *testing.T, items []map[string]any) []string {
	t.Helper()
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, stringValue(t, item, "Id"))
	}
	return ids
}

func (f *themeHTTPFixture) state(t *testing.T) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'userdata',(SELECT COALESCE(jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id),'[]'::jsonb) FROM user_item_data u),
		'owners',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM theme_owner_ids o),
		'owner_sequence',(SELECT jsonb_build_array(last_value,is_called) FROM theme_owner_ids_id_seq),
		'resources',(SELECT jsonb_agg(to_jsonb(r) ORDER BY resource_item_id) FROM item_theme_resources r),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m))::text`).Scan(&state); err != nil {
		t.Fatal("read theme HTTP preservation snapshot")
	}
	return state
}

func TestHTTPThemeMediaDefaultsInheritanceAndIndependentDisables(t *testing.T) {
	f := newThemeHTTPFixture(t)
	before := f.state(t)
	for _, test := range []struct {
		seed, query, songOwner, videoOwner string
		songs, videos                      []string
	}{
		{"th-movie", "", "th-movie", "th-movie", []string{"th-song"}, []string{"th-video"}},
		{"th-episode", "", "th-episode", "th-episode", []string{}, []string{}},
		{"th-episode", "InheritFromParent=true", "th-series", "th-season", []string{"th-series-song"}, []string{"th-season-video"}},
		{"th-episode", "InheritFromParent=true&EnableThemeSongs=false", "", "th-season", []string{}, []string{"th-season-video"}},
		{"th-episode", "InheritFromParent=true&EnableThemeVideos=false", "th-series", "", []string{"th-series-song"}, []string{}},
		{"th-movie", "EnableThemeSongs=false&EnableThemeVideos=false", "", "", []string{}, []string{}},
		{"th-empty", "InheritFromParent=true", library.VirtualRootItemID, library.VirtualRootItemID, []string{}, []string{}},
	} {
		groups := f.get(t, test.seed, test.query)
		if songs, videos := groups["ThemeSongsResult"], groups["ThemeVideosResult"]; songs.owner != f.owner(t, test.songOwner) || videos.owner != f.owner(t, test.videoOwner) ||
			!reflect.DeepEqual(themeHTTPItemIDs(t, songs.items), test.songs) || !reflect.DeepEqual(themeHTTPItemIDs(t, videos.items), test.videos) {
			t.Fatalf("theme defaults or independently inherited groups differ for %s?%s", test.seed, test.query)
		}
	}
	if f.state(t) != before {
		t.Fatal("theme GET allocated identifiers or created playback/metadata state")
	}
	var rows int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM user_item_data").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("default UserData projection created persistent user-item rows")
	}
}

func TestHTTPThemeMediaFieldsExtraTypesAndProjectionSwitches(t *testing.T) {
	f := newThemeHTTPFixture(t)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played)
		VALUES($1,'th-song',30000000,3,true,false),($2,'th-song',50000000,7,false,true)`, f.viewerID, f.otherID); err != nil {
		t.Fatal("seed independent existing theme user data")
	}
	before := f.state(t)
	groups := f.get(t, "th-movie", "")
	for _, test := range []struct{ group, itemID, kind, extra string }{
		{"ThemeSongsResult", "th-song", "Audio", "ThemeSong"},
		{"ThemeVideosResult", "th-video", "Video", "ThemeVideo"},
	} {
		item := groups[test.group].items[0]
		if item["Id"] != test.itemID || item["Type"] != test.kind || item["MediaType"] != test.kind ||
			item["ExtraType"] != test.extra || item["ParentId"] != "th-movie" {
			t.Fatal("theme resource was relabeled as its owner or as a Movie")
		}
		// Positive reference captures selected these fields explicitly. The
		// default here follows Goby's ordinary list projection, not detail=true.
		for _, name := range []string{"Path", "MediaSources", "MediaStreams", "Overview", "Genres", "PrimaryImageAspectRatio"} {
			if _, exists := item[name]; exists {
				t.Fatalf("default theme projection forced detail field %s", name)
			}
		}
		assertNoItemPaths(t, item, "/theme-http")
		if objectValue(t, item, "UserData")["Played"] != false {
			t.Fatal("theme resource did not retain the selected user's unplayed state")
		}
	}
	song := groups["ThemeSongsResult"].items[0]
	data := objectValue(t, song, "UserData")
	if data["PlayCount"] != float64(3) || data["PlaybackPositionTicks"] != float64(30000000) || data["IsFavorite"] != true {
		t.Fatal("theme UserData projection ignored the selected user's existing state")
	}
	if objectValue(t, song, "ImageTags")["Primary"] != strings.Repeat("a", 64) ||
		!reflect.DeepEqual(song["BackdropImageTags"], []any{strings.Repeat("b", 64)}) {
		t.Fatal("active theme resource did not receive its indexed artwork metadata")
	}
	fields := url.Values{"Fields": {"Path,MediaSources,MediaStreams,Overview,PrimaryImageAspectRatio"}}.Encode()
	groups = f.get(t, "th-movie", fields)
	for _, name := range []string{"ThemeSongsResult", "ThemeVideosResult"} {
		item := groups[name].items[0]
		id := stringValue(t, item, "Id")
		path := stringValue(t, item, "Path")
		if !strings.HasPrefix(path, "/theme-http/visible/Film/") || item["ParentId"] != "th-movie" {
			t.Fatal("explicit theme path/parent projection escaped its owner")
		}
		sources, ok := item["MediaSources"].([]any)
		if !ok || len(sources) != 1 {
			t.Fatal("explicit theme MediaSources projection is incomplete")
		}
		source, ok := sources[0].(map[string]any)
		if !ok || source["Id"] != media.SourceID(id) || source["Path"] != path || source["Protocol"] != "File" {
			t.Fatal("theme source identity no longer names the actual indexed resource")
		}
		streams, ok := item["MediaStreams"].([]any)
		if !ok || len(streams) == 0 || !reflect.DeepEqual(streams, source["MediaStreams"]) {
			t.Fatal("explicit theme media stream projections disagree")
		}
	}
	if groups["ThemeSongsResult"].items[0]["PrimaryImageAspectRatio"] != float64(320)/180 {
		t.Fatal("explicit primary image dimensions were not projected")
	}
	groups = f.get(t, "th-movie", "EnableImages=false&EnableUserData=false&"+fields)
	for _, name := range []string{"ThemeSongsResult", "ThemeVideosResult"} {
		item := groups[name].items[0]
		for _, field := range []string{"ImageTags", "BackdropImageTags", "PrimaryImageAspectRatio", "UserData"} {
			if _, found := item[field]; found {
				t.Fatalf("disabled theme projection retained %s", field)
			}
		}
	}
	limited := f.get(t, "th-movie", "ImageTypeLimit=0")["ThemeSongsResult"].items[0]
	if len(objectValue(t, limited, "ImageTags")) != 0 || !reflect.DeepEqual(limited["BackdropImageTags"], []any{}) {
		t.Fatal("ImageTypeLimit=0 changed membership or retained image tags")
	}
	if f.state(t) != before {
		t.Fatal("theme projection switches changed metadata or persistent user data")
	}
}

func TestHTTPThemeMediaGenreInheritanceMatchesDirectResourcesAndEmptyControls(t *testing.T) {
	f := newThemeHTTPFixture(t)
	owner := `{"Genres":["Drama","Adventure"],"Tags":["OwnerTag"],"Studios":["OwnerStudio"],"ProductionYear":2000}`
	if _, err := f.pool.Exec(f.ctx, "UPDATE item_metadata_state SET effective=$2::jsonb WHERE item_id=$1", "th-movie", owner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "SELECT sync_catalog_item_entities($1,$2::jsonb)", "th-movie", owner); err != nil {
		t.Fatal(err)
	}
	fields := "Fields=Genres,Tags,Studios,ProductionYear,Path,MediaSources,MediaStreams"
	for _, test := range []struct {
		name, source, overrides, locks string
		genres                         []any
	}{
		{"inherit", `{"Tags":["ResourceTag"]}`, `{}`, `{}`, []any{"Drama", "Adventure"}},
		{"own", `{"Genres":["OwnGenre"],"Tags":["ResourceTag"]}`, `{}`, `{}`, []any{"OwnGenre"}},
		{"empty override", `{"Genres":[],"Tags":["ResourceTag"]}`, `{"Genres":[]}`, `{}`, []any{}},
		{"empty lock", `{"Genres":[],"Tags":["ResourceTag"]}`, `{}`, `{"Genres":[]}`, []any{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, `UPDATE item_metadata_state SET effective=$2::jsonb,overrides=$3::jsonb,locked_values=$4::jsonb
				WHERE item_id=$1`, "th-song", test.source, test.overrides, test.locks); err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(f.ctx, "SELECT sync_catalog_item_entities($1,$2::jsonb)", "th-song", test.source); err != nil {
				t.Fatal(err)
			}
			before := f.state(t)
			item := f.get(t, "th-movie", fields)["ThemeSongsResult"].items[0]
			if !reflect.DeepEqual(item["Genres"], test.genres) {
				t.Fatal("theme Genres replaced an own value or empty native control")
			}
			if _, exists := item["ProductionYear"]; exists {
				t.Fatal("genre fallback copied the owner's unrelated year")
			}
			if !reflect.DeepEqual(item["Studios"], []any{}) {
				t.Fatal("genre fallback copied the owner's studios")
			}
			tags, ok := item["TagItems"].([]any)
			if !ok || len(tags) != 1 || tags[0].(map[string]any)["Name"] != "ResourceTag" {
				t.Fatal("genre fallback replaced the resource's own tags")
			}
			response := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/th-song?"+fields, nil, f.headers)
			expectStatus(t, response, http.StatusOK)
			direct := jsonObject(t, response)
			for _, field := range []string{"Id", "Type", "ExtraType", "ParentId", "Genres", "GenreItems", "TagItems"} {
				if !reflect.DeepEqual(direct[field], item[field]) {
					t.Fatalf("direct and ThemeMedia resource DTOs disagree on %s", field)
				}
			}
			refs, ok := item["GenreItems"].([]any)
			if !ok || len(refs) != len(test.genres) {
				t.Fatal("inherited GenreItems lost their entity population")
			}
			for _, ref := range refs {
				entry := ref.(map[string]any)
				id, ok := entry["Id"].(float64)
				if !ok || id <= 0 {
					t.Fatal("inherited GenreItems invented a string or empty identity")
				}
			}
			if f.state(t) != before {
				t.Fatal("ThemeMedia or direct GET persisted genre inheritance or changed controls")
			}
		})
	}
}

func TestHTTPThemeMediaOrdinaryListsHideActiveInactiveAndReservedResources(t *testing.T) {
	f := newThemeHTTPFixture(t)
	before := f.state(t)
	items, count := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Users/"+f.viewerID+"/Items?Recursive=true&Limit=100&Fields=Path", nil, f.headers))
	ids := themeHTTPItemIDs(t, items)
	sort.Strings(ids)
	want := []string{"th-empty", "th-episode", "th-movie", "th-season", "th-series"}
	if count != len(want) || !reflect.DeepEqual(ids, want) {
		t.Fatalf("ordinary enumeration leaked a theme role or hidden library: %v/%d", ids, count)
	}
	for _, item := range items {
		if _, exists := item["ExtraType"]; exists {
			t.Fatal("ordinary catalog item was mislabeled as theme media")
		}
	}
	filtered, count := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Users/"+f.viewerID+"/Items?Recursive=true&Ids=th-song,th-video,th-retired,th-reserved", nil, f.headers))
	if count != 0 || len(filtered) != 0 {
		t.Fatal("explicit ordinary Ids filtering bypassed permanent theme exclusion")
	}
	for _, id := range []string{"th-song", "th-video"} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+id, nil, f.headers), http.StatusOK)
	}
	for _, id := range []string{"th-retired", "th-reserved"} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+id, nil, f.headers), http.StatusNotFound)
	}
	if f.state(t) != before {
		t.Fatal("ordinary/direct browsing changed theme roles, owner mappings, or user data")
	}
}

func TestHTTPThemeMediaAuthenticatesAndRejectsInvalidControlsOrCrossUserScope(t *testing.T) {
	f := newThemeHTTPFixture(t)
	path := "/emby/Items/th-movie/ThemeMedia?UserId=" + f.viewerID
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, path+"&InheritFromParent=invalid", nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {"invalid-theme-token"}}), http.StatusUnauthorized)
	for _, control := range []string{"InheritFromParent", "EnableThemeSongs", "EnableThemeVideos"} {
		for _, values := range [][]string{{""}, {"invalid"}, {"true", "false"}, {"false", "false"}} {
			expectStatus(t, f.request(t, http.MethodGet, path+"&"+url.Values{control: values}.Encode(), nil, f.headers), http.StatusBadRequest)
		}
	}
	for _, suffix := range []string{"EnableUserData=invalid", "EnableImages=invalid", "ImageTypeLimit=-1"} {
		expectStatus(t, f.request(t, http.MethodGet, path+"&"+suffix, nil, f.headers), http.StatusBadRequest)
	}
	for _, seed := range []string{"th-private", "missing-theme-item"} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/"+seed+"/ThemeMedia?EnableThemeSongs=false&EnableThemeVideos=false", nil, f.headers), http.StatusNotFound)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/th-private/ThemeMedia?UserId="+f.otherID, nil, f.headers), http.StatusForbidden)
	private := themeHTTPGroups(t, f.request(t, http.MethodGet, "/emby/Items/th-private/ThemeMedia?UserId="+f.otherID, nil, f.otherHeaders))
	if !reflect.DeepEqual(themeHTTPItemIDs(t, private["ThemeSongsResult"].items), []string{"th-private-song"}) {
		t.Fatal("independent authorized user did not receive its own library's theme")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", f.viewerID, `{"EnableAllFolders":false,"EnabledFolders":[]}`); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"&EnableThemeSongs=false&EnableThemeVideos=false", nil, f.headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, f.headers), http.StatusNoContent)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, f.headers), http.StatusUnauthorized)
	var rows int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM user_item_data").Scan(&rows); err != nil || rows != 0 {
		t.Fatal("rejected or cross-user ThemeMedia requests created user data")
	}
}
