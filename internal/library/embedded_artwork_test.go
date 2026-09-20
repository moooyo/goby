package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func embeddedArtworkTestPicture(t *testing.T, index int, kind string, pixel color.Color) media.EmbeddedPicture {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			frame.Set(x, y, pixel)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	digest := sha256.Sum256(data)
	return media.EmbeddedPicture{StreamIndex: index, PictureType: kind, Data: data, Hash: hex.EncodeToString(digest[:]), MIMEType: "image/png", Width: 4, Height: 3}
}

func TestEmbeddedArtworkCapabilityResultRequiresActualBytesAndStableSelection(t *testing.T) {
	back := embeddedArtworkTestPicture(t, 1, "Back", color.NRGBA{R: 200, A: 255})
	front := embeddedArtworkTestPicture(t, 9, "Front", color.NRGBA{G: 200, A: 255})
	preferred := embeddedArtworkTestPicture(t, 2, "Front", color.NRGBA{B: 200, A: 255})
	other := embeddedArtworkTestPicture(t, 0, "Other", color.NRGBA{R: 100, A: 255})
	result := media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{back, front, other, preferred}}
	selected, err := validateEmbeddedArtworkResult(context.Background(), result)
	if err != nil || selected == nil || selected.Hash != preferred.Hash || selected.StreamIndex != 2 {
		t.Fatalf("selection=%+v, %v", selected, err)
	}
	for _, test := range []struct {
		name   string
		change func(*media.EmbeddedArtworkResult)
	}{
		{"wrong_version", func(v *media.EmbeddedArtworkResult) { v.Version++ }},
		{"false_hash", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].Hash = preferred.Hash }},
		{"false_dimensions", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].Width++ }},
		{"false_mime", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].MIMEType = "image/jpeg" }},
		{"corrupt_unselected_back", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].Data = []byte("corrupt") }},
		{"duplicate_absolute_index", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].StreamIndex = v.Pictures[1].StreamIndex }},
		{"negative_index", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].StreamIndex = -1 }},
		{"invalid_type", func(v *media.EmbeddedArtworkResult) { v.Pictures[0].PictureType = "Front; commands" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			copy := result
			copy.Pictures = append([]media.EmbeddedPicture(nil), result.Pictures...)
			test.change(&copy)
			if _, err := validateEmbeddedArtworkResult(context.Background(), copy); err == nil {
				t.Fatal("invalid extraction capability result accepted")
			}
		})
	}
	if selected, err := validateEmbeddedArtworkResult(context.Background(), media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion}); err != nil || selected != nil {
		t.Fatalf("explicit no-picture result=%+v,%v", selected, err)
	}
}

func TestEmbeddedArtworkCapabilityCannotInventStreamsOrAbsence(t *testing.T) {
	picture := embeddedArtworkTestPicture(t, 7, "Front", color.NRGBA{G: 100, A: 255})
	result := media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture}}
	probe := media.Info{Streams: []media.Stream{{Index: 0, CodecType: "audio"}, {Index: 7, CodecType: "video", IsAttachedPicture: true, Width: 4, Height: 3}}}
	if err := embeddedArtworkMatchesProbe(result, probe); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		result media.EmbeddedArtworkResult
		probe  media.Info
	}{
		{"false_absence", media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion}, probe},
		{"invented_picture", result, media.Info{Streams: []media.Stream{{Index: 0, CodecType: "audio"}}}},
		{"wrong_absolute_index", result, media.Info{Streams: []media.Stream{{Index: 1, CodecType: "video", IsAttachedPicture: true, Width: 4, Height: 3}}}},
		{"ordinary_video", result, media.Info{Streams: []media.Stream{{Index: 7, CodecType: "video", Width: 4, Height: 3}}}},
		{"external_picture", result, media.Info{Streams: []media.Stream{{Index: 7, CodecType: "video", IsExternal: true, IsAttachedPicture: true, Width: 4, Height: 3}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := embeddedArtworkMatchesProbe(test.result, test.probe); err == nil {
				t.Fatal("result did not bind the complete indexed attached-picture set")
			}
		})
	}
}
