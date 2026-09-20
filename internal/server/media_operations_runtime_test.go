package server

import (
	"context"
	"testing"
	"time"
)

func TestDisabledMediaOperationsShutdownWaitsForAdmittedRequests(t *testing.T) {
	lifetime, cancel := context.WithCancel(context.Background())
	r := &mediaOperationsRuntime{ctx: lifetime, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{}), loopStarted: true, workers: map[string]*mediaOperationExecution{}}
	go r.loop()
	if !r.enter() {
		t.Fatal("initial request was not admitted")
	}
	r.BeginClose()
	if r.enter() {
		t.Fatal("shutdown admitted another request")
	}
	select {
	case <-r.done:
		t.Fatal("shutdown detached an in-flight request")
	default:
	}
	r.operations.Done()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r.BeginClose()
}
