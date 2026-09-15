package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// A session owns a single resource domain for preparation, stage commands and
// verification together. The enclosing owner supplies administrator and
// conversion-slot admission, result retention, and stage acceptance. A failed
// close must be retained by that owner; it never permits another run.
type diagnosticProcessSession struct {
	mu              sync.Mutex
	ctx             context.Context
	cancel          context.CancelFunc
	group           *diagnosticCgroup
	tool            *os.File
	toolPath        string
	toolStamp       string
	toolHash        string
	environment     []string
	environmentHash string
	directoryFD     *os.File
	commands        int
	pending         <-chan error
	failed          bool
	closed          bool
}

// A non-nil session returned with an error still owns a failed cleanup. Its
// caller must retain it and retry close; it is never eligible for execution.
func newDiagnosticProcessSession(ctx context.Context, options diagnosticProcessOptions) (*diagnosticProcessSession, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// The service must have no capability that can bypass the borrowed scratch
	// directory's permissions. No privilege or mount operation is performed.
	if os.Geteuid() == 0 || !diagnosticUnprivileged() {
		return nil, ErrDiagnosticResources
	}
	environment, err := diagnosticEnvironment(options)
	if err != nil {
		return nil, err
	}
	resolved, err := exec.LookPath(options.FFmpegPath)
	if err != nil {
		return nil, ErrDiagnosticTool
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return nil, ErrDiagnosticTool
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return nil, ErrDiagnosticTool
	}
	tool, err := diagnosticOpenAbsolute(resolved, unix.O_RDONLY)
	if err != nil {
		return nil, ErrDiagnosticTool
	}
	info, err := tool.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || info.Mode().Perm()&0111 == 0 {
		_ = tool.Close()
		return nil, ErrDiagnosticTool
	}
	capabilityBytes, capabilityErr := unix.Fgetxattr(int(tool.Fd()), "security.capability", nil)
	if capabilityBytes != 0 || capabilityErr != nil && !errors.Is(capabilityErr, unix.ENODATA) && !errors.Is(capabilityErr, unix.ENOTSUP) {
		_ = tool.Close()
		return nil, ErrDiagnosticTool
	}
	var magic [4]byte
	if _, err := tool.ReadAt(magic[:], 0); err != nil || !bytes.Equal(magic[:], []byte{0x7f, 'E', 'L', 'F'}) {
		_ = tool.Close()
		return nil, ErrDiagnosticTool
	}
	stamp, err := VideoSeekSourceIdentity(info)
	if err != nil {
		_ = tool.Close()
		return nil, ErrDiagnosticTool
	}
	runCtx, cancel := context.WithTimeout(ctx, diagnosticRunDeadline)
	s := &diagnosticProcessSession{ctx: runCtx, cancel: cancel, tool: tool, toolPath: resolved, toolStamp: stamp}
	fail := func(cause error) (*diagnosticProcessSession, error) {
		s.failed = true
		if err := s.close(); err != nil {
			return s, errors.Join(cause, err)
		}
		return nil, cause
	}
	s.toolHash, err = s.hashTool()
	if err != nil {
		return fail(err)
	}
	s.directoryFD, err = diagnosticOpenAbsolute(options.ScratchDirectory, unix.O_RDONLY|unix.O_DIRECTORY)
	if err == nil {
		err = diagnosticReadOnlyScratch(s.directoryFD)
	}
	if err == nil {
		s.group, err = newDiagnosticCgroup(options.CgroupParent)
	}
	if err != nil {
		return fail(ErrDiagnosticResources)
	}
	for _, key := range []string{"HOME", "TMPDIR", "TMP", "TEMP", "XDG_CACHE_HOME", "XDG_CONFIG_HOME"} {
		environment = append(environment, key+"=/proc/self/fd/5")
	}
	s.environment = environment
	encoded, _ := json.Marshal(environment)
	hash := sha256.Sum256(encoded)
	s.environmentHash = hex.EncodeToString(hash[:])
	return s, nil
}

func diagnosticUnprivileged() bool {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if unix.Capget(&header, &data[0]) != nil {
		return false
	}
	for _, word := range data {
		if word.Effective != 0 || word.Permitted != 0 || word.Inheritable != 0 {
			return false
		}
	}
	return true
}

