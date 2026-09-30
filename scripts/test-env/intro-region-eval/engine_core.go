package main

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"sort"
)

const (
	frameSide           = 96
	frameBytes          = frameSide * frameSide
	patchSide           = 16
	patchCount          = 25
	maxPatchComparisons = 200_000_000
	maxClockLookups     = 100_000_000
	maxRenderPixels     = 360_000_000
	maxHypotheses       = 128
)

type geometry struct {
	ScaleX float64 `json:"scaleX"`
	ScaleY float64 `json:"scaleY"`
	ShiftX int     `json:"shiftX"`
	ShiftY int     `json:"shiftY"`
}

type descriptor struct {
	Bits     [2]uint64
	Reliable [2]uint64
	Usable   bool
}

type frame struct {
	PTS      float64
	Patches  [patchCount]descriptor
	Previous int
}

type view struct {
	ID       string
	Geometry geometry
	Frames   []frame
}

type budget struct {
	ctx                context.Context
	stage              string
	PatchComparisons   int64 `json:"patchComparisons"`
	ClockLookups       int64 `json:"clockLookups"`
	RenderPixels       int64 `json:"renderPixels"`
	RetainedHypotheses int   `json:"retainedHypotheses"`
}

func (b *budget) checkpoint() error {
	if b.ctx != nil {
		return b.ctx.Err()
	}
	return nil
}

func (b *budget) retain() error {
	if err := b.checkpoint(); err != nil {
		return err
	}
	if b.RetainedHypotheses >= maxHypotheses {
		return errors.New("cohort hypothesis budget exceeded")
	}
	b.RetainedHypotheses++
	return nil
}

func (b *budget) patch() error {
	if err := b.checkpoint(); err != nil {
		return err
	}
	b.PatchComparisons++
	if b.PatchComparisons > maxPatchComparisons {
		return errors.New("patch comparison budget exceeded")
	}
	return nil
}

func (b *budget) clock() error {
	if err := b.checkpoint(); err != nil {
		return err
	}
	b.ClockLookups++
	if b.ClockLookups > maxClockLookups {
		return errors.New("clock lookup budget exceeded")
	}
	return nil
}

type comparison struct{ A, B int }

var comparisons = makeComparisons()

func makeComparisons() [128]comparison {
	var result [128]comparison
	seed := uint32(0x474f4259)
	next := func() int {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return int(seed & 255)
	}
	seen := map[[2]int]bool{}
	for i := 0; i < len(result); {
		a, b := next(), next()
		if absInt(a%16-b%16)+absInt(a/16-b/16) < 4 {
			continue
		}
		key := [2]int{min(a, b), max(a, b)}
		if seen[key] {
			continue
		}
		seen[key] = true
		result[i] = comparison{a, b}
		i++
	}
	return result
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func describe(values [256]float64) descriptor {
	var sum, squared float64
	for _, value := range values {
		sum += value
		squared += value * value
	}
	if squared/256-(sum/256)*(sum/256) < 64 {
		return descriptor{}
	}
	d := descriptor{Usable: true}
	for i, pair := range comparisons {
		delta := values[pair.A] - values[pair.B]
		if math.Abs(delta) >= 3 {
			d.Reliable[i/64] |= 1 << uint(i%64)
		}
		if delta > 0 {
			d.Bits[i/64] |= 1 << uint(i%64)
		}
	}
	return d
}

func closePatch(a, b descriptor, work *budget) (bool, int, error) {
	if err := work.patch(); err != nil {
		return false, 0, err
	}
	if !a.Usable || !b.Usable {
		return false, 0, nil
	}
	active, different := 0, 0
	for i := 0; i < 2; i++ {
		mask := a.Reliable[i] & b.Reliable[i]
		active += bits.OnesCount64(mask)
		different += bits.OnesCount64((a.Bits[i] ^ b.Bits[i]) & mask)
	}
	return active >= 80 && different*5 <= active, different, nil
}

func synchronousPatch(a, ap, b, bp descriptor, work *budget) (bool, error) {
	previousClose, _, err := closePatch(ap, bp, work)
	if err != nil || !previousClose {
		return false, err
	}
	active, changeA, changeB, common := 0, 0, 0, 0
	for i := 0; i < 2; i++ {
		mask := a.Reliable[i] & ap.Reliable[i] & b.Reliable[i] & bp.Reliable[i]
		x := (a.Bits[i] ^ ap.Bits[i]) & mask
		y := (b.Bits[i] ^ bp.Bits[i]) & mask
		active += bits.OnesCount64(mask)
		changeA += bits.OnesCount64(x)
		changeB += bits.OnesCount64(y)
		common += bits.OnesCount64(x & y & ^(a.Bits[i] ^ b.Bits[i]) & ^(ap.Bits[i] ^ bp.Bits[i]))
	}
	return active >= 80 && changeA >= 12 && changeB >= 12 && common*2 >= min(changeA, changeB), nil
}

// Group motion uses the common evidence of all current and previous source
// descriptors. Pairwise reliable masks cannot be combined as a substitute.
func synchronousGroup(current, previous [3]descriptor, work *budget) (bool, error) {
	if err := work.patch(); err != nil {
		return false, err
	}
	active, common := 0, 0
	var changes [3]int
	for source := range current {
		if !current[source].Usable || !previous[source].Usable {
			return false, nil
		}
	}
	for word := 0; word < 2; word++ {
		mask := ^uint64(0)
		for source := range current {
			mask &= current[source].Reliable[word] & previous[source].Reliable[word]
		}
		active += bits.OnesCount64(mask)
		shared := mask
		for source := range current {
			changed := (current[source].Bits[word] ^ previous[source].Bits[word]) & mask
			changes[source] += bits.OnesCount64(changed)
			shared &= changed
			shared &= ^(current[0].Bits[word] ^ current[source].Bits[word])
			shared &= ^(previous[0].Bits[word] ^ previous[source].Bits[word])
		}
		common += bits.OnesCount64(shared)
	}
	minimum := min(changes[0], min(changes[1], changes[2]))
	return active >= 80 && minimum >= 12 && common*2 >= minimum, nil
}

func neutral() geometry { return geometry{ScaleX: 1, ScaleY: 1} }

func geometryGrid() []geometry {
	result := []geometry{}
	for _, sx := range []float64{.9, 1, 1.1} {
		for _, sy := range []float64{.9, 1, 1.1} {
			for _, dx := range []int{-3, 0, 3} {
				for _, dy := range []int{-3, 0, 3} {
					result = append(result, geometry{sx, sy, dx, dy})
				}
			}
		}
	}
	return result
}

func render(raw []byte, pts float64, g geometry, work *budget) (frame, error) {
	if err := work.checkpoint(); err != nil {
		return frame{}, err
	}
	work.RenderPixels += 80 * 80
	if work.RenderPixels > maxRenderPixels {
		return frame{}, errors.New("render pixel budget exceeded")
	}
	var blurred [frameBytes]float64
	for y := 1; y < frameSide-1; y++ {
		for x := 1; x < frameSide-1; x++ {
			sum := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					sum += int(raw[(y+dy)*frameSide+x+dx])
				}
			}
			blurred[y*frameSide+x] = float64(sum) / 9
		}
	}
	outOfBounds := false
	sample := func(x, y float64) float64 {
		x -= .5
		y -= .5
		x0, y0 := int(math.Floor(x)), int(math.Floor(y))
		rx, ry := x-float64(x0), y-float64(y0)
		x1, y1 := x0+1, y0+1
		if x0 < 1 || x1 > 94 || y0 < 1 || y1 > 94 {
			outOfBounds = true
			return 0
		}
		return (1-ry)*((1-rx)*blurred[y0*96+x0]+rx*blurred[y0*96+x1]) + ry*((1-rx)*blurred[y1*96+x0]+rx*blurred[y1*96+x1])
	}
	f := frame{PTS: pts, Previous: -1}
	for p := 0; p < patchCount; p++ {
		var values [256]float64
		for y := 0; y < patchSide; y++ {
			for x := 0; x < patchSide; x++ {
				px, py := float64((p%5)*16+x)+.5, float64((p/5)*16+y)+.5
				sx := 48 + (px/80-.5)*96*.8*g.ScaleX + float64(g.ShiftX)
				sy := 48 + (py/80-.5)*96*.8*g.ScaleY + float64(g.ShiftY)
				values[y*16+x] = sample(sx, sy)
			}
		}
		f.Patches[p] = describe(values)
	}
	if outOfBounds {
		return frame{}, errors.New("geometry exceeds the complete blur and interpolation neighborhood")
	}
	return f, nil
}

