// Package vision answers the question a DOM check cannot: does this look right?
//
// A text assertion passes on a button whose label is correct and whose colour has turned the same
// shade as the background behind it. That is the bug this package exists for, so the default is a
// template match over colour, and the threshold is a number a person can defend.
package vision

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// Result is a match score plus where it was found.
type Result struct {
	// Score is normalised cross-correlation, per channel averaged: 1 is identical, 0 is unrelated,
	// and a uniformly bright template against a uniformly bright region scores 0 because correlation
	// says nothing about two flat signals.
	Score float64
	X, Y  int // top-left of the best match, in haystack coordinates
	// Delta is the mean absolute per-channel difference at that position, 0-255. A match at 0.98 with
	// a delta of 30 is a shape that happens to be there and a colour that is wrong, which is exactly
	// the case a screenshot diff should report rather than hide behind one number.
	Delta float64
	// Region is the area that was searched, so a report can draw it.
	Region Rect
}

// Rect is a region of interest, in image coordinates.
type Rect struct{ X, Y, W, H int }

func (r Rect) valid(bounds image.Rectangle) bool {
	return r.W > 0 && r.H > 0 && r.X >= 0 && r.Y >= 0 && r.X+r.W <= bounds.Dx() && r.Y+r.H <= bounds.Dy()
}

// Region clips a region against the image, returning the whole image when the region is unset.
func clip(region *Rect, bounds image.Rectangle) (Rect, error) {
	full := Rect{X: 0, Y: 0, W: bounds.Dx(), H: bounds.Dy()}
	if region == nil {
		return full, nil
	}
	if !region.valid(bounds) {
		return Rect{}, fmt.Errorf("region %+v is outside the %dx%d screenshot", *region, bounds.Dx(), bounds.Dy())
	}
	return *region, nil
}

// Find locates template inside img, optionally within a region.
//
// The search is coarse to fine: the same answer as looking at every position, found in a fraction of
// the work, and TestPyramidAgreesWithBruteForce holds that claim to account.
func Find(img image.Image, template image.Image, region *Rect) (Result, error) {
	area, err := clip(region, img.Bounds())
	if err != nil {
		return Result{}, err
	}
	hay := cropPlanes(img, area)
	needle := planesOf(template)
	if needle.W > hay.W || needle.H > hay.H {
		return Result{}, fmt.Errorf("template %dx%d does not fit in the %dx%d search area",
			needle.W, needle.H, hay.W, hay.H)
	}

	pyramid := buildPyramid(hay)
	templatePyramid := buildPyramid(needle)
	// Both pyramids have to reach the level being searched: the template's is shorter when the template
	// is small, and indexing past it is how this line came to exist.
	levels := min(len(pyramid), len(templatePyramid))

	best := searchPyramid(pyramid, templatePyramid, levels, startFrom(pyramid, templatePyramid, levels))

	// A score below the confidence bar means one of two things: there is no match, or the pyramid
	// landed on the wrong peak. The two are indistinguishable from here, so the search pays for one
	// more pass at half resolution with more candidates - four times cheaper than looking everywhere at
	// full resolution, and enough to find what the coarse levels missed on a textured page.
	if best.Score < confidenceBar {
		wider := searchPyramid(pyramid, templatePyramid, levels, min(1, levels-1))
		if wider.Score > best.Score {
			best = wider
		}
	}
	// Still unconvinced: look at every position, cheaply. A sampled sum of absolute differences cannot
	// produce the final score - it is a filter, not a metric - but an exact match gives it zero, so the
	// right position is always among the candidates it keeps.
	if best.Score < confidenceBar {
		if exact := exhaustive(hay, needle); exact.Score > best.Score {
			best = exact
		}
	}

	// Score the winner at full resolution, whatever proposed it.
	best = scoreAt(hay, needle, max(0, min(best.X, hay.W-needle.W)), max(0, min(best.Y, hay.H-needle.H)))
	best.Region = area
	return best, nil
}

