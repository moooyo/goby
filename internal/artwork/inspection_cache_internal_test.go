package artwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestInspectionCacheBoundsSuccessfulMetadata(t *testing.T) {
	frame := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	frame.SetNRGBA(0, 0, color.NRGBA{R: 17, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	var cache InspectionCache
	const total = 2*inspectionCacheEntries + 7
	digests := make([][sha256.Size]byte, total)
	for index := 0; index < total; index++ {
		data := append(append([]byte(nil), encoded.Bytes()...), byte(index), byte(index>>8))
		info, err := cache.InspectJoined(context.Background(), bytes.NewReader(data))
		if err != nil || info.Tag != contentHash(data) {
			t.Fatalf("distinct valid source failed inspection: %+v %v", info, err)
		}
		digests[index] = sha256.Sum256(data)
		if len(cache.entries) != min(index+1, inspectionCacheEntries) || cache.entries[digests[index]] != info {
			t.Fatalf("successful metadata lost retained entries or the accepted value: entries=%d index=%d", len(cache.entries), index)
		}
	}
	for index, digest := range digests {
		_, exists := cache.lookup(digest)
		if want := index >= total-inspectionCacheEntries; exists != want {
			t.Fatalf("FIFO retention differs after two capacity wraps: index=%d retained=%t want=%t", index, exists, want)
		}
	}
	before := len(cache.entries)
	if _, err := cache.InspectJoined(context.Background(), bytes.NewReader([]byte("invalid image"))); err == nil || len(cache.entries) != before {
		t.Fatal("failed validation changed the successful metadata population")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.InspectJoined(ctx, bytes.NewReader(encoded.Bytes())); err == nil || len(cache.entries) != before {
		t.Fatal("canceled validation changed the successful metadata population")
	}
}

func TestInspectionCacheHitsAndDuplicatesKeepInsertionOrder(t *testing.T) {
	var cache InspectionCache
	for index := 0; index < inspectionCacheEntries; index++ {
		digest, info := inspectionCacheTestEntry(index)
		if err := cache.remember(context.Background(), digest, info); err != nil {
			t.Fatal(err)
		}
	}
	oldest, want := inspectionCacheTestEntry(0)
	if info, exists := cache.lookup(oldest); !exists || info != want {
		t.Fatal("oldest entry was unavailable before reaching capacity")
	}
	if err := cache.remember(context.Background(), oldest, Info{Tag: "duplicate replacement"}); err != nil {
		t.Fatal(err)
	}
	if info, exists := cache.lookup(oldest); !exists || info != want {
		t.Fatal("duplicate insertion replaced successful metadata")
	}
	for index := inspectionCacheEntries; index < inspectionCacheEntries+2; index++ {
		digest, info := inspectionCacheTestEntry(index)
		if err := cache.remember(context.Background(), digest, info); err != nil {
			t.Fatal(err)
		}
		if len(cache.entries) != inspectionCacheEntries {
			t.Fatalf("single insertion changed the full cache population: %d", len(cache.entries))
		}
		for previous := 0; previous <= index; previous++ {
			key, wantInfo := inspectionCacheTestEntry(previous)
			current, exists := cache.lookup(key)
			retained := previous > index-inspectionCacheEntries
			if exists != retained || retained && current != wantInfo {
				t.Fatalf("hit or duplicate changed FIFO eviction: insertion=%d previous=%d retained=%t", index, previous, exists)
			}
		}
	}
}

func TestInspectionCacheRetainsHotEntriesDuringColdInsertions(t *testing.T) {
	data := inspectionCacheConcurrentPNG(t)
	var cache InspectionCache
	const hotEntries = 32
	const coldInsertions = 64
	payloads := make([][]byte, inspectionCacheEntries)
	metadata := make([]Info, inspectionCacheEntries)
	for index := range payloads {
		payloads[index] = append(append([]byte(nil), data...), byte(index), byte(index>>8))
		info, err := cache.InspectJoined(context.Background(), bytes.NewReader(payloads[index]))
		if err != nil {
			t.Fatal(err)
		}
		metadata[index] = info
	}
	for step := 0; step < coldInsertions; step++ {
		hot := inspectionCacheEntries - hotEntries + step%hotEntries
		digest := sha256.Sum256(payloads[hot])
		if cached, exists := cache.lookup(digest); !exists || cached != metadata[hot] {
			t.Fatalf("cold insertion discarded a hot entry before its FIFO turn: step=%d hot=%d", step, hot)
		}
		if info, err := cache.InspectJoined(context.Background(), bytes.NewReader(payloads[hot])); err != nil || info != metadata[hot] {
			t.Fatalf("retained hot inspection changed metadata: %+v %v", info, err)
		}
		index := inspectionCacheEntries + step
		cold := append(append([]byte(nil), data...), byte(index), byte(index>>8))
		if _, err := cache.InspectJoined(context.Background(), bytes.NewReader(cold)); err != nil {
			t.Fatal(err)
		}
		if len(cache.entries) != inspectionCacheEntries {
			t.Fatalf("cold insertion discarded more than one cached entry: %d", len(cache.entries))
		}
	}
	for index, payload := range payloads {
		info, exists := cache.lookup(sha256.Sum256(payload))
		retained := index >= coldInsertions
		if exists != retained || retained && info != metadata[index] {
			t.Fatalf("mixed access changed unrelated cached metadata: index=%d retained=%t", index, exists)
		}
	}
}

func inspectionCacheTestEntry(index int) ([sha256.Size]byte, Info) {
	data := []byte{byte(index), byte(index >> 8)}
	return sha256.Sum256(data), Info{Tag: contentHash(data), MIMEType: "image/png", Width: 1, Height: 1}
}
