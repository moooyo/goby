package media

import (
	"context"
	"errors"
	"os"
	"testing"
)

func chapterProofTestContainer(t *testing.T, language *string, title string, displayed bool) mediaEditContainerProof {
	t.Helper()
	elements := [][]byte{containerTestUint(0x73C4, 99), containerTestUint(0x91, 0), containerTestUint(0x92, 800000000)}
	if displayed {
		display := [][]byte{containerTestEBML(0x85, []byte(title))}
		if language != nil {
			display = append(display, containerTestEBML(0x437C, []byte(*language)))
		}
		elements = append(elements, containerTestEBML(0x80, display...))
	}
	chapter := containerTestEBML(0x1043A770, containerTestEBML(0x45B9, containerTestEBML(0xB6, elements...)))
	data := containerTestMKV(nil, nil, nil, chapter)
	file, err := os.CreateTemp(t.TempDir(), "chapter-proof-*.mkv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	proof, err := mediaEditReadContainerProof(context.Background(), file, int64(len(data)), "mkv")
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func TestMediaEditChapterLanguageProofCapturesMuxerUndAndMatroskaDefault(t *testing.T) {
	for _, language := range []string{"und", "eng", "fra", "chi"} {
		t.Run(language, func(t *testing.T) {
			proof := chapterProofTestContainer(t, &language, "Opening", true)
			if len(proof.Chapters) != 1 || proof.Chapters[0].Language != language || proof.Chapters[0].Title != "Opening" || !proof.Chapters[0].HasDisplay || proof.Chapters[0].EndNanoseconds != 800000000 {
				t.Fatalf("chapter display facts were not preserved: %+v", proof.Chapters)
			}
			if digest, err := compareMediaEditContainerProofs(proof, proof, nil); err != nil || len(digest) != 64 {
				t.Fatalf("matching chapter display proof=%q %v", digest, err)
			}
		})
	}
	implicit := chapterProofTestContainer(t, nil, "Opening", true)
	english := "eng"
	explicit := chapterProofTestContainer(t, &english, "Opening", true)
	if implicit.Chapters[0].Language != "eng" {
		t.Fatal("absent ChapLanguage did not retain its Matroska default")
	}
	if _, err := compareMediaEditContainerProofs(implicit, explicit, nil); err != nil {
		t.Fatalf("equivalent implicit/explicit English chapter language rejected: %v", err)
	}
}

func TestMediaEditChapterLanguageProofRejectsHiddenRemuxChanges(t *testing.T) {
	english, undefined, french := "eng", "und", "fra"
	source := chapterProofTestContainer(t, &undefined, "Opening", true)
	for _, test := range []struct {
		name          string
		before, after mediaEditContainerProof
	}{
		{"default_to_undefined", chapterProofTestContainer(t, nil, "Opening", true), source},
		{"english_to_undefined", chapterProofTestContainer(t, &english, "Opening", true), source},
		{"french_to_undefined", chapterProofTestContainer(t, &french, "Opening", true), source},
		{"renamed_title", source, chapterProofTestContainer(t, &undefined, "Different title", true)},
		{"lost_display", source, chapterProofTestContainer(t, nil, "", false)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := compareMediaEditContainerProofs(test.before, test.after, nil); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("hidden chapter change accepted: %v", err)
			}
		})
	}
	changed := source
	changed.Chapters = append([]mediaEditChapterDisplayProof(nil), source.Chapters...)
	changed.Chapters[0].EndNanoseconds++
	if _, err := compareMediaEditContainerProofs(source, changed, nil); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("sub-tick chapter change accepted: %v", err)
	}
}

func TestMediaEditChapterLanguageProfileRejectsUnrepresentedOverrides(t *testing.T) {
	for _, extra := range [][]byte{
		containerTestEBML(0x437D, []byte("fr-FR")),
		containerTestEBML(0x437E, []byte("FR")),
		containerTestEBML(0x437C, []byte("und"), []byte("eng")),
		append(containerTestEBML(0x437C, []byte("und")), containerTestEBML(0x437C, []byte("eng"))...),
		containerTestEBML(0x437C, []byte("")),
	} {
		chapter := containerTestEBML(0x1043A770, containerTestEBML(0x45B9, containerTestEBML(0xB6, containerTestUint(0x73C4, 99), containerTestUint(0x91, 0), containerTestUint(0x92, 1), containerTestEBML(0x80, containerTestEBML(0x85, []byte("Opening")), extra))))
		if _, err := containerTestAdmit(t, containerTestMKV(nil, nil, nil, chapter), "mkv"); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
			t.Fatalf("unrepresented chapter language extension accepted: %v", err)
		}
	}
}