// confidenceBar is the score above which a result is taken at face value. It is not the assertion
// threshold: a scenario can ask for 0.95 on a page where 0.92 is a correct match, and that is its
// business. This number decides how much work the search does, nothing else.
const confidenceBar = 0.9

// startFrom picks the coarsest level worth searching: one where the template is still wide enough to be
// distinctive. A 34x28 template at one eighth is four pixels across, and at four pixels a textured page
// has dozens of equally good peaks - which is the wrong answer this threshold exists to avoid.
func startFrom(pyramid, templatePyramid []planeSet, levels int) int {
	const smallestUseful = 8
	for level := levels - 1; level > 0; level-- {
		t := templatePyramid[level]
		if t.W >= smallestUseful && t.H >= smallestUseful &&
			t.W < pyramid[level].W && t.H < pyramid[level].H {
			return level
		}
	}
	return 0
}

// searchPyramid searches from a coarse level down to full resolution, refining several candidates.
func searchPyramid(pyramid, templatePyramid []planeSet, levels, start int) Result {
	const candidates = 4
	found := searchTopK(pyramid[start], templatePyramid[start], Rect{0, 0, pyramid[start].W, pyramid[start].H}, candidates)

	best := Result{Score: math.Inf(-1)}
	for _, candidate := range found {
		refined := candidate
		scale := 1 << start
		for level := min(start, levels) - 1; level >= 0; level-- {
			levelScale := 1 << level
			radius := (scale / levelScale) + 2
			window := Rect{
				X: max(0, refined.X*scale/levelScale-radius), Y: max(0, refined.Y*scale/levelScale-radius),
				W: 2*radius + 1, H: 2*radius + 1,
			}
			step := search(pyramid[level], templatePyramid[level], window)
			refined = Result{Score: step.Score, X: step.X * levelScale, Y: step.Y * levelScale}
			scale = levelScale
		}
		hay, needle := pyramid[0], templatePyramid[0]
		refined.X = max(0, min(refined.X, hay.W-needle.W))
		refined.Y = max(0, min(refined.Y, hay.H-needle.H))
		exact := scoreAt(hay, needle, refined.X, refined.Y)
		if exact.Score > best.Score {
			best = exact
		}
	}
	return best
}

// searchTopK returns the k best positions, kept apart from each other. Near-duplicates of one peak are
// not four candidates, they are one candidate counted four times.
func searchTopK(hay, needle planeSet, window Rect, k int) []Result {
	lastX := hay.W - needle.W
	lastY := hay.H - needle.H
	x0 := max(0, min(window.X, lastX))
	y0 := max(0, min(window.Y, lastY))
	x1 := min(lastX, window.X+window.W)
	y1 := min(lastY, window.Y+window.H)

	var best []Result
	separation := max(1, min(needle.W, needle.H)/2)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			r := scoreAt(hay, needle, x, y)
			if len(best) == k && r.Score <= best[len(best)-1].Score {
				continue
			}
			tooClose := false
			for _, b := range best {
				if abs(b.X-x) < separation && abs(b.Y-y) < separation {
					tooClose = true
					break
				}
			}
			if tooClose {
				continue
			}
			best = append(best, r)
			for i := len(best) - 1; i > 0 && best[i].Score > best[i-1].Score; i-- {
				best[i], best[i-1] = best[i-1], best[i]
			}
			if len(best) > k {
				best = best[:k]
			}
		}
	}
	if len(best) == 0 && lastX >= 0 && lastY >= 0 {
		best = append(best, scoreAt(hay, needle, x0, y0))
	}
	return best
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// search walks every position in window and keeps the best score. window is expected to be small at
// every level below the coarsest one, which is what makes the pyramid worth the code.
func search(hay, needle planeSet, window Rect) Result {
	best := Result{Score: math.Inf(-1)}
	lastX := hay.W - needle.W
	lastY := hay.H - needle.H
	x0 := max(0, min(window.X, lastX))
	y0 := max(0, min(window.Y, lastY))
	x1 := min(lastX, window.X+window.W)
	y1 := min(lastY, window.Y+window.H)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			r := scoreAt(hay, needle, x, y)
			if r.Score > best.Score {
				best = r
			}
		}
	}
	return best
}

