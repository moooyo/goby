//go:build linux && !amd64

package commanddomain

// RunLauncher refuses Linux architectures without the implemented native kernel policy.
func RunLauncher() int { return 126 }
