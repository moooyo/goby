//go:build linux

package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNFORecoveryProvesUniqueLeafInAcceptedDirectoryGeneration(t *testing.T) {
	ctx := t.Context()
	for _, tc := range []struct {
		name, path, itemType, leaf, kind, document, collision string
		folder                                                bool
	}{
		{"same stem", "Film.mkv", "Movie", "Film.nfo", "movie", `<movie><title>Film</title></movie>`, "Film.mp4", false},
		{"case collision", "Film.mkv", "Movie", "Film.nfo", "movie", `<movie><title>Film</title></movie>`, "film.nfo", false},
		{"folder collision", ".", "Season", "season.nfo", "season", `<season><title>Season</title></season>`, "Season.mp4", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			write := func(leaf, data string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(path, leaf), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.folder {
				write(tc.path, "video")
			}
			write(tc.leaf, tc.document)
			dir, err := os.OpenRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := dir.Close(); err != nil {
					t.Error(err)
				}
			})
			observe := func() nfoIngestProof {
				t.Helper()
				observation, proof, err := observeNFORecoveryCandidate(ctx, dir, ".", []string{tc.leaf}, tc.path, tc.itemType, tc.folder)
				if err != nil {
					t.Fatal(err)
				}
				local, err := parseObservedNFO(observation, tc.kind, nil)
				if err != nil || local.path != tc.leaf {
					t.Fatalf("valid descriptive import was lost: path=%q err=%v", local.path, err)
				}
				return proof
			}
			unique := observe()
			if unique.recoveryDirectory == nil {
				t.Fatal("initial unique observation did not prove its directory generation")
			}
			write(tc.collision, "competing entry")
			ambiguous := observe()
			if ambiguous.recoveryDirectory != nil {
				t.Fatal("ambiguous observation authorized a recovery copy")
			}
			if err := os.Remove(filepath.Join(path, tc.collision)); err != nil {
				t.Fatal(err)
			}
			if err := unique.verifyAt(ctx, dir); err != nil {
				t.Fatalf("unrelated directory changes invalidated the ordinary sidecar stamp: %v", err)
			}
			current, err := dir.Stat(".")
			if err != nil {
				t.Fatal(err)
			}
			if err := unique.verifyRecoveryDirectory(current); !errors.Is(err, errNFORecoveryDirectoryChanged) {
				t.Fatalf("the same final entries must not reauthorize an earlier generation: %v", err)
			}
			if fresh := observe(); fresh.recoveryDirectory == nil {
				t.Fatal("a newly observed unique generation did not authorize a recovery copy")
			}
		})
	}
}
