package server

import (
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/providers"
)

func TestRemoteSubtitleSelectionBindsSessionItemSourceAndExpiry(t *testing.T) {
	claims := subtitleSelectionClaims{ItemID: "item", SessionID: "session", SourceTag: "source-snapshot", Expires: time.Now().Add(time.Minute).Unix(), Selection: providers.RemoteSubtitle{Provider: "opensubtitles", ID: "12", FileID: 34, Language: "en", Name: "Unneeded filename"}}
	token, err := encodeSubtitleSelection(claims)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeSubtitleSelection(token, "item", "session")
	if err != nil || decoded.SourceTag != claims.SourceTag || decoded.Selection.FileID != 34 || decoded.Selection.Name != "" {
		t.Fatalf("round trip failed: %#v, %v", decoded, err)
	}
	if _, err := decodeSubtitleSelection(token, "other-item", "session"); err == nil {
		t.Fatal("token accepted for another item")
	}
	if _, err := decodeSubtitleSelection(token, "item", "other-session"); err == nil {
		t.Fatal("token accepted for another session")
	}
	parts := strings.Split(token, ".")
	if _, err := decodeSubtitleSelection(parts[0]+".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "item", "session"); err == nil {
		t.Fatal("token accepted forged MAC")
	}
	claims.Expires = time.Now().Add(-time.Minute).Unix()
	token, err = encodeSubtitleSelection(claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeSubtitleSelection(token, "item", "session"); err == nil {
		t.Fatal("expired token accepted")
	}
}
