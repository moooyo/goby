package server

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

func managedHardwareTestIdentity(inode uint64) managedHardwareIdentity {
	return managedHardwareIdentity{
		Node:         managedHardwareFileIdentity{Device: 1, Inode: inode, Special: 226<<8 | 128, Mode: 0600, ChangeSeconds: 100},
		SystemDevice: managedHardwareFileIdentity{Device: 2, Inode: inode + 100},
		VendorFile:   managedHardwareFileIdentity{Device: 2, Inode: inode + 200},
		SystemPath:   "/sys/devices/private-pci-address", Vendor: "0x1002",
	}
}

func TestManagedHardwareInventoryAuthorizesOnlyStartupUnion(t *testing.T) {
	cfg := config.TranscodingConfig{Hardware: transcode.Hardware{Decode: "vaapi", Encode: "software"},
		AllowedAMDDevices: [8]string{"/dev/dri/renderD255", "/dev/dri/renderD129"}, AllowedAMDDeviceCount: 2}
	var observed []string
	inspect := func(device string) (managedHardwareIdentity, string) {
		observed = append(observed, device)
		return managedHardwareTestIdentity(20), ""
	}
	inventory := newManagedHardwareInventoryWithInspector(cfg, inspect)
	if !slices.Equal(observed, []string{"/dev/dri/renderD128", "/dev/dri/renderD129", "/dev/dri/renderD255"}) {
		t.Fatalf("inventory inspected outside the sorted startup union: %v", observed)
	}
	selection := inventory.defaultSelection()
	if selection.Decode != "vaapi" || selection.Encode != "software" || selection.DeviceID != managedHardwareDeviceID("/dev/dri/renderD128") {
		t.Fatalf("VAAPI startup default = %+v", selection)
	}
	hardware, available, code := inventory.resolve(selection)
	if !available || code != "" || hardware != (transcode.Hardware{Decode: "vaapi", Encode: "software", Device: "/dev/dri/renderD128"}) {
		t.Fatalf("startup device resolution = %+v, %v, %s", hardware, available, code)
	}
	ids := inventory.authorizedDeviceIDs()
	ids[0] = "mutated-id"
	cfg.AllowedAMDDevices[0] = "/dev/dri/renderD200"
	if inventory.authorizedDeviceIDs()[0] == "mutated-id" || inventory.defaultSelection() != selection {
		t.Fatal("inventory generation aliases a mutable input or returned slice")
	}
	projection, err := json.Marshal(inventory.devices())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/dev/", "renderD", "/sys/", "private-pci", "Inode", "Special"} {
		if strings.Contains(string(projection), forbidden) {
			t.Fatalf("management projection contains private identity %q: %s", forbidden, projection)
		}
	}
	if !strings.Contains(string(projection), `"Label":"AMD device 1"`) || !strings.Contains(string(projection), `"Available":true`) {
		t.Fatalf("management projection lost safe device state: %s", projection)
	}
}

func TestManagedHardwareInventoryIDsRemainStableAcrossStartupOrder(t *testing.T) {
	inspect := func(string) (managedHardwareIdentity, string) { return managedHardwareTestIdentity(20), "" }
	first := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{
		AllowedAMDDevices: [8]string{"/dev/dri/renderD255", "/dev/dri/renderD128"}, AllowedAMDDeviceCount: 2}, inspect)
	second := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{
		AllowedAMDDevices: [8]string{"/dev/dri/renderD128", "/dev/dri/renderD255"}, AllowedAMDDeviceCount: 2}, inspect)
	if !slices.Equal(first.authorizedDeviceIDs(), second.authorizedDeviceIDs()) || first.authorizedDeviceIDs()[0] == first.authorizedDeviceIDs()[1] {
		t.Fatal("opaque device IDs depend on list ordering or are not distinct")
	}
	for _, id := range first.authorizedDeviceIDs() {
		if !strings.HasPrefix(id, "amd-") || len(id) != 36 {
			t.Fatalf("unexpected opaque ID format: %q", id)
		}
	}
}

