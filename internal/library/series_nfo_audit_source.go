package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/primaryio"
)

type seriesNFOAuditObservation struct {
	claim           seriesNFOAuditClaim
	relative, leaf  string
	directory, file os.FileInfo
}

type seriesNFOAuditRootPlan struct {
	root        libraryRoot
	revision    int64
	fingerprint string
}

type seriesNFOAuditRootWitness struct {
	anchor, registered os.FileInfo
}

type seriesNFOAuditRootEvidence struct {
	plan         seriesNFOAuditRootPlan
	witness      seriesNFOAuditRootWitness
	observations []seriesNFOAuditObservation
	remaining    int64
}

func (s *Store) observeSeriesNFOAudit(ctx context.Context, actor identity.Principal, libraryID string, snapshot seriesNFOAuditSnapshot,
	retired *<-chan struct{}) (_ []seriesNFOAuditClaim, resultErr error) {
	administrator := &catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	type rootGroup struct {
		plan    seriesNFOAuditRootPlan
		targets []seriesNFOAuditTarget
	}
	var groups []rootGroup
	var roots []mediaSourceRootHint
	targets := snapshot.targets
	for len(targets) != 0 {
		count := 1
		for count < len(targets) && targets[count].rootID == targets[0].rootID {
			count++
		}
		var candidates []seriesNFOAuditTarget
		for _, target := range targets[:count] {
			if target.accepted() {
				candidates = append(candidates, target)
			}
		}
		targets = targets[count:]
		if len(candidates) == 0 {
			continue
		}
		first := candidates[0]
		plan, err := s.planSeriesNFOAuditRoot(ctx, administrator, libraryID, first.rootID, first.bindingRevision, snapshot.finishedAt)
		if err != nil {
			return nil, err
		}
		groups = append(groups, rootGroup{plan: plan, targets: candidates})
		roots = append(roots, mediaSourceRootHint{root: plan.root, bindingRevision: plan.revision})
	}
	if len(groups) == 0 {
		return nil, nil
	}
	operation, err := s.preparePrimaryRootIO(ctx, roots)
	if err != nil {
		return nil, err
	}
	*retired = operation.handle.state.done
	defer func() {
		if err := operation.Close(); err != nil {
			resultErr = errors.Join(resultErr, ErrUnavailable, err)
		}
	}()
	var evidence []seriesNFOAuditRootEvidence
	remaining := int64(maxSeriesNFOAuditBytes)
	for _, group := range groups {
		result, err := runSeriesNFOAuditRoot(ctx, operation, group.plan, func(work context.Context, root *os.Root, witness seriesNFOAuditRootWitness) (seriesNFOAuditRootEvidence, error) {
			return readSeriesNFOAuditCandidates(work, root, group.plan, witness, group.targets, remaining)
		})
		if err != nil {
			return nil, err
		}
		remaining = result.remaining
		if len(result.observations) != 0 {
			evidence = append(evidence, result)
		}
	}
	// A peer can live in another registered root. Reopen every contributing
	// directory and sidecar after collecting all of them, before classification.
	var claims []seriesNFOAuditClaim
	for _, result := range evidence {
		plan, err := s.planSeriesNFOAuditRoot(ctx, administrator, libraryID, result.plan.root.id, result.plan.revision, snapshot.finishedAt)
		if err != nil {
			return nil, err
		}
		if plan != result.plan {
			return nil, errSeriesNFOAuditRoot
		}
		verified, err := runSeriesNFOAuditRoot(ctx, operation, plan, result.verify)
		if err != nil {
			return nil, err
		}
		claims = append(claims, verified...)
	}
	return claims, nil
}

func (s *Store) planSeriesNFOAuditRoot(ctx context.Context, administrator *catalogAdministrator, libraryID, rootID string,
	revision int64, finishedAt time.Time) (seriesNFOAuditRootPlan, error) {
	row, err := s.readRootBinding(ctx, administrator, libraryID, rootID)
	if errors.Is(err, ErrNotFound) {
		return seriesNFOAuditRootPlan{}, errSeriesNFOAuditRoot
	}
	if err != nil {
		return seriesNFOAuditRootPlan{}, err
	}
	approved, err := row.validate()
	if err != nil || row.revision != revision || row.boundAt != nil && row.boundAt.After(finishedAt) {
		return seriesNFOAuditRootPlan{}, errSeriesNFOAuditRoot
	}
	if _, err := s.configuredRootBindingPath(row.root); err != nil {
		return seriesNFOAuditRootPlan{}, err
	}
	plan := seriesNFOAuditRootPlan{root: row.root, revision: row.revision}
	if approved != nil {
		plan.fingerprint, err = approved.Fingerprint()
		if err != nil {
			return seriesNFOAuditRootPlan{}, errSeriesNFOAuditRoot
		}
	}
	return plan, nil
}

