package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxMediaOperationsConfigBytes  = 64 << 10
	maxMediaOperationsScratchBytes = int64(1 << 40)
)

// MediaOperationsConfig is an explicit startup inventory and resource policy.
// Its zero value disables media operations. It can be serialized unchanged as
// part of a job snapshot; loading it never probes tools, models, or media paths.
type MediaOperationsConfig struct {
	Enabled           bool                     `json:"enabled"`
	MaxConcurrent     int                      `json:"maxConcurrent"`
	MaxQueued         int                      `json:"maxQueued"`
	MaxRuntimeSeconds int                      `json:"maxRuntimeSeconds"`
	MaxScratchBytes   int64                    `json:"maxScratchBytes"`
	ScratchDirectory  string                   `json:"scratchDirectory"`
	WritableProfiles  []string                 `json:"writableProfiles,omitempty"`
	OCR               MediaOperationsOCRConfig `json:"ocr"`
}

// MediaOperationsOCRConfig pins an operator-selected OCR tool and model set.
// An empty value leaves OCR unavailable without disabling other operations.
type MediaOperationsOCRConfig struct {
	Engine            string                    `json:"engine,omitempty"`
	Executable        string                    `json:"executable,omitempty"`
	ToolSHA256        string                    `json:"toolSHA256,omitempty"`
	TessdataDirectory string                    `json:"tessdataDirectory,omitempty"`
	Models            []MediaOperationsOCRModel `json:"models,omitempty"`
}

