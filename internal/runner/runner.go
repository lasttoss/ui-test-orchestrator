// Package runner executes scenarios. It owns retries, parallelism and artifacts, and it decides nothing
// about what a scenario means.
package runner

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/driver"
	"github.com/lasttoss/ui-test-orchestrator/internal/scenario"
	"github.com/lasttoss/ui-test-orchestrator/internal/vision"
)

// Options are a whole execution of the suite.
type Options struct {
	Scenarios    []*scenario.Scenario
	BaseURL      string
	TemplatesDir string
	// NewDriver is a factory, not a driver. Every attempt gets a fresh one: retrying on the page left
	// behind by the failure re-runs the test in the state that broke it, which is not a retry.
	NewDriver   func() driver.Driver
	Attempts    int
	Parallel    int
	MaxDelta    float64
	ArtifactDir string
	Timeout     time.Duration
	// ImageThreshold, when set, replaces the template-match threshold every visual assertion
	// declares. It exists for one reason: a visual assertion is calibrated against the browser
	// build that captured the template, and a lab that runs the same suite against another browser
	// (the compose file attaches to chromedp's headless shell) renders the same page a little
	// differently - measured here, the same badge matched at 0.73 instead of 1.00. Lowering the
	// threshold is a deliberate, visible act: every run that sets it says so in the output and in
	// every failure note, so an override cannot be mistaken for a strict pass.
	ImageThreshold float64
}

// StepResult records one step, including the ones that passed: a report that lists only failures cannot
// explain a flake that passed on the second attempt.
type StepResult struct {
	Action     scenario.Action `json:"action"`
	Passed     bool            `json:"passed"`
	Error      string          `json:"error,omitempty"`
	DurationMS float64         `json:"duration_ms"`
}

// Outcome is one scenario's result, across all its attempts.
type Outcome struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Tags       []string     `json:"tags,omitempty"`
	Passed     bool         `json:"passed"`
	Attempts   int          `json:"attempts"`
	Flaked     bool         `json:"flaked"`
	DurationMS float64      `json:"duration_ms"`
	Error      string       `json:"error,omitempty"`
	FailedStep int          `json:"failed_step,omitempty"`
	Steps      []StepResult `json:"steps,omitempty"`
	// Artifacts are files written under ArtifactDir for a failure: the screenshot the assertion saw.
	Artifacts []string `json:"artifacts,omitempty"`
	// Embeds are the same images, small, for the report to inline. A self-contained report that links
	// to files beside it is not self-contained.
	Embeds []string `json:"-"`
	// ImageNote explains what a failing visual assertion actually saw, in numbers.
	ImageNote string `json:"image_note,omitempty"`
}

// Summary is the whole run.
// DefaultViewport is the canvas every scenario runs on unless it declares its own. It is a size, not
// a window: SetViewport uses the DevTools emulation override, so the browser's window does not
// matter.
var DefaultViewport = struct {
	Width  int
	Height int
}{Width: 1280, Height: 900}

type Summary struct {
	Outcomes    []Outcome `json:"outcomes"`
	WallClockMS float64   `json:"wall_clock_ms"`
	Passed      int       `json:"passed"`
	Failed      int       `json:"failed"`
	Flaked      int       `json:"flaked"`
	Parallel    int       `json:"parallel"`
}

func (s *Summary) Outcome(id string) (Outcome, bool) {
	for _, o := range s.Outcomes {
		if o.ID == id {
			return o, true
		}
	}
	return Outcome{}, false
}

// Run executes every scenario and returns what happened, whether or not any of them failed.
func Run(ctx context.Context, opts Options) (Summary, error) {
	if opts.Attempts <= 0 {
		opts.Attempts = 2
	}
	if opts.Parallel <= 0 {
		opts.Parallel = 1
	}
	if opts.MaxDelta == 0 {
		opts.MaxDelta = 40
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.NewDriver == nil {
		return Summary{}, fmt.Errorf("no driver factory")
	}
	if opts.ArtifactDir != "" {
		if err := os.MkdirAll(opts.ArtifactDir, 0o755); err != nil {
			return Summary{}, err
		}
	}

	started := time.Now()
	summary := Summary{Outcomes: make([]Outcome, len(opts.Scenarios)), Parallel: opts.Parallel}

	indexes := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < opts.Parallel; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				summary.Outcomes[i] = runOne(ctx, opts, opts.Scenarios[i])
			}
		}()
	}
	for i := range opts.Scenarios {
		indexes <- i
	}
	close(indexes)
	wg.Wait()

	summary.WallClockMS = float64(time.Since(started).Microseconds()) / 1000
	for _, o := range summary.Outcomes {
		switch {
		case !o.Passed:
			summary.Failed++
		case o.Flaked:
			summary.Flaked++
		default:
			summary.Passed++
		}
	}
	sort.Slice(summary.Outcomes, func(i, j int) bool { return summary.Outcomes[i].ID < summary.Outcomes[j].ID })
	return summary, nil
}

