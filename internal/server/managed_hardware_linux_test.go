//go:build linux

package server

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type managedHardwareFakeFileInfo struct {
	mode os.FileMode
	stat *syscall.Stat_t
}

func (info managedHardwareFakeFileInfo) Name() string       { return "fixture" }
func (info managedHardwareFakeFileInfo) Size() int64        { return 7 }
func (info managedHardwareFakeFileInfo) Mode() os.FileMode  { return info.mode }
func (info managedHardwareFakeFileInfo) ModTime() time.Time { return time.Unix(100, 0) }
func (info managedHardwareFakeFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info managedHardwareFakeFileInfo) Sys() any           { return info.stat }

type managedAMDTestFilesystem struct {
	files      map[string]managedHardwareFakeFileInfo
	systemPath string
	vendor     []byte
	readError  error
	readHook   func()
	reads      int
}

func newManagedAMDTestFilesystem() *managedAMDTestFilesystem {
	directory := managedHardwareFakeFileInfo{mode: os.ModeDir | 0755,
		stat: &syscall.Stat_t{Dev: 1, Ino: 1, Mode: unix.S_IFDIR | 0755}}
	fixture := &managedAMDTestFilesystem{systemPath: "/sys/devices/pci0000:00/0000:00:01.0", vendor: []byte("0x1002\n"),
		files: map[string]managedHardwareFakeFileInfo{"/dev": directory, "/dev/dri": directory,
			"/dev/dri/renderD128": {mode: os.ModeDevice | os.ModeCharDevice | 0600,
				stat: &syscall.Stat_t{Dev: 1, Ino: 10, Rdev: unix.Mkdev(226, 128), Mode: unix.S_IFCHR | 0600, Ctim: syscall.Timespec{Sec: 100}}},
		}}
	fixture.files[fixture.systemPath] = managedHardwareFakeFileInfo{mode: os.ModeDir | 0755,
		stat: &syscall.Stat_t{Dev: 2, Ino: 20, Mode: unix.S_IFDIR | 0755}}
	fixture.files[fixture.systemPath+"/vendor"] = managedHardwareFakeFileInfo{mode: 0444,
		stat: &syscall.Stat_t{Dev: 2, Ino: 30, Mode: unix.S_IFREG | 0444}}
	return fixture
}

func (fixture *managedAMDTestFilesystem) filesystem() managedHardwareFilesystem {
	lookup := func(path string) (os.FileInfo, error) {
		info, exists := fixture.files[path]
		if !exists {
			return nil, fs.ErrNotExist
		}
		return info, nil
	}
	return managedHardwareFilesystem{lstat: lookup, stat: lookup,
		evalSymlinks: func(path string) (string, error) {
			if path != "/sys/dev/char/226:128/device" {
				return "", fs.ErrNotExist
			}
			return fixture.systemPath, nil
		},
		readVendor: func(path string) ([]byte, error) {
			fixture.reads++
			if path != fixture.systemPath+"/vendor" {
				return nil, fs.ErrNotExist
			}
			if fixture.readHook != nil {
				fixture.readHook()
			}
			return append([]byte(nil), fixture.vendor...), fixture.readError
		},
	}
}

func TestManagedAMDHardwareObserverBindsDeviceNumberToAMDVendor(t *testing.T) {
	fixture := newManagedAMDTestFilesystem()
	identity, code := inspectManagedAMDHardwareWithFilesystem("/dev/dri/renderD128", fixture.filesystem())
	if code != "" || identity.Node.Inode != 10 || identity.Node.Special != unix.Mkdev(226, 128) ||
		identity.SystemPath != fixture.systemPath || identity.SystemDevice.Inode != 20 || identity.VendorFile.Inode != 30 || identity.Vendor != "0x1002" || fixture.reads != 1 {
		t.Fatalf("incorrect bounded AMD identity: %+v, %s, reads=%d", identity, code, fixture.reads)
	}
}

