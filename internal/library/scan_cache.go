package library

const maxScannedVirtualFolders = 4096

// Virtual folders have no filesystem metadata source. Keep only the latest
// accepted input for each path within this root's scan; a later different title
// must publish again even when an earlier matching title was already visited.
type scannedVirtualFolder struct {
	id, path, name, itemType, parentID string
	indexNumber                        int
}

func (folder scannedVirtualFolder) sameInput(other scannedVirtualFolder) bool {
	return folder.path == other.path && folder.name == other.name && folder.itemType == other.itemType &&
		folder.parentID == other.parentID && folder.indexNumber == other.indexNumber
}

func (state *scanState) cacheVirtualFolder(relative string, folder scannedVirtualFolder, id string) {
	if state.virtualFolders == nil {
		state.virtualFolders = make(map[string]scannedVirtualFolder)
	}
	if _, exists := state.virtualFolders[relative]; !exists && len(state.virtualFolders) >= maxScannedVirtualFolders {
		return
	}
	folder.id = id
	state.virtualFolders[relative] = folder
}

// The role column is checked before readStoredFileAccepted inspects claims or
// decodes any metadata. Rejected resource metadata may itself be malformed.
type scannedRoleRow struct{ row rowScanner }

func (row scannedRoleRow) Scan(destinations ...any) error {
	var compatible bool
	if err := row.row.Scan(append(destinations, &compatible)...); err != nil {
		return err
	}
	if !compatible {
		return errScannedMediaRoleConflict
	}
	return nil
}
