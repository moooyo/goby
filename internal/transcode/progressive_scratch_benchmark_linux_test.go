//go:build linux

package transcode

import "testing"

// This benchmark can run unchanged against the prior implementation. Every
// iteration still stats, rereads and parses the same incomplete media prefix.
func BenchmarkProgressiveObserverIncompletePrefix(b *testing.B) {
	directory := b.TempDir()
	observer, err := newProgressiveObserver(directory, nil, Plan{OutputMode: "progressive", Container: "mp3", AudioCodec: "mp3"}, nil, func() {})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = observer.file.Close() })
	prefix := make([]byte, 1<<20)
	copy(prefix, []byte{'I', 'D', '3', 4, 0, 0, 1, 0, 0, 0})
	if _, err := observer.file.Write(prefix); err != nil {
		b.Fatal(err)
	}
	if err := observer.inspect(false); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := observer.inspect(false); err != nil {
			b.Fatal(err)
		}
	}
}
