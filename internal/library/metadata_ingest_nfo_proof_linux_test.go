//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataIngestNFOProofRejectsCandidateAppearingDuringFinalCheck(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "movie.nfo"), []byte(`<movie><mpaa>PG</mpaa></movie>`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	observation, proof, err := observeMetadataIngestNFO(ctx, dir, ".", []string{"Film.nfo", "movie.nfo"})
	if err != nil || observation.invalid || observation.path != "movie.nfo" || len(proof.entries) != 2 {
		t.Fatalf("initial observation did not select the fallback: %+v, %v", observation, err)
	}
	if err := proof.verifyAt(ctx, dir); err != nil {
		t.Fatalf("unchanged selection failed: %v", err)
	}
	changed := &nfoAppearingDirectory{Root: dir, mutate: func() error {
		return os.WriteFile(filepath.Join(path, "Film.nfo"), []byte(`<movie><mpaa>R</mpaa></movie>`), 0o600)
	}}
	if err := proof.verifyAt(ctx, changed); !errors.Is(err, errMetadataIngestChanged) || changed.mutationErr != nil || !changed.inserted {
		t.Fatalf("the winning candidate changed between lookups: injected=%t error=%v, proof=%v", changed.inserted, changed.mutationErr, err)
	}
}

type nfoAppearingDirectory struct {
	*os.Root
	mutate      func() error
	mutationErr error
	inserted    bool
}

func (dir *nfoAppearingDirectory) Lstat(name string) (os.FileInfo, error) {
	info, err := dir.Root.Lstat(name)
	if name == "Film.nfo" && errors.Is(err, os.ErrNotExist) && !dir.inserted {
		dir.inserted = true
		dir.mutationErr = dir.mutate()
	}
	return info, err
}
