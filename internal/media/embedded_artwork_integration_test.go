package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type embeddedArtworkIntegrationImage struct {
	path        string
	data        []byte
	mimeType    string
	pictureType string
	width       int
	height      int
}

func TestExtractActualEmbeddedArtworkPreservesAPICAndFLACPictures(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	for _, extension := range []string{"mp3", "flac"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			pictures := []embeddedArtworkIntegrationImage{
				embeddedArtworkIntegrationWriteImage(t, directory, "back", "jpeg", "Back", 27, 19),
				embeddedArtworkIntegrationWriteImage(t, directory, "front-first", "png", "Front", 24, 18),
				embeddedArtworkIntegrationWriteImage(t, directory, "other", "jpeg", "Other", 31, 17),
				embeddedArtworkIntegrationWriteImage(t, directory, "front-second", "jpeg", "Front", 22, 20),
			}
			path := filepath.Join(directory, "multiple-pictures."+extension)
			embeddedArtworkIntegrationMux(t, ffmpeg, path, pictures)
			embeddedArtworkIntegrationAssertContainer(t, path, extension, pictures)
			prober := Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 20 * time.Second}
			file, info := embeddedArtworkIntegrationOpen(t, prober, path, pictures)
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if err != nil {
				t.Fatalf("extract original %s pictures: %v", extension, err)
			}
			// The audio stream is index zero. The picture order deliberately
			// differs from both its source order and its video-relative index.
			embeddedArtworkIntegrationAssertPictures(t, result, pictures, []int{2, 4, 3, 1})
			embeddedArtworkIntegrationAssertOffset(t, file)
		})
	}
}

func TestExtractActualEmbeddedArtworkPreservesM4ACovr(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			directory := t.TempDir()
			pictures := []embeddedArtworkIntegrationImage{
				embeddedArtworkIntegrationWriteImage(t, directory, "cover", format, "Other", 29, 21),
			}
			path := filepath.Join(directory, "covr."+format+".m4a")
			embeddedArtworkIntegrationMux(t, ffmpeg, path, pictures)
			embeddedArtworkIntegrationAssertContainer(t, path, "m4a", pictures)
			prober := Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 20 * time.Second}
			file, info := embeddedArtworkIntegrationOpen(t, prober, path, pictures)
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if err != nil {
				t.Fatalf("extract original %s covr: %v", format, err)
			}
			embeddedArtworkIntegrationAssertPictures(t, result, pictures, []int{1})
			embeddedArtworkIntegrationAssertOffset(t, file)
		})
	}
}

func TestExtractActualEmbeddedArtworkBorrowsStableDescriptor(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	pictures := []embeddedArtworkIntegrationImage{
		embeddedArtworkIntegrationWriteImage(t, directory, "front", "png", "Front", 25, 23),
	}
	path := filepath.Join(directory, "audio; $filename.mp3")
	embeddedArtworkIntegrationMux(t, ffmpeg, path, pictures)
	prober := Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 20 * time.Second}
	file, info := embeddedArtworkIntegrationOpen(t, prober, path, pictures)
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("the replacement path is not media"), 0600); err != nil {
		t.Fatal(err)
	}
	// Rename may change ctime, so refresh source facts using the same held
	// descriptor after replacement without resolving the pathname again.
	info, err := prober.ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatalf("probe the original source after its pathname was replaced: %v", err)
	}
	result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
	if err != nil {
		t.Fatalf("extract the original picture through the borrowed descriptor: %v", err)
	}
	embeddedArtworkIntegrationAssertPictures(t, result, pictures, []int{1})
	embeddedArtworkIntegrationAssertOffset(t, file)
}

func TestExtractActualEmbeddedArtworkWithoutPictures(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	for _, extension := range []string{"mp3", "flac", "m4a"} {
		t.Run(extension, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "no-pictures."+extension)
			embeddedArtworkIntegrationMux(t, ffmpeg, path, nil)
			prober := Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 20 * time.Second}
			file, info := embeddedArtworkIntegrationOpen(t, prober, path, nil)
			result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info)
			if err != nil || result.Version != EmbeddedArtworkVersion || len(result.Pictures) != 0 {
				t.Fatalf("audio without artwork must produce a versioned empty success: %+v, %v", result, err)
			}
			embeddedArtworkIntegrationAssertOffset(t, file)
		})
	}
}

