package media

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	subtitleOCRDeadline       = 15 * time.Minute
	subtitleOCRCueDeadline    = 20 * time.Second
	subtitleOCREngineMaxBytes = 128 << 20
	subtitleOCRModelMaxBytes  = 256 << 20
	subtitleOCRModelsMaxBytes = 512 << 20
	subtitleOCRMaxTSVBytes    = 1 << 20
	subtitleOCRImageBorder    = 10
	subtitleOCRTSVHeader      = "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext"
)

var subtitleOCRSlots = make(chan struct{}, 2)

// RecognizeBitmapSubtitles decodes an indexed bitmap stream and returns a review
// candidate. It never publishes a track or treats low-confidence text as final.
// The caller retains ownership of input and its current file position.
func RecognizeBitmapSubtitles(ctx context.Context, config SubtitleOCRConfig, input *os.File, stream Stream, source Info, modelIDs []string) (SubtitleOCRResult, error) {
	ctx, cancel := context.WithTimeout(ctx, subtitleOCRDeadline)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return SubtitleOCRResult{}, err
	}
	if runtime.GOOS != "linux" {
		return SubtitleOCRResult{}, fmt.Errorf("%w: Linux is required", ErrSubtitleOCR)
	}
	models, err := selectSubtitleOCRModels(config.Models, modelIDs)
	if err != nil {
		return SubtitleOCRResult{}, err
	}
	scratchDirectory, scratchLimit, err := subtitleOCRScratchSettings(config)
	if err != nil {
		return SubtitleOCRResult{}, err
	}
	if input == nil {
		return SubtitleOCRResult{}, fmt.Errorf("%w: source descriptor is missing", ErrSubtitleOCR)
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || source.Size != before.Size() ||
		source.FileChangeTimeNs != FileChangeTime(before) {
		return SubtitleOCRResult{}, fmt.Errorf("%w: source identity does not match", ErrSubtitleOCR)
	}
	select {
	case subtitleOCRSlots <- struct{}{}:
		defer func() { <-subtitleOCRSlots }()
	case <-ctx.Done():
		return SubtitleOCRResult{}, ctx.Err()
	}
	engine, engineBefore, err := openPinnedSubtitleFile(ctx, config.TesseractPath, config.TesseractSHA256, subtitleOCREngineMaxBytes)
	if err != nil {
		if ctx.Err() != nil {
			return SubtitleOCRResult{}, ctx.Err()
		}
		return SubtitleOCRResult{}, fmt.Errorf("%w: engine identity could not be verified", ErrSubtitleOCR)
	}
	defer engine.Close()
	if err := validateSubtitleOCREngine(engine, engineBefore); err != nil {
		return SubtitleOCRResult{}, err
	}
	var directory string
	var pinnedModels []subtitleOCRPinnedModel
	defer func() {
		for _, model := range pinnedModels {
			model.original.Close()
			if model.copy != nil {
				model.copy.Close()
			}
		}
		if directory != "" {
			_ = os.RemoveAll(directory)
		}
	}()
	totalModelBytes := int64(0)
	for _, model := range models {
		file, before, err := openPinnedSubtitleFile(ctx, model.Path, model.SHA256, subtitleOCRModelMaxBytes)
		if err != nil {
			if ctx.Err() != nil {
				return SubtitleOCRResult{}, ctx.Err()
			}
			return SubtitleOCRResult{}, fmt.Errorf("%w: model identity could not be verified", ErrSubtitleOCR)
		}
		pinnedModels = append(pinnedModels, subtitleOCRPinnedModel{model: model, original: file, originalBefore: before})
		totalModelBytes, err = reserveSubtitleOCRModelBytes(totalModelBytes, before.Size(), scratchLimit)
		if err != nil {
			return SubtitleOCRResult{}, err
		}
	}
	// Admit every selected model before creating any scratch file. A small job
	// reservation must not leave a partially copied model set on disk.
	directory, err = os.MkdirTemp(scratchDirectory, "goby-subtitle-ocr-")
	if err != nil {
		return SubtitleOCRResult{}, fmt.Errorf("%w: private model directory unavailable", ErrSubtitleOCR)
	}
	for index := range pinnedModels {
		model := &pinnedModels[index]
		copyFile, copyBefore, err := copySubtitleOCRModel(ctx, model.original, model.originalBefore, directory, model.model)
		if err != nil {
			return SubtitleOCRResult{}, err
		}
		model.copy, model.copyBefore = copyFile, copyBefore
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return SubtitleOCRResult{}, fmt.Errorf("%w: private model descriptor unavailable", ErrSubtitleOCR)
	}
	defer directoryFile.Close()
	modelID, modelDigest, identities := subtitleOCRModelIdentity(models)
	accumulator := subtitleOCRAccumulator{durationTicks: source.DurationTicks, result: SubtitleOCRResult{
		EngineSHA256: config.TesseractSHA256, ModelID: modelID, ModelSHA256: modelDigest, Models: identities,
	}}
	// The walker calls emit synchronously. Each invocation finishes recognition
	// before the decoder advances; only compressed evidence survives its return.
	warnings, err := walkBitmapSubtitles(ctx, BitmapSubtitleConfig{
		FFprobePath: config.FFprobePath, FFprobeSHA256: config.FFprobeSHA256,
	}, input, stream, source, func(cue BitmapSubtitleCue) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !subtitleFileUnchanged(input, before) || !subtitleFileUnchanged(engine, engineBefore) {
			return fmt.Errorf("%w: source, engine, or model changed", ErrSubtitleOCR)
		}
		for _, model := range pinnedModels {
			if !subtitleFileUnchanged(model.original, model.originalBefore) || !subtitleFileUnchanged(model.copy, model.copyBefore) {
				return fmt.Errorf("%w: model changed", ErrSubtitleOCR)
			}
		}
		return accumulator.emit(ctx, cue, func(prepared *image.Gray) (string, float64, bool, error) {
			preparedPNG, err := encodeSubtitleOCRPNG(prepared, MaxSubtitleOCRCueImageBytes)
			if err != nil {
				return "", 0, false, err
			}
			output, diagnostics, err := runSubtitleOCR(ctx, engine, directoryFile, modelID, preparedPNG)
			if err != nil {
				return "", 0, false, err
			}
			text, confidence, err := parseSubtitleOCRTSV(output, prepared.Bounds().Dx(), prepared.Bounds().Dy(), modelID)
			return text, confidence, diagnostics, err
		})
	})
	if err != nil {
		return SubtitleOCRResult{}, err
	}
	if len(accumulator.result.Cues) == 0 {
		return SubtitleOCRResult{}, fmt.Errorf("%w: no bitmap cues were decoded", ErrSubtitleOCR)
	}
	accumulator.result.Warnings = append(append([]string(nil), warnings...), accumulator.result.Warnings...)
	if !subtitleFileUnchanged(input, before) {
		return SubtitleOCRResult{}, fmt.Errorf("%w: source changed during recognition", ErrSubtitleOCR)
	}
	if err := verifySubtitleOCRFile(ctx, engine, engineBefore, config.TesseractSHA256); err != nil {
		return SubtitleOCRResult{}, err
	}
	for _, model := range pinnedModels {
		if err := verifySubtitleOCRFile(ctx, model.original, model.originalBefore, model.model.SHA256); err != nil {
			return SubtitleOCRResult{}, err
		}
		if err := verifySubtitleOCRFile(ctx, model.copy, model.copyBefore, model.model.SHA256); err != nil {
			return SubtitleOCRResult{}, err
		}
	}
	if !subtitleFileUnchanged(input, before) {
		return SubtitleOCRResult{}, fmt.Errorf("%w: source changed during final identity checks", ErrSubtitleOCR)
	}
	if err := ctx.Err(); err != nil {
		return SubtitleOCRResult{}, err
	}
	if accumulator.recognized == 0 {
		return SubtitleOCRResult{}, fmt.Errorf("%w: no text was recognized in this track", ErrSubtitleOCR)
	}
	return accumulator.result, nil
}

