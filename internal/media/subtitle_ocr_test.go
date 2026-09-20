package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func subtitleOCRTestTSV(rows ...string) []byte {
	return []byte(subtitleOCRTSVHeader + "\n1\t1\t0\t0\t0\t0\t0\t0\t100\t60\t-1\t\n" + strings.Join(rows, "\n") + "\n")
}

func subtitleOCRTestWord(line, word int, confidence, text string) string {
	return fmt.Sprintf("5\t1\t1\t1\t%d\t%d\t0\t0\t20\t10\t%s\t%s", line, word, confidence, text)
}

func TestSubtitleOCRTSVRetainsLineBreaksAndWordConfidence(t *testing.T) {
	data := subtitleOCRTestTSV(
		"2\t1\t1\t0\t0\t0\t0\t0\t100\t60\t-1\t",
		subtitleOCRTestWord(1, 1, "90.5", "First"),
		subtitleOCRTestWord(1, 2, "59.5", "line."),
		subtitleOCRTestWord(2, 1, "30", "Second"),
	)
	text, confidence, err := parseSubtitleOCRTSV(data, 100, 60, "eng")
	if err != nil || text != "First line.\nSecond" || confidence != 60 {
		t.Fatalf("recognized layout or confidence changed: %q, %v, %v", text, confidence, err)
	}
	text, confidence, err = parseSubtitleOCRTSV(bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n")), 100, 60, "eng")
	if err != nil || text != "First line.\nSecond" || confidence != 60 {
		t.Fatalf("CRLF output changed recognition: %q, %v, %v", text, confidence, err)
	}
}

func TestSubtitleOCRTSVChineseAndMixedModelWordSpacing(t *testing.T) {
	for _, model := range []string{"chi_sim", "chi_tra", "chi_sim+eng", "chi_sim+chi_tra+eng"} {
		t.Run(model, func(t *testing.T) {
			data := subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, "96", "\u4f60"),
				subtitleOCRTestWord(1, 2, "94", "\u597d"), subtitleOCRTestWord(1, 3, "92", "\uff0c"),
				subtitleOCRTestWord(1, 4, "90", "hello"), subtitleOCRTestWord(1, 5, "88", "world"))
			text, confidence, err := parseSubtitleOCRTSV(data, 100, 60, model)
			if err != nil || text != "\u4f60\u597d\uff0chello world" || confidence != 92 {
				t.Fatalf("mixed language text changed: %q, %v, %v", text, confidence, err)
			}
		})
	}
}

func TestSubtitleOCRTSVEmptyPageRemainsExplicitlyEmpty(t *testing.T) {
	data := []byte(subtitleOCRTSVHeader + "\n1\t1\t0\t0\t0\t0\t0\t0\t100\t60\t-1\t\n")
	text, confidence, err := parseSubtitleOCRTSV(data, 100, 60, "eng")
	if err != nil || text != "" || confidence != 0 {
		t.Fatalf("empty page fabricated recognition: %q, %v, %v", text, confidence, err)
	}
	if _, _, err := parseSubtitleOCRTSV([]byte(subtitleOCRTSVHeader+"\n"), 100, 60, "eng"); !errors.Is(err, ErrSubtitleOCR) {
		t.Fatalf("missing page was accepted as a valid empty page: %v", err)
	}
}