func runOne(ctx context.Context, opts Options, spec *scenario.Scenario) Outcome {
	outcome := Outcome{ID: spec.ID(), Name: spec.Name, Tags: spec.Tags}
	began := time.Now()
	for attempt := 1; attempt <= opts.Attempts; attempt++ {
		outcome.Attempts = attempt
		outcome.Steps = nil
		outcome.Error = ""
		outcome.FailedStep = 0
		outcome.ImageNote = ""

		steps, err := runAttempt(ctx, opts, spec, &outcome)
		outcome.Steps = steps
		if err == nil {
			outcome.Passed = true
			outcome.Flaked = attempt > 1
			outcome.DurationMS = float64(time.Since(began).Microseconds()) / 1000
			return outcome
		}
		outcome.Error = err.Error()
	}
	outcome.Passed = false
	outcome.DurationMS = float64(time.Since(began).Microseconds()) / 1000
	return outcome
}

func runAttempt(ctx context.Context, opts Options, spec *scenario.Scenario, outcome *Outcome) ([]StepResult, error) {
	d := opts.NewDriver()
	if err := d.Start(ctx); err != nil {
		return nil, err
	}
	defer func() { _ = d.Stop(context.Background()) }()

	// Every run gets the same canvas, whether the browser was launched or attached to. A visual
	// assertion with a fixed region is a statement about a page layout, and leaving the viewport to
	// whatever the browser happens to default to makes that statement depend on the browser: the
	// four visual scenarios here passed on the machine that captured their templates and failed
	// against an attached browser with an 800x600 window, because a region at y=150 with height 480
	// does not fit in 600 pixels.
	width, height := DefaultViewport.Width, DefaultViewport.Height
	if spec.Viewport.Width > 0 && spec.Viewport.Height > 0 {
		width, height = spec.Viewport.Width, spec.Viewport.Height
	}
	if width > 0 && height > 0 {
		if err := d.SetViewport(ctx, width, height); err != nil {
			return nil, err
		}
	}
	if spec.URL != "" {
		if err := d.Open(ctx, join(opts.BaseURL, spec.URL)); err != nil {
			return nil, err
		}
	}

	var results []StepResult
	// capture is true only here: a screenshot belongs to the failure that ends a scenario, not to every
	// poll of an assert_eventually that is still waiting.
	err := execute(ctx, opts, spec, spec.Steps, d, &results, outcome, true)
	return results, err
}

func execute(ctx context.Context, opts Options, spec *scenario.Scenario, steps []scenario.Step, d driver.Driver, results *[]StepResult, outcome *Outcome, capture bool) error {
	for i, step := range steps {
		began := time.Now()
		result := StepResult{Action: step.Action}
		err := runStep(ctx, opts, spec, step, d, outcome)
		result.DurationMS = float64(time.Since(began).Microseconds()) / 1000
		result.Passed = err == nil
		if err != nil {
			result.Error = err.Error()
			outcome.FailedStep = i + 1
		}
		*results = append(*results, result)
		if err != nil {
			// A screenshot of the failure, taken while the page is still in the state that failed - but
			// only once. An assert_eventually polling every 20 ms for three seconds used to write a
			// hundred and fifty near-identical PNGs, which is a report nobody opens.
			if capture {
				saveFailureShot(ctx, opts, d, outcome, i+1)
			}
			return fmt.Errorf("step %d (%s): %w", i+1, step.Action, err)
		}
	}
	return nil
}

// saveFailureShot keeps what the assertion saw.
func saveFailureShot(ctx context.Context, opts Options, d driver.Driver, outcome *Outcome, stepIndex int) {
	if shot, shotErr := d.Screenshot(ctx); shotErr == nil {
		artifacts, embeds := saveShot(opts, outcome, stepIndex, shot)
		outcome.Artifacts = append(outcome.Artifacts, artifacts...)
		outcome.Embeds = append(outcome.Embeds, embeds...)
	}
}

func runStep(ctx context.Context, opts Options, spec *scenario.Scenario, step scenario.Step, d driver.Driver, outcome *Outcome) error {
	switch step.Action {
	case scenario.ActionOpen:
		return d.Open(ctx, join(opts.BaseURL, step.URL))

	case scenario.ActionClick:
		return d.Click(ctx, step.Selector)

	case scenario.ActionFill:
		return d.Fill(ctx, step.Selector, step.Value)

	case scenario.ActionAssertVisible:
		ok, err := d.Visible(ctx, step.Selector)
		if err != nil {
			return err
		}
		if !ok {
			// Say which of the two ways it failed. "not visible" covers a missing element and an
			// element on the page but hidden, and they are different problems.
			exists, existErr := d.Exist(ctx, step.Selector)
			if existErr == nil && exists {
				return fmt.Errorf("%q is in the page but not visible", step.Selector)
			}
			return fmt.Errorf("nothing matches %q", step.Selector)
		}
		return nil

	case scenario.ActionAssertText:
		got, err := d.Text(ctx, step.Selector)
		if err != nil {
			return err
		}
		if !strings.Contains(got, step.Text) {
			return fmt.Errorf("%q is %q, which does not contain %q", step.Selector, truncate(got), step.Text)
		}
		return nil

	case scenario.ActionAssertImage:
		return assertImage(ctx, opts, step, d, outcome)

	case scenario.ActionAssertEventually:
		timeout := step.Timeout
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		poll := step.Poll
		if poll == 0 {
			poll = 200 * time.Millisecond
		}
		deadline := time.Now().Add(timeout)
		var last error
		for {
			var scratch []StepResult
			last = execute(ctx, opts, spec, step.Steps, d, &scratch, outcome, false)
			if last == nil {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("still failing after %s: %w", timeout, last)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(poll):
			}
		}
	}
	return fmt.Errorf("unhandled action %q", step.Action)
}

