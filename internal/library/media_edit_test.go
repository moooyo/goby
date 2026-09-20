package library

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestMediaEditCandidateWitnessBindsEveryPublicationPrecondition(t *testing.T) {
	operation := MediaOperation{ID: strings.Repeat("1", 32), Kind: MediaOperationRemoveSubtitle, ItemID: "item", LibraryID: "library", RootID: "root", MediaSourceID: media.SourceID("item"), SourceRevision: "source-a", StreamIndex: 2}
	now := time.Unix(1700000000, 123000).UTC()
	journal := mediaEditJournal{Version: 1, OperationID: operation.ID, SourceRevision: operation.SourceRevision, StreamIndex: 2, Container: "mkv", Host: strings.Repeat("a", 64), StageName: ".goby-edit-" + operation.ID, StageIdentity: "1:2", SourceSHA256: strings.Repeat("b", 64), AttributeSHA256: strings.Repeat("c", 64),
		Target:    deletionTargetDocument{Target: fileDeletionTarget{ItemID: "item", LibraryID: "library", BindingRevision: 1, BindingFingerprint: "binding", File: fileDeletionSpec{RelativePath: "Film.mkv"}}, Root: deletionRootDocument{ID: "root", LibraryID: "library"}},
		Candidate: mediaEditFile{Identity: "1:3", Size: 100, ModifiedAt: now, ChangeTimeNs: 99, SHA256: strings.Repeat("d", 64)}, CandidateMedia: media.Info{ProbeVersion: media.CurrentProbeVersion, FileChangeTimeNs: 99, Size: 100, Streams: []media.Stream{{Index: 0, CodecType: "video"}}}, Proof: media.SubtitleRemovalEvidence{SourceBytes: 123}}
	var err error
	journal.ReadyHash, err = mediaEditReadyHash(journal)
	if err != nil {
		t.Fatal(err)
	}
	operation.ResultHash = journal.ReadyHash
	operation.Journal, _ = json.Marshal(journal)
	if _, err := decodeMediaEditJournal(operation); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name string
		edit func(*mediaEditJournal)
	}{
		{"source", func(j *mediaEditJournal) { j.SourceRevision = "source-b" }},
		{"stream", func(j *mediaEditJournal) { j.StreamIndex = 1 }},
		{"candidate_digest", func(j *mediaEditJournal) { j.Candidate.SHA256 = strings.Repeat("e", 64) }},
		{"root_binding", func(j *mediaEditJournal) { j.Target.Target.BindingFingerprint = "changed" }},
		{"stage_identity", func(j *mediaEditJournal) { j.StageIdentity = "1:4" }},
		{"source_digest", func(j *mediaEditJournal) { j.SourceSHA256 = strings.Repeat("e", 64) }},
		{"probe", func(j *mediaEditJournal) { j.CandidateMedia.DurationTicks = 1 }},
		{"proof", func(j *mediaEditJournal) { j.Proof.SourceBytes = 0 }},
		{"host", func(j *mediaEditJournal) { j.Host = strings.Repeat("e", 64) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			copy := journal
			change.edit(&copy)
			operation.Journal, _ = json.Marshal(copy)
			if _, err := decodeMediaEditJournal(operation); err == nil {
				t.Fatal("modified ready witness accepted")
			}
		})
	}
	journal.Published = &mediaEditFile{Identity: journal.Candidate.Identity, Size: journal.Candidate.Size, ModifiedAt: journal.Candidate.ModifiedAt, ChangeTimeNs: 100, SHA256: journal.Candidate.SHA256}
	journal.PublishedSeekIdentity = strings.Repeat("f", 64)
	operation.Journal, _ = json.Marshal(journal)
	if _, err := decodeMediaEditJournal(operation); err != nil {
		t.Fatalf("publication observation changed immutable ready evidence: %v", err)
	}
}

func TestMediaEditProfileAndStageAdmission(t *testing.T) {
	for path, want := range map[string]string{"Film.MKV": "mkv", "Album.mka": "mka", "Film.mp4": "mp4", "Film.webm": "", "Film.mov": "", "Film.avi": "", "Film.mkv/remote.m3u8": ""} {
		if got := mediaEditContainer(path); got != want {
			t.Errorf("profile %q=%q, want %q", path, got, want)
		}
	}
	for _, name := range []string{".goby-edit-", ".goby-edit-" + strings.Repeat("A", 32), ".goby-edit-../" + strings.Repeat("a", 32), ".goby-delete-" + strings.Repeat("a", 32)} {
		if validMediaEditStageName(name) {
			t.Errorf("unsafe stage %q accepted", name)
		}
	}
	if !validMediaEditStageName(".goby-edit-" + strings.Repeat("a", 32)) {
		t.Fatal("server generated edit stage rejected")
	}
}
