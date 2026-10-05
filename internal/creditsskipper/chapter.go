// SPDX-FileCopyrightText: 2022 ConfusedPolarBear
// SPDX-FileCopyrightText: 2024-2026 rlauuzo
// SPDX-FileCopyrightText: 2024-2026 AbandonedCart
// SPDX-FileCopyrightText: 2024-2026 Kilian von Pflugk
// SPDX-License-Identifier: GPL-3.0-only

package creditsskipper

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Match candidate keywords before evaluating the surrounding clauses below.
var chapterWords = regexp.MustCompile(`(?i)(Credits?|Ending|ED|Outro)`)

// DefaultChapterMatches evaluates the pinned default .NET expression without
// translating its negative lookahead into an unsupported Go regular expression.
// The surrounding whitespace uses Unicode IsSpace, matching .NET's default \s.
func DefaultChapterMatches(name string, sponsorBlock bool) bool {
	const prefix = "[SponsorBlock]:"
	if sponsorBlock && len(name) >= len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
		label := strings.TrimSpace(name[len(prefix):])
		if strings.EqualFold(label, "outro") || strings.EqualFold(label, "endcards/credits") {
			return true
		}
	}
	for _, loc := range chapterWords.FindAllStringIndex(name, -1) {
		if loc[0] > 0 {
			previous, _ := utf8.DecodeLastRuneInString(name[:loc[0]])
			if !unicode.IsSpace(previous) {
				continue
			}
		}
		suffix := name[loc[1]:]
		if suffix != "" {
			next, _ := utf8.DecodeRuneInString(suffix)
			if next != ':' && !unicode.IsSpace(next) {
				continue
			}
		}
		trimmed := strings.TrimLeftFunc(suffix, func(r rune) bool { return r == ':' || unicode.IsSpace(r) })
		if len(trimmed) != len(suffix) && len(trimmed) >= 3 && strings.EqualFold(trimmed[:3], "End") {
			continue
		}
		return true
	}
	return false
}

// FindChapterCandidates preserves the upstream credits reverse order and
// single-match policy. An adjacent matching chapter makes the later start
// ambiguous, so that candidate is skipped rather than combined here.
func FindChapterCandidates(chapters []Chapter, duration float64, isMovie bool, options Options) []Segment {
	minimum, maximum := float64(options.MinimumCreditsDuration), float64(options.MaximumCreditsDuration)
	if isMovie {
		maximum = float64(options.MaximumMovieCreditsDuration)
	}
	if options.FullLengthChapters {
		minimum, maximum = 1, duration-1
	}
	for i := len(chapters) - 1; i >= 0; i-- {
		chapter := chapters[i]
		end := duration
		if i+1 < len(chapters) {
			end = chapters[i+1].StartSeconds
		}
		if strings.TrimSpace(chapter.Name) == "" || end-chapter.StartSeconds < minimum || end-chapter.StartSeconds > maximum {
			continue
		}
		if !DefaultChapterMatches(chapter.Name, options.EnableSponsorBlockChapterDetection) {
			continue
		}
		if i > 0 && strings.TrimSpace(chapters[i-1].Name) != "" && DefaultChapterMatches(chapters[i-1].Name, options.EnableSponsorBlockChapterDetection) {
			continue
		}
		return []Segment{{chapter.StartSeconds, end, ChapterSource}}
	}
	return nil
}
