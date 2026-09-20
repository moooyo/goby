package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"
)

// AnalysisGeometryProfile admits exact orthogonal display matrices without
// reflection, scaling, translation, or perspective. Rotation is applied before
// source frame auditing. Known SAR is verified on that rotated raster. An
// explicitly unknown source SAR remains unknown in the audit and uses a square
// pixel display fallback, following FFplay n8.0 calculate_display_rect. Output
// pixels are normalized only after sampling; final dimensions use the exact
// effective display ratio without claiming an unknown source SAR was observed.
const AnalysisGeometryProfile = "orthogonal-display-sar-v2"

const (
	maxAnalysisGeometryJSON     = 64 << 10
	maxAnalysisGeometrySideData = 16
	maxAnalysisGeometryMatrix   = 512
)

type analysisDisplayGeometry struct {
	width, height int
	// SAR describes the effective display ratio; the flag retains source proof.
	sarNumerator, sarDenominator int64
	sourceSARUnknown             bool
	clockwise                    int
	ffprobeSHA                   string
}

type analysisGeometryDocument struct {
	Programs     []json.RawMessage        `json:"programs"`
	StreamGroups []json.RawMessage        `json:"stream_groups"`
	Streams      []analysisGeometryStream `json:"streams"`
}

type analysisGeometryStream struct {
	Index             *int                       `json:"index"`
	CodecType         string                     `json:"codec_type"`
	Width             int                        `json:"width"`
	Height            int                        `json:"height"`
	TimeBase          string                     `json:"time_base"`
	SampleAspectRatio json.RawMessage            `json:"sample_aspect_ratio,omitempty"`
	SideData          []analysisGeometrySideData `json:"side_data_list"`
	Tags              struct {
		Rotate string `json:"rotate"`
	} `json:"tags"`
}

type analysisGeometrySideData struct {
	Type     string `json:"side_data_type"`
	Matrix   string `json:"displaymatrix"`
	Rotation *int   `json:"rotation"`
}

// analysisGeometry inspects only the authorized descriptor. It does not change
// the global prober schema or reinterpret cached Info as display geometry.
func (extractor AnalysisExtractor) analysisGeometry(ctx context.Context, input *os.File, stream Stream, limits AnalysisLimits) (geometry analysisDisplayGeometry, resultErr error) {
	tool, err := analysisOpenToolExpected(ctx, extractor.FFprobePath, extractor.ExpectedFFprobeSHA256)
	if err != nil {
		return geometry, err
	}
	defer tool.file.Close()
	defer func() {
		if err := tool.check(); err != nil {
			geometry, resultErr = analysisDisplayGeometry{}, err
		}
	}()
	if err := analysisValidateFFprobe(ctx, tool); err != nil {
		return geometry, err
	}
	args := []string{"-v", "error", "-max_alloc", "268435456", "-threads", "1",
		"-max_pixels", strconv.FormatInt(limits.MaxSourcePixels, 10), "-probesize", "8388608", "-analyzeduration", "10000000", "-max_probe_packets", "256",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mpegts,avi",
		"-select_streams", strconv.Itoa(stream.Index),
		"-show_entries", "stream=index,codec_type,width,height,time_base,sample_aspect_ratio:stream_side_data=side_data_type,displaymatrix,rotation:stream_tags=rotate",
		"-of", "json", "-i", "/proc/self/fd/3"}
	sink := &analysisDiscardStderr{}
	err = runAnalysisProcess(ctx, "/proc/self/fd/4", input, nil, args, min(limits.Timeout, 30*time.Second), maxAnalysisGeometryJSON, sink,
		func(reader io.Reader) error {
			var err error
			geometry, err = parseAnalysisGeometry(reader, stream, limits)
			return err
		}, tool.file)
	if err == nil {
		err = sink.failure()
	}
	if err != nil {
		return analysisDisplayGeometry{}, err
	}
	geometry.ffprobeSHA = tool.sha
	return geometry, nil
}

