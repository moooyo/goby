//go:build !linux

package media

import (
	"fmt"
	"os"
)

// VideoSeekSourceIdentity requires Linux filesystem identity information.
func VideoSeekSourceIdentity(info os.FileInfo) (string, error) {
	return "", fmt.Errorf("video seek source identity requires Linux")
}
