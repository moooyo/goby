package backupformat

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

func TestCreateLeavesChangedSourceUnauthenticated(t *testing.T) {
	releaseKDFMemory()
	t.Cleanup(releaseKDFMemory)
	manifest, members := fixture(false)
	members[DatabaseName] = bytes.Repeat([]byte("synthetic changed source"), 4096)
	manifest.Files[0] = describe(DatabaseName, members[DatabaseName])
	members[DatabaseName][100] ^= 1
	var destination bytes.Buffer
	err := Create(context.Background(), &destination, syntheticPassphrase, manifest, entryReaders(members))
	if !errors.Is(err, ErrSourceChanged) || destination.Len() == 0 {
		t.Fatalf("changed source result: %v", err)
	}
	releaseKDFMemory()
	// Even completed encrypted chunks cannot make a failed creation look like
	// a completed backup. Do not rely solely on Create's returned error.
	identity, err := age.NewScryptIdentity(string(syntheticPassphrase))
	if err != nil {
		t.Fatal(err)
	}
	identity.SetMaxWorkFactor(18)
	reader, err := age.Decrypt(bytes.NewReader(destination.Bytes()), identity)
	if err == nil {
		_, err = io.Copy(io.Discard, reader)
	}
	if err == nil {
		t.Fatal("failed creation was finalized as a valid age file")
	}
}

func TestMemberStreamingRechecksSourceAndSinkFailures(t *testing.T) {
	data := []byte("synthetic source")
	for name, source := range map[string]io.Reader{
		"short":   bytes.NewReader(data[:len(data)-1]),
		"long":    bytes.NewReader(append(append([]byte{}, data...), 1)),
		"changed": bytes.NewReader(bytes.Repeat([]byte("x"), len(data))),
	} {
		t.Run(name, func(t *testing.T) {
			if err := writeMember(context.Background(), io.Discard, DatabaseName, int64(len(data)), source, digest(data)); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("source difference ignored: %v", err)
			}
		})
	}
	if err := writeMember(context.Background(), io.Discard, DatabaseName, int64(len(data)), errorReader{}, digest(data)); !errors.Is(err, ErrSource) {
		t.Fatalf("source failure: %v", err)
	}
	if err := writeMember(context.Background(), contextWriter{ctx: context.Background(), destination: errorWriter{}}, DatabaseName, int64(len(data)), bytes.NewReader(data), digest(data)); !errors.Is(err, ErrSink) {
		t.Fatalf("sink failure: %v", err)
	}
	if err := writeMember(context.Background(), io.Discard, DatabaseName, int64(len(data)), stalledReader{}, digest(data)); !errors.Is(err, ErrSource) {
		t.Fatalf("stalled source was not bounded: %v", err)
	}
}

