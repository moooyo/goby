//go:build linux

package recoverycontrol

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestControlRepeatedReadsKeepIndependentPayloadsAcrossCAS(t *testing.T) {
	store := openControl(t, controlDirectory(t))
	initial := readControl(t, store)
	published := writeControl(t, store, initial.Digest, []byte(`{"phase":"prepared"}`))
	first := readControl(t, store)
	second := readControl(t, store)
	first.Payload[0], published.Payload[0] = 'x', 'x'
	if string(second.Payload) != `{"phase":"prepared"}` {
		t.Fatal("read snapshots share payload storage")
	}
	if actual := readControl(t, store); actual.Digest != second.Digest || !bytes.Equal(actual.Payload, second.Payload) {
		t.Fatal("mutating a returned snapshot changed a later read")
	}
	next := writeControl(t, store, second.Digest, []byte(`{"phase":"finished"}`))
	for range 2 {
		actual := readControl(t, store)
		if actual.Revision != next.Revision || actual.Digest != next.Digest || string(actual.Payload) != `{"phase":"finished"}` {
			t.Fatal("a repeated read retained the previous publication")
		}
	}
}

func TestControlMaximumPayloadCASIsolatesPublicStorage(t *testing.T) {
	directory := controlDirectory(t)
	store := openControl(t, directory)
	initial := readControl(t, store)
	prefix, suffix := `{"text":"`, `"}`
	payload := []byte(prefix + strings.Repeat("x", MaxPayloadBytes-len(prefix)-len(suffix)) + suffix)
	first := writeControl(t, store, initial.Digest, payload)
	if first.Revision != 1 || len(first.Payload) != MaxPayloadBytes || !bytes.Equal(first.Payload, payload) {
		t.Fatal("CAS changed a maximum-sized payload")
	}
	payload[len(payload)/2], first.Payload[0] = 'y', 'x'
	current := readControl(t, store)
	if current.Payload[0] != '{' || current.Payload[len(current.Payload)/2] != 'x' {
		t.Fatal("maximum-sized CAS retained public payload storage")
	}
	next := writeControl(t, store, current.Digest, current.Payload)
	if next.Revision != current.Revision+1 || next.Digest == current.Digest || !bytes.Equal(next.Payload, current.Payload) {
		t.Fatal("maximum-sized equal-payload CAS did not advance the record")
	}
	current.Payload[len(current.Payload)/2], next.Payload[len(next.Payload)-1] = 'y', 'x'
	actual := readControl(t, store)
	if len(actual.Payload) != MaxPayloadBytes || actual.Payload[len(actual.Payload)/2] != 'x' || actual.Payload[len(actual.Payload)-1] != '}' {
		t.Fatal("consecutive maximum-sized CAS shared payload storage")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openControl(t, directory)
	reopened := readControl(t, store)
	if reopened.Revision != next.Revision || reopened.Digest != next.Digest || !bytes.Equal(reopened.Payload, actual.Payload) {
		t.Fatal("reopen changed the maximum-sized publication")
	}
}

func TestControlPublishedRecordMatchesFreshDecode(t *testing.T) {
	for _, test := range []struct {
		payload string
		read    string
	}{
		{payload: `{"text":"<>&","encoded":"\u003c","slash":"a\/b"}`},
		{payload: "{\"text\":\"\u2028\u2029\u263a\"}", read: "{\"text\":\"\\u2028\\u2029\u263a\"}"},
		{payload: `{"large":9007199254740993,"exponent":1e+1000,"negativeZero":-0}`},
		{payload: `{"z":[true,null,{"later":2,"first":1}],"a":false}`},
	} {
		t.Run(test.payload, func(t *testing.T) {
			expected := test.read
			if expected == "" {
				expected = test.payload
			}
			directory := controlDirectory(t)
			store := openControl(t, directory)
			initial := readControl(t, store)
			published := writeControl(t, store, initial.Digest, []byte(test.payload))
			cached := readControl(t, store)
			if string(published.Payload) != test.payload {
				t.Fatal("CAS changed the normalized caller payload representation")
			}
			if string(cached.Payload) != expected {
				t.Fatal("read differs from the encoded payload representation")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openControl(t, directory)
			fresh := readControl(t, store)
			if fresh.Revision != cached.Revision || fresh.Digest != cached.Digest || !bytes.Equal(fresh.Payload, cached.Payload) {
				t.Fatal("fresh record decoding differs from the published record")
			}
		})
	}
}

func TestControlExpandedPayloadIsNotAcceptedByReadCache(t *testing.T) {
	ctx := context.Background()
	directory := controlDirectory(t)
	store := openControl(t, directory)
	initial := readControl(t, store)
	prefix, suffix := `{"text":"`, `"}`
	separator := "\u2028"
	payload := []byte(prefix + strings.Repeat("x", MaxPayloadBytes-len(prefix)-len(suffix)-len(separator)) + separator + suffix)
	published := writeControl(t, store, initial.Digest, payload)
	if len(published.Payload) != MaxPayloadBytes || !bytes.Equal(published.Payload, payload) {
		t.Fatal("CAS changed its accepted normalized payload representation")
	}
	if _, err := store.Read(ctx); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("read accepted a payload expanded beyond the byte limit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(ctx, directory, deploymentOne); !errors.Is(err, ErrRecoveryRequired) {
		if reopened != nil {
			reopened.Close()
		}
		t.Fatalf("reopen differs from the expanded-payload read limit: %v", err)
	}
}

func TestControlRepeatedReadAndCASRecheckCurrentProofRelation(t *testing.T) {
	ctx := context.Background()
	store := openControl(t, controlDirectory(t))
	initial := readControl(t, store)
	published := writeControl(t, store, initial.Digest, []byte(`{"phase":"prepared"}`))
	readControl(t, store)
	proof := store.proof
	proof.Candidate.Digest = strings.Repeat("f", 64)
	data, err := encode(proof)
	if err != nil {
		t.Fatal(err)
	}
	file, err := store.replace(ctx, proofName, data, store.proofFile)
	if err != nil {
		t.Fatal(err)
	}
	store.proof, store.proofFile = proof, file
	if err := store.validateProof(); err != nil {
		t.Fatalf("fixture proof is invalid independently of current: %v", err)
	}
	if _, err := store.Read(ctx); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("repeated read accepted an unrelated publication proof: %v", err)
	}
	if _, err := store.CompareAndSwap(ctx, published.Digest, []byte(`{"phase":"finished"}`)); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatalf("CAS accepted an unrelated publication proof: %v", err)
	}
}