type subtitleOCRAccumulator struct {
	result                 SubtitleOCRResult
	durationTicks          int64
	totalImages, totalText int
	recognized             int
}

// emit consumes the borrowed raster before returning. No raster or recognition
// closure is retained, so the decoder may reuse its image storage immediately.
func (a *subtitleOCRAccumulator) emit(ctx context.Context, cue BitmapSubtitleCue, recognize func(*image.Gray) (string, float64, bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	index := len(a.result.Cues)
	if index >= MaxSubtitleOCRCues {
		return fmt.Errorf("%w: decoded cue count exceeds the supported range", ErrSubtitleOCR)
	}
	if cue.StartTicks < 0 || cue.EndTicks <= cue.StartTicks || cue.EndTicks > a.durationTicks || cue.X < 0 || cue.Y < 0 {
		return fmt.Errorf("%w: invalid decoded cue interval", ErrSubtitleOCR)
	}
	prepared, err := prepareSubtitleOCRImage(cue.Image)
	if err != nil {
		return err
	}
	originalPNG, err := encodeSubtitleOCRPNG(cue.Image, min(MaxSubtitleOCRCueImageBytes, MaxSubtitleOCRImageBytes-a.totalImages))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(originalPNG)
	originalHash := hex.EncodeToString(digest[:])
	if cue.ImageSHA256 != "" && cue.ImageSHA256 != originalHash {
		return fmt.Errorf("%w: decoded image identity does not match", ErrSubtitleOCR)
	}
	text, confidence, diagnostics, err := recognize(prepared)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(text) > MaxSubtitleOCRCueTextBytes || len(text) > MaxSubtitleOCRTextBytes-a.totalText {
		return fmt.Errorf("%w: recognized text budget exceeded", ErrSubtitleOCR)
	}
	if math.IsNaN(confidence) || math.IsInf(confidence, 0) || confidence < 0 || confidence > 100 {
		return fmt.Errorf("%w: invalid recognition confidence", ErrSubtitleOCR)
	}
	if text == "" {
		confidence = 0
		a.result.Warnings = append(a.result.Warnings, fmt.Sprintf("cue_%d_empty_text", index+1))
	} else {
		a.recognized++
		if confidence < 60 {
			a.result.Warnings = append(a.result.Warnings, fmt.Sprintf("cue_%d_low_confidence", index+1))
		}
	}
	if diagnostics {
		a.result.Warnings = append(a.result.Warnings, fmt.Sprintf("cue_%d_engine_diagnostics", index+1))
	}
	a.totalImages += len(originalPNG)
	a.totalText += len(text)
	a.result.Cues = append(a.result.Cues, SubtitleOCRCue{StartTicks: cue.StartTicks, EndTicks: cue.EndTicks,
		Text: text, Confidence: confidence, ImagePNG: originalPNG, ImageSHA256: originalHash,
		Forced: cue.Forced, HearingImpaired: cue.HearingImpaired, X: cue.X, Y: cue.Y,
		Width: cue.Image.Bounds().Dx(), Height: cue.Image.Bounds().Dy()})
	return nil
}

type subtitleOCRPinnedModel struct {
	model                      SubtitleOCRModel
	original, copy             *os.File
	originalBefore, copyBefore os.FileInfo
}

func subtitleOCRScratchSettings(config SubtitleOCRConfig) (string, int64, error) {
	if config.MaxScratchBytes < 0 {
		return "", 0, fmt.Errorf("%w: negative scratch budget", ErrSubtitleOCR)
	}
	limit := int64(subtitleOCRModelsMaxBytes)
	if config.MaxScratchBytes != 0 {
		limit = min(limit, config.MaxScratchBytes)
	}
	directory := config.ScratchDirectory
	if directory == "" {
		return "", limit, nil
	}
	if !filepath.IsAbs(directory) || strings.ContainsAny(directory, "\x00\r\n") {
		return "", 0, fmt.Errorf("%w: invalid scratch directory", ErrSubtitleOCR)
	}
	directory = filepath.Clean(directory)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 ||
		info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || !subtitleOCRDirectoryOwned(info) {
		return "", 0, fmt.Errorf("%w: scratch directory must be private and owned by the service", ErrSubtitleOCR)
	}
	return directory, limit, nil
}

func reserveSubtitleOCRModelBytes(current, size, limit int64) (int64, error) {
	if current < 0 || size <= 0 || size > subtitleOCRModelMaxBytes || limit <= 0 || limit > subtitleOCRModelsMaxBytes ||
		current > limit || size > limit-current {
		return 0, fmt.Errorf("%w: combined model size exceeds the scratch budget", ErrSubtitleOCR)
	}
	return current + size, nil
}

func selectSubtitleOCRModels(models []SubtitleOCRModel, ids []string) ([]SubtitleOCRModel, error) {
	if len(ids) == 0 || len(ids) > 3 || len(models) == 0 || len(models) > 3 {
		return nil, fmt.Errorf("%w: unsupported or unavailable language model", ErrSubtitleOCR)
	}
	seen := make(map[string]bool, len(models))
	available := make(map[string]SubtitleOCRModel, len(models))
	for _, model := range models {
		if !supportedSubtitleOCRModel(model.ID) || seen[model.ID] {
			return nil, fmt.Errorf("%w: invalid language model configuration", ErrSubtitleOCR)
		}
		seen[model.ID] = true
		available[model.ID] = model
	}
	selected := make([]SubtitleOCRModel, 0, len(ids))
	clear(seen)
	for _, id := range ids {
		model, exists := available[id]
		if !supportedSubtitleOCRModel(id) || !exists || seen[id] {
			return nil, fmt.Errorf("%w: unsupported, duplicate, or unavailable language model", ErrSubtitleOCR)
		}
		seen[id] = true
		selected = append(selected, model)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	return selected, nil
}

func subtitleOCRModelIdentity(models []SubtitleOCRModel) (string, string, []SubtitleOCRModelIdentity) {
	ids := make([]string, 0, len(models))
	identities := make([]SubtitleOCRModelIdentity, 0, len(models))
	var manifest strings.Builder
	for _, model := range models {
		ids = append(ids, model.ID)
		identities = append(identities, SubtitleOCRModelIdentity{ID: model.ID, SHA256: model.SHA256})
		fmt.Fprintf(&manifest, "%s:%s\n", model.ID, model.SHA256)
	}
	if len(models) == 1 {
		return models[0].ID, models[0].SHA256, identities
	}
	digest := sha256.Sum256([]byte(manifest.String()))
	return strings.Join(ids, "+"), hex.EncodeToString(digest[:]), identities
}

func supportedSubtitleOCRModel(id string) bool {
	return id == "eng" || id == "chi_sim" || id == "chi_tra"
}

func validSubtitleOCRModelKey(key string) bool {
	ids := strings.Split(key, "+")
	if len(ids) == 0 || len(ids) > 3 {
		return false
	}
	for index, id := range ids {
		if !supportedSubtitleOCRModel(id) || (index > 0 && ids[index-1] >= id) {
			return false
		}
	}
	return true
}

func validateSubtitleOCREngine(file *os.File, before os.FileInfo) error {
	if before.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("%w: engine is not executable", ErrSubtitleOCR)
	}
	binary, err := elf.NewFile(file)
	if err != nil {
		return fmt.Errorf("%w: engine must be a native ELF executable", ErrSubtitleOCR)
	}
	defer binary.Close()
	if binary.Type != elf.ET_EXEC && binary.Type != elf.ET_DYN {
		return fmt.Errorf("%w: engine must be a native ELF executable", ErrSubtitleOCR)
	}
	native := (runtime.GOARCH == "amd64" && binary.Machine == elf.EM_X86_64 && binary.Class == elf.ELFCLASS64) ||
		(runtime.GOARCH == "arm64" && binary.Machine == elf.EM_AARCH64 && binary.Class == elf.ELFCLASS64)
	if !native || binary.Data != elf.ELFDATA2LSB {
		return fmt.Errorf("%w: engine architecture does not match the host", ErrSubtitleOCR)
	}
	return nil
}

func copySubtitleOCRModel(ctx context.Context, source *os.File, before os.FileInfo, directory string, model SubtitleOCRModel) (*os.File, os.FileInfo, error) {
	file, err := os.OpenFile(filepath.Join(directory, model.ID+".traineddata"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: private model copy unavailable", ErrSubtitleOCR)
	}
	fail := func(err error) (*os.File, os.FileInfo, error) { file.Close(); return nil, nil, err }
	hash := sha256.New()
	reader := io.NewSectionReader(source, 0, before.Size())
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if _, err := file.Write(buffer[:n]); err != nil {
				return fail(fmt.Errorf("%w: private model copy failed", ErrSubtitleOCR))
			}
			_, _ = hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fail(fmt.Errorf("%w: model copy read failed", ErrSubtitleOCR))
		}
	}
	if !subtitleFileUnchanged(source, before) || hex.EncodeToString(hash.Sum(nil)) != model.SHA256 {
		return fail(fmt.Errorf("%w: model changed while copying", ErrSubtitleOCR))
	}
	if err := file.Chmod(0400); err != nil {
		return fail(fmt.Errorf("%w: private model permissions unavailable", ErrSubtitleOCR))
	}
	copied, err := file.Stat()
	if err != nil || !copied.Mode().IsRegular() || copied.Size() != before.Size() {
		return fail(fmt.Errorf("%w: private model copy is incomplete", ErrSubtitleOCR))
	}
	return file, copied, nil
}

