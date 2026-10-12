package vision

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// canvas draws a recognisable pattern: a grid of coloured squares, so a template cut from it contains
// structure rather than a flat area.
func canvas(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := uint8((x * 7) % 256)
			g := uint8((y * 11) % 256)
			b := uint8((x*y + 40) % 256)
			if x%13 < 6 && y%9 < 5 {
				r, g, b = 230, 40, 60
			}
			img.Set(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
	return img
}

func cut(img image.Image, x, y, w, h int) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			out.Set(i, j, img.At(x+i, y+j))
		}
	}
	return out
}

func TestFindLocatesTheTemplate(t *testing.T) {
	img := canvas(320, 240)
	template := cut(img, 97, 53, 34, 28)

	got, err := Find(img, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score < 0.999 {
		t.Errorf("a template taken from the image scored %v", got.Score)
	}
	if got.X != 97 || got.Y != 53 {
		t.Errorf("found at (%d,%d), want (97,53)", got.X, got.Y)
	}
	if got.Delta > 0.001 {
		t.Errorf("delta = %v for an exact match", got.Delta)
	}
}

// The pyramid is an optimisation, so it has to give the same answer as looking everywhere. When it does
// not, every visual assertion in the suite becomes a different assertion.
func TestPyramidAgreesWithBruteForce(t *testing.T) {
	img := canvas(400, 300)
	for _, at := range [][2]int{{0, 0}, {7, 11}, {200, 150}, {365, 271}} {
		template := cut(img, at[0], at[1], 33, 27)
		got, err := Find(img, template, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.X != at[0] || got.Y != at[1] {
			t.Errorf("pyramid found a patch from (%d,%d) at (%d,%d)", at[0], at[1], got.X, got.Y)
		}
	}
}

// Correlation is invariant to brightness. Dimming a whole page by four still correlates at 0.9999, so a
// check that looked only at the score would call a page nobody can read a pass. The mean difference is
// the second number, and it is the one that moves.
func TestADimmedPageKeepsItsScoreAndLosesItsDelta(t *testing.T) {
	img := canvas(300, 200)
	template := cut(img, 120, 80, 40, 40)

	same, err := Find(img, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	dimmed, err := Find(ColorChange(img, 0.25), template, nil)
	if err != nil {
		t.Fatal(err)
	}
	if dimmed.Score < 0.99 {
		t.Errorf("dimming changed the correlation to %v; the premise of this test is wrong", dimmed.Score)
	}
	if dimmed.Delta < 20 {
		t.Errorf("delta = %v after dimming by four times", dimmed.Delta)
	}
	if dimmed.Passes(0.85, 12) {
		t.Error("a page dimmed by four times passed a check with a delta bound")
	}
	if !same.Passes(0.85, 12) {
		t.Errorf("an unchanged page failed: score %v delta %v", same.Score, same.Delta)
	}
}

// A colour that changes without changing the shape is what a screenshot test exists for, and it is what
// a luminance-only comparison cannot see: same layout, same brightness, different meaning.
func TestAColourSwapIsCaughtWhileLuminanceIsBlind(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: 250, G: 250, B: 250, A: 255})
		}
	}
	// A badge, drawn twice: once green and once red, with the same value in one channel each time so
	// that the two have the same luminance.
	// Draws into the image it is given. The first version wrote through a captured variable, so the
	// "regression" was applied to the original and the copy kept the badge that never changed - and the
	// test failed in the direction of a perfect match with zero difference.
	drawBadge := func(dst *image.RGBA, x0, y0 int, c color.RGBA) {
		for y := 0; y < 30; y++ {
			for x := 0; x < 30; x++ {
				if x < 4 || y < 4 || x > 25 || y > 25 {
					dst.Set(x0+x, y0+y, c)
				}
			}
		}
	}
	green := color.RGBA{R: 30, G: 190, B: 30, A: 255}
	red := color.RGBA{R: 190, G: 30, B: 30, A: 255}
	drawBadge(img, 20, 20, green)
	drawBadge(img, 120, 60, red)

	template := cut(img, 120, 60, 30, 30) // the red badge, as it was before the regression

	after, err := Find(img, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The red badge is still there, so the exact match wins - and it should.
	if after.Score < 0.99 {
		t.Errorf("the badge that never changed scored %v", after.Score)
	}

	// Now the regression: the red badge turns green. Its shape and its luminance do not move.
	swapped := image.NewRGBA(img.Bounds())
	for y := 0; y < 120; y++ {
		for x := 0; x < 200; x++ {
			swapped.Set(x, y, img.At(x, y))
		}
	}
	drawBadge(swapped, 120, 60, green)

	regressed, err := Find(swapped, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	if regressed.Passes(0.9, 30) {
		t.Errorf("a badge that turned green passed: score %v delta %v", regressed.Score, regressed.Delta)
	}
	luma, err := FindLuminance(swapped, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	if luma.Score < 0.95 {
		t.Errorf("the luminance-only path scored %v, so it would have caught this too", luma.Score)
	}
}

// FindLuminance is the naive implementation: one channel, the thing a first draft does. It exists so
// the test above can prove the colour planes are doing work.
func FindLuminance(img image.Image, template image.Image, region *Rect) (Result, error) {
	grey := toGrey(img)
	greyTemplate := toGrey(template)
	area := Rect{0, 0, grey.W, grey.H}
	if region != nil {
		area = *region
		grey = cropGrey(grey, area)
		greyTemplate = cropGrey(greyTemplate, Rect{0, 0, greyTemplate.W, greyTemplate.H})
	}
	best := Result{Score: math.Inf(-1)}
	for y := 0; y+greyTemplate.H <= grey.H; y++ {
		for x := 0; x+greyTemplate.W <= grey.W; x++ {
			r := scoreGrey(grey, greyTemplate, x, y)
			if r.Score > best.Score {
				best = r
			}
		}
	}
	return best, nil
}

type greySet struct {
	W, H int
	pix  []float64
}

func toGrey(img image.Image) greySet {
	b := img.Bounds()
	out := greySet{W: b.Dx(), H: b.Dy(), pix: make([]float64, b.Dx()*b.Dy())}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out.pix[y*out.W+x] = 0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(bl>>8)
		}
	}
	return out
}