func assertImage(ctx context.Context, opts Options, step scenario.Step, d driver.Driver, outcome *Outcome) error {
	templatePath := step.Template
	if !filepath.IsAbs(templatePath) {
		templatePath = filepath.Join(opts.TemplatesDir, templatePath)
	}
	template, err := loadImage(templatePath)
	if err != nil {
		return err
	}
	shot, err := d.Screenshot(ctx)
	if err != nil {
		return err
	}

	var region *vision.Rect
	if step.Region != nil {
		region = &vision.Rect{X: step.Region.X, Y: step.Region.Y, W: step.Region.W, H: step.Region.H}
	}
	found, err := vision.Find(shot, template, region)
	if err != nil {
		return err
	}
	threshold := step.ThresholdOf()
	if opts.ImageThreshold > 0 {
		threshold = opts.ImageThreshold
	}
	note := fmt.Sprintf("best match %.3f at (%d,%d), mean colour difference %.1f, threshold %.2f, max delta %.0f",
		found.Score, found.X, found.Y, found.Delta, threshold, opts.MaxDelta)
	if opts.ImageThreshold > 0 {
		// The note has to carry the override: a report that says "threshold 0.60" without saying
		// whose threshold it is looks like a test that passed on its own terms.
		note += fmt.Sprintf(" (run threshold %.2f overrides the scenario's %.2f)", opts.ImageThreshold, step.ThresholdOf())
	}
	outcome.ImageNote = note
	if !found.Passes(threshold, opts.MaxDelta) {
		// A picture of what it saw, with the match boxed, so the failure can be judged by looking.
		highlighted := vision.Highlight(shot, found, template.Bounds().Dx(), template.Bounds().Dy(), 3)
		artifacts, embeds := saveShot(opts, outcome, 0, highlighted)
		outcome.Artifacts = append(outcome.Artifacts, artifacts...)
		outcome.Embeds = append(outcome.Embeds, embeds...)
		return fmt.Errorf("the page does not contain the expected badge (%s): %s", step.Template, note)
	}
	return nil
}

func saveShot(opts Options, outcome *Outcome, stepIndex int, img image.Image) (artifacts []string, embeds []string) {
	if opts.ArtifactDir == "" {
		return nil, nil
	}
	suffix := "match"
	if stepIndex > 0 {
		suffix = fmt.Sprintf("step%d", stepIndex)
	}
	attempt := outcome.Attempts
	if attempt == 0 {
		attempt = 1
	}
	name := fmt.Sprintf("%s-attempt%d-%s.png", outcome.ID, attempt, suffix)
	path := filepath.Join(opts.ArtifactDir, name)
	if err := writePNG(path, img); err != nil {
		return nil, nil
	}
	artifacts = append(artifacts, path)
	small := filepath.Join(opts.ArtifactDir, strings.TrimSuffix(name, ".png")+"-small.png")
	if err := writePNG(small, downscale(img, 640)); err != nil {
		return artifacts, nil
	}
	return artifacts, []string{small}
}

// downscale is a box filter, written here because pulling an image library in for one resize is not a
// trade this repository makes.
func downscale(img image.Image, maxWidth int) image.Image {
	bounds := img.Bounds()
	if bounds.Dx() <= maxWidth {
		return img
	}
	factor := bounds.Dx() / maxWidth
	if factor < 1 {
		factor = 1
	}
	out := image.NewRGBA(image.Rect(0, 0, bounds.Dx()/factor, bounds.Dy()/factor))
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			var r, g, b, a, n uint32
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					sr, sg, sb, sa := img.At(bounds.Min.X+x*factor+dx, bounds.Min.Y+y*factor+dy).RGBA()
					r += sr >> 8
					g += sg >> 8
					b += sb >> 8
					a += sa >> 8
					n++
				}
			}
			out.Set(x, y, rgba(uint8(r/n), uint8(g/n), uint8(b/n), uint8(a/n)))
		}
	}
	return out
}

func join(base, path string) string {
	if path == "" {
		return base
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(path, "/")
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}
