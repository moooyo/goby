package tasks

import (
	"context"
	"testing"
)

func TestFencedDrainCancelsEveryWorkerBeforeWaitingForCleanup(t *testing.T) {
	firstCtx, firstCancel := context.WithCancel(context.Background())
	secondCtx, secondCancel := context.WithCancel(context.Background())
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	manager := &Manager{executions: map[string]*workerExecution{
		"first": {cancel: firstCancel, done: firstDone}, "second": {cancel: secondCancel, done: secondDone},
	}}
	drained := make(chan struct{})
	go func() { manager.drainExecutions(); close(drained) }()
	<-firstCtx.Done()
	<-secondCtx.Done()
	select {
	case <-drained:
		t.Fatal("fenced shutdown released dependencies before worker cleanup")
	default:
	}
	close(firstDone)
	select {
	case <-drained:
		t.Fatal("fenced shutdown ignored a remaining worker")
	default:
	}
	close(secondDone)
	<-drained
	if len(manager.executions) != 0 {
		t.Fatal("drained worker handles were retained")
	}
}