func TestSubtitleOCRTSVRejectsMalformedUnboundedAndNonFiniteEvidence(t *testing.T) {
	valid := subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, "80", "recognized"))
	tests := map[string][]byte{
		"missing header":     valid[len(subtitleOCRTSVHeader)+1:],
		"invalid header":     bytes.Replace(valid, []byte("word_num"), []byte("word_id"), 1),
		"unknown page":       bytes.ReplaceAll(valid, []byte("5\t1\t"), []byte("5\t2\t")),
		"duplicate page":     append(append([]byte(nil), valid...), []byte("1\t1\t0\t0\t0\t0\t0\t0\t100\t60\t-1\t\n")...),
		"missing columns":    subtitleOCRTestTSV("5\t1"),
		"negative rectangle": subtitleOCRTestTSV("5\t1\t1\t1\t1\t1\t-1\t0\t20\t10\t80\tword"),
		"outside rectangle":  subtitleOCRTestTSV("5\t1\t1\t1\t1\t1\t90\t0\t20\t10\t80\tword"),
		"empty rectangle":    subtitleOCRTestTSV("5\t1\t1\t1\t1\t1\t0\t0\t0\t10\t80\tword"),
		"unknown level":      subtitleOCRTestTSV("6\t1\t1\t1\t1\t1\t0\t0\t20\t10\t80\tword"),
		"invalid hierarchy":  subtitleOCRTestTSV("5\t1\t0\t1\t1\t1\t0\t0\t20\t10\t80\tword"),
		"tab injection":      subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, "80", "word\tmore")),
		"nul injection":      subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, "80", "word\x00more")),
		"invalid UTF-8":      append(append([]byte(nil), valid...), 0xff),
		"output budget":      bytes.Repeat([]byte("x"), subtitleOCRMaxTSVBytes+1),
		"word budget":        subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, "80", strings.Repeat("a", MaxSubtitleOCRCueTextBytes+1))),
		"combined word budget": subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, "80", strings.Repeat("a", MaxSubtitleOCRCueTextBytes/2)),
			subtitleOCRTestWord(1, 2, "80", strings.Repeat("b", MaxSubtitleOCRCueTextBytes/2))),
		"hierarchy text": subtitleOCRTestTSV("4\t1\t1\t1\t1\t0\t0\t0\t20\t10\t-1\tword"),
	}
	for _, value := range []string{"NaN", "+Inf", "-Inf", "-1", "100.01", "invalid", ""} {
		tests["confidence "+value] = subtitleOCRTestTSV(subtitleOCRTestWord(1, 1, value, "word"))
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if text, confidence, err := parseSubtitleOCRTSV(data, 100, 60, "eng"); !errors.Is(err, ErrSubtitleOCR) || text != "" || confidence != 0 {
				t.Fatalf("invalid evidence accepted: text bytes=%d confidence=%v error=%v", len(text), confidence, err)
			}
		})
	}
	for _, key := range []string{"", "eng+chi_sim", "eng+eng", "../eng", "eng -c debug_file=/tmp/report"} {
		if _, _, err := parseSubtitleOCRTSV(valid, 100, 60, key); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("unadmitted model key %q accepted: %v", key, err)
		}
	}
}

