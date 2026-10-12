// Package flake decides what to do about a test that fails sometimes.
//
// The usual answers are both bad: retry until it passes and nobody learns anything, or fail the build
// and everyone learns to ignore the build. This keeps the answer in a file that is committed, so the
// decision is visible, reviewable and reversible: a scenario that fails whole runs in a row is
// quarantined, keeps running, and comes back on its own after it passes whole runs in a row.
package flake

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Status is what the policy currently thinks of a scenario.
type Status string

const (
	// Active scenarios count: a failure is a build failure.
	Active Status = "active"
	// Quarantined scenarios still run and are still reported, but their failures do not fail the build.
	// They are not hidden, because hiding them is how a quarantined test becomes a deleted test.
	Quarantined Status = "quarantined"
)

// Entry is one scenario's history. Everything here is derived from runs that happened; nothing is
// hand-maintained, so the file cannot drift from what the suite actually did.
type Entry struct {
	Status              Status    `json:"status"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	ConsecutivePasses   int       `json:"consecutive_passes"`
	Flakes              int       `json:"flakes"`
	Runs                int       `json:"runs"`
	Fails               int       `json:"fails"`
	QuarantinedAt       time.Time `json:"quarantined_at,omitempty"`
	QuarantinedReason   string    `json:"quarantined_reason,omitempty"`
	LastRun             time.Time `json:"last_run,omitempty"`
	LastError           string    `json:"last_error,omitempty"`
}

// State is the file. The map is keyed by scenario id, and encoding/json sorts map keys, so a run that
// changes nothing produces no diff.
type State struct {
	Version   int              `json:"version"`
	Policy    Policy           `json:"policy"`
	Scenarios map[string]Entry `json:"scenarios"`
	// Runs counts how many times the suite has been executed with this file, which is how the report
	// can say "quarantined two runs ago".
	Runs int `json:"runs"`
}

// Policy is the threshold pair, kept in the file so a reader can see which rules produced the state.
type Policy struct {
	QuarantineAfter   int `json:"quarantine_after"`
	UnquarantineAfter int `json:"unquarantine_after"`
}

// DefaultPolicy: two whole runs failing in a row is a scenario that does not work here; three whole
// runs passing in a row is enough evidence to trust it again.
func DefaultPolicy() Policy { return Policy{QuarantineAfter: 2, UnquarantineAfter: 3} }

// New returns an empty state with the default policy.
func New() *State {
	return &State{Version: 1, Policy: DefaultPolicy(), Scenarios: map[string]Entry{}}
}

// Load reads the state, or returns a fresh one when the file does not exist yet.
func Load(path string) (*State, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Scenarios == nil {
		s.Scenarios = map[string]Entry{}
	}
	if s.Policy.QuarantineAfter == 0 {
		s.Policy = DefaultPolicy()
	}
	return &s, nil
}

// Save writes the state atomically: a suite that is interrupted halfway through must not leave a
// half-written history that the next run reads as fact.
func (s *State) Save(path string) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Run is what happened to one scenario in one execution of the suite.
type Run struct {
	// Attempts is how many times it was tried, 1 when it passed first time.
	Attempts int
	// Passed is the outcome of the last attempt.
	Passed bool
	// Error is the failure message, empty when it passed.
	Error string
}

// Flaked reports whether this run failed and then passed. That is the definition the flake rate in the
// README uses: not "it failed", but "it failed and then did not".
func (r Run) Flaked() bool { return r.Passed && r.Attempts > 1 }

// Apply folds one run into the state and returns the entry as it now stands. The returned entry is what
// the report shows: the policy has already had its say.
func (s *State) Apply(id string, run Run, now time.Time) Entry {
	entry := s.Scenarios[id]
	entry.Runs++
	entry.LastRun = now.UTC()
	entry.LastError = run.Error

	switch {
	case run.Passed:
		entry.ConsecutivePasses++
		entry.ConsecutiveFailures = 0
		if run.Flaked() {
			entry.Flakes++
		}
		if entry.Status == Quarantined && entry.ConsecutivePasses >= s.Policy.UnquarantineAfter {
			entry.Status = Active
			entry.QuarantinedAt = time.Time{}
			entry.QuarantinedReason = ""
		}
	case !run.Passed:
		entry.Fails++
		entry.ConsecutiveFailures++
		entry.ConsecutivePasses = 0
		if entry.Status == Quarantined {
			break
		}
		if entry.ConsecutiveFailures >= s.Policy.QuarantineAfter {
			entry.Status = Quarantined
			entry.QuarantinedAt = now.UTC()
			entry.QuarantinedReason = fmt.Sprintf("%d whole runs failed in a row; last error: %s",
				entry.ConsecutiveFailures, firstLine(run.Error))
		}
	}
	if entry.Status == "" {
		entry.Status = Active
	}
	s.Scenarios[id] = entry
	return entry
}

// Begin increments the run counter. It is separate from Apply so that a suite which crashes still
// records that it ran.
func (s *State) Begin() { s.Runs++ }

// Quarantined reports the ids currently quarantined, sorted.
func (s *State) Quarantined() []string {
	var out []string
	for id, entry := range s.Scenarios {
		if entry.Status == Quarantined {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// FlakeRate is the share of runs that failed and then passed, over all scenarios. It is the number the
// README quotes, and it is computed from the same history the policy used - not from a separate tally
// that could disagree with it.
func (s *State) FlakeRate() (flakes, runs int, rate float64) {
	for _, entry := range s.Scenarios {
		flakes += entry.Flakes
		runs += entry.Runs
	}
	if runs == 0 {
		return 0, 0, 0
	}
	return flakes, runs, float64(flakes) / float64(runs) * 100
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