func runSeriesNFOAuditRoot[T any](ctx context.Context, operation *PrimaryRootIO, plan seriesNFOAuditRootPlan,
	work func(context.Context, *os.Root, seriesNFOAuditRootWitness) (T, error)) (T, error) {
	var zero T
	results := make(chan T, 1)
	err := operation.RunImmediate(ctx, plan.root.id, primaryio.Background, func(phase context.Context) error {
		return runStorageObservation(phase, nil, func(admitted context.Context) error {
			result, err := withSeriesNFOAuditRoot(admitted, plan, work)
			if err == nil {
				results <- result
			}
			return err
		})
	})
	if err != nil {
		var retirement *storageObservationRetirementFailure
		if errors.As(err, &retirement) || errors.Is(err, errSidecarRetirementUnknown) || errors.Is(err, errStorageObservationUnavailable) {
			return zero, errors.Join(ErrUnavailable, err)
		}
		return zero, err
	}
	return <-results, nil
}

// The read-only worker uses fresh names, not a cached scan anchor. If a root
// has a stored topology approval, it must match on the exact handle we use.
func withSeriesNFOAuditRoot[T any](ctx context.Context, plan seriesNFOAuditRootPlan,
	work func(context.Context, *os.Root, seriesNFOAuditRootWitness) (T, error)) (result T, resultErr error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	mapping := RootTopologyMapping{ApprovedPath: plan.root.allowedPath, RegisteredPath: plan.root.path}
	anchor, registered, err := openRootTopologyNames(ctx, mapping)
	if err != nil {
		return result, errors.Join(errSeriesNFOAuditRoot, ctx.Err())
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, registered), closeDirectoryPrimaryRoot(ctx, anchor))
	}()
	var capture *RootTopologyCapture
	if plan.fingerprint != "" {
		lease := libraryRootLease{approved: anchor, relativePath: plan.root.relativePath}
		capture, err = lease.CaptureTopology(ctx, mapping, registered)
		if err != nil {
			return result, errSeriesNFOAuditRoot
		}
		defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, capture.Close)) }()
		if live, err := capture.Fingerprint(); err != nil || live != plan.fingerprint {
			return result, errSeriesNFOAuditRoot
		}
	}
	witness, err := readSeriesNFOAuditRootWitness(ctx, anchor, registered)
	if err != nil {
		return result, err
	}
	result, err = work(ctx, registered, witness)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if capture != nil {
		if err := capture.Revalidate(ctx); err != nil {
			return result, errors.Join(errSeriesNFOAuditRoot, ctx.Err())
		}
	} else {
		// Even an unbound root may supply a warning, but never from an
		// anchor or registered directory detached during this observation.
		currentAnchor, currentRoot, err := openRootTopologyNames(ctx, mapping)
		if err != nil {
			return result, errors.Join(errSeriesNFOAuditRoot, ctx.Err())
		}
		defer func() {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, currentRoot), closeDirectoryPrimaryRoot(ctx, currentAnchor))
		}()
		current, err := readSeriesNFOAuditRootWitness(ctx, currentAnchor, currentRoot)
		if err != nil {
			return result, err
		}
		if !witness.same(current) {
			return result, errSeriesNFOAuditRoot
		}
	}
	return result, ctx.Err()
}

func readSeriesNFOAuditRootWitness(ctx context.Context, anchor, root *os.Root) (seriesNFOAuditRootWitness, error) {
	if err := ctx.Err(); err != nil {
		return seriesNFOAuditRootWitness{}, err
	}
	first, err := anchor.Stat(".")
	if err != nil || ctx.Err() != nil {
		return seriesNFOAuditRootWitness{}, errors.Join(errSeriesNFOAuditRoot, ctx.Err())
	}
	second, err := root.Stat(".")
	if err != nil {
		return seriesNFOAuditRootWitness{}, errSeriesNFOAuditRoot
	}
	return seriesNFOAuditRootWitness{anchor: first, registered: second}, ctx.Err()
}

func (witness seriesNFOAuditRootWitness) same(other seriesNFOAuditRootWitness) bool {
	return os.SameFile(witness.anchor, other.anchor) && os.SameFile(witness.registered, other.registered)
}

