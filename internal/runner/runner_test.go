package runner_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/driver"
	"github.com/lasttoss/ui-test-orchestrator/internal/runner"
	"github.com/lasttoss/ui-test-orchestrator/internal/scenario"
)

// The runner is tested against a fake driver, which is the point of the interface: what a scenario does,
// how a retry is counted and which artifacts are kept are all decidable without a browser, and a test
// that needs one is a test that runs in fewer places.
type fakeDriver struct {
	// Two scenarios run at once, so the counters below are guarded: an unguarded fake is a race in the
	// test, and a race in the test is reported against the code under test.
	mu sync.Mutex

	// failSelectors: a click or fill on one of these fails, which is how a scenario is made to fail.
	failSelectors []string
	// failTimes: how many times a scenario may fail before it succeeds. Zero means: every time.
	failTimes int
	attempts  int
	text      map[string]string
	visible   map[string]bool
	// becomeVisibleAfter: an element that appears after this many looks, which is what a slow application
	// looks like from an assertion.
	becomeVisibleAfter int
	looks              int
}

func (f *fakeDriver) Name() string { return "fake" }
func (f *fakeDriver) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	return nil
}
func (f *fakeDriver) Stop(ctx context.Context) error                  { return nil }
func (f *fakeDriver) SetViewport(ctx context.Context, w, h int) error { return nil }
func (f *fakeDriver) Open(ctx context.Context, url string) error      { return nil }
func (f *fakeDriver) Box(ctx context.Context, s string) (driver.Box, error) {
	return driver.Box{W: 10, H: 10}, nil
}

func (f *fakeDriver) fail(selector string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.failSelectors {
		if s == selector {
			if f.failTimes > 0 && f.attempts > f.failTimes {
				return nil
			}
			return fmt.Errorf("the fake was told to fail on %q (attempt %d)", selector, f.attempts)
		}
	}
	return nil
}

func (f *fakeDriver) Click(ctx context.Context, s string) error { return f.fail(s) }
func (f *fakeDriver) Fill(ctx context.Context, s, v string) error {
	return f.fail(s)
}
func (f *fakeDriver) Text(ctx context.Context, s string) (string, error) {
	if err := f.fail(s); err != nil {
		return "", err
	}
	if v, ok := f.text[s]; ok {
		return v, nil
	}
	return "", fmt.Errorf("the fake has no text for %q", s)
}
func (f *fakeDriver) Exist(ctx context.Context, s string) (bool, error) {
	if _, ok := f.text[s]; ok {
		return true, nil
	}
	return f.visible[s], nil
}
func (f *fakeDriver) Visible(ctx context.Context, s string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.looks++
	if f.visible[s] {
		return true, nil
	}
	if _, ok := f.text[s]; ok {
		return true, nil
	}
	if f.becomeVisibleAfter > 0 && f.looks >= f.becomeVisibleAfter {
		return true, nil
	}
	return false, nil
}
func (f *fakeDriver) Screenshot(ctx context.Context) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}
	return img, nil
}

// spec writes one scenario to its own file: two scenarios in one file name share an id, and an id is
// what the policy keys on.
func spec(t *testing.T, body string) *scenario.Scenario {
	t.Helper()
	dir, err := os.MkdirTemp("", "uito-spec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	name := fmt.Sprintf("%s-%d.yaml", strings.TrimPrefix(firstName(body), "name: "), idCounter())
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := scenario.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var idSeq int

func idCounter() int { idSeq++; return idSeq }

func firstName(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(line)
		}
	}
	return "spec"
}

func options(t *testing.T, f *fakeDriver, specs ...*scenario.Scenario) runner.Options {
	t.Helper()
	return runner.Options{
		Scenarios: specs, BaseURL: "http://fake", TemplatesDir: t.TempDir(),
		Parallel: 2, Attempts: 2, ArtifactDir: filepath.Join(t.TempDir(), "artifacts"),
		NewDriver: func() driver.Driver { return f },
	}
}

func TestPassingSuiteIsCountedAsPassed(t *testing.T) {
	f := &fakeDriver{text: map[string]string{"#title": "Live operations"}}
	specs := []*scenario.Scenario{
		spec(t, "name: a\nurl: /\nsteps:\n  - action: assert_text\n    selector: '#title'\n    text: Live operations\n"),
		spec(t, "name: b\nurl: /\nsteps:\n  - action: assert_visible\n    selector: '#title'\n"),
	}
	summary, err := runner.Run(context.Background(), options(t, f, specs...))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Passed != 2 || summary.Failed != 0 || summary.Flaked != 0 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.WallClockMS <= 0 {
		t.Error("no wall clock was measured")
	}
	// Sorted by id, so a run's report has a stable order and a diff shows what changed.
	if summary.Outcomes[0].ID >= summary.Outcomes[1].ID {
		t.Errorf("outcomes are not sorted: %v, %v", summary.Outcomes[0].ID, summary.Outcomes[1].ID)
	}
}

