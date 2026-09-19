//go:build linux

package transcode

import (
	"context"
	"math"
	"testing"
)

// The opt-in fixture is the original neutral PQ chart produced by the pinned
// fixture generator. Its nonidentity luma curve raises black and reduces the
// brightest stripe. This tests useful signal properties, not Dolby certification
// or a colorimetric match to a proprietary reference renderer.
func verifyDolbyVisionNeutralChart(t *testing.T, ctx context.Context, ffmpeg, control, converted, outputRange string) {
	t.Helper()
	decode := func(path string) [8][3]float64 {
		pixels := progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-xerror", "-nostdin",
			"-threads", "1", "-i", path, "-map", "0:v:0", "-frames:v", "1", "-filter_threads", "1",
			"-pix_fmt", "rgb24", "-threads", "1", "-f", "rawvideo", "-")
		if len(pixels) != 320*192*3 {
			t.Fatalf("neutral chart has unexpected decoded geometry: %d RGB bytes", len(pixels))
		}
		var means [8][3]float64
		for stripe := range means {
			for y := 28; y < 36; y++ {
				for x := stripe*40 + 16; x < stripe*40+24; x++ {
					for channel := range means[stripe] {
						means[stripe][channel] += float64(pixels[(y*320+x)*3+channel]) / 64
					}
				}
			}
		}
		return means
	}
	before, after := decode(control), decode(converted)
	intensity := func(rgb [3]float64) float64 { return (rgb[0] + rgb[1] + rgb[2]) / 3 }
	for index, rgb := range after {
		if math.Max(rgb[0], math.Max(rgb[1], rgb[2]))-math.Min(rgb[0], math.Min(rgb[1], rgb[2])) > 12 {
			t.Fatalf("neutral Dolby Vision chart acquired a color cast at stripe %d: RGB=%v", index, rgb)
		}
		if index > 0 && intensity(rgb)+3 < intensity(after[index-1]) {
			t.Fatalf("Dolby Vision conversion reversed neutral brightness order: %v", after)
		}
	}
	if intensity(after[7])-intensity(after[0]) < 20 ||
		intensity(after[0]) <= intensity(before[0])+.5 {
		t.Fatalf("Dolby Vision luma reshaping disagrees with the authored curve: control=%v converted=%v", before, after)
	}
	// HDR10 retains the PQ signal domain in which the authored polynomial
	// lowers the brightest stripe. SDR also applies BT.2390 display mapping.
	// The no-RPU control disables Dolby Vision color/HDR interpretation as well
	// as reshaping; it is not the same display mapping with one polynomial
	// removed. Do not assert PQ-domain highlight ordering across those SDR
	// outputs. The production graph explicitly disables peak detection.
	if outputRange == "hdr10" && intensity(after[7]) >= intensity(before[7])-.5 {
		t.Fatalf("HDR10 output did not reduce the authored bright PQ stripe: control=%v converted=%v", before, after)
	}
	t.Logf("neutral chart RGB means: control=%v converted=%v", before, after)
}