func TestManagedHardwareInventoryRejectsChangedStartupIdentity(t *testing.T) {
	for name, change := range map[string]func(*managedHardwareIdentity){
		"node inode":     func(identity *managedHardwareIdentity) { identity.Node.Inode++ },
		"node device":    func(identity *managedHardwareIdentity) { identity.Node.Device++ },
		"node rdev":      func(identity *managedHardwareIdentity) { identity.Node.Special++ },
		"node ctime":     func(identity *managedHardwareIdentity) { identity.Node.ChangeNanoseconds++ },
		"system device":  func(identity *managedHardwareIdentity) { identity.SystemDevice.Inode++ },
		"system binding": func(identity *managedHardwareIdentity) { identity.SystemPath += "-replaced" },
		"vendor file":    func(identity *managedHardwareIdentity) { identity.VendorFile.Inode++ },
		"vendor":         func(identity *managedHardwareIdentity) { identity.Vendor = "0x8086" },
	} {
		t.Run(name, func(t *testing.T) {
			identity := managedHardwareTestIdentity(20)
			inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{Hardware: transcode.Hardware{Encode: "vaapi"}},
				func(string) (managedHardwareIdentity, string) { return identity, "" })
			selection := inventory.defaultSelection()
			hardware, available, _ := inventory.resolve(selection)
			if !available {
				t.Fatal("unchanged startup identity rejected")
			}
			change(&identity)
			rejected, available, code := inventory.resolve(selection)
			if available || code != managedHardwareIdentityChanged || rejected.Device != "" || rejected.Encode != "vaapi" {
				t.Fatalf("changed identity resolved: %+v, %v, %s", rejected, available, code)
			}
			if available, code := inventory.checkHardware(hardware); available || code != managedHardwareIdentityChanged {
				t.Fatalf("accepted plan reused changed hardware: %v, %s", available, code)
			}
			if devices := inventory.devices(); len(devices) != 1 || devices[0].Available || devices[0].Code != managedHardwareIdentityChanged {
				t.Fatalf("management state concealed replacement: %+v", devices)
			}
		})
	}
}

func TestManagedHardwareInventoryDoesNotAdoptDeviceMissingAtStartup(t *testing.T) {
	for _, startupCode := range []string{managedHardwareMissing, managedHardwareNotAMD, managedHardwareInvalid, managedHardwarePlatformMissing} {
		t.Run(startupCode, func(t *testing.T) {
			calls := 0
			inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{Hardware: transcode.Hardware{Decode: "vaapi"}},
				func(string) (managedHardwareIdentity, string) {
					calls++
					if calls == 1 {
						return managedHardwareIdentity{}, startupCode
					}
					return managedHardwareTestIdentity(20), ""
				})
			if len(inventory.authorizedDeviceIDs()) != 1 {
				t.Fatal("startup unavailability erased an explicitly authorized selection")
			}
			if available := inventory.availableDeviceIDs(); available == nil || len(available) != 0 || inventory.legacyDefault() {
				t.Fatal("unavailable AMD startup device used legacy availability defaults")
			}
			_, available, code := inventory.resolve(inventory.defaultSelection())
			if available || code != startupCode || inventory.devices()[0].Available || calls != 1 {
				t.Fatalf("newly appearing device adopted into old generation: %v, %s, observations=%d", available, code, calls)
			}
		})
	}
}

func TestManagedHardwareInventoryResolutionHasNoImplicitDeviceFallback(t *testing.T) {
	inspections := 0
	inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{
		AllowedAMDDevices: [8]string{"/dev/dri/renderD129"}, AllowedAMDDeviceCount: 1},
		func(string) (managedHardwareIdentity, string) {
			inspections++
			return managedHardwareTestIdentity(20), ""
		})
	for _, selection := range []settings.HardwareSelection{
		{Decode: "vaapi", Encode: "software"}, {Decode: "software", Encode: "vaapi", DeviceID: "unknown-id"},
		{Decode: "software", Encode: "software", DeviceID: "retired-id"},
	} {
		hardware, available, code := inventory.resolve(selection)
		if available || code != managedHardwareNotAuthorized || hardware.Device != "" {
			t.Fatalf("unknown selection used implicit default: %+v, %v, %s", hardware, available, code)
		}
	}
	if inspections != 1 {
		t.Fatalf("unknown selections inspected a device: %d", inspections)
	}
	if hardware, available, code := inventory.resolve(settings.HardwareSelection{}); !available || code != "" || hardware != (transcode.Hardware{Decode: "software", Encode: "software"}) {
		t.Fatalf("software-only selection needs no device: %+v, %v, %s", hardware, available, code)
	}
	selection := settings.HardwareSelection{Decode: "software", Encode: "software", DeviceID: inventory.authorizedDeviceIDs()[0]}
	hardware, available, code := inventory.resolve(selection)
	if !available || code != "" || hardware.Device != "/dev/dri/renderD129" {
		t.Fatalf("software codec selection lost its authorized Vulkan filter device: %+v, %v, %s", hardware, available, code)
	}
	if available, code := inventory.checkHardware(transcode.Hardware{Encode: "vaapi", Device: "/dev/dri/renderD128"}); available || code != managedHardwareNotAuthorized {
		t.Fatalf("unlisted concrete plan was authorized: %v, %s", available, code)
	}
}