// A same-UID 0500 directory is not immutable: its owner can chmod it. Borrow
// an empty read-only mount or a non-owned directory with no write permissions
// instead. No directory or driver cache is created, chmodded, or removed.
func diagnosticReadOnlyScratch(directory *os.File) error {
	var filesystem unix.Statfs_t
	var info unix.Stat_t
	if unix.Fstatfs(int(directory.Fd()), &filesystem) != nil || unix.Fstat(int(directory.Fd()), &info) != nil {
		return ErrDiagnosticResources
	}
	if filesystem.Flags&unix.ST_RDONLY == 0 && (info.Uid == uint32(os.Geteuid()) || info.Mode&0222 != 0) {
		return ErrDiagnosticResources
	}
	names, err := directory.Readdirnames(1)
	if len(names) != 0 || err != io.EOF {
		return ErrDiagnosticResources
	}
	return nil
}

func diagnosticEnvironment(options diagnosticProcessOptions) ([]string, error) {
	env := []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "AV_LOG_FORCE_NOCOLOR=1",
		"CUDA_CACHE_DISABLE=1", "MESA_SHADER_CACHE_DISABLE=true", "__GL_SHADER_DISK_CACHE=0"}
	if len(options.LoaderDirectories) > 16 || len(options.HardwareEnvironment) > 4 {
		return nil, ErrDiagnosticResources
	}
	for _, dir := range options.LoaderDirectories {
		if len(dir) > 1024 || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || strings.ContainsAny(dir, ":\x00\r\n") {
			return nil, ErrDiagnosticResources
		}
	}
	if len(options.LoaderDirectories) != 0 {
		env = append(env, "LD_LIBRARY_PATH="+strings.Join(options.LoaderDirectories, ":"))
	}
	keys := make([]string, 0, len(options.HardwareEnvironment))
	for key, value := range options.HardwareEnvironment {
		if value == "" || len(value) > 1024 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, ErrDiagnosticResources
		}
		switch key {
		case "LIBVA_DRIVER_NAME", "MESA_LOADER_DRIVER_OVERRIDE":
			if len(value) > 64 || strings.IndexFunc(value, func(r rune) bool {
				return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
			}) >= 0 {
				return nil, ErrDiagnosticResources
			}
		case "LIBVA_DRIVERS_PATH":
			if !filepath.IsAbs(value) || filepath.Clean(value) != value || strings.Contains(value, ":") {
				return nil, ErrDiagnosticResources
			}
		case "CUDA_VISIBLE_DEVICES":
			// Numeric device remapping is explicit and included in the environment
			// identity. UUID selection and ambient remapping are not inferred.
			if len(value) > 85 || strings.IndexFunc(value, func(r rune) bool { return !(r >= '0' && r <= '9' || r == ',') }) >= 0 {
				return nil, ErrDiagnosticResources
			}
			seen := make(map[int]bool)
			for _, field := range strings.Split(value, ",") {
				device, err := strconv.Atoi(field)
				if err != nil || device < 0 || device > 31 || strconv.Itoa(device) != field || seen[device] {
					return nil, ErrDiagnosticResources
				}
				seen[device] = true
			}
		default:
			return nil, ErrDiagnosticResources
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+options.HardwareEnvironment[key])
	}
	return env, nil
}

