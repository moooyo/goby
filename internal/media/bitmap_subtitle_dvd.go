package media

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"
)

// DVD SPU command dates use 1024/90000-second units. Rounding once to source
// ticks avoids the millisecond quantization used by some playback renderers.
func dvdCommandTicks(date uint16) int64 {
	return int64(date) * 1024 * TicksPerSecond / 90000
}

type dvdSubtitleStyle struct {
	colors [4]byte
	alpha  [4]byte
}

type dvdSubtitleState struct {
	style               dvdSubtitleStyle
	x, y, width, height int
	even, odd           int
	geometry, offsets   bool
	visible, forced     bool
}

func decodeDVDSubtitles(ctx context.Context, packets []bitmapSubtitlePacket, extra []byte, limits BitmapSubtitleLimits) ([]BitmapSubtitleCue, []string, error) {
	var result []BitmapSubtitleCue
	pixels := 0
	warnings, err := walkDVDSubtitles(ctx, packets, extra, limits, func(cue BitmapSubtitleCue) error {
		count := cue.Image.Bounds().Dx() * cue.Image.Bounds().Dy()
		if count > limits.MaxTotalPixels-pixels {
			return fmt.Errorf("DVD batch bitmap pixel limit")
		}
		pixels += count
		result = append(result, cue)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return result, warnings, nil
}

func walkDVDSubtitles(ctx context.Context, packets []bitmapSubtitlePacket, extra []byte, limits BitmapSubtitleLimits, emit func(BitmapSubtitleCue) error) ([]string, error) {
	if len(packets) == 0 || limits.MaxCues <= 0 || limits.MaxPixels <= 0 || limits.MaxTotalPixels <= 0 || limits.MaxPacketBytes <= 0 {
		return nil, fmt.Errorf("invalid DVD subtitle limits")
	}
	palette, hasPalette, err := dvdSubtitlePalette(extra)
	if err != nil {
		return nil, err
	}
	count := 0
	consume := func(cue BitmapSubtitleCue) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count >= limits.MaxCues || emit == nil {
			return fmt.Errorf("DVD decoded cue limit")
		}
		count++
		return emit(cue)
	}
	var warnings []string
	if !hasPalette {
		warnings = append(warnings, "dvd_palette_missing_monochrome_review")
	}
	style := dvdSubtitleStyle{}
	var pending []byte
	var pendingPTS, pendingDuration int64
	totalBytes := 0
	// Work includes transparent and superseded rasters, plus crop allocations.
	// It cannot be refunded by same-time commands that never retain a cue.
	remainingWorkPixels := limits.MaxWorkPixels
	if remainingWorkPixels == 0 {
		remainingWorkPixels = int64(limits.MaxTotalPixels) * 2
	}
	if remainingWorkPixels <= 0 {
		return nil, fmt.Errorf("DVD pixel budget overflow")
	}
	for _, packet := range packets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		totalBytes += len(packet.Data)
		if totalBytes > limits.MaxPacketBytes || len(packet.Data) == 0 {
			return nil, fmt.Errorf("DVD packet byte limit")
		}
		if len(pending) == 0 {
			pendingPTS, pendingDuration = packet.PTS, packet.Duration
		}
		if len(pending)+len(packet.Data) > 65535 {
			return nil, fmt.Errorf("DVD SPU packet too large")
		}
		pending = append(pending, packet.Data...)
		if len(pending) < 4 {
			continue
		}
		size := int(binary.BigEndian.Uint16(pending[:2]))
		if size == 0 {
			return nil, fmt.Errorf("HD-DVD subpictures are unsupported")
		}
		if size < 10 || size < len(pending) {
			return nil, fmt.Errorf("invalid DVD SPU packet length")
		}
		if len(pending) < size {
			continue
		}
		_, packetWarnings, nextStyle, err := decodeDVDSPU(ctx, pending, pendingPTS, pendingDuration, palette, hasPalette, style, limits, &remainingWorkPixels, consume)
		if err != nil {
			return nil, err
		}
		style = nextStyle
		for _, warning := range packetWarnings {
			if !subtitleWarningPresent(warnings, warning) {
				warnings = append(warnings, warning)
			}
		}
		pending = nil
	}
	if len(pending) != 0 {
		return nil, fmt.Errorf("truncated DVD SPU packet")
	}
	if count == 0 {
		return nil, fmt.Errorf("DVD track contains no visible subtitle cues")
	}
	return warnings, nil
}

