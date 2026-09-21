package library

// StorageObservationStatus reports process-wide observation capacity, including
// canceled callers whose filesystem workers have not finished.
type StorageObservationStatus struct {
	Active   int
	Capacity int
}

// StorageObservationSnapshot reads admission occupancy without acquiring a slot
// or waiting for workers, filesystem operations, database work, or locks.
func StorageObservationSnapshot() StorageObservationStatus {
	return StorageObservationStatus{Active: len(storageObservationSlots), Capacity: cap(storageObservationSlots)}
}
