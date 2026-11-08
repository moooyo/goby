//go:build linux

package backuppg

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBackupCommandArtworkProjectionUsesOnlyCompiledDataExclusions(t *testing.T) {
	tool := commandFixture(t, "import json, sys\njson.dump(sys.argv[1:], sys.stdout)")
	options := Options{PGDump: tool, SourceURL: commandFixtureURI, Schema: "public", OmitManagedArtwork: true}
	var output bytes.Buffer
	if err := dumpCommand(t.Context(), options, commandFixtureSnapshot, &output); err != nil {
		t.Fatal(err)
	}
	var arguments []string
	if err := json.Unmarshal(output.Bytes(), &arguments); err != nil {
		t.Fatal("read the fixture's exact pg_dump arguments")
	}
	var exclusions []string
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "--exclude") {
			exclusions = append(exclusions, argument)
		}
	}
	if len(exclusions) != 4 || exclusions[0] != `--exclude-table-data="public"."artwork_state"` || exclusions[1] != `--exclude-table-data="public"."artwork_images"` ||
		exclusions[2] != `--exclude-table-data="public"."item_provider_images"` || exclusions[3] != `--exclude-table-data="public"."item_embedded_artwork"` {
		t.Fatal("artwork projection changed schema selection or excluded unrelated table data")
	}
	options.OmitManagedArtwork = false
	output.Reset()
	if err := dumpCommand(t.Context(), options, commandFixtureSnapshot, &output); err != nil || bytes.Contains(output.Bytes(), []byte("--exclude")) {
		t.Fatalf("the default backup unexpectedly excludes artwork: %v", err)
	}
}
