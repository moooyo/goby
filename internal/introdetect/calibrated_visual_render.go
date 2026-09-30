package introdetect

import "math"

// Geometry is expressed in permille of the normalized source raster. Positive
// shifts sample farther right or down; scaling is around the source center.
// This policy uses one transform for the entire prefix of each source.
type calibratedGeometry struct {
	ScaleYPermille int
	ShiftXPermille int
	ShiftYPermille int
}

const calibratedCropPermille = 800

func calibratedGeometryGrid() []calibratedGeometry {
	values := make([]calibratedGeometry, 0, 147)
	for _, scale := range []int{940, 1000, 1060} {
		for x := -30; x <= 30; x += 10 {
			for y := -30; y <= 30; y += 10 {
				values = append(values, calibratedGeometry{scale, x, y})
			}
		}
	}
	return values
}

func calibratedRender(sample RefinementSample, geometry calibratedGeometry) VisualSample {
	var raster [1024]float64
	var sum, squared float64
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			sx := (.5+(float64(x)+.5-16)/32*.8+float64(geometry.ShiftXPermille)/1000)*16 - .5
			sy := (.5+(float64(y)+.5-16)/32*.8*float64(geometry.ScaleYPermille)/1000+float64(geometry.ShiftYPermille)/1000)*16 - .5
			sx = math.Max(0, math.Min(15, sx))
			sy = math.Max(0, math.Min(15, sy))
			x0, y0 := int(sx), int(sy)
			x1, y1 := min(x0+1, 15), min(y0+1, 15)
			fx, fy := sx-float64(x0), sy-float64(y0)
			value := (1-fy)*((1-fx)*float64(sample.Raster[y0*16+x0])+fx*float64(sample.Raster[y0*16+x1])) + fy*((1-fx)*float64(sample.Raster[y1*16+x0])+fx*float64(sample.Raster[y1*16+x1]))
			raster[y*32+x] = value
			sum += value
			squared += value * value
		}
	}
	mean := sum / 1024
	contrast := math.Sqrt(math.Max(0, squared/1024-mean*mean)) * 1000 / 255
	result := VisualSample{Ticks: sample.Ticks, Contrast: uint16(math.Round(contrast))}
	var cells [64]float64
	sum, squared = 0, 0
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			var total float64
			for y := row * 4; y < (row+1)*4; y++ {
				for x := column * 4; x < (column+1)*4; x++ {
					total += raster[y*32+x]
				}
			}
			value := total / 16
			cells[row*8+column] = value
			sum += value
			squared += value * value
		}
	}
	mean = sum / 64
	variance := math.Max(0, squared/64-mean*mean)
	if variance > 0 {
		result.LumaKnown = true
		deviation := math.Sqrt(math.Max(1, variance))
		for i, value := range cells {
			result.Luma[i] = int8(math.Max(-127, math.Min(127, math.Round((value-mean)*32/deviation))))
		}
	}
	for row := 0; row < 8; row++ {
		var previous float64
		for column := 0; column < 9; column++ {
			left, right := column*32/9, (column+1)*32/9
			var total float64
			for y := row * 4; y < (row+1)*4; y++ {
				for x := left; x < right; x++ {
					total += raster[y*32+x]
				}
			}
			value := total / float64((right-left)*4)
			if column > 0 && previous > value {
				result.Hash |= uint64(1) << uint(row*8+column-1)
			}
			previous = value
		}
	}
	return result
}

func calibratedView(episode Episode, geometry calibratedGeometry, budget *workBudget) (Episode, error) {
	view := episode
	view.Visual = make([]VisualSample, 0, len(episode.Refinement))
	for _, sample := range episode.Refinement {
		if sample.Ticks >= sequenceLimit(episode) {
			break
		}
		// One rendering work unit covers 64 bilinear output pixels. This
		// charges all 1024 pixels, in addition to downstream comparisons.
		for block := 0; block < 16; block++ {
			if err := budget.spend(); err != nil {
				return Episode{}, err
			}
		}
		view.Visual = append(view.Visual, calibratedRender(sample, geometry))
	}
	return view, nil
}
