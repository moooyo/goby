package server

import (
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedWindowResourceRolesPreserveWindowAndVariantIdentity(t *testing.T) {
	for _, segmentType := range []string{"mpegts", "fmp4"} {
		for _, count := range []int{0, 2, transcode.MaxHLSRenditions} {
			plan := transcode.Plan{HLS: transcode.HLSPlan{SegmentType: segmentType, RenditionCount: count}}
			for variant := 0; variant < max(1, count); variant++ {
				for _, initialization := range []bool{false, true} {
					resource := hlsGeneratedWindowResource{number: 15, variant: variant, initialization: initialization}
					name := hlsGeneratedWindowResourceName(plan, resource)
					if segmentType == "mpegts" && initialization {
						if name != "" {
							t.Fatal("a TS presentation acquired an initialization admission role")
						}
						continue
					}
					parsed, valid := hlsGeneratedWindowResourceFromName(plan, name)
					if !valid || parsed != resource {
						t.Fatalf("logical resource lost its source slot, rendition or map role: %q, %+v", name, parsed)
					}
					if _, exact := transcode.HLSArtifact(name); exact {
						t.Fatal("an admission resource was accepted as exact manager bytes")
					}
					for _, alias := range []string{"../" + name, name + "?window=15", name + ".ts", "v01-" + name,
						fmt.Sprintf("v%d-window-init-000015.mp4", max(1, count)), "window-segment-15.m4s", "window-init-+00015.mp4"} {
						if _, valid := hlsGeneratedWindowResourceFromName(plan, alias); valid {
							t.Fatalf("a resource alias or foreign rendition acquired an admission role: %q", alias)
						}
					}
				}
			}
		}
	}
}

func TestGeneratedWindowResourceRolesRejectOtherContainersAndMapConfusion(t *testing.T) {
	for _, segmentType := range []string{"", "packed", "unknown"} {
		plan := transcode.Plan{HLS: transcode.HLSPlan{SegmentType: segmentType}}
		for _, name := range []string{"window-segment-000015.ts", "window-segment-000015.m4s", "window-init-000015.mp4"} {
			if _, valid := hlsGeneratedWindowResourceFromName(plan, name); valid {
				t.Fatalf("unsupported output admitted a logical resource: %s/%s", segmentType, name)
			}
		}
	}
	for segmentType, names := range map[string][]string{
		"mpegts": {"window-init-000015.mp4", "window-segment-000015.m4s"},
		"fmp4":   {"window-segment-000015.ts", "window-init-000015.m4s", "window-segment-000015.mp4", "init.mp4", "segment-000015.m4s"},
	} {
		plan := transcode.Plan{HLS: transcode.HLSPlan{SegmentType: segmentType}}
		for _, name := range names {
			if _, valid := hlsGeneratedWindowResourceFromName(plan, name); valid {
				t.Fatalf("logical map/media roles crossed output containers or exact filenames: %s/%s", segmentType, name)
			}
		}
	}
}
