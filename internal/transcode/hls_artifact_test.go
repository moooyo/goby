package transcode

import (
	"fmt"
	"strings"
	"testing"
)

func TestHLSArtifactClassifiesGeneratedFiles(t *testing.T) {
	cases := map[string]string{
		"main.m3u8":      "playlist",
		"init.mp4":       "init",
		"segment-0.ts":   "segment",
		"segment-999.ts": "segment",
	}
	cases["segment-"+strings.Repeat("0", 244)+".ts"] = "segment"
	for variant := 0; variant < 4; variant++ {
		cases[fmt.Sprintf("v%d.m3u8", variant)] = "playlist"
		cases[fmt.Sprintf("v%d-init.mp4", variant)] = "init"
	}
	for _, prefix := range []string{"", "v0-", "v1-", "v2-", "v3-"} {
		for _, extension := range []string{"ts", "m4s", "aac", "mp3", "vtt"} {
			kind := "segment"
			if extension == "vtt" {
				kind = "subtitle"
			}
			for _, number := range []string{"000000", "000001", "1000000", "2147483647"} {
				cases[prefix+"segment-"+number+"."+extension] = kind
			}
		}
	}
	for name, want := range cases {
		if kind, ok := HLSArtifact(name); !ok || kind != want {
			t.Errorf("HLSArtifact(%q) = (%q, %v), want (%q, true)", name, kind, ok, want)
		}
	}
}

func TestHLSArtifactRejectsPathsAndUnpublishedFiles(t *testing.T) {
	for _, name := range []string{
		"", "master.m3u8", "segment-list.m3u8", "main.m3u8.tmp", "main.m3u8.publish.tmp",
		"v4.m3u8", "v00.m3u8", "V0.m3u8", "v-1.m3u8", "v0-main.m3u8",
		"init.MP4", "v4-init.mp4", "v00-init.mp4", "init.mp4.tmp", "v0-init.mp4.tmp",
		"segment-.ts", "segment--1.ts", "segment-000001.TS", "segment-000001.mp4",
		"segment-000000.m4s.tmp", "segment-0.m4s", "v0-segment-0.ts", "v4-segment-000000.ts",
		"segment-2147483648.m4s", "segment-00000000000.aac", "segment-00000.mp3",
		"v0-segment-000000.vtt.tmp", "v0-segment-000000.vtt?token=value",
		"v0-segment-000000.vtt#fragment", "segment-00000١.ts", "segment-00000１.ts",
		"segment-000000.ts/extra", "segment-000000.ts\x00", "segment-000000.ts\n",
		"segment-" + strings.Repeat("0", 245) + ".ts",
		"../init.mp4", "./main.m3u8", "/main.m3u8", `C:\init.mp4`, `v0\init.mp4`,
		"%2e%2e%2finit.mp4", "segment-%30%30%30%30%30%30.ts", "https://example.test/init.mp4",
	} {
		if kind, ok := HLSArtifact(name); ok || kind != "" {
			t.Errorf("HLSArtifact(%q) = (%q, %v), want rejection", name, kind, ok)
		}
	}
}
