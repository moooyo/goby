package library

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestScannedIDAvailabilityPreservesClaimScopeAndOwnership(t *testing.T) {
	state := &scanState{root: libraryRoot{id: "root-a"}}
	if !state.scannedIDAvailable("item", "Film.mp4", scannedRoleOrdinary) {
		t.Fatal("an untracked scan unexpectedly rejected an item")
	}
	local := map[string]string{"local": "ordinary:root-a:Film.mp4"}
	shared := map[string]string{"same": "ordinary:root-a:Film.mp4", "other-path": "ordinary:root-a:Other.mp4",
		"other-root": "ordinary:root-b:Film.mp4", "theme": "theme:root-a:Film.mp4", "empty": ""}
	state.themes = &themeScan{claimed: local}
	if !state.scannedIDAvailable("local", "Film.mp4", scannedRoleOrdinary) || state.scannedIDAvailable("local", "Other.mp4", scannedRoleOrdinary) {
		t.Fatal("root-local claims lost their exact path scope")
	}
	state.themeLibrary = &themeLibraryScan{claimed: shared}
	for _, test := range []struct {
		id, relative string
		role         scannedMediaRole
		available    bool
	}{
		{"unseen", "Film.mp4", scannedRoleOrdinary, true},
		{"local", "Other.mp4", scannedRoleOrdinary, true},
		{"same", "Film.mp4", scannedRoleOrdinary, true},
		{"same", "Film.mp4", scannedRoleTheme, false},
		{"other-path", "Film.mp4", scannedRoleOrdinary, false},
		{"other-root", "Film.mp4", scannedRoleOrdinary, false},
		{"theme", "Film.mp4", scannedRoleOrdinary, false},
		{"theme", "Film.mp4", scannedRoleTheme, true},
		{"empty", "Film.mp4", scannedRoleOrdinary, false},
	} {
		if got := state.scannedIDAvailable(test.id, test.relative, test.role); got != test.available {
			t.Errorf("claim %s/%s/%s availability=%v, want %v", test.id, test.relative, test.role, got, test.available)
		}
	}
	if len(local) != 1 || len(shared) != 5 {
		t.Fatal("availability lookup registered or removed claims")
	}
	// A claim made later by the same worker must be observed; lookup never
	// freezes a prior snapshot or lets a second path reuse the same ID.
	shared["unseen"] = "ordinary:root-b:Elsewhere.mp4"
	if state.scannedIDAvailable("unseen", "Film.mp4", scannedRoleOrdinary) {
		t.Fatal("a later cross-root claim was ignored")
	}
}

func TestScannedIDAvailabilityMatchesRenameExclusions(t *testing.T) {
	claims := make(map[string]string, 20000)
	for index := 0; index < 20000; index++ {
		claims[fmt.Sprintf("item-%05d", index)] = fmt.Sprintf("ordinary:root-a:Film-%05d.mp4", index)
	}
	state := &scanState{root: libraryRoot{id: "root-a"}, themes: &themeScan{}, themeLibrary: &themeLibraryScan{claimed: claims}}
	excluded := state.claimedScannedIDs("Film-12345.mp4", scannedRoleOrdinary)
	sort.Strings(excluded)
	want := make([]string, 0, len(claims)-1)
	for id := range claims {
		if id != "item-12345" {
			want = append(want, id)
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(excluded, want) || !state.scannedIDAvailable("item-12345", "Film-12345.mp4", scannedRoleOrdinary) ||
		state.scannedIDAvailable("item-00001", "Film-12345.mp4", scannedRoleOrdinary) {
		t.Fatal("single-ID lookup diverged from complete rename claim exclusion")
	}
}
