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
	for index := 0; index < 2*inspectionCacheEntries+7; index++ {
		data := append(append([]byte(nil), encoded.Bytes()...), byte(index), byte(index>>8))
		info, err := cache.InspectJoined(context.Background(), bytes.NewReader(data))
		if err != nil || info.Tag != contentHash(data) {
			t.Fatalf("distinct valid source failed inspection: %+v %v", info, err)
		}
		if len(cache.entries) > inspectionCacheEntries || cache.entries[sha256.Sum256(data)] != info {
			t.Fatalf("successful metadata exceeded its bound or lost the accepted value: entries=%d", len(cache.entries))
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
