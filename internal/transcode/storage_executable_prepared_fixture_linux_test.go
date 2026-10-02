//go:build linux

package transcode

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// This adapter is only the trusted root preparer for the explicitly enabled
// kernel fixture. Its immutable setup receipt and live PID bind the approved
// executable input; the production pooled factory supplies the actual issuer.
func fixedPoolPreparedFixtureExecutable(t *testing.T, pool fixedBackingPoolConfig, token, configSHA string) (*os.File, string, string) {
	t.Helper()
	root := filepath.Dir(pool.PoolRoot)
	receipt, err := fixedVolumeOpenAbsolute(filepath.Join(root, "setup-receipt.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer receipt.Close()
	var receiptStat unix.Stat_t
	if err = fixedVolumeOwnedRegular(receipt, &receiptStat); err != nil || receiptStat.Mode&0o7777 != 0o400 || receiptStat.Size != 16<<10 {
		t.Fatal("trusted executable preparation requires its exact private setup receipt")
	}
	flags, err := unix.IoctlGetInt(int(receipt.Fd()), unix.FS_IOC_GETFLAGS)
	if err != nil || flags&fixedVolumeImmutableFlag == 0 {
		t.Fatal("setup receipt is not immutable")
	}
	var document struct {
		Root         string
		Token        string
		Schema       string
		Stage        string
		ConfigSHA256 string
		Domain       struct {
			BootID                                    string
			MountNamespaceDevice, MountNamespaceInode uint64
		}
		ActiveCommand struct {
			Name      string
			PID       int
			StartTime uint64
			Joined    bool
		}
		FileSHA256 map[string]string
		Objects    map[string]struct {
			Device, Inode  uint64
			AllocatedBytes int64
		}
	}
	decoder := json.NewDecoder(io.LimitReader(receipt, (16<<10)+1))
	if err = decoder.Decode(&document); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatal("setup receipt has trailing data")
	}
	self, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Fatal(err)
	}
	end := strings.LastIndexByte(string(self), ')')
	if end < 0 {
		t.Fatal("actual root fixture process identity is unavailable")
	}
	fields := strings.Fields(string(self[end+1:]))
	if len(fields) < 20 {
		t.Fatal("actual root fixture process identity is incomplete")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if document.Schema != "goby-fixed-pool-fixture-v3" || document.Root != root || document.Token != token ||
		document.Stage != "fixture-running" || document.ConfigSHA256 != configSHA || document.Domain.BootID != pool.Expected.BootID ||
		document.Domain.MountNamespaceDevice != pool.Expected.MountNamespaceDevice || document.Domain.MountNamespaceInode != pool.Expected.MountNamespaceInode ||
		document.ActiveCommand.Name != "prepared-fixture" || document.ActiveCommand.PID != os.Getpid() || document.ActiveCommand.Joined ||
		document.ActiveCommand.StartTime != start {
		t.Fatal("immutable executable approval does not belong to this live root preparation")
	}
	path := filepath.Join(root, "test-binary")
	digest := document.FileSHA256["test-binary"]
	file, err := fixedVolumeOpenAbsolute(path, false)
	if err != nil {
		t.Fatal(err)
	}
	stat, allocated, err := fixedExecutableFile(file, digest)
	object, listed := document.Objects["test-binary"]
	if err != nil || !listed || object.Device != uint64(stat.Dev) || object.Inode != stat.Ino || object.AllocatedBytes != allocated {
		_ = file.Close()
		t.Fatal("live approved binary differs from its actual preparation inventory")
	}
	return file, path, digest
}