// scoreAt is the correlation itself, over three colour planes. One plane would be cheaper and would
// miss a red button that stayed red-shaped and turned grey.
func scoreAt(hay, needle planeSet, x, y int) Result {
	n := float64(needle.W * needle.H)
	var total float64
	var delta float64
	for channel := 0; channel < 3; channel++ {
		var sumHay, sumNeedle, sumHaySq, sumNeedleSq, sumProduct, sumAbs float64
		for ty := 0; ty < needle.H; ty++ {
			for tx := 0; tx < needle.W; tx++ {
				h := hay.planes[channel][(y+ty)*hay.W+(x+tx)]
				t := needle.planes[channel][ty*needle.W+tx]
				sumHay += h
				sumNeedle += t
				sumHaySq += h * h
				sumNeedleSq += t * t
				sumProduct += h * t
				sumAbs += math.Abs(h - t)
			}
		}
		covariance := sumProduct - sumHay*sumNeedle/n
		varianceHay := sumHaySq - sumHay*sumHay/n
		varianceNeedle := sumNeedleSq - sumNeedle*sumNeedle/n
		denominator := math.Sqrt(varianceHay * varianceNeedle)
		if denominator > 1e-9 {
			total += covariance / denominator
		}
		delta += sumAbs
	}
	return Result{Score: total / 3, X: x, Y: y, Delta: delta / (3 * n)}
}

type planeSet struct {
	W, H   int
	planes [3][]float64
}

// cropPlanes extracts a region as three float planes.
func cropPlanes(img image.Image, area Rect) planeSet {
	out := planeSet{W: area.W, H: area.H}
	for i := range out.planes {
		out.planes[i] = make([]float64, area.W*area.H)
	}
	for y := 0; y < area.H; y++ {
		for x := 0; x < area.W; x++ {
			r, g, b, _ := img.At(area.X+x, area.Y+y).RGBA()
			i := y*area.W + x
			out.planes[0][i] = float64(r >> 8)
			out.planes[1][i] = float64(g >> 8)
			out.planes[2][i] = float64(b >> 8)
		}
	}
	return out
}

func planesOf(img image.Image) planeSet {
	return cropPlanes(img, Rect{0, 0, img.Bounds().Dx(), img.Bounds().Dy()})
}

// buildPyramid halves the image repeatedly. Level 0 is the original.
func buildPyramid(in planeSet) []planeSet {
	out := []planeSet{in}
	for {
		last := out[len(out)-1]
		if last.W < 16 || last.H < 16 {
			return out
		}
		out = append(out, halve(last))
	}
}

func halve(in planeSet) planeSet {
	out := planeSet{W: max(1, in.W/2), H: max(1, in.H/2)}
	for channel := range out.planes {
		out.planes[channel] = make([]float64, out.W*out.H)
	}
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			var sums [3]float64
			var count float64
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					sx, sy := 2*x+dx, 2*y+dy
					if sx >= in.W || sy >= in.H {
						continue
					}
					i := sy*in.W + sx
					for channel := range sums {
						sums[channel] += in.planes[channel][i]
					}
					count++
				}
			}
			if count == 0 {
				continue
			}
			i := y*out.W + x
			for channel := range sums {
				out.planes[channel][i] = sums[channel] / count
			}
		}
	}
	return out
}

// DiffCrop returns the part of the screenshot the match landed on, so a failed assertion can show what
// it actually saw instead of the whole page.
func DiffCrop(img image.Image, r Result, w, h int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(x, y, img.At(r.X+x, r.Y+y))
		}
	}
	return out
}

