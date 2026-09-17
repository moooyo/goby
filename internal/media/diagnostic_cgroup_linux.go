package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type diagnosticCgroup struct {
	parent   *os.File
	group    *os.File
	name     string
	identity unix.Stat_t
}

type diagnosticLimitEvents struct {
	memoryMax      uint64
	memoryOOM      uint64
	memoryOOMKills uint64
	tasksMax       uint64
}

func (events diagnosticLimitEvents) exhausted() bool {
	return events.memoryMax != 0 || events.memoryOOM != 0 || events.memoryOOMKills != 0 || events.tasksMax != 0
}

// Open every component without following a symlink or a procfs magic link.
// No fallback to pathname traversal is permitted on an unsupported kernel.
func diagnosticOpenAbsolute(path string, flags int) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return nil, ErrDiagnosticResources
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, ErrDiagnosticResources
	}
	return os.NewFile(uintptr(fd), path), nil
}

func newDiagnosticCgroup(parentPath string) (*diagnosticCgroup, error) {
	parent, err := diagnosticOpenAbsolute(parentPath, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return nil, ErrDiagnosticResources
	}
	var filesystem unix.Statfs_t
	if unix.Fstatfs(int(parent.Fd()), &filesystem) != nil || filesystem.Type != unix.CGROUP2_SUPER_MAGIC {
		_ = parent.Close()
		return nil, ErrDiagnosticResources
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		_ = parent.Close()
		return nil, ErrDiagnosticResources
	}
	name := "goby-media-" + hex.EncodeToString(random[:])
	if unix.Mkdirat(int(parent.Fd()), name, 0700) != nil {
		_ = parent.Close()
		return nil, ErrDiagnosticResources
	}
	g := &diagnosticCgroup{parent: parent, name: name}
	if unix.Fstatat(int(parent.Fd()), name, &g.identity, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return g, ErrDiagnosticResources
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return g, ErrDiagnosticResources
	}
	g.group = os.NewFile(uintptr(fd), name)
	fail := func() (*diagnosticCgroup, error) {
		if err := g.close(); err != nil {
			return g, errors.Join(ErrDiagnosticResources, err)
		}
		return nil, ErrDiagnosticResources
	}
	// These files must already exist in this new leaf. Missing controllers do
	// not authorize changing an ancestor's subtree_control or resource limits.
	for _, setting := range [][2]string{
		{"memory.max", strconv.Itoa(diagnosticMemoryBytes)},
		{"memory.swap.max", "0"}, {"memory.oom.group", "1"},
		{"pids.max", strconv.Itoa(diagnosticMaximumTasks)},
	} {
		if g.write(setting[0], setting[1]) != nil {
			return fail()
		}
		actual, err := g.read(setting[0])
		if err != nil || strings.TrimSpace(string(actual)) != setting[1] {
			return fail()
		}
	}
	if err := g.checkEmpty(); err != nil {
		return fail()
	}
	// A fresh group must start with zero limit events, and the kernel must
	// expose a whole-subtree kill interface before any executable is started.
	events, err := g.limitEvents()
	kill, killErr := g.open("cgroup.kill", unix.O_WRONLY)
	if kill != nil {
		_ = kill.Close()
	}
	if err != nil || events.exhausted() || killErr != nil {
		return fail()
	}
	return g, nil
}

func (g *diagnosticCgroup) open(name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(g.group.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDiagnosticResources
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (g *diagnosticCgroup) write(name, value string) error {
	f, err := g.open(name, unix.O_WRONLY)
	if err != nil {
		return err
	}
	n, writeErr := f.WriteString(value)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || n != len(value) {
		return ErrDiagnosticResources
	}
	return nil
}

func (g *diagnosticCgroup) read(name string) ([]byte, error) {
	f, err := g.open(name, unix.O_RDONLY)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, 4097))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(data) > 4096 {
		return nil, ErrDiagnosticResources
	}
	return data, nil
}

func diagnosticCounter(data []byte, key string) (uint64, error) {
	var value uint64
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return 0, ErrDiagnosticResources
		}
		if fields[0] != key {
			continue
		}
		if found {
			return 0, ErrDiagnosticResources
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, ErrDiagnosticResources
		}
		value, found = n, true
	}
	if !found {
		return 0, ErrDiagnosticResources
	}
	return value, nil
}

func (g *diagnosticCgroup) limitEvents() (diagnosticLimitEvents, error) {
	var events diagnosticLimitEvents
	memoryData, err := g.read("memory.events")
	if err != nil {
		return events, err
	}
	for key, target := range map[string]*uint64{"max": &events.memoryMax, "oom": &events.memoryOOM, "oom_kill": &events.memoryOOMKills} {
		*target, err = diagnosticCounter(memoryData, key)
		if err != nil {
			return events, err
		}
	}
	taskData, err := g.read("pids.events")
	if err != nil {
		return events, err
	}
	events.tasksMax, err = diagnosticCounter(taskData, "max")
	return events, err
}

func (g *diagnosticCgroup) checkEmpty() error {
	data, err := g.read("cgroup.events")
	if err != nil {
		return err
	}
	populated, err := diagnosticCounter(data, "populated")
	if err != nil || populated != 0 {
		return ErrDiagnosticClosure
	}
	return nil
}

// The directory descriptor remains held through signaling and the empty check.
// No numeric PID is used after reaping, and only this freshly created leaf is
// killed. A failed close preserves the group and handles for a later join.
func (g *diagnosticCgroup) retire(ctx context.Context) error {
	if err := g.write("cgroup.kill", "1"); err != nil {
		return ErrDiagnosticClosure
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := g.checkEmpty(); err == nil {
			return nil
		} else if !errors.Is(err, ErrDiagnosticClosure) {
			return err
		}
		select {
		case <-ctx.Done():
			return ErrDiagnosticClosure
		case <-ticker.C:
		}
	}
}

func (g *diagnosticCgroup) close() error {
	if g == nil || g.parent == nil {
		return nil
	}
	var held, named unix.Stat_t
	if g.identity.Ino == 0 || unix.Fstatat(int(g.parent.Fd()), g.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
		g.identity.Dev != named.Dev || g.identity.Ino != named.Ino {
		return ErrDiagnosticClosure
	}
	if g.group == nil {
		fd, err := unix.Openat(int(g.parent.Fd()), g.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return ErrDiagnosticClosure
		}
		g.group = os.NewFile(uintptr(fd), g.name)
	}
	if unix.Fstat(int(g.group.Fd()), &held) != nil || held.Dev != named.Dev || held.Ino != named.Ino {
		return ErrDiagnosticClosure
	}
	if err := g.checkEmpty(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(g.parent.Fd()), g.name, unix.AT_REMOVEDIR); err != nil {
		return ErrDiagnosticClosure
	}
	groupErr, parentErr := g.group.Close(), g.parent.Close()
	g.group, g.parent = nil, nil
	if groupErr != nil || parentErr != nil {
		return ErrDiagnosticClosure
	}
	return nil
}