func parseAnalysisGeometry(reader io.Reader, expected Stream, limits AnalysisLimits) (analysisDisplayGeometry, error) {
	bounded := &io.LimitedReader{R: reader, N: maxAnalysisGeometryJSON + 1}
	data, err := io.ReadAll(bounded)
	if bounded.N <= 0 || len(data) > maxAnalysisGeometryJSON {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: display geometry JSON", ErrAnalysisBudget)
	}
	if err != nil {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: incomplete display geometry JSON", ErrAnalysisUnproven)
	}
	if _, err := mediaEditDecodeJSON(data); err != nil {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: invalid or duplicate display geometry evidence", ErrAnalysisUnproven)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document analysisGeometryDocument
	if err := decoder.Decode(&document); err != nil {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: invalid display geometry JSON", ErrAnalysisUnproven)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: trailing display geometry JSON", ErrAnalysisUnproven)
	}
	if len(document.Streams) != 1 || len(document.Programs) > 16 || len(document.StreamGroups) > 16 {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: ambiguous display geometry stream", ErrAnalysisUnproven)
	}
	stream := document.Streams[0]
	actualBase, actualBaseErr := analysisTimeBase(stream.TimeBase)
	expectedBase, expectedBaseErr := analysisTimeBase(expected.TimeBase)
	if stream.Index == nil || *stream.Index != expected.Index || stream.CodecType != "video" || stream.Width != expected.Width || stream.Height != expected.Height ||
		stream.Width <= 0 || stream.Height <= 0 || actualBaseErr != nil || expectedBaseErr != nil || actualBase.Cmp(expectedBase) != 0 {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: display geometry does not match the selected stream", ErrAnalysisUnproven)
	}
	if int64(stream.Width) > limits.MaxSourcePixels/int64(stream.Height) || len(stream.SideData) > maxAnalysisGeometrySideData {
		return analysisDisplayGeometry{}, fmt.Errorf("%w: display geometry dimensions or side data", ErrAnalysisBudget)
	}
	sar, sourceSARUnknown, err := analysisGeometrySAR(stream.SampleAspectRatio)
	if err != nil {
		return analysisDisplayGeometry{}, err
	}
	geometry := analysisDisplayGeometry{width: stream.Width, height: stream.Height,
		sarNumerator: sar.Num().Int64(), sarDenominator: sar.Denom().Int64(), sourceSARUnknown: sourceSARUnknown}
	matrixSeen := false
	for _, side := range stream.SideData {
		if len(side.Type) > 128 || len(side.Matrix) > maxAnalysisGeometryMatrix {
			return analysisDisplayGeometry{}, fmt.Errorf("%w: display matrix metadata", ErrAnalysisBudget)
		}
		if side.Type != "Display Matrix" {
			if side.Matrix != "" || side.Rotation != nil {
				return analysisDisplayGeometry{}, fmt.Errorf("%w: unexpected display matrix side data", ErrAnalysisUnproven)
			}
			continue
		}
		if matrixSeen || side.Matrix == "" || side.Rotation == nil {
			return analysisDisplayGeometry{}, fmt.Errorf("%w: ambiguous or incomplete display matrix", ErrAnalysisUnproven)
		}
		matrixSeen = true
		geometry.clockwise, err = analysisOrthogonalRotation(side.Matrix)
		if err != nil || *side.Rotation < -180 || *side.Rotation > 180 || analysisNormalizeRotation(*side.Rotation) != analysisNormalizeRotation(-geometry.clockwise) {
			return analysisDisplayGeometry{}, fmt.Errorf("%w: unsupported or inconsistent display matrix", ErrAnalysisUnproven)
		}
	}
	if stream.Tags.Rotate != "" {
		angle, err := analysisGeometryTagRotation(stream.Tags.Rotate)
		if err != nil || analysisNormalizeRotation(angle) != analysisNormalizeRotation(-geometry.clockwise) {
			return analysisDisplayGeometry{}, fmt.Errorf("%w: unproven legacy rotation tag", ErrAnalysisUnproven)
		}
	}
	if geometry.clockwise == 90 || geometry.clockwise == 270 {
		geometry.width, geometry.height = geometry.height, geometry.width
		// Invert the effective display ratio. FFmpeg's transpose preserves a
		// decoded unknown 0/1 SAR; sourceSARUnknown keeps that separate audit.
		geometry.sarNumerator, geometry.sarDenominator = geometry.sarDenominator, geometry.sarNumerator
	}
	return geometry, nil
}

