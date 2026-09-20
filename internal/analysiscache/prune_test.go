//go:build linux

package analysiscache

import (
	"context"
	"testing"
)

func TestCachePruneCountsRemovedEntriesAndUniqueBusyOwners(t *testing.T) {
	ctx := context.Background()
	store := cacheTestOpen(t, cacheTestConfig(t))
	first := cacheTestPublish(t, store, 1, []byte("first"))
	second := cacheTestPublish(t, store, 2, []byte("second"))
	reader, err := store.Acquire(ctx, first.Key, first.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	otherReader, err := store.Acquire(ctx, first.Key, first.Seal, "240.bif")
	if err != nil {
		t.Fatal(err)
	}
	defer otherReader.Close()
	pendingBuilder, err := store.Begin(ctx, cacheTestKey(3), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer pendingBuilder.Abort(ctx)
	if _, err := pendingBuilder.WriteFile(ctx, "240.bif", cacheTestProducer([]byte("pending"))); err != nil {
		t.Fatal(err)
	}
	publication, err := pendingBuilder.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer publication.Keep()
	builder, err := store.Begin(ctx, cacheTestKey(4), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer builder.Abort(ctx)
	result, err := store.PruneUnreferencedResult(ctx, nil)
	if err != nil || result.RemovedEntries != 1 || result.RemovedBytes != second.Bytes || result.BusyEntries != 3 {
		t.Fatalf("pruning conflated reader count, owners or removal bytes: %+v, %v", result, err)
	}
	if result.RemainingBytes != store.Stats().TotalBytes {
		t.Fatal("pruning omitted still-owned reservations or control bytes")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := otherReader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publication.Keep(); err != nil {
		t.Fatal(err)
	}
	if err := builder.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = store.PruneUnreferencedResult(ctx, nil)
	if err != nil || result.RemovedEntries != 2 || result.BusyEntries != 0 || result.RemainingBytes != store.Stats().ControlBytes {
		t.Fatalf("released entries or retained owner marker were miscounted: %+v, %v", result, err)
	}
}
