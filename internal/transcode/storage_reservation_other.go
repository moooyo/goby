//go:build !linux

package transcode

func openStorageReservationJournal(string, string) (storageReservationJournal, error) {
	return nil, ErrStorageUnavailable
}
