package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestRuntimePlaybackTypeErrorsDenyOnlyThatPermission(t *testing.T) {
	fields := []string{"EnableMediaPlayback", "EnablePlaybackRemuxing", "EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding"}
	for _, field := range fields {
		for _, malformed := range []string{`null`, `"false"`, `"true"`, `0`, `1`, `[]`, `{}`} {
			t.Run(field+"/"+malformed, func(t *testing.T) {
				raw := json.RawMessage(fmt.Sprintf(`{"%s":%s,"EnableAllFolders":false,"EnabledFolders":["allowed-library"],"MaxParentalRating":5,"BlockedTags":["Adults"],"EnableContentDownloading":true}`, field, malformed))
				before := append([]byte(nil), raw...)
				policy, err := ParseRuntimePolicy(raw)
				if err != nil {
					t.Fatalf("independent playback permission prevented policy interpretation: %v", err)
				}
				for _, permission := range fields {
					if enabled := reflect.ValueOf(policy).FieldByName(permission).Bool(); enabled != (permission != field) {
						t.Errorf("%s changed permission %s to %t", field, permission, enabled)
					}
				}
				if policy.EnableAllFolders || !reflect.DeepEqual(policy.EnabledFolders, []string{"allowed-library"}) ||
					policy.MaxParentalRating == nil || *policy.MaxParentalRating != 5 || !reflect.DeepEqual(policy.BlockedTags, []string{"Adults"}) || !policy.EnableContentDownloading {
					t.Fatalf("playback fallback changed independent restrictions: %+v", policy)
				}
				if !loginPolicyAllows(raw, "current-device", time.Now()) {
					t.Fatal("playback formatting disabled an independently permitted login")
				}
				if _, err := ParseManagedPolicy(raw); !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("management validation accepted a wrong-typed permission: %v", err)
				}
				if !bytes.Equal(before, raw) {
					t.Fatal("runtime parsing rewrote the stored document")
				}
			})
		}
	}
}

func TestRuntimePlaybackFallbackCannotRepairOtherRestrictionsOrAmbiguousDocuments(t *testing.T) {
	for _, raw := range []string{
		`{"EnableMediaPlayback":"false","EnableAllFolders":null}`,
		`{"EnableMediaPlayback":"false","EnabledFolders":[null]}`,
		`{"EnableMediaPlayback":"false","MaxParentalRating":"5"}`,
		`{"EnableMediaPlayback":"false","BlockedTags":null}`,
		`{"EnableMediaPlayback":"false","ExcludedSubFolders":[1]}`,
		`{"EnableMediaPlayback":"false","RestrictedFeatures":[null]}`,
		`{"EnableMediaPlayback":"false","EnableContentDownloading":"true"}`,
		`{"EnableMediaPlayback":"false","EnableRemoteAccess":"true"}`,
		`{"EnableMediaPlayback":"false","EnableSubtitleManagement":null}`,
		`{"EnableMediaPlayback":"false","AccessSchedules":[{}]}`,
		`{"EnableMediaPlayback":"false","LockedOutDate":"0"}`,
		`{"EnableMediaPlayback":"false","EnableMediaPlayback":true}`,
		`{"EnableMediaPlayback":"false","enablemediaplayback":true}`,
		`{"enablemediaplayback":"false"}`,
		`{"EnableMediaPlayback":{"duplicate":1,"duplicate":2}}`,
		`{"EnableMediaPlayback":"false"} {}`,
		`null`, `[]`, `{`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseRuntimePolicy(json.RawMessage(raw)); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("runtime fallback accepted an unrelated malformed restriction or structure: %v", err)
			}
		})
	}
}
