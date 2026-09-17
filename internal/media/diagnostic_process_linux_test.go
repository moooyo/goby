package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiagnosticProcessPreservesPreExecCgroupPlacement(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "/unused/ffmpeg")
	attributes := &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: 91, Pdeathsig: syscall.SIGKILL}
	cmd.SysProcAttr = attributes
	_ = configureMediaProcess(cmd)
	if cmd.SysProcAttr != attributes || !attributes.UseCgroupFD || attributes.CgroupFD != 91 ||
		attributes.Pdeathsig != syscall.SIGKILL || !attributes.Setpgid || cmd.Cancel == nil {
		t.Fatal("process setup lost pre-exec isolation attributes")
	}
}

func TestDiagnosticEnvironmentIsClosedAndDeterministic(t *testing.T) {
	t.Setenv("FFREPORT", "file=private-output")
	t.Setenv("LD_PRELOAD", "/private/preload.so")
	t.Setenv("CUDA_VISIBLE_DEVICES", "ambient-marker")
	first := diagnosticProcessOptions{LoaderDirectories: []string{"/opt/tool/lib"}, HardwareEnvironment: map[string]string{
		"LIBVA_DRIVER_NAME": "iHD", "CUDA_VISIBLE_DEVICES": "1,0",
	}}
	second := diagnosticProcessOptions{LoaderDirectories: []string{"/opt/tool/lib"}, HardwareEnvironment: map[string]string{
		"CUDA_VISIBLE_DEVICES": "1,0", "LIBVA_DRIVER_NAME": "iHD",
	}}
	a, err := diagnosticEnvironment(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := diagnosticEnvironment(second)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("environment identity depends on map iteration")
	}
	joined := strings.Join(a, "\n")
	for _, rejected := range []string{"FFREPORT", "LD_PRELOAD", "private-output", "ambient-marker"} {
		if strings.Contains(joined, rejected) {
			t.Fatalf("inherited %s", rejected)
		}
	}
	if !strings.Contains(joined, "LD_LIBRARY_PATH=/opt/tool/lib") || !strings.Contains(joined, "CUDA_CACHE_DISABLE=1") {
		t.Fatal("explicit loader path or cache policy missing")
	}
	for _, options := range []diagnosticProcessOptions{
		{LoaderDirectories: []string{"."}}, {LoaderDirectories: []string{"/opt/lib:"}},
		{HardwareEnvironment: map[string]string{"LD_PRELOAD": "/tmp/library"}},
		{HardwareEnvironment: map[string]string{"LIBVA_DRIVER_NAME": "../driver"}},
		{HardwareEnvironment: map[string]string{"CUDA_VISIBLE_DEVICES": "0\nPRIVATE=value"}},
	} {
		if _, err := diagnosticEnvironment(options); !errors.Is(err, ErrDiagnosticResources) {
			t.Fatal("accepted an open environment")
		}
	}
}

func TestDiagnosticCountersRejectMissingDuplicateAndMalformedFacts(t *testing.T) {
	value, err := diagnosticCounter([]byte("low 0\nmax 7\noom 0\n"), "max")
	if err != nil || value != 7 {
		t.Fatal("lost actual limit event count")
	}
	for _, data := range []string{"", "oom 1\n", "max 0\nmax 0\n", "max -1\n", "max 18446744073709551616\n", "max 0 extra\n"} {
		if _, err := diagnosticCounter([]byte(data), "max"); !errors.Is(err, ErrDiagnosticResources) {
			t.Fatalf("accepted malformed counters: %q", data)
		}
	}
}

func TestDiagnosticInputIsImmutableReadOnlyAndContentBound(t *testing.T) {
	raw, err := GenerateDiagnosticSample(DiagnosticRawAudio)
	if err != nil {
		t.Fatal(err)
	}
	input, err := newDiagnosticInput(raw.Data)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	plan, err := BuildDiagnosticPlan(DiagnosticEncode, DiagnosticRawAudio, DiagnosticProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = diagnosticCheckInput(input, plan, raw.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err = input.WriteAt([]byte{0}, 0); err == nil {
		t.Fatal("sample descriptor is writable")
	}
	if err = unix.Ftruncate(int(input.Fd()), 0); err == nil {
		t.Fatal("sample can shrink")
	}
	if _, err = diagnosticCheckInput(input, plan, strings.Repeat("0", 64)); !errors.Is(err, ErrDiagnosticInput) {
		t.Fatal("accepted wrong input digest")
	}
	changed := append([]byte(nil), raw.Data...)
	changed[17] ^= 0x40
	mutated, err := newDiagnosticInput(changed)
	if err != nil {
		t.Fatal(err)
	}
	defer mutated.Close()
	digest := sha256.Sum256(changed)
	if _, err = diagnosticCheckInput(mutated, plan, hex.EncodeToString(digest[:])); !errors.Is(err, ErrDiagnosticInput) {
		t.Fatal("accepted a caller-selected raw fixture despite its matching digest")
	}
	path := t.TempDir() + "/ordinary-sample"
	if err := os.WriteFile(path, raw.Data, 0400); err != nil {
		t.Fatal(err)
	}
	ordinary, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	if _, err = diagnosticCheckInput(ordinary, plan, raw.SHA256); !errors.Is(err, ErrDiagnosticInput) {
		t.Fatal("a mutable filesystem inode was accepted as sealed input")
	}
}

func TestDiagnosticCgroupRejectsOrdinaryFilesystemWithoutCreatingChildren(t *testing.T) {
	parent := t.TempDir()
	if _, err := newDiagnosticCgroup(parent); !errors.Is(err, ErrDiagnosticResources) {
		t.Fatal("accepted a fake cgroup filesystem")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed cgroup admission wrote ordinary filesystem state")
	}
}

func TestDiagnosticReadOnlyScratchRejectsSameUIDOwnership(t *testing.T) {
	path := t.TempDir()
	if err := os.Chmod(path, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0700) })
	directory, err := diagnosticOpenAbsolute(path, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := diagnosticReadOnlyScratch(directory); !errors.Is(err, ErrDiagnosticResources) {
		t.Fatal("same-UID chmod-reversible permissions were accepted as hard read-only scratch")
	}
}

func TestDiagnosticOOMInvalidatesSuccessWithoutLeafMaxEvents(t *testing.T) {
	if (diagnosticLimitEvents{}).exhausted() {
		t.Fatal("zero events are exhausted")
	}
	for _, events := range []diagnosticLimitEvents{{memoryOOM: 1}, {memoryOOMKills: 1}, {tasksMax: 1}, {memoryMax: 1}} {
		if !events.exhausted() {
			t.Fatal("resource failure may be hidden by an otherwise successful process exit")
		}
	}
}

func TestDiagnosticSessionClosesBeforeAnyResourceWasAcquired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	session := &diagnosticProcessSession{ctx: ctx, cancel: cancel, failed: true}
	if err := session.close(); err != nil || !session.closed || ctx.Err() != context.Canceled {
		t.Fatal("partial session did not close")
	}
	if err := session.close(); err != nil {
		t.Fatal("partial close is not idempotent")
	}
}