func TestExtractActualEmbeddedArtworkRejectsCorruptPicture(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	for _, extension := range []string{"mp3", "flac", "m4a"} {
		t.Run(extension, func(t *testing.T) {
			directory := t.TempDir()
			pictureType := "Back"
			if extension == "m4a" {
				pictureType = "Other"
			}
			pictures := []embeddedArtworkIntegrationImage{
				embeddedArtworkIntegrationWriteImage(t, directory, "corrupt", "png", pictureType, 26, 18),
			}
			if extension != "m4a" {
				pictures = append(pictures, embeddedArtworkIntegrationWriteImage(t, directory, "valid-front", "jpeg", "Front", 28, 20))
			}
			path := filepath.Join(directory, "corrupt-picture."+extension)
			embeddedArtworkIntegrationMux(t, ffmpeg, path, pictures)
			embeddedArtworkIntegrationAssertContainer(t, path, extension, pictures)
			embeddedArtworkIntegrationCorruptPNG(t, path, pictures[0].data)
			prober := Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 20 * time.Second}
			// Probe the already damaged source so the failure cannot be caused
			// merely by changing a valid file after its identity was recorded.
			file, info := embeddedArtworkIntegrationOpen(t, prober, path, pictures)
			if result, err := prober.ExtractEmbeddedArtwork(context.Background(), file, info); err == nil {
				t.Fatalf("a corrupt declared picture must fail the whole extraction, including when a valid front exists: %+v", result)
			}
			embeddedArtworkIntegrationAssertOffset(t, file)
		})
	}
}

func embeddedArtworkIntegrationWriteImage(t *testing.T, directory, name, format, pictureType string, width, height int) embeddedArtworkIntegrationImage {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.SetRGBA(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 11), B: uint8((x + y) * 5), A: 255})
		}
	}
	var data bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&data, source, &jpeg.Options{Quality: 87})
	case "png":
		err = png.Encode(&data, source)
	default:
		t.Fatalf("unsupported fixture image format %q", format)
	}
	if err != nil {
		t.Fatalf("encode fixture picture: %v", err)
	}
	path := filepath.Join(directory, name+"."+format)
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return embeddedArtworkIntegrationImage{path: path, data: data.Bytes(), mimeType: "image/" + format,
		pictureType: pictureType, width: width, height: height}
}

func embeddedArtworkIntegrationMux(t *testing.T, ffmpeg, path string, pictures []embeddedArtworkIntegrationImage) {
	t.Helper()
	args := []string{"-f", "lavfi", "-i", "sine=frequency=800:sample_rate=48000"}
	for _, picture := range pictures {
		args = append(args, "-i", picture.path)
	}
	args = append(args, "-map", "0:a:0")
	for index := range pictures {
		args = append(args, "-map", fmt.Sprintf("%d:v:0", index+1))
	}
	args = append(args, "-af", "atrim=end_sample=9600,asetpts=PTS-STARTPTS")
	switch filepath.Ext(path) {
	case ".mp3":
		args = append(args, "-c:a", "libmp3lame", "-b:a", "32k", "-id3v2_version", "3")
	case ".flac":
		args = append(args, "-c:a", "flac")
	case ".m4a":
		args = append(args, "-c:a", "aac", "-b:a", "32k")
	default:
		t.Fatalf("unsupported fixture container %q", filepath.Ext(path))
	}
	if len(pictures) > 0 {
		args = append(args, "-c:v", "copy")
	}
	for index, picture := range pictures {
		specifier := fmt.Sprintf(":v:%d", index)
		comment := "Other"
		switch picture.pictureType {
		case "Front":
			comment = "Cover (front)"
		case "Back":
			comment = "Cover (back)"
		}
		// Both ID3 APIC and native FLAC PICTURE muxing obtain the picture
		// type from this comment; a title alone does not set the FLAC type.
		args = append(args, "-disposition"+specifier, "attached_pic",
			"-metadata:s"+specifier, "comment="+comment,
			"-metadata:s"+specifier, "title="+filepath.Base(picture.path))
	}
	args = append(args, path)
	audioProbeRunFFmpeg(t, ffmpeg, args...)
}

func embeddedArtworkIntegrationOpen(t *testing.T, prober Prober, path string, pictures []embeddedArtworkIntegrationImage) (*os.File, Info) {
	t.Helper()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info, err := prober.ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatalf("probe embedded artwork fixture through its descriptor: %v", err)
	}
	embeddedArtworkIntegrationAssertOffset(t, file)
	if len(info.Streams) != len(pictures)+1 || info.Streams[0].Index != 0 || info.Streams[0].CodecType != "audio" {
		t.Fatalf("the fixture must retain one audio stream followed by its picture streams: %+v", info.Streams)
	}
	for index, picture := range pictures {
		stream := info.Streams[index+1]
		codec := "png"
		if picture.mimeType == "image/jpeg" {
			codec = "mjpeg"
		}
		if stream.Index != index+1 || stream.CodecType != "video" || !stream.IsAttachedPicture || stream.Codec != codec ||
			stream.Width != picture.width || stream.Height != picture.height {
			t.Fatalf("picture %d lost its actual source index, codec, attachment flag, or dimensions: %+v", index, stream)
		}
	}
	return file, info
}

