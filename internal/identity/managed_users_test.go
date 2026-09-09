package identity

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestManagedPolicyDefaultsAndConfigurationFacts(t *testing.T) {
	policy := projectManagedPolicy(json.RawMessage(`{}`))
	if !policy.EnableAllFolders || !policy.EnableMediaPlayback || !policy.EnablePlaybackRemuxing ||
		!policy.EnableAudioPlaybackTranscoding || !policy.EnableVideoPlaybackTranscoding || policy.EnabledFolders == nil {
		t.Fatalf("missing supported flags must retain permissive defaults: %+v", policy)
	}
	policy = projectManagedPolicy(json.RawMessage(`{"IsAdministrator":true,"IsDisabled":true,
		"EnableAllFolders":false,"EnabledFolders":["library-b","library-a","library-a"],
		"EnableMediaPlayback":true,"EnablePlaybackRemuxing":false,
		"EnableAudioPlaybackTranscoding":false,"EnableVideoPlaybackTranscoding":true}`))
	if policy.EnableAllFolders || !policy.EnableMediaPlayback || policy.EnablePlaybackRemuxing ||
		policy.EnableAudioPlaybackTranscoding || !policy.EnableVideoPlaybackTranscoding {
		t.Fatalf("stored role mirrors must not override editable policy facts: %+v", policy)
	}
	if !reflect.DeepEqual(policy.EnabledFolders, []string{"library-a", "library-b"}) {
		t.Fatalf("folder projection = %#v", policy.EnabledFolders)
	}
	policy = projectManagedPolicy(json.RawMessage(`{"EnableAllFolders":true,"EnabledFolders":["library-a"]}`))
	if !policy.EnableAllFolders || !reflect.DeepEqual(policy.EnabledFolders, []string{"library-a"}) {
		t.Fatalf("explicit folder selections must survive the all-folders configuration: %+v", policy)
	}
}

func TestManagedPolicyMalformedFactsFailClosed(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", "false", `"policy"`, "{", string([]byte{'{', '"', 0xff, '"', ':', '1', '}'})} {
		t.Run(raw, func(t *testing.T) {
			policy := projectManagedPolicy(json.RawMessage(raw))
			if policy.EnableAllFolders || policy.EnableMediaPlayback || policy.EnablePlaybackRemuxing ||
				policy.EnableAudioPlaybackTranscoding || policy.EnableVideoPlaybackTranscoding || policy.EnabledFolders == nil || len(policy.EnabledFolders) != 0 {
				t.Fatalf("malformed policy must deny all supported permissions: %+v", policy)
			}
		})
	}
	for _, raw := range []string{"null", `"true"`, "1", "[]", "{}"} {
		t.Run("flag_"+raw, func(t *testing.T) {
			policy := projectManagedPolicy(json.RawMessage(`{"EnableAllFolders":` + raw + `,"EnabledFolders":["library-a"],` +
				`"EnableMediaPlayback":` + raw + `,"EnablePlaybackRemuxing":` + raw + `,` +
				`"EnableAudioPlaybackTranscoding":` + raw + `,"EnableVideoPlaybackTranscoding":` + raw + `}`))
			if policy.EnableAllFolders || policy.EnableMediaPlayback || policy.EnablePlaybackRemuxing ||
				policy.EnableAudioPlaybackTranscoding || policy.EnableVideoPlaybackTranscoding || len(policy.EnabledFolders) != 0 {
				t.Fatalf("wrong-typed flags must not enable policy: %+v", policy)
			}
		})
	}
	for _, raw := range []string{`null`, `17`, `"library-a"`, `["library-a",17]`, `["library-a",null]`, `["library-a",""]`, `[" library-a"]`} {
		policy := projectManagedPolicy(json.RawMessage(`{"EnableAllFolders":false,"EnabledFolders":` + raw + `}`))
		if policy.EnableAllFolders || policy.EnabledFolders == nil || len(policy.EnabledFolders) != 0 {
			t.Errorf("malformed folders %s must grant no explicit access: %+v", raw, policy)
		}
	}
}

func TestManagedUserValidationAggregatesFieldsAndCopiesFolders(t *testing.T) {
	_, _, _, err := validateManagedUserUpdate(ManagedUserUpdate{Revision: 0, Name: " ", Policy: ManagedPolicy{EnabledFolders: []string{""}}})
	var validation *ManagedUserValidationError
	if !errors.Is(err, ErrInvalidInput) || !errors.As(err, &validation) {
		t.Fatalf("validation must expose ErrInvalidInput and typed fields: %v", err)
	}
	for _, name := range []string{"Revision", "Name", "Policy.EnabledFolders"} {
		if validation.Fields[name] == "" {
			t.Errorf("missing field error for %s: %#v", name, validation.Fields)
		}
	}
	original := []string{"library-b", "library-a", "library-a"}
	name, normalized, policy, err := validateManagedUserUpdate(ManagedUserUpdate{Revision: 1, Name: "  ViewerΣ  ", Policy: ManagedPolicy{EnabledFolders: original}})
	if err != nil || name != "ViewerΣ" || normalized != "viewerσ" {
		t.Fatalf("normalization = %q, %q, %v", name, normalized, err)
	}
	if !reflect.DeepEqual(policy.EnabledFolders, []string{"library-a", "library-b"}) || !reflect.DeepEqual(original, []string{"library-b", "library-a", "library-a"}) {
		t.Fatalf("folder canonicalization mutated caller input: input=%#v, policy=%#v", original, policy.EnabledFolders)
	}
}

func TestManagedFolderBounds(t *testing.T) {
	for _, folders := range [][]string{
		{strings.Repeat("x", 257)}, {strings.Repeat("é", 129)}, {"folder\n"}, {" folder"}, {"folder "}, {string([]byte{0xff})},
		make([]string, maxManagedFolders+1),
	} {
		if _, err := canonicalManagedFolders(folders); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("invalid folder list accepted: %v", err)
		}
	}
	for _, folders := range [][]string{nil, {}, {strings.Repeat("é", 128)}, {"library.with-symbols_1"}} {
		canonical, err := canonicalManagedFolders(folders)
		if err != nil || canonical == nil {
			t.Errorf("valid folder list rejected or encoded as null: %#v, %v", canonical, err)
		}
	}
}

func TestManagedActorRequiresTrustedAdminSessionShape(t *testing.T) {
	for _, actor := range []Principal{
		{},
		{Kind: "emby", SessionID: "session", User: User{ID: "user", IsAdministrator: true}},
		{Kind: "admin", SessionID: "", User: User{ID: "user", IsAdministrator: true}},
		{Kind: "admin", SessionID: "session", User: User{ID: " user", IsAdministrator: true}},
	} {
		if validManagedActor(actor) {
			t.Errorf("invalid trusted-session shape accepted: %+v", actor)
		}
	}
	// Authorization comes from the locked database account, never this snapshot.
	if !validManagedActor(Principal{Kind: "admin", SessionID: "session", User: User{ID: "user", IsAdministrator: false, IsDisabled: true}}) {
		t.Error("snapshot role or disabled flags must not replace database authorization")
	}
}
