//go:build !linux

package media

import (
	"fmt"
	"os"
)

func openLocalMedia(string) (*os.File, error) {
	return nil, fmt.Errorf("media probing requires Linux")
}
