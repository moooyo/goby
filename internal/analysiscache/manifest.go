package analysiscache

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const ownerName = ".owner.json"
const manifestName = ".entry.json"
const sealName = ".seal"

type ownerRecord struct {
	Marker string `json:"marker"`
	Owner  string `json:"owner"`
	Key    string `json:"key"`
	Token  string `json:"token"`
}

type manifestRecord struct {
	Version   int        `json:"version"`
	Owner     string     `json:"owner"`
	Key       string     `json:"key"`
	Artifacts []Artifact `json:"artifacts"`
}

func randomToken() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
func readyDirectory(key string) string            { return "entry-" + key }
func (s *Store) temporaryPrefix() string          { return "tmp-" + s.disk.owner + "-" }
func (s *Store) trashDirectory(key string) string { return "trash-" + s.disk.owner + "-" + key }

func readNames(root *os.Root, limit int) ([]string, error) {
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > limit {
		return nil, ErrLimit
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	sort.Strings(names)
	return names, nil
}

func readControl(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := openRegular(root, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	identity, err := fileIdentity(before)
	if err != nil {
		return nil, err
	}
	if before.Size() < 1 || before.Size() > limit {
		return nil, ErrUnsafe
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	current, err := fileIdentity(after)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != before.Size() || current != identity {
		return nil, ErrUnsafe
	}
	return data, nil
}

func writeControl(root *os.Root, name string, data []byte) error {
	file, err := openRegular(root, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	return errors.Join(writeErr, file.Close())
}

func decodeCanonical(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrUnsafe
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return ErrUnsafe
	}
	canonical, err := json.Marshal(target)
	if err != nil || !bytes.Equal(canonical, data) {
		return ErrUnsafe
	}
	return nil
}

func readOwner(root *os.Root, owner, key string) (ownerRecord, int64, error) {
	data, err := readControl(root, ownerName, 512)
	if err != nil {
		return ownerRecord{}, 0, err
	}
	var record ownerRecord
	if err := decodeCanonical(data, &record); err != nil {
		return record, 0, err
	}
	if record.Marker != "goby-analysis-entry-v1" || record.Owner != owner || record.Key != key || !keyPattern.MatchString(key) || !tokenPattern.MatchString(record.Token) {
		return record, 0, ErrUnsafe
	}
	return record, int64(len(data)), nil
}

func loadEntry(root *os.Root, owner, key string) (Entry, error) {
	_, ownerBytes, err := readOwner(root, owner, key)
	if err != nil {
		return Entry{}, entryControlError(err)
	}
	encoded, err := readControl(root, manifestName, maxManifestBytes)
	if err != nil {
		return Entry{}, entryControlError(err)
	}
	sealed, err := readControl(root, sealName, 65)
	if err != nil {
		return Entry{}, entryControlError(err)
	}
	digest := sha256.Sum256(encoded)
	seal := hex.EncodeToString(digest[:])
	if string(sealed) != seal+"\n" {
		return Entry{}, ErrSealMismatch
	}
	var record manifestRecord
	if err := decodeCanonical(encoded, &record); err != nil {
		return Entry{}, err
	}
	if record.Version != 1 || record.Owner != owner || record.Key != key || len(record.Artifacts) < 1 || len(record.Artifacts) > 4 {
		return Entry{}, ErrUnsafe
	}
	expected := []string{ownerName, manifestName, sealName}
	entry := Entry{Key: key, Seal: seal, Bytes: ownerBytes + int64(len(encoded)) + int64(len(sealed)), Artifacts: append([]Artifact(nil), record.Artifacts...)}
	previous := ""
	missing := false
	for _, artifact := range record.Artifacts {
		if !allowedArtifact(artifact.Name) || artifact.Name <= previous || artifact.Size < 0 || artifact.Size > 512<<20 || !keyPattern.MatchString(artifact.SHA256) || len(artifact.Identity) == 0 || len(artifact.Identity) > 256 {
			return Entry{}, ErrUnsafe
		}
		if artifact.Name == "manifest.json" && artifact.Size > maxSmallManifestBytes {
			return Entry{}, ErrUnsafe
		}
		entry.Bytes += artifact.Size
		if entry.Bytes > 1<<30 {
			return Entry{}, ErrLimit
		}
		previous = artifact.Name
		file, err := openRegular(root, artifact.Name, os.O_RDONLY, 0)
		if errors.Is(err, os.ErrNotExist) {
			// Only a payload named by intact, owned and sealed control records
			// may be a disposable cache miss. Validate the remaining inventory
			// before returning that classification or authorizing cleanup.
			missing = true
			continue
		}
		if err != nil {
			return Entry{}, err
		}
		info, statErr := file.Stat()
		closeErr := file.Close()
		if statErr != nil || closeErr != nil {
			return Entry{}, errors.Join(statErr, closeErr)
		}
		identity, err := fileIdentity(info)
		if err != nil || info.Size() != artifact.Size || identity != artifact.Identity {
			return Entry{}, ErrUnsafe
		}
		expected = append(expected, artifact.Name)
	}
	names, err := readNames(root, 7)
	if err != nil {
		return Entry{}, entryControlError(err)
	}
	sort.Strings(expected)
	if strings.Join(expected, "\x00") != strings.Join(names, "\x00") {
		return Entry{}, ErrUnsafe
	}
	if missing {
		return entry, ErrNotFound
	}
	return entry, nil
}

func entryControlError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrUnsafe, err)
	}
	return err
}

