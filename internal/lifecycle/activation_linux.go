//go:build linux

package lifecycle

import "context"

func (s *Store) Plan(ctx context.Context, expected State, candidate Candidate) (Plan, error) {
	if !validHex(candidate.GenerationID, 32) || candidate.DatabaseSlot != DatabasePrimary && candidate.DatabaseSlot != DatabaseRecovery || candidate.Master != MasterDefault && candidate.Master != MasterGeneration {
		return Plan{}, ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return Plan{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return Plan{}, err
	}
	if s.journal != nil || expected != s.stateOf(s.active) || expected.Revision == ^uint64(0) {
		return Plan{}, ErrConflict
	}
	entry, ok := s.findGeneration(candidate.GenerationID)
	if !ok {
		return Plan{}, ErrNotFound
	}
	if !entry.Complete {
		return Plan{}, ErrIncomplete
	}
	manifest := activeManifest{Version: 1, DeploymentID: s.marker.DeploymentID, Revision: expected.Revision + 1, GenerationID: entry.ID, DatabaseSlot: candidate.DatabaseSlot, Master: candidate.Master, Config: entry.Config.FileDescriptor}
	if candidate.Master == MasterGeneration {
		if entry.Master == nil {
			return Plan{}, ErrInvalid
		}
		descriptor := entry.Master.FileDescriptor
		manifest.MasterKey = &descriptor
	}
	id, err := randomID()
	if err != nil {
		return Plan{}, err
	}
	journal := activationJournal{Version: 1, DeploymentID: s.marker.DeploymentID, ID: id, Before: s.active, BeforeDigest: expected.Digest, After: manifest, AfterDigest: s.stateOf(&manifest).Digest}
	data, _ := encode(journal)
	file, err := s.atomicWrite(ctx, journalName, data, s.journalFile)
	if err != nil {
		return Plan{}, err
	}
	s.journal = &journal
	s.journalFile = file
	return *s.plan(), nil
}

func (s *Store) plan() *Plan {
	if s.journal == nil {
		return nil
	}
	value := &Plan{ID: s.journal.ID, Before: s.stateOf(s.journal.Before), After: s.stateOf(&s.journal.After), Status: PlanPrepared}
	if s.stateOf(s.active) == value.After {
		value.Status = PlanActivated
	}
	return value
}

func (s *Store) Pending(ctx context.Context) (*Plan, error) {
	if err := s.enter(ctx); err != nil {
		return nil, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return nil, err
	}
	return s.plan(), nil
}

// Activate publishes a prepared manifest with an exact current-state CAS.
// Retrying an already activated plan is idempotent. ErrRecoveryRequired means
// the outcome may already be visible; close, reopen and inspect Pending.
func (s *Store) Activate(ctx context.Context, planID string) (State, error) {
	if !validHex(planID, 32) {
		return State{}, ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return State{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return State{}, err
	}
	if s.journal == nil || s.journal.ID != planID {
		return State{}, ErrConflict
	}
	plan := s.plan()
	if plan.Status == PlanActivated {
		return plan.After, nil
	}
	if s.stateOf(s.active) != plan.Before {
		return State{}, ErrConflict
	}
	data, _ := encode(s.journal.After)
	file, err := s.atomicWrite(ctx, activeName, data, s.activeFile)
	if err != nil {
		return State{}, err
	}
	manifest := s.journal.After
	s.active = &manifest
	s.activeFile = file
	return s.stateOf(s.active), nil
}

// Finish acknowledges only filesystem publication. It says nothing about
// database health and must not be used as a business rollback authorization.
func (s *Store) Finish(ctx context.Context, planID string) error {
	if !validHex(planID, 32) {
		return ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return err
	}
	if s.journal == nil || s.journal.ID != planID || s.plan().Status != PlanActivated {
		return ErrConflict
	}
	s.registry.BaselineDigest = s.stateOf(s.active).Digest
	if err := s.persistRegistry(ctx); err != nil {
		return err
	}
	if err := s.removeTracked(ctx, journalName, s.journalFile); err != nil {
		return err
	}
	s.journal = nil
	s.journalFile = trackedFile{}
	return nil
}

// Abort discards an intention only while the exact before-manifest remains
// current. It retains all staged generations and never restores old files.
func (s *Store) Abort(ctx context.Context, planID string) error {
	if !validHex(planID, 32) {
		return ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return err
	}
	if s.journal == nil || s.journal.ID != planID || s.plan().Status != PlanPrepared {
		return ErrConflict
	}
	if err := s.removeTracked(ctx, journalName, s.journalFile); err != nil {
		return err
	}
	s.journal = nil
	s.journalFile = trackedFile{}
	return nil
}