// MediaOperationsOCRModel identifies one allowed Tesseract language model.
// Its public ID is the language name; aliases are not accepted. Filenames and
// digests remain execution inventory.
type MediaOperationsOCRModel struct {
	ID       string `json:"id"`
	Language string `json:"language"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
}

func loadMediaOperations() (MediaOperationsConfig, error) {
	configPath := os.Getenv("GOBY_MEDIA_OPERATIONS_FILE")
	if configPath == "" {
		return MediaOperationsConfig{}, nil
	}
	if len(configPath) > 4096 || !utf8.ValidString(configPath) || strings.IndexFunc(configPath, unicode.IsControl) >= 0 {
		return MediaOperationsConfig{}, errors.New("GOBY_MEDIA_OPERATIONS_FILE must name a bounded configuration file")
	}
	info, err := os.Lstat(configPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxMediaOperationsConfigBytes {
		return MediaOperationsConfig{}, errors.New("GOBY_MEDIA_OPERATIONS_FILE must be a bounded readable regular file")
	}
	input, err := os.Open(configPath)
	if err != nil {
		return MediaOperationsConfig{}, errors.New("cannot read GOBY_MEDIA_OPERATIONS_FILE")
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) || opened.Size() > maxMediaOperationsConfigBytes {
		return MediaOperationsConfig{}, errors.New("GOBY_MEDIA_OPERATIONS_FILE changed while opening or exceeds its size limit")
	}
	data, err := io.ReadAll(io.LimitReader(input, maxMediaOperationsConfigBytes+1))
	if err != nil || len(data) > maxMediaOperationsConfigBytes {
		return MediaOperationsConfig{}, errors.New("cannot read bounded GOBY_MEDIA_OPERATIONS_FILE")
	}
	return parseMediaOperations(data)
}

func parseMediaOperations(data []byte) (MediaOperationsConfig, error) {
	if len(data) > maxMediaOperationsConfigBytes || !utf8.Valid(data) {
		return MediaOperationsConfig{}, errors.New("media operation configuration must be bounded UTF-8 JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	fields, err := readMediaOperationsJSONObject(decoder, "configuration")
	if err != nil {
		return MediaOperationsConfig{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return MediaOperationsConfig{}, errors.New("media operation configuration must contain exactly one JSON document")
	}
	var result MediaOperationsConfig
	if err := json.Unmarshal(data, &result); err != nil {
		return MediaOperationsConfig{}, errors.New("media operation configuration contains an invalid field type")
	}
	if result.Enabled {
		if !fields["maxConcurrent"] {
			result.MaxConcurrent = 1
		}
		if !fields["maxQueued"] {
			result.MaxQueued = 16
		}
		if !fields["maxRuntimeSeconds"] {
			result.MaxRuntimeSeconds = 7200
		}
		if !fields["maxScratchBytes"] {
			result.MaxScratchBytes = 64 << 30
		}
	}
	if err := result.Validate(); err != nil {
		return MediaOperationsConfig{}, err
	}
	return result, nil
}

// Validate checks only the captured configuration. The worker must separately
// verify filesystem ownership, executable digests, model digests, and tool
// availability before admitting any execution.
func (c MediaOperationsConfig) Validate() error {
	if !c.Enabled {
		if c.MaxConcurrent != 0 || c.MaxQueued != 0 || c.MaxRuntimeSeconds != 0 || c.MaxScratchBytes != 0 ||
			c.ScratchDirectory != "" || len(c.WritableProfiles) != 0 || !c.OCR.empty() {
			return errors.New("disabled media operations must not retain execution configuration")
		}
		return nil
	}
	for _, field := range []struct {
		name    string
		value   int64
		maximum int64
	}{
		{"maxConcurrent", int64(c.MaxConcurrent), 4},
		{"maxQueued", int64(c.MaxQueued), 128},
		{"maxRuntimeSeconds", int64(c.MaxRuntimeSeconds), 86400},
		{"maxScratchBytes", c.MaxScratchBytes, maxMediaOperationsScratchBytes},
	} {
		if field.value < 1 || field.value > field.maximum {
			return fmt.Errorf("media operation %s must be between 1 and %d", field.name, field.maximum)
		}
	}
	if !mediaOperationsPath(c.ScratchDirectory) {
		return errors.New("media operation scratchDirectory must be a bounded canonical absolute Linux directory other than the filesystem root")
	}
	if len(c.WritableProfiles) > 2 {
		return errors.New("media operation writableProfiles exceeds its inventory limit")
	}
	profiles := make(map[string]bool, len(c.WritableProfiles))
	for _, profile := range c.WritableProfiles {
		if (profile != "matroska-v1" && profile != "mp4-movtext-v1") || profiles[profile] {
			return errors.New("media operation writableProfiles must contain unique supported profile identifiers")
		}
		profiles[profile] = true
	}
	return c.OCR.validate()
}

func (c MediaOperationsOCRConfig) empty() bool {
	return c.Engine == "" && c.Executable == "" && c.ToolSHA256 == "" && c.TessdataDirectory == "" && len(c.Models) == 0
}

func (c MediaOperationsOCRConfig) validate() error {
	if c.empty() {
		return nil
	}
	if c.Engine != "tesseract" || !mediaOperationsPath(c.Executable) || !mediaOperationsDigest(c.ToolSHA256) ||
		!mediaOperationsPath(c.TessdataDirectory) || len(c.Models) < 1 || len(c.Models) > 3 {
		return errors.New("media operation OCR requires tesseract, canonical absolute Linux paths, a tool SHA-256 digest, and one to three explicit models")
	}
	ids := make(map[string]bool, len(c.Models))
	languages := make(map[string]bool, len(c.Models))
	filenames := make(map[string]bool, len(c.Models))
	for _, model := range c.Models {
		if model.ID != model.Language || ids[model.ID] || languages[model.Language] || filenames[model.Filename] ||
			(model.Language != "eng" && model.Language != "chi_sim" && model.Language != "chi_tra") ||
			model.Filename != model.Language+".traineddata" || !mediaOperationsDigest(model.SHA256) {
			return errors.New("media operation OCR models require unique supported language IDs, matching traineddata filenames, and SHA-256 digests")
		}
		ids[model.ID], languages[model.Language], filenames[model.Filename] = true, true, true
	}
	return nil
}

func mediaOperationsPath(value string) bool {
	return value != "/" && len(value) <= 4096 && utf8.ValidString(value) && path.IsAbs(value) &&
		path.Clean(value) == value && !strings.Contains(value, "\\") && strings.IndexFunc(value, unicode.IsControl) < 0
}

func mediaOperationsDigest(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

// Token inspection preserves duplicate and case-sensitive key rejection, which
// encoding/json's struct decoder does not provide. Typed decoding follows this
// bounded schema walk; no null values or arbitrary nested objects are accepted.
func readMediaOperationsJSONObject(decoder *json.Decoder, scope string) (map[string]bool, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("media operation configuration requires JSON objects")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, errors.New("media operation configuration contains an invalid field name")
		}
		kind := mediaOperationsJSONField(scope, name)
		if kind == "" {
			return nil, errors.New("media operation configuration contains an unknown field")
		}
		if seen[name] {
			return nil, fmt.Errorf("media operation %s contains duplicate field %q", scope, name)
		}
		seen[name] = true
		switch kind {
		case "ocr":
			if _, err := readMediaOperationsJSONObject(decoder, "ocr"); err != nil {
				return nil, err
			}
		case "profiles", "models":
			if err := readMediaOperationsJSONArray(decoder, kind); err != nil {
				return nil, err
			}
		default:
			if err := readMediaOperationsJSONScalar(decoder); err != nil {
				return nil, err
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, errors.New("media operation configuration contains an invalid JSON object")
	}
	return seen, nil
}

func mediaOperationsJSONField(scope, name string) string {
	switch scope {
	case "configuration":
		switch name {
		case "enabled", "maxConcurrent", "maxQueued", "maxRuntimeSeconds", "maxScratchBytes", "scratchDirectory":
			return "scalar"
		case "writableProfiles":
			return "profiles"
		case "ocr":
			return "ocr"
		}
	case "ocr":
		switch name {
		case "engine", "executable", "toolSHA256", "tessdataDirectory":
			return "scalar"
		case "models":
			return "models"
		}
	case "model":
		switch name {
		case "id", "language", "filename", "sha256":
			return "scalar"
		}
	}
	return ""
}

func readMediaOperationsJSONArray(decoder *json.Decoder, kind string) error {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('[') {
		return errors.New("media operation configuration requires non-null inventory arrays")
	}
	maximum := 2
	if kind == "models" {
		maximum = 3
	}
	count := 0
	for decoder.More() {
		count++
		if count > maximum {
			return errors.New("media operation configuration exceeds its inventory limit")
		}
		if kind == "models" {
			if _, err := readMediaOperationsJSONObject(decoder, "model"); err != nil {
				return err
			}
		} else if err := readMediaOperationsJSONScalar(decoder); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return errors.New("media operation configuration contains an invalid inventory array")
	}
	return nil
}

func readMediaOperationsJSONScalar(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return errors.New("media operation configuration requires non-null scalar fields")
	}
	if _, nested := token.(json.Delim); nested {
		return errors.New("media operation configuration contains an invalid scalar field")
	}
	return nil
}
