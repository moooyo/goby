// Package updates observes an operator-selected Goby release feed. It never
// downloads an image, installs a package, or controls a container runtime.
package updates

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxFeedBytes = 64 << 10

var ErrInvalidFeed = errors.New("invalid Goby update feed")

type feedDocument struct {
	SchemaVersion int         `json:"schemaVersion"`
	Product       string      `json:"product"`
	Release       feedRelease `json:"release"`
}

type feedRelease struct {
	Version        string `json:"version"`
	Classification string `json:"classification"`
	PublishedAt    string `json:"publishedAt"`
	Description    string `json:"description"`
	InfoURL        string `json:"infoUrl"`
	SourceURL      string `json:"sourceUrl"`
	SHA256         string `json:"sha256"`
	TargetFilename string `json:"targetFilename"`
}

// Release is publisher-provided information, not an installation or signature
// verification receipt. SourceUrl is never fetched by this package.
type Release struct {
	Version        string
	Classification string
	PublishedAt    time.Time
	Description    string
	InfoUrl        string
	SourceUrl      string
	SHA256         string
	TargetFilename string
}

// ValidateFeedURL validates syntax without performing DNS or other network I/O.
// The production transport authorizes every resolved address before dialing.
func ValidateFeedURL(raw string) error {
	_, err := parsePublicHTTPSURL(raw)
	return err
}

func parsePublicHTTPSURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) == 0 || len(raw) > 2048 || strings.TrimSpace(raw) != raw ||
		strings.IndexFunc(raw, unicode.IsControl) >= 0 || parsed.Scheme != "https" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") ||
		parsed.Host == "" || parsed.Port() != "" && parsed.Port() != "443" {
		return nil, ErrInvalidFeed
	}
	host := parsed.Hostname()
	if net.ParseIP(host) != nil || len(host) > 253 || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") {
		return nil, ErrInvalidFeed
	}
	// Browsers interpret numeric terminal labels using IPv4 legacy forms.
	// Require an actual DNS-style destination consistently in API and UI URLs.
	labels := strings.Split(host, ".")
	lastLabel := strings.ToLower(labels[len(labels)-1])
	if numericIdentifier(lastLabel) || strings.HasPrefix(lastLabel, "0x") && strings.Trim(lastLabel[2:], "0123456789abcdef") == "" {
		return nil, ErrInvalidFeed
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return nil, ErrInvalidFeed
		}
		for _, character := range label {
			if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character == '-') {
				return nil, ErrInvalidFeed
			}
		}
	}
	return parsed, nil
}

// rejectDuplicateNames prevents JSON's last-value-wins behavior from changing
// the apparent product, artifact identity, or publisher classification.
func rejectDuplicateNames(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return ErrInvalidFeed
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidFeed
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[strings.ToLower(name)] {
				return ErrInvalidFeed
			}
			seen[strings.ToLower(name)] = true
			if err := rejectDuplicateNames(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := rejectDuplicateNames(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalidFeed
	}
	closing, err := decoder.Token()
	if err != nil || delimiter == '{' && closing != json.Delim('}') || delimiter == '[' && closing != json.Delim(']') {
		return ErrInvalidFeed
	}
	return nil
}

func exactObject(data []byte, fields ...string) (map[string]json.RawMessage, bool) {
	var values map[string]json.RawMessage
	if json.Unmarshal(data, &values) != nil || len(values) != len(fields) {
		return nil, false
	}
	for _, field := range fields {
		value, present := values[field]
		if !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
	}
	return values, true
}

// Valid UTF-8 bytes can still contain unpaired escaped UTF-16 code units.
// Reject those before encoding/json can silently replace them with U+FFFD.
func losslessJSON(data []byte) bool {
	for index := 0; index < len(data); index++ {
		if data[index] != '"' {
			continue
		}
		for index++; index < len(data) && data[index] != '"'; index++ {
			if data[index] != '\\' {
				continue
			}
			index++
			if index >= len(data) {
				return false
			}
			if data[index] != 'u' {
				continue
			}
			if index+4 >= len(data) {
				return false
			}
			unit, err := strconv.ParseUint(string(data[index+1:index+5]), 16, 16)
			if err != nil {
				return false
			}
			index += 4
			if unit >= 0xdc00 && unit <= 0xdfff {
				return false
			}
			if unit < 0xd800 || unit > 0xdbff {
				continue
			}
			if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[index+3:index+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}

func parseFeed(data []byte) (Release, error) {
	var result Release
	if len(data) == 0 || len(data) > MaxFeedBytes || !utf8.Valid(data) || !losslessJSON(data) {
		return result, ErrInvalidFeed
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if err := rejectDuplicateNames(check, 0); err != nil {
		return result, ErrInvalidFeed
	}
	fields, ok := exactObject(data, "schemaVersion", "product", "release")
	if !ok {
		return result, ErrInvalidFeed
	}
	if _, ok := exactObject(fields["release"], "version", "classification", "publishedAt", "description", "infoUrl", "sourceUrl", "sha256", "targetFilename"); !ok {
		return result, ErrInvalidFeed
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document feedDocument
	if err := decoder.Decode(&document); err != nil || document.SchemaVersion != 1 || document.Product != "goby" {
		return result, ErrInvalidFeed
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return result, ErrInvalidFeed
	}
	value := document.Release
	if _, ok := parseVersion(value.Version); !ok {
		return result, ErrInvalidFeed
	}
	if value.Classification != "Release" && value.Classification != "Beta" && value.Classification != "Dev" {
		return result, ErrInvalidFeed
	}
	published, err := time.Parse(time.RFC3339, value.PublishedAt)
	published = published.UTC()
	if err != nil || published.IsZero() || published.Year() < 1 || published.Year() > 9999 || len(value.PublishedAt) > 35 {
		return result, ErrInvalidFeed
	}
	if len(value.Description) > 4096 || strings.IndexFunc(value.Description, func(character rune) bool {
		return unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t'
	}) >= 0 {
		return result, ErrInvalidFeed
	}
	if ValidateFeedURL(value.InfoURL) != nil || ValidateFeedURL(value.SourceURL) != nil || len(value.SHA256) != 64 || strings.Trim(value.SHA256, "0123456789abcdef") != "" {
		return result, ErrInvalidFeed
	}
	if value.TargetFilename == "" || len(value.TargetFilename) > 128 || value.TargetFilename == "." || value.TargetFilename == ".." {
		return result, ErrInvalidFeed
	}
	for index, character := range value.TargetFilename {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && (character == '.' || character == '_' || character == '-')) {
			return result, ErrInvalidFeed
		}
	}
	return Release{
		Version: value.Version, Classification: value.Classification, PublishedAt: published, Description: value.Description,
		InfoUrl: value.InfoURL, SourceUrl: value.SourceURL, SHA256: value.SHA256, TargetFilename: value.TargetFilename,
	}, nil
}
