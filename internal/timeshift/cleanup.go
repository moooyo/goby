package timeshift

// A single worker owns retirement I/O. Each charged artifact can enqueue at
// most one task and each window at most one destruction, so existing resource
// limits also bound this queue. Failed tasks remain tombstones until restart.
type cleanupTask struct {
	window   *windowState
	artifact *artifactState
	next     *cleanupTask
}

func (store *Store) enqueueCleanupLocked(window *windowState, artifact *artifactState) {
	if window.cleanupCount == 0 {
		window.cleanupDone = make(chan struct{})
	}
	window.cleanupCount++
	store.cleanupCount++
	if artifact != nil {
		window.cleanupBytes += artifact.charge
		store.cleanupBytes += artifact.charge
		store.cleanupArtifacts++
	}
	task := &cleanupTask{window: window, artifact: artifact}
	if store.cleanupTail == nil {
		store.cleanupHead = task
	} else {
		store.cleanupTail.next = task
	}
	store.cleanupTail = task
	store.wakeCleanupLocked()
}

func (store *Store) wakeCleanupLocked() {
	select {
	case store.cleanupWake <- struct{}{}:
	default:
	}
}

// Waiting releases the shared lock while preserving synchronous observations
// of actual retirement. Cancellation never releases storage or directory pins.
// Destruction added by the last artifact is part of the same completion.
func (store *Store) waitCleanupLocked(window *windowState) {
	for window.cleanupCount != 0 {
		done := window.cleanupDone
		store.mu.Unlock()
		<-done
		store.mu.Lock()
	}
}

func (store *Store) clean() {
	defer close(store.cleanupStopped)
	for {
		store.mu.Lock()
		task := store.cleanupHead
		if task == nil {
			stopping := store.cleanupStopping
			store.mu.Unlock()
			if stopping {
				return
			}
			<-store.cleanupWake
			continue
		}
		store.cleanupHead = task.next
		if store.cleanupHead == nil {
			store.cleanupTail = nil
		}
		store.mu.Unlock()

		window := task.window
		var err error
		destroyed := false
		if task.artifact != nil {
			err = store.removeArtifact(window.storage, task.artifact.artifact.ID)
		} else {
			err = window.storage.destroy()
			if err == nil {
				destroyed = true
				err = window.storage.close()
			}
		}

		store.mu.Lock()
		if err != nil {
			store.failLocked(err)
		} else if artifact := task.artifact; artifact != nil {
			delete(window.artifacts, artifact.artifact.ID)
			window.bytes -= artifact.charge
			store.bytes -= artifact.charge
			store.artifacts--
		}
		if destroyed {
			delete(store.windows, window.id)
		}
		// Enqueue directory retirement before dropping the final artifact pin.
		// Otherwise a waiting ClosePresentation could observe an empty window
		// whose directory and capacity have not actually been released yet.
		store.destroyClosedLocked(window)
		if artifact := task.artifact; artifact != nil {
			window.cleanupBytes -= artifact.charge
			store.cleanupBytes -= artifact.charge
			store.cleanupArtifacts--
			if window.destroying && len(window.artifacts) == 0 {
				// The last reader that enables closed-window destruction joins
				// that directory task as well as its own artifact retirement.
				window.destroyReaders = append(window.destroyReaders, artifact.finishedReaders...)
			} else {
				for _, reader := range artifact.finishedReaders {
					close(reader.done)
				}
			}
			artifact.finishedReaders = nil
		} else {
			for _, reader := range window.destroyReaders {
				close(reader.done)
			}
			window.destroyReaders = nil
		}
		window.cleanupCount--
		store.cleanupCount--
		close(store.cleanupChanged)
		store.cleanupChanged = make(chan struct{})
		if window.cleanupCount == 0 {
			close(window.cleanupDone)
		}
		store.mu.Unlock()
	}
}
