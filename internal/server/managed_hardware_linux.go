//go:build linux

package server

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/moooyo/goby/internal/config"
	"golang.org/x/sys/unix"
)

type managedHardwareFilesystem struct {
	lstat        func(string) (os.FileInfo, error)
	stat         func(string) (os.FileInfo, error)
	evalSymlinks func(string) (string, error)
	readVendor   func(string) ([]byte, error)
}

func inspectManagedAMDHardware(device string) (managedHardwareIdentity, string) {
	return inspectManagedAMDHardwareWithFilesystem(device, managedHardwareFilesystem{
		lstat: os.Lstat, stat: os.Stat, evalSymlinks: filepath.EvalSymlinks,
		readVendor: func(path string) ([]byte, error) {
			file, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			return io.ReadAll(io.LimitReader(file, 65))
		},
	})
}

// The sysfs association is derived from the actual character-device identity,
// never from a directory scan or the untrusted basename alone. Normal sysfs
// device links are resolved, while the authorized /dev path rejects all links.
func inspectManagedAMDHardwareWithFilesystem(device string, filesystem managedHardwareFilesystem) (managedHardwareIdentity, string) {
	node, code := managedHardwareNodeIdentity(device, filesystem)
	if code != "" {
		return managedHardwareIdentity{}, code
	}
	deviceLink := fmt.Sprintf("/sys/dev/char/%d:%d/device", unix.Major(node.Special), unix.Minor(node.Special))
	systemPath, err := filesystem.evalSymlinks(deviceLink)
	if err != nil || !strings.HasPrefix(systemPath, "/sys/devices/") || filepath.Clean(systemPath) != systemPath {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	systemInfo, err := filesystem.stat(systemPath)
	if err != nil || systemInfo == nil || !systemInfo.IsDir() {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	systemIdentity, valid := managedHardwareStatIdentity(systemInfo)
	if !valid {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	vendorPath := filepath.Join(systemPath, "vendor")
	vendorInfo, err := filesystem.lstat(vendorPath)
	if err != nil || vendorInfo == nil || !vendorInfo.Mode().IsRegular() {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	vendorIdentity, valid := managedHardwareStatIdentity(vendorInfo)
	if !valid {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	vendor, err := filesystem.readVendor(vendorPath)
	if err != nil || len(vendor) > 64 {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	if strings.TrimSpace(string(vendor)) != "0x1002" {
		return managedHardwareIdentity{}, managedHardwareNotAMD
	}
	// Reobserve the node and the sysfs association after reading vendor data so
	// a replacement during observation does not become a startup baseline.
	afterNode, code := managedHardwareNodeIdentity(device, filesystem)
	if code != "" {
		return managedHardwareIdentity{}, code
	}
	afterPath, pathErr := filesystem.evalSymlinks(deviceLink)
	afterSystemInfo, systemErr := filesystem.stat(systemPath)
	afterVendorInfo, vendorErr := filesystem.lstat(vendorPath)
	if pathErr != nil || systemErr != nil || vendorErr != nil {
		return managedHardwareIdentity{}, managedHardwareUnavailable
	}
	afterSystem, systemValid := managedHardwareStatIdentity(afterSystemInfo)
	afterVendor, vendorValid := managedHardwareStatIdentity(afterVendorInfo)
	if !systemValid || !vendorValid || node != afterNode || systemPath != afterPath ||
		systemIdentity != afterSystem || vendorIdentity != afterVendor {
		return managedHardwareIdentity{}, managedHardwareIdentityChanged
	}
	return managedHardwareIdentity{Node: node, SystemDevice: systemIdentity, VendorFile: vendorIdentity, SystemPath: systemPath, Vendor: "0x1002"}, ""
}

func managedHardwareNodeIdentity(device string, filesystem managedHardwareFilesystem) (managedHardwareFileIdentity, string) {
	if !config.ValidAMDDevicePath(device) {
		return managedHardwareFileIdentity{}, managedHardwareInvalid
	}
	for _, parent := range []string{"/dev", "/dev/dri"} {
		info, err := filesystem.lstat(parent)
		if err != nil {
			if os.IsNotExist(err) {
				return managedHardwareFileIdentity{}, managedHardwareMissing
			}
			return managedHardwareFileIdentity{}, managedHardwareUnavailable
		}
		if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return managedHardwareFileIdentity{}, managedHardwareInvalid
		}
	}
	info, err := filesystem.lstat(device)
	if err != nil {
		if os.IsNotExist(err) {
			return managedHardwareFileIdentity{}, managedHardwareMissing
		}
		return managedHardwareFileIdentity{}, managedHardwareUnavailable
	}
	if info == nil || info.Mode()&os.ModeSymlink != 0 || info.Mode()&(os.ModeDevice|os.ModeCharDevice) != (os.ModeDevice|os.ModeCharDevice) {
		return managedHardwareFileIdentity{}, managedHardwareInvalid
	}
	identity, valid := managedHardwareStatIdentity(info)
	minor, _ := strconv.Atoi(strings.TrimPrefix(device, "/dev/dri/renderD"))
	if !valid || unix.Major(identity.Special) != 226 || unix.Minor(identity.Special) != uint32(minor) {
		return managedHardwareFileIdentity{}, managedHardwareInvalid
	}
	return identity, ""
}

func managedHardwareStatIdentity(info os.FileInfo) (managedHardwareFileIdentity, bool) {
	if info == nil {
		return managedHardwareFileIdentity{}, false
	}
	stat, valid := info.Sys().(*syscall.Stat_t)
	if !valid || stat == nil {
		return managedHardwareFileIdentity{}, false
	}
	return managedHardwareFileIdentity{Device: uint64(stat.Dev), Inode: stat.Ino, Special: uint64(stat.Rdev), Mode: stat.Mode,
		ChangeSeconds: int64(stat.Ctim.Sec), ChangeNanoseconds: int64(stat.Ctim.Nsec)}, true
}
