// Package scenario parses the YAML a person writes: a list of steps, nothing more.
//
// The schema is closed. An unknown key is an error rather than something quietly ignored, because the
// failure mode of an open schema is a test that passes while asserting something nobody asked for -
// a typo in "asert_text" that runs a scenario doing nothing at all.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Action is the step discriminator. Every step names what it does, so a reader can scan a scenario
// without knowing the order of the keys.
type Action string

const (
	ActionOpen             Action = "open"
	ActionClick            Action = "click"
	ActionFill             Action = "fill"
	ActionAssertText       Action = "assert_text"
	ActionAssertVisible    Action = "assert_visible"
	ActionAssertImage      Action = "assert_image"
	ActionAssertEventually Action = "assert_eventually"
)

// Step is one instruction. Fields that belong to another action must be left out: the validator says
// so, instead of carrying a value that never has an effect.
type Step struct {
	Action    Action        `yaml:"action"`
	URL       string        `yaml:"url,omitempty"`
	Selector  string        `yaml:"selector,omitempty"`
	Value     string        `yaml:"value,omitempty"`
	Text      string        `yaml:"text,omitempty"`
	Template  string        `yaml:"template,omitempty"`
	Threshold float64       `yaml:"threshold,omitempty"`
	Region    *Region       `yaml:"region,omitempty"`
	Timeout   time.Duration `yaml:"timeout,omitempty"`
	Poll      time.Duration `yaml:"poll,omitempty"`
	Steps     []Step        `yaml:"steps,omitempty"`
	Note      string        `yaml:"note,omitempty"`
}

// Region restricts a visual assertion to part of the screenshot. Whole-page matching finds a template
// anywhere, including in a header that happens to look the same, so a page with repeated chrome passes
// a check that was written about a panel.
type Region struct {
	X int `yaml:"x"`
	Y int `yaml:"y"`
	W int `yaml:"w"`
	H int `yaml:"h"`
}

// Scenario is one file under scenarios/.
type Scenario struct {
	Name     string   `yaml:"name"`
	URL      string   `yaml:"url"`
	Tags     []string `yaml:"tags,omitempty"`
	Viewport struct {
		Width  int `yaml:"width,omitempty"`
		Height int `yaml:"height,omitempty"`
	} `yaml:"viewport,omitempty"`
	Steps []Step `yaml:"steps"`
	// Path is where the scenario was read from, filled in by Load. Not part of the file.
	Path string `yaml:"-"`
}

func (s *Scenario) ID() string {
	if s.Path == "" {
		return s.Name
	}
	return strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
}

// Load reads one scenario file, strictly.
func Load(path string) (*Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Scenario
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.Path = path
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// LoadDir reads every .yaml under dir, sorted by path so a run is reproducible.
func LoadDir(dir string) ([]*Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		paths = append(paths, filepath.Join(dir, e.Name()))
	}
	sort.Strings(paths)
	scenarios := make([]*Scenario, 0, len(paths))
	for _, p := range paths {
		s, err := Load(p)
		if err != nil {
			return nil, err
		}
		scenarios = append(scenarios, s)
	}
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios in %s", dir)
	}
	return scenarios, nil
}

// Validate rejects a scenario that would silently do nothing, and one that carries a field its action
// does not use.
func (s *Scenario) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}
	return validateSteps(s.Steps, "")
}

func validateSteps(steps []Step, prefix string) error {
	for i, st := range steps {
		where := fmt.Sprintf("step %d%s", i+1, prefix)
		if err := st.validate(); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if err := validateSteps(st.Steps, fmt.Sprintf(".%d", i+1)); err != nil {
			return err
		}
	}
	return nil
}

func (st *Step) validate() error {
	need := func(fields ...string) error {
		for _, f := range fields {
			var empty bool
			switch f {
			case "selector":
				empty = st.Selector == ""
			case "text":
				empty = st.Text == ""
			case "template":
				empty = st.Template == ""
			case "url":
				empty = st.URL == ""
			case "steps":
				empty = len(st.Steps) == 0
			}
			if empty {
				return fmt.Errorf("%s requires %s", st.Action, f)
			}
		}
		return nil
	}
	forbid := func(fields ...string) error {
		for _, f := range fields {
			var set bool
			switch f {
			case "selector":
				set = st.Selector != ""
			case "value":
				set = st.Value != ""
			case "text":
				set = st.Text != ""
			case "template":
				set = st.Template != ""
			case "threshold":
				set = st.Threshold != 0
			case "region":
				set = st.Region != nil
			case "steps":
				set = len(st.Steps) > 0
			case "url":
				set = st.URL != ""
			}
			if set {
				return fmt.Errorf("%s does not use %s", st.Action, f)
			}
		}
		return nil
	}

	switch st.Action {
	case ActionOpen:
		if err := need("url"); err != nil {
			return err
		}
		return forbid("selector", "value", "text", "template", "steps")
	case ActionClick:
		if err := need("selector"); err != nil {
			return err
		}
		return forbid("value", "text", "template", "steps")
	case ActionFill:
		if err := need("selector"); err != nil {
			return err
		}
		if st.Value == "" {
			return fmt.Errorf("fill requires value")
		}
		return forbid("text", "template", "steps")
	case ActionAssertText:
		if err := need("text"); err != nil {
			return err
		}
		return forbid("template", "steps", "value")
	case ActionAssertVisible:
		if err := need("selector"); err != nil {
			return err
		}
		return forbid("value", "text", "template", "steps")
	case ActionAssertImage:
		if err := need("template"); err != nil {
			return err
		}
		if st.Threshold != 0 && (st.Threshold <= 0 || st.Threshold > 1) {
			return fmt.Errorf("threshold must be within (0,1], got %v", st.Threshold)
		}
		return forbid("value", "text", "steps")
	case ActionAssertEventually:
		if err := need("steps"); err != nil {
			return err
		}
		return forbid("value", "text", "template", "selector")
	default:
		return fmt.Errorf("unknown action %q", st.Action)
	}
}

// ThresholdOf is the match score a visual assertion needs, defaulting to the one the README documents.
func (st *Step) ThresholdOf() float64 {
	if st.Threshold == 0 {
		return 0.85
	}
	return st.Threshold
}
