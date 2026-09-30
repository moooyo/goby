package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxInputManifestBytes = 1 << 20
	maxExtractionBytes    = 2 << 20
	maxGzipBytes          = 12 << 20
	expectedFrames        = 1200
	extractionImage       = "sha256:45dc7d9ff3eefbe79f6c8205ce2fda332777d1c1f2ecc22ef491fe7ab51bf89d"
	extractionFFmpeg      = "b0d61d340336ae7260493404a72a6584684f2201eab795204cf2c6ba4c71b5d1"
	extractionFFprobe     = "10624ede2701b47457ed880c12cf72ca25927dd00143f96df170c329a37ecec5"
	extractionPolicy      = "First actual source frame at or after each nominal slot; complete slots required."
)

type inputSource struct {
	ID                 string `json:"id"`
	EpisodeKey         string `json:"episodeKey"`
	ManifestPath       string `json:"manifestPath"`
	SourceSHA256       string `json:"sourceSha256"`
	ManifestSHA256     string `json:"manifestSha256"`
	OrchestratorSHA256 string `json:"orchestratorSha256"`
	DriverSHA256       string `json:"driverSha256"`
}

type inputManifest struct {
	Sources     []inputSource   `json:"sources"`
	DerivedFrom *manifestOrigin `json:"derivedFrom,omitempty"`
}

type manifestOrigin struct {
	ManifestPath string `json:"manifestPath"`
	SHA256       string `json:"sha256"`
}

type extractionManifest struct {
	SchemaVersion       int    `json:"schemaVersion"`
	CaseID              string `json:"caseId"`
	Complete            bool   `json:"complete"`
	MatcherExecuted     bool   `json:"matcherExecuted"`
	DetectorOutputsUsed bool   `json:"detectorOutputsUsed"`
	Raster              struct {
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		PixelFormat string `json:"pixelFormat"`
		FrameBytes  int    `json:"frameBytes"`
		FullFrame   bool   `json:"fullFrame"`
		ScaleFlags  string `json:"scaleFlags"`
	} `json:"raster"`
	Frames []extractionFrame `json:"frames"`
	Raw    struct {
		Frames     int    `json:"frames"`
		Bytes      int    `json:"bytes"`
		SHA256     string `json:"sha256"`
		GzipFile   string `json:"gzipFile"`
		GzipBytes  int    `json:"gzipBytes"`
		GzipSHA256 string `json:"gzipSHA256"`
	} `json:"raw"`
	Sampling struct {
		IntervalTicks  int64  `json:"intervalTicks"`
		EndTicks       int64  `json:"endTicks"`
		StartTicks     int64  `json:"startTicks"`
		TicksPerSecond int64  `json:"ticksPerSecond"`
		Policy         string `json:"policy"`
	} `json:"sampling"`
	Source struct {
		Path             string          `json:"path"`
		SHA256           string          `json:"sha256"`
		FormatStartTicks int64           `json:"formatStartTicks"`
		IdentityBefore   json.RawMessage `json:"identityBefore"`
		IdentityAfter    json.RawMessage `json:"identityAfter"`
	} `json:"source"`
	Tool struct {
		Image              string `json:"image"`
		FFmpegSHA256       string `json:"ffmpegSHA256"`
		FFprobeSHA256      string `json:"ffprobeSHA256"`
		OrchestratorSHA256 string `json:"orchestratorSHA256"`
		DriverSHA256       string `json:"driverSHA256"`
	} `json:"tool"`
	Audit struct {
		ExitCode               int  `json:"exitCode"`
		FullRawReadbackMatched bool `json:"fullRawReadbackMatched"`
	} `json:"audit"`
}

type extractionFrame struct {
	Index         int    `json:"index"`
	NominalTicks  int64  `json:"nominalTicks"`
	ActualTicks   int64  `json:"actualTicks"`
	SourceOrdinal int    `json:"sourceOrdinal"`
	RawOffset     int    `json:"rawOffset"`
	RawBytes      int    `json:"rawBytes"`
	RawSHA256     string `json:"rawSHA256"`
}