func embeddedArtworkIntegrationAssertPictures(t *testing.T, result EmbeddedArtworkResult, source []embeddedArtworkIntegrationImage, indexes []int) {
	t.Helper()
	if result.Version != EmbeddedArtworkVersion || len(result.Pictures) != len(indexes) {
		t.Fatalf("unexpected extraction version or picture count: version=%d, pictures=%d, want version=%d, pictures=%d",
			result.Version, len(result.Pictures), EmbeddedArtworkVersion, len(indexes))
	}
	for position, index := range indexes {
		picture, original := result.Pictures[position], source[index-1]
		wantHash := fmt.Sprintf("%x", sha256.Sum256(original.data))
		if picture.StreamIndex != index || picture.PictureType != original.pictureType ||
			picture.MIMEType != original.mimeType || picture.Hash != wantHash ||
			picture.Width != original.width || picture.Height != original.height || !bytes.Equal(picture.Data, original.data) {
			t.Fatalf("picture %d did not preserve ordered source %d: index=%d type=%q MIME=%q hash=%q size=%dx%d bytes=%d; want type=%q MIME=%q hash=%q size=%dx%d bytes=%d",
				position, index, picture.StreamIndex, picture.PictureType, picture.MIMEType, picture.Hash, picture.Width, picture.Height, len(picture.Data),
				original.pictureType, original.mimeType, wantHash, original.width, original.height, len(original.data))
		}
	}
}

func embeddedArtworkIntegrationAssertOffset(t *testing.T, file *os.File) {
	t.Helper()
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 17 {
		t.Fatalf("the caller-owned descriptor was closed or its offset changed: %d, %v", position, err)
	}
}

func embeddedArtworkIntegrationAssertContainer(t *testing.T, path, extension string, pictures []embeddedArtworkIntegrationImage) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for index, picture := range pictures {
		if bytes.Count(data, picture.data) != 1 {
			t.Fatalf("source picture %d is not preserved once as original bytes in the %s container", index, extension)
		}
	}
	switch extension {
	case "mp3":
		if !bytes.HasPrefix(data, []byte("ID3")) || bytes.Count(data, []byte("APIC")) != len(pictures) {
			t.Fatal("the MP3 fixture must contain an ID3 APIC frame for every picture")
		}
	case "flac":
		if !bytes.HasPrefix(data, []byte("fLaC")) {
			t.Fatal("the FLAC fixture must contain native metadata blocks")
		}
		var pictureBlocks int
		for position := 4; ; {
			if position+4 > len(data) {
				t.Fatal("the FLAC fixture has an incomplete metadata header")
			}
			header := data[position : position+4]
			length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
			if position+4+length > len(data) {
				t.Fatal("the FLAC fixture has an incomplete metadata block")
			}
			if header[0]&0x7f == 6 {
				pictureBlocks++
			}
			if header[0]&0x80 != 0 {
				break
			}
			position += 4 + length
		}
		if pictureBlocks != len(pictures) {
			t.Fatalf("the FLAC fixture must contain %d native PICTURE blocks, got %d", len(pictures), pictureBlocks)
		}
	case "m4a":
		if !bytes.Contains(data, []byte("covr")) {
			t.Fatal("the M4A fixture must contain a covr atom")
		}
	}
}

func embeddedArtworkIntegrationCorruptPNG(t *testing.T, path string, original []byte) {
	t.Helper()
	damaged := bytes.Clone(original)
	corrupted := false
	for position := 8; position+12 <= len(damaged); {
		length := int(binary.BigEndian.Uint32(damaged[position : position+4]))
		end := position + 12 + length
		if end > len(damaged) {
			t.Fatal("the PNG fixture has an incomplete chunk")
		}
		if string(damaged[position+4:position+8]) == "IDAT" {
			// Keep all image and container lengths intact, including IHDR,
			// while invalidating the compressed image data's checksum.
			damaged[end-1] ^= 0xff
			corrupted = true
			break
		}
		position = end
	}
	if !corrupted {
		t.Fatal("the PNG fixture has no IDAT chunk")
	}
	if _, err := png.Decode(bytes.NewReader(damaged)); err == nil {
		t.Fatal("the damaged PNG fixture is still decodable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(data, original) != 1 {
		t.Fatal("the container does not contain exactly one original picture to corrupt")
	}
	data = bytes.Replace(data, original, damaged, 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
