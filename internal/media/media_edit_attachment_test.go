package media

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMediaEditOpaqueAttachmentRequiresCompleteDeclaredByteProof(t *testing.T) {
	source := mediaEditTestDocument(t)
	delete(source.Streams[3], "codec_name")
	if err := source.admit("mkv", 7); err != nil {
		t.Fatalf("fully proved opaque Matroska attachment rejected: %v", err)
	}
	candidate := mediaEditTestCandidate(t, source)
	_, streams, err := compareMediaEditDocuments(source, candidate, "mkv", 7)
	if err != nil || len(streams) != 3 || streams[2].CodecType != "attachment" || streams[2].ExtradataSHA256 != strings.Repeat("c", 64) {
		t.Fatalf("opaque attachment proof was not retained independently: %+v %v", streams, err)
	}
	for _, change := range []string{"bytes", "length", "name", "mime", "codec_fact"} {
		t.Run(change, func(t *testing.T) {
			candidate := mediaEditTestCandidate(t, source)
			attachment := candidate.Streams[2]
			switch change {
			case "bytes":
				attachment["extradata_hash"] = "SHA256:" + strings.Repeat("d", 64)
			case "length":
				attachment["extradata_size"] = json.Number("11")
			case "name":
				attachment["tags"].(map[string]any)["filename"] = "Different.ttf"
			case "mime":
				attachment["tags"].(map[string]any)["mimetype"] = "application/octet-stream"
			case "codec_fact":
				attachment["codec_name"] = "ttf"
			}
			if _, _, err := compareMediaEditDocuments(source, candidate, "mkv", 7); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("changed opaque attachment accepted: %v", err)
			}
		})
	}
}

func TestMediaEditOpaqueAttachmentDoesNotAdmitUnknownMediaOrIncompleteDeclarations(t *testing.T) {
	for _, change := range []string{"video", "audio", "subtitle", "data", "empty_bytes", "oversized_bytes", "missing_hash", "bad_hash", "missing_name", "missing_mime", "bad_mime"} {
		t.Run(change, func(t *testing.T) {
			source := mediaEditTestDocument(t)
			attachment := source.Streams[3]
			delete(attachment, "codec_name")
			switch change {
			case "video", "audio", "subtitle", "data":
				attachment["codec_type"] = change
			case "empty_bytes":
				attachment["extradata_size"] = json.Number("0")
			case "oversized_bytes":
				attachment["extradata_size"] = json.Number("268435457")
			case "missing_hash":
				delete(attachment, "extradata_hash")
			case "bad_hash":
				attachment["extradata_hash"] = "SHA256:invalid"
			case "missing_name":
				delete(attachment["tags"].(map[string]any), "filename")
			case "missing_mime":
				delete(attachment["tags"].(map[string]any), "mimetype")
			case "bad_mime":
				attachment["tags"].(map[string]any)["mimetype"] = "not a MIME type"
			}
			if err := source.admit("mkv", 7); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
				t.Fatalf("unproven attachment or unknown stream admitted: %v", err)
			}
		})
	}
}

func TestMediaEditOpaqueAttachmentsAreProvenIndividually(t *testing.T) {
	source := mediaEditTestDocument(t)
	delete(source.Streams[3], "codec_name")
	extra := map[string]any{"index": json.Number("21"), "codec_type": "attachment", "time_base": "1/90000", "disposition": map[string]any{"default": json.Number("0"), "forced": json.Number("0")}, "extradata_size": json.Number("12"), "extradata_hash": "SHA256:" + strings.Repeat("d", 64), "tags": map[string]any{"filename": "Separate.bin", "mimetype": "application/octet-stream"}}
	source.Streams = append(source.Streams, extra)
	if err := source.admit("mkv", 7); err != nil {
		t.Fatal(err)
	}
	candidate := mediaEditTestCandidate(t, source)
	_, proof, err := compareMediaEditDocuments(source, candidate, "mkv", 7)
	if err != nil || len(proof) != 4 || proof[2].SourceIndex != 13 || proof[3].SourceIndex != 21 || proof[2].ExtradataSHA256 != strings.Repeat("c", 64) || proof[3].ExtradataSHA256 != strings.Repeat("d", 64) {
		t.Fatalf("per-attachment proof lost an object: %+v %v", proof, err)
	}
	candidate.Streams[2]["extradata_hash"], candidate.Streams[3]["extradata_hash"] = candidate.Streams[3]["extradata_hash"], candidate.Streams[2]["extradata_hash"]
	if _, _, err := compareMediaEditDocuments(source, candidate, "mkv", 7); !errors.Is(err, ErrSubtitleRemovalUnsupported) {
		t.Fatalf("attachment permutation preserved only aggregate bytes: %v", err)
	}
}
