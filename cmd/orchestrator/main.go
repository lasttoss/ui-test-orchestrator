// Command orchestrator runs a scenario suite against a URL, or serves the fixture application the demo
// in the README uses, or captures a template for a visual assertion.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/app"
	"github.com/lasttoss/ui-test-orchestrator/internal/driver"
	"github.com/lasttoss/ui-test-orchestrator/internal/flake"
	"github.com/lasttoss/ui-test-orchestrator/internal/report"
	"github.com/lasttoss/ui-test-orchestrator/internal/runner"
	"github.com/lasttoss/ui-test-orchestrator/internal/scenario"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	switch command {
	case "run":
		return runSuite(args)
	case "serve":
		return serveFixture(args)
	case "capture":
		return captureTemplate(args)
	case "policy":
		return showPolicy(args)
	default:
		return fmt.Errorf("unknown command %q: try run, serve, capture or policy", command)
	}
}

type runFlags struct {
	scenarios string
	baseURL   string
	templates string
	parallel  int
	attempts  int
	browser   string
	cdpURL    string
	reportDir string
	artifacts string
	state     string
	noState   bool
	tag       string
	name      string
	maxDelta  float64
	jsonOut   string
	quiet     bool
}

func runSuite(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var f runFlags
	fs.StringVar(&f.scenarios, "scenarios", "scenarios", "directory of scenario YAML files")
	fs.StringVar(&f.baseURL, "base-url", "", "URL the scenarios run against (required)")
	fs.StringVar(&f.templates, "templates", "testdata/templates", "directory holding visual templates")
	fs.IntVar(&f.parallel, "parallel", 2, "how many scenarios run at once")
	fs.IntVar(&f.attempts, "attempts", 2, "attempts per scenario before it counts as a failure")
	fs.StringVar(&f.browser, "browser", "", "path to chromium (default: find one)")
	fs.StringVar(&f.cdpURL, "cdp-url", "", "attach to a browser already running, e.g. http://127.0.0.1:9222, instead of launching one")
	fs.StringVar(&f.reportDir, "report", "report", "directory for index.html and summary.json")
	fs.StringVar(&f.artifacts, "artifacts", "report/artifacts", "directory for failure screenshots")
	fs.StringVar(&f.state, "state", "state/quarantine.json", "flake history file")
	fs.BoolVar(&f.noState, "no-state", false, "do not read or write the flake history")
	fs.StringVar(&f.tag, "tag", "", "only run scenarios with this tag")
	fs.StringVar(&f.name, "name", "", "only run scenarios whose id contains this")
	fs.Float64Var(&f.maxDelta, "max-delta", 40, "largest mean colour difference a visual match may have")
	fs.StringVar(&f.jsonOut, "json", "", "also write the summary JSON here")
	fs.BoolVar(&f.quiet, "quiet", false, "only print the summary line")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if f.baseURL == "" {
		return errors.New("--base-url is required: the suite has to know what it is testing")
	}

	all, err := scenario.LoadDir(f.scenarios)
	if err != nil {
		return err
	}
	selected := selectScenarios(all, f.tag, f.name)
	if len(selected) == 0 {
		return fmt.Errorf("no scenario matched tag=%q name=%q", f.tag, f.name)
	}

	// With --cdp-url there is no browser to find: the one to use is already running.
	binary := f.cdpURL
	if f.cdpURL == "" {
		found, err := driver.FindBinary(f.browser)
		if err != nil {
			return err
		}
		binary = found
	}

	state := flake.New()
	if !f.noState {
		state, err = flake.Load(f.state)
		if err != nil {
			return err
		}
		state.Begin()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	summary, err := runner.Run(ctx, runner.Options{
		Scenarios: selected, BaseURL: f.baseURL, TemplatesDir: f.templates,
		Attempts: f.attempts, Parallel: f.parallel, MaxDelta: f.maxDelta,
		ArtifactDir: f.artifacts,
		NewDriver: func() driver.Driver {
			return &driver.Chromium{Binary: binary, CDPURL: f.cdpURL, Timeout: 60 * time.Second}
		},
	})
	if err != nil {
		return err
	}

	// The policy is applied to real outcomes, never to intentions: a scenario marked quarantined in the
	// file still has to fail here to stay quarantined.
	now := time.Now()
	for _, o := range summary.Outcomes {
		if f.noState {
			continue
		}
		state.Apply(o.ID, flake.Run{Attempts: o.Attempts, Passed: o.Passed, Error: o.Error}, now)
	}

	if !f.noState {
		if err := state.Save(f.state); err != nil {
			return err
		}
	}
	flakes, scenarioRuns, rate := state.FlakeRate()
	if err := report.Write(f.reportDir, report.Data{
		Generated: now, BaseURL: f.baseURL, Summary: summary, State: state, Policy: state.Policy,
		Browser: browserLabel(binary), FlakeRate: rate, Flakes: flakes, ScenarioRun: scenarioRuns,
	}); err != nil {
		return err
	}
	if f.jsonOut != "" {
		raw, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(f.jsonOut, append(raw, '\n'), 0o644); err != nil {
			return err
		}
	}

	printSummary(summary, selected, state, rate, f)
	return exitFor(summary, state)
}

// exitFor is the whole point of the policy: a failure in an active scenario fails the build, and the
// same failure in a quarantined one does not - but it is still printed.
func exitFor(summary runner.Summary, state *flake.State) error {
	var failing []string
	for _, o := range summary.Outcomes {
		if o.Passed {
			continue
		}
		if state.Scenarios[o.ID].Status == flake.Quarantined {
			continue
		}
		failing = append(failing, o.ID)
	}
	if len(failing) > 0 {
		return fmt.Errorf("%d scenario(s) failed: %s", len(failing), strings.Join(failing, ", "))
	}
	return nil
}

func selectScenarios(all []*scenario.Scenario, tag, name string) []*scenario.Scenario {
	var out []*scenario.Scenario
	for _, s := range all {
		if tag != "" && !hasTag(s, tag) {
			continue
		}
		if name != "" && !strings.Contains(strings.ToLower(s.ID()), strings.ToLower(name)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func hasTag(s *scenario.Scenario, tag string) bool {
	for _, t := range s.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

func printSummary(summary runner.Summary, selected []*scenario.Scenario, state *flake.State, rate float64, f runFlags) {
	for _, o := range summary.Outcomes {
		if f.quiet && o.Passed && !o.Flaked {
			continue
		}
		mark := "ok  "
		switch {
		case !o.Passed && state.Scenarios[o.ID].Status == flake.Quarantined:
			mark = "QUAR"
		case !o.Passed:
			mark = "FAIL"
		case o.Flaked:
			mark = "FLAKE"
		}
		line := fmt.Sprintf("%-5s %-34s %s", mark, o.ID, ms(o.DurationMS))
		if o.Attempts > 1 {
			line += fmt.Sprintf("  attempts=%d", o.Attempts)
		}
		if o.Error != "" {
			line += "  " + firstLine(o.Error)
		}
		if o.ImageNote != "" {
			line += "  | " + o.ImageNote
		}
		fmt.Println(line)
	}
	fmt.Printf("\n%d scenarios · %d passed · %d flaked · %d failed · wall clock %s (parallel %d)\n",
		len(summary.Outcomes), summary.Passed, summary.Flaked, summary.Failed, ms(summary.WallClockMS), summary.Parallel)
	fmt.Printf("flake history: %d flakes over %d scenario runs (%.1f%%), quarantined now: %v\n",
		totalFlakes(state), totalRuns(state), rate, state.Quarantined())
	fmt.Printf("report: %s/index.html\n", f.reportDir)
}

func totalFlakes(state *flake.State) int {
	flakes, _, _ := state.FlakeRate()
	return flakes
}

func totalRuns(state *flake.State) int {
	_, runs, _ := state.FlakeRate()
	return runs
}

func ms(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("%.2fs", v/1000)
	}
	return fmt.Sprintf("%.0fms", v)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func browserLabel(path string) string {
	if strings.HasPrefix(path, "http") {
		return "attached: " + path
	}
	return filepath.Base(path)
}

// serveFixture runs the application the demo uses. It prints the address on stdout and then serves
// until interrupted, which is what a shell script needs to capture.
func serveFixture(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:0", "address to listen on")
	listenOnly := fs.Bool("print-address", true, "print the address on stdout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	options := app.FaultFromEnv()
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	if *listenOnly {
		fmt.Printf("http://%s\n", listener.Addr().String())
	}
	server := &http.Server{Handler: app.Handler(options), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		<-signals
		_ = server.Close()
	}()
	return server.Serve(listener)
}

// captureTemplate is how a visual assertion gets its template: find the element, crop the screenshot,
// write a PNG. Hand-cropping a screenshot is how templates end up one pixel out and permanently flaky.
func captureTemplate(args []string) error {
	fs := flag.NewFlagSet("capture", flag.ExitOnError)
	url := fs.String("url", "", "page to open (required)")
	selector := fs.String("selector", "", "element to capture (required)")
	out := fs.String("out", "", "PNG to write (required)")
	browser := fs.String("browser", "", "path to chromium")
	padding := fs.Int("padding", 4, "pixels of margin around the element")
	viewport := fs.String("viewport", "1280x800", "viewport as WxH")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || *selector == "" || *out == "" {
		return errors.New("--url, --selector and --out are required")
	}
	var width, height int
	if _, err := fmt.Sscanf(*viewport, "%dx%d", &width, &height); err != nil {
		return fmt.Errorf("--viewport %q: %w", *viewport, err)
	}

	ctx := context.Background()
	d := &driver.Chromium{Binary: *browser, Timeout: 60 * time.Second}
	if err := d.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = d.Stop(ctx) }()
	if err := d.SetViewport(ctx, width, height); err != nil {
		return err
	}
	if err := d.Open(ctx, *url); err != nil {
		return err
	}
	shot, err := d.Screenshot(ctx)
	if err != nil {
		return err
	}
	box, err := d.Box(ctx, *selector)
	if err != nil {
		return err
	}
	if box.W == 0 || box.H == 0 {
		return fmt.Errorf("element %q has no size on the page", *selector)
	}
	x := max(0, box.X-*padding)
	y := max(0, box.Y-*padding)
	w := min(shot.Bounds().Dx()-x, box.W+2**padding)
	h := min(shot.Bounds().Dy()-y, box.H+2**padding)
	if w <= 0 || h <= 0 {
		return fmt.Errorf("element %q has no size on the page", *selector)
	}
	cropped := image.NewRGBA(image.Rect(0, 0, w, h))
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			cropped.Set(i, j, shot.At(x+i, y+j))
		}
	}
	file, err := os.Create(*out)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := png.Encode(file, cropped); err != nil {
		return err
	}
	fmt.Printf("captured %s from %s at (%d,%d) %dx%d\n", *out, *selector, x, y, w, h)
	return nil
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

func showPolicy(args []string) error {
	fs := flag.NewFlagSet("policy", flag.ExitOnError)
	state := fs.String("state", "state/quarantine.json", "flake history file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := flake.Load(*state)
	if err != nil {
		return err
	}
	flakes, runs, rate := s.FlakeRate()
	fmt.Printf("state %s: %d runs recorded, %d flakes (%.1f%%), policy %d fail runs to quarantine, %d pass runs to release\n",
		*state, runs, flakes, rate, s.Policy.QuarantineAfter, s.Policy.UnquarantineAfter)
	for id, entry := range s.Scenarios {
		fmt.Printf("  %-34s %-12s runs=%d fails=%d flakes=%d streak(pass=%d fail=%d)\n",
			id, entry.Status, entry.Runs, entry.Fails, entry.Flakes, entry.ConsecutivePasses, entry.ConsecutiveFailures)
	}
	return nil
}