func TestControlRepeatedReadCASAndReopenRejectChangedMalformedRecords(t *testing.T) {
	for _, change := range []struct {
		name string
		old  string
		new  string
	}{
		{name: "duplicate payload key", old: `"key":1`, new: `"key":1,"key":2`},
		{name: "duplicate metadata key", old: `"version":1`, new: `"version":1,"version":1`},
		{name: "noncanonical metadata", old: `"version":1`, new: `"version": 1`},
		{name: "foreign metadata", old: `"version":1`, new: `"version":1,"foreign":true`},
		{name: "malformed payload", old: `"key":1`, new: `"key":`},
	} {
		t.Run(change.name, func(t *testing.T) {
			ctx := context.Background()
			directory := controlDirectory(t)
			store := openControl(t, directory)
			initial := readControl(t, store)
			published := writeControl(t, store, initial.Digest, []byte(`{"key":1}`))
			readControl(t, store)
			filename := filepath.Join(directory, currentName)
			data, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(data, []byte(change.old), []byte(change.new), 1)
			if bytes.Equal(changed, data) {
				t.Fatal("fixture did not change the current record")
			}
			if err := os.WriteFile(filename, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(ctx); err == nil {
				t.Fatal("repeated read accepted changed current bytes")
			}
			if _, err := store.CompareAndSwap(ctx, published.Digest, []byte(`{"key":2}`)); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("CAS accepted changed current bytes: %v", err)
			}
			proof := store.proof
			proof.Candidate.Digest = hash(changed)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			proofData, err := encode(proof)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, proofName), proofData, 0600); err != nil {
				t.Fatal(err)
			}
			if reopened, err := Open(ctx, directory, deploymentOne); err == nil {
				reopened.Close()
				t.Fatal("reopen accepted malformed current bytes with a matching digest")
			}
		})
	}
}
