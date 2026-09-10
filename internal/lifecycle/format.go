package lifecycle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
)

const (
	markerName       = ".goby-lifecycle.json"
	lockName         = ".goby-lifecycle.lock"
	registryName     = "generation-registry.json"
	activeName       = "active-generation.json"
	journalName      = "activation-journal.json"
	configName       = "config.json"
	masterName       = "master.key"
	maxMetadataBytes = 8 << 20
)

type identity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type marker struct {
	Version      int      `json:"version"`
	DeploymentID string   `json:"deploymentId"`
	Lock         identity `json:"lock"`
}

type registeredFile struct {
	FileDescriptor
	Identity identity `json:"identity"`
}

type registeredGeneration struct {
	ID        string          `json:"id"`
	Complete  bool            `json:"complete"`
	Directory identity        `json:"directory"`
	Config    registeredFile  `json:"config"`
	Master    *registeredFile `json:"master,omitempty"`
}

type registry struct {
	Version        int                    `json:"version"`
	DeploymentID   string                 `json:"deploymentId"`
	BaselineDigest string                 `json:"baselineDigest"`
	Generations    []registeredGeneration `json:"generations"`
}

type activeManifest struct {
	Version      int             `json:"version"`
	DeploymentID string          `json:"deploymentId"`
	Revision     uint64          `json:"revision"`
	GenerationID string          `json:"generationId"`
	DatabaseSlot DatabaseSlot    `json:"databaseSlot"`
	Master       MasterSource    `json:"master"`
	Config       FileDescriptor  `json:"config"`
	MasterKey    *FileDescriptor `json:"masterKey,omitempty"`
}

type activationJournal struct {
	Version      int             `json:"version"`
	DeploymentID string          `json:"deploymentId"`
	ID           string          `json:"id"`
	Before       *activeManifest `json:"before"`
	BeforeDigest string          `json:"beforeDigest"`
	After        activeManifest  `json:"after"`
	AfterDigest  string          `json:"afterDigest"`
}

func digest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

func validHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func encode(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalid
	}
	return append(data, '\n'), nil
}

func decode(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrUnavailable
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrUnavailable
	}
	// Requiring canonical bytes rejects duplicate keys and ambiguous encodings.
	canonical, err := encode(value)
	if err != nil || !bytes.Equal(canonical, data) {
		return ErrUnavailable
	}
	return nil
}

func stateOf(manifest *activeManifest) State {
	if manifest == nil {
		return State{DatabaseSlot: DatabasePrimary, Master: MasterDefault, Digest: digest(nil)}
	}
	data, _ := encode(manifest)
	return State{DeploymentID: manifest.DeploymentID, Revision: manifest.Revision, GenerationID: manifest.GenerationID, DatabaseSlot: manifest.DatabaseSlot, Master: manifest.Master, Digest: digest(data)}
}

func publicGeneration(entry registeredGeneration) Generation {
	result := Generation{ID: entry.ID, Complete: entry.Complete, Config: entry.Config.FileDescriptor}
	if entry.Master != nil {
		value := entry.Master.FileDescriptor
		result.Master = &value
	}
	return result
}

func validDescriptor(file FileDescriptor, master bool) bool {
	if !validHex(file.SHA256, 64) {
		return false
	}
	if master {
		return file.Name == masterName && file.Size == 32
	}
	return file.Name == configName && file.Size > 0 && file.Size <= MaxConfigBytes
}
