package library

import (
	"fmt"
	"testing"
)

func TestAutomaticDiscoveryHintsRetainOnlyCurrentCompletedGeneration(t *testing.T) {
	var hints automaticDiscoveryHints
	key := automaticDiscoveryKey{"library", TaskBackgroundPreviewGenerationKey}
	old, known := hints.lookup(key)
	if known {
		t.Fatal("a new owner trusted an unprepared discovery")
	}
	hints.remember(key, old)
	if _, known := hints.lookup(key); !known {
		t.Fatal("successful unchanged discovery was not reused")
	}
	hints.invalidate()
	hints.remember(key, old)
	current, known := hints.lookup(key)
	if known || current == old {
		t.Fatal("a late completed discovery hid a newer catalog commit")
	}
	hints.remember(key, current)
	if _, known := hints.lookup(key); !known {
		t.Fatal("new generation could not be prepared")
	}
	if _, known := hints.lookup(automaticDiscoveryKey{"library", TaskAudioWaveformGenerationKey}); known {
		t.Fatal("one sidecar population authorized another population's reuse")
	}
}

func TestAutomaticDiscoveryHintsEvictOneEntryAndInvalidateWithoutListener(t *testing.T) {
	store := &Store{}
	keys := make([]automaticDiscoveryKey, maxAutomaticDiscoveryHints)
	for index := range keys {
		keys[index] = automaticDiscoveryKey{fmt.Sprint(index), TaskBackgroundPreviewGenerationKey}
		generation, _ := store.automaticDiscovery.lookup(keys[index])
		store.automaticDiscovery.remember(keys[index], generation)
	}
	extra := automaticDiscoveryKey{"extra", TaskBackgroundPreviewGenerationKey}
	generation, _ := store.automaticDiscovery.lookup(extra)
	store.automaticDiscovery.remember(extra, generation)
	if len(store.automaticDiscovery.completed) != maxAutomaticDiscoveryHints {
		t.Fatal("discovery hints exceeded their bound")
	}
	if _, known := store.automaticDiscovery.lookup(keys[0]); known {
		t.Fatal("the oldest hint was not evicted")
	}
	for _, key := range keys[1:] {
		if _, known := store.automaticDiscovery.lookup(key); !known {
			t.Fatal("insertion discarded an unrelated retained hint")
		}
	}
	store.notifyCatalogChanges(CatalogNotification{})
	if _, known := store.automaticDiscovery.lookup(extra); !known {
		t.Fatal("an empty commit invalidated unchanged discovery")
	}
	store.notifyCatalogChanges(CatalogNotification{Resync: true})
	if _, known := store.automaticDiscovery.lookup(extra); known {
		t.Fatal("catalog resync without a listener retained stale discovery")
	}
}
