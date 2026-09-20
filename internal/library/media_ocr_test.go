package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

func TestMediaOCRReviewRetainsPerCueWarningsWithoutOversizedSummary(t *testing.T) {
	result := media.SubtitleOCRResult{EngineSHA256: strings.Repeat("a", 64), ModelID: "eng", ModelSHA256: strings.Repeat("b", 64)}
	for index := 0; index < media.MaxSubtitleOCRCues; index++ {
		result.Cues = append(result.Cues, media.SubtitleOCRCue{StartTicks: int64(index) * media.TicksPerSecond,
			EndTicks: int64(index+1) * media.TicksPerSecond, Text: "Caption", Confidence: 40})
		result.Warnings = append(result.Warnings, fmt.Sprintf("cue_%d_low_confidence", index+1), fmt.Sprintf("cue_%d_engine_diagnostics", index+1))
	}
	review, err := mediaOCRReviewResult(MediaOperation{Parameters: MediaOperationParameters{ModelIDs: []string{"eng"}, OutputFormat: "srt"}}, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Summary) > 1024 || len(review.Cues) != media.MaxSubtitleOCRCues || len(review.Cues[media.MaxSubtitleOCRCues-1].Warnings) != 2 {
		t.Fatal("per-cue engine diagnostics were lost or expanded the summary document")
	}
	var summary struct {
		WarningCount int
		Warnings     []string
	}
	if err := json.Unmarshal(review.Summary, &summary); err != nil || summary.WarningCount != 2*media.MaxSubtitleOCRCues || len(summary.Warnings) != 0 {
		t.Fatalf("OCR diagnostic accounting changed: %v", err)
	}
}

func TestRenderMediaOCRPreservesTimingOverlapAndLiteralText(t *testing.T) {
	second := media.TicksPerSecond
	cues := []MediaOperationCue{
		{Ordinal: 0, StartTicks: 2 * second, EndTicks: 4 * second, Text: "Later & <b>literal</b>\nSecond line", Included: true},
		{Ordinal: 1, StartTicks: second, EndTicks: 3 * second, Text: "English and 中文", Included: true},
		{Ordinal: 2, StartTicks: 0, EndTicks: second, Text: "", Included: false},
	}
	for _, format := range []string{"srt", "vtt"} {
		t.Run(format, func(t *testing.T) {
			data, err := renderMediaOCRCues(cues, format, 5*second)
			if err != nil {
				t.Fatal(err)
			}
			document, err := subtitle.Parse(data, subtitle.Format(format))
			if err != nil || len(document.Cues) != 2 {
				t.Fatalf("reviewed output is not a two-cue document: %v", err)
			}
			if document.Cues[0].StartTicks != second || document.Cues[0].EndTicks != 3*second ||
				document.Cues[1].StartTicks != 2*second || document.Cues[1].EndTicks != 4*second {
				t.Fatalf("overlapping source timing was rewritten: %+v", document.Cues)
			}
			if !bytes.Contains(data, []byte("English and 中文")) || bytes.Contains(data, []byte("<b>literal</b>")) ||
				!bytes.Contains(data, []byte("&lt;b&gt;literal&lt;/b&gt;")) {
				t.Fatal("OCR plain text became markup or lost its multilingual characters")
			}
		})
	}
}

func TestRenderMediaOCRRequiresReviewableBoundedCues(t *testing.T) {
	valid := MediaOperationCue{StartTicks: 0, EndTicks: media.TicksPerSecond, Text: "Caption", Included: true}
	for _, test := range []struct {
		name   string
		mutate func(*MediaOperationCue)
	}{
		{"empty", func(cue *MediaOperationCue) { cue.Text = " " }},
		{"negative", func(cue *MediaOperationCue) { cue.StartTicks = -1 }},
		{"zero-duration", func(cue *MediaOperationCue) { cue.EndTicks = 0 }},
		{"beyond-source", func(cue *MediaOperationCue) { cue.EndTicks = 3 * media.TicksPerSecond }},
		{"invalid-utf8", func(cue *MediaOperationCue) { cue.Text = string([]byte{0xff}) }},
		{"control", func(cue *MediaOperationCue) { cue.Text = "caption\x00" }},
		{"oversized-cue", func(cue *MediaOperationCue) { cue.Text = strings.Repeat("x", subtitle.MaxCueTextBytes+1) }},
		{"blank-line-syntax", func(cue *MediaOperationCue) { cue.Text = "First\n\nInjected block" }},
		{"all-excluded", func(cue *MediaOperationCue) { cue.Included = false }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cue := valid
			test.mutate(&cue)
			if data, err := renderMediaOCRCues([]MediaOperationCue{cue}, "srt", 2*media.TicksPerSecond); !errors.Is(err, ErrInvalidInput) || len(data) != 0 {
				t.Fatalf("invalid review produced a subtitle: bytes=%d error=%v", len(data), err)
			}
		})
	}
}

func TestOwnedSubtitleValidationRejectsTamperAndFilesystemImpersonation(t *testing.T) {
	data := []byte("1\n00:00:01,000 --> 00:00:02,000\nCaption\n")
	digest := sha256.Sum256(data)
	valid := storedSubtitle{Subtitle: Subtitle{Index: 4, Codec: "srt", Language: "zh-CN", Title: "Reviewed captions",
		MIMEType: subtitleMIME("srt"), Tag: hex.EncodeToString(digest[:]), Size: int64(len(data)), ModifiedAt: time.Now().UTC(), Owned: true},
		rootID: "owned-root", ownedData: data}
	if err := validateOwnedSubtitle(valid); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*storedSubtitle)
	}{
		{"hash", func(track *storedSubtitle) { track.Tag = strings.Repeat("0", 64) }},
		{"bytes", func(track *storedSubtitle) { track.ownedData = []byte("different bytes") }},
		{"codec", func(track *storedSubtitle) { track.Codec = "ass" }},
		{"sidecar-name", func(track *storedSubtitle) { track.Filename = "Feature.en.srt" }},
		{"sidecar-path", func(track *storedSubtitle) { track.relativePath = "movies/Feature.en.srt" }},
		{"sidecar-identity", func(track *storedSubtitle) { track.identity = "inode" }},
		{"language", func(track *storedSubtitle) { track.Language = "en/../../other" }},
		{"title-control", func(track *storedSubtitle) { track.Title = "Caption\nInjected" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			track := valid
			test.mutate(&track)
			if err := validateOwnedSubtitle(track); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("invalid owned row remained usable: %v", err)
			}
		})
	}
}
