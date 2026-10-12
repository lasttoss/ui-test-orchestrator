package runner_test

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/driver"
	"github.com/lasttoss/ui-test-orchestrator/internal/runner"
	"github.com/lasttoss/ui-test-orchestrator/internal/scenario"
)

// The same gradient fakeDriver.Screenshot draws in runner_test.go, cropped: a template cut out of
// the page it looks at matches, so a run can be made to pass without weakening anything.
func gradientCrop(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}
	path := filepath.Join(t.TempDir(), "crop.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

// visualNote is the text the report and the terminal both read a match score out of.
func visualNote(t *testing.T, summary runner.Summary) string {
	t.Helper()
	if len(summary.Outcomes) != 1 {
		t.Fatalf("expected one scenario, got %+v", summary.Outcomes)
	}
	return summary.Outcomes[0].ImageNote
}

// viewportFake embeds the package's fake driver and remembers what canvas it was given.
type viewportFake struct {
	fakeDriver
	width, height int
}

func (f *viewportFake) SetViewport(_ context.Context, w, h int) error {
	f.width, f.height = w, h
	return nil
}

// The canvas is not left to the browser. Attaching to a browser whose window is smaller than a
// scenario's region is the failure this default exists to prevent.
func TestEveryScenarioGetsTheSameCanvas(t *testing.T) {
	fake := &viewportFake{}
	template := gradientCrop(t, 60, 40)
	specs := []*scenario.Scenario{spec(t, "name: visual\nurl: /\nsteps:\n  - action: assert_image\n    template: "+filepath.Base(template)+"\n    threshold: 0.9\n")}

	if _, err := runner.Run(context.Background(), runner.Options{
		Scenarios: specs, BaseURL: "http://fake", TemplatesDir: filepath.Dir(template),
		NewDriver: func() driver.Driver { return fake },
		Attempts:  1, Parallel: 1, MaxDelta: 40, Timeout: 5 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	if fake.width != runner.DefaultViewport.Width || fake.height != runner.DefaultViewport.Height {
		t.Errorf("viewport = %dx%d, want the default %dx%d", fake.width, fake.height, runner.DefaultViewport.Width, runner.DefaultViewport.Height)
	}
}

// A scenario that declares its own viewport keeps it.
func TestAScenarioCanKeepItsOwnCanvas(t *testing.T) {
	fake := &viewportFake{}
	template := gradientCrop(t, 60, 40)
	specs := []*scenario.Scenario{spec(t, "name: visual\nurl: /\nviewport:\n  width: 640\n  height: 480\nsteps:\n  - action: assert_image\n    template: "+filepath.Base(template)+"\n    threshold: 0.9\n")}

	if _, err := runner.Run(context.Background(), runner.Options{
		Scenarios: specs, BaseURL: "http://fake", TemplatesDir: filepath.Dir(template),
		NewDriver: func() driver.Driver { return fake },
		Attempts:  1, Parallel: 1, MaxDelta: 40, Timeout: 5 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	if fake.width != 640 || fake.height != 480 {
		t.Errorf("viewport = %dx%d, want the scenario's 640x480", fake.width, fake.height)
	}
}

func visualRun(t *testing.T, threshold float64) runner.Summary {
	t.Helper()
	template := gradientCrop(t, 60, 40)
	specs := []*scenario.Scenario{spec(t, "name: visual\nurl: /\nsteps:\n  - action: assert_image\n    template: "+filepath.Base(template)+"\n    threshold: 0.90\n")}

	summary, err := runner.Run(context.Background(), runner.Options{
		Scenarios: specs, BaseURL: "http://fake", TemplatesDir: filepath.Dir(template),
		NewDriver:      func() driver.Driver { return &fakeDriver{} },
		Attempts:       1,
		Parallel:       1,
		MaxDelta:       40,
		Timeout:        5 * time.Second,
		ImageThreshold: threshold,
	})
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

// Without an override the scenario's own 0.90 decides.
//
// The score for a template that is present and pixel-identical is 0.667 here rather than 1.0, and
// that is worth knowing before touching any threshold: the matcher reports a correlation, not a
// percentage of matching pixels, so a template with little structure scores below one even when the
// mean colour difference is 0.0. A threshold is a statement about a particular template and a
// particular browser, which is why one comes from the scenario and not from a global default.
func TestTheScenarioThresholdIsUsedByDefault(t *testing.T) {
	summary := visualRun(t, 0)
	if summary.Failed != 1 {
		t.Fatalf("the scenario's own 0.90 should have failed this match: %+v", summary.Outcomes)
	}
	note := visualNote(t, summary)
	if strings.Contains(note, "run threshold") {
		t.Errorf("note claims an override that was not asked for: %q", note)
	}
	if !strings.Contains(note, "threshold 0.90") {
		t.Errorf("note does not show the threshold that was used: %q", note)
	}
}

// An override is visible where it matters: in the note, next to the threshold it replaced, so a
// report cannot show a relaxed number as if the scenario had passed on its own terms.
//
// 0.60 is the number the compose file uses, for the reason in its comment: the lab's browser is a
// different build from the one that captured the templates.
func TestAnOverrideIsNamedInTheNote(t *testing.T) {
	summary := visualRun(t, 0.60)
	if summary.Failed != 0 {
		t.Fatalf("the override did not let a 0.667 match through: %+v", summary.Outcomes)
	}
	note := visualNote(t, summary)
	if !strings.Contains(note, "run threshold 0.60 overrides the scenario's 0.90") {
		t.Errorf("note does not say whose threshold this was: %q", note)
	}
}