func readSeriesNFOAuditCandidates(ctx context.Context, registered *os.Root, plan seriesNFOAuditRootPlan, witness seriesNFOAuditRootWitness,
	candidates []seriesNFOAuditTarget, remaining int64) (seriesNFOAuditRootEvidence, error) {
	result := seriesNFOAuditRootEvidence{plan: plan, witness: witness, remaining: remaining}
	for _, target := range candidates {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !target.inRoot(plan.root) {
			continue
		}
		leafName := filepath.Base(target.sourceRelative())
		if target.relative == tvRootSeriesPath {
			leafName = filepath.Base(plan.root.path)
		}
		observation, err := readSeriesNFOAuditCandidate(ctx, registered, target, leafName, &result.remaining)
		if err != nil {
			return result, err
		}
		if observation.file != nil {
			result.observations = append(result.observations, observation)
		}
	}
	return result, nil
}

func readSeriesNFOAuditCandidate(ctx context.Context, root *os.Root, target seriesNFOAuditTarget, leafName string, remaining *int64) (_ seriesNFOAuditObservation, resultErr error) {
	relative := target.sourceRelative()
	dir, err := openRootTopologyRelative(ctx, root, relative)
	if err != nil {
		return seriesNFOAuditObservation{}, ctx.Err()
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	if err := ctx.Err(); err != nil {
		return seriesNFOAuditObservation{}, err
	}
	directoryFile, err := dir.Open(".")
	if err != nil {
		return seriesNFOAuditObservation{}, ctx.Err()
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, directoryFile)) }()
	if err := ctx.Err(); err != nil {
		return seriesNFOAuditObservation{}, err
	}
	directory, err := directoryFile.Stat()
	if err != nil || fileIdentity(directory) == "" || ctx.Err() != nil {
		return seriesNFOAuditObservation{}, ctx.Err()
	}
	leaf := filepath.Base(target.nfoPath)
	before, err := dir.Lstat(leaf)
	if err := ctx.Err(); err != nil {
		return seriesNFOAuditObservation{}, err
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > maxLocalNFOBytes || fileIdentity(before) == "" {
		return seriesNFOAuditObservation{}, nil
	}
	// Charge attempts before opening: even a failed recheck may have read data.
	if before.Size()+1 > *remaining {
		return seriesNFOAuditObservation{}, errSeriesNFOAuditBytes
	}
	*remaining -= before.Size() + 1
	file, err := openLiteralNFOLeaf(directoryFile, leaf, os.O_RDONLY)
	if err != nil {
		return seriesNFOAuditObservation{}, ctx.Err()
	}
	data, invalid, err := readOpenedLocalNFO(ctx, file, before, func() (os.FileInfo, error) { return dir.Lstat(leaf) })
	if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
		return seriesNFOAuditObservation{}, fatal
	}
	if invalid || err != nil {
		return seriesNFOAuditObservation{}, nil
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != target.digest {
		return seriesNFOAuditObservation{}, nil
	}
	claim, ok := parseSeriesNFOAuditClaim(data, target.id, fileIdentity(directory), fileIdentity(before), leafName)
	if !ok {
		return seriesNFOAuditObservation{}, nil
	}
	return seriesNFOAuditObservation{claim: claim, relative: relative, directory: directory, file: before, leaf: leaf}, nil
}

func (evidence seriesNFOAuditRootEvidence) verify(ctx context.Context, root *os.Root, witness seriesNFOAuditRootWitness) ([]seriesNFOAuditClaim, error) {
	if !evidence.witness.same(witness) {
		return nil, errSeriesNFOAuditRoot
	}
	var claims []seriesNFOAuditClaim
	for _, observation := range evidence.observations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		matches, err := observation.verify(ctx, root)
		if err != nil {
			return nil, err
		}
		if matches {
			claims = append(claims, observation.claim)
		}
	}
	return claims, nil
}

func (observation seriesNFOAuditObservation) verify(ctx context.Context, root *os.Root) (_ bool, resultErr error) {
	dir, err := openRootTopologyRelative(ctx, root, observation.relative)
	if err != nil {
		return false, ctx.Err()
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	directory, err := dir.Stat(".")
	if err != nil || !os.SameFile(observation.directory, directory) || ctx.Err() != nil {
		return false, ctx.Err()
	}
	current, err := dir.Lstat(observation.leaf)
	return err == nil && sameLocalNFOFile(observation.file, current), ctx.Err()
}
