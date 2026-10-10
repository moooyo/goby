package library

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestDecodedAcceptedMusicMetadataPreservesUnextractedBytes(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte(" \n null \t"), []byte(" { \n } ")} {
		music, extracted, err := decodeAcceptedMusicSource(raw)
		if err != nil || extracted {
			t.Fatalf("unextracted snapshot was rejected or promoted: extracted=%t error=%v", extracted, err)
		}
		local := []byte("uninterpreted local source bytes")
		merged, err := mergeAcceptedMusicMetadata(local, music, extracted)
		if err != nil || !bytes.Equal(merged, local) {
			t.Fatalf("unextracted merge interpreted or changed local bytes: merged=%q error=%v", merged, err)
		}
		merged[0] = 'X'
		if local[0] != 'u' {
			t.Fatal("unextracted merge exposed the caller's local byte storage")
		}
		if hash, err := acceptedMusicMetadataHash(music, extracted); err != nil || hash != "" {
			t.Fatalf("unextracted snapshot acquired a canonical hash: hash=%q error=%v", hash, err)
		}
	}
}

func TestDecodedAcceptedMusicMetadataKeepsCanonicalFactsAndLocalIsolation(t *testing.T) {
	raw := []byte(` { "Artists" : [" A ","A"," A ",""], "AlbumArtists" : [" Ensemble "], "Name" : "  Embedded title  ", "Version" : 1 } `)
	music, extracted, err := decodeAcceptedMusicSource(raw)
	if err != nil || !extracted {
		t.Fatalf("decode extracted snapshot: extracted=%t error=%v", extracted, err)
	}
	hash, err := acceptedMusicMetadataHash(music, extracted)
	if err != nil || hash == "" {
		t.Fatalf("hash extracted snapshot: hash=%q error=%v", hash, err)
	}
	wantHash, err := acceptedMusicSourceHash([]byte(`{"Version":1,"Name":"  Embedded title  ","Artists":[" A ","A",""],"AlbumArtists":[" Ensemble "]}`))
	if err != nil || hash != wantHash {
		t.Fatalf("typed hash lost canonical decoded facts: got=%q want=%q error=%v", hash, wantHash, err)
	}
	local := []byte(`{"Name":"  Local title  ","Album":"Stale album","Artists":["Stale artist"],"FutureNumber":90071992547409931234}`)
	before := bytes.Clone(local)
	name, err := acceptedMusicMetadataName(local, music, "Fallback")
	if err != nil || name != "  Local title  " {
		t.Fatalf("typed name changed NFO priority or exact text: name=%q error=%v", name, err)
	}
	merged, err := mergeAcceptedMusicMetadata(local, music, extracted)
	if err != nil {
		t.Fatal(err)
	}
	object := metadataTestObject(t, merged)
	metadataMusicSourceAssertString(t, object, "Name", name)
	metadataMusicSourceAssertNames(t, object, "Artists", []string{" A ", "A", ""})
	metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{" Ensemble "})
	if _, exists := object["Album"]; exists || string(object["FutureNumber"]) != "90071992547409931234" || !bytes.Equal(local, before) {
		t.Fatalf("typed merge changed sparse-local semantics: merged=%s local=%s", merged, local)
	}
	// Reusing the decoded music must not retain the mutable local map created
	// by another merge or name selection.
	name, err = acceptedMusicMetadataName(nil, music, "Fallback")
	if err != nil || name != "  Embedded title  " {
		t.Fatalf("typed helper retained an earlier local name: name=%q error=%v", name, err)
	}
	empty, extracted, err := decodeAcceptedMusicSource([]byte(`{"Version":1}`))
	if err != nil || !extracted {
		t.Fatal("extracted empty facts were treated as unread")
	}
	if hash, err := acceptedMusicMetadataHash(empty, extracted); err != nil || hash == "" {
		t.Fatalf("extracted empty facts lost their canonical hash: hash=%q error=%v", hash, err)
	}
	merged, err = mergeAcceptedMusicMetadata(local, empty, extracted)
	if err != nil {
		t.Fatal(err)
	}
	object = metadataTestObject(t, merged)
	metadataMusicSourceAssertNames(t, object, "Artists", []string{})
	metadataMusicSourceAssertNames(t, object, "AlbumArtists", []string{})
	if _, exists := object["Album"]; exists {
		t.Fatal("extracted empty facts retained the stale local album")
	}
}

func BenchmarkAcceptedMusicMetadataSynchronization(b *testing.B) {
	source := musicMetadataSource{Version: 1, Name: "Embedded title", Album: "Embedded album",
		Artists: []string{"Artist one", "Artist two"}, AlbumArtists: []string{"Album artist"}}
	for index := range 64 {
		source.Genres = append(source.Genres, fmt.Sprintf("Genre %02d", index))
	}
	populated, err := json.Marshal(source)
	if err != nil {
		b.Fatal(err)
	}
	local := []byte(`{"Name":"Local title","SortName":"Local sort","FutureNumber":90071992547409931234}`)
	for _, scenario := range []struct {
		name string
		raw  []byte
	}{
		{name: "Unextracted", raw: []byte(`{}`)},
		{name: "ExtractedEmpty", raw: []byte(`{"Version":1}`)},
		{name: "Populated", raw: populated},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			b.Run("StandaloneDecodes", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					hash, err := acceptedMusicSourceHash(scenario.raw)
					if err != nil {
						b.Fatal(err)
					}
					if hash != "" {
						if _, err := acceptedMusicName(local, scenario.raw, "Fallback"); err != nil {
							b.Fatal(err)
						}
					}
					if _, err := mergeAcceptedMusicSource(local, scenario.raw); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("SingleDecode", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					music, extracted, err := decodeAcceptedMusicSource(scenario.raw)
					if err != nil {
						b.Fatal(err)
					}
					hash, err := acceptedMusicMetadataHash(music, extracted)
					if err != nil {
						b.Fatal(err)
					}
					if hash != "" {
						if _, err := acceptedMusicMetadataName(local, music, "Fallback"); err != nil {
							b.Fatal(err)
						}
					}
					if _, err := mergeAcceptedMusicMetadata(local, music, extracted); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