// A failure followed by a pass is a flake, and it is neither a pass nor a failure. Getting this wrong in
// either direction is how a suite becomes noise.
func TestFailureThenPassIsAFlake(t *testing.T) {
	f := &fakeDriver{failSelectors: []string{"#flaky"}, failTimes: 1, text: map[string]string{}}
	specs := []*scenario.Scenario{spec(t, "name: flaky\nurl: /\nsteps:\n  - action: click\n    selector: '#flaky'\n")}
	summary, err := runner.Run(context.Background(), options(t, f, specs...))
	if err != nil {
		t.Fatal(err)
	}
	got := summary.Outcomes[0]
	if !got.Passed || !got.Flaked || got.Attempts != 2 {
		t.Fatalf("outcome = %+v", got)
	}
	if summary.Flaked != 1 || summary.Failed != 0 || summary.Passed != 0 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestFailingScenarioKeepsTheScreenshotOfTheFailure(t *testing.T) {
	f := &fakeDriver{failSelectors: []string{"#gone"}}
	specs := []*scenario.Scenario{spec(t, "name: broken\nurl: /\nsteps:\n  - action: click\n    selector: '#gone'\n")}
	opts := options(t, f, specs...)
	summary, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := summary.Outcomes[0]
	if got.Passed || got.Attempts != 2 {
		t.Fatalf("outcome = %+v", got)
	}
	if got.FailedStep != 1 {
		t.Errorf("failed step = %d", got.FailedStep)
	}
	if len(got.Artifacts) == 0 {
		t.Fatal("no artifact was kept: a failure a reader cannot look at is a failure they will argue with")
	}
	for _, path := range got.Artifacts {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("artifact %s: %v", path, err)
		}
	}
	if len(got.Embeds) == 0 {
		t.Error("no small copy was written for the report to inline")
	}
	// The embed has to be small enough to inline, which means narrower than the 200px screenshot only if
	// downscaling was needed - here it is not, so it should at least exist and decode.
	file, err := os.Open(got.Embeds[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := png.Decode(file); err != nil {
		t.Errorf("the embedded image does not decode: %v", err)
	}
}

// A visual failure has to explain itself in numbers, or the person reading the report has to guess
// whether the page moved, changed colour, or was never right.
func TestVisualFailureReportsScoreAndColour(t *testing.T) {
	f := &fakeDriver{}
	specs := []*scenario.Scenario{spec(t, "name: visual\nurl: /\nsteps:\n  - action: assert_image\n    template: nothing-like-this.png\n    threshold: 0.99\n")}
	opts := options(t, f, specs...)
	templates := filepath.Join(t.TempDir(), "templates")
	if err := os.MkdirAll(templates, 0o755); err != nil {
		t.Fatal(err)
	}
	// A template that is not in the screenshot, so the assertion fails and says why.
	blank := image.NewRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			blank.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	path := filepath.Join(templates, "nothing-like-this.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, blank); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	opts.TemplatesDir = templates

	summary, err := runner.Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	got := summary.Outcomes[0]
	if got.Passed {
		t.Fatal("a template that is not on the page passed")
	}
	if got.ImageNote == "" {
		t.Fatal("the failure did not say what it saw")
	}
	for _, want := range []string{"best match", "threshold 0.99"} {
		if !contains(got.ImageNote, want) {
			t.Errorf("image note %q does not mention %q", got.ImageNote, want)
		}
	}
}

// assert_eventually is the difference between a check that races the application and one that waits for
// it, so it is asserted here rather than assumed.
func TestAssertEventuallyWaitsForTheApplication(t *testing.T) {
	f := &fakeDriver{becomeVisibleAfter: 3, text: map[string]string{}}
	specs := []*scenario.Scenario{spec(t, `
name: waits
url: /
steps:
  - action: assert_eventually
    timeout: 3s
    poll: 10ms
    steps:
      - action: assert_visible
        selector: '#rows'
`)}
	summary, err := runner.Run(context.Background(), options(t, f, specs...))
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Outcomes[0].Passed {
		t.Fatalf("the step did not wait: passed=%v attempts=%d failedStep=%d error=%q",
			summary.Outcomes[0].Passed, summary.Outcomes[0].Attempts, summary.Outcomes[0].FailedStep,
			summary.Outcomes[0].Error)
	}
	f.mu.Lock()
	looks := f.looks
	f.mu.Unlock()
	if looks < 3 {
		t.Errorf("it only looked %d times", looks)
	}
	// Polling must not leave a screenshot behind for every look.
	if len(summary.Outcomes[0].Artifacts) != 0 {
		t.Errorf("a passing scenario kept %d artifacts", len(summary.Outcomes[0].Artifacts))
	}
}

func TestAssertEventuallyGivesUpAndSaysSo(t *testing.T) {
	f := &fakeDriver{failSelectors: []string{"#never"}}
	specs := []*scenario.Scenario{spec(t, `
name: gives up
url: /
steps:
  - action: assert_eventually
    timeout: 150ms
    poll: 20ms
    steps:
      - action: click
        selector: '#never'
`)}
	started := time.Now()
	summary, err := runner.Run(context.Background(), options(t, f, specs...))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Outcomes[0].Passed {
		t.Fatal("a step that never passes passed")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("gave up after %s, which is not the timeout it was given", elapsed)
	}
	if !contains(summary.Outcomes[0].Error, "still failing after") {
		t.Errorf("error = %q", summary.Outcomes[0].Error)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
