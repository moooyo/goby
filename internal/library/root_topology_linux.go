//go:build linux

package library

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

func captureRootTopologyPlatform(ctx context.Context, lease *libraryRootLease, mapping RootTopologyMapping, heldRoot *os.Root) (_ *RootTopologyCapture, resultErr error) {
	// /proc/self describes the process leader. Keep this goroutine on one
	// thread and reject a thread whose actual namespace differs from it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	capture := &RootTopologyCapture{}
	defer func() {
		if resultErr != nil {
			_ = capture.Close()
		}
	}()
	namespace, witness, err := openRootTopologyNamespace()
	if err != nil {
		return nil, err
	}
	capture.namespace, capture.live.Namespace = namespace, witness
	entries, err := readRootTopologyMountInfo(ctx)
	if err != nil {
		return nil, err
	}
	for _, root := range []*os.Root{lease.approved, heldRoot} {
		point, err := retainRootTopologyPoint(ctx, root)
		if err != nil {
			return nil, err
		}
		capture.held = append(capture.held, point)
	}
	plan, err := planRootMounts(entries, mapping, capture.held[0].observation.Live.MountID, capture.held[1].observation.Live.MountID)
	if err != nil {
		return nil, err
	}
	if err := checkRootTopologyMountRecord(plan.Anchor, capture.held[0].observation); err != nil {
		return nil, err
	}
	if err := checkRootTopologyMountRecord(plan.Root, capture.held[1].observation); err != nil {
		return nil, err
	}
	anchor, root, err := openRootTopologyNames(ctx, mapping)
	if err != nil {
		return nil, err
	}
	defer anchor.Close()
	defer root.Close()
	for index, named := range []*os.Root{anchor, root} {
		point, err := retainRootTopologyPoint(ctx, named)
		if err != nil {
			return nil, err
		}
		err = compareRootTopologyPoint(capture.held[index].observation, point.observation)
		_ = point.file.Close()
		if err != nil {
			return nil, err
		}
	}
	capture.snapshot = RootTopologySnapshot{Version: RootTopologyVersion, Mapping: mapping,
		Anchor: capture.held[0].observation.Identity, RegisteredRoot: capture.held[1].observation.Identity,
		Boundaries: []RootTopologyBoundary{}}
	capture.live.Anchor, capture.live.RegisteredRoot = capture.held[0].observation.Live, capture.held[1].observation.Live
	capture.live.Boundaries = []RootTopologyLiveBoundary{}
	for _, entry := range plan.Nested {
		relative := strings.TrimPrefix(entry.MountPoint, strings.TrimSuffix(mapping.RegisteredPath, "/")+"/")
		opened, err := openRootTopologyRelative(ctx, root, relative)
		if err != nil {
			return nil, err
		}
		point, err := retainRootTopologyPoint(ctx, opened)
		_ = opened.Close()
		if err != nil {
			return nil, err
		}
		capture.held = append(capture.held, point)
		if err := checkRootTopologyMountRecord(entry, point.observation); err != nil {
			return nil, err
		}
		capture.snapshot.Boundaries = append(capture.snapshot.Boundaries, RootTopologyBoundary{
			RelativePath: strings.Clone(relative), Identity: point.observation.Identity})
		capture.live.Boundaries = append(capture.live.Boundaries, RootTopologyLiveBoundary{
			RelativePath: strings.Clone(relative), Witness: point.observation.Live})
	}
	capture.records = cloneRootTopologyRecords(plan.Records)
	if _, err := capture.snapshot.Fingerprint(); err != nil {
		return nil, err
	}
	if err := revalidateRootTopologyLockedThread(ctx, capture); err != nil {
		return nil, err
	}
	return capture, nil
}

func revalidateRootTopologyPlatform(ctx context.Context, capture *RootTopologyCapture) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return revalidateRootTopologyLockedThread(ctx, capture)
}

