package library

import (
	"errors"
	"runtime"
	"testing"
)

func TestNFORecoveryRootsRequireExactApprovedRegistration(t *testing.T) {
	approved := []approvedRoot{{path: "/media"}}
	paths := []string{"/media/movies"}
	option := WithNFORecoveryRoots(paths)
	paths[0] = "/media"
	var settings storeOptions
	if err := option(&settings); err != nil {
		t.Fatal(err)
	}
	roots, err := assignNFORecoveryRoots(settings.nfoRecoveryPaths, approved)
	if runtime.GOOS != "linux" {
		if !errors.Is(err, ErrInvalidInput) || roots != nil {
			t.Fatalf("unsupported platform accepted NFO write roots: %v, %v", roots, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	store := Store{roots: approved, nfoRecoveryRoots: roots}
	exact := libraryRoot{id: "root", libraryID: "library", path: "/media/movies", allowedPath: "/media", relativePath: "movies"}
	if !store.nfoRecoveryWritable(exact) || (&Store{roots: approved}).nfoRecoveryWritable(exact) {
		t.Fatal("NFO write permission did not require explicit opt-in")
	}
	if exactRoots, err := assignNFORecoveryRoots([]string{"/media"}, approved); err != nil ||
		(&Store{roots: approved, nfoRecoveryRoots: exactRoots}).nfoRecoveryWritable(exact) {
		t.Fatalf("an opted-in media anchor granted writes to a separately registered child: %v", err)
	}
	for _, root := range []libraryRoot{
		{id: "parent", libraryID: "library", path: "/media", allowedPath: "/media", relativePath: "."},
		{id: "child", libraryID: "library", path: "/media/movies/kids", allowedPath: "/media", relativePath: "movies/kids"},
		{id: "different-anchor", libraryID: "library", path: "/media/movies", allowedPath: "/", relativePath: "media/movies"},
		{id: "invalid-relative", libraryID: "library", path: "/media/movies", allowedPath: "/media", relativePath: "other"},
		{libraryID: "library", path: "/media/movies", allowedPath: "/media", relativePath: "movies"},
	} {
		if store.nfoRecoveryWritable(root) {
			t.Errorf("unexpected NFO write permission for %+v", root)
		}
	}
}

func TestNFORecoveryRootsRejectInvalidDefinitionsWithoutPartialPermission(t *testing.T) {
	approved := []approvedRoot{{path: "/media"}}
	for _, path := range []string{"", "movies", "/media/../other", "/media-other/movies", "/", "/other", "/media/\x00bad"} {
		if roots, err := assignNFORecoveryRoots([]string{"/media/movies", path}, approved); !errors.Is(err, ErrInvalidInput) || roots != nil {
			t.Errorf("invalid declaration %q = %v, %v", path, roots, err)
		}
	}
	if roots, err := assignNFORecoveryRoots(nil, approved); err != nil || roots != nil {
		t.Fatalf("empty deployment declaration = %v, %v", roots, err)
	}
}

func TestNFORecoveryRootsRejectRepeatedDeclarations(t *testing.T) {
	var settings storeOptions
	if err := WithNFORecoveryRoots(nil)(&settings); err != nil {
		t.Fatal(err)
	}
	if err := WithNFORecoveryRoots([]string{"/media/movies"})(&settings); !errors.Is(err, ErrInvalidInput) || len(settings.nfoRecoveryPaths) != 0 {
		t.Fatalf("repeated deployment declaration changed permissions: %v, %v", settings.nfoRecoveryPaths, err)
	}
}
