package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadAcceptsTheDocumentedShape(t *testing.T) {
	s, err := Load(write(t, `
name: panel shows the audit view
url: /
tags: [smoke, panel]
steps:
  - action: open
    url: /
  - action: assert_text
    text: Live operations
  - action: click
    selector: "button[data-testid=audit]"
  - action: assert_eventually
    timeout: 5s
    poll: 200ms
    steps:
      - action: assert_visible
        selector: "[data-testid=audit-row]"
  - action: assert_image
    template: templates/badge.png
    threshold: 0.9
`))
	if err != nil {
		t.Fatalf("a valid scenario was rejected: %v", err)
	}
	if s.ID() != "s" {
		t.Errorf("ID = %q", s.ID())
	}
	if got := s.Steps[3].Steps[0].Action; got != ActionAssertVisible {
		t.Errorf("nested step action = %q", got)
	}
	if got := s.Steps[4].ThresholdOf(); got != 0.9 {
		t.Errorf("threshold = %v, want 0.9", got)
	}
	if got := s.Steps[0].ThresholdOf(); got != 0.85 {
		t.Errorf("default threshold = %v, want 0.85", got)
	}
}

// An open schema is a scenario that passes while asserting nothing: "asert_text" is a typo, not an
// instruction, and it must fail loudly.
func TestUnknownKeyIsAnError(t *testing.T) {
	_, err := Load(write(t, `
name: typo
steps:
  - action: open
    url: /
  - action: asert_text
    text: hello
`))
	if err == nil {
		t.Fatal("an unknown action was accepted")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error = %v", err)
	}
}

func TestFieldOnTheWrongActionIsAnError(t *testing.T) {
	for _, body := range []string{
		"name: n\nsteps:\n  - action: open\n    url: /\n    selector: '#a'\n",
		"name: n\nsteps:\n  - action: click\n    selector: '#a'\n    value: nope\n",
		"name: n\nsteps:\n  - action: fill\n    selector: '#a'\n",
		"name: n\nsteps:\n  - action: assert_image\n    template: t.png\n    threshold: 1.5\n",
		"name: n\nsteps:\n  - action: assert_text\n    text: x\n    steps:\n      - action: assert_visible\n        selector: '#a'\n",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("accepted a scenario that should fail:\n%s", body)
		}
	}
}

func TestEmptyScenarioIsRejected(t *testing.T) {
	if _, err := Load(write(t, "name: nothing\nsteps: []\n")); err == nil {
		t.Fatal("a scenario with no steps was accepted")
	}
}

func TestLoadDirIsSortedAndRefusesEmpty(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b.yaml", "a.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("name: "+name+"\nsteps:\n  - action: open\n    url: /\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID() != "a" || got[1].ID() != "b" {
		t.Errorf("order = %v, %v", got[0].ID(), got[1].ID())
	}
	if _, err := LoadDir(t.TempDir()); err == nil {
		t.Error("an empty directory was accepted")
	}
}