func TestImportBoundsBeforeKDFAndForEntireStreams(t *testing.T) {
	manifest, members := fixture(false)
	plain := plainFixture(t, manifest, members)
	archive := encryptFixture(t, plain)
	for name, limits := range map[string]Limits{
		"encrypted": {MaxEncryptedBytes: int64(len(archive) - 1)},
		"plaintext": {MaxPlaintextBytes: int64(len(plain) - 1)},
		"manifest":  {MaxManifestBytes: 128},
		"dump":      {MaxDatabaseBytes: int64(len(members[DatabaseName]) - 1)},
		"config":    {MaxConfigurationBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			reader := &countingReader{source: bytes.NewReader(archive)}
			_, err := Inspect(context.Background(), reader, syntheticPassphrase, limits)
			if !errors.Is(err, ErrLimit) {
				t.Fatalf("limit ignored: %v", err)
			}
			if limits.MaxEncryptedBytes != 0 && reader.total > limits.MaxEncryptedBytes+1 {
				t.Fatal("encrypted limit performed unbounded lookahead")
			}
		})
	}
	if _, err := Inspect(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Limits{
		MaxEncryptedBytes: int64(len(archive)), MaxPlaintextBytes: int64(len(plain)),
	}); err != nil {
		t.Fatalf("exact-size limits incorrectly replaced EOF: %v", err)
	}
	reader := &countingReader{source: io.MultiReader(strings.NewReader("age-encryption.org/v1\n"), &repeatReader{value: 'x'})}
	if _, err := Inspect(context.Background(), reader, syntheticPassphrase, Limits{}); !errors.Is(err, ErrLimit) || reader.total > maxHeaderBytes {
		t.Fatalf("unbounded age header: read %d, error %v", reader.total, err)
	}
	reader = &countingReader{source: bytes.NewReader(archive)}
	if _, err := Inspect(context.Background(), reader, syntheticPassphrase, Limits{MaxEncryptedBytes: 20}); !errors.Is(err, ErrLimit) || reader.total > 21 {
		t.Fatalf("header bypassed encrypted input budget: read %d, error %v", reader.total, err)
	}
	// The header is otherwise valid, but advertises an expensive work factor.
	// Never construct a factor-19 ciphertext just to test the import cap.
	lines := bytes.SplitN(archive, []byte{'\n'}, 3)
	if len(lines) != 3 || !bytes.HasSuffix(lines[1], []byte(" 1")) {
		t.Fatal("unexpected synthetic age header")
	}
	lines[1] = append(append([]byte{}, lines[1][:len(lines[1])-1]...), []byte("19")...)
	tooExpensive := bytes.Join(lines, []byte{'\n'})
	if _, err := Inspect(context.Background(), bytes.NewReader(tooExpensive), syntheticPassphrase, Limits{}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("excessive scrypt factor accepted: %v", err)
	}
}

