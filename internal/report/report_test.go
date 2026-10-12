package report_test

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/flake"
	"github.com/lasttoss/ui-test-orchestrator/internal/report"
	"github.com/lasttoss/ui-test-orchestrator/internal/runner"
)

func data(t *testing.T) (report.Data, string) {
	t.Helper()
	dir := t.TempDir()

	// A screenshot on disk, because the report inlines what it finds and skips what it does not.
	shot := filepath.Join(dir, "shot.png")
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	file, err := os.Create(shot)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()

	state := flake.New()
	state.Apply("broken", flake.Run{Attempts: 2, Passed: false, Error: "step 1 (assert_image): nope"}, time.Now())
	state.Apply("broken", flake.Run{Attempts: 2, Passed: false, Error: "step 1 (assert_image): nope"}, time.Now())

	return report.Data{
		Generated: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
		BaseURL:   "http://fixture",
		Browser:   "chrome",
		State:     state,
		Policy:    state.Policy,
		FlakeRate: 12.5,
		Summary: runner.Summary{
			WallClockMS: 1234, Parallel: 3, Passed: 1, Failed: 1, Flaked: 1,
			Outcomes: []runner.Outcome{
				{ID: "good", Name: "passes", Passed: true, Attempts: 1, DurationMS: 40},
				{ID: "flaky", Name: "flaked", Passed: true, Attempts: 2, Flaked: true, DurationMS: 900},
				{ID: "broken", Name: "fails", Passed: false, Attempts: 2, DurationMS: 800,
					FailedStep: 1, Error: "step 1 (assert_image): nope",
					ImageNote: "best match 0.999 at (77,176), mean colour difference 51.7",
					Embeds:    []string{shot}},
			},
		},
	}, dir
}

func TestReportIsSelfContainedAndReadable(t *testing.T) {
	d, _ := data(t)
	out := filepath.Join(t.TempDir(), "report")
	if err := report.Write(out, d); err != nil {
		t.Fatal(err)
	}

	html, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	for _, want := range []string{
		"data:image/png;base64", // the screenshot travels inside the file
		"best match 0.999",      // what the visual assertion saw
		"mean colour difference 51.7",
		"quarantined",
		"broken", "flaky", "good",
		"passed on attempt 2",
		"12.5%",
		"2 / 3", // the policy, printed so a reader knows the rules
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the report does not mention %q", want)
		}
	}
	if strings.Contains(page, "<img alt=\"screenshot of the failure\" src=\"shot.png\"") {
		t.Error("the report links to the image instead of inlining it")
	}

	var summary map[string]any
	raw, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["failed"].(float64) != 1 || summary["flaked"].(float64) != 1 {
		t.Errorf("summary counts = %v", summary)
	}
	if q, ok := summary["quarantined"].([]any); !ok || len(q) != 1 || q[0] != "broken" {
		t.Errorf("quarantined = %v", summary["quarantined"])
	}
	if summary["flake_rate_percent"].(float64) != 12.5 {
		t.Errorf("flake rate = %v", summary["flake_rate_percent"])
	}
}

func TestReportWithNoImagesStillWrites(t *testing.T) {
	// A suite that never fails has nothing to inline, and a report writer that assumes a screenshot is a
	// report writer that panics on a green run.
	d, _ := data(t)
	d.Summary.Outcomes = []runner.Outcome{{ID: "good", Name: "passes", Passed: true, Attempts: 1}}
	out := filepath.Join(t.TempDir(), "report")
	if err := report.Write(out, d); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "good") {
		t.Error("the green run is missing from the report")
	}
}