func subtitleWarningPresent(warnings []string, value string) bool {
	for _, warning := range warnings {
		if warning == value {
			return true
		}
	}
	return false
}

func dvdSubtitlePalette(extra []byte) ([16]color.NRGBA, bool, error) {
	var palette [16]color.NRGBA
	if len(extra) > 64<<10 {
		return palette, false, fmt.Errorf("DVD extra data limit")
	}
	text := strings.TrimRight(string(extra), "\x00")
	hasPalette := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "palette:") {
			continue
		}
		if hasPalette {
			return palette, false, fmt.Errorf("duplicate DVD palette")
		}
		entries := strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "palette:")), ",")
		if len(entries) != 16 {
			return palette, false, fmt.Errorf("invalid DVD palette length")
		}
		for index, entry := range entries {
			entry = strings.TrimSpace(entry)
			if len(entry) != 6 {
				return palette, false, fmt.Errorf("invalid DVD palette color")
			}
			rgb, err := strconv.ParseUint(entry, 16, 24)
			if err != nil {
				return palette, false, fmt.Errorf("invalid DVD palette color")
			}
			palette[index] = color.NRGBA{R: byte(rgb >> 16), G: byte(rgb >> 8), B: byte(rgb), A: 255}
		}
		hasPalette = true
	}
	return palette, hasPalette, nil
}

