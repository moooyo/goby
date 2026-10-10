package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestHTTPStorageRootsMissingUnderConfiguredAliasRecovers(t *testing.T) {
	for _, mode := range []string{"existing-parent", "dangling-root", "dangling-parent", "relative-parent-alias", "relative-target-alias", "absolute-target-alias"} {
		t.Run(mode, func(t *testing.T) { testHTTPStorageRootsMissingAliasRecovers(t, mode) })
	}
}

func testHTTPStorageRootsMissingAliasRecovers(t *testing.T, mode string) {
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	base := t.TempDir()
	physical := filepath.Join(base, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	target := physical
	if mode == "dangling-root" || mode == "dangling-parent" {
		target = filepath.Join(physical, "missing", "nested")
	}
	configured := filepath.Join(alias, "missing", "nested")
	canonical := filepath.Join(physical, "missing", "nested")
	if mode == "dangling-root" {
		configured = alias
	} else if mode == "dangling-parent" {
		canonical = filepath.Join(target, "missing", "nested")
	}
	var wrongTarget string
	if mode == "relative-parent-alias" || mode == "relative-target-alias" || mode == "absolute-target-alias" {
		inner := filepath.Join(physical, "inner")
		if err := os.Mkdir(inner, 0o700); err != nil {
			t.Fatal(err)
		}
		otherAlias := filepath.Join(base, "other-alias")
		if err := os.Symlink(inner, otherAlias); err != nil {
			t.Fatal(err)
		}
		configured = alias
		if mode == "relative-parent-alias" {
			alias = filepath.Join(inner, "leaf")
			configured = filepath.Join(otherAlias, "leaf")
			target = "../missing/nested"
		} else if mode == "relative-target-alias" {
			target = "other-alias/../missing/nested"
		} else {
			target = otherAlias + "/../missing/nested"
		}
		wrongTarget = filepath.Join(base, "missing", "nested")
		if err := os.MkdirAll(wrongTarget, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	catalog, err := library.New(f.pool, apiMediaProber{}, []string{configured})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots = []string{configured}
	f.cfg.MediaRoots = []string{configured}
	f.handler = f.app.Handler()
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	availability := func(want bool) {
		t.Helper()
		response := f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		body := jsonObject(t, response)
		items := body["Items"].([]any)
		if len(items) != 1 {
			t.Fatalf("missing root disappeared from configuration: %#v", body)
		}
		item := items[0].(map[string]any)
		if item["Path"] != configured || item["Available"] != want {
			t.Fatalf("missing alias root did not preserve its fixed mapping: %#v", item)
		}
	}
	availability(false)
	if wrongTarget != "" {
		expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
			"Name": "Wrong alias destination", "CollectionType": "movies", "Paths": []string{wrongTarget},
		}, http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusForbidden, "access_denied", false)
	}
	if err := os.MkdirAll(canonical, 0o700); err != nil {
		t.Fatal(err)
	}
	availability(true)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/storage/directories?Path="+url.QueryEscape(canonical), nil, nil, cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Recovered configured root", "CollectionType": "movies", "Paths": []string{canonical},
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusCreated)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "still-missing"), alias); err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie), http.StatusServiceUnavailable, "library_unavailable", false)
}

func TestHTTPStorageRootsPreserveConfiguredOrderAliasesAndReadability(t *testing.T) {
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	base := t.TempDir()
	first := filepath.Join(base, "short")
	second := filepath.Join(base, "longer", "empty")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeAPIMediaFile(t, first, "marker.txt")
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(base, "missing")
	regular := writeAPIMediaFile(t, base, "regular.txt")
	paths := []string{first, missing, alias, regular, second, first}
	catalog, err := library.New(f.pool, apiMediaProber{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots = paths
	f.cfg.MediaRoots = paths
	f.handler = f.app.Handler()
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	response := f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	body := jsonObject(t, response)
	items, ok := body["Items"].([]any)
	if !ok || len(items) != len(paths) || body["Configured"] != true {
		t.Fatalf("storage root DTO changed: %#v", body)
	}
	for index, path := range paths {
		item, ok := items[index].(map[string]any)
		wantAvailable := path != missing && path != regular
		if !ok || len(item) != 2 || item["Path"] != path || item["Available"] != wantAvailable {
			t.Fatalf("configuration order, spelling, duplicates or readability changed at %d: %#v", index, item)
		}
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), alias); err != nil {
		t.Fatal(err)
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie), http.StatusServiceUnavailable, "library_unavailable", false)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, alias); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie), http.StatusOK)
}

func TestHTTPStorageRootsUnconfiguredUsesAnEmptyArray(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	response := f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	body := jsonObject(t, response)
	items, ok := body["Items"].([]any)
	if !ok || len(items) != 0 || body["Configured"] != false {
		t.Fatalf("empty configured roots changed their DTO: %#v", body)
	}
}
