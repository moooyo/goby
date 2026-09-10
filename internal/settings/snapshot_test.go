package settings

import (
	"reflect"
	"testing"
	"time"
)

func TestManagedSettingsSnapshotRemainsAvailableWhilePublicationIsLocked(t *testing.T) {
	// No owner or pool exists here. A runtime snapshot must use only the
	// already published immutable value, even while a writer holds its lock.
	store := &Store{}
	initial := Snapshot{Revision: 1, Defaults: settingsTestDefaults(), Effective: settingsTestDefaults(), UpdatedAt: time.Unix(1, 0).UTC()}
	store.publish(initial)
	store.publicationMu.Lock()
	defer store.publicationMu.Unlock()
	result := make(chan Snapshot, 1)
	go func() { result <- store.Snapshot() }()
	select {
	case snapshot := <-result:
		if !reflect.DeepEqual(snapshot, initial) {
			t.Fatal("runtime read returned a different published value")
		}
	case <-time.After(time.Second):
		t.Fatal("runtime snapshot waited for the settings writer")
	}
}

func TestManagedSettingsPublicationRejectsOlderRevisionsAndOwnsEveryPointer(t *testing.T) {
	store := &Store{}
	name, bitrate, width, height, channels := "Current", int64(10_000_000), 1280, 720, 2
	value := Snapshot{Revision: 5, Defaults: settingsTestDefaults(), UpdatedAt: time.Unix(5, 0).UTC(),
		Overrides: Overrides{ServerName: &name, MaxBitrate: &bitrate, MaxWidth: &width, MaxHeight: &height, MaxAudioChannels: &channels}}
	value.Effective = effectiveValues(value.Defaults, value.Overrides)
	published := store.publish(value)
	name, bitrate, width, height, channels = "Changed input", 1, 1, 1, 1
	*published.Overrides.ServerName = "Changed returned name"
	*published.Overrides.MaxBitrate = 2
	*published.Overrides.MaxWidth = 2
	*published.Overrides.MaxHeight = 2
	*published.Overrides.MaxAudioChannels = 1
	current := store.Snapshot()
	if current.Effective != (Values{ServerName: "Current", MaxBitrate: 10_000_000, MaxWidth: 1280, MaxHeight: 720, MaxAudioChannels: 2}) ||
		current.Effective != effectiveValues(current.Defaults, current.Overrides) {
		t.Fatal("publication retained caller-owned override pointers")
	}
	stale := Snapshot{Revision: 4, Defaults: settingsTestDefaults(), Effective: settingsTestDefaults()}
	returned := store.publish(stale)
	if !reflect.DeepEqual(returned, current) || !reflect.DeepEqual(store.Snapshot(), current) {
		t.Fatal("a delayed publisher replaced a newer revision")
	}
	*returned.Overrides.MaxWidth = 3
	*current.Overrides.MaxHeight = 3
	if store.Snapshot().Effective != effectiveValues(store.Snapshot().Defaults, store.Snapshot().Overrides) {
		t.Fatal("read snapshots leaked mutable override references")
	}
}
