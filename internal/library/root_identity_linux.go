//go:build linux

package library

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux asm-generic ioctl ABI, used by amd64 and arm64. The current target's
// UAPI observation verified sizeof(fsuuid2)=17 and offsetof(uuid)=1 on amd64.
// Other Linux ioctl ABIs remain explicitly unsupported by this adapter.
const rootFSUUIDRequest = uintptr(0x80111500)

type rootFSUUID2 struct {
	Length uint8
	UUID   [16]byte
}

// name_to_handle_at receives one fixed Linux file_handle buffer. Unlike the
// x/sys convenience wrapper, this adapter never allocates a size reported by
// the kernel or retries with an unbounded buffer after EOVERFLOW.
type rootDirectoryHandle struct {
	Bytes  uint32
	Type   int32
	Handle [MaxRootStorageHandleBytes]byte
}

// ObserveRootStorageIdentity observes only the already opened directory. The
// caller owns its lifetime and safe opening; this function neither closes nor
// reopens it by name. RawConn.Control pins the descriptor across all syscalls.
// No fallback device/inode identity or root/deletion authorization is produced.
func ObserveRootStorageIdentity(directory *os.File) (RootStorageObservation, error) {
	if directory == nil {
		return RootStorageObservation{}, ErrInvalidRootStorageIdentity
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return RootStorageObservation{}, ErrRootStorageIdentityUnsupported
	}
	connection, err := directory.SyscallConn()
	if err != nil {
		return RootStorageObservation{}, rootStorageOperationError("directory descriptor", err)
	}
	var result RootStorageObservation
	var observationErr error
	err = connection.Control(func(descriptor uintptr) {
		result, observationErr = observeRootStorageDescriptor(int(descriptor))
	})
	if err != nil {
		return RootStorageObservation{}, rootStorageOperationError("directory descriptor", err)
	}
	if observationErr != nil {
		return RootStorageObservation{}, observationErr
	}
	return result, nil
}

func observeRootStorageDescriptor(descriptor int) (RootStorageObservation, error) {
	var before, after unix.Stat_t
	if err := unix.Fstat(descriptor, &before); err != nil {
		return RootStorageObservation{}, rootStorageOperationError("directory stat", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return RootStorageObservation{}, ErrInvalidRootStorageIdentity
	}
	first, err := rootStorageKernelIdentity(descriptor)
	if err != nil {
		return RootStorageObservation{}, err
	}
	second, err := rootStorageKernelIdentity(descriptor)
	if err != nil {
		return RootStorageObservation{}, err
	}
	if err := unix.Fstat(descriptor, &after); err != nil {
		return RootStorageObservation{}, rootStorageOperationError("directory stat", err)
	}
	// Content modifications may legitimately change timestamps, size and link
	// counts. Only the open object's identity and mount witness must agree.
	if after.Mode&unix.S_IFMT != unix.S_IFDIR || before.Dev != after.Dev || before.Ino != after.Ino ||
		!first.Identity.Equal(second.Identity) || first.Live.MountID != second.Live.MountID {
		return RootStorageObservation{}, fmt.Errorf("directory identity changed during observation: %w", ErrRootStorageIdentityUnavailable)
	}
	first.Live.Device, first.Live.Inode = uint64(before.Dev), uint64(before.Ino)
	return first, nil
}

func rootStorageKernelIdentity(descriptor int) (RootStorageObservation, error) {
	var uuid rootFSUUID2
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(descriptor), rootFSUUIDRequest, uintptr(unsafe.Pointer(&uuid)))
	if errno != 0 {
		return RootStorageObservation{}, rootStorageOperationError("filesystem UUID", errno)
	}
	if err := rootStorageUUIDError(uuid); err != nil {
		return RootStorageObservation{}, err
	}
	var handle rootDirectoryHandle
	handle.Bytes = MaxRootStorageHandleBytes
	var mountID int32
	empty := [1]byte{}
	_, _, errno = unix.Syscall6(unix.SYS_NAME_TO_HANDLE_AT, uintptr(descriptor), uintptr(unsafe.Pointer(&empty[0])),
		uintptr(unsafe.Pointer(&handle)), uintptr(unsafe.Pointer(&mountID)), uintptr(unix.AT_EMPTY_PATH), 0)
	if errno == unix.EOVERFLOW {
		return RootStorageObservation{}, rootStorageOperationError("directory handle exceeds the fixed profile", errno)
	}
	if errno != 0 {
		return RootStorageObservation{}, rootStorageOperationError("directory handle", errno)
	}
	return rootStorageObservationFromKernel(uuid, handle, mountID)
}

func rootStorageObservationFromKernel(uuid rootFSUUID2, handle rootDirectoryHandle, mountID int32) (RootStorageObservation, error) {
	if err := rootStorageUUIDError(uuid); err != nil {
		return RootStorageObservation{}, err
	}
	if handle.Bytes > MaxRootStorageHandleBytes {
		return RootStorageObservation{}, fmt.Errorf("directory handle exceeds the fixed profile: %w", ErrRootStorageIdentityUnsupported)
	}
	if handle.Bytes == 0 || mountID <= 0 {
		return RootStorageObservation{}, fmt.Errorf("incomplete kernel directory identity: %w", ErrRootStorageIdentityUnavailable)
	}
	identity := RootStorageIdentity{Version: RootStorageIdentityVersion, Profile: RootStorageIdentityProfile,
		FilesystemUUID: hex.EncodeToString(uuid.UUID[:]), HandleType: handle.Type,
		Handle: append([]byte(nil), handle.Handle[:handle.Bytes]...)}
	if err := identity.Validate(); err != nil {
		return RootStorageObservation{}, fmt.Errorf("invalid kernel directory identity: %w: %w", ErrRootStorageIdentityUnavailable, err)
	}
	return RootStorageObservation{Identity: identity, Live: RootStorageWitness{MountID: int(mountID)}}, nil
}

func rootStorageUUIDError(uuid rootFSUUID2) error {
	if uuid.Length == 0 || uuid.Length > 16 {
		return fmt.Errorf("incomplete kernel filesystem UUID: %w", ErrRootStorageIdentityUnavailable)
	}
	if uuid.Length != 16 {
		// A valid shorter UAPI UUID cannot be expressed by this exact profile.
		// Do not pad it into a different filesystem identity.
		return fmt.Errorf("filesystem UUID length is outside this profile: %w", ErrRootStorageIdentityUnsupported)
	}
	if uuid.UUID == ([16]byte{}) {
		return fmt.Errorf("empty kernel filesystem UUID: %w: %w", ErrRootStorageIdentityUnavailable, ErrInvalidRootStorageIdentity)
	}
	return nil
}

func rootStorageOperationError(operation string, err error) error {
	classification := ErrRootStorageIdentityUnavailable
	if errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOVERFLOW) {
		classification = ErrRootStorageIdentityUnsupported
	}
	return fmt.Errorf("%s: %w: %w", operation, classification, err)
}
