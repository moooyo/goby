package media

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
)

const (
	pgsPaletteSegment      = 0x14
	pgsObjectSegment       = 0x15
	pgsPresentationSegment = 0x16
	pgsWindowSegment       = 0x17
	pgsEndSegment          = 0x80
	pgsMaxObjects          = 64
	pgsMaxPalettes         = 8
	pgsMaxReferences       = 2
)

type pgsPalette struct {
	colors  [256]color.NRGBA
	defined [256]bool
}

type pgsObject struct {
	version       byte
	width, height int
	expected      int
	rle           []byte
	indices       []byte
	complete      bool
}

type pgsReference struct {
	id      uint16
	window  byte
	x, y    int
	forced  bool
	crop    image.Rectangle
	cropped bool
}

type pgsPresentation struct {
	pts, fallbackEnd int64
	fallbackKnown    bool
	width, height    int
	palette          byte
	objects          []pgsReference
}

type pgsDecoder struct {
	ctx           context.Context
	limits        BitmapSubtitleLimits
	palettes      map[byte]*pgsPalette
	objects       map[uint16]*pgsObject
	windows       map[byte]image.Rectangle
	presentation  *pgsPresentation
	pending       []BitmapSubtitleCue
	emit          func(BitmapSubtitleCue) error
	emittedCues   int
	pendingPixels int
	fallbackEnd   int64
	fallbackKnown bool
	lastPTS       int64
	hasEvent      bool
	width, height int
	objectPixels  int
	objectBytes   int
	workPixels    int64
}

