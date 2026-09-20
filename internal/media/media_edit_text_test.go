package media

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestMediaEditEBMLTextAcceptsOnlyNeutralTrailingPadding(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  []byte
		id   uint64
		want string
	}{
		{"duration", append([]byte("00:00:02.000000000"), 0), 0x4487, "00:00:02.000000000"},
		{"multiple_zero_padding", []byte{'e', 'n', 'g', 0, 0, 0}, 0x437C, "eng"},
		{"empty", []byte{}, 0x4487, ""},
		{"all_padding", []byte{0, 0, 0}, 0x4487, ""},
		{"unicode_utf8", []byte{'C', 'a', 'f', 0xc3, 0xa9, 0, 0}, 0x85, "Caf\u00e9"},
		{"untrimmed_spaces", []byte{' ', 'N', 'a', 'm', 'e', ' ', 0}, 0x536E, " Name "},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := mediaEditEBMLText(test.raw, test.id)
			if err != nil || got != test.want {
				t.Fatalf("decoded text=%q error=%v", got, err)
			}
		})
	}
	for _, test := range []struct {
		name string
		raw  []byte
		id   uint64
	}{
		{"hidden_ascii", []byte("shown\x00hidden"), 0x4487},
		{"nonzero_after_padding", []byte{'x', 0, 0, 'y'}, 0x4487},
		{"leading_terminator", []byte{0, 'x'}, 0x4487},
		{"invalid_utf8", []byte{0xff, 0}, 0x4487},
		{"truncated_utf8", []byte{0xe2, 0x82, 0}, 0x85},
		{"ascii_control", []byte{'e', 'n', 'g', '\n', 0}, 0x437C},
		{"unicode_in_ascii", []byte{0xc3, 0xa9, 0}, 0x86},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mediaEditEBMLText(test.raw, test.id); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven text accepted: %v", err)
			}
		})
	}
}

func TestMediaEditContainerNormalizesPaddedChapterTextWithoutLosingSemantics(t *testing.T) {
	proof := func(title, language []byte) mediaEditContainerProof {
		chapter := containerTestEBML(0x1043A770, containerTestEBML(0x45B9, containerTestEBML(0xB6, containerTestUint(0x73C4, 99), containerTestUint(0x91, 0), containerTestUint(0x92, 800000000), containerTestEBML(0x80, containerTestEBML(0x85, title), containerTestEBML(0x437C, language)))))
		data := containerTestMKV(nil, nil, nil, chapter, containerTestTag("DURATION", "00:00:02.000000000\x00", 0x63C5, 11))
		file, err := os.CreateTemp(t.TempDir(), "padded-text-*.mkv")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
		result, err := mediaEditReadContainerProof(context.Background(), file, int64(len(data)), "mkv")
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	source := proof([]byte("Opening\x00\x00"), []byte("und\x00"))
	candidate := proof([]byte("Opening"), []byte("und"))
	if _, err := compareMediaEditContainerProofs(source, candidate, nil); err != nil {
		t.Fatalf("neutral EBML text padding changed chapter semantics: %v", err)
	}
	changed := proof([]byte("Changed\x00"), []byte("und\x00"))
	if _, err := compareMediaEditContainerProofs(source, changed, nil); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("padded title alteration was accepted: %v", err)
	}
}

func TestMediaEditContainerTextFailureReportsSafeRoleAndElement(t *testing.T) {
	privateValue := "hidden-private-marker"
	data := containerTestMKV(nil, nil, nil, containerTestTag("COMMENT", "visible\x00"+privateValue, 0, 0))
	_, err := containerTestAdmit(t, data, "mkv")
	if !errors.Is(err, ErrSubtitleRemovalUnsupported) || !strings.Contains(err.Error(), "role=simpletag") || !strings.Contains(err.Error(), "element=0x4487") || strings.Contains(err.Error(), privateValue) || strings.Contains(err.Error(), "container-") {
		t.Fatalf("unsafe or unhelpful text failure: %v", err)
	}
}

func TestMediaEditContainerReadFailureDoesNotExposeDescriptorPath(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "private-descriptor-marker-*")
	if err != nil {
		t.Fatal(err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	scanner := mediaEditContainerScanner{ctx: context.Background(), file: file, size: 1}
	err = scanner.read(0, make([]byte, 1))
	if !errors.Is(err, ErrSubtitleRemovalUnsupported) || !errors.Is(err, os.ErrClosed) || strings.Contains(err.Error(), name) || strings.Contains(err.Error(), "private-descriptor-marker") {
		t.Fatalf("descriptor read failure lost classification or exposed its path: %v", err)
	}
}