func decodeDVDSPU(ctx context.Context, data []byte, pts, duration int64, palette [16]color.NRGBA, hasPalette bool, style dvdSubtitleStyle, limits BitmapSubtitleLimits, remainingWorkPixels *int64, emit func(BitmapSubtitleCue) error) ([]BitmapSubtitleCue, []string, dvdSubtitleStyle, error) {
	control := int(binary.BigEndian.Uint16(data[2:4]))
	if control < 4 || control > len(data)-5 {
		return nil, nil, style, fmt.Errorf("invalid DVD control offset")
	}
	state := dvdSubtitleState{style: style}
	var cues []BitmapSubtitleCue
	var warnings []string
	var active *BitmapSubtitleCue
	pixels := 0
	lastDate := int64(-1)
	visited := map[int]bool{}
	position := control
	closeActive := func(end int64) error {
		if active == nil {
			return nil
		}
		if end < active.StartTicks {
			return fmt.Errorf("DVD display time moved backwards")
		}
		if end > active.StartTicks {
			active.EndTicks = end
			if emit != nil {
				if err := emit(*active); err != nil {
					return err
				}
			} else {
				cuePixels := active.Image.Bounds().Dx() * active.Image.Bounds().Dy()
				if len(cues) >= limits.MaxCues || cuePixels > limits.MaxTotalPixels-pixels {
					return fmt.Errorf("DVD cue or pixel limit")
				}
				pixels += cuePixels
				cues = append(cues, *active)
			}
		}
		active = nil
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, style, err
		}
		if len(visited) >= 4096 || visited[position] || position < control || position > len(data)-5 {
			return nil, nil, style, fmt.Errorf("invalid DVD control chain")
		}
		visited[position] = true
		date := dvdCommandTicks(binary.BigEndian.Uint16(data[position : position+2]))
		if date < lastDate {
			return nil, nil, style, fmt.Errorf("DVD control dates are unordered")
		}
		lastDate = date
		next := int(binary.BigEndian.Uint16(data[position+2 : position+4]))
		if next != position && (next <= position || next > len(data)-5) {
			return nil, nil, style, fmt.Errorf("invalid DVD next control offset")
		}
		end := len(data)
		if next != position {
			end = next
		}
		cursor := position + 4
		if err := closeActive(pts + date); err != nil {
			return nil, nil, style, err
		}
		terminated := false
		for cursor < end {
			command := data[cursor]
			cursor++
			need := func(count int) bool { return count >= 0 && cursor <= end-count }
			switch command {
			case 0x00:
				state.forced, state.visible = true, true
			case 0x01:
				state.visible = true
			case 0x02:
				state.visible = false
			case 0x03, 0x04:
				if !need(2) {
					return nil, nil, style, fmt.Errorf("truncated DVD color command")
				}
				values := [4]byte{data[cursor+1] & 15, data[cursor+1] >> 4, data[cursor] & 15, data[cursor] >> 4}
				if command == 3 {
					state.style.colors = values
				} else {
					state.style.alpha = values
				}
				cursor += 2
			case 0x05:
				if !need(6) {
					return nil, nil, style, fmt.Errorf("truncated DVD coordinates")
				}
				x1 := int(data[cursor])<<4 | int(data[cursor+1]>>4)
				x2 := int(data[cursor+1]&15)<<8 | int(data[cursor+2])
				y1 := int(data[cursor+3])<<4 | int(data[cursor+4]>>4)
				y2 := int(data[cursor+4]&15)<<8 | int(data[cursor+5])
				if x2 < x1 || y2 < y1 || x2-x1+1 > limits.MaxPixels/(y2-y1+1) {
					return nil, nil, style, fmt.Errorf("DVD bitmap pixel limit")
				}
				state.x, state.y, state.width, state.height, state.geometry = x1, y1, x2-x1+1, y2-y1+1, true
				cursor += 6
			case 0x06:
				if !need(4) {
					return nil, nil, style, fmt.Errorf("truncated DVD image offsets")
				}
				state.even = int(binary.BigEndian.Uint16(data[cursor : cursor+2]))
				state.odd = int(binary.BigEndian.Uint16(data[cursor+2 : cursor+4]))
				if state.even < 4 || state.even >= control || state.odd < 4 || state.odd >= control {
					return nil, nil, style, fmt.Errorf("invalid DVD image offsets")
				}
				state.offsets = true
				cursor += 4
			case 0xff:
				terminated = true
			default:
				return nil, nil, style, fmt.Errorf("unsupported DVD subpicture command 0x%02x", command)
			}
			if terminated {
				break
			}
		}
		if !terminated {
			return nil, nil, style, fmt.Errorf("unterminated DVD command sequence")
		}
		if state.visible {
			if !state.geometry || !state.offsets {
				return nil, nil, style, fmt.Errorf("DVD display is missing image state")
			}
			if state.width > (limits.MaxTotalPixels-pixels)/state.height {
				return nil, nil, style, fmt.Errorf("DVD total pixel limit")
			}
			bitmap, dx, dy, err := dvdSubtitleImage(ctx, data[:control], state, palette, hasPalette, limits, remainingWorkPixels)
			if err != nil {
				return nil, nil, style, err
			}
			if bitmap == nil {
				if !subtitleWarningPresent(warnings, "transparent_dvd_display_ignored") {
					warnings = append(warnings, "transparent_dvd_display_ignored")
				}
			} else {
				active = &BitmapSubtitleCue{StartTicks: pts + date, Image: bitmap, X: state.x + dx, Y: state.y + dy, Forced: state.forced}
			}
		}
		if next == position {
			break
		}
		position = next
	}
	if active != nil {
		end := pts + duration
		if duration <= 0 || end <= active.StartTicks {
			return nil, nil, style, fmt.Errorf("DVD display has no explicit end or packet duration")
		}
		if err := closeActive(end); err != nil {
			return nil, nil, style, err
		}
		warnings = append(warnings, "dvd_display_closed_at_packet_duration")
	}
	return cues, warnings, state.style, nil
}

type dvdNibbleReader struct {
	data  []byte
	index int
}

func (r *dvdNibbleReader) read() (int, error) {
	if r.index < 0 || r.index/2 >= len(r.data) {
		return 0, fmt.Errorf("truncated DVD bitmap run")
	}
	value := r.data[r.index/2]
	if r.index%2 == 0 {
		value >>= 4
	} else {
		value &= 15
	}
	r.index++
	return int(value), nil
}

