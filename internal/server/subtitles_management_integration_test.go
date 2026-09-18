//go:build linux

package server

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func setSubtitleManagementPolicy(t *testing.T, fixture *subtitleHTTPFixture, enabled bool) {
	t.Helper()
	s := fixture.p.s
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE users SET policy = jsonb_set(policy, '{EnableSubtitleManagement}', to_jsonb($2::boolean), true) WHERE id = $1`, s.viewerID, enabled); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPSubtitleManagementRetiresIndexesAndProviderMappings(t *testing.T) {
	fixture := newSubtitleHTTPFixture(t)
	s := fixture.p.s
	initialRoute := fmt.Sprintf("/emby/Items/%s/Subtitles/6", s.video.id)
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodDelete, initialRoute, s.token, nil, nil), http.StatusForbidden)
	if _, err := os.Stat(fixture.srtPath); err != nil {
		t.Fatalf("denied subtitle deletion changed its source: %v", err)
	}
	setSubtitleManagementPolicy(t, fixture, true)
	var persistentBinding bool
	if err := s.f.pool.QueryRow(s.f.ctx, `SELECT COALESCE(bool_and(storage_binding IS NOT NULL),false) FROM library_roots WHERE library_id=$1`, s.video.libraryID).Scan(&persistentBinding); err != nil {
		t.Fatal(err)
	}
	if !persistentBinding {
		t.Skip("safe deletion requires persistent root binding support")
	}
	for offset, route := range []struct{ method, resource, suffix string }{
		{http.MethodDelete, "Items", ""}, {http.MethodDelete, "Videos", ""},
		{http.MethodPost, "Items", "/Delete"}, {http.MethodPost, "Videos", "/Delete"},
	} {
		index := 6 + offset
		if offset > 0 {
			writeSubtitleHTTPFile(t, fixture.srtPath, subtitleHTTPSRT)
			s.rescan(t, s.video.libraryID)
		}
		if _, err := s.f.pool.Exec(s.f.ctx, `INSERT INTO item_subtitle_provider_sources(item_id,stream_index,provider,provider_id) VALUES($1,$2,'opensubtitles','deletion-fixture')`, s.video.id, index); err != nil {
			t.Fatal(err)
		}
		notifications := make(chan library.CatalogNotification, 4)
		s.f.app.library.SetCatalogChangeListener(func(notification library.CatalogNotification) {
			s.f.app.catalogNotifier.Enqueue(notification)
			select {
			case notifications <- notification:
			default:
			}
		})
		path := fmt.Sprintf("/emby/%s/%s/Subtitles/%d%s", route.resource, s.video.id, index, route.suffix)
		expectSubtitleHTTPStatus(t, s.request(t, route.method, path, s.token, nil, nil), http.StatusNoContent)
		s.f.app.library.SetCatalogChangeListener(s.f.app.catalogNotifier.Enqueue)
		if _, err := os.Stat(fixture.srtPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("managed subtitle remained at its source name: %v", err)
		}
		var active bool
		if err := s.f.pool.QueryRow(s.f.ctx, `SELECT active FROM item_subtitles WHERE item_id=$1 AND stream_index=$2`, s.video.id, index).Scan(&active); err != nil || active {
			t.Fatalf("deleted subtitle identity was not retained as inactive: %v", err)
		}
		var provenance int
		if err := s.f.pool.QueryRow(s.f.ctx, `SELECT count(*) FROM item_subtitle_provider_sources WHERE item_id=$1 AND stream_index=$2`, s.video.id, index).Scan(&provenance); err != nil || provenance != 0 {
			t.Fatalf("deleted subtitle retained an active provider selection: %v", err)
		}
		expectSubtitleHTTPStatus(t, s.request(t, http.MethodGet, subtitleHTTPRoute(fixture, index, "0", "srt"), s.token, nil, nil), http.StatusNotFound)
		select {
		case notification := <-notifications:
			matched := false
			for _, change := range notification.Changes {
				matched = matched || change.Kind == library.CatalogUpdated && change.ItemID == s.video.id && change.LibraryID == s.video.libraryID
			}
			if !matched {
				t.Fatal("subtitle deletion did not publish its committed source item update")
			}
		default:
			t.Fatal("subtitle deletion did not emit a catalog notification")
		}
	}
}

func TestHTTPSubtitleManagementRechecksVisibilityPolicyAndEmbeddedReadOnly(t *testing.T) {
	fixture := newSubtitleHTTPFixture(t)
	s := fixture.p.s
	setSubtitleManagementPolicy(t, fixture, true)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE items SET media=jsonb_set(media,'{Streams}',(media->'Streams') || '[{"Index":4,"CodecType":"subtitle","Codec":"subrip","IsTextSubtitleStream":true}]'::jsonb) WHERE id=$1`, s.video.id); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/emby/Videos/%s/Subtitles/4", s.video.id)
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodDelete, path, s.token, nil, nil), http.StatusBadRequest)
	path = fmt.Sprintf("/emby/Videos/%s/Subtitles/6", s.video.id)
	s.setPolicy(t, s.viewerID, true, []string{})
	setSubtitleManagementPolicy(t, fixture, true)
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodDelete, path, s.token, nil, nil), http.StatusNotFound)
	s.setPolicy(t, s.viewerID, true, []string{s.video.libraryID})
	setSubtitleManagementPolicy(t, fixture, false)
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodPost, path+"/Delete", s.token, nil, nil), http.StatusForbidden)
	setSubtitleManagementPolicy(t, fixture, true)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, fixture.p.authSessionID); err != nil {
		t.Fatal(err)
	}
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodDelete, path, s.token, nil, nil), http.StatusUnauthorized)
	if _, err := os.Stat(fixture.srtPath); err != nil {
		t.Fatalf("rejected subtitle management changed the source: %v", err)
	}
}
