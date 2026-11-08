//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPAdminMediaInfoUsesNativeAuthorityAndIndexedFacts(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	target := "/admin/v1/items/" + f.itemID + "/media-info"
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {f.adminToken}}, {"X-Emby-Token": {f.viewerToken}}} {
		expectStatus(t, f.request(t, http.MethodGet, target, nil, headers), http.StatusUnauthorized)
	}
	// This endpoint must keep working without observing the source path again.
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=jsonb_build_object(
		'ProbeVersion',8,'Container','matroska','DurationTicks',1500000000::bigint,'Size',9007199254740993::bigint,
		'Bitrate',30000000,'VideoSeekIndexes',jsonb_build_array(jsonb_build_object('private','decoder-evidence')),
		'Streams',jsonb_build_array(jsonb_build_object('Index',7,'CodecType','video','Codec','hevc','Width',3840,'Height',2160),
		jsonb_build_object('Index',11,'CodecType','audio','Codec','aac','Channels',6,'IsDefault',true,'Filename','private-attachment-name')),
		'Chapters',jsonb_build_array(jsonb_build_object('StartTicks',0,'EndTicks',10000000,'Title','Opening'),
		jsonb_build_object('StartTicks',10000000,'EndTicks',20000000,'Title','Second'))),
		path='/unavailable-media-for-index-only-read.mkv' WHERE id=$1`, f.itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO item_subtitles(item_id,root_id,stream_index,relative_path,file_identity,
		source_hash,file_size,modified_at,change_time_ns,codec,language,title,is_default,is_forced,is_hearing_impaired,mime_type)
		SELECT id,root_id,100000,'private-caption-sidecar.vtt','private-caption-identity',repeat('a',64),100,clock_timestamp(),1,
		'vtt','eng','English SDH',true,false,true,'text/vtt' FROM items WHERE id=$1`, f.itemID); err != nil {
		t.Fatal(err)
	}
	before := f.playbackCounts(t)
	response := f.request(t, http.MethodGet, target, nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	info := jsonObject(t, response)
	summary := objectValue(t, info, "Summary")
	if info["Id"] != f.itemID || info["LibraryId"] != f.libraryID || summary["Size"] != "9007199254740993" || summary["DurationTicks"] != "1500000000" {
		t.Fatalf("media information did not preserve identity or exact facts: %#v", info)
	}
	if len(info["Streams"].([]any)) != 3 || len(info["Chapters"].([]any)) != 2 || info["StreamsTruncated"] != false || info["ChaptersTruncated"] != false {
		t.Fatal("media information omitted indexed tracks or chapters")
	}
	caption := info["Streams"].([]any)[2].(map[string]any)
	if caption["Index"] != float64(100000) || caption["Codec"] != "webvtt" || caption["IsExternal"] != true || caption["IsHearingImpaired"] != true || caption["IsDefault"] != true {
		t.Fatalf("indexed external captions were not retained: %#v", caption)
	}
	for _, private := range []string{"private-attachment-name", "private-caption", "decoder-evidence", "VideoSeekIndexes", "/unavailable-media"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("display DTO exposed private or filesystem facts %q", private)
		}
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=jsonb_set(media,'{Streams}',jsonb_build_array(
		jsonb_build_object('Index',3,'CodecType','video','Codec','mjpeg','IsAttachedPicture',true),
		jsonb_build_object('Index',4,'CodecType','audio','Codec','mp3','Channels',2)) || (media->'Streams')) WHERE id=$1`, f.itemID); err != nil {
		t.Fatal(err)
	}
	listing := f.request(t, http.MethodGet, "/admin/v1/libraries/"+f.libraryID+"/items?SearchTerm=Automatic+Alpha", nil, nil, f.cookie)
	expectStatus(t, listing, http.StatusOK)
	var page struct{ Items []map[string]json.RawMessage }
	if err := json.Unmarshal(listing.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || string(page.Items[0]["Media"]) == "null" || len(page.Items[0]["Media"]) == 0 {
		t.Fatalf("metadata page omitted batched technical summaries: %#v, %v", page, err)
	}
	var pageMedia struct {
		Video struct{ Index int }
		Audio struct{ Index int }
	}
	if err := json.Unmarshal(page.Items[0]["Media"], &pageMedia); err != nil || pageMedia.Video.Index != 7 || pageMedia.Audio.Index != 11 {
		t.Fatalf("bounded summary selected an attached cover or ignored the default audio track: %#v, %v", pageMedia, err)
	}
	if after := f.playbackCounts(t); after != before {
		t.Fatal("read-only media projection changed playback state")
	}
	expectStatus(t, f.request(t, http.MethodGet, target+"?unexpected=1", nil, nil, f.cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/items/missing-media-item/media-info", nil, nil, f.cookie), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=(SELECT id FROM sessions WHERE user_id=$1 AND kind='admin' ORDER BY created_at DESC LIMIT 1)", f.adminID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, target, nil, nil, f.cookie), http.StatusUnauthorized)
}
