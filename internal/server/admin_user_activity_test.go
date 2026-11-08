package server

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestParseAdminUserActivityQueryRejectsAmbiguousAndUnboundedBatches(t *testing.T) {
	for _, query := range []string{
		"", "UserIds=", "userids=alpha", "UserIds=alpha&UserIds=beta", "UserIds=alpha&Extra=1",
		"UserIds=alpha,alpha", "UserIds=alpha,", "UserIds=%20alpha", "UserIds=alpha%00", "UserIds=%ff", "UserIds=%zz",
		"UserIds=" + strings.Repeat("alpha,", identity.MaxUserActivityIDs) + "overflow",
	} {
		request := httptest.NewRequest("GET", "/admin/v1/users/activity", nil)
		request.URL.RawQuery = query
		if _, err := parseAdminUserActivityQuery(request); !errors.Is(err, identity.ErrInvalidInput) {
			t.Fatal("an ambiguous or invalid user activity query was accepted")
		}
	}
	request := httptest.NewRequest("GET", "/admin/v1/users/activity?UserIds="+url.QueryEscape("alpha,user-two"), nil)
	ids, err := parseAdminUserActivityQuery(request)
	if err != nil || !reflect.DeepEqual(ids, []string{"alpha", "user-two"}) {
		t.Fatal("a valid activity batch changed its exact identifiers")
	}
}