func dvdSubtitleImage(ctx context.Context, data []byte, state dvdSubtitleState, palette [16]color.NRGBA, hasPalette bool, limits BitmapSubtitleLimits, remainingWorkPixels *int64) (*image.NRGBA, int, int, error) {
	if state.width <= 0 || state.height <= 0 || state.width > limits.MaxPixels/state.height {
		return nil, 0, 0, fmt.Errorf("DVD pixel limit")
	}
	if remainingWorkPixels == nil || *remainingWorkPixels < int64(state.width)*int64(state.height) {
		return nil, 0, 0, fmt.Errorf("DVD raster work pixel limit")
	}
	*remainingWorkPixels -= int64(state.width) * int64(state.height)
	colors := [4]color.NRGBA{}
	if hasPalette {
		for index := range colors {
			colors[index] = palette[state.style.colors[index]]
			colors[index].A = state.style.alpha[index] * 17
		}
	} else {
		var distinct []byte
		for index := range colors {
			if state.style.alpha[index] == 0 {
				continue
			}
			found := false
			for _, previous := range distinct {
				if previous == state.style.colors[index] {
					found = true
				}
			}
			if !found {
				distinct = append(distinct, state.style.colors[index])
			}
		}
		for index := range colors {
			for rank, value := range distinct {
				if value != state.style.colors[index] {
					continue
				}
				gray := byte(255)
				if len(distinct) > 1 {
					gray = byte(rank * 255 / (len(distinct) - 1))
				}
				colors[index] = color.NRGBA{R: gray, G: gray, B: gray, A: state.style.alpha[index] * 17}
			}
		}
	}
	bitmap := image.NewNRGBA(image.Rect(0, 0, state.width, state.height))
	for field, offset := range []int{state.even, state.odd} {
		reader := dvdNibbleReader{data: data, index: offset * 2}
		for y := field; y < state.height; y += 2 {
			if err := ctx.Err(); err != nil {
				return nil, 0, 0, err
			}
			for x := 0; x < state.width; {
				code := 0
				for threshold := 1; code < threshold && threshold <= 64; threshold *= 4 {
					nibble, err := reader.read()
					if err != nil {
						return nil, 0, 0, err
					}
					code = code*16 + nibble
				}
				count := code >> 2
				if code < 4 {
					count = state.width - x
				}
				if count <= 0 || count > state.width-x {
					return nil, 0, 0, fmt.Errorf("DVD run exceeds scan line")
				}
				value := colors[code&3]
				for right := x + count; x < right; x++ {
					bitmap.SetNRGBA(x, y, value)
				}
			}
			if reader.index%2 != 0 {
				reader.index++
			}
		}
	}
	left, top, right, bottom := state.width, state.height, 0, 0
	for y := 0; y < state.height; y++ {
		for x := 0; x < state.width; x++ {
			if bitmap.Pix[y*bitmap.Stride+x*4+3] == 0 {
				continue
			}
			left = min(left, x)
			top = min(top, y)
			right = max(right, x+1)
			bottom = max(bottom, y+1)
		}
	}
	if left == state.width {
		return nil, 0, 0, nil
	}
	if left == 0 && top == 0 && right == state.width && bottom == state.height {
		return bitmap, 0, 0, nil
	}
	if (right-left)*(bottom-top) > limits.MaxTotalPixels-state.width*state.height {
		return nil, 0, 0, fmt.Errorf("DVD crop working-set pixel limit")
	}
	if int64(right-left)*int64(bottom-top) > *remainingWorkPixels {
		return nil, 0, 0, fmt.Errorf("DVD crop work pixel limit")
	}
	*remainingWorkPixels -= int64(right-left) * int64(bottom-top)
	cropped := image.NewNRGBA(image.Rect(0, 0, right-left, bottom-top))
	for y := top; y < bottom; y++ {
		copy(cropped.Pix[(y-top)*cropped.Stride:], bitmap.Pix[y*bitmap.Stride+left*4:y*bitmap.Stride+right*4])
	}
	return cropped, left, top, nil
}