func TestCancellationAndFailingSinksDoNotReturnManifest(t *testing.T) {
	manifest, members := fixture(false)
	archive := encryptFixture(t, plainFixture(t, manifest, members))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	unread := &countingReader{source: bytes.NewReader(archive)}
	if _, err := Inspect(ctx, unread, syntheticPassphrase, Limits{}); !errors.Is(err, context.Canceled) || unread.total != 0 {
		t.Fatalf("already cancelled import: %v", err)
	}
	var destination bytes.Buffer
	if err := Create(ctx, &destination, syntheticPassphrase, manifest, entryReaders(members)); !errors.Is(err, context.Canceled) || destination.Len() != 0 {
		t.Fatalf("already cancelled creation: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	headerEnd := bytes.Index(archive, []byte("\n--- "))
	if headerEnd < 0 {
		t.Fatal("missing fixture age footer")
	}
	headerEnd += 1 + bytes.IndexByte(archive[headerEnd+1:], '\n') + 1
	reader := &cancelReader{source: bytes.NewReader(archive), after: headerEnd, cancel: cancel}
	if _, err := Inspect(ctx, reader, syntheticPassphrase, Limits{}); !errors.Is(err, context.Canceled) || reader.total != headerEnd {
		t.Fatalf("header-boundary cancellation: %v, read %d", err, reader.total)
	}
	ctx, cancel = context.WithCancel(context.Background())
	got, err := Extract(ctx, bytes.NewReader(archive), syntheticPassphrase, Sinks{Database: cancelWriter{cancel: cancel}}, Limits{})
	if !errors.Is(err, context.Canceled) || got.Format != "" {
		t.Fatalf("sink cancellation returned success: %v", err)
	}
	for name, writer := range map[string]io.Writer{"failure": errorWriter{}, "short write": shortWriter{}} {
		t.Run(name, func(t *testing.T) {
			got, err := Extract(context.Background(), bytes.NewReader(archive), syntheticPassphrase, Sinks{Database: writer}, Limits{})
			if !errors.Is(err, ErrSink) || got.Format != "" || strings.Contains(err.Error(), "synthetic secret") {
				t.Fatalf("unsafe sink error: %v", err)
			}
		})
	}
}

func TestLargeDumpStreamsThroughFixedMemoryIO(t *testing.T) {
	const size int64 = 32 << 20
	manifest, members := fixture(false)
	hash := sha256.New()
	if _, err := io.CopyN(hash, &repeatReader{value: 0xa7}, size); err != nil {
		t.Fatal(err)
	}
	manifest.Files[0] = File{Name: DatabaseName, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	input, output := io.Pipe()
	defer input.Close()
	producerDone := make(chan error, 1)
	go func() {
		err := produceStreamingFixture(output, manifestData, manifest, members)
		output.CloseWithError(err)
		producerDone <- err
	}()
	sink := &countingWriter{}
	source := &countingReader{source: input}
	_, extractErr := Extract(context.Background(), source, syntheticPassphrase, Sinks{Database: sink}, Limits{})
	input.Close()
	producerErr := <-producerDone
	if extractErr != nil || producerErr != nil {
		t.Fatalf("streaming: extract %v, produce %v", extractErr, producerErr)
	}
	if sink.total != size || sink.maximum > 64<<10 || source.maximum > (64<<10)+16 {
		t.Fatalf("unbounded stream I/O: sink %d bytes / %d per write, source %d per read", sink.total, sink.maximum, source.maximum)
	}
}

func TestEncryptionPayloadBudgetRetainsLimitClassification(t *testing.T) {
	recipient, err := age.NewScryptRecipient(string(syntheticPassphrase))
	if err != nil {
		t.Fatal(err)
	}
	recipient.SetWorkFactor(1)
	encoded := &budgetWriter{destination: io.Discard, remaining: 1000}
	encrypted, err := age.Encrypt(encoded, recipient)
	if err != nil {
		t.Fatal(err)
	}
	guarded := contextWriter{ctx: context.Background(), destination: encrypted}
	_, err = guarded.Write(make([]byte, 128<<10))
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("payload budget error changed classification: %v", err)
	}
}

func produceStreamingFixture(output io.Writer, manifestData []byte, manifest Manifest, members map[string][]byte) error {
	recipient, err := age.NewScryptRecipient(string(syntheticPassphrase))
	if err != nil {
		return err
	}
	recipient.SetWorkFactor(1)
	encrypted, err := age.Encrypt(output, recipient)
	if err != nil {
		return err
	}
	writer := tar.NewWriter(encrypted)
	write := func(name string, size int64, reader io.Reader) error {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: size, ModTime: time.Unix(0, 0).UTC(), Typeflag: tar.TypeReg, Format: tar.FormatGNU}); err != nil {
			return err
		}
		_, err := io.CopyN(writer, reader, size)
		return err
	}
	if err := write(ManifestName, int64(len(manifestData)), bytes.NewReader(manifestData)); err != nil {
		return err
	}
	if err := write(DatabaseName, manifest.Files[0].Size, &repeatReader{value: 0xa7}); err != nil {
		return err
	}
	if err := write(ConfigurationName, int64(len(members[ConfigurationName])), bytes.NewReader(members[ConfigurationName])); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return encrypted.Close()
}

type countingReader struct {
	source  io.Reader
	total   int64
	maximum int
}

func (r *countingReader) Read(data []byte) (int, error) {
	r.maximum = max(r.maximum, len(data))
	n, err := r.source.Read(data)
	r.total += int64(n)
	return n, err
}

type countingWriter struct {
	total   int64
	maximum int
}

func (w *countingWriter) Write(data []byte) (int, error) {
	w.total += int64(len(data))
	w.maximum = max(w.maximum, len(data))
	return len(data), nil
}

type repeatReader struct{ value byte }

func (r *repeatReader) Read(data []byte) (int, error) {
	for index := range data {
		data[index] = r.value
	}
	return len(data), nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("synthetic secret source path") }

type stalledReader struct{}

func (stalledReader) Read([]byte) (int, error) { return 0, nil }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic secret destination path")
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(data []byte) (int, error) {
	w.cancel()
	return len(data), nil
}

type cancelReader struct {
	source io.Reader
	after  int
	total  int
	cancel context.CancelFunc
}

func (r *cancelReader) Read(data []byte) (int, error) {
	n, err := r.source.Read(data)
	r.total += n
	if r.total >= r.after {
		r.cancel()
	}
	return n, err
}