func verifySubtitleOCRFile(ctx context.Context, file *os.File, before os.FileInfo, digest string) error {
	if !subtitleFileUnchanged(file, before) {
		return fmt.Errorf("%w: pinned file changed", ErrSubtitleOCR)
	}
	hash := sha256.New()
	reader := io.NewSectionReader(file, 0, before.Size())
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := reader.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: pinned file could not be revalidated", ErrSubtitleOCR)
		}
	}
	if !subtitleFileUnchanged(file, before) || hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("%w: pinned file identity changed", ErrSubtitleOCR)
	}
	return nil
}

// Fixed options avoid loading a named config file, user dictionaries, OSD, or
// any unselected model. See Tesseract's Command-Line-Usage reference.
func subtitleOCRArguments(modelID string) []string {
	return []string{"stdin", "stdout", "--tessdata-dir", "/proc/self/fd/4", "-l", modelID,
		"--oem", "1", "--psm", "6", "--dpi", "300", "-c", "tessedit_create_tsv=1", "-c", "tessedit_create_txt=0"}
}

func subtitleOCREnvironment() []string {
	// Do not inherit loader, TESSDATA_PREFIX, user configuration, or report paths.
	return []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "HOME=/proc/self/fd/4",
		"TMPDIR=/proc/self/fd/4", "XDG_CONFIG_HOME=/proc/self/fd/4", "OMP_THREAD_LIMIT=1", "OMP_NUM_THREADS=1", "OMP_DYNAMIC=FALSE"}
}