func revalidateRootTopologyLockedThread(ctx context.Context, capture *RootTopologyCapture) error {
	// Two bounded passes re-open names rather than relying on a first pass's
	// retained directory after a rename. Each pass is surrounded by mountinfo
	// and namespace checks. These are observations, not an atomic lock on the
	// external filesystem or a database deletion authorization.
	for pass := 0; pass < 2; pass++ {
		if err := checkRootTopologyNamespace(ctx, capture); err != nil {
			return err
		}
		before, err := readRootTopologyMountInfo(ctx)
		if err != nil {
			return err
		}
		plan, err := planRootMounts(before, capture.snapshot.Mapping, capture.live.Anchor.MountID, capture.live.RegisteredRoot.MountID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(plan.Records, capture.records) || len(plan.Nested) != len(capture.snapshot.Boundaries) ||
			len(capture.held) != len(plan.Nested)+2 {
			return ErrRootTopologyChanged
		}
		if err := verifyRootTopologyNames(ctx, capture, plan); err != nil {
			return err
		}
		after, err := readRootTopologyMountInfo(ctx)
		if err != nil {
			return err
		}
		afterPlan, err := planRootMounts(after, capture.snapshot.Mapping, capture.live.Anchor.MountID, capture.live.RegisteredRoot.MountID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(afterPlan.Records, capture.records) {
			return ErrRootTopologyChanged
		}
		if err := checkRootTopologyNamespace(ctx, capture); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func verifyRootTopologyNames(ctx context.Context, capture *RootTopologyCapture, plan rootMountPlan) error {
	anchor, root, err := openRootTopologyNames(ctx, capture.snapshot.Mapping)
	if err != nil {
		return err
	}
	defer anchor.Close()
	defer root.Close()
	for index := range capture.held {
		if err := ctx.Err(); err != nil {
			return err
		}
		var named *os.Root
		var record rootMountInfo
		switch index {
		case 0:
			named, record = anchor, plan.Anchor
		case 1:
			named, record = root, plan.Root
		default:
			record = plan.Nested[index-2]
			relative := capture.snapshot.Boundaries[index-2].RelativePath
			if record.MountPoint != strings.TrimSuffix(capture.snapshot.Mapping.RegisteredPath, "/")+"/"+relative {
				return ErrRootTopologyChanged
			}
			named, err = openRootTopologyRelative(ctx, root, relative)
			if err != nil {
				return err
			}
		}
		current, err := retainRootTopologyPoint(ctx, named)
		if index >= 2 {
			_ = named.Close()
		}
		if err != nil {
			return err
		}
		err = compareRootTopologyPoint(capture.held[index].observation, current.observation)
		if err == nil {
			err = checkRootTopologyMountRecord(record, current.observation)
		}
		_ = current.file.Close()
		if err != nil {
			return err
		}
		held, err := ObserveRootStorageIdentity(capture.held[index].file)
		if err != nil {
			return fmt.Errorf("held topology directory: %w: %w", ErrRootTopologyUnavailable, err)
		}
		if err := compareRootTopologyPoint(capture.held[index].observation, held); err != nil {
			return err
		}
	}
	return nil
}

func compareRootTopologyPoint(expected, actual RootStorageObservation) error {
	if !expected.Identity.Equal(actual.Identity) || expected.Live != actual.Live {
		return ErrRootTopologyChanged
	}
	return nil
}

func checkRootTopologyMountRecord(record rootMountInfo, observation RootStorageObservation) error {
	if record.ID != observation.Live.MountID || record.Major != unix.Major(observation.Live.Device) ||
		record.Minor != unix.Minor(observation.Live.Device) {
		return ErrRootTopologyAmbiguous
	}
	return nil
}

func retainRootTopologyPoint(ctx context.Context, root *os.Root) (rootTopologyHeldPoint, error) {
	if err := ctx.Err(); err != nil {
		return rootTopologyHeldPoint{}, err
	}
	file, err := root.Open(".")
	if err != nil {
		return rootTopologyHeldPoint{}, fmt.Errorf("retain topology directory: %w", ErrRootTopologyUnavailable)
	}
	observation, err := ObserveRootStorageIdentity(file)
	if err != nil {
		_ = file.Close()
		return rootTopologyHeldPoint{}, fmt.Errorf("observe topology directory: %w: %w", ErrRootTopologyUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return rootTopologyHeldPoint{}, err
	}
	return rootTopologyHeldPoint{file: file, observation: observation}, nil
}

func openRootTopologyNames(ctx context.Context, mapping RootTopologyMapping) (*os.Root, *os.Root, error) {
	relative, err := rootTopologyRelative(mapping)
	if err != nil {
		return nil, nil, err
	}
	slash, err := os.OpenRoot("/")
	if err != nil {
		return nil, nil, fmt.Errorf("open namespace root: %w", ErrRootTopologyUnavailable)
	}
	defer slash.Close()
	anchorName := strings.TrimPrefix(mapping.ApprovedPath, "/")
	if anchorName == "" {
		anchorName = "."
	}
	anchor, err := openRootTopologyRelative(ctx, slash, anchorName)
	if err != nil {
		return nil, nil, err
	}
	root, err := openRootTopologyRelative(ctx, anchor, relative)
	if err != nil {
		_ = anchor.Close()
		return nil, nil, err
	}
	return anchor, root, nil
}

func openRootTopologyRelative(ctx context.Context, anchor *os.Root, relative string) (*os.Root, error) {
	if !validRootTopologyRelative(relative) {
		return nil, ErrInvalidRootTopology
	}
	parts := strings.Split(relative, "/")
	if len(parts) > maxRootTopologyComponents {
		return nil, ErrRootTopologyLimit
	}
	current, err := anchor.OpenRoot(".")
	if err != nil {
		return nil, fmt.Errorf("open topology anchor: %w", ErrRootTopologyUnavailable)
	}
	for _, component := range parts {
		if err := ctx.Err(); err != nil {
			_ = current.Close()
			return nil, err
		}
		before, err := current.Lstat(component)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			_ = current.Close()
			return nil, fmt.Errorf("topology name is unavailable or is not a nonsymlink directory: %w", ErrRootTopologyUnavailable)
		}
		next, err := current.OpenRoot(component)
		_ = current.Close()
		if err != nil {
			return nil, fmt.Errorf("open topology name: %w", ErrRootTopologyUnavailable)
		}
		after, err := next.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			_ = next.Close()
			return nil, ErrRootTopologyChanged
		}
		current = next
	}
	return current, nil
}

func readRootTopologyMountInfo(ctx context.Context) ([]rootMountInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("read mountinfo: %w", ErrRootTopologyUnavailable)
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxRootMountInfoBytes+1))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return nil, fmt.Errorf("read mountinfo: %w", ErrRootTopologyUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return parseRootMountInfo(raw)
}

func rootTopologyNamespaceStat(file *os.File) (RootMountNamespaceWitness, error) {
	connection, err := file.SyscallConn()
	if err != nil {
		return RootMountNamespaceWitness{}, ErrRootTopologyUnavailable
	}
	var value unix.Stat_t
	var statErr error
	err = connection.Control(func(fd uintptr) { statErr = unix.Fstat(int(fd), &value) })
	if err != nil || statErr != nil || value.Ino == 0 {
		return RootMountNamespaceWitness{}, ErrRootTopologyUnavailable
	}
	return RootMountNamespaceWitness{Device: uint64(value.Dev), Inode: uint64(value.Ino)}, nil
}

func openRootTopologyNamespace() (*os.File, RootMountNamespaceWitness, error) {
	file, err := os.Open("/proc/self/ns/mnt")
	if err != nil {
		return nil, RootMountNamespaceWitness{}, ErrRootTopologyUnavailable
	}
	witness, err := rootTopologyNamespaceStat(file)
	if err == nil {
		var thread *os.File
		thread, err = os.Open("/proc/thread-self/ns/mnt")
		if err == nil {
			var current RootMountNamespaceWitness
			current, err = rootTopologyNamespaceStat(thread)
			_ = thread.Close()
			if err == nil && current != witness {
				err = ErrRootTopologyChanged
			}
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, RootMountNamespaceWitness{}, fmt.Errorf("mount namespace observation: %w: %w", ErrRootTopologyUnavailable, err)
	}
	return file, witness, nil
}

func checkRootTopologyNamespace(ctx context.Context, capture *RootTopologyCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if capture.namespace == nil {
		return ErrRootTopologyUnavailable
	}
	file, current, err := openRootTopologyNamespace()
	if err != nil {
		return err
	}
	_ = file.Close()
	held, err := rootTopologyNamespaceStat(capture.namespace)
	if err != nil {
		return err
	}
	if held != capture.live.Namespace || current != held {
		return ErrRootTopologyChanged
	}
	return nil
}

func cloneRootTopologyRecords(records []rootMountInfo) []rootMountInfo {
	result := append([]rootMountInfo{}, records...)
	for index := range result {
		row := &result[index]
		row.Root, row.MountPoint = strings.Clone(row.Root), strings.Clone(row.MountPoint)
		row.FilesystemType, row.Source = strings.Clone(row.FilesystemType), strings.Clone(row.Source)
		for _, field := range []*[]string{&row.Options, &row.OptionalFields, &row.SuperOptions} {
			if *field == nil {
				continue
			}
			values := make([]string, len(*field))
			for index, value := range *field {
				values[index] = strings.Clone(value)
			}
			*field = values
		}
	}
	return result
}