func TestSubtitleOCRImagePreparationPreservesEvidenceAndNormalizesContrast(t *testing.T) {
	for _, test := range []struct {
		name       string
		background color.NRGBA
		foreground color.NRGBA
	}{
		{"transparent white glyph", color.NRGBA{}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{"transparent black glyph", color.NRGBA{}, color.NRGBA{A: 255}},
		{"opaque white on black", color.NRGBA{A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{"opaque black on white", color.NRGBA{R: 255, G: 255, B: 255, A: 255}, color.NRGBA{A: 255}},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := image.NewNRGBA(image.Rect(7, 9, 12, 14))
			for y := 9; y < 14; y++ {
				for x := 7; x < 12; x++ {
					original.SetNRGBA(x, y, test.background)
				}
			}
			original.SetNRGBA(9, 11, test.foreground)
			pixels := append([]byte(nil), original.Pix...)
			prepared, err := prepareSubtitleOCRImage(original)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(pixels, original.Pix) || original.Bounds() != image.Rect(7, 9, 12, 14) {
				t.Fatal("OCR preprocessing modified original evidence")
			}
			if prepared.GrayAt(12, 12).Y != 0 || prepared.GrayAt(10, 10).Y != 255 || prepared.GrayAt(0, 0).Y != 255 || prepared.Bounds() != image.Rect(0, 0, 25, 25) {
				t.Fatal("OCR preprocessing lost the glyph, background, or safe border")
			}
			data, err := encodeSubtitleOCRPNG(original, 4096)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := png.Decode(bytes.NewReader(data))
			if err != nil || color.NRGBAModel.Convert(decoded.At(2, 2)) != test.foreground || color.NRGBAModel.Convert(decoded.At(0, 0)) != test.background {
				t.Fatalf("original PNG evidence changed: %v", err)
			}
		})
	}
}

func TestSubtitleOCRImageBudgetsRejectOversizedAndTruncatedEvidence(t *testing.T) {
	for _, image := range []*image.NRGBA{nil, {}, {Rect: image.Rect(0, 0, 4097, 1)}, {Rect: image.Rect(0, 0, 1, 2161)}} {
		if _, err := prepareSubtitleOCRImage(image); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("invalid bitmap dimensions were accepted: %v", err)
		}
	}
	original := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	data, err := encodeSubtitleOCRPNG(original, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if exact, err := encodeSubtitleOCRPNG(original, len(data)); err != nil || !bytes.Equal(exact, data) {
		t.Fatalf("exact evidence budget changed its bytes: %v", err)
	}
	// Include signature-only and near-complete budgets: PNG writes its header
	// through io.WriteString, which must share the same bound as chunk writes.
	for limit := 1; limit < len(data); limit++ {
		if encoded, err := encodeSubtitleOCRPNG(original, limit); !errors.Is(err, ErrOutputLimit) || encoded != nil {
			t.Fatalf("oversized evidence was accepted with budget %d: %v", limit, err)
		}
	}
}

func TestSubtitleOCRImageWriterFastPathsCannotBypassTheBudget(t *testing.T) {
	writer := &subtitleOCRImageBuffer{remaining: 4}
	if n, err := io.WriteString(writer, "ab"); err != nil || n != 2 {
		t.Fatalf("string write failed within budget: %d, %v", n, err)
	}
	// LimitedReader omits WriterTo, allowing io.Copy to use an exposed ReadFrom.
	if n, err := io.Copy(writer, io.LimitReader(strings.NewReader("cd"), 2)); err != nil || n != 2 {
		t.Fatalf("reader copy failed within budget: %d, %v", n, err)
	}
	if n, err := io.WriteString(writer, "x"); !errors.Is(err, ErrOutputLimit) || n != 0 {
		t.Fatalf("string writer bypassed the exhausted budget: %d, %v", n, err)
	}
	if n, err := io.Copy(writer, io.LimitReader(strings.NewReader("y"), 1)); !errors.Is(err, ErrOutputLimit) || n != 0 {
		t.Fatalf("reader copy bypassed the exhausted budget: %d, %v", n, err)
	}
	if writer.buffer.String() != "abcd" || writer.remaining != 0 {
		t.Fatal("rejected write modified retained evidence or its budget")
	}
}

func TestSubtitleOCRModelsAreExplicitDistinctAndCanonical(t *testing.T) {
	models := []SubtitleOCRModel{
		{ID: "eng", Path: "/operator/eng.traineddata", SHA256: strings.Repeat("a", 64)},
		{ID: "chi_sim", Path: "/operator/chi_sim.traineddata", SHA256: strings.Repeat("b", 64)},
		{ID: "chi_tra", Path: "/operator/chi_tra.traineddata", SHA256: strings.Repeat("c", 64)},
	}
	first, err := selectSubtitleOCRModels(models, []string{"eng", "chi_sim"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := selectSubtitleOCRModels(models, []string{"chi_sim", "eng"})
	if err != nil || !reflect.DeepEqual(first, second) || len(first) != 2 || first[0].ID != "chi_sim" || first[1].ID != "eng" {
		t.Fatalf("model request order changed execution identity: %+v, %+v, %v", first, second, err)
	}
	key, digest, identities := subtitleOCRModelIdentity(first)
	manifest := sha256.Sum256([]byte("chi_sim:" + strings.Repeat("b", 64) + "\neng:" + strings.Repeat("a", 64) + "\n"))
	if key != "chi_sim+eng" || digest != hex.EncodeToString(manifest[:]) || len(identities) != 2 || identities[0].ID != "chi_sim" || identities[1].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("multi-model provenance is incomplete: %q, %q, %+v", key, digest, identities)
	}
	key, digest, identities = subtitleOCRModelIdentity(models[:1])
	if key != "eng" || digest != models[0].SHA256 || len(identities) != 1 {
		t.Fatal("single-model identity no longer identifies its exact bytes")
	}
	for _, requested := range [][]string{nil, {}, {"eng", "eng"}, {"eng+chi_sim"}, {"../eng"}, {"jpn"}, {"eng", "chi_sim", "chi_tra", "eng"}} {
		if selected, err := selectSubtitleOCRModels(models, requested); !errors.Is(err, ErrSubtitleOCR) || selected != nil {
			t.Fatalf("invalid request selected a subset: %q, %+v, %v", requested, selected, err)
		}
	}
	if _, err := selectSubtitleOCRModels(models[:1], []string{"eng", "chi_sim"}); !errors.Is(err, ErrSubtitleOCR) {
		t.Fatalf("missing requested model silently fell back: %v", err)
	}
	if _, err := selectSubtitleOCRModels(append(models[:1:1], models[0]), []string{"eng"}); !errors.Is(err, ErrSubtitleOCR) {
		t.Fatalf("duplicate configured model selected arbitrarily: %v", err)
	}
}

func TestSubtitleOCRModelCopyPinsBytesAndPreservesTheSourceOffset(t *testing.T) {
	content := []byte("fixed language model bytes")
	digest := sha256.Sum256(content)
	directory := t.TempDir()
	path := filepath.Join(directory, "source.traineddata")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	before, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	model := SubtitleOCRModel{ID: "eng", Path: path, SHA256: hex.EncodeToString(digest[:])}
	copied, copiedBefore, err := copySubtitleOCRModel(context.Background(), source, before, directory, model)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	actual, err := io.ReadAll(io.NewSectionReader(copied, 0, copiedBefore.Size()))
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("private model differs from pinned bytes: %v", err)
	}
	if offset, err := source.Seek(0, io.SeekCurrent); err != nil || offset != 7 {
		t.Fatalf("copy modified the admitted source offset: %d, %v", offset, err)
	}
	if err := verifySubtitleOCRFile(context.Background(), copied, copiedBefore, model.SHA256); err != nil {
		t.Fatalf("pinned model copy did not revalidate: %v", err)
	}
	if _, err := source.WriteAt([]byte("X"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := verifySubtitleOCRFile(context.Background(), source, before, model.SHA256); !errors.Is(err, ErrSubtitleOCR) {
		t.Fatalf("same-size content change with restored mtime was accepted: %v", err)
	}
	if err := verifySubtitleOCRFile(context.Background(), copied, copiedBefore, model.SHA256); err != nil {
		t.Fatalf("external model mutation affected the private copy: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifySubtitleOCRFile(ctx, copied, copiedBefore, model.SHA256); !errors.Is(err, context.Canceled) {
		t.Fatalf("identity verification ignored cancellation: %v", err)
	}
}

func TestSubtitleOCRModelCopyRejectsWrongHashAndExistingDestination(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "source")
	if err := os.WriteFile(path, []byte("model"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	model := SubtitleOCRModel{ID: "eng", Path: path, SHA256: strings.Repeat("0", 64)}
	if copied, _, err := copySubtitleOCRModel(context.Background(), file, before, directory, model); !errors.Is(err, ErrSubtitleOCR) || copied != nil {
		t.Fatalf("wrong model digest was copied successfully: %v", err)
	}
	if copied, _, err := copySubtitleOCRModel(context.Background(), file, before, directory, model); !errors.Is(err, ErrSubtitleOCR) || copied != nil {
		t.Fatalf("existing model destination was overwritten: %v", err)
	}
}

func TestSubtitleOCRRejectsScriptEnginesAndUntrustedEnvironment(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "tesseract")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSubtitleOCREngine(file, before); !errors.Is(err, ErrSubtitleOCR) {
		t.Fatalf("script engine was accepted: %v", err)
	}
	for _, key := range []string{"PATH", "TESSDATA_PREFIX", "LD_LIBRARY_PATH", "LD_PRELOAD", "HOME", "OMP_THREAD_LIMIT", "TMPDIR"} {
		t.Setenv(key, "untrusted-marker")
	}
	environment := strings.Join(subtitleOCREnvironment(), "\n")
	if strings.Contains(environment, "untrusted-marker") || strings.Contains(environment, "TESSDATA_PREFIX=") || strings.Contains(environment, "LD_") ||
		!strings.Contains(environment, "OMP_THREAD_LIMIT=1") || !strings.Contains(environment, "OMP_NUM_THREADS=1") {
		t.Fatal("OCR inherited user configuration, loader state, or thread settings")
	}
	arguments := subtitleOCRArguments("chi_sim+eng")
	for _, forbidden := range []string{"tsv", "--user-words", "--user-patterns", "osd", "-psm"} {
		for _, argument := range arguments {
			if argument == forbidden {
				t.Fatalf("OCR loaded implicit configuration %q", forbidden)
			}
		}
	}
	if !strings.Contains(strings.Join(arguments, " "), "-l chi_sim+eng") {
		t.Fatal("OCR dropped a selected model")
	}
}

func TestSubtitleOCRCancellationPrecedesResourceAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := RecognizeBitmapSubtitles(ctx, SubtitleOCRConfig{}, nil, Stream{}, Info{}, []string{"eng"}); !errors.Is(err, context.Canceled) || len(result.Cues) != 0 {
		t.Fatalf("canceled recognition admitted work: %+v, %v", result, err)
	}
}

func TestSubtitleOCRScratchBudgetDefaultsAndHardCeiling(t *testing.T) {
	for _, test := range []struct {
		configured int64
		want       int64
	}{
		{0, subtitleOCRModelsMaxBytes},
		{1, 1},
		{64 << 20, 64 << 20},
		{subtitleOCRModelsMaxBytes, subtitleOCRModelsMaxBytes},
		{subtitleOCRModelsMaxBytes + 1, subtitleOCRModelsMaxBytes},
		{1<<63 - 1, subtitleOCRModelsMaxBytes},
	} {
		directory, limit, err := subtitleOCRScratchSettings(SubtitleOCRConfig{MaxScratchBytes: test.configured})
		if err != nil || directory != "" || limit != test.want {
			t.Fatalf("scratch reservation %d became directory=%q limit=%d error=%v", test.configured, directory, limit, err)
		}
	}
	if _, _, err := subtitleOCRScratchSettings(SubtitleOCRConfig{MaxScratchBytes: -1}); !errors.Is(err, ErrSubtitleOCR) {
		t.Fatalf("negative scratch reservation was accepted: %v", err)
	}
	for _, directory := range []string{"relative", "../outside", "\x00"} {
		if _, _, err := subtitleOCRScratchSettings(SubtitleOCRConfig{ScratchDirectory: directory}); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("invalid scratch path %q was accepted: %v", directory, err)
		}
	}
}

func TestSubtitleOCRScratchBudgetAdmitsTheWholeModelSet(t *testing.T) {
	used, err := reserveSubtitleOCRModelBytes(0, 7, 20)
	if err != nil || used != 7 {
		t.Fatalf("first model reservation failed: %d, %v", used, err)
	}
	used, err = reserveSubtitleOCRModelBytes(used, 13, 20)
	if err != nil || used != 20 {
		t.Fatalf("exact combined reservation failed: %d, %v", used, err)
	}
	for _, values := range [][3]int64{
		{7, 14, 20},
		{20, 1, 20},
		{0, 21, 20},
		{-1, 1, 20},
		{0, 0, 20},
		{0, -1, 20},
		{0, 1, 0},
		{0, 1, -1},
		{0, 1, subtitleOCRModelsMaxBytes + 1},
		{0, subtitleOCRModelMaxBytes + 1, subtitleOCRModelsMaxBytes},
		{1<<63 - 1, 1, subtitleOCRModelsMaxBytes},
	} {
		if _, err := reserveSubtitleOCRModelBytes(values[0], values[1], values[2]); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("invalid combined reservation %v was accepted: %v", values, err)
		}
	}
	if total, err := reserveSubtitleOCRModelBytes(subtitleOCRModelMaxBytes, subtitleOCRModelMaxBytes, subtitleOCRModelsMaxBytes); err != nil || total != subtitleOCRModelsMaxBytes {
		t.Fatalf("supported maximum model set was rejected: %d, %v", total, err)
	}
}

func TestSubtitleOCRScratchRequiresAnExistingPrivateOwnedDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("scratch ownership admission requires Linux")
	}
	parent := t.TempDir()
	directory := filepath.Join(parent, "job")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	selected, limit, err := subtitleOCRScratchSettings(SubtitleOCRConfig{ScratchDirectory: directory, MaxScratchBytes: 4096})
	if err != nil || selected != directory || limit != 4096 {
		t.Fatalf("private job scratch was rejected: %q, %d, %v", selected, limit, err)
	}
	for _, mode := range []os.FileMode{0755, 0750, 0710, 0704, 0702, 0701, 0600, 0700 | os.ModeSticky} {
		if err := os.Chmod(directory, mode); err != nil {
			t.Fatal(err)
		}
		if _, _, err := subtitleOCRScratchSettings(SubtitleOCRConfig{ScratchDirectory: directory}); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("scratch permissions %#o were accepted: %v", mode, err)
		}
	}
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "symlink")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, []byte("unrelated data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, link + string(os.PathSeparator), file, filepath.Join(parent, "missing")} {
		if _, _, err := subtitleOCRScratchSettings(SubtitleOCRConfig{ScratchDirectory: path}); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("scratch path type %q was accepted: %v", path, err)
		}
	}
	if entries, err := os.ReadDir(directory); err != nil || len(entries) != 0 {
		t.Fatalf("scratch admission created files before model reservation: %v", err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(directory, 65534, -1); err != nil {
			t.Fatal(err)
		}
		defer os.Chown(directory, 0, -1)
		if _, _, err := subtitleOCRScratchSettings(SubtitleOCRConfig{ScratchDirectory: directory}); !errors.Is(err, ErrSubtitleOCR) {
			t.Fatalf("scratch owned by another user was accepted: %v", err)
		}
	}
}