func validateSourceIdentity(data []byte) error {
	if err := requireFields(data, "device", "inode", "size", "mode", "mtimeNs", "ctimeNs"); err != nil {
		return err
	}
	var identity struct {
		Device  uint64 `json:"device"`
		Inode   uint64 `json:"inode"`
		Size    int64  `json:"size"`
		Mode    uint32 `json:"mode"`
		MtimeNs int64  `json:"mtimeNs"`
		CtimeNs int64  `json:"ctimeNs"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return err
	}
	if identity.Size < 0 || identity.Mode&0170000 != 0100000 {
		return errors.New("source identity must describe a regular file with a nonnegative size")
	}
	return nil
}

type source struct {
	Info       inputSource
	PTS        []float64
	Raw        []byte
	GraySHA256 string
	PTSSHA256  string
}

type sourceBinding struct {
	ID           string  `json:"id"`
	SourceSHA256 string  `json:"sourceSha256"`
	GraySHA256   string  `json:"graySha256"`
	PTSSHA256    string  `json:"ptsSha256"`
	Frames       int     `json:"frames"`
	FirstPTS     float64 `json:"firstPTS"`
	LastPTS      float64 `json:"lastPTS"`
}

type admittedSource struct {
	Source                        sourceBinding `json:"source"`
	EpisodeKey                    string        `json:"episodeKey"`
	ManifestPath                  string        `json:"manifestPath"`
	ManifestSHA256                string        `json:"manifestSha256"`
	GzipPath                      string        `json:"gzipPath"`
	GzipSHA256                    string        `json:"gzipSha256"`
	GzipBytes                     int           `json:"gzipBytes"`
	OrchestratorSHA256            string        `json:"orchestratorSha256"`
	DriverSHA256                  string        `json:"driverSha256"`
	IndividualFrameHashesVerified bool          `json:"individualFrameHashesVerified"`
}

func binding(s source) sourceBinding {
	return sourceBinding{s.Info.ID, s.Info.SourceSHA256, s.GraySHA256, s.PTSSHA256, len(s.PTS), s.PTS[0], s.PTS[len(s.PTS)-1]}
}

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func validHash(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == sha256.Size && value == strings.ToLower(value)
}

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var episodeKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,255}$`)

func canonicalPath(base, name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", errors.New("empty or invalid path")
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(base, name)
	}
	return filepath.Abs(filepath.Clean(name))
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func readRegular(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > limit {
		return nil, errors.New("input is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, file}, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if len(data) > int(limit) || int64(len(data)) != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !os.SameFile(before, after) {
		return nil, errors.New("input exceeded its bound or changed while reading")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func foldedJSONKey(key string) string {
	var result strings.Builder
	for _, initial := range key {
		canonical := initial
		for next := unicode.SimpleFold(initial); next != initial; next = unicode.SimpleFold(next) {
			if next < canonical {
				canonical = next
			}
		}
		result.WriteRune(canonical)
	}
	return result.String()
}

// Reject duplicate object keys, including Unicode case aliases accepted by encoding/json.
// Extraction manifests intentionally permit additional audited metadata fields.
func checkJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("JSON must contain valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON nesting exceeds bound")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok {
					return errors.New("invalid JSON object key")
				}
				key = foldedJSONKey(key)
				if seen[key] {
					return errors.New("duplicate JSON object key")
				}
				seen[key] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON value or data")
	}
	return nil
}

func requireFields(data []byte, fields ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("JSON object is required")
	}
	for _, field := range fields {
		parts := strings.SplitN(field, ".", 2)
		value, ok := object[parts[0]]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("missing or null field %s", field)
		}
		if len(parts) == 2 {
			if err := requireFields(value, parts[1]); err != nil {
				return fmt.Errorf("field %s: %w", parts[0], err)
			}
		}
	}
	return nil
}

