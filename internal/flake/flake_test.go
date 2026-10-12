package flake

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

func fail(msg string) Run { return Run{Attempts: 1, Passed: false, Error: msg} }
func pass() Run           { return Run{Attempts: 1, Passed: true} }
func flaked() Run         { return Run{Attempts: 2, Passed: true} }

func TestTwoWholeRunsFailingQuarantines(t *testing.T) {
	s := New()
	if got := s.Apply("a", fail("boom"), now); got.Status != Active {
		t.Fatalf("one failure quarantined: %+v", got)
	}
	got := s.Apply("a", fail("boom"), now)
	if got.Status != Quarantined {
		t.Fatalf("two failures did not quarantine: %+v", got)
	}
	if got.QuarantinedAt.IsZero() || !strings.Contains(got.QuarantinedReason, "boom") {
		t.Errorf("the reason for quarantine is not recorded: %+v", got)
	}
	if ids := s.Quarantined(); len(ids) != 1 || ids[0] != "a" {
		t.Errorf("quarantined = %v", ids)
	}
}

func TestThreeWholeRunsPassingReleases(t *testing.T) {
	s := New()
	s.Apply("a", fail("x"), now)
	s.Apply("a", fail("x"), now)

	for i := 0; i < 2; i++ {
		if got := s.Apply("a", pass(), now); got.Status != Quarantined {
			t.Fatalf("released after %d passes: %+v", i+1, got)
		}
	}
	got := s.Apply("a", pass(), now)
	if got.Status != Active {
		t.Fatalf("three passes did not release: %+v", got)
	}
	if !got.QuarantinedAt.IsZero() || got.QuarantinedReason != "" {
		t.Errorf("release left the quarantine record behind: %+v", got)
	}
}

// A failure in the middle resets the evidence. Otherwise a scenario that fails every other run would
// be released by three passes spread across six runs, which is not evidence of anything.
func TestReleaseNeedsConsecutivePasses(t *testing.T) {
	s := New()
	s.Apply("a", fail("x"), now)
	s.Apply("a", fail("x"), now)
	s.Apply("a", pass(), now)
	s.Apply("a", pass(), now)
	s.Apply("a", fail("x"), now)
	got := s.Apply("a", pass(), now)
	if got.Status != Quarantined {
		t.Fatalf("a failure in the middle did not reset the streak: %+v", got)
	}
	if got.ConsecutivePasses != 1 {
		t.Errorf("consecutive passes = %d", got.ConsecutivePasses)
	}
}

// A scenario that fails the first attempt and passes the second is a flake, not a failure: it must not
// count towards quarantine, and it must count towards the flake rate.
func TestFlakeIsNotAFailure(t *testing.T) {
	s := New()
	s.Apply("a", flaked(), now)
	s.Apply("a", flaked(), now)
	s.Apply("a", flaked(), now)

	entry := s.Scenarios["a"]
	if entry.Status != Active {
		t.Errorf("a flake quarantined the scenario: %+v", entry)
	}
	if entry.ConsecutiveFailures != 0 {
		t.Errorf("a flake counted as a failure: %+v", entry)
	}
	flakes, runs, rate := s.FlakeRate()
	if flakes != 3 || runs != 3 || rate != 100 {
		t.Errorf("flake rate = %d/%d = %v", flakes, runs, rate)
	}
}

func TestFlakeRateOverMixedHistory(t *testing.T) {
	s := New()
	s.Apply("a", flaked(), now)
	s.Apply("a", pass(), now)
	s.Apply("b", flaked(), now)
	s.Apply("b", pass(), now)
	if flakes, runs, rate := s.FlakeRate(); flakes != 2 || runs != 4 || rate != 50 {
		t.Errorf("flake rate = %d/%d = %v", flakes, runs, rate)
	}
	if _, _, rate := New().FlakeRate(); rate != 0 {
		t.Errorf("empty state reported %v", rate)
	}
}

// The state file is committed, so a run that changes nothing has to produce no diff. Map key order is
// the usual reason that fails.
func TestSaveIsStable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "quarantine.json")

	s := New()
	for _, id := range []string{"zeta", "alpha", "mid"} {
		s.Apply(id, pass(), now)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Save(path); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("a save-load-save cycle changed the file:\n%s\n%s", first, second)
	}
	if !strings.HasSuffix(string(first), "\n") {
		t.Error("the file does not end with a newline")
	}

	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["policy"].(map[string]any)["quarantine_after"].(float64) != 2 {
		t.Errorf("the policy is not recorded in the file: %s", first)
	}
}

func TestLoadMissingFileIsAFreshState(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Runs != 0 || len(s.Scenarios) != 0 || s.Policy.QuarantineAfter != 2 {
		t.Errorf("fresh state = %+v", s)
	}
}

func TestLoadRejectsBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("broken state was accepted")
	}
}

// A run recorded with Begin but never applied still counts: a suite that crashes halfway is evidence
// that the suite ran, and the report says so.
func TestBeginCountsTheRun(t *testing.T) {
	s := New()
	s.Begin()
	s.Begin()
	if s.Runs != 2 {
		t.Errorf("runs = %d", s.Runs)
	}
}
