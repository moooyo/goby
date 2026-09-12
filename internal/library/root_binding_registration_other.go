//go:build !linux

package library

// Unsupported platforms return the capability sentinel without kernel calls.
func rootBindingRegistrationUnsupportedKernelError(error) bool {
	return false
}