func TestManagedHardwareInventoryPreservesOnlyExactLegacyStartupDefaults(t *testing.T) {
	for _, hardware := range []transcode.Hardware{{Encode: "nvenc", Device: "1"}, {Decode: "qsv", Encode: "qsv", Device: "/dev/dri/renderD129"}} {
		inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{Hardware: hardware},
			func(string) (managedHardwareIdentity, string) {
				t.Fatal("legacy hardware authorized an AMD inspection")
				return managedHardwareIdentity{}, ""
			})
		selection := inventory.defaultSelection()
		actual, available, code := inventory.resolve(selection)
		if !available || code != "" || actual != hardware || len(inventory.authorizedDeviceIDs()) != 0 || !inventory.legacyDefault() {
			t.Fatalf("legacy startup tuple changed: %+v, %v, %s", actual, available, code)
		}
		if available, code := inventory.checkHardware(hardware); !available || code != "" {
			t.Fatalf("exact legacy plan rejected: %v, %s", available, code)
		}
		selection.DeviceID = "arbitrary-id"
		if _, available, code := inventory.resolve(selection); available || code != managedHardwareUnsupported {
			t.Fatalf("legacy deployment allowed new managed input: %v, %s", available, code)
		}
	}
}

func TestManagedHardwareInventoryPreservesExplicitSoftwareFilterDevice(t *testing.T) {
	inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{
		Hardware: transcode.Hardware{Decode: "software", Encode: "software", Device: "/dev/dri/renderD129"}},
		func(device string) (managedHardwareIdentity, string) {
			if device != "/dev/dri/renderD129" {
				t.Fatalf("explicit filter device changed to %q", device)
			}
			return managedHardwareTestIdentity(20), ""
		})
	selection := inventory.defaultSelection()
	hardware, available, code := inventory.resolve(selection)
	if !available || code != "" || hardware.Device != "/dev/dri/renderD129" || selection.DeviceID == "" || inventory.legacyDefault() {
		t.Fatalf("explicit software filter device did not retain AMD authorization: %+v, %v, %s", hardware, available, code)
	}
	if ids := inventory.availableDeviceIDs(); ids == nil || !slices.Equal(ids, inventory.authorizedDeviceIDs()) {
		t.Fatalf("usable authorized device absent from explicit availability: %v", ids)
	}
	var absent *managedHardwareInventory
	if ids := absent.availableDeviceIDs(); ids == nil || len(ids) != 0 || absent.legacyDefault() {
		t.Fatal("absent inventory used unspecified availability or legacy defaults")
	}
}

func TestManagedHardwareInventoryInvalidDirectConfigurationFailsClosed(t *testing.T) {
	inventory := newManagedHardwareInventoryWithInspector(config.TranscodingConfig{
		AllowedAMDDevices: [8]string{"/private/arbitrary-device"}, AllowedAMDDeviceCount: 1},
		func(string) (managedHardwareIdentity, string) {
			t.Fatal("malformed configuration touched a device")
			return managedHardwareIdentity{}, ""
		})
	if len(inventory.devices()) != 0 || len(inventory.authorizedDeviceIDs()) != 0 {
		t.Fatal("invalid direct configuration retained authorization")
	}
}
