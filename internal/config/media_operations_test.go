package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mediaOperationsTestConfig() MediaOperationsConfig {
	return MediaOperationsConfig{
		Enabled: true, MaxConcurrent: 1, MaxQueued: 16, MaxRuntimeSeconds: 7200,
		MaxScratchBytes: 64 << 30, ScratchDirectory: "/unopened-media-operations-fixture/scratch",
		WritableProfiles: []string{"matroska-v1", "mp4-movtext-v1"},
		OCR: MediaOperationsOCRConfig{
			Engine: "tesseract", Executable: "/unopened-media-operations-fixture/bin/tesseract",
			ToolSHA256: strings.Repeat("a", 64), TessdataDirectory: "/unopened-media-operations-fixture/tessdata",
			Models: []MediaOperationsOCRModel{
				{ID: "eng", Language: "eng", Filename: "eng.traineddata", SHA256: strings.Repeat("b", 64)},
				{ID: "chi_sim", Language: "chi_sim", Filename: "chi_sim.traineddata", SHA256: strings.Repeat("c", 64)},
				{ID: "chi_tra", Language: "chi_tra", Filename: "chi_tra.traineddata", SHA256: strings.Repeat("d", 64)},
			},
		},
	}
}

func mediaOperationsTestJSON(t *testing.T, cfg MediaOperationsConfig) []byte {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMediaOperationsDefaultsAndDisabledConfiguration(t *testing.T) {
	for _, data := range []string{`{}`, `{"enabled":false}`} {
		cfg, err := parseMediaOperations([]byte(data))
		if err != nil || !reflect.DeepEqual(cfg, MediaOperationsConfig{}) {
			t.Fatalf("disabled configuration = %+v, error = %v", cfg, err)
		}
	}
	if err := (MediaOperationsConfig{}).Validate(); err != nil {
		t.Fatalf("zero-value configuration must remain disabled: %v", err)
	}
	cfg, err := parseMediaOperations([]byte(`{"enabled":true,"scratchDirectory":"/unopened-media-operations-fixture/scratch"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.MaxConcurrent != 1 || cfg.MaxQueued != 16 || cfg.MaxRuntimeSeconds != 7200 ||
		cfg.MaxScratchBytes != 64<<30 || cfg.ScratchDirectory != "/unopened-media-operations-fixture/scratch" ||
		len(cfg.WritableProfiles) != 0 || !reflect.DeepEqual(cfg.OCR, MediaOperationsOCRConfig{}) {
		t.Fatalf("unexpected enabled defaults: %+v", cfg)
	}
	for _, test := range []struct {
		name   string
		mutate func(*MediaOperationsConfig)
	}{
		{"concurrent", func(c *MediaOperationsConfig) { c.MaxConcurrent = 1 }},
		{"queued", func(c *MediaOperationsConfig) { c.MaxQueued = 16 }},
		{"runtime", func(c *MediaOperationsConfig) { c.MaxRuntimeSeconds = 7200 }},
		{"scratch_bytes", func(c *MediaOperationsConfig) { c.MaxScratchBytes = 1 }},
		{"scratch_directory", func(c *MediaOperationsConfig) { c.ScratchDirectory = "/scratch" }},
		{"profiles", func(c *MediaOperationsConfig) { c.WritableProfiles = []string{"matroska-v1"} }},
		{"ocr", func(c *MediaOperationsConfig) { c.OCR = mediaOperationsTestConfig().OCR }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var disabled MediaOperationsConfig
			test.mutate(&disabled)
			if err := disabled.Validate(); err == nil {
				t.Fatal("disabled configuration retained execution settings")
			}
			if _, err := parseMediaOperations(mediaOperationsTestJSON(t, disabled)); err == nil {
				t.Fatal("parser concealed execution settings in disabled configuration")
			}
		})
	}
}

func TestMediaOperationsRejectsNonCanonicalJSON(t *testing.T) {
	valid := string(mediaOperationsTestJSON(t, mediaOperationsTestConfig()))
	for _, test := range []struct{ name, data string }{
		{"empty", ""},
		{"whitespace", " \n\t"},
		{"array", `[]`},
		{"null_root", `null`},
		{"string_root", `"disabled"`},
		{"truncated", `{"enabled":`},
		{"trailing_object", valid + `{}`},
		{"trailing_scalar", valid + ` true`},
		{"root_unknown", strings.Replace(valid, `"enabled":true`, `"enabled":true,"unknown":1`, 1)},
		{"root_case", strings.Replace(valid, `"enabled"`, `"Enabled"`, 1)},
		{"root_duplicate", strings.Replace(valid, `"enabled":true`, `"enabled":true,"enabled":true`, 1)},
		{"escaped_duplicate", strings.Replace(valid, `"enabled":true`, `"enabled":true,"\u0065nabled":true`, 1)},
		{"ocr_unknown", strings.Replace(valid, `"engine":"tesseract"`, `"engine":"tesseract","unknown":1`, 1)},
		{"ocr_case", strings.Replace(valid, `"toolSHA256"`, `"toolSha256"`, 1)},
		{"ocr_duplicate", strings.Replace(valid, `"engine":"tesseract"`, `"engine":"tesseract","engine":"tesseract"`, 1)},
		{"model_unknown", strings.Replace(valid, `"id":"eng"`, `"id":"eng","unknown":1`, 1)},
		{"model_case", strings.Replace(valid, `"sha256"`, `"SHA256"`, 1)},
		{"model_duplicate", strings.Replace(valid, `"id":"eng"`, `"id":"eng","id":"eng"`, 1)},
		{"string_number", strings.Replace(valid, `"maxConcurrent":1`, `"maxConcurrent":"1"`, 1)},
		{"fraction_number", strings.Replace(valid, `"maxConcurrent":1`, `"maxConcurrent":1.5`, 1)},
		{"overflow_number", strings.Replace(valid, `"maxConcurrent":1`, `"maxConcurrent":9223372036854775808`, 1)},
		{"string_boolean", strings.Replace(valid, `"enabled":true`, `"enabled":"true"`, 1)},
		{"profile_object", strings.Replace(valid, `"writableProfiles":[`, `"writableProfiles":[{},`, 1)},
		{"null_profile", strings.Replace(valid, `"writableProfiles":[`, `"writableProfiles":[null,`, 1)},
		{"null_model", strings.Replace(valid, `"models":[`, `"models":[null,`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseMediaOperations([]byte(test.data)); err == nil {
				t.Fatal("accepted malformed or ambiguous JSON configuration")
			}
		})
	}
	for _, location := range []string{"root", "ocr", "model"} {
		var document map[string]any
		if err := json.Unmarshal([]byte(valid), &document); err != nil {
			t.Fatal(err)
		}
		fields := document
		if location == "ocr" || location == "model" {
			fields = document["ocr"].(map[string]any)
		}
		if location == "model" {
			fields = fields["models"].([]any)[0].(map[string]any)
		}
		for name, value := range fields {
			t.Run(location+"_null_"+name, func(t *testing.T) {
				fields[name] = nil
				defer func() { fields[name] = value }()
				data, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := parseMediaOperations(data); err == nil {
					t.Fatal("accepted an explicit null configuration value")
				}
			})
		}
	}
}

func TestMediaOperationsNumericBoundsAndExplicitZero(t *testing.T) {
	for _, test := range []struct {
		name string
		max  int64
		set  func(*MediaOperationsConfig, int64)
	}{
		{"maxConcurrent", 4, func(c *MediaOperationsConfig, v int64) { c.MaxConcurrent = int(v) }},
		{"maxQueued", 128, func(c *MediaOperationsConfig, v int64) { c.MaxQueued = int(v) }},
		{"maxRuntimeSeconds", 86400, func(c *MediaOperationsConfig, v int64) { c.MaxRuntimeSeconds = int(v) }},
		{"maxScratchBytes", 1 << 40, func(c *MediaOperationsConfig, v int64) { c.MaxScratchBytes = v }},
	} {
		for _, value := range []int64{-1, 0, 1, test.max, test.max + 1} {
			t.Run(fmt.Sprintf("%s/%d", test.name, value), func(t *testing.T) {
				cfg := mediaOperationsTestConfig()
				test.set(&cfg, value)
				wantValid := value >= 1 && value <= test.max
				if err := cfg.Validate(); (err == nil) != wantValid {
					t.Fatalf("Validate validity = %v, want %v: %v", err == nil, wantValid, err)
				}
				data := fmt.Sprintf(`{"enabled":true,"scratchDirectory":"/scratch","%s":%d}`, test.name, value)
				if _, err := parseMediaOperations([]byte(data)); (err == nil) != wantValid {
					t.Fatalf("parse validity = %v, want %v: %v", err == nil, wantValid, err)
				}
			})
		}
	}
}

func TestMediaOperationsRejectsUnsafePaths(t *testing.T) {
	for index, value := range []string{"", ".", "/", "relative/path", "//parent/child", "/parent/../child", "/parent/./child", "/parent/child/",
		`C:\scratch`, `/parent\child`, "/parent\x00child", "/parent\nchild", "/parent\tchild", "/parent\x7fchild",
		"/parent\u0085child", "/parent\xffchild", "/" + strings.Repeat("a", 4096)} {
		for _, field := range []string{"scratch", "executable", "tessdata"} {
			t.Run(fmt.Sprintf("%s/%d", field, index), func(t *testing.T) {
				cfg := mediaOperationsTestConfig()
				switch field {
				case "scratch":
					cfg.ScratchDirectory = value
				case "executable":
					cfg.OCR.Executable = value
				case "tessdata":
					cfg.OCR.TessdataDirectory = value
				}
				if err := cfg.Validate(); err == nil {
					t.Fatal("accepted unsafe or noncanonical Linux path syntax")
				}
			})
		}
	}
}

func TestMediaOperationsWritableProfilesAreClosedAndUnique(t *testing.T) {
	for _, test := range []struct {
		name     string
		profiles []string
		valid    bool
	}{
		{"none", nil, true},
		{"matroska", []string{"matroska-v1"}, true},
		{"mp4", []string{"mp4-movtext-v1"}, true},
		{"both", []string{"mp4-movtext-v1", "matroska-v1"}, true},
		{"empty", []string{""}, false},
		{"unknown", []string{"mpegts-v1"}, false},
		{"case", []string{"Matroska-v1"}, false},
		{"duplicate", []string{"matroska-v1", "matroska-v1"}, false},
		{"over_budget", []string{"matroska-v1", "mp4-movtext-v1", "matroska-v1"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := mediaOperationsTestConfig()
			cfg.WritableProfiles = test.profiles
			if err := cfg.Validate(); (err == nil) != test.valid {
				t.Fatalf("profile validity = %v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}
}

func TestMediaOperationsOCRRequiresCompletePinnedConfiguration(t *testing.T) {
	cfg := mediaOperationsTestConfig()
	cfg.OCR = MediaOperationsOCRConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("OCR must be independently optional: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*MediaOperationsOCRConfig)
	}{
		{"missing_engine", func(c *MediaOperationsOCRConfig) { c.Engine = "" }},
		{"unsupported_engine", func(c *MediaOperationsOCRConfig) { c.Engine = "other" }},
		{"engine_case", func(c *MediaOperationsOCRConfig) { c.Engine = "Tesseract" }},
		{"missing_executable", func(c *MediaOperationsOCRConfig) { c.Executable = "" }},
		{"missing_tessdata", func(c *MediaOperationsOCRConfig) { c.TessdataDirectory = "" }},
		{"missing_digest", func(c *MediaOperationsOCRConfig) { c.ToolSHA256 = "" }},
		{"short_digest", func(c *MediaOperationsOCRConfig) { c.ToolSHA256 = strings.Repeat("a", 63) }},
		{"long_digest", func(c *MediaOperationsOCRConfig) { c.ToolSHA256 = strings.Repeat("a", 65) }},
		{"uppercase_digest", func(c *MediaOperationsOCRConfig) { c.ToolSHA256 = strings.Repeat("A", 64) }},
		{"nonhex_digest", func(c *MediaOperationsOCRConfig) { c.ToolSHA256 = strings.Repeat("g", 64) }},
		{"missing_models", func(c *MediaOperationsOCRConfig) { c.Models = nil }},
		{"too_many_models", func(c *MediaOperationsOCRConfig) { c.Models = append(c.Models, c.Models[0]) }},
		{"duplicate_id", func(c *MediaOperationsOCRConfig) { c.Models[1].ID = c.Models[0].ID }},
		{"duplicate_language", func(c *MediaOperationsOCRConfig) {
			c.Models[1].Language = "eng"
			c.Models[1].Filename = "eng.traineddata"
		}},
		{"duplicate_filename", func(c *MediaOperationsOCRConfig) { c.Models[1].Filename = c.Models[0].Filename }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := mediaOperationsTestConfig()
			test.mutate(&cfg.OCR)
			if err := cfg.Validate(); err == nil {
				t.Fatal("accepted incomplete or unpinned OCR configuration")
			}
		})
	}
}

func TestMediaOperationsOCRModelsHaveClosedIdentities(t *testing.T) {
	for _, test := range []struct {
		name   string
		values []string
		set    func(*MediaOperationsOCRModel, string)
	}{
		{"id", []string{"", "eng-v1", "ENG", "chi_sim", "-eng", ".eng", "_eng", "eng model", "eng/model", "eng\nmodel", "eng\u00e9", strings.Repeat("a", 65)}, func(m *MediaOperationsOCRModel, v string) { m.ID = v }},
		{"language", []string{"", "en", "ENG", "eng+chi_sim", "osd"}, func(m *MediaOperationsOCRModel, v string) { m.Language = v }},
		{"filename", []string{"", "custom.traineddata", "chi_sim.traineddata", "/eng.traineddata", "../eng.traineddata", `model\eng.traineddata`, "eng.TRAINEDDATA"}, func(m *MediaOperationsOCRModel, v string) { m.Filename = v }},
		{"digest", []string{"", strings.Repeat("b", 63), strings.Repeat("b", 65), strings.Repeat("B", 64), strings.Repeat("z", 64)}, func(m *MediaOperationsOCRModel, v string) { m.SHA256 = v }},
	} {
		for index, value := range test.values {
			t.Run(fmt.Sprintf("%s/%d", test.name, index), func(t *testing.T) {
				cfg := mediaOperationsTestConfig()
				test.set(&cfg.OCR.Models[0], value)
				if err := cfg.Validate(); err == nil {
					t.Fatal("accepted invalid OCR model identity")
				}
			})
		}
	}
	for _, language := range []string{"eng", "chi_sim", "chi_tra"} {
		cfg := mediaOperationsTestConfig()
		cfg.OCR.Models = []MediaOperationsOCRModel{{ID: language, Language: language, Filename: language + ".traineddata", SHA256: strings.Repeat("a", 64)}}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("supported language model was rejected: %v", err)
		}
	}
	for _, language := range []string{"osd", "fra", "eng-v1"} {
		cfg := mediaOperationsTestConfig()
		cfg.OCR.Models = []MediaOperationsOCRModel{{ID: language, Language: language, Filename: language + ".traineddata", SHA256: strings.Repeat("a", 64)}}
		if err := cfg.Validate(); err == nil {
			t.Fatal("accepted an unsupported model language with matching identity")
		}
	}
	for count := 1; count <= 3; count++ {
		cfg := mediaOperationsTestConfig()
		cfg.OCR.Models = cfg.OCR.Models[:count]
		if err := cfg.Validate(); err != nil {
			t.Fatalf("valid %d-model inventory was rejected: %v", count, err)
		}
	}
}

func TestMediaOperationsLoadReadsOnlyBoundedRegularConfigurationFile(t *testing.T) {
	t.Setenv("GOBY_MEDIA_OPERATIONS_FILE", "")
	cfg, err := loadMediaOperations()
	if err != nil || !reflect.DeepEqual(cfg, MediaOperationsConfig{}) {
		t.Fatalf("unconfigured media operations must be disabled: %v", err)
	}
	directory := t.TempDir()
	filename := filepath.Join(directory, "media-operations.json")
	want := mediaOperationsTestConfig()
	if err := os.WriteFile(filename, mediaOperationsTestJSON(t, want), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOBY_MEDIA_OPERATIONS_FILE", filename)
	cfg, err = loadMediaOperations()
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("syntax-only configuration load = %+v, error = %v", cfg, err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"malformed", []byte(`{"enabled":false,"enabled":false}`)},
		{"oversized", append([]byte(`{}`), bytes.Repeat([]byte(" "), (64<<10)-1)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(filename, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadMediaOperations(); err == nil {
				t.Fatal("accepted invalid or oversized configuration file")
			}
		})
	}
	boundary := append([]byte(`{}`), bytes.Repeat([]byte(" "), (64<<10)-2)...)
	if err := os.WriteFile(filename, boundary, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadMediaOperations(); err != nil {
		t.Fatalf("configuration at the byte budget was rejected: %v", err)
	}
	for _, filename := range []string{directory, filepath.Join(directory, "missing.json")} {
		t.Setenv("GOBY_MEDIA_OPERATIONS_FILE", filename)
		if _, err := loadMediaOperations(); err == nil {
			t.Fatal("accepted a directory or missing configuration file")
		}
	}
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(directory, "linked-configuration.json")
		if err := os.Symlink(filename, link); err != nil {
			t.Skipf("symbolic links are unavailable: %v", err)
		}
		t.Setenv("GOBY_MEDIA_OPERATIONS_FILE", link)
		if _, err := loadMediaOperations(); err == nil {
			t.Fatal("accepted a symbolic link as a regular configuration file")
		}
	})
}

func TestMediaOperationsParserEnforcesByteBudget(t *testing.T) {
	boundary := append([]byte(`{}`), bytes.Repeat([]byte(" "), (64<<10)-2)...)
	if _, err := parseMediaOperations(boundary); err != nil {
		t.Fatalf("JSON at the byte budget was rejected: %v", err)
	}
	if _, err := parseMediaOperations(append(boundary, ' ')); err == nil {
		t.Fatal("parser accepted JSON exceeding the byte budget")
	}
}

func TestMediaOperationsJSONSnapshotRoundTrip(t *testing.T) {
	withoutOCR := mediaOperationsTestConfig()
	withoutOCR.OCR = MediaOperationsOCRConfig{}
	resourcesOnly := withoutOCR
	resourcesOnly.WritableProfiles = nil
	for _, test := range []struct {
		name string
		cfg  MediaOperationsConfig
	}{
		{"disabled", MediaOperationsConfig{}},
		{"resources_only", resourcesOnly},
		{"remux_only", withoutOCR},
		{"remux_and_ocr", mediaOperationsTestConfig()},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := mediaOperationsTestJSON(t, test.cfg)
			got, err := parseMediaOperations(data)
			if err != nil || !reflect.DeepEqual(got, test.cfg) {
				t.Fatalf("configuration snapshot round trip = %+v, want %+v: %v", got, test.cfg, err)
			}
		})
	}
}
