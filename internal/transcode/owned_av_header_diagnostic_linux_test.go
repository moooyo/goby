//go:build linux

package transcode

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// This task-owned harness records real headers without qualifying an endpoint.
func TestOwnedGeneratedAVHeaderDiagnostic(t *testing.T) {
	directory := os.Getenv("GOBY_OWNED_AV_HEADER_DIAGNOSTIC_DIR")
	if directory == "" {
		t.Skip("explicit task-owned AV header diagnostic is required")
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		t.Fatal("invalid evidence directory")
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal("fresh evidence directory is required")
	}
	ctx, ffmpeg, ffprobe := generatedWindowMediaTools(t)
	for _, fixture := range []struct {
		name              string
		seconds, channels int
		extra             bool
	}{
		{"native-mono", 24, 1, false}, {"native-stereo", 24, 2, false}, {"extra-video", 2, 2, true},
	} {
		path := generatedAVSourceMediaFixture(t, ctx, ffmpeg, fixture.seconds, fixture.channels, fixture.extra, 48000, 0)
		before, err := os.Stat(path)
		if err != nil || before.Size() <= 0 || before.Size() > 16<<20 {
			t.Fatal("source budget or stat failed")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		parser := generatedMP4EndpointParser{ctx: ctx}
		tracks := make([]map[string]any, 0, 3)
		if err := parser.walk(data, func(top generatedMP4Box) error {
			if top.kind != "moov" {
				return nil
			}
			return parser.walk(top.body, func(child generatedMP4Box) error {
				if child.kind != "trak" {
					return nil
				}
				if len(tracks) == 3 {
					return fmt.Errorf("track diagnostic budget exceeded")
				}
				fact := make(map[string]any)
				err := parser.walk(child.body, func(field generatedMP4Box) error {
					switch field.kind {
					case "tkhd":
						body := field.body
						if len(body) != 84 && len(body) != 96 {
							return fmt.Errorf("unknown track header shape")
						}
						version := body[0]
						idOffset, groupOffset := 12, 34
						if version == 1 {
							idOffset, groupOffset = 20, 46
						} else if version != 0 {
							return fmt.Errorf("unknown header version")
						}
						if version == 0 && len(body) != 84 || version == 1 && len(body) != 96 {
							return fmt.Errorf("header size disagrees with version")
						}
						fact["BodyHex"] = hex.EncodeToString(body)
						fact["Version"] = version
						fact["Flags"] = binary.BigEndian.Uint32(body[:4]) & 0xffffff
						fact["TrackID"] = binary.BigEndian.Uint32(body[idOffset : idOffset+4])
						fact["AlternateGroup"] = binary.BigEndian.Uint16(body[groupOffset : groupOffset+2])
					case "mdia":
						return parser.walk(field.body, func(mediaField generatedMP4Box) error {
							if mediaField.kind == "hdlr" {
								if len(mediaField.body) < 12 {
									return fmt.Errorf("short media handler")
								}
								fact["Handler"] = string(mediaField.body[8:12])
							}
							return nil
						})
					}
					return nil
				})
				if err != nil {
					return err
				}
				tracks = append(tracks, fact)
				return nil
			})
		}); err != nil {
			t.Fatal(err)
		}
		projection := generatedAVSourceMediaCommand(t, ctx, ffprobe, "-v", "error", "-threads", "1", "-fflags", "+nofillin-genpts",
			"-show_streams", "-show_format", "-show_entries", "stream=index,id,codec_type,codec_name,start_pts,time_base:stream_tags=:stream_disposition=:stream_side_data=:format=start_time,duration:format_tags=", "-of", "json", path)
		if !json.Valid(projection) {
			t.Fatal("invalid projection JSON")
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
			t.Fatal("source changed during diagnostic")
		}
		digest := sha256.Sum256(data)
		result := map[string]any{"Marker": "goby-owned-av-header-diagnostic-v1", "Fixture": fixture.name,
			"Qualified": false, "SourceSHA256": hex.EncodeToString(digest[:]), "Tracks": tracks, "Projection": json.RawMessage(projection)}
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil || len(encoded) > 64<<10 {
			t.Fatal("diagnostic encoding budget exceeded")
		}
		file, err := os.OpenFile(filepath.Join(directory, fixture.name+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.Write(encoded)
		syncErr, closeErr := file.Sync(), file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			t.Fatal("diagnostic evidence persistence failed")
		}
		t.Logf("actual AV header facts: fixture=%s tracks=%d qualified=false", fixture.name, len(tracks))
	}
}
