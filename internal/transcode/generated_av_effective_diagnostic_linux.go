//go:build linux

package transcode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/media"
)

type generatedAVDiagnosticBuffer struct {
	buffer       bytes.Buffer
	limit        int
	cancel       context.CancelFunc
	err          error
	failNonempty bool
}

// Keep the backing buffer private: promoting bytes.Buffer.ReadFrom would let
// os/exec's io.Copy bypass the budget/cancellation checks in Write.
func (buffer *generatedAVDiagnosticBuffer) Len() int      { return buffer.buffer.Len() }
func (buffer *generatedAVDiagnosticBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

func generatedAVDiagnosticOuterFence(files []*os.File) (func() error, error) {
	if len(files) < 1 || len(files) > 8 {
		return nil, ErrInvalidOptions
	}
	var before []os.FileInfo
	var identities []string
	var offsets []int64
	for _, file := range files {
		if file == nil {
			return nil, ErrInvalidInput
		}
		info, err := file.Stat()
		if err != nil {
			return nil, ErrInvalidInput
		}
		identity, err := media.VideoSeekSourceIdentity(info)
		if err != nil {
			return nil, ErrInvalidInput
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, ErrInvalidInput
		}
		before = append(before, info)
		identities = append(identities, identity)
		offsets = append(offsets, offset)
	}
	return func() error {
		for index, file := range files {
			info, err := file.Stat()
			offset, offsetErr := file.Seek(0, io.SeekCurrent)
			if err != nil || offsetErr != nil || offset != offsets[index] || !transcodeSourceUnchanged(file, before[index]) {
				return ErrInvalidInput
			}
			identity, err := media.VideoSeekSourceIdentity(info)
			if err != nil || identity != identities[index] {
				return ErrInvalidInput
			}
		}
		return nil
	}, nil
}

func (buffer *generatedAVDiagnosticBuffer) Write(data []byte) (int, error) {
	if buffer.err != nil {
		return 0, buffer.err
	}
	if len(data) > buffer.limit-buffer.Len() {
		buffer.err = ErrTimelineLimit
	} else if buffer.failNonempty && len(data) > 0 {
		buffer.err = ErrTimelineProbe
	}
	if buffer.err != nil {
		buffer.cancel()
		return 0, buffer.err
	}
	return buffer.buffer.Write(data)
}

// generatedAVDiagnosticDecode borrows all parts, hashes every copied input
// byte, and observes real copier EOF. RunProcessWithRetirement owns Start/Wait;
// its WNOWAIT callback joins group retirement before Wait joins exec copiers.
// Parsing and final borrowed-file fences follow that joined process result.
func generatedAVDiagnosticDecode(ctx context.Context, executable string, files []*os.File, args []string, stdout io.Writer) (int64, [][32]byte, error) {
	if ctx == nil || len(files) < 1 || len(files) > 8 {
		return 0, nil, ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	if executable == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return 0, nil, ErrStart
	}
	resolved, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(resolved) {
		return 0, nil, ErrStart
	}
	processCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	input := &generatedBoundsInput{ctx: processCtx}
	var owned []*os.File
	defer func() {
		for _, file := range owned {
			_ = file.Close()
		}
	}()
	var before []os.FileInfo
	var identities []string
	var offsets []int64
	var total int64
	for _, file := range files {
		if file == nil {
			return 0, nil, ErrInvalidInput
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 1 {
			return 0, nil, ErrInvalidInput
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return 0, nil, ErrInvalidInput
		}
		for _, prior := range before {
			if os.SameFile(info, prior) {
				return 0, nil, ErrInvalidInput
			}
		}
		identity, err := media.VideoSeekSourceIdentity(info)
		if err != nil {
			return 0, nil, ErrInvalidInput
		}
		offset, err := file.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, nil, ErrInvalidInput
		}
		if info.Size() > generatedAVTransportBytes-total {
			return 0, nil, ErrTimelineLimit
		}
		total += info.Size()
		duplicate, err := DuplicateInput(file)
		if err != nil {
			return 0, nil, err
		}
		owned = append(owned, duplicate)
		before = append(before, info)
		identities = append(identities, identity)
		offsets = append(offsets, offset)
		input.parts = append(input.parts, generatedBoundsInputPart{reader: io.NewSectionReader(duplicate, 0, info.Size()), sum: sha256.New()})
	}
	command := exec.CommandContext(processCtx, resolved, args...)
	command.Env, command.Dir = processEnvironment(), "/"
	command.Stdin, command.Stdout = input, stdout
	stderr := &generatedAVDiagnosticBuffer{limit: 64 << 10, cancel: cancel, failNonempty: true}
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	runErr := media.RunProcessWithRetirement(processCtx, command, func() error {
		waitErr := waitWithoutReaping(command.Process.Pid)
		groupMu.Lock()
		defer groupMu.Unlock()
		retired = true
		if waitErr != nil {
			return waitErr
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	})
	if err := errors.Join(runErr, stderr.err, processCtx.Err(), ctx.Err()); err != nil {
		return 0, nil, err
	}
	if !input.eof || input.bytes != total {
		return 0, nil, generatedAVTransportInvalid("decoder input copier did not reach complete EOF")
	}
	for index, file := range files {
		info, err := file.Stat()
		offset, offsetErr := file.Seek(0, io.SeekCurrent)
		if err != nil || offsetErr != nil || offset != offsets[index] || !transcodeSourceUnchanged(file, before[index]) {
			return 0, nil, ErrInvalidInput
		}
		identity, err := media.VideoSeekSourceIdentity(info)
		if err != nil || identity != identities[index] {
			return 0, nil, ErrInvalidInput
		}
	}
	var hashes [][32]byte
	for _, part := range input.parts {
		var hash [32]byte
		copy(hash[:], part.sum.Sum(nil))
		hashes = append(hashes, hash)
	}
	return total, hashes, nil
}

// MeasureGeneratedAVEffectiveDecodeDiagnostic observes all returned frames of
// both tracks from the complete held extent. No -t, frame limit or read interval
// is used. Normal decoder completion is required, but does not itself certify
// source payload completeness, applied metadata edits or source terminal EOF.
func MeasureGeneratedAVEffectiveDecodeDiagnostic(ctx context.Context, ffprobe string, files []*os.File) (GeneratedAVEffectiveDecodeDiagnostic, error) {
	var empty GeneratedAVEffectiveDecodeDiagnostic
	projection, err := measureGeneratedAVJoinedFrameProjection(ctx, ffprobe, files)
	if err != nil {
		return empty, err
	}
	result, err := ParseGeneratedAVEffectiveDecodeDiagnostic(ctx, projection.data)
	if err != nil {
		return empty, err
	}
	if err := projection.fence(); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.InputBytes, result.InputSHA256, result.Complete = projection.inputBytes, projection.hashes, true
	return result, nil
}

// MeasureGeneratedAVPCMDiagnostic streams complete actual s16le output without
// resampling, remixing or nominal clipping. channels must come from independent
// native frame/stream observation; it is not supplied as an output -ac option.
func MeasureGeneratedAVPCMDiagnostic(ctx context.Context, ffmpeg string, files []*os.File, channels int, maxBytes int64) (GeneratedAVPCMDiagnostic, error) {
	var empty GeneratedAVPCMDiagnostic
	if ctx == nil {
		return empty, ErrInvalidOptions
	}
	fence, err := generatedAVDiagnosticOuterFence(files)
	if err != nil {
		return empty, err
	}
	writer, err := newGeneratedAVPCMWriter(channels, maxBytes)
	if err != nil {
		return empty, err
	}
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	writer.cancel = cancel
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts", "-err_detect", "crccheck+bitstream+buffer+explode",
		"-max_alloc", strconv.Itoa(media.MaxVideoSeekAllocationBytes), "-max_pixels", strconv.FormatInt(media.MaxVideoSeekPixels, 10),
		"-protocol_whitelist", "pipe", "-format_whitelist", inputFormats, "-i", "pipe:0", "-map", "0:a:0", "-vn", "-sn", "-dn", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1"}
	_, _, err = generatedAVDiagnosticDecode(processCtx, ffmpeg, files, args, writer)
	if err := errors.Join(err, writer.err); err != nil {
		return empty, err
	}
	result, err := writer.finish()
	if err != nil {
		return empty, err
	}
	if err := fence(); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.Complete = true
	return result, nil
}