func runSubtitleOCR(ctx context.Context, engine, directory *os.File, modelID string, pngBytes []byte) ([]byte, bool, error) {
	if !validSubtitleOCRModelKey(modelID) || engine == nil || directory == nil || len(pngBytes) == 0 || len(pngBytes) > MaxSubtitleOCRImageBytes {
		return nil, false, fmt.Errorf("%w: invalid engine input", ErrSubtitleOCR)
	}
	ctx, cancel := context.WithTimeout(ctx, subtitleOCRCueDeadline)
	defer cancel()
	stdout := &limitedOutput{limit: subtitleOCRMaxTSVBytes, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command := exec.CommandContext(ctx, "/proc/self/fd/3", subtitleOCRArguments(modelID)...)
	command.Args[0] = "tesseract"
	command.ExtraFiles = []*os.File{engine, directory}
	command.Env = subtitleOCREnvironment()
	// Chdir precedes ExtraFiles remapping, so use the parent's descriptor here.
	command.Dir = fmt.Sprintf("/proc/self/fd/%d", directory.Fd())
	command.Stdin = bytes.NewReader(pngBytes)
	command.Stdout, command.Stderr, command.WaitDelay = stdout, stderr, time.Second
	retired, err := startMediaProcess(command)
	if err == nil {
		err = errors.Join(<-retired, command.Wait())
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, false, fmt.Errorf("%w: %w", ErrSubtitleOCR, ErrOutputLimit)
	}
	if ctx.Err() != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrSubtitleOCR, ctx.Err())
	}
	if err != nil {
		// Stderr can contain media text or host paths; retain only a fixed error.
		return nil, false, fmt.Errorf("%w: engine execution failed: %w", ErrSubtitleOCR, err)
	}
	return stdout.buffer.Bytes(), stderr.buffer.Len() != 0, nil
}

