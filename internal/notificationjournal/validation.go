package notificationjournal

import (
	"bytes"
	"encoding/json"
	"io"
	"net/netip"
	"net/url"
	"strconv"
)

func ValidateTransport(endpoint string, networks []string, enabled, hasSecret bool) error {
	if networks == nil || len(networks) > 32 || enabled && (!hasSecret || endpoint == "") {
		return ErrJournal
	}
	seen := map[string]bool{}
	for _, raw := range networks {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix.Masked().String() != raw || seen[raw] {
			return ErrJournal
		}
		seen[raw] = true
	}
	if endpoint == "" && !enabled {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return ErrJournal
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return ErrJournal
		}
	}
	return nil
}
func ValidateEvents(values []string) error {
	if len(values) < 1 || len(values) > 2 {
		return ErrJournal
	}
	seen := map[string]bool{}
	for _, value := range values {
		if value != "CatalogInvalidated" && value != "UserDataInvalidated" || seen[value] {
			return ErrJournal
		}
		seen[value] = true
	}
	return nil
}

// ValidateReferences checks stored JSON without importing a runtime/domain.
// Source userdata requires its immutable owner; delivery rows derive the owner
// from their registration. Terminal rows may have cleared reference arrays.
func ValidateReferences(kind, owner string, raw []byte, delivery bool) error {
	if len(raw) > 524288 {
		return ErrJournal
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var objects []map[string]json.RawMessage
	if decoder.Decode(&objects) != nil || objects == nil {
		return ErrJournal
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || len(objects) > 4096 {
		return ErrJournal
	}
	switch kind {
	case "CatalogInvalidated":
		if owner != "" || !delivery && len(objects) == 0 {
			return ErrJournal
		}
	case "UserDataInvalidated":
		if !delivery && !ValidID(owner) {
			return ErrJournal
		}
	case "ResyncRequired", "Test":
		if !delivery || owner != "" {
			return ErrJournal
		}
	default:
		return ErrJournal
	}
	if kind == "Test" && len(objects) != 0 {
		return ErrJournal
	}
	seen := map[Reference]bool{}
	for _, object := range objects {
		var ref Reference
		for key, value := range object {
			var text string
			if json.Unmarshal(value, &text) != nil {
				return ErrJournal
			}
			switch key {
			case "Kind":
				ref.Kind = text
			case "Id":
				ref.ID = text
			case "LibraryId":
				ref.LibraryID = text
			case "SourceId":
				ref.SourceID = text
			default:
				return ErrJournal
			}
		}
		if !ValidID(ref.ID) || ref.Kind != "Item" && ref.Kind != "Entity" && ref.Kind != "Library" || ref.LibraryID != "" && !ValidID(ref.LibraryID) || seen[ref] {
			return ErrJournal
		}
		seen[ref] = true
		if ref.SourceID != "" && (!ValidID(ref.SourceID) || ref.LibraryID == "" || ref.Kind == "Entity") {
			return ErrJournal
		}
		if ref.Kind == "Entity" {
			id, err := strconv.ParseInt(ref.ID, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != ref.ID || ref.LibraryID != "" {
				return ErrJournal
			}
		}
		if kind == "CatalogInvalidated" && (ref.Kind == "Entity" || ref.LibraryID == "") {
			return ErrJournal
		}
		if kind == "UserDataInvalidated" && (ref.Kind == "Library" || ref.LibraryID != "" || ref.SourceID != "") {
			return ErrJournal
		}
	}
	return nil
}
