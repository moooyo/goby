//go:build !linux

package server

func inspectManagedAMDHardware(string) (managedHardwareIdentity, string) {
	return managedHardwareIdentity{}, managedHardwarePlatformMissing
}
