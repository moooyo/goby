package media

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This optional actual-test recorder copies only existing generated files.
// It does not run a probe, change fixture assertions, or alter production paths.
type mediaEditTestEvidence struct {
	Version                         int
	Test, Container, Stage, Failure string
	Failed                          bool
	CapturedAt                      time.Time
	Result                          *SubtitleRemovalEvidence     `json:",omitempty"`
	CandidateInfo                   *Info                        `json:",omitempty"`
	CandidateDecode                 *mediaEditTestDecodeEvidence `json:",omitempty"`
	Files                           []mediaEditTestEvidenceFile
}

type mediaEditTestEvidenceFile struct {
	Role, OriginalName, RetainedName, SHA256, Mode string
	Size, CopiedBytes                              int64
	ModifiedAt                                     time.Time
	Complete                                       bool
}

func retainMediaEditTestEvidence(t *testing.T, fixture, container string) *mediaEditTestEvidence {
	t.Helper()
	record := &mediaEditTestEvidence{Version: 1, Test: t.Name(), Container: container, Stage: "generating"}
	root := os.Getenv("GOBY_MEDIA_EDIT_EVIDENCE_DIR")
	if root == "" {
		return record
	}
	t.Cleanup(func() {
		record.Failed, record.CapturedAt = t.Failed(), time.Now().UTC()
		if err := record.retain(root, fixture); err != nil {
			t.Errorf("retain media edit test evidence: %v", err)
		}
	})
	return record
}

func (record *mediaEditTestEvidence) retain(path, fixture string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(filepath.Separator) || (record.Container != "mkv" && record.Container != "mka" && record.Container != "mp4") {
		return errors.New("evidence root must be a canonical owned directory")
	}
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.IsDir() || before.Mode().Perm() != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return errors.New("evidence root must be private and owned by the verifier")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(before, opened) {
		return errors.New("evidence root changed while opening")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := record.Container + "-" + hex.EncodeToString(nonce[:])
	if err := root.Mkdir(name, 0700); err != nil {
		return err
	}
	destination, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	defer destination.Close()
	input, err := os.OpenRoot(fixture)
	if err != nil {
		return err
	}
	defer input.Close()
	directory, err := input.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(32)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) >= 32 {
		return errors.New("fixture directory exceeds evidence inventory limit")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var captureErr error
	candidates := 0
	for _, entry := range entries {
		role, retained := "", ""
		if entry.Name() == "source."+record.Container {
			role, retained = "source", entry.Name()
		}
		if strings.HasPrefix(entry.Name(), "candidate-") {
			role = "candidate"
			retained = fmt.Sprintf("candidate-%02d.%s", candidates, record.Container)
			candidates++
		}
		if role == "" {
			continue
		}
		file, err := retainMediaEditTestFile(input, destination, entry.Name(), retained, role)
		record.Files = append(record.Files, file)
		captureErr = errors.Join(captureErr, err)
	}
	if captureErr != nil {
		record.Failed = true
	}
	manifest, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return errors.Join(captureErr, err)
	}
	file, err := destination.OpenFile("manifest.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.Join(captureErr, err)
	}
	_, writeErr := file.Write(append(manifest, '\n'))
	err = errors.Join(writeErr, file.Sync(), file.Close())
	return errors.Join(captureErr, err)
}

func retainMediaEditTestFile(input, destination *os.Root, original, retained, role string) (mediaEditTestEvidenceFile, error) {
	record := mediaEditTestEvidenceFile{Role: role, OriginalName: original, RetainedName: retained}
	before, err := input.Lstat(original)
	if err != nil || !before.Mode().IsRegular() {
		return record, errors.Join(errors.New("fixture artifact is not a regular file"), err)
	}
	record.Size, record.ModifiedAt, record.Mode = before.Size(), before.ModTime().UTC(), before.Mode().String()
	source, err := input.Open(original)
	if err != nil {
		return record, err
	}
	defer source.Close()
	held, err := source.Stat()
	if err != nil || !os.SameFile(before, held) {
		return record, errors.New("fixture artifact changed while opening")
	}
	target, err := destination.OpenFile(retained, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return record, err
	}
	digest := sha256.New()
	record.CopiedBytes, err = io.Copy(io.MultiWriter(target, digest), io.NewSectionReader(source, 0, before.Size()))
	after, statErr := source.Stat()
	unchanged := statErr == nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && FileChangeTime(before) == FileChangeTime(after)
	syncErr, closeErr := target.Sync(), target.Close()
	record.SHA256 = hex.EncodeToString(digest.Sum(nil))
	record.Complete = err == nil && syncErr == nil && closeErr == nil && record.CopiedBytes == record.Size && unchanged
	if !record.Complete {
		return record, errors.Join(errors.New("fixture artifact was not completely retained"), err, statErr, syncErr, closeErr)
	}
	return record, nil
}

func TestMediaEditEvidenceRetainsCompleteBytesWithoutOverwrite(t *testing.T) {
	fixture := t.TempDir()
	if err := os.WriteFile(filepath.Join(fixture, "source.mkv"), []byte("existing generated source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "candidate-existing."), nil, 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "retained")
	for i := 0; i < 2; i++ {
		record := &mediaEditTestEvidence{Version: 1, Container: "mkv", Stage: "remux_and_proof", Failed: true}
		if err := record.retain(root, fixture); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 2 {
		t.Fatalf("retention overwrote an earlier attempt: %d %v", len(entries), err)
	}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatalf("retained directory permissions: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var record mediaEditTestEvidence
		if json.Unmarshal(data, &record) != nil || !record.Failed || len(record.Files) != 2 {
			t.Fatal("failed-run evidence was not retained")
		}
		for _, file := range record.Files {
			data, err := os.ReadFile(filepath.Join(dir, file.RetainedName))
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			info, err := os.Stat(filepath.Join(dir, file.RetainedName))
			if err != nil || info.Mode().Perm() != 0600 || !file.Complete || int64(len(data)) != file.Size || file.SHA256 != hex.EncodeToString(digest[:]) {
				t.Fatalf("retained fixture bytes or metadata disagree: %+v %v", file, err)
			}
		}
	}
}

func TestMediaEditEvidenceDoesNotFollowFixtureSymlinks(t *testing.T) {
	fixture := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-data")
	if err := os.WriteFile(outside, []byte("must not be copied"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(fixture, "source.mkv")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "retained")
	record := &mediaEditTestEvidence{Version: 1, Container: "mkv"}
	if err := record.retain(root, fixture); err == nil {
		t.Fatal("evidence recorder followed an unowned source")
	}
	if len(record.Files) != 1 || record.Files[0].Complete || record.Files[0].CopiedBytes != 0 {
		t.Fatal("symlink evidence was marked complete")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatal("failure evidence was not retained")
	}
	if _, err := os.Stat(filepath.Join(root, entries[0].Name(), "source.mkv")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("symlink target bytes were copied")
	}
}
