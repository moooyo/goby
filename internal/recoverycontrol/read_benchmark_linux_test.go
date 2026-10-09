//go:build linux

package recoverycontrol

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// BenchmarkControlReadVerifiedRecord includes every filesystem and publication
// check performed by Read. The payload approximates bounded operation history.
func BenchmarkControlReadVerifiedRecord(b *testing.B) {
	for _, count := range []int{1, 64, 512} {
		b.Run(fmt.Sprintf("operations=%d", count), func(b *testing.B) {
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
			payload := controlBenchmarkPayload(count)
			published, err := store.CompareAndSwap(ctx, initial.Digest, payload)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for range b.N {
				actual, err := store.Read(ctx)
				if err != nil || actual.Revision != published.Revision || actual.Digest != published.Digest || len(actual.Payload) != len(payload) {
					b.Fatalf("read changed the published record: %v", err)
				}
			}
		})
	}
}

// BenchmarkControlCASVerifiedRecord covers consecutive journal publications,
// including payload normalization, current verification and durable writes.
func BenchmarkControlCASVerifiedRecord(b *testing.B) {
	for _, count := range []int{1, 64, 512} {
		b.Run(fmt.Sprintf("operations=%d", count), func(b *testing.B) {
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
			payload := controlBenchmarkPayload(count)
			current, err := store.CompareAndSwap(ctx, initial.Digest, payload)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for range b.N {
				next, err := store.CompareAndSwap(ctx, current.Digest, payload)
				if err != nil || next.Revision != current.Revision+1 || next.Digest == current.Digest || len(next.Payload) != len(payload) {
					b.Fatalf("CAS did not advance the published record: %v", err)
				}
				current = next
			}
		})
	}
}

// BenchmarkControlRecordValidation measures the parsing work within a fresh
// verified read, separately from filesystem integrity and publication checks.
func BenchmarkControlRecordValidation(b *testing.B) {
	for _, count := range []int{1, 64, 512} {
		b.Run(fmt.Sprintf("operations=%d", count), func(b *testing.B) {
			payload := controlBenchmarkPayload(count)
			data, err := encode(record{Version: 1, DeploymentID: deploymentOne, StoreID: deploymentTwo, Revision: 1, PreviousDigest: strings.Repeat("a", 64), Payload: payload})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for range b.N {
				var current record
				if err := decode(data, &current); err != nil {
					b.Fatal(err)
				}
				normalized, err := normalizePayload(current.Payload)
				if err != nil || !bytes.Equal(normalized, current.Payload) {
					b.Fatalf("invalid benchmark payload: %v", err)
				}
			}
		})
	}
}

func controlBenchmarkPayload(count int) []byte {
	var payload strings.Builder
	payload.WriteString(`{"version":1,"operations":[`)
	for index := range count {
		if index > 0 {
			payload.WriteByte(',')
		}
		fmt.Fprintf(&payload, `{"id":"%032x","requestId":"%032x","fingerprint":"%s","revision":7,"kind":"restore","state":"completed","phase":"finished","source":{"serverName":"%s","tables":[{"name":"items","rows":10000,"sha256":"%s"}]},"createdAt":"2026-10-09T00:00:00Z","updatedAt":"2026-10-09T00:01:00Z"}`, index+1, index+1, strings.Repeat("a", 64), strings.Repeat("n", 128), strings.Repeat("b", 64))
	}
	payload.WriteString(`],"slots":[]}`)
	return []byte(payload.String())
}