// removeOwnedDirectory never follows child links or recursively removes an
// unknown tree. Validate the complete single-level inventory before unlinking.
// The owner marker is removed last, retaining proof across partial cleanup.
func (s *Store) removeOwnedDirectory(ctx context.Context, directory, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.disk.check(); err != nil {
		return err
	}
	root, err := openChild(s.disk.root, directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	before, err := root.Stat(".")
	if err != nil {
		return err
	}
	names, err := readNames(root, hardMaxTemporaryFiles+7)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		// Empty remnants are removable only inside this root's unguessable
		// temporary/trash namespace, never arbitrary empty directories.
		if !strings.HasPrefix(directory, s.temporaryPrefix()) && !strings.HasPrefix(directory, "trash-"+s.disk.owner+"-") {
			return ErrUnsafe
		}
	} else {
		if _, _, err := readOwner(root, s.disk.owner, key); err != nil {
			return err
		}
		for _, name := range names {
			if name != ownerName && name != manifestName && name != sealName && !allowedArtifact(name) && !temporaryPattern.MatchString(name) {
				return ErrUnsafe
			}
			file, err := openRegular(root, name, os.O_RDONLY, 0)
			if err != nil {
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
		for _, name := range names {
			if name == ownerName {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := root.Remove(name); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root.Remove(ownerName); err != nil {
			return err
		}
	}
	current, err := s.disk.root.Lstat(directory)
	if err != nil {
		return err
	}
	if !os.SameFile(before, current) {
		return ErrUnsafe
	}
	if err := s.disk.check(); err != nil {
		return err
	}
	if err := s.disk.root.Remove(directory); err != nil {
		return err
	}
	return syncDirectory(s.disk.root)
}

func (s *Store) removeEntry(ctx context.Context, state *entryState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.disk.check(); err != nil {
		return err
	}
	trash := s.trashDirectory(state.entry.Key)
	if state.directory != trash {
		if err := renameNoReplace(s.disk.root, state.directory, trash); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if err := s.disk.check(); err != nil {
					return err
				}
				if _, currentErr := s.disk.root.Lstat(state.directory); errors.Is(currentErr, os.ErrNotExist) {
					// An uncertain or external rename may have left the same
					// generation at its known trash name. Keep its charge until
					// that owned directory is actually inspected and retired.
					if _, trashErr := s.disk.root.Lstat(trash); !errors.Is(trashErr, os.ErrNotExist) {
						return errors.Join(ErrUnsafe, trashErr)
					}
					// No object is removed. The registered generation disappeared
					// beneath an unchanged owned root; its logical charge can end
					// once all existing descriptor leases have already released it.
					return nil
				}
			}
			return err
		}
		state.directory = trash
		if err := syncDirectory(s.disk.root); err != nil {
			return err
		}
	}
	if err := s.removeOwnedDirectory(ctx, state.directory, state.entry.Key); err != nil {
		return fmt.Errorf("remove owned cache entry: %w", err)
	}
	return nil
}
