package library

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRootStorageIdentityProfileConstants(t *testing.T) {
	if RootStorageIdentityVersion != 1 || RootStorageIdentityProfile != "linux-fsuuid-filehandle-v1" || MaxRootStorageHandleBytes != 128 {
		t.Fatal("the persisted root identity profile or bounded handle contract changed")
	}
}

func TestRootStorageIdentityValidateCanonicalFieldsAndHandleBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		edit  func(*RootStorageIdentity)
		valid bool
	}{
		{"valid identity", func(*RootStorageIdentity) {}, true},
		{"one opaque handle byte", func(value *RootStorageIdentity) { value.Handle = []byte{0} }, true},
		{"maximum opaque handle bytes", func(value *RootStorageIdentity) { value.Handle = bytes.Repeat([]byte{0xff}, MaxRootStorageHandleBytes) }, true},
		{"zero opaque handle type", func(value *RootStorageIdentity) { value.HandleType = 0 }, true},
		{"negative opaque handle type", func(value *RootStorageIdentity) { value.HandleType = -2147483648 }, true},
		{"maximum opaque handle type", func(value *RootStorageIdentity) { value.HandleType = 2147483647 }, true},
		{"nonzero UUID with leading zeroes", func(value *RootStorageIdentity) { value.FilesystemUUID = strings.Repeat("0", 31) + "1" }, true},
		{"missing version", func(value *RootStorageIdentity) { value.Version = 0 }, false},
		{"negative version", func(value *RootStorageIdentity) { value.Version = -1 }, false},
		{"future version", func(value *RootStorageIdentity) { value.Version = RootStorageIdentityVersion + 1 }, false},
		{"missing profile", func(value *RootStorageIdentity) { value.Profile = "" }, false},
		{"different profile", func(value *RootStorageIdentity) { value.Profile = "unrecognized-storage-profile" }, false},
		{"uppercase profile", func(value *RootStorageIdentity) { value.Profile = strings.ToUpper(RootStorageIdentityProfile) }, false},
		{"padded profile", func(value *RootStorageIdentity) { value.Profile = RootStorageIdentityProfile + " " }, false},
		{"missing UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = "" }, false},
		{"short UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = value.FilesystemUUID[:31] }, false},
		{"long UUID", func(value *RootStorageIdentity) { value.FilesystemUUID += "0" }, false},
		{"uppercase UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = strings.ToUpper(value.FilesystemUUID) }, false},
		{"nonhex UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = "g" + value.FilesystemUUID[1:] }, false},
		{"zero UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = strings.Repeat("0", 32) }, false},
		{"hyphenated UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = "01234567-89ab-cdef-0123-456789abcdef" }, false},
		{"padded UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = " " + value.FilesystemUUID[1:] }, false},
		{"UUID control byte", func(value *RootStorageIdentity) { value.FilesystemUUID = "\x00" + value.FilesystemUUID[1:] }, false},
		{"UUID invalid UTF-8", func(value *RootStorageIdentity) {
			value.FilesystemUUID = string([]byte{0xff}) + value.FilesystemUUID[1:]
		}, false},
		{"nil handle", func(value *RootStorageIdentity) { value.Handle = nil }, false},
		{"empty handle", func(value *RootStorageIdentity) { value.Handle = []byte{} }, false},
		{"oversized handle", func(value *RootStorageIdentity) { value.Handle = make([]byte, MaxRootStorageHandleBytes+1) }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := rootStorageIdentityFixture()
			test.edit(&value)
			err := value.Validate()
			if test.valid {
				if err != nil {
					t.Fatalf("valid opaque storage identity was rejected: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidRootStorageIdentity) {
				t.Fatalf("invalid identity error = %v, want ErrInvalidRootStorageIdentity", err)
			}
		})
	}
}

func TestRootStorageIdentityEqualRequiresEveryStableField(t *testing.T) {
	first := rootStorageIdentityFixture()
	copy := first
	copy.Handle = bytes.Clone(first.Handle)
	if !first.Equal(copy) || !copy.Equal(first) {
		t.Fatal("equal identities with independent handle storage did not match")
	}
	for _, test := range []struct {
		name string
		edit func(*RootStorageIdentity)
	}{
		{"UUID", func(value *RootStorageIdentity) { value.FilesystemUUID = "1" + value.FilesystemUUID[1:] }},
		{"handle type", func(value *RootStorageIdentity) { value.HandleType++ }},
		{"handle type high bits", func(value *RootStorageIdentity) { value.HandleType += 1 << 24 }},
		{"handle type sign", func(value *RootStorageIdentity) { value.HandleType = -value.HandleType }},
		{"handle first byte", func(value *RootStorageIdentity) { value.Handle[0] ^= 0xff }},
		{"handle last byte", func(value *RootStorageIdentity) { value.Handle[len(value.Handle)-1] ^= 0xff }},
		{"handle length", func(value *RootStorageIdentity) { value.Handle = append(value.Handle, 0) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := first
			other.Handle = bytes.Clone(first.Handle)
			test.edit(&other)
			if err := other.Validate(); err != nil {
				t.Fatalf("comparison fixture must remain a valid identity: %v", err)
			}
			if first.Equal(other) || other.Equal(first) {
				t.Fatal("a different stable identity field was ignored")
			}
		})
	}
}

func TestRootStorageIdentityEqualUsesCompleteUUIDAndMaximumHandle(t *testing.T) {
	first := rootStorageIdentityFixture()
	first.Handle = bytes.Repeat([]byte{0xa5}, MaxRootStorageHandleBytes)
	for index := range len(first.FilesystemUUID) {
		other := first
		uuid := []byte(first.FilesystemUUID)
		uuid[index] = 'f'
		if first.FilesystemUUID[index] == 'f' {
			uuid[index] = 'e'
		}
		other.FilesystemUUID = string(uuid)
		if other.Validate() != nil || first.Equal(other) || other.Equal(first) {
			t.Fatalf("filesystem UUID position %d did not participate in equality", index)
		}
	}
	for index := range first.Handle {
		other := first
		other.Handle = bytes.Clone(first.Handle)
		other.Handle[index] ^= 1
		if other.Validate() != nil || first.Equal(other) || other.Equal(first) {
			t.Fatalf("opaque handle byte %d did not participate in equality", index)
		}
	}
}

func TestRootStorageIdentityEqualNeverAcceptsInvalidOrZeroValues(t *testing.T) {
	valid := rootStorageIdentityFixture()
	invalidVersion := rootStorageIdentityFixture()
	invalidVersion.Version++
	invalidProfile := rootStorageIdentityFixture()
	invalidProfile.Profile = "unknown"
	invalidUUID := rootStorageIdentityFixture()
	invalidUUID.FilesystemUUID = strings.Repeat("0", 32)
	invalidHandle := rootStorageIdentityFixture()
	invalidHandle.Handle = nil
	for _, test := range []struct {
		name  string
		value RootStorageIdentity
	}{
		{"zero identity", RootStorageIdentity{}},
		{"invalid version", invalidVersion},
		{"invalid profile", invalidProfile},
		{"invalid UUID", invalidUUID},
		{"invalid handle", invalidHandle},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.value.Validate(), ErrInvalidRootStorageIdentity) {
				t.Fatal("invalid equality fixture unexpectedly passed validation")
			}
			if test.value.Equal(test.value) || test.value.Equal(valid) || valid.Equal(test.value) {
				t.Fatal("an invalid identity established storage equality")
			}
		})
	}
}

func TestRootStorageIdentityEqualityDoesNotIncludeLiveWitness(t *testing.T) {
	first := RootStorageObservation{Identity: rootStorageIdentityFixture(), Live: RootStorageWitness{Device: 11, Inode: 22, MountID: 33}}
	for _, test := range []struct {
		name string
		live RootStorageWitness
	}{
		{"device changes", RootStorageWitness{Device: 44, Inode: 22, MountID: 33}},
		{"inode changes", RootStorageWitness{Device: 11, Inode: 44, MountID: 33}},
		{"mount changes", RootStorageWitness{Device: 11, Inode: 22, MountID: 44}},
		{"all live values change", RootStorageWitness{Device: 44, Inode: 55, MountID: 66}},
	} {
		t.Run(test.name, func(t *testing.T) {
			other := RootStorageObservation{Identity: rootStorageIdentityFixture(), Live: test.live}
			if reflect.DeepEqual(first.Live, other.Live) {
				t.Fatal("live witness fixture did not change")
			}
			if !first.Identity.Equal(other.Identity) || !other.Identity.Equal(first.Identity) {
				t.Fatal("a live witness change altered persistent storage equality")
			}
		})
	}
}

func TestRootStorageIdentityAndObservationJSONRoundTripPreservesEveryField(t *testing.T) {
	identity := rootStorageIdentityFixture()
	identity.HandleType = -2147483648
	identity.Handle = bytes.Repeat([]byte{0, 0xff, 0x80, 0x7f}, MaxRootStorageHandleBytes/4)
	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatalf("encode persistent root identity: %v", err)
	}
	var decoded RootStorageIdentity
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode persistent root identity: %v", err)
	}
	if err := decoded.Validate(); err != nil || !identity.Equal(decoded) || !reflect.DeepEqual(identity, decoded) {
		t.Fatalf("JSON changed an opaque persistent identity field: %v", err)
	}
	observation := RootStorageObservation{Identity: identity, Live: RootStorageWitness{
		Device: 9007199254740993, Inode: ^uint64(0), MountID: 2147483647,
	}}
	encoded, err = json.Marshal(observation)
	if err != nil {
		t.Fatalf("encode root storage observation: %v", err)
	}
	var recovered RootStorageObservation
	if err := json.Unmarshal(encoded, &recovered); err != nil {
		t.Fatalf("decode root storage observation: %v", err)
	}
	if !observation.Identity.Equal(recovered.Identity) || !reflect.DeepEqual(observation, recovered) {
		t.Fatal("JSON changed the persistent identity, opaque handle type, handle bytes, or exact live integers")
	}
}

func rootStorageIdentityFixture() RootStorageIdentity {
	return RootStorageIdentity{Version: RootStorageIdentityVersion, Profile: RootStorageIdentityProfile,
		FilesystemUUID: "0123456789abcdef0123456789abcdef", HandleType: 17, Handle: []byte{0, 0x12, 0xff}}
}
