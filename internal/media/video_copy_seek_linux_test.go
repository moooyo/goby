//go:build linux

package media

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func videoCopySeekBoundCandidate(t *testing.T, file *os.File, executable string, candidate VideoCopySeekCandidate) string {
	t.Helper()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	candidate.Index.SourceIdentity, err = VideoSeekSourceIdentity(info)
	if err != nil {
		t.Fatal(err)
	}
	_, candidate.Index.ToolIdentity, err = VideoSeekToolIdentity(context.Background(), executable)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestVideoCopySeekFreshProofBorrowsDescriptorAndChecksBothIdentities(t *testing.T) {
	file, _ := videoSeekLinuxTestSource(t)
	_, candidate := videoCopySeekTestCandidate(t)
	executable := videoSeekLinuxTestTool(t, "dd if=/proc/self/fd/3 of=/dev/null bs=1 count=1 2>/dev/null", videoCopySeekTestHash(candidate))
	encoded := videoCopySeekBoundCandidate(t, file, executable, candidate)
	t.Setenv("GOBY_DATABASE_URL", "must-not-reach-copy-proof")
	t.Setenv("FFREPORT", "file=must-not-write-copy-report")
	t.Setenv("LD_PRELOAD", "/must-not-load-untrusted-library")
	verification, err := VerifyVideoCopySeekCandidate(context.Background(), executable, file, encoded, 2)
	if err != nil || !verification.Verified || verification.InputSeekTicks != candidate.RequestedStartTicks {
		t.Fatalf("fresh exact copy proof failed: %+v, %v", verification, err)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 7 {
		t.Fatalf("copy proof moved the borrowed file: %d, %v", position, err)
	}
	otherTool := videoSeekLinuxTestTool(t, "true", videoCopySeekTestHash(candidate))
	verification, err = VerifyVideoCopySeekCandidate(context.Background(), otherTool, file, encoded, 1)
	if err != nil || verification.Verified {
		t.Fatalf("changed executable was accepted: %+v, %v", verification, err)
	}
	if _, err := file.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	verification, err = VerifyVideoCopySeekCandidate(context.Background(), executable, file, encoded, 1)
	if err != nil || verification.Verified {
		t.Fatalf("changed source was accepted: %+v, %v", verification, err)
	}
}

func TestVideoCopySeekFailedPacketProofNeverVerifies(t *testing.T) {
	_, candidate := videoCopySeekTestCandidate(t)
	for name, fixture := range map[string]struct{ prefix, output string }{
		"wrong first IDR":  {output: strings.Replace(videoCopySeekTestHash(candidate), candidate.Index.Entries[0].CodedSHA256, strings.Repeat("a", 64), 1)},
		"error diagnostic": {prefix: "printf '[error] controlled copy failure\\n' >&2", output: videoCopySeekTestHash(candidate)},
		"source mutation":  {prefix: "printf x >> /proc/self/fd/3", output: videoCopySeekTestHash(candidate)},
	} {
		t.Run(name, func(t *testing.T) {
			file, _ := videoSeekLinuxTestSource(t)
			executable := videoSeekLinuxTestTool(t, fixture.prefix, fixture.output)
			encoded := videoCopySeekBoundCandidate(t, file, executable, candidate)
			verification, err := VerifyVideoCopySeekCandidate(context.Background(), executable, file, encoded, 1)
			if verification.Verified || name == "source mutation" && err == nil || name != "source mutation" && err != nil {
				t.Fatalf("failed copy proof returned %+v, %v", verification, err)
			}
		})
	}
}
