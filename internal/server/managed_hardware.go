package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	managedHardwareUnavailable     = "hardware_device_unavailable"
	managedHardwareMissing         = "hardware_device_missing"
	managedHardwareInvalid         = "hardware_device_invalid"
	managedHardwareNotAMD          = "hardware_device_not_amd"
	managedHardwareIdentityChanged = "hardware_device_identity_changed"
	managedHardwareNotAuthorized   = "hardware_device_not_authorized"
	managedHardwareUnsupported     = "hardware_selection_unsupported"
	managedHardwarePlatformMissing = "hardware_device_unsupported_platform"
)

// managedHardwareDevice is the entire management projection. Device paths and
// filesystem identities remain private to the startup authorization generation.
type managedHardwareDevice struct {
	DeviceID  string `json:"DeviceID"`
	Label     string `json:"Label"`
	Available bool   `json:"Available"`
	Code      string `json:"Code,omitempty"`
}

type managedHardwareFileIdentity struct {
	Device            uint64
	Inode             uint64
	Special           uint64
	Mode              uint32
	ChangeSeconds     int64
	ChangeNanoseconds int64
}

type managedHardwareIdentity struct {
	Node         managedHardwareFileIdentity
	SystemDevice managedHardwareFileIdentity
	VendorFile   managedHardwareFileIdentity
	SystemPath   string
	Vendor       string
}

type managedHardwareEntry struct {
	id       string
	label    string
	path     string
	identity managedHardwareIdentity
	code     string
}

// Each inventory captures one immutable startup authorization generation.
// A missing or foreign device can become available only in a new generation;
// current observations never replace the startup identity or expand this list.
type managedHardwareInventory struct {
	entries  []managedHardwareEntry
	defaults settings.HardwareSelection
	legacy   transcode.Hardware
	inspect  func(string) (managedHardwareIdentity, string)
}

func newManagedHardwareInventory(cfg config.TranscodingConfig) *managedHardwareInventory {
	return newManagedHardwareInventoryWithInspector(cfg, inspectManagedAMDHardware)
}

func newManagedHardwareInventoryWithInspector(cfg config.TranscodingConfig, inspect func(string) (managedHardwareIdentity, string)) *managedHardwareInventory {
	if inspect == nil {
		inspect = inspectManagedAMDHardware
	}
	inventory := &managedHardwareInventory{inspect: inspect, defaults: settings.HardwareSelection{
		Decode: cfg.Hardware.Decode, Encode: cfg.Hardware.Encode,
	}}
	if inventory.defaults.Decode == "" {
		inventory.defaults.Decode = "software"
	}
	if inventory.defaults.Encode == "" {
		inventory.defaults.Encode = "software"
	}
	if inventory.defaults.Decode != "vaapi" && inventory.defaults.Encode != "vaapi" &&
		(inventory.defaults.Decode != "software" || inventory.defaults.Encode != "software") {
		// Existing non-AMD deployment defaults retain their original execution
		// tuple. They are not new managed inputs and authorize no AMD device.
		inventory.legacy = cfg.Hardware
	}
	paths, err := cfg.AuthorizedAMDDevices()
	if err != nil {
		// Configuration validation reports malformed authorization during normal
		// startup. A direct caller still fails closed without touching a path.
		return inventory
	}
	sort.Strings(paths)
	for index, device := range paths {
		identity, code := inspect(device)
		inventory.entries = append(inventory.entries, managedHardwareEntry{
			id: managedHardwareDeviceID(device), label: fmt.Sprintf("AMD device %d", index+1),
			path: device, identity: identity, code: code,
		})
	}
	if inventory.defaults.Decode == "vaapi" || inventory.defaults.Encode == "vaapi" ||
		inventory.defaults.Decode == "software" && inventory.defaults.Encode == "software" && cfg.Hardware.Device != "" {
		device := cfg.Hardware.Device
		if device == "" {
			device = "/dev/dri/renderD128"
		}
		inventory.defaults.DeviceID = managedHardwareDeviceID(device)
	}
	return inventory
}

func managedHardwareDeviceID(device string) string {
	digest := sha256.Sum256([]byte("goby:authorized-amd-device:v1\x00" + device))
	return "amd-" + hex.EncodeToString(digest[:16])
}

