package library

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func libraryStorageDirectoryUnitHint(id, libraryID, path, approved, relative string) mediaSourceRootHint {
	return mediaSourceRootHint{root: libraryRoot{id: id, libraryID: libraryID, path: path,
		allowedPath: approved, relativePath: relative}, bindingRevision: 1}
}

func TestLibraryStorageDirectoryGroupsDeduplicateOnlyExactPaths(t *testing.T) {
	first := libraryStorageDirectoryUnitHint("root-a", "library-a", "/media/movies", "/media", "movies")
	second := libraryStorageDirectoryUnitHint("root-b", "library-b", "/media/movies", "/media", "movies")
	sibling := libraryStorageDirectoryUnitHint("root-tv", "library-tv", "/media/tv", "/media", "tv")
	child := libraryStorageDirectoryUnitHint("root-extra", "library-extra", "/media/movies/extra", "/media", "movies/extra")
	prefix := libraryStorageDirectoryUnitHint("root-prefix", "library-prefix", "/media/movies-extra", "/media", "movies-extra")
	roots := []mediaSourceRootHint{first, sibling, second, child, prefix}
	before := append([]mediaSourceRootHint(nil), roots...)
	groups := groupLibraryStorageDirectoryRoots(roots)
	want := []libraryStorageDirectoryGroup{
		{path: first.root.path, roots: []mediaSourceRootHint{first, second}},
		{path: sibling.root.path, roots: []mediaSourceRootHint{sibling}},
		{path: child.root.path, roots: []mediaSourceRootHint{child}},
		{path: prefix.root.path, roots: []mediaSourceRootHint{prefix}},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("exact registered paths or registration identities were merged: got=%+v want=%+v", groups, want)
	}
	if !reflect.DeepEqual(roots, before) {
		t.Fatal("grouping changed the committed registration metadata")
	}
	if groups := groupLibraryStorageDirectoryRoots(nil); len(groups) != 0 {
		t.Fatalf("empty registrations invented filesystem groups: %+v", groups)
	}
}

func TestLibraryStorageDirectoryGroupsRejectConflictingOrInvalidMappings(t *testing.T) {
	first := libraryStorageDirectoryUnitHint("root-a", "library-a", "/media/movies", "/media", "movies")
	for _, test := range []struct {
		name   string
		change func(*mediaSourceRootHint)
	}{
		{name: "different valid approved mapping", change: func(hint *mediaSourceRootHint) {
			hint.root.allowedPath, hint.root.relativePath = hint.root.path, "."
		}},
		{name: "relative mapping differs", change: func(hint *mediaSourceRootHint) {
			hint.root.relativePath = "different"
		}},
		{name: "zero revision", change: func(hint *mediaSourceRootHint) {
			hint.bindingRevision = 0
		}},
		{name: "negative revision", change: func(hint *mediaSourceRootHint) {
			hint.bindingRevision = -1
		}},
		{name: "missing root identity", change: func(hint *mediaSourceRootHint) {
			hint.root.id = ""
		}},
		{name: "missing library identity", change: func(hint *mediaSourceRootHint) {
			hint.root.libraryID = ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			second := libraryStorageDirectoryUnitHint("root-b", "library-b", first.root.path, first.root.allowedPath, first.root.relativePath)
			test.change(&second)
			for _, roots := range [][]mediaSourceRootHint{{first, second}, {second, first}} {
				groups := groupLibraryStorageDirectoryRoots(roots)
				if len(groups) != 1 || groups[0].path != first.root.path || !groups[0].conflict || !reflect.DeepEqual(groups[0].roots, roots) {
					t.Fatalf("conflicting registrations became an observable filesystem: roots=%+v groups=%+v", roots, groups)
				}
				if sameLibraryStorageDirectoryGroup(groups[0], groups[0]) {
					t.Fatal("a conflicting group allowed an earlier capacity observation to be reused")
				}
			}
		})
	}
}