func (s *diagnosticProcessSession) hashTool() (string, error) {
	info, err := s.tool.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maxVideoSeekToolBytes {
		return "", ErrDiagnosticTool
	}
	stamp, err := VideoSeekSourceIdentity(info)
	pathInfo, pathErr := os.Stat(s.toolPath)
	if err != nil || pathErr != nil || stamp != s.toolStamp || !os.SameFile(info, pathInfo) {
		return "", ErrDiagnosticTool
	}
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	for offset := int64(0); offset < info.Size(); {
		if err := s.ctx.Err(); err != nil {
			return "", err
		}
		n, err := s.tool.ReadAt(buffer[:min(int64(len(buffer)), info.Size()-offset)], offset)
		if err != nil || n == 0 {
			return "", ErrDiagnosticTool
		}
		_, _ = hash.Write(buffer[:n])
		offset += int64(n)
	}
	after, err := s.tool.Stat()
	if err != nil {
		return "", ErrDiagnosticTool
	}
	afterStamp, err := VideoSeekSourceIdentity(after)
	if err != nil || afterStamp != s.toolStamp {
		return "", ErrDiagnosticTool
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// The immutable memfd has no writable descriptors or paths by the time it is
// borrowed by FFmpeg. Regular files with merely matching before/after hashes
// are insufficient: a concurrent writer could change their intermediate bytes.
func newDiagnosticInput(data []byte) (*os.File, error) {
	if len(data) == 0 || len(data) > DiagnosticCompressedBytesLimit {
		return nil, ErrDiagnosticInput
	}
	fd, err := unix.MemfdCreate("goby-media-sample", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, ErrDiagnosticInput
	}
	writable := os.NewFile(uintptr(fd), "goby-media-sample")
	defer writable.Close()
	if n, err := writable.Write(data); err != nil || n != len(data) {
		return nil, ErrDiagnosticInput
	}
	const seals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	if _, err = unix.FcntlInt(writable.Fd(), unix.F_ADD_SEALS, seals); err != nil {
		return nil, ErrDiagnosticInput
	}
	readFD, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", fd), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDiagnosticInput
	}
	return os.NewFile(uintptr(readFD), "goby-media-sample"), nil
}

func diagnosticCheckInput(file *os.File, plan DiagnosticPlan, expectedHash string) (string, error) {
	if file == nil || !videoSeekSHA256(expectedHash) {
		return "", ErrDiagnosticInput
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(plan.Limits.MaximumInputBytes) {
		return "", ErrDiagnosticInput
	}
	flags, err := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return "", ErrDiagnosticInput
	}
	const requiredSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	seals, err := unix.FcntlInt(file.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&requiredSeals != requiredSeals {
		return "", ErrDiagnosticInput
	}
	data := make([]byte, int(info.Size()))
	if _, err := file.ReadAt(data, 0); err != nil {
		return "", ErrDiagnosticInput
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	if digest != expectedHash {
		return "", ErrDiagnosticInput
	}
	if !plan.Input.RequiresPreparation {
		raw, err := GenerateDiagnosticSample(plan.Input.Kind)
		if err != nil || raw.SHA256 != digest {
			return "", ErrDiagnosticInput
		}
	}
	return digest, nil
}

// The caller retains the separately validated preparation/reference record for
// compressed inputs. A matching immutable input digest is not that validation.
func (s *diagnosticProcessSession) run(plan DiagnosticPlan, input *os.File, expectedHash string) (diagnosticCommandObservation, error) {
	if err := ValidateDiagnosticPlan(plan); err != nil {
		return diagnosticCommandObservation{}, err
	}
	hash, err := diagnosticCheckInput(input, plan, expectedHash)
	if err != nil {
		return diagnosticCommandObservation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.command(input, plan.Args, plan.Limits.MaximumStdoutBytes)
	result.InputSHA256 = hash
	return result, err
}

// Tool version collection uses the same aggregate limits, environment, file
// identity and command count as media work. No ambient runLimited call is used.
func (s *diagnosticProcessSession) version() (diagnosticCommandObservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.command(nil, []string{"-version"}, maxProcessStderr)
}

func (s *diagnosticProcessSession) command(input *os.File, args []string, outputBytes int) (diagnosticCommandObservation, error) {
	result := diagnosticCommandObservation{ExitCode: -1, ToolSHA256: s.toolHash, EnvironmentSHA256: s.environmentHash}
	if s.closed || s.failed || s.pending != nil || s.commands >= diagnosticMaximumCommands {
		return result, ErrDiagnosticResources
	}
	if err := s.ctx.Err(); err != nil {
		return result, err
	}
	actualHash, err := s.hashTool()
	if err != nil || actualHash != s.toolHash {
		s.failed = true
		return result, ErrDiagnosticTool
	}
	if err = s.group.checkEmpty(); err != nil {
		s.failed = true
		return result, ErrDiagnosticClosure
	}
	s.commands++
	started := time.Now()
	ctx, cancel := context.WithTimeout(s.ctx, DiagnosticDeadline)
	defer cancel()
	stdout := &limitedOutput{limit: outputBytes, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	cmd := exec.CommandContext(ctx, "/proc/self/fd/4", args...)
	cmd.Args[0] = "ffmpeg"
	cmd.ExtraFiles = []*os.File{input, s.tool, s.directoryFD}
	cmd.Env = s.environment
	// Go changes directory before remapping ExtraFiles. At that point the
	// child still has the parent's held descriptor number, not the future fd5.
	cmd.Dir = fmt.Sprintf("/proc/self/fd/%d", s.directoryFD.Fd())
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = stdout, stderr, time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(s.group.group.Fd())}
	retired, startErr := startMediaProcess(cmd)
	if startErr != nil {
		s.failed = true
		closeCtx, closeCancel := context.WithTimeout(context.Background(), diagnosticCloseDeadline)
		closeErr := s.group.retire(closeCtx)
		closeCancel()
		result.ProcessesClosed = closeErr == nil
		if closeErr != nil {
			return result, ErrDiagnosticClosure
		}
		return result, ErrDiagnosticResources
	}
	result.Started = true
	done := make(chan error, 1)
	go func() { done <- errors.Join(<-retired, cmd.Wait()) }()
	s.pending = done
	var waitErr error
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		// Cancel may leave an uninterruptible kernel task. Keep the owning
		// session and join handle; never read buffers while the writer lives.
		timer := time.NewTimer(diagnosticCloseDeadline)
		select {
		case waitErr = <-done:
			timer.Stop()
		case <-timer.C:
			s.failed = true
			return result, ErrDiagnosticClosure
		}
	}
	s.pending = nil
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	result.Elapsed = time.Since(started)
	result.Stdout, result.Stderr = stdout.buffer.Bytes(), stderr.buffer.Bytes()
	// Reaching a muxer cap exactly is also a truncation suspicion. Success
	// therefore requires strictly less than the byte limit on both streams.
	result.OutputLimitReached = stdout.exceeded || stderr.exceeded || len(result.Stdout) >= outputBytes || len(result.Stderr) >= maxProcessStderr
	closeCtx, closeCancel := context.WithTimeout(context.Background(), diagnosticCloseDeadline)
	closeErr := s.group.retire(closeCtx)
	closeCancel()
	result.ProcessesClosed = closeErr == nil
	events, err := s.group.limitEvents()
	result.MemoryLimitEvents, result.MemoryOOMEvents, result.MemoryOOMKills, result.TaskLimitEvents =
		events.memoryMax, events.memoryOOM, events.memoryOOMKills, events.tasksMax
	if closeErr != nil || err != nil {
		s.failed = true
		return result, ErrDiagnosticClosure
	}
	if events.exhausted() {
		s.failed = true
		return result, ErrDiagnosticResources
	}
	if result.OutputLimitReached {
		return result, ErrOutputLimit
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if waitErr != nil {
		return result, ErrDiagnosticExecution
	}
	actualHash, err = s.hashTool()
	if err != nil || actualHash != s.toolHash {
		s.failed = true
		return result, ErrDiagnosticTool
	}
	return result, nil
}

func (s *diagnosticProcessSession) close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticCloseDeadline)
	defer cancel()
	if s.group != nil && s.group.group != nil {
		if err := s.group.retire(ctx); err != nil {
			s.failed = true
			return err
		}
	}
	if s.pending != nil {
		select {
		case <-s.pending:
			s.pending = nil
		case <-ctx.Done():
			s.failed = true
			return ErrDiagnosticClosure
		}
	}
	if err := s.group.close(); err != nil {
		s.failed = true
		return err
	}
	if s.tool != nil {
		toolErr := s.tool.Close()
		s.tool = nil
		if toolErr != nil {
			s.failed = true
			return ErrDiagnosticClosure
		}
	}
	// The scratch descriptor is borrowed from deployment; its directory is
	// never modified or removed by this process owner.
	if s.directoryFD != nil {
		err := s.directoryFD.Close()
		s.directoryFD = nil
		if err != nil {
			s.failed = true
			return ErrDiagnosticClosure
		}
	}
	s.closed = true
	return nil
}
