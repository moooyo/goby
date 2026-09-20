package tasks

import (
	"context"
	"encoding/json"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// Execution identity contains identifiers and the originally authenticated peer,
// never bearer tokens, policy/role claims, passwords, or client display metadata.
type executionAuthority struct {
	ApplicationKeyID int64
	ClientSessionID  string
	PeerIP           string
}

// Authority columns are decoded for execution but never serialized with Run.
func (run *Run) UnmarshalJSON(data []byte) error {
	type publicRun Run
	var wire struct {
		*publicRun
		ApplicationKeyID int64  `json:"actor_application_key_id"`
		ClientSessionID  string `json:"actor_client_session_id"`
		PeerIP           string `json:"actor_peer_ip"`
	}
	*run = Run{}
	wire.publicRun = (*publicRun)(run)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	run.authority = executionAuthority{ApplicationKeyID: wire.ApplicationKeyID, ClientSessionID: wire.ClientSessionID, PeerIP: wire.PeerIP}
	return nil
}

func executionActor(run Run) (*Actor, error) {
	if run.Source == "schedule" || run.Source == "startup" || run.Source == "system_event" {
		if run.ActorKind != "system" || run.ActorUserID != "" || run.ActorSessionID != "" || run.authority != (executionAuthority{}) {
			return nil, identity.ErrUnauthorized
		}
		return nil, nil
	}
	var audience identity.AdministratorAudience
	switch run.Source {
	case "manual":
		audience = identity.AdministratorNative
	case "compatibility":
		audience = identity.AdministratorEmby
	default:
		return nil, identity.ErrUnauthorized
	}
	principal := identity.Principal{Kind: run.ActorKind, SessionID: run.ActorSessionID,
		User: identity.User{ID: run.ActorUserID}, ApplicationKeyID: run.authority.ApplicationKeyID,
		ClientSessionID: run.authority.ClientSessionID, PeerIP: run.authority.PeerIP}
	return &Actor{Principal: principal, Audience: audience}, nil
}

func sameExecutionIdentity(first, second Run) bool {
	return first.ID == second.ID && first.TaskID == second.TaskID && first.TaskKey == second.TaskKey && first.Source == second.Source &&
		first.ActorUserID == second.ActorUserID && first.ActorSessionID == second.ActorSessionID && first.ActorKind == second.ActorKind && first.authority == second.authority &&
		first.AnalysisConfigFingerprint == second.AnalysisConfigFingerprint && sameAnalysisInput(first.AnalysisInput, second.AnalysisInput)
}

func executionWork(ctx context.Context, run Run, child Child, token string) Work {
	// The capability captures different storage than the exported DTO so an
	// executor cannot redirect it by changing a Work field or selection slice.
	sealed := run
	sealed.AnalysisInput = cloneAnalysisSelection(run.AnalysisInput)
	work := Work{RunID: run.ID, ChildID: child.ID, LibraryID: child.LibraryID, TaskKey: run.TaskKey,
		AnalysisInput: cloneAnalysisSelection(run.AnalysisInput), AnalysisScopeKey: child.AnalysisScopeKey,
		AnalysisConfigFingerprint: run.AnalysisConfigFingerprint, publicationContexts: []context.Context{ctx}}
	work.fence = func(tx library.OwnedTx, presented Work) error {
		if ctx == nil || ctx.Err() != nil {
			return context.Canceled
		}
		if presented.RunID != sealed.ID || presented.ChildID != child.ID || presented.LibraryID != child.LibraryID ||
			presented.TaskKey != sealed.TaskKey || presented.AnalysisScopeKey != child.AnalysisScopeKey ||
			presented.AnalysisConfigFingerprint != sealed.AnalysisConfigFingerprint || !sameAnalysisInput(presented.AnalysisInput, sealed.AnalysisInput) {
			return ErrInconsistent
		}
		if token == "" || child.RunID != sealed.ID {
			return ErrInconsistent
		}
		actor, err := executionActor(sealed)
		if err != nil {
			return err
		}
		// Actor locks precede run/child and any publication business locks. A
		// second call in this transaction retains those same locks and rechecks.
		if actor != nil {
			if err := checkActor(tx, *actor, true); err != nil {
				return err
			}
		}
		current, err := readRun(tx, sealed.ID, true)
		if err != nil {
			return err
		}
		if !sameExecutionIdentity(current, sealed) {
			return ErrInconsistent
		}
		var state ChildState
		var libraryID, scopeKey string
		var claimed, scan *string
		if err := tx.QueryRow(`SELECT state,library_id,analysis_scope_key,executor_token,scan_job_id FROM task_run_children
			WHERE id=$1 AND run_id=$2 FOR UPDATE`, child.ID, sealed.ID).Scan(&state, &libraryID, &scopeKey, &claimed, &scan); err != nil {
			return err
		}
		if state != ChildRunning || libraryID != child.LibraryID || scopeKey != child.AnalysisScopeKey || claimed == nil || *claimed != token || scan != nil {
			return context.Canceled
		}
		var active bool
		if err := tx.QueryRow(`SELECT state='running' AND stop_requested_at IS NULL AND stop_reason=''
			AND (deadline_at IS NULL OR deadline_at>clock_timestamp()) FROM task_runs WHERE id=$1`, sealed.ID).Scan(&active); err != nil {
			return err
		}
		if !active || ctx.Err() != nil {
			return context.Canceled
		}
		if actor != nil {
			if err := checkActor(tx, *actor, false); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	return work
}