func analysisGeometrySAR(raw json.RawMessage) (*big.Rat, bool, error) {
	if len(raw) == 0 {
		return big.NewRat(1, 1), true, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, false, fmt.Errorf("%w: malformed display sample aspect ratio", ErrAnalysisUnproven)
	}
	if text == "N/A" || text == "0:1" {
		return big.NewRat(1, 1), true, nil
	}
	// A present null/empty value or a malformed rational is not an absent SAR.
	// Keep the positive time-base parser strict; only SAR has a display fallback.
	sar, err := analysisTimeBase(strings.ReplaceAll(text, ":", "/"))
	if err != nil || strings.Count(text, ":") != 1 {
		return nil, false, fmt.Errorf("%w: invalid display sample aspect ratio", ErrAnalysisUnproven)
	}
	return sar, false, nil
}

func analysisGeometryTagRotation(text string) (int, error) {
	if len(text) > 64 {
		return 0, ErrAnalysisUnproven
	}
	parts := strings.Split(strings.TrimSpace(text), ".")
	if len(parts) > 2 || len(parts) == 2 && (parts[1] == "" || strings.Trim(parts[1], "0") != "") {
		return 0, ErrAnalysisUnproven
	}
	angle, err := strconv.Atoi(parts[0])
	if err != nil || angle < -360 || angle > 360 || angle%90 != 0 {
		return 0, ErrAnalysisUnproven
	}
	return angle, nil
}

func analysisOrthogonalRotation(text string) (int, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 3 {
		return 0, ErrAnalysisUnproven
	}
	var matrix [9]int64
	for row, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 4 || fields[0] != fmt.Sprintf("%08d:", row) {
			return 0, ErrAnalysisUnproven
		}
		for column := 0; column < 3; column++ {
			value, err := strconv.ParseInt(fields[column+1], 10, 32)
			if err != nil {
				return 0, ErrAnalysisUnproven
			}
			matrix[row*3+column] = value
		}
	}
	// These are exactly FFmpeg's fixed-point matrices for the four rotations.
	// FFmpeg's display-matrix angle is counterclockwise; filter directions below
	// follow fftools get_rotation and its explicit clockwise/cclock branches.
	for index, supported := range [][9]int64{
		{65536, 0, 0, 0, 65536, 0, 0, 0, 1073741824},
		{0, 65536, 0, -65536, 0, 0, 0, 0, 1073741824},
		{-65536, 0, 0, 0, -65536, 0, 0, 0, 1073741824},
		{0, -65536, 0, 65536, 0, 0, 0, 0, 1073741824},
	} {
		if matrix == supported {
			return index * 90, nil
		}
	}
	return 0, ErrAnalysisUnproven
}

func analysisNormalizeRotation(angle int) int {
	angle %= 360
	if angle < 0 {
		angle += 360
	}
	return angle
}

func (geometry analysisDisplayGeometry) filter() string {
	switch geometry.clockwise {
	case 90:
		return "transpose=clock,"
	case 180:
		return "hflip,vflip,"
	case 270:
		return "transpose=cclock,"
	default:
		return ""
	}
}

func analysisSquareGeometry(stream Stream) analysisDisplayGeometry {
	return analysisDisplayGeometry{width: stream.Width, height: stream.Height, sarNumerator: 1, sarDenominator: 1}
}

// previewHeight rounds the exact observed display ratio to the nearest output
// pixel. There is no rounded intermediate SAR-normalization raster.
func (geometry analysisDisplayGeometry) previewHeight(width int, limits AnalysisLimits) (int, error) {
	if width <= 0 || geometry.width <= 0 || geometry.height <= 0 || geometry.sarNumerator <= 0 || geometry.sarDenominator <= 0 {
		return 0, ErrAnalysisUnproven
	}
	numerator := new(big.Int).Mul(big.NewInt(int64(width)), big.NewInt(int64(geometry.height)))
	numerator.Mul(numerator, big.NewInt(geometry.sarDenominator))
	denominator := new(big.Int).Mul(big.NewInt(int64(geometry.width)), big.NewInt(geometry.sarNumerator))
	numerator.Add(numerator, new(big.Int).Quo(new(big.Int).Set(denominator), big.NewInt(2)))
	height := new(big.Int).Quo(numerator, denominator)
	if !height.IsInt64() || height.Sign() <= 0 || height.Int64() > limits.MaxFramePixels/int64(width) {
		return 0, fmt.Errorf("%w: normalized display raster", ErrAnalysisBudget)
	}
	return int(height.Int64()), nil
}