func TestSubtitleOCRStreamingConsumerKeepsEvidenceAfterRasterReuse(t *testing.T) {
	raster := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	accumulator := subtitleOCRAccumulator{durationTicks: 30}
	called := 0
	recognize := func(prepared *image.Gray) (string, float64, bool, error) {
		called++
		if prepared.GrayAt(12, 12).Y != 0 || prepared.GrayAt(0, 0).Y != 255 {
			t.Fatal("streamed glyph preprocessing changed")
		}
		return fmt.Sprintf("caption %d", called), 95, false, nil
	}
	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	black := color.NRGBA{A: 255}
	for index, ink := range []color.NRGBA{white, black} {
		raster.SetNRGBA(2, 2, ink)
		cue := BitmapSubtitleCue{StartTicks: int64(index * 10), EndTicks: int64(index*10 + 9),
			Image: raster, X: 40, Y: 60, Forced: true, HearingImpaired: true}
		if err := accumulator.emit(context.Background(), cue, recognize); err != nil {
			t.Fatal(err)
		}
	}
	// A streaming decoder may overwrite the same storage after every callback.
	clear(raster.Pix)
	if called != 2 || accumulator.recognized != 2 || len(accumulator.result.Cues) != 2 {
		t.Fatalf("streamed recognition was not completed synchronously: calls=%d cues=%d", called, len(accumulator.result.Cues))
	}
	for index, ink := range []color.NRGBA{white, black} {
		cue := accumulator.result.Cues[index]
		decoded, err := png.Decode(bytes.NewReader(cue.ImagePNG))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(cue.ImagePNG)
		if color.NRGBAModel.Convert(decoded.At(2, 2)) != ink || cue.ImageSHA256 != hex.EncodeToString(digest[:]) ||
			cue.Text != fmt.Sprintf("caption %d", index+1) || cue.StartTicks != int64(index*10) || cue.EndTicks != int64(index*10+9) ||
			!cue.Forced || !cue.HearingImpaired || cue.X != 40 || cue.Y != 60 || cue.Width != 5 || cue.Height != 5 {
			t.Fatalf("retained evidence borrowed overwritten raster storage or lost cue metadata: cue %d", index)
		}
	}
}