func parseInput(data []byte, base string) (inputManifest, error) {
	var manifest inputManifest
	if err := checkJSON(data); err != nil {
		return manifest, err
	}
	if err := requireFields(data, "sources"); err != nil {
		return manifest, err
	}
	var raw struct {
		Sources []json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return manifest, err
	}
	for _, item := range raw.Sources {
		if err := requireFields(item, "id", "episodeKey", "manifestPath", "sourceSha256", "manifestSha256", "orchestratorSha256", "driverSha256"); err != nil {
			return manifest, err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	if len(manifest.Sources) != 3 {
		return manifest, errors.New("cohort requires exactly three independent sources")
	}
	if manifest.DerivedFrom != nil {
		if err := requireFields(data, "derivedFrom.manifestPath", "derivedFrom.sha256"); err != nil {
			return manifest, err
		}
		if !validHash(manifest.DerivedFrom.SHA256) {
			return manifest, errors.New("invalid upstream manifest SHA256")
		}
		path, err := canonicalPath(base, manifest.DerivedFrom.ManifestPath)
		if err != nil {
			return manifest, err
		}
		manifest.DerivedFrom.ManifestPath = path
	}
	ids, hashes, paths, episodes := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := range manifest.Sources {
		info := &manifest.Sources[i]
		if !sourceIDPattern.MatchString(info.ID) || ids[strings.ToLower(info.ID)] {
			return manifest, errors.New("invalid or duplicate canonical source ID")
		}
		ids[strings.ToLower(info.ID)] = true
		if !episodeKeyPattern.MatchString(info.EpisodeKey) || episodes[info.EpisodeKey] {
			return manifest, errors.New("invalid or duplicate original episodeKey; distinct encodes of one work are not independent episodes")
		}
		episodes[info.EpisodeKey] = true
		if !validHash(info.SourceSHA256) || !validHash(info.ManifestSHA256) || !validHash(info.OrchestratorSHA256) || !validHash(info.DriverSHA256) {
			return manifest, errors.New("source requires canonical lowercase SHA256 bindings")
		}
		if hashes[info.SourceSHA256] {
			return manifest, errors.New("duplicate source content")
		}
		hashes[info.SourceSHA256] = true
		path, err := canonicalPath(base, info.ManifestPath)
		if err != nil {
			return manifest, err
		}
		if paths[path] {
			return manifest, errors.New("duplicate extraction manifest path")
		}
		paths[path] = true
		info.ManifestPath = path
	}
	sort.Slice(manifest.Sources, func(i, j int) bool { return manifest.Sources[i].ID < manifest.Sources[j].ID })
	return manifest, nil
}

func inflateSingle(ctx context.Context, data []byte, size int) ([]byte, error) {
	compressed := bytes.NewReader(data)
	z, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, err
	}
	z.Multistream(false)
	raw, readErr := io.ReadAll(io.LimitReader(contextReader{ctx, z}, int64(size)+1))
	closeErr := z.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(raw) != size {
		return nil, errors.New("gzip uncompressed size mismatch or expansion limit exceeded")
	}
	if compressed.Len() != 0 {
		return nil, errors.New("gzip has trailing data or additional members")
	}
	return raw, ctx.Err()
}

func loadSource(ctx context.Context, info inputSource) (source, admittedSource, error) {
	var admission admittedSource
	data, err := readRegular(ctx, info.ManifestPath, maxExtractionBytes)
	if err != nil {
		return source{}, admission, err
	}
	if digest(data) != info.ManifestSHA256 {
		return source{}, admission, errors.New("extraction manifest SHA256 mismatch")
	}
	if err = checkJSON(data); err != nil {
		return source{}, admission, err
	}
	if err = requireFields(data, "schemaVersion", "caseId", "complete", "matcherExecuted", "detectorOutputsUsed", "raster.width", "raster.height", "raster.pixelFormat", "raster.frameBytes", "raster.fullFrame", "raster.scaleFlags", "frames", "raw.frames", "raw.bytes", "raw.sha256", "raw.gzipFile", "raw.gzipBytes", "raw.gzipSHA256", "sampling.intervalTicks", "sampling.endTicks", "sampling.startTicks", "sampling.ticksPerSecond", "sampling.policy", "source.path", "source.sha256", "source.formatStartTicks", "source.identityBefore", "source.identityAfter", "tool.image", "tool.ffmpegSHA256", "tool.ffprobeSHA256", "tool.orchestratorSHA256", "tool.driverSHA256", "audit.exitCode", "audit.fullRawReadbackMatched"); err != nil {
		return source{}, admission, err
	}
	var rawFrames struct {
		Frames []json.RawMessage `json:"frames"`
	}
	if err = json.Unmarshal(data, &rawFrames); err != nil {
		return source{}, admission, err
	}
	for _, item := range rawFrames.Frames {
		if err = requireFields(item, "index", "nominalTicks", "actualTicks", "sourceOrdinal", "rawOffset", "rawBytes", "rawSHA256"); err != nil {
			return source{}, admission, err
		}
	}
	var extraction extractionManifest
	if err = json.Unmarshal(data, &extraction); err != nil {
		return source{}, admission, err
	}
	e := &extraction
	if e.SchemaVersion != 1 || e.CaseID != info.ID || !e.Complete || e.MatcherExecuted || e.DetectorOutputsUsed || e.Raster.Width != 96 || e.Raster.Height != 96 || e.Raster.PixelFormat != "gray" || e.Raster.FrameBytes != frameBytes || !e.Raster.FullFrame || e.Raster.ScaleFlags != "area" || e.Sampling.IntervalTicks != 1_000_000 || e.Sampling.EndTicks != 1_200_000_000 || e.Sampling.StartTicks != 0 || e.Sampling.TicksPerSecond != 10_000_000 || e.Sampling.Policy != extractionPolicy {
		return source{}, admission, errors.New("incomplete or incompatible extraction contract")
	}
	if e.Source.FormatStartTicks != 0 || len(e.Source.IdentityBefore) == 0 || e.Source.IdentityBefore[0] != '{' || !bytes.Equal(e.Source.IdentityBefore, e.Source.IdentityAfter) || e.Audit.ExitCode != 0 || !e.Audit.FullRawReadbackMatched {
		return source{}, admission, errors.New("incompatible extraction audit")
	}
	if err := validateSourceIdentity(e.Source.IdentityBefore); err != nil {
		return source{}, admission, fmt.Errorf("incomplete source identity audit: %w", err)
	}
	if e.Tool.Image != extractionImage || e.Tool.FFmpegSHA256 != extractionFFmpeg || e.Tool.FFprobeSHA256 != extractionFFprobe || e.Tool.OrchestratorSHA256 != info.OrchestratorSHA256 || e.Tool.DriverSHA256 != info.DriverSHA256 || e.Source.SHA256 != info.SourceSHA256 {
		return source{}, admission, errors.New("frozen extraction tool or source identity mismatch")
	}
	if len(e.Frames) != expectedFrames || e.Raw.Frames != expectedFrames || e.Raw.Bytes != expectedFrames*frameBytes || e.Raw.GzipBytes <= 0 || e.Raw.GzipBytes > maxGzipBytes || !validHash(e.Raw.SHA256) || !validHash(e.Raw.GzipSHA256) {
		return source{}, admission, errors.New("invalid bounded extraction sizes or hashes")
	}
	pts := make([]float64, expectedFrames)
	for i, f := range e.Frames {
		if err := ctx.Err(); err != nil {
			return source{}, admission, err
		}
		if f.Index != i || f.NominalTicks != int64(i)*1_000_000 || f.RawOffset != i*frameBytes || f.RawBytes != frameBytes || f.SourceOrdinal < 0 || i > 0 && f.SourceOrdinal <= e.Frames[i-1].SourceOrdinal || f.ActualTicks < f.NominalTicks || f.ActualTicks >= f.NominalTicks+1_000_000 || !validHash(f.RawSHA256) {
			return source{}, admission, fmt.Errorf("invalid frame ordering, ownership, PTS, or hash at frame %d", i)
		}
		pts[i] = float64(f.ActualTicks) / 10_000_000
	}
	gzipPath, err := canonicalPath(filepath.Dir(info.ManifestPath), e.Raw.GzipFile)
	if err != nil {
		return source{}, admission, err
	}
	compressed, err := readRegular(ctx, gzipPath, int64(e.Raw.GzipBytes))
	if err != nil {
		return source{}, admission, err
	}
	if len(compressed) != e.Raw.GzipBytes || digest(compressed) != e.Raw.GzipSHA256 {
		return source{}, admission, errors.New("gzip SHA256 or size mismatch")
	}
	raw, err := inflateSingle(ctx, compressed, e.Raw.Bytes)
	if err != nil {
		return source{}, admission, err
	}
	if digest(raw) != e.Raw.SHA256 {
		return source{}, admission, errors.New("raw SHA256 mismatch")
	}
	for i, f := range e.Frames {
		if err := ctx.Err(); err != nil {
			return source{}, admission, err
		}
		if digest(raw[i*frameBytes:(i+1)*frameBytes]) != f.RawSHA256 {
			return source{}, admission, fmt.Errorf("frame SHA256 mismatch at frame %d", i)
		}
	}
	s := source{Info: info, PTS: pts, Raw: raw, GraySHA256: e.Raw.SHA256, PTSSHA256: info.ManifestSHA256}
	admission = admittedSource{Source: binding(s), EpisodeKey: info.EpisodeKey, ManifestPath: info.ManifestPath, ManifestSHA256: info.ManifestSHA256, GzipPath: gzipPath, GzipSHA256: e.Raw.GzipSHA256, GzipBytes: e.Raw.GzipBytes, OrchestratorSHA256: info.OrchestratorSHA256, DriverSHA256: info.DriverSHA256, IndividualFrameHashesVerified: true}
	return s, admission, nil
}