// decodePGSSubtitles is the bounded batch adapter used by callers that retain
// all images. Production OCR uses walkPGSSubtitles to release each image after
// processing instead of charging a whole movie against a resident pixel limit.
func decodePGSSubtitles(ctx context.Context, packets []bitmapSubtitlePacket, limits BitmapSubtitleLimits) ([]BitmapSubtitleCue, []string, error) {
	var cues []BitmapSubtitleCue
	pixels := 0
	warnings, err := walkPGSSubtitles(ctx, packets, limits, func(cue BitmapSubtitleCue) error {
		area := cue.Image.Bounds().Dx() * cue.Image.Bounds().Dy()
		if area > limits.MaxTotalPixels-pixels {
			return fmt.Errorf("retained bitmaps exceed the batch pixel budget")
		}
		pixels += area
		cues = append(cues, cue)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return cues, warnings, nil
}

// walkPGSSubtitles interprets display sets, not one image per packet. A clear
// presentation or the next display set ends the preceding visible interval.
// Callbacks complete synchronously before the next display set is rendered.
// Callers must stage results until success and supply their own retention
// budget if they keep images after the callback returns.
// Protocol references: FFmpeg libavcodec/pgssubdec.c and the HDMV PGS segment
// layout. The implementation deliberately rejects damaged or incomplete data.
func walkPGSSubtitles(ctx context.Context, packets []bitmapSubtitlePacket, limits BitmapSubtitleLimits, emit func(BitmapSubtitleCue) error) ([]string, error) {
	fail := func(err error) ([]string, error) {
		return nil, fmt.Errorf("%w: PGS: %w", ErrBitmapSubtitle, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if limits.MaxCues <= 0 || limits.MaxPixels <= 0 || limits.MaxTotalPixels <= 0 || limits.MaxPacketBytes <= 0 || limits.DurationTicks < 0 || limits.MaxWorkPixels < 0 || emit == nil {
		return fail(fmt.Errorf("invalid decoder limits"))
	}
	if limits.MaxWorkPixels == 0 {
		if int64(limits.MaxTotalPixels) > int64(^uint64(0)>>1)/2 {
			return fail(fmt.Errorf("invalid default work budget"))
		}
		limits.MaxWorkPixels = 2 * int64(limits.MaxTotalPixels)
	}
	if len(packets) == 0 {
		return fail(fmt.Errorf("no subtitle packets"))
	}
	decoder := pgsDecoder{ctx: ctx, limits: limits, emit: emit}
	decoder.resetEpoch()
	inputBytes := 0
	for packetIndex, packet := range packets {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if packet.Duration < 0 || packet.PTS > int64(^uint64(0)>>1)-packet.Duration {
			return fail(fmt.Errorf("packet %d has invalid timing", packetIndex))
		}
		if len(packet.Data) == 0 || len(packet.Data) > limits.MaxPacketBytes-inputBytes {
			return fail(fmt.Errorf("packet data exceeds the decoder budget"))
		}
		inputBytes += len(packet.Data)
		for offset := 0; offset < len(packet.Data); {
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			data := packet.Data[offset:]
			// SUP files add a ten-byte PG/PTS/DTS prefix. The caller's packet
			// timestamps remain authoritative because container offsets and
			// 90 kHz timestamp wrap have already been resolved there.
			if len(data) >= 2 && data[0] == 'P' && data[1] == 'G' {
				if len(data) < 13 {
					return fail(fmt.Errorf("packet %d has a truncated SUP segment", packetIndex))
				}
				offset += 10
				data = data[10:]
			}
			if len(data) < 3 {
				return fail(fmt.Errorf("packet %d has a truncated segment header", packetIndex))
			}
			kind, size := data[0], int(binary.BigEndian.Uint16(data[1:3]))
			if size > len(data)-3 {
				return fail(fmt.Errorf("packet %d has a truncated segment payload", packetIndex))
			}
			if err := decoder.segment(kind, data[3:3+size], packet); err != nil {
				return fail(fmt.Errorf("packet %d segment 0x%02x: %w", packetIndex, kind, err))
			}
			offset += size + 3
		}
	}
	if decoder.presentation != nil {
		return fail(fmt.Errorf("incomplete display set"))
	}
	var warnings []string
	if len(decoder.pending) != 0 {
		end := decoder.fallbackEnd
		known := decoder.fallbackKnown
		warning := "PGS final display interval ended at the declared packet duration."
		if limits.DurationTicks > 0 && limits.DurationTicks > decoder.lastPTS && (!known || end <= decoder.lastPTS || limits.DurationTicks < end) {
			end = limits.DurationTicks
			known = true
			warning = "PGS final display interval ended at the indexed source duration."
		}
		if !known || end <= decoder.lastPTS {
			return fail(fmt.Errorf("final display interval has no explicit end time"))
		}
		if err := decoder.closePending(end); err != nil {
			return fail(err)
		}
		warnings = append(warnings, warning)
	}
	if decoder.emittedCues == 0 {
		return fail(fmt.Errorf("no visible subtitle intervals"))
	}
	return warnings, nil
}

func (d *pgsDecoder) resetEpoch() {
	d.palettes = make(map[byte]*pgsPalette)
	d.objects = make(map[uint16]*pgsObject)
	d.windows = make(map[byte]image.Rectangle)
	d.objectPixels, d.objectBytes = 0, 0
}

func (d *pgsDecoder) segment(kind byte, payload []byte, packet bitmapSubtitlePacket) error {
	if kind == pgsPresentationSegment {
		return d.parsePresentation(payload, packet)
	}
	if d.presentation == nil {
		return fmt.Errorf("segment outside a presentation display set")
	}
	switch kind {
	case pgsPaletteSegment:
		return d.parsePalette(payload)
	case pgsObjectSegment:
		return d.parseObject(payload)
	case pgsWindowSegment:
		return d.parseWindows(payload)
	case pgsEndSegment:
		if len(payload) != 0 {
			return fmt.Errorf("end segment is not empty")
		}
		return d.display(packet)
	default:
		return fmt.Errorf("unsupported segment type")
	}
}

func (d *pgsDecoder) parsePresentation(payload []byte, packet bitmapSubtitlePacket) error {
	if d.presentation != nil || len(payload) < 11 {
		return fmt.Errorf("missing display end or truncated presentation")
	}
	width, height := int(binary.BigEndian.Uint16(payload)), int(binary.BigEndian.Uint16(payload[2:]))
	if width == 0 || height == 0 || payload[7]&0x3f != 0 || payload[8]&0x7f != 0 || int(payload[10]) > pgsMaxReferences {
		return fmt.Errorf("invalid presentation fields")
	}
	if d.hasEvent && packet.PTS < d.lastPTS {
		return fmt.Errorf("presentation timestamps move backwards")
	}
	if d.limits.DurationTicks > 0 && packet.PTS > d.limits.DurationTicks {
		return fmt.Errorf("presentation starts after the indexed source end")
	}
	if payload[7] != 0 {
		d.resetEpoch()
	} else if d.width != 0 && (width != d.width || height != d.height) {
		return fmt.Errorf("presentation dimensions changed without an epoch boundary")
	}
	d.width, d.height = width, height
	presentation := &pgsPresentation{pts: packet.PTS, width: width, height: height, palette: payload[9]}
	if packet.Duration != 0 {
		presentation.fallbackEnd = packet.PTS + packet.Duration
		presentation.fallbackKnown = true
	}
	remaining := payload[11:]
	for range int(payload[10]) {
		if len(remaining) < 8 || remaining[3]&0x3f != 0 {
			return fmt.Errorf("invalid presentation object reference")
		}
		reference := pgsReference{
			id: binary.BigEndian.Uint16(remaining), window: remaining[2],
			x: int(binary.BigEndian.Uint16(remaining[4:])), y: int(binary.BigEndian.Uint16(remaining[6:])),
			forced: remaining[3]&0x40 != 0, cropped: remaining[3]&0x80 != 0,
		}
		remaining = remaining[8:]
		if reference.cropped {
			if len(remaining) < 8 {
				return fmt.Errorf("truncated object crop")
			}
			x, y := int(binary.BigEndian.Uint16(remaining)), int(binary.BigEndian.Uint16(remaining[2:]))
			w, h := int(binary.BigEndian.Uint16(remaining[4:])), int(binary.BigEndian.Uint16(remaining[6:]))
			if w == 0 || h == 0 {
				return fmt.Errorf("empty object crop")
			}
			reference.crop = image.Rect(x, y, x+w, y+h)
			remaining = remaining[8:]
		}
		presentation.objects = append(presentation.objects, reference)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("trailing presentation data")
	}
	d.presentation = presentation
	return nil
}

func (d *pgsDecoder) parsePalette(payload []byte) error {
	if len(payload) < 2 || (len(payload)-2)%5 != 0 {
		return fmt.Errorf("truncated palette entries")
	}
	palette := d.palettes[payload[0]]
	if palette == nil {
		if len(d.palettes) >= pgsMaxPalettes {
			return fmt.Errorf("too many palettes in one epoch")
		}
		palette = &pgsPalette{}
		// An omitted zero entry is transparent; other omitted entries are
		// rejected if referenced, rather than fabricating visible colors.
		palette.defined[0] = true
		d.palettes[payload[0]] = palette
	}
	for entry := payload[2:]; len(entry) != 0; entry = entry[5:] {
		y, cr, cb := int(entry[1])-16, int(entry[2])-128, int(entry[3])-128
		// Limited-range BT.601/BT.709 coefficients use ten fractional
		// bits, including the 219-level luma and 224-level chroma ranges.
		r, g, b := 1192*y+1634*cr, 1192*y-401*cb-832*cr, 1192*y+2066*cb
		if d.height > 576 {
			r, g, b = 1192*y+1836*cr, 1192*y-218*cb-546*cr, 1192*y+2163*cb
		}
		palette.colors[entry[0]] = color.NRGBA{R: pgsColorByte(r), G: pgsColorByte(g), B: pgsColorByte(b), A: entry[4]}
		palette.defined[entry[0]] = true
	}
	return nil
}

func pgsColorByte(value int) uint8 {
	return uint8(min(255, max(0, (value+512)>>10)))
}

func (d *pgsDecoder) parseObject(payload []byte) error {
	if len(payload) < 4 || payload[3]&0x3f != 0 {
		return fmt.Errorf("invalid object sequence header")
	}
	id, version, flags := binary.BigEndian.Uint16(payload), payload[2], payload[3]
	object := d.objects[id]
	fragment := payload[4:]
	if flags&0x80 != 0 {
		if len(fragment) < 7 {
			return fmt.Errorf("truncated initial object fragment")
		}
		length := int(fragment[0])<<16 | int(fragment[1])<<8 | int(fragment[2])
		width, height := int(binary.BigEndian.Uint16(fragment[3:])), int(binary.BigEndian.Uint16(fragment[5:]))
		if length <= 4 || length-4 > d.limits.MaxPacketBytes || width == 0 || height == 0 ||
			width > d.width || height > d.height || width > d.limits.MaxPixels/height {
			return fmt.Errorf("object dimensions or encoded size exceed the decoder budget")
		}
		oldPixels, oldBytes := 0, 0
		if object != nil {
			if !object.complete {
				return fmt.Errorf("unfinished object replaced by a new sequence")
			}
			oldPixels, oldBytes = object.width*object.height, object.expected
		} else if len(d.objects) >= pgsMaxObjects {
			return fmt.Errorf("too many objects in one epoch")
		}
		pixels, encoded := width*height, length-4
		if pixels > d.limits.MaxTotalPixels-(d.objectPixels-oldPixels)-d.pendingPixels || encoded > d.limits.MaxPacketBytes-(d.objectBytes-oldBytes) {
			return fmt.Errorf("cached objects exceed the decoder budget")
		}
		d.objectPixels += pixels - oldPixels
		d.objectBytes += encoded - oldBytes
		object = &pgsObject{version: version, width: width, height: height, expected: encoded, rle: make([]byte, 0, encoded)}
		d.objects[id] = object
		fragment = fragment[7:]
	} else if object == nil || object.complete || object.version != version {
		return fmt.Errorf("object continuation has no matching initial fragment")
	}
	if len(fragment) == 0 || len(fragment) > object.expected-len(object.rle) {
		return fmt.Errorf("object fragment exceeds its declared length")
	}
	object.rle = append(object.rle, fragment...)
	if flags&0x40 != 0 {
		if len(object.rle) != object.expected {
			return fmt.Errorf("final object fragment is incomplete")
		}
		object.complete = true
	} else if len(object.rle) == object.expected {
		return fmt.Errorf("complete object is missing its final fragment flag")
	}
	return nil
}

func (d *pgsDecoder) parseWindows(payload []byte) error {
	if len(payload) == 0 || payload[0] > pgsMaxReferences || len(payload) != 1+int(payload[0])*9 {
		return fmt.Errorf("invalid window segment")
	}
	windows := make(map[byte]image.Rectangle)
	for entry := payload[1:]; len(entry) != 0; entry = entry[9:] {
		x, y := int(binary.BigEndian.Uint16(entry[1:])), int(binary.BigEndian.Uint16(entry[3:]))
		width, height := int(binary.BigEndian.Uint16(entry[5:])), int(binary.BigEndian.Uint16(entry[7:]))
		if _, duplicate := windows[entry[0]]; duplicate || width == 0 || height == 0 || x+width > d.width || y+height > d.height {
			return fmt.Errorf("invalid or duplicate display window")
		}
		windows[entry[0]] = image.Rect(x, y, x+width, y+height)
	}
	d.windows = windows
	return nil
}

func (d *pgsDecoder) display(packet bitmapSubtitlePacket) error {
	presentation := d.presentation
	for _, object := range d.objects {
		if !object.complete {
			return fmt.Errorf("display set contains an incomplete object")
		}
	}
	if err := d.closePending(presentation.pts); err != nil {
		return err
	}
	d.lastPTS, d.hasEvent = presentation.pts, true
	d.fallbackEnd = presentation.fallbackEnd
	d.fallbackKnown = presentation.fallbackKnown
	if !d.fallbackKnown && packet.Duration > 0 && packet.PTS+packet.Duration > presentation.pts {
		d.fallbackEnd = packet.PTS + packet.Duration
		d.fallbackKnown = true
	}
	if len(presentation.objects) != 0 {
		palette := d.palettes[presentation.palette]
		if palette == nil {
			return fmt.Errorf("presentation references an undefined palette")
		}
		// Keep forced and ordinary text separate when they share a display
		// set, so the review cannot incorrectly promote optional text.
		for _, forced := range []bool{false, true} {
			cue, err := d.render(presentation, palette, forced)
			if err != nil {
				return err
			}
			if cue != nil {
				d.pending = append(d.pending, *cue)
				d.pendingPixels += cue.Image.Bounds().Dx() * cue.Image.Bounds().Dy()
			}
		}
	}
	d.presentation = nil
	return nil
}

func (d *pgsDecoder) closePending(end int64) error {
	if len(d.pending) == 0 {
		return nil
	}
	if end < d.lastPTS {
		return fmt.Errorf("display interval ends before it starts")
	}
	defer func() {
		clear(d.pending)
		d.pending = nil
		d.pendingPixels = 0
	}()
	if end > d.lastPTS {
		for index := range d.pending {
			if err := d.ctx.Err(); err != nil {
				return err
			}
			if d.emittedCues >= d.limits.MaxCues {
				return fmt.Errorf("too many visible subtitle intervals")
			}
			cue := d.pending[index]
			d.pending[index] = BitmapSubtitleCue{}
			cue.EndTicks = end
			if err := d.emit(cue); err != nil {
				return err
			}
			d.emittedCues++
			if err := d.ctx.Err(); err != nil {
				return err
			}
		}
	}
	// Multiple complete display sets at one timestamp replace one another;
	// the intermediate state had no visible interval and is not OCR input.
	return nil
}

func (d *pgsDecoder) chargeWork(pixels int) error {
	if pixels <= 0 || int64(pixels) > d.limits.MaxWorkPixels-d.workPixels {
		return fmt.Errorf("bitmap decoding exceeds the work pixel budget")
	}
	d.workPixels += int64(pixels)
	return nil
}

type pgsLayer struct {
	object *pgsObject
	source image.Point
	dest   image.Rectangle
}

func (d *pgsDecoder) render(presentation *pgsPresentation, palette *pgsPalette, forced bool) (*BitmapSubtitleCue, error) {
	var layers []pgsLayer
	var bounds image.Rectangle
	for _, reference := range presentation.objects {
		if reference.forced != forced {
			continue
		}
		object, window := d.objects[reference.id], d.windows[reference.window]
		if object == nil || window.Empty() {
			return nil, fmt.Errorf("presentation references an undefined object or window")
		}
		crop := image.Rect(0, 0, object.width, object.height)
		if reference.cropped {
			if !reference.crop.In(crop) {
				return nil, fmt.Errorf("object crop exceeds the bitmap")
			}
			crop = reference.crop
		}
		dest := image.Rect(reference.x, reference.y, reference.x+crop.Dx(), reference.y+crop.Dy())
		visible := dest.Intersect(window).Intersect(image.Rect(0, 0, presentation.width, presentation.height))
		if visible.Empty() {
			continue
		}
		if object.indices == nil {
			if err := d.chargeWork(object.width * object.height); err != nil {
				return nil, err
			}
			indices, err := decodePGSRLE(d.ctx, object.rle, object.width, object.height)
			if err != nil {
				return nil, err
			}
			object.indices = indices
		}
		layers = append(layers, pgsLayer{object: object, source: crop.Min.Add(visible.Min.Sub(dest.Min)), dest: visible})
		bounds = bounds.Union(visible)
	}
	if bounds.Empty() {
		return nil, nil
	}
	if bounds.Dx() > d.limits.MaxPixels/bounds.Dy() || bounds.Dx() > (d.limits.MaxTotalPixels-d.objectPixels-d.pendingPixels)/bounds.Dy() || bounds.Dx() > int(^uint(0)>>1)/4/bounds.Dy() {
		return nil, fmt.Errorf("composed bitmap exceeds the decoder pixel budget")
	}
	if err := d.chargeWork(bounds.Dx() * bounds.Dy()); err != nil {
		return nil, err
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for _, layer := range layers {
		for y := layer.dest.Min.Y; y < layer.dest.Max.Y; y++ {
			if err := d.ctx.Err(); err != nil {
				return nil, err
			}
			row := (layer.source.Y+y-layer.dest.Min.Y)*layer.object.width + layer.source.X
			for x := layer.dest.Min.X; x < layer.dest.Max.X; x++ {
				index := layer.object.indices[row+x-layer.dest.Min.X]
				if !palette.defined[index] {
					return nil, fmt.Errorf("bitmap references an undefined palette entry")
				}
				pixel := palette.colors[index]
				if pixel.A == 0 {
					continue
				}
				point := image.Pt(x-bounds.Min.X, y-bounds.Min.Y)
				if pixel.A == 255 || canvas.NRGBAAt(point.X, point.Y).A == 0 {
					canvas.SetNRGBA(point.X, point.Y, pixel)
				} else {
					canvas.SetNRGBA(point.X, point.Y, pgsComposite(pixel, canvas.NRGBAAt(point.X, point.Y)))
				}
			}
		}
	}
	visible := false
	for offset := 3; offset < len(canvas.Pix); offset += 4 {
		if canvas.Pix[offset] != 0 {
			visible = true
			break
		}
	}
	if !visible {
		return nil, nil
	}
	return &BitmapSubtitleCue{StartTicks: presentation.pts, Image: canvas, X: bounds.Min.X, Y: bounds.Min.Y, Forced: forced}, nil
}

func pgsComposite(source, destination color.NRGBA) color.NRGBA {
	sourceAlpha, destinationAlpha := int(source.A), int(destination.A)*(255-int(source.A))
	alpha := sourceAlpha*255 + destinationAlpha
	channel := func(front, back uint8) uint8 {
		return uint8((int(front)*sourceAlpha*255 + int(back)*destinationAlpha + alpha/2) / alpha)
	}
	return color.NRGBA{R: channel(source.R, destination.R), G: channel(source.G, destination.G),
		B: channel(source.B, destination.B), A: uint8((alpha + 127) / 255)}
}

func decodePGSRLE(ctx context.Context, encoded []byte, width, height int) ([]byte, error) {
	indices := make([]byte, width*height)
	x, y := 0, 0
	for offset := 0; offset < len(encoded); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if y >= height {
			return nil, fmt.Errorf("trailing object RLE data")
		}
		index, run := encoded[offset], 1
		offset++
		if index == 0 {
			if offset >= len(encoded) {
				return nil, fmt.Errorf("truncated object RLE control")
			}
			flags := encoded[offset]
			offset++
			if flags == 0 {
				if x != width {
					return nil, fmt.Errorf("object RLE row has the wrong width")
				}
				x, y = 0, y+1
				continue
			}
			run = int(flags & 0x3f)
			if flags&0x40 != 0 {
				if offset >= len(encoded) {
					return nil, fmt.Errorf("truncated long object RLE run")
				}
				run = run<<8 | int(encoded[offset])
				offset++
			}
			if flags&0x80 != 0 {
				if offset >= len(encoded) {
					return nil, fmt.Errorf("truncated object RLE color")
				}
				index = encoded[offset]
				offset++
			}
		}
		if run == 0 || run > width-x {
			return nil, fmt.Errorf("object RLE run exceeds its row")
		}
		start := y*width + x
		for position := start; position < start+run; position++ {
			indices[position] = index
		}
		x += run
	}
	if y != height || x != 0 {
		return nil, fmt.Errorf("incomplete object RLE bitmap")
	}
	return indices, nil
}