// prepareSubtitleOCRImage provides dark text on white without modifying the
// evidence. Opaque dark borders identify inverted captions. Transparent-edge
// captions instead use their visible ink luminance to retain white glyphs.
func prepareSubtitleOCRImage(source *image.NRGBA) (*image.Gray, error) {
	if source == nil || source.Bounds().Empty() || source.Bounds().Dx() > 4096 || source.Bounds().Dy() > 2160 ||
		source.Bounds().Dx() > maxBitmapSubtitlePixels/source.Bounds().Dy() {
		return nil, fmt.Errorf("%w: invalid bitmap dimensions", ErrSubtitleOCR)
	}
	bounds := source.Bounds()
	var borderLuma, borderAlpha, visibleLuma, visibleAlpha int64
	borderPixels := int64(0)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := source.NRGBAAt(x, y)
			luma := (299*int64(pixel.R) + 587*int64(pixel.G) + 114*int64(pixel.B) + 500) / 1000
			alpha := int64(pixel.A)
			visibleLuma += luma * alpha
			visibleAlpha += alpha
			if x == bounds.Min.X || x == bounds.Max.X-1 || y == bounds.Min.Y || y == bounds.Max.Y-1 {
				borderLuma += luma * alpha
				borderAlpha += alpha
				borderPixels++
			}
		}
	}
	invert := visibleAlpha > 0 && visibleLuma/visibleAlpha >= 128
	if borderAlpha >= borderPixels*250 {
		invert = borderLuma/borderAlpha < 128
	}
	prepared := image.NewGray(image.Rect(0, 0, bounds.Dx()+2*subtitleOCRImageBorder, bounds.Dy()+2*subtitleOCRImageBorder))
	for index := range prepared.Pix {
		prepared.Pix[index] = 255
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := source.NRGBAAt(x, y)
			luma := (299*int(pixel.R) + 587*int(pixel.G) + 114*int(pixel.B) + 500) / 1000
			if invert {
				luma = 255 - luma
			}
			value := (luma*int(pixel.A) + 255*(255-int(pixel.A)) + 127) / 255
			prepared.Pix[(y-bounds.Min.Y+subtitleOCRImageBorder)*prepared.Stride+x-bounds.Min.X+subtitleOCRImageBorder] = uint8(value)
		}
	}
	return prepared, nil
}