func TestManagedAMDHardwareObserverRejectsUnsafeNodeWithoutReadingVendor(t *testing.T) {
	for name, mutate := range map[string]func(*managedAMDTestFilesystem){
		"missing node":   func(f *managedAMDTestFilesystem) { delete(f.files, "/dev/dri/renderD128") },
		"missing parent": func(f *managedAMDTestFilesystem) { delete(f.files, "/dev/dri") },
		"node symlink": func(f *managedAMDTestFilesystem) {
			info := f.files["/dev/dri/renderD128"]
			info.mode = os.ModeSymlink
			f.files["/dev/dri/renderD128"] = info
		},
		"parent symlink": func(f *managedAMDTestFilesystem) {
			info := f.files["/dev/dri"]
			info.mode = os.ModeSymlink
			f.files["/dev/dri"] = info
		},
		"regular file": func(f *managedAMDTestFilesystem) {
			info := f.files["/dev/dri/renderD128"]
			info.mode = 0600
			f.files["/dev/dri/renderD128"] = info
		},
		"block device": func(f *managedAMDTestFilesystem) {
			info := f.files["/dev/dri/renderD128"]
			info.mode = os.ModeDevice
			f.files["/dev/dri/renderD128"] = info
		},
		"missing stat": func(f *managedAMDTestFilesystem) {
			info := f.files["/dev/dri/renderD128"]
			info.stat = nil
			f.files["/dev/dri/renderD128"] = info
		},
		"wrong major": func(f *managedAMDTestFilesystem) { f.files["/dev/dri/renderD128"].stat.Rdev = unix.Mkdev(1, 128) },
		"wrong minor": func(f *managedAMDTestFilesystem) { f.files["/dev/dri/renderD128"].stat.Rdev = unix.Mkdev(226, 129) },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newManagedAMDTestFilesystem()
			mutate(fixture)
			identity, code := inspectManagedAMDHardwareWithFilesystem("/dev/dri/renderD128", fixture.filesystem())
			if code == "" || identity != (managedHardwareIdentity{}) || fixture.reads != 0 {
				t.Fatalf("unsafe node reached vendor read: %+v, %s, reads=%d", identity, code, fixture.reads)
			}
		})
	}
	for _, device := range []string{"/dev/dri/renderD127", "/dev/dri/renderD256", "/dev/dri/renderD0128", "/dev/dri/renderD128/../renderD129", "/tmp/renderD128"} {
		fixture := newManagedAMDTestFilesystem()
		if _, code := inspectManagedAMDHardwareWithFilesystem(device, fixture.filesystem()); code != managedHardwareInvalid || fixture.reads != 0 {
			t.Fatalf("noncanonical device %q reached filesystem inspection: %s", device, code)
		}
	}
}

func TestManagedAMDHardwareObserverRejectsUnprovenVendor(t *testing.T) {
	for name, mutate := range map[string]func(*managedAMDTestFilesystem){
		"foreign vendor":    func(f *managedAMDTestFilesystem) { f.vendor = []byte("0x8086\n") },
		"empty vendor":      func(f *managedAMDTestFilesystem) { f.vendor = nil },
		"vendor prefix":     func(f *managedAMDTestFilesystem) { f.vendor = []byte("0x1002-extra") },
		"oversized vendor":  func(f *managedAMDTestFilesystem) { f.vendor = []byte("0x1002" + strings.Repeat(" ", 59)) },
		"vendor read error": func(f *managedAMDTestFilesystem) { f.readError = errors.New("private device path failure") },
		"missing vendor":    func(f *managedAMDTestFilesystem) { delete(f.files, f.systemPath+"/vendor") },
		"vendor symlink": func(f *managedAMDTestFilesystem) {
			info := f.files[f.systemPath+"/vendor"]
			info.mode = os.ModeSymlink
			f.files[f.systemPath+"/vendor"] = info
		},
		"missing system device": func(f *managedAMDTestFilesystem) { delete(f.files, f.systemPath) },
		"system outside sysfs":  func(f *managedAMDTestFilesystem) { f.systemPath = "/private/device" },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newManagedAMDTestFilesystem()
			mutate(fixture)
			identity, code := inspectManagedAMDHardwareWithFilesystem("/dev/dri/renderD128", fixture.filesystem())
			if code == "" || identity != (managedHardwareIdentity{}) || strings.Contains(code, "/") || strings.Contains(code, "private") {
				t.Fatalf("unproven vendor was accepted or exposed details: %+v, %s", identity, code)
			}
		})
	}
}

func TestManagedAMDHardwareObserverRejectsReplacementDuringObservation(t *testing.T) {
	for name, mutate := range map[string]func(*managedAMDTestFilesystem){
		"node inode":     func(f *managedAMDTestFilesystem) { f.files["/dev/dri/renderD128"].stat.Ino++ },
		"node ctime":     func(f *managedAMDTestFilesystem) { f.files["/dev/dri/renderD128"].stat.Ctim.Nsec++ },
		"system binding": func(f *managedAMDTestFilesystem) { f.systemPath = "/sys/devices/replaced-device" },
		"system inode":   func(f *managedAMDTestFilesystem) { f.files[f.systemPath].stat.Ino++ },
		"vendor inode":   func(f *managedAMDTestFilesystem) { f.files[f.systemPath+"/vendor"].stat.Ino++ },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newManagedAMDTestFilesystem()
			fixture.readHook = func() { mutate(fixture) }
			identity, code := inspectManagedAMDHardwareWithFilesystem("/dev/dri/renderD128", fixture.filesystem())
			if code != managedHardwareIdentityChanged || identity != (managedHardwareIdentity{}) {
				t.Fatalf("replacement became startup identity: %+v, %s", identity, code)
			}
		})
	}
}
