package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/dynamicsource"
)

const maxDynamicSourceConfigBytes = 1 << 20

// DynamicSourceDefinition binds an operator-controlled network input to an
// existing catalog item. URLs and headers may contain credentials and must
// never be returned in a public media-source description.
type DynamicSourceDefinition struct {
	ItemID        string                             `json:"itemId"`
	Name          string                             `json:"name,omitempty"`
	URL           string                             `json:"url"`
	Headers       map[string]string                  `json:"headers,omitempty"`
	Infinite      bool                               `json:"infinite,omitempty"`
	MaxReconnects int                                `json:"maxReconnects,omitempty"`
	Subtitles     []dynamicsource.SubtitleDefinition `json:"subtitles,omitempty"`
}

func (DynamicSourceDefinition) String() string   { return "<dynamic-source configuration>" }
func (DynamicSourceDefinition) GoString() string { return "<dynamic-source configuration>" }

func loadDynamicSources() ([]DynamicSourceDefinition, error) {
	path := os.Getenv("GOBY_DYNAMIC_SOURCES_FILE")
	if path == "" {
		return nil, nil
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read GOBY_DYNAMIC_SOURCES_FILE")
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDynamicSourceConfigBytes {
		return nil, errors.New("GOBY_DYNAMIC_SOURCES_FILE must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(input, maxDynamicSourceConfigBytes+1))
	if err != nil || len(data) > maxDynamicSourceConfigBytes {
		return nil, errors.New("cannot read bounded GOBY_DYNAMIC_SOURCES_FILE")
	}
	return parseDynamicSources(data)
}

func parseDynamicSources(data []byte) ([]DynamicSourceDefinition, error) {
	if len(data) > maxDynamicSourceConfigBytes {
		return nil, errors.New("dynamic source configuration exceeds its size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var definitions []DynamicSourceDefinition
	if err := decoder.Decode(&definitions); err != nil || definitions == nil {
		return nil, errors.New("dynamic sources must be a JSON array of source definitions")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("dynamic sources must contain exactly one JSON document")
	}
	return definitions, validateDynamicSources(definitions)
}

func validateDynamicSources(definitions []DynamicSourceDefinition) error {
	if len(definitions) > 128 {
		return errors.New("at most 128 dynamic sources may be registered")
	}
	seen := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		if len(definition.ItemID) != 32 || strings.Trim(definition.ItemID, "0123456789abcdef") != "" || seen[definition.ItemID] {
			return errors.New("dynamic sources need unique canonical catalog item identifiers")
		}
		seen[definition.ItemID] = true
		if !utf8.ValidString(definition.Name) || len(definition.Name) > 256 || strings.TrimSpace(definition.Name) != definition.Name || !validPrivateSetting(definition.Name) {
			return errors.New("dynamic source names must be bounded single-line values")
		}
		if definition.MaxReconnects < 0 || definition.MaxReconnects > 3 {
			return errors.New("dynamic source reconnect and header limits are exceeded")
		}
		if err := validateDynamicRequest(definition.URL, definition.Headers); err != nil {
			return err
		}
		if err := dynamicsource.ValidateSubtitleDefinitions(definition.Subtitles); err != nil {
			return errors.New("dynamic subtitles require unique bounded identities, supported formats, explicit clocks, and private HTTP(S) requests")
		}
		for _, subtitle := range definition.Subtitles {
			if err := validateDynamicRequest(subtitle.URL, subtitle.Headers); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDynamicRequest(rawURL string, headers map[string]string) error {
	address, err := url.Parse(rawURL)
	if err != nil || len(rawURL) > 8192 || address.Hostname() == "" || address.User != nil || address.Opaque != "" ||
		(address.Scheme != "http" && address.Scheme != "https") || address.Fragment != "" || !validPrivateSetting(rawURL) {
		return errors.New("dynamic source URLs must be bounded HTTP(S) URLs without user information or fragments")
	}
	if len(headers) > 16 {
		return errors.New("dynamic source header count is exceeded")
	}
	headerBytes := 0
	headerNames := make(map[string]bool, len(headers))
	for key, value := range headers {
		name := strings.ToLower(key)
		headerBytes += len(key) + len(value)
		allowed := name == "authorization" || name == "cookie" || name == "user-agent" || name == "accept" || strings.HasPrefix(name, "x-")
		if !allowed || len(key) > 128 || strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || len(value) > 4096 || !validPrivateSetting(value) || headerNames[name] || headerBytes > 16384 {
			return errors.New("dynamic source headers must be bounded allowed request headers")
		}
		headerNames[name] = true
	}
	return nil
}