type subtitleOCRImageBuffer struct {
	bytes.Buffer
	remaining int
}

func (buffer *subtitleOCRImageBuffer) Write(data []byte) (int, error) {
	if len(data) > buffer.remaining {
		return 0, ErrOutputLimit
	}
	buffer.remaining -= len(data)
	return buffer.Buffer.Write(data)
}

func encodeSubtitleOCRPNG(source image.Image, limit int) ([]byte, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: bitmap evidence budget exceeded", ErrSubtitleOCR)
	}
	buffer := &subtitleOCRImageBuffer{remaining: limit}
	if err := png.Encode(buffer, source); err != nil {
		return nil, fmt.Errorf("%w: bitmap encoding: %w", ErrSubtitleOCR, err)
	}
	return buffer.Bytes(), nil
}

func parseSubtitleOCRTSV(data []byte, width, height int, modelID string) (string, float64, error) {
	invalid := func() (string, float64, error) {
		return "", 0, fmt.Errorf("%w: invalid or excessive recognition output", ErrSubtitleOCR)
	}
	if len(data) == 0 || len(data) > subtitleOCRMaxTSVBytes || !utf8.Valid(data) || !validSubtitleOCRModelKey(modelID) || width <= 0 || height <= 0 {
		return invalid()
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), MaxSubtitleOCRCueTextBytes+2048)
	if !scanner.Scan() || scanner.Text() != subtitleOCRTSVHeader {
		return invalid()
	}
	var text strings.Builder
	var previous [4]int
	var previousWord string
	confidence, words, rows := 0.0, 0, 0
	pageSeen := false
	for scanner.Scan() {
		rows++
		if rows > 32768 {
			return invalid()
		}
		fields := strings.SplitN(scanner.Text(), "\t", 12)
		if len(fields) != 12 {
			return invalid()
		}
		var numbers [10]int
		for index := range numbers {
			parsed, err := strconv.Atoi(fields[index])
			if err != nil || parsed < 0 || parsed > 1<<30 {
				return invalid()
			}
			numbers[index] = parsed
		}
		level := numbers[0]
		if level < 1 || level > 5 || numbers[1] != 1 || numbers[6] > width || numbers[7] > height ||
			numbers[8] > width-numbers[6] || numbers[9] > height-numbers[7] {
			return invalid()
		}
		wordConfidence, err := strconv.ParseFloat(fields[10], 64)
		if err != nil || math.IsNaN(wordConfidence) || math.IsInf(wordConfidence, 0) {
			return invalid()
		}
		if level == 1 {
			if pageSeen || numbers[2] != 0 || numbers[3] != 0 || numbers[4] != 0 || numbers[5] != 0 ||
				numbers[6] != 0 || numbers[7] != 0 || numbers[8] != width || numbers[9] != height {
				return invalid()
			}
			pageSeen = true
		}
		if !pageSeen {
			return invalid()
		}
		if level < 5 {
			if wordConfidence != -1 || fields[11] != "" {
				return invalid()
			}
			continue
		}
		if wordConfidence < 0 || wordConfidence > 100 || numbers[2] < 1 || numbers[3] < 1 || numbers[4] < 1 || numbers[5] < 1 ||
			numbers[8] == 0 || numbers[9] == 0 {
			return invalid()
		}
		word := strings.TrimSpace(fields[11])
		if strings.IndexFunc(fields[11], unicode.IsControl) >= 0 {
			return invalid()
		}
		if word == "" {
			continue
		}
		line := [4]int{numbers[1], numbers[2], numbers[3], numbers[4]}
		separator := ""
		if words > 0 {
			if line != previous {
				separator = "\n"
			} else {
				separator = subtitleOCRWordSeparator(previousWord, word, modelID)
			}
		}
		if text.Len()+len(separator)+len(word) > MaxSubtitleOCRCueTextBytes {
			return invalid()
		}
		text.WriteString(separator)
		text.WriteString(word)
		confidence += wordConfidence
		words++
		previous, previousWord = line, word
	}
	if scanner.Err() != nil || !pageSeen {
		return invalid()
	}
	if words == 0 {
		return "", 0, nil
	}
	return text.String(), confidence / float64(words), nil
}

func subtitleOCRWordSeparator(previous, next, modelID string) string {
	if strings.Contains(modelID, "chi_") {
		last, _ := utf8.DecodeLastRuneInString(previous)
		first, _ := utf8.DecodeRuneInString(next)
		if unicode.Is(unicode.Han, last) || unicode.Is(unicode.Han, first) ||
			(last >= 0x3000 && last <= 0x303f) || (first >= 0x3000 && first <= 0x303f) ||
			(last >= 0xff01 && last <= 0xff60) || (first >= 0xff01 && first <= 0xff60) {
			return ""
		}
	}
	return " "
}