func (inventory *managedHardwareInventory) authorizedDeviceIDs() []string {
	ids := make([]string, 0)
	if inventory != nil {
		for _, entry := range inventory.entries {
			ids = append(ids, entry.id)
		}
	}
	return ids
}

// availableDeviceIDs deliberately returns a non-nil empty slice when no device
// is usable. Settings treats nil as unspecified availability for older callers.
func (inventory *managedHardwareInventory) availableDeviceIDs() []string {
	ids := make([]string, 0)
	if inventory != nil {
		for _, entry := range inventory.entries {
			if available, _ := inventory.checkEntry(entry); available {
				ids = append(ids, entry.id)
			}
		}
	}
	return ids
}

func (inventory *managedHardwareInventory) legacyDefault() bool {
	return inventory != nil && inventory.legacy != (transcode.Hardware{})
}

func (inventory *managedHardwareInventory) defaultSelection() settings.HardwareSelection {
	if inventory == nil {
		return settings.HardwareSelection{Decode: "software", Encode: "software"}
	}
	return inventory.defaults
}

func (inventory *managedHardwareInventory) devices() []managedHardwareDevice {
	devices := make([]managedHardwareDevice, 0)
	if inventory != nil {
		for _, entry := range inventory.entries {
			available, code := inventory.checkEntry(entry)
			devices = append(devices, managedHardwareDevice{DeviceID: entry.id, Label: entry.label, Available: available, Code: code})
		}
	}
	return devices
}

// resolve preserves the selected axes on rejection, but never supplies a device
// path for an unknown or replaced ID. A caller must gate hardware admission on
// the returned availability instead of treating an empty path as a default.
func (inventory *managedHardwareInventory) resolve(selection settings.HardwareSelection) (transcode.Hardware, bool, string) {
	hardware := transcode.Hardware{Decode: selection.Decode, Encode: selection.Encode}
	if hardware.Decode == "" {
		hardware.Decode = "software"
	}
	if hardware.Encode == "" {
		hardware.Encode = "software"
	}
	if inventory != nil && inventory.legacy != (transcode.Hardware{}) && selection == inventory.defaults {
		return inventory.legacy, true, ""
	}
	if hardware.Decode != "software" && hardware.Decode != "vaapi" || hardware.Encode != "software" && hardware.Encode != "vaapi" {
		return hardware, false, managedHardwareUnsupported
	}
	if selection.DeviceID == "" {
		if hardware.Decode == "software" && hardware.Encode == "software" {
			return hardware, true, ""
		}
		return hardware, false, managedHardwareNotAuthorized
	}
	if inventory != nil {
		for _, entry := range inventory.entries {
			if entry.id != selection.DeviceID {
				continue
			}
			available, code := inventory.checkEntry(entry)
			if !available {
				return hardware, false, code
			}
			// A software codec pair may still use this device for Vulkan filters.
			hardware.Device = entry.path
			return hardware, true, ""
		}
	}
	return hardware, false, managedHardwareNotAuthorized
}

// checkHardware rechecks a concrete accepted plan against this generation. It
// does not consult current settings, so a later selection cannot retarget a job.
func (inventory *managedHardwareInventory) checkHardware(hardware transcode.Hardware) (bool, string) {
	if inventory != nil && inventory.legacy != (transcode.Hardware{}) && hardware == inventory.legacy {
		return true, ""
	}
	if hardware.Decode != "" && hardware.Decode != "software" && hardware.Decode != "vaapi" ||
		hardware.Encode != "" && hardware.Encode != "software" && hardware.Encode != "vaapi" {
		return false, managedHardwareUnsupported
	}
	if hardware.Device == "" {
		if (hardware.Decode == "" || hardware.Decode == "software") && (hardware.Encode == "" || hardware.Encode == "software") {
			return true, ""
		}
		return false, managedHardwareNotAuthorized
	}
	if inventory != nil {
		for _, entry := range inventory.entries {
			if entry.path == hardware.Device {
				return inventory.checkEntry(entry)
			}
		}
	}
	return false, managedHardwareNotAuthorized
}

func (inventory *managedHardwareInventory) checkEntry(entry managedHardwareEntry) (bool, string) {
	if entry.code != "" {
		return false, entry.code
	}
	identity, code := inventory.inspect(entry.path)
	if code != "" {
		return false, code
	}
	if identity != entry.identity {
		return false, managedHardwareIdentityChanged
	}
	return true, ""
}