func nearest(pts []float64, target, tolerance float64) int {
	i := sort.SearchFloat64s(pts, target)
	if i >= len(pts) {
		i = len(pts) - 1
	}
	if i > 0 && math.Abs(pts[i-1]-target) <= math.Abs(pts[i]-target) {
		i--
	}
	if i < 0 || math.Abs(pts[i]-target) > tolerance {
		return -1
	}
	return i
}

func makeView(s source, g geometry, indices []int, work *budget) (view, error) {
	v := view{ID: s.Info.ID, Geometry: g}
	if indices == nil {
		indices = make([]int, len(s.PTS))
		for i := range indices {
			indices[i] = i
		}
	}
	for _, index := range indices {
		f, err := render(s.Raw[index*frameBytes:(index+1)*frameBytes], s.PTS[index], g, work)
		if err != nil {
			return view{}, err
		}
		v.Frames = append(v.Frames, f)
	}
	pts := make([]float64, len(v.Frames))
	for i, f := range v.Frames {
		pts[i] = f.PTS
	}
	for i := range v.Frames {
		v.Frames[i].Previous = nearest(pts, pts[i]-.5, .060001)
	}
	return v, nil
}

func spatial(mask uint32) bool {
	if bits.OnesCount32(mask) < 8 {
		return false
	}
	rows, cols, center := uint32(0), uint32(0), 0
	for p := 0; p < patchCount; p++ {
		if mask&(1<<uint(p)) != 0 {
			rows |= 1 << uint(p/5)
			cols |= 1 << uint(p%5)
			if p/5 >= 1 && p/5 <= 3 && p%5 >= 1 && p%5 <= 3 {
				center++
			}
		}
	}
	return bits.OnesCount32(rows) >= 3 && bits.OnesCount32(cols) >= 3 && center >= 3
}

type masks struct {
	Appearance uint32 `json:"appearanceMask"`
	Dynamic    uint32 `json:"dynamicMask"`
	Distance   int    `json:"distance"`
}

func compareFrames(a view, ai int, b view, bi int, work *budget) (masks, error) {
	x, y := a.Frames[ai], b.Frames[bi]
	result := masks{}
	for p := 0; p < patchCount; p++ {
		match, distance, err := closePatch(x.Patches[p], y.Patches[p], work)
		if err != nil {
			return masks{}, err
		}
		if !match {
			continue
		}
		result.Appearance |= 1 << uint(p)
		result.Distance += distance
		if x.Previous >= 0 && y.Previous >= 0 {
			moving, err := synchronousPatch(x.Patches[p], a.Frames[x.Previous].Patches[p], y.Patches[p], b.Frames[y.Previous].Patches[p], work)
			if err != nil {
				return masks{}, err
			}
			if moving {
				result.Dynamic |= 1 << uint(p)
			}
		}
	}
	return result, nil
}