func TestSubtitleOCRStreamingAdmissionStopsBeforeRecognition(t *testing.T) {
	for _, mode := range []string{"cue count", "image bytes", "cancellation", "image identity"} {
		t.Run(mode, func(t *testing.T) {
			accumulator := subtitleOCRAccumulator{durationTicks: 20}
			cue := BitmapSubtitleCue{StartTicks: 1, EndTicks: 10, Image: image.NewNRGBA(image.Rect(0, 0, 5, 5))}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "cue count":
				accumulator.result.Cues = make([]SubtitleOCRCue, MaxSubtitleOCRCues)
			case "image bytes":
				accumulator.totalImages = MaxSubtitleOCRImageBytes
			case "cancellation":
				cancel()
			case "image identity":
				cue.ImageSHA256 = strings.Repeat("0", 64)
			}
			before := len(accumulator.result.Cues)
			called := false
			err := accumulator.emit(ctx, cue, func(*image.Gray) (string, float64, bool, error) {
				called = true
				return "unexpected recognition", 95, false, nil
			})
			if err == nil || called || len(accumulator.result.Cues) != before || accumulator.recognized != 0 {
				t.Fatalf("rejected streamed cue reached OCR or published output: %v", err)
			}
		})
	}
}

func TestSubtitleOCRStreamingFailureDoesNotPublishAnIncompleteCue(t *testing.T) {
	for _, mode := range []string{"text budget", "engine failure", "cancellation after recognition"} {
		t.Run(mode, func(t *testing.T) {
			accumulator := subtitleOCRAccumulator{durationTicks: 20}
			if mode == "text budget" {
				accumulator.totalText = MaxSubtitleOCRTextBytes - 3
			}
			beforeText := accumulator.totalText
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := accumulator.emit(ctx, BitmapSubtitleCue{StartTicks: 1, EndTicks: 10, Image: image.NewNRGBA(image.Rect(0, 0, 5, 5))},
				func(*image.Gray) (string, float64, bool, error) {
					if mode == "engine failure" {
						return "", 0, false, errors.New("recognizer failed")
					}
					if mode == "cancellation after recognition" {
						cancel()
					}
					return "four", 95, false, nil
				})
			if err == nil || len(accumulator.result.Cues) != 0 || accumulator.recognized != 0 || accumulator.totalImages != 0 ||
				accumulator.totalText != beforeText || len(accumulator.result.Warnings) != 0 {
				t.Fatalf("failed streamed cue changed the review candidate: %v", err)
			}
		})
	}
	accumulator := subtitleOCRAccumulator{durationTicks: 20}
	err := accumulator.emit(context.Background(), BitmapSubtitleCue{StartTicks: 1, EndTicks: 10, Image: image.NewNRGBA(image.Rect(0, 0, 5, 5))},
		func(*image.Gray) (string, float64, bool, error) { return "", 85, false, nil })
	if err != nil || accumulator.recognized != 0 || len(accumulator.result.Cues) != 1 || accumulator.result.Cues[0].Confidence != 0 ||
		!reflect.DeepEqual(accumulator.result.Warnings, []string{"cue_1_empty_text"}) {
		t.Fatalf("empty OCR lost its explicit review evidence: %v", err)
	}
}