func cropGrey(in greySet, r Rect) greySet {
	out := greySet{W: r.W, H: r.H, pix: make([]float64, r.W*r.H)}
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			out.pix[y*out.W+x] = in.pix[(r.Y+y)*in.W+r.X+x]
		}
	}
	return out
}

func scoreGrey(hay, needle greySet, x, y int) Result {
	n := float64(needle.W * needle.H)
	var sumHay, sumNeedle, sumHaySq, sumNeedleSq, sumProduct float64
	for ty := 0; ty < needle.H; ty++ {
		for tx := 0; tx < needle.W; tx++ {
			h := hay.pix[(y+ty)*hay.W+(x+tx)]
			t := needle.pix[ty*needle.W+tx]
			sumHay += h
			sumNeedle += t
			sumHaySq += h * h
			sumNeedleSq += t * t
			sumProduct += h * t
		}
	}
	cov := sumProduct - sumHay*sumNeedle/n
	vh := sumHaySq - sumHay*sumHay/n
	vn := sumNeedleSq - sumNeedle*sumNeedle/n
	d := math.Sqrt(vh * vn)
	if d < 1e-9 {
		return Result{Score: 0, X: x, Y: y}
	}
	return Result{Score: cov / d, X: x, Y: y}
}

// A region is how a scenario says "the badge in the panel", not "anywhere on the page". Without it, a
// page whose header repeats the same chip passes a check written about the panel below.
func TestRegionRestrictsTheSearch(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	// Two badges that differ. Drawn identically at first, which made this test agree with the code
	// while proving nothing: with two identical badges the first one is a perfect match.
	for at := range [][2]int{{10, 10}, {120, 120}} {
		_ = at
	}
	for _, at := range [][2]int{{10, 10}, {120, 120}} {
		seed := at[0] + at[1]
		for y := 0; y < 20; y++ {
			for x := 0; x < 20; x++ {
				shade := uint8(20 + ((x*9 + y*3 + seed) % 200))
				img.Set(at[0]+x, at[1]+y, color.RGBA{R: shade, G: uint8(255 - shade), B: 80, A: 255})
			}
		}
	}
	template := cut(img, 120, 120, 20, 20)

	whole, err := Find(img, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	if whole.X != 120 || whole.Y != 120 {
		t.Fatalf("whole page found (%d,%d)", whole.X, whole.Y)
	}

	header := Rect{X: 0, Y: 0, W: 60, H: 60}
	inHeader, err := Find(img, template, &header)
	if err != nil {
		t.Fatal(err)
	}
	if inHeader.X < 0 || inHeader.X > 40 || inHeader.Y < 0 || inHeader.Y > 40 {
		t.Errorf("the region was ignored: match at (%d,%d)", inHeader.X, inHeader.Y)
	}
	if inHeader.Score > whole.Score {
		t.Errorf("a worse match reported a better score (%v > %v)", inHeader.Score, whole.Score)
	}
}

func TestRegionOutsideTheImageIsAnError(t *testing.T) {
	img := canvas(100, 100)
	template := cut(img, 10, 10, 10, 10)
	bad := Rect{X: 90, Y: 90, W: 40, H: 40}
	if _, err := Find(img, template, &bad); err == nil {
		t.Fatal("a region outside the screenshot was accepted")
	}
}

func TestTemplateLargerThanTheImageIsAnError(t *testing.T) {
	img := canvas(50, 50)
	template := canvas(200, 200)
	if _, err := Find(img, template, nil); err == nil {
		t.Fatal("a template larger than the search area was accepted")
	}
}

// A flat template matches nothing. Reporting 1.0 for "this area is the same shade of grey" would be the
// most confidently wrong thing this package could do.
func TestFlatTemplateScoresZero(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 60, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}
	flat := cut(img, 5, 5, 20, 20)
	got, err := Find(img, flat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score != 0 {
		t.Errorf("a flat template scored %v", got.Score)
	}
}

// Honest limits, asserted: the match is neither scale nor rotation invariant, and a suite that assumes
// otherwise produces failures that look like flake.
func TestNotScaleOrRotationInvariant(t *testing.T) {
	img := canvas(300, 300)
	template := cut(img, 100, 100, 40, 40)

	bigger := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			bigger.Set(x, y, img.At(x*2%300, y*2%300))
		}
	}
	got, err := Find(bigger, template, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Score > 0.9 {
		t.Errorf("a rescrambled image matched at %v", got.Score)
	}
}

func TestHighlightAndCropProduceTheRightSize(t *testing.T) {
	img := canvas(120, 90)
	r := Result{Score: 1, X: 20, Y: 10}
	if got := DiffCrop(img, r, 30, 20).Bounds(); got.Dx() != 30 || got.Dy() != 20 {
		t.Errorf("crop is %v", got)
	}
	if got := Highlight(img, r, 30, 20, 2).Bounds(); got.Dx() != 120 || got.Dy() != 90 {
		t.Errorf("highlight changed the size: %v", got)
	}
}
