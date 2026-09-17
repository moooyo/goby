package server

import (
	"context"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

type deletedUserTestJobs struct {
	*hlsRuntimeTestJobs
	sessions []string
}

func (jobs *deletedUserTestJobs) CancelSession(id string) {
	jobs.sessions = append(jobs.sessions, id)
}

func TestDeletedUserCredentialRetiresRegisteredAndUnregisteredProducers(t *testing.T) {
	h, originalJobs := hlsRuntimeTestFixture(t)
	jobs := &deletedUserTestJobs{hlsRuntimeTestJobs: originalJobs}
	h.manager = jobs
	add := func(id, credential string) context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		key := hlsKey{scope: transcode.Scope{AuthSessionID: credential}}
		session := &hlsSession{id: id, key: key, ctx: ctx, cancel: cancel, producers: []hlsProducer{{id: id + "-producer"}}}
		h.sessions[id], h.byKey[key] = session, session
		return ctx
	}
	deleted := add("deleted-stream", "deleted-auth")
	foreign := add("foreign-stream", "foreign-auth")
	h.cancelCredential("deleted-auth")
	if deleted.Err() == nil || foreign.Err() != nil || len(h.sessions) != 1 || h.sessions["foreign-stream"] == nil {
		t.Fatal("deleted credential did not close only its registered stream")
	}
	if len(jobs.cancels) != 1 || jobs.cancels[0].id != "deleted-stream-producer" {
		t.Fatal("credential retirement cancelled a foreign producer")
	}
	h.cancelCredential("previously-retired-auth")
	if !slices.Equal(jobs.sessions, []string{"deleted-auth", "previously-retired-auth"}) {
		t.Fatal("unregistered conversion work was not fenced by credential")
	}
}
