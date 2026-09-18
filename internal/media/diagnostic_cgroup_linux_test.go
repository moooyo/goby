package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDiagnosticControllersRequireOnlyTheOwnedDomainDelegation(t *testing.T) {
	for _, test := range []struct {
		data  string
		exact bool
		want  bool
	}{
		{"memory pids\n", false, true},
		{"cpu memory pids\n", false, true},
		{"pids memory\n", true, true},
		{"cpu memory pids\n", true, false},
		{"memory\n", false, false},
		{"pids\n", false, false},
		{"memory pids pids\n", false, false},
		{"", true, false},
	} {
		if got := diagnosticControllers([]byte(test.data), test.exact); got != test.want {
			t.Errorf("controllers %q exact=%t: got %t, want %t", test.data, test.exact, got, test.want)
		}
	}
}

func TestDiagnosticCgroupRejectedFilesystemClosesTransferredDescriptor(t *testing.T) {
	parent, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	group, err := createDiagnosticCgroup(parent, "command-")
	if group != nil || !errors.Is(err, ErrDiagnosticResources) {
		t.Fatal("ordinary filesystem admission did not fail without owned state")
	}
	if _, err := parent.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("rejected group creation leaked its transferred parent descriptor")
	}
}

// These ordinary directories exercise descriptor ownership and failure paths,
// not kernel cgroup behavior. Production creation still requires cgroup v2.
func diagnosticTestOwnedGroup(t *testing.T) (*diagnosticCgroup, string) {
	t.Helper()
	parentPath := t.TempDir()
	path := filepath.Join(parentPath, "owned")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	group, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = group.Close() })
	g := &diagnosticCgroup{parent: parent, group: group, name: "owned"}
	if err := unix.Fstat(int(group.Fd()), &g.identity); err != nil {
		t.Fatal(err)
	}
	return g, path
}

func diagnosticTestControl(t *testing.T, path, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(path, name), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticCgroupKillAttemptMakesDomainTerminal(t *testing.T) {
	for _, writable := range []bool{false, true} {
		g, path := diagnosticTestOwnedGroup(t)
		g.domain = true
		diagnosticTestControl(t, path, "cgroup.events", "populated 0\n")
		if writable {
			diagnosticTestControl(t, path, "cgroup.kill", "0")
		}
		err := g.retire(context.Background())
		if (err == nil) != writable || !g.retired {
			t.Fatal("kill attempt did not permanently retire the domain")
		}
		if leaf, err := g.newCommand(); leaf != nil || !errors.Is(err, ErrDiagnosticResources) {
			t.Fatal("a killed domain accepted another command")
		}
	}
}

func TestDiagnosticCgroupRemovalFailurePreservesRetryOwnership(t *testing.T) {
	g, path := diagnosticTestOwnedGroup(t)
	blocker := filepath.Join(path, "blocker")
	if err := os.WriteFile(blocker, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := g.removeEmpty(); !errors.Is(err, ErrDiagnosticClosure) || g.removed || g.parent == nil || g.group == nil {
		t.Fatal("failed removal discarded group ownership")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := g.removeEmpty(); err != nil || !g.removed || g.parent != nil || g.group != nil {
		t.Fatal("retry did not remove the original group and release both descriptors")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := g.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("idempotent close removed a later directory at the old name")
	}
}

func TestDiagnosticCgroupRemovalRejectsReplacementDirectory(t *testing.T) {
	g, path := diagnosticTestOwnedGroup(t)
	moved := path + "-moved"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := g.removeEmpty(); !errors.Is(err, ErrDiagnosticClosure) || g.removed || g.group == nil || g.parent == nil {
		t.Fatal("replacement directory was accepted or original ownership was lost")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("foreign replacement was removed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(moved, path); err != nil {
		t.Fatal(err)
	}
	if err := g.removeEmpty(); err != nil {
		t.Fatal("restored original directory could not be closed:", err)
	}
}

func TestDiagnosticCgroupKillRejectsUnprovenDescriptor(t *testing.T) {
	g, path := diagnosticTestOwnedGroup(t)
	foreign, foreignPath := diagnosticTestOwnedGroup(t)
	diagnosticTestControl(t, path, "cgroup.kill", "0")
	diagnosticTestControl(t, foreignPath, "cgroup.kill", "0")
	// Model a partial open whose descriptor did not match the directory that
	// was recorded after creation. Retrying cleanup must not signal that FD.
	original := g.group
	g.group = foreign.group
	defer func() { g.group = original }()
	if err := g.retire(context.Background()); !errors.Is(err, ErrDiagnosticClosure) || !g.retired {
		t.Fatal("unproven descriptor was signaled or left eligible for execution")
	}
	for _, directory := range []string{path, foreignPath} {
		data, err := os.ReadFile(filepath.Join(directory, "cgroup.kill"))
		if err != nil || string(data) != "0" {
			t.Fatal("failed identity check wrote a kill interface")
		}
	}
}

func TestDiagnosticCgroupRemovedDescriptorRetryDoesNotTouchReplacement(t *testing.T) {
	g, path := diagnosticTestOwnedGroup(t)
	if err := unix.Unlinkat(int(g.parent.Fd()), g.name, unix.AT_REMOVEDIR); err != nil {
		t.Fatal(err)
	}
	g.removed = true
	// Close can report an error after the FD has already been closed. Keep
	// that closed File in its slot, as a previous cleanup attempt would.
	if err := g.group.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	diagnosticTestControl(t, path, "cgroup.kill", "0")
	if err := g.retire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.close(); err != nil || g.group != nil || g.parent != nil {
		t.Fatal("removed group did not release retained descriptor slots")
	}
	data, err := os.ReadFile(filepath.Join(path, "cgroup.kill"))
	if err != nil || string(data) != "0" {
		t.Fatal("descriptor-only retry touched a replacement directory")
	}
}
