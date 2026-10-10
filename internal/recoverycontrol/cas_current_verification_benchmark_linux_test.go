//go:build linux

package recoverycontrol

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkControlCASCurrentVerification measures the existing current-record
// verification phase used by CAS, including the operation gate and every
// filesystem and publication check. Ignoring the first return value permits
// identical benchmark code before and after moving the public payload copy.
func BenchmarkControlCASCurrentVerification(b *testing.B) {
	for _, size := range []int{1024, MaxPayloadBytes} {
		for _, parsedCache := range []bool{true, false} {
			b.Run(fmt.Sprintf("payload_bytes=%d/parsed_cache=%t", size, parsedCache), func(b *testing.B) {
				ctx := context.Background()
				store, err := Open(ctx, filepath.Join(b.TempDir(), "control"), deploymentOne)
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = store.Close() })
				initial, err := store.Read(ctx)
				if err != nil {
					b.Fatal(err)
				}
				prefix, suffix := `{"text":"`, `"}`
				payload := []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
				published, err := store.CompareAndSwap(ctx, initial.Digest, payload)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.SetBytes(int64(size))
				b.ResetTimer()
				for range b.N {
					if err := store.enter(ctx); err != nil {
						b.Fatal(err)
					}
					// A parse-cache miss still uses the complete production reader.
					// Both cases reopen and verify current bytes on every iteration.
					if !parsedCache {
						store.parsedCurrent = nil
					}
					_, reference, err := store.readCurrent(ctx)
					store.leave()
					if err != nil || reference.Revision != published.Revision || reference.Digest != published.Digest {
						b.Fatalf("CAS verification changed the current reference: %v", err)
					}
				}
			})
		}
	}
}