// Highlight draws a box around the reported match: a report that says "0.71" and shows nothing is a
// report nobody can act on.
func Highlight(img image.Image, r Result, w, h, thickness int) image.Image {
	bounds := img.Bounds()
	out := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	red := color.RGBA{R: 255, A: 255}
	for t := 0; t < thickness; t++ {
		for x := r.X; x < r.X+w && x < bounds.Dx(); x++ {
			for _, y := range []int{r.Y + t, r.Y + h - 1 - t} {
				if y >= 0 && y < bounds.Dy() {
					out.Set(x, y, red)
				}
			}
		}
		for y := r.Y; y < r.Y+h && y < bounds.Dy(); y++ {
			for _, x := range []int{r.X + t, r.X + w - 1 - t} {
				if x >= 0 && x < bounds.Dx() {
					out.Set(x, y, red)
				}
			}
		}
	}
	return out
}

// ColorChange turns every pixel of a copy towards another hue, which is how the test suite creates the
// regression a text check is blind to.
func ColorChange(img image.Image, scale float64) image.Image {
	bounds := img.Bounds()
	out := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			rr := clamp8(float64(r>>8) * scale)
			gg := clamp8(float64(g>>8) * scale)
			bb := clamp8(float64(b>>8) * scale)
			out.Set(x, y, color.RGBA{R: rr, G: gg, B: bb, A: uint8(a >> 8)})
		}
	}
	return out
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// exhaustive searches every position. It exists because a pyramid can miss: on a page whose detail is
// concentrated at one scale, the coarse levels see a pattern that repeats and settle on the wrong peak.
// The cost is bounded by the filter below rather than by the image size.
func exhaustive(hay, needle planeSet) Result {
	lastX := hay.W - needle.W
	lastY := hay.H - needle.H
	if lastX < 0 || lastY < 0 {
		return Result{Score: math.Inf(-1)}
	}

	// The filter compares a grid of sample points, at most 64 of them, which is 25 times less work than
	// the full comparison and enough to rank an exact match first.
	stepX := max(1, needle.W/8)
	stepY := max(1, needle.H/8)
	var filter []float64
	for index := 0; index < 3; index++ {
		filter = append(filter, 1)
	}
	_ = filter

	type candidate struct {
		x, y  int
		score float64
	}
	const keep = 32
	var top []candidate
	for y := 0; y <= lastY; y++ {
		for x := 0; x <= lastX; x++ {
			var sum float64
			for ty := 0; ty < needle.H; ty += stepY {
				for tx := 0; tx < needle.W; tx += stepX {
					i := (y+ty)*hay.W + x + tx
					j := ty*needle.W + tx
					for channel := 0; channel < 3; channel++ {
						sum += math.Abs(hay.planes[channel][i] - needle.planes[channel][j])
					}
				}
			}
			if len(top) == keep && sum >= top[len(top)-1].score {
				continue
			}
			top = append(top, candidate{x: x, y: y, score: sum})
			for i := len(top) - 1; i > 0 && top[i].score < top[i-1].score; i-- {
				top[i], top[i-1] = top[i-1], top[i]
			}
			if len(top) > keep {
				top = top[:keep]
			}
		}
	}

	best := Result{Score: math.Inf(-1)}
	for _, c := range top {
		if r := scoreAt(hay, needle, c.x, c.y); r.Score > best.Score {
			best = r
		}
	}
	return best
}

// Passes is the rule a visual assertion applies. Two numbers, because one is not enough: correlation
// is invariant to brightness, so a page that has been dimmed to a quarter still correlates at 0.9999,
// and a red badge turned green can keep a high correlation while its mean difference triples. A check
// that reports only the first number calls both of those a pass.
func (r Result) Passes(threshold, maxDelta float64) bool {
	return r.Score >= threshold && r.Delta <= maxDelta
}
