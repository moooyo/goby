package metadata

import (
	"fmt"
	"io"
	"slices"
	"strings"
)

// EpisodePair identifies two adjacent episodes in the same physical season.
// Callers must obtain it from the media binding, never from the sidecar.
type EpisodePair struct {
	Season, First, Last int
}

// ParseEpisodeNFO accepts an ordinary episode NFO or a bounded pair of complete
// episode roots with explicit matching numbers. The result reports whether the
// metadata describes both episodes; on failure it contains no partial facts.
func ParseEpisodeNFO(r io.Reader, pair EpisodePair) (Metadata, bool, error) {
	if pair.Season < 0 || pair.Season > 999 || pair.First < 0 || pair.First >= 9999 || pair.Last != pair.First+1 {
		return Metadata{}, false, fmt.Errorf("invalid physical episode pair")
	}
	roots, err := readNFORoots(r, true)
	if err != nil {
		return Metadata{}, false, err
	}
	if !episodeNFORoot(roots[0]) {
		return Metadata{}, false, fmt.Errorf("NFO root is not an episode")
	}
	first, err := extractNFO(roots[0])
	if err != nil || len(roots) == 1 {
		return first, false, err
	}
	second, err := extractNFO(roots[1])
	if err != nil {
		return Metadata{}, false, err
	}
	if !matchesEpisodeNFO(roots[0], first, pair.Season, pair.First) || !matchesEpisodeNFO(roots[1], second, pair.Season, pair.Last) {
		return Metadata{}, false, fmt.Errorf("NFO episode pair does not match the physical season and file numbers")
	}
	if first.OfficialRating != second.OfficialRating || !sameNFOTags(first.Tags, second.Tags) {
		return Metadata{}, false, fmt.Errorf("NFO episode pair has conflicting rating or tag policy")
	}
	if len(first.Genres)+len(second.Genres) > maxEntries || len(first.Tags)+len(second.Tags) > maxEntries ||
		len(first.Studios)+len(second.Studios) > maxEntries || len(first.People)+len(second.People) > maxEntries ||
		len(first.ProviderIDs)+len(second.ProviderIDs) > maxEntries {
		return Metadata{}, false, fmt.Errorf("NFO episode pair exceeds collection limit")
	}
	first.Name = joinNFODescriptiveText(first.Name, second.Name, ",  ")
	first.Overview = joinNFODescriptiveText(first.Overview, second.Overview, "\n\n")
	if len(first.Name) > maxNameBytes || len(first.Overview) > maxFieldBytes {
		return Metadata{}, false, fmt.Errorf("NFO episode pair exceeds combined title or overview limit")
	}
	return first, true, nil
}

func episodeNFORoot(root *element) bool {
	return root.name.Space == "" && strings.EqualFold(root.name.Local, "episodedetails")
}

func matchesEpisodeNFO(root *element, value Metadata, season, episode int) bool {
	if value.ParentIndexNumber == nil || *value.ParentIndexNumber != season || value.IndexNumber == nil || *value.IndexNumber != episode {
		return false
	}
	var seasons, episodes int
	for _, child := range root.children {
		if child.name.Space != "" {
			continue
		}
		switch strings.ToLower(child.name.Local) {
		case "season":
			seasons++
		case "episode":
			episodes++
		}
	}
	return seasons == 1 && episodes == 1
}

func sameNFOTags(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	left, right := slices.Clone(first), slices.Clone(second)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func joinNFODescriptiveText(first, second, separator string) string {
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	return first + separator + second
}
