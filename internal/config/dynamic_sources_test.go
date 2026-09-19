package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestDynamicSourceConfigurationBoundaries(t *testing.T) {
	valid := `[{"itemId":"0123456789abcdef0123456789abcdef","url":"https://media.example/stream","headers":{"Authorization":"Bearer private-value"},"infinite":true,"maxReconnects":2}]`
	definitions, err := parseDynamicSources([]byte(valid))
	if err != nil || len(definitions) != 1 || !definitions[0].Infinite {
		t.Fatalf("valid registered source: %v", err)
	}
	for _, invalid := range []string{
		`null`, `{}`, valid + ` []`,
		strings.Replace(valid, `"infinite":true`, `"unknown":true`, 1),
		strings.Replace(valid, `https://media.example/stream`, `file:///etc/passwd`, 1),
		strings.Replace(valid, `https://media.example/stream`, `https://user:private-value@media.example/stream`, 1),
		strings.Replace(valid, `"Authorization"`, `"Host"`, 1),
		strings.Replace(valid, `"maxReconnects":2`, `"maxReconnects":6`, 1),
		strings.Replace(valid, `"0123456789abcdef0123456789abcdef"`, `"unregistered"`, 1),
		"[" + strings.Trim(valid, "[]") + "," + strings.Trim(valid, "[]") + "]",
	} {
		if _, err := parseDynamicSources([]byte(invalid)); err == nil {
			t.Fatal("invalid dynamic-source configuration accepted")
		}
	}
	for _, output := range []string{fmt.Sprint(definitions), fmt.Sprintf("%+v", definitions), fmt.Sprintf("%#v", definitions)} {
		if strings.Contains(output, "private-value") || strings.Contains(output, "media.example") {
			t.Fatal("private source configuration escaped through formatting")
		}
	}
}

func TestDynamicSubtitleConfigurationUsesExplicitClocksAndPrivateRequests(t *testing.T) {
	valid := `[{"itemId":"0123456789abcdef0123456789abcdef","url":"https://media.example/stream","subtitles":[{"id":"english","name":"English","language":"eng","format":"webvtt","mode":"webvtt-hls","clock":"mpegts","segmentClock":"timestamp-map","url":"https://captions.example/private-token/index.m3u8","headers":{"Authorization":"Bearer subtitle-secret"},"offsetTicks":5000000,"default":true}]}]`
	definitions, err := parseDynamicSources([]byte(valid))
	if err != nil || len(definitions) != 1 || len(definitions[0].Subtitles) != 1 {
		t.Fatalf("valid dynamic subtitle declaration: %v", err)
	}
	subtitle := definitions[0].Subtitles[0]
	if subtitle.ID != "english" || subtitle.Mode != "webvtt-hls" || subtitle.Clock != "mpegts" || subtitle.SegmentClock != "timestamp-map" || subtitle.OffsetTicks != 5000000 {
		t.Fatal("configuration lost its explicit subtitle delivery and clock")
	}
	for _, invalid := range []string{
		strings.Replace(valid, `"clock":"mpegts",`, ``, 1),
		strings.Replace(valid, `"clock":"mpegts"`, `"clock":"wallclock"`, 1),
		strings.Replace(valid, `,"segmentClock":"timestamp-map"`, ``, 1),
		strings.Replace(valid, `"segmentClock":"timestamp-map"`, `"segmentClock":"automatic"`, 1),
		strings.Replace(valid, `"clock":"mpegts"`, `"clock":"media"`, 1),
		strings.Replace(valid, `"format":"webvtt"`, `"format":"pgs"`, 1),
		strings.Replace(valid, `"mode":"webvtt-hls"`, `"mode":"auto"`, 1),
		strings.Replace(valid, `"id":"english"`, `"id":""`, 1),
		strings.Replace(valid, `"offsetTicks":5000000`, `"offsetTicks":864000000001`, 1),
		strings.Replace(valid, `https://captions.example/private-token/index.m3u8`, `file:///tmp/subtitles.vtt`, 1),
		strings.Replace(valid, `"Authorization":"Bearer subtitle-secret"`, `"Content-Type":"text/vtt"`, 1),
		strings.Replace(valid, `"Authorization":"Bearer subtitle-secret"`, `"Host":"captions.example"`, 1),
		strings.Replace(valid, `"default":true`, `"default":true,"unrecognized":1`, 1),
	} {
		if _, err := parseDynamicSources([]byte(invalid)); err == nil {
			t.Fatal("invalid or unrestricted subtitle configuration accepted")
		} else if strings.Contains(err.Error(), "subtitle-secret") || strings.Contains(err.Error(), "private-token") {
			t.Fatal("configuration error leaked the private subtitle request")
		}
	}
	stream := strings.Replace(valid, `"mode":"webvtt-hls"`, `"mode":"webvtt-stream"`, 1)
	stream = strings.Replace(stream, `"segmentClock":"timestamp-map"`, `"streamWatermarks":"goby-note-v1"`, 1)
	if _, err := parseDynamicSources([]byte(stream)); err != nil {
		t.Fatal("explicit stream watermark contract was rejected", err)
	}
	for _, invalid := range []string{
		strings.Replace(stream, `,"streamWatermarks":"goby-note-v1"`, ``, 1),
		strings.Replace(stream, `"streamWatermarks":"goby-note-v1"`, `"streamWatermarks":"automatic"`, 1),
		strings.Replace(stream, `"mode":"webvtt-stream"`, `"mode":"document"`, 1),
	} {
		if _, err := parseDynamicSources([]byte(invalid)); err == nil {
			t.Fatal("unproven or irrelevant stream watermark settings were accepted")
		}
	}
	for _, output := range []string{fmt.Sprint(definitions), fmt.Sprintf("%+v", definitions), fmt.Sprintf("%#v", definitions), fmt.Sprintf("%+v", subtitle)} {
		if strings.Contains(output, "subtitle-secret") || strings.Contains(output, "captions.example") {
			t.Fatal("subtitle configuration escaped through formatting")
		}
	}
}
