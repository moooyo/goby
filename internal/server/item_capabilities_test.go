package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestItemCapabilitiesSkipOnlyUnobservableProjection(t *testing.T) {
	for _, test := range []struct {
		name, query         string
		detail, empty, skip bool
	}{
		{name: "default list", skip: true},
		{name: "detail requests both", detail: true},
		{name: "detail excludes both", query: "ExcludeFields=CanDelete,CanDownload", detail: true, skip: true},
		{name: "detail excludes only deletion", query: "ExcludeFields=CanDelete", detail: true},
		{name: "detail excludes only downloading", query: "ExcludeFields=CanDownload", detail: true},
		{name: "single requested field excluded", query: "Fields=CanDownload&ExcludeFields=CanDownload", skip: true},
		{name: "single deletion field excluded", query: "Fields=CanDelete&ExcludeFields=CanDelete", skip: true},
		{name: "case insensitive lists", query: "Fields=CANDELETE&Fields=candownload&ExcludeFields=candelete&ExcludeFields=CANDOWNLOAD", skip: true},
		{name: "one requested field remains", query: "Fields=CanDelete,CanDownload&ExcludeFields=CanDownload"},
		{name: "unrelated exclusion", query: "Fields=CanDownload&ExcludeFields=Path"},
		{name: "disabled images retain capabilities", query: "Fields=CanDownload&EnableImages=false"},
		{name: "empty result", detail: true, empty: true, skip: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Items?"+test.query, nil)
			items := []map[string]any{{"Id": "item"}}
			if test.empty {
				items = nil
			}
			// A nil catalog and absent actor expose any unwanted optional
			// admission. A retained projection still requires its real actor.
			s := &Server{}
			w := httptest.NewRecorder()
			ok := s.applyItemCapabilities(w, r, items, test.detail, readItemPresentation(r))
			if ok != test.skip {
				t.Fatalf("optional capability admission = %v, want skipped=%v", ok, test.skip)
			}
			if !test.skip && w.Code != http.StatusForbidden {
				t.Fatalf("requested capabilities did not retain actor admission: status=%d body=%s", w.Code, w.Body.String())
			}
			for _, item := range items {
				if len(item) != 1 {
					t.Fatalf("omitted or unauthorized capabilities changed the DTO: %#v", item)
				}
			}
		})
	}
}
