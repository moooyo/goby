package library

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func imageStoreTestPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 220, G: 80, B: 30, A: 255})
	picture.Set(1, 1, color.RGBA{R: 20, G: 150, B: 230, A: 255})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// Acquiring every slot is a barrier after the workers' deferred cleanup. The
// temporary test tokens are released before returning, including timeout paths.
func imageStoreWaitWorkerCleanup(slots chan struct{}) bool {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	held := 0
	defer func() {
		for range held {
			<-slots
		}
	}()
	for held < cap(slots) {
		select {
		case slots <- struct{}{}:
			held++
		case <-timer.C:
			return false
		}
	}
	return true
}
