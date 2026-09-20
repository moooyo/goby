package media

import (
	"bytes"
	"errors"
	"image/jpeg"
	"testing"
)

func TestAnalysisPreviewQualityControlsTheActualJPEG(t *testing.T) {
	const width, height = 32, 24
	pixels := make([]byte, width*height*3)
	for index := range pixels {
		pixels[index] = byte(index*71 + index/7)
	}
	outputs := map[int][]byte{}
	for _, quality := range []int{0, 40, 80, 95} {
		data, err := analysisEncodePreviewJPEG(pixels, width, height, quality, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != width || config.Height != height {
			t.Fatalf("quality %d produced invalid JPEG: %+v %v", quality, config, err)
		}
		outputs[quality] = data
	}
	if !bytes.Equal(outputs[0], outputs[80]) {
		t.Fatal("zero quality does not mean the documented default 80")
	}
	if bytes.Equal(outputs[40], outputs[80]) || bytes.Equal(outputs[80], outputs[95]) {
		t.Fatal("different admitted qualities did not reach the JPEG encoder")
	}
	if _, err := analysisEncodePreviewJPEG(pixels, width, height, 95, int64(len(outputs[95])-1)); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("quality-specific JPEG output exceeded its byte budget: %v", err)
	}
}

func TestAnalysisPreviewRejectsUnsupportedQuality(t *testing.T) {
	for _, quality := range []int{-1, 1, 39, 96, 100} {
		if _, err := analysisPreviewQuality(quality); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("quality %d was admitted: %v", quality, err)
		}
		if _, err := analysisEncodePreviewJPEG(make([]byte, 3), 1, 1, quality, 1024); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("quality %d reached encoding: %v", quality, err)
		}
	}
}
