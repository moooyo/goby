package lifecycle

import (
	"bytes"
	"testing"
)

func TestCanonicalMetadataRejectsAmbiguousAndUnknownFields(t *testing.T) {
	value := marker{Version: 1, DeploymentID: "0123456789abcdef0123456789abcdef", Lock: identity{Device: 1, Inode: 2}}
	data, err := encode(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded marker
	if err := decode(data, &decoded); err != nil || decoded != value {
		t.Fatalf("canonical round trip: %v", err)
	}
	inputs := [][]byte{
		bytes.TrimSpace(data),
		append(append([]byte{}, data...), data...),
		[]byte(`{"version":1,"version":1,"deploymentId":"0123456789abcdef0123456789abcdef","lock":{"device":1,"inode":2}}` + "\n"),
		[]byte(`{"version":1,"deploymentId":"0123456789abcdef0123456789abcdef","lock":{"device":1,"inode":2},"credential":"secret"}` + "\n"),
	}
	for index, input := range inputs {
		if decode(input, new(marker)) == nil {
			t.Fatalf("accepted ambiguous input %d", index)
		}
	}
}

func TestDefaultStateDistinguishesAbsenceFromPublishedManifest(t *testing.T) {
	initial := stateOf(nil)
	if initial.Revision != 0 || initial.DatabaseSlot != DatabasePrimary || initial.Master != MasterDefault || initial.GenerationID != "" || !validHex(initial.Digest, 64) {
		t.Fatal("invalid default state")
	}
	manifest := activeManifest{Version: 1, Revision: 1, GenerationID: "0123456789abcdef0123456789abcdef", DatabaseSlot: DatabaseRecovery, Master: MasterDefault}
	published := stateOf(&manifest)
	if published.Digest == initial.Digest || published.Revision != 1 || published.DatabaseSlot != DatabaseRecovery {
		t.Fatal("published state collapsed to default")
	}
}

func TestDescriptorsPermitOnlyFixedGenerationFiles(t *testing.T) {
	config := FileDescriptor{Name: configName, Size: 2, SHA256: digest([]byte("{}"))}
	master := FileDescriptor{Name: masterName, Size: 32, SHA256: digest(make([]byte, 32))}
	if !validDescriptor(config, false) || !validDescriptor(master, true) {
		t.Fatal("valid descriptor rejected")
	}
	for _, name := range []string{"../config.json", "/config.json", "config.json/other", "master.key", "config.json\x00"} {
		invalid := config
		invalid.Name = name
		if validDescriptor(invalid, false) {
			t.Fatalf("accepted path %q", name)
		}
	}
}
