// SPDX-License-Identifier: GPL-3.0-only

package introskipper

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestNativeOracleParity requires the retained, independently produced native
// reference. It is deliberately not a generated expectation from this package.
// The optional native boundary adjustments were disabled in that reference.
func TestNativeOracleParity(t *testing.T) {
	root := os.Getenv("GOBY_INTROSKIPPER_ORACLE_DIR")
	if root == "" {
		t.Skip("set GOBY_INTROSKIPPER_ORACLE_DIR to the audited remote native fixture directory")
	}
	read := func(name, expectedSHA string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != expectedSHA {
			t.Fatalf("oracle file %s does not match its frozen digest", name)
		}
		return data
	}
	var input struct {
		Pools []struct {
			ID       string `json:"id"`
			Episodes []struct {
				ID              string   `json:"id"`
				DurationSeconds float64  `json:"durationSeconds"`
				FingerprintEnd  float64  `json:"fingerprintEnd"`
				Fingerprint     []uint32 `json:"fingerprint"`
			} `json:"episodes"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(read("native-fingerprints.json", "d233fe95d835ffffcf136580964c07759e533325d50bb6fe88bc2eea9a207fe5"), &input); err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Cases []struct {
			ID       string                         `json:"id"`
			Segments []struct{ Start, End float64 } `json:"segments"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(read("native-expected.json", "4044cbd4a449118693ac38ee31a9d721b87b30321e342c3d74f7dc3a93acb205"), &expected); err != nil {
		t.Fatal(err)
	}
	oracle := map[string]*Interval{}
	for _, e := range expected.Cases {
		if _, exists := oracle[e.ID]; exists || len(e.Segments) > 1 {
			t.Fatal("invalid native oracle identity or segment count")
		}
		oracle[e.ID] = nil
		if len(e.Segments) == 1 {
			interval, err := segmentInterval(segment{Start: e.Segments[0].Start, End: e.Segments[0].End})
			if err != nil {
				t.Fatal(err)
			}
			oracle[e.ID] = &interval
		}
	}
	seen, candidateCount := map[string]bool{}, 0
	for _, pool := range input.Pools {
		cohort := Cohort{Key: pool.ID}
		for _, e := range pool.Episodes {
			duration, err := secondsToTicks(e.DurationSeconds)
			if err != nil {
				t.Fatal(err)
			}
			entry := contractEpisode(e.ID, e.Fingerprint)
			entry.DurationTicks = duration
			cohort.Episodes = append(cohort.Episodes, entry)
			if got := FingerprintEndSeconds(duration, DefaultOptions()); got != e.FingerprintEnd {
				t.Fatalf("native extraction horizon differs for %s: %.17g vs %.17g", e.ID, got, e.FingerprintEnd)
			}
		}
		result, err := Analyze(context.Background(), cohort, DefaultOptions())
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range result.Episodes {
			want, exists := oracle[e.EpisodeKey]
			if !exists || seen[e.EpisodeKey] {
				t.Fatalf("unexpected or repeated episode %s", e.EpisodeKey)
			}
			seen[e.EpisodeKey] = true
			if (want != nil) != (e.Candidate != nil) {
				t.Fatalf("candidate presence differs for %s", e.EpisodeKey)
			}
			if want != nil {
				candidateCount++
				if e.Candidate.Interval != *want {
					t.Fatalf("native ticks differ for %s: got %#v want %#v", e.EpisodeKey, e.Candidate.Interval, want)
				}
			}
		}
	}
	if len(seen) != 19 || len(oracle) != 19 || candidateCount != 6 {
		t.Fatalf("incomplete oracle: episodes=%d expected=%d candidates=%d", len(seen), len(oracle), candidateCount)
	}
	t.Log("19 native episodes: 6 candidates, 13 empty outcomes, 12 exact stored endpoints; no new accuracy claim")
}