func TestLibraryStorageDirectoryGroupComparisonRequiresEveryRegistration(t *testing.T) {
	first := libraryStorageDirectoryUnitHint("root-a", "library-a", "/media/movies", "/media", "movies")
	second := libraryStorageDirectoryUnitHint("root-b", "library-b", first.root.path, first.root.allowedPath, first.root.relativePath)
	before := groupLibraryStorageDirectoryRoots([]mediaSourceRootHint{first, second})[0]
	for _, test := range []struct {
		name   string
		change func(*libraryStorageDirectoryGroup)
		want   bool
	}{
		{name: "unchanged registrations", want: true},
		{name: "binding revision changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[1].bindingRevision++
		}},
		{name: "root identity changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[1].root.id = "root-replacement"
		}},
		{name: "library identity changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[1].root.libraryID = "library-replacement"
		}},
		{name: "group path changed", change: func(group *libraryStorageDirectoryGroup) {
			group.path = "/media/replacement"
		}},
		{name: "registered path changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[1].root.path = "/media/replacement"
		}},
		{name: "approved path changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[1].root.allowedPath = group.roots[1].root.path
		}},
		{name: "relative path changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[1].root.relativePath = "."
		}},
		{name: "registration added", change: func(group *libraryStorageDirectoryGroup) {
			group.roots = append(group.roots, libraryStorageDirectoryUnitHint("root-c", "library-c", first.root.path, first.root.allowedPath, first.root.relativePath))
		}},
		{name: "registration removed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots = group.roots[:1]
		}},
		{name: "registration order changed", change: func(group *libraryStorageDirectoryGroup) {
			group.roots[0], group.roots[1] = group.roots[1], group.roots[0]
		}},
		{name: "current group conflicts", change: func(group *libraryStorageDirectoryGroup) {
			group.conflict = true
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := before
			after.roots = append([]mediaSourceRootHint(nil), before.roots...)
			if test.change != nil {
				test.change(&after)
			}
			if got := sameLibraryStorageDirectoryGroup(before, after); got != test.want {
				t.Fatalf("registration change did not invalidate earlier capacity: before=%+v after=%+v got=%v want=%v", before, after, got, test.want)
			}
			if got := sameLibraryStorageDirectoryGroup(after, before); got != test.want {
				t.Fatalf("group comparison lost symmetry: got=%v want=%v", got, test.want)
			}
		})
	}
}

func TestLibraryStorageDirectoryProjectionPreservesDecimalCapacity(t *testing.T) {
	readable, writable := true, false
	item := projectLibraryStorageDirectory("/media/movies", libraryStorageDirectoryObservation{
		readable: &readable, writable: &writable,
		capacity: &directoryCapacity{totalBytes: math.MaxUint64, usedBytes: math.MaxUint64 - 1, availableBytes: 9007199254740993},
	})
	if item.Path != "/media/movies" || !item.Available || item.Readable == nil || !*item.Readable || item.Writable == nil || *item.Writable {
		t.Fatalf("filesystem capacity changed the independent access observation: %+v", item)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"TotalBytes": "18446744073709551615", "UsedBytes": "18446744073709551614", "AvailableBytes": "9007199254740993",
	} {
		if payload[key] != want {
			t.Fatalf("capacity lost exact decimal transport at %s: got=%#v want=%q", key, payload[key], want)
		}
	}
}

func TestLibraryStorageDirectoryProjectionKeepsUnknownCapacityNull(t *testing.T) {
	yes, no := true, false
	for _, readable := range []*bool{nil, &no, &yes} {
		item := projectLibraryStorageDirectory("/media/movies", libraryStorageDirectoryObservation{readable: readable})
		if item.Available != (readable != nil && *readable) || item.Readable != readable || item.Writable != nil || item.TotalBytes != nil || item.UsedBytes != nil || item.AvailableBytes != nil {
			t.Fatalf("unknown capacity invented access or byte values: %+v", item)
		}
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"Writable", "TotalBytes", "UsedBytes", "AvailableBytes"} {
			if value, exists := payload[key]; !exists || value != nil {
				t.Fatalf("unknown observation must remain an explicit null at %s: payload=%s", key, raw)
			}
		}
	}
}
