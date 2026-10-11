//go:build linux

package server

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestHTTPItemProjectionClientCasingAndExclusions(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items?Ids=" + f.itemID
	canonical, canonicalCount := responseItems(t, f.request(t, http.MethodGet, base+"&Fields=Overview,ProviderIds,MediaStreams,MediaSources,Path", nil, headers))
	folded, foldedCount := responseItems(t, f.request(t, http.MethodGet, base+"&fields=Overview,ProviderIds,MediaStreams,MediaSources,Path&X-Emby-Language=en-US", nil, headers))
	if canonicalCount != 1 || foldedCount != 1 || !reflect.DeepEqual(canonical, folded) || folded[0]["Overview"] != "Automatic overview." {
		t.Fatal("the original client's lowercase Fields projection lost metadata or membership")
	}
	detailPath := "/emby/Items/" + f.itemID
	detail := jsonObject(t, f.request(t, http.MethodGet, detailPath, nil, headers))
	if _, present := detail["MediaStreams"]; !present {
		t.Fatal("the detail fixture lacks source streams")
	}
	excluded := jsonObject(t, f.request(t, http.MethodGet, detailPath+"?fields=MediaStreams,MediaSources,Path&excludefields=MediaStreams,Path,PrimaryImageAspectRatio&X-Emby-Language=zh-CN", nil, headers))
	for _, name := range []string{"MediaStreams", "Path", "PrimaryImageAspectRatio"} {
		if _, present := excluded[name]; present {
			t.Fatalf("detail retained excluded %s", name)
		}
	}
	sources, ok := excluded["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("excluding source facts removed the media source itself")
	}
	source := sources[0].(map[string]any)
	if _, present := source["MediaStreams"]; present {
		t.Fatal("excluded streams survived inside MediaSources")
	}
	if _, present := source["Path"]; present {
		t.Fatal("excluded path survived inside MediaSources")
	}
	items, count := responseItems(t, f.request(t, http.MethodGet, base+"&fields=Overview,ProviderIds&ExcludeFields=Overview,ImageTags,BackdropImageTags", nil, headers))
	if count != 1 || items[0]["ProviderIds"] == nil {
		t.Fatal("exclusion changed list membership or unrelated selected metadata")
	}
	for _, name := range []string{"Overview", "ImageTags", "BackdropImageTags"} {
		if _, present := items[0][name]; present {
			t.Fatalf("list postprocessing restored excluded %s", name)
		}
	}
	root := jsonObject(t, f.request(t, http.MethodGet, "/emby/Items/"+virtualRootID()+"?ExcludeFields=ChildCount", nil, headers))
	if _, present := root["ChildCount"]; present || root["Id"] != virtualRootID() || root["CanDelete"] != false || root["CanDownload"] != false {
		t.Fatal("virtual root bypassed projection or gained a file capability")
	}
	for _, query := range []string{"fields=Overview&Fields=Path", "ExcludeFields=" + url.QueryEscape("MediaStreams\x00"), "X-Emby-Language=%00", "X-Emby-Language=en-US&x-emby-language=en-US"} {
		expectStatus(t, f.request(t, http.MethodGet, detailPath+"?"+query, nil, headers), http.StatusBadRequest)
		expectStatus(t, f.request(t, http.MethodGet, detailPath+"?"+query, nil, nil), http.StatusUnauthorized)
	}
}

func TestHTTPItemProjectionUserDataSwitchPreservesStateSelection(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	if _, err := f.pool.Exec(f.ctx, "INSERT INTO user_item_data(user_id,item_id,played,is_favorite,play_count) VALUES($1,$2,true,true,3)", f.viewerID, f.itemID); err != nil {
		t.Fatal(err)
	}
	base := "/emby/Items?Ids=" + f.itemID + "," + f.secondID + "&SortBy=PlayCount&SortOrder=Descending&EnableImages=false"
	for _, filter := range []string{"", "&IsFavorite=true", "&IsPlayed=true"} {
		baseline, total := responseItems(t, f.request(t, http.MethodGet, base+filter, nil, headers))
		enabled, enabledTotal := responseItems(t, f.request(t, http.MethodGet, base+filter+"&EnableUserData=true", nil, headers))
		if total != enabledTotal || len(baseline) == 0 || !reflect.DeepEqual(baseline, enabled) {
			t.Fatal("explicit user-data enable changed defaults, ordering, or membership")
		}
		disabled, disabledTotal := responseItems(t, f.request(t, http.MethodGet, base+filter+"&EnableUserData=false", nil, headers))
		for _, item := range baseline {
			if item["UserData"] == nil {
				t.Fatal("the baseline fixture did not include user data")
			}
			delete(item, "UserData")
		}
		if disabledTotal != total || !reflect.DeepEqual(baseline, disabled) {
			t.Fatalf("disabling user data changed state selection or unrelated DTO fields: filter=%q", filter)
		}
		countOnly, count := responseItems(t, f.request(t, http.MethodGet, base+filter+"&EnableUserData=false&Limit=0", nil, headers))
		if len(countOnly) != 0 || count != total {
			t.Fatal("disabling user data changed a filtered count-only response")
		}
	}
	detail := jsonObject(t, f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"?EnableUserData=false&EnableImages=false", nil, headers))
	if detail["UserData"] != nil || detail["Id"] != f.itemID {
		t.Fatal("detail did not honor the user-data projection independently of identity")
	}
}
