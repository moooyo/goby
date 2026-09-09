package media

import (
	"crypto/sha256"
	"fmt"
	"os"
	"syscall"
)

// VideoSeekSourceIdentity fingerprints a held regular filesystem object. A
// pathname alone cannot preserve this identity through replacement or mutation.
func VideoSeekSourceIdentity(info os.FileInfo) (string, error) {
	if info == nil || !info.Mode().IsRegular() || info.Size() < 0 || FileChangeTime(info) == 0 {
		return "", fmt.Errorf("video seek source identity is unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return "", fmt.Errorf("video seek source identity is unavailable")
	}
	identity := fmt.Sprintf("video-seek-source-v1:%d:%d:%d:%d:%d:%d:%d:%d",
		stat.Dev, stat.Ino, stat.Mode, info.Size(), stat.Mtim.Sec, stat.Mtim.Nsec, stat.Ctim.Sec, stat.Ctim.Nsec)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity))), nil
}
