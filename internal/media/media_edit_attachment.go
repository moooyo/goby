package media

import (
	"encoding/hex"
	"mime"
	"strings"
)

// Matroska attachments are byte objects with a declared name and MIME type.
// ffprobe legitimately omits codec_name for MIME types it does not decode, such
// as font/ttf. This does not authorize unknown media streams: only an explicit
// attachment with a complete, bounded extradata digest qualifies. The retained
// stream comparison independently checks every metadata field and all bytes.
func mediaEditValidateAttachment(stream map[string]any) error {
	if stream["codec_type"] != "attachment" {
		return mediaEditContainerError("opaque bytes are not declared as an attachment")
	}
	size, err := mediaEditInteger(stream["extradata_size"])
	if err != nil || size <= 0 || size > 256<<20 {
		return mediaEditContainerError("attachment byte extent is missing or exceeds its budget")
	}
	digest, ok := stream["extradata_hash"].(string)
	decoded, err := hex.DecodeString(strings.TrimPrefix(digest, "SHA256:"))
	if !ok || !strings.HasPrefix(digest, "SHA256:") || err != nil || len(decoded) != 32 {
		return mediaEditContainerError("attachment lacks a complete extradata SHA-256")
	}
	tags, err := mediaEditTags(stream["tags"])
	if err != nil {
		return err
	}
	name, declared := tags["filename"], tags["mimetype"]
	if name == "" || len(name) > 4096 || strings.ContainsRune(name, 0) || declared == "" || len(declared) > 1024 || strings.ContainsRune(declared, 0) {
		return mediaEditContainerError("attachment lacks a bounded filename or MIME declaration")
	}
	if mediaType, _, err := mime.ParseMediaType(declared); err != nil || !strings.Contains(mediaType, "/") {
		return mediaEditContainerError("attachment MIME declaration is invalid")
	}
	return nil
}
