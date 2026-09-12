//go:build linux

package library

import "golang.org/x/sys/unix"

// These are the exact kernel failures classified as unsupported by the current
// identity adapter. Other syscall failures must not become unbound approvals.
func rootBindingRegistrationUnsupportedKernelError(err error) bool {
	return err == unix.ENOTTY || err == unix.EOPNOTSUPP || err == unix.ENOSYS || err == unix.EOVERFLOW
}
