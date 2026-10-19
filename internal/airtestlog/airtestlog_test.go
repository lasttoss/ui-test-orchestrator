package airtestlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixtures are real: both were written by Airtest 1.4.3's own log writer, and testdata/README.md
// says how. A hand-typed log would prove that this package can read what this package wrote.
func fixed(t *testing.T) []Entry {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "airtest-log-fixed.txt"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	entries, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return entries
}

func TestParseReadsATracebackFromARealRun(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "airtest-log.txt"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	entries, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 7 {
		t.Fatalf("read %d entries, want 7", len(entries))
	}
	tags := []string{"log", "function", "log", "function", "log", "function", "function"}
	for i, want := range tags {
		if entries[i].Tag != want {
			t.Errorf("entry %d tag = %q, want %q", i, entries[i].Tag, want)
		}
	}
	last := entries[len(entries)-1]
	if !strings.Contains(last.Data.Traceback, "balance did not grow by 500") {
		t.Fatalf("the last entry lost its traceback: %q", last.Data.Traceback)
	}
	if last.Data.Name != "test_purchase" || last.Depth != 1 {
		t.Errorf("last entry = %+v, want the depth-1 function that raised", last)
	}
}

func TestParseSkipsBlankLines(t *testing.T) {
	entries, err := Parse(strings.NewReader("\n" + `{"tag":"log","depth":1,"time":1,"data":{"msg":"hi"}}` + "\n\n\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(entries) != 1 || entries[0].Data.Msg != "hi" {
		t.Fatalf("got %+v, want one entry", entries)
	}
}

func TestParseNamesTheLineItCouldNotRead(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "airtest-log-malformed.txt"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	_, err = Parse(f)
	if err == nil {
		t.Fatal("a line that is not JSON was accepted")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("err = %v, want it to name line 2", err)
	}
}

// The default bufio.Scanner buffer is 64 KiB, and a Python traceback with a deep framework stack plus
// a long assertion message can pass it. Truncating the log of a failure would turn a red run into a
// parse error, so the buffer is raised and this test holds it there.
func TestParseReadsALinePastTheDefaultScannerLimit(t *testing.T) {
	long := `{"tag":"function","depth":1,"time":1792380914.0,"data":{"name":"deep_stack","traceback":"` +
		strings.Repeat("frame\\n", 120_000) + `AssertionError: still here"}}`
	entries, err := Parse(strings.NewReader(long + "\n"))
	if err != nil {
		t.Fatalf("Parse refused a %d byte line: %v", len(long), err)
	}
	if len(entries) != 1 || len(entries[0].Data.Traceback) < 64*1024 {
		t.Fatalf("the traceback came back short: %d bytes", len(entries[0].Data.Traceback))
	}
	if _, err := Parse(strings.NewReader(strings.Repeat("x", maxLineBytes+1))); err == nil {
		t.Fatal("a line past the ceiling should be an error, not silence")
	}
}

func TestAnalyzeGroupsCasesStepsAndDurations(t *testing.T) {
	run := Analyze(fixed(t))

	if len(run.Rows) != 3 {
		t.Fatalf("rows = %d, want 3 (the two functions and the log line before them)", len(run.Rows))
	}
	if len(run.Cases) != 2 {
		t.Fatalf("cases = %d, want 2: %+v", len(run.Cases), run.Cases)
	}
	if run.CasesPassed != 1 || run.CasesFailed != 1 {
		t.Errorf("passed/failed = %d/%d, want 1/1", run.CasesPassed, run.CasesFailed)
	}
	if !run.Failed {
		t.Error("a case with a traceback did not make the run fail")
	}
	if run.AirtestResult {
		t.Error("airtest's own verdict is computed from the last row, which carries a traceback")
	}

	health, purchase := run.Cases[0], run.Cases[1]
	if health.Name != "test_healthcheck" || health.Failed {
		t.Errorf("first case = %+v, want a passing test_healthcheck", health)
	}
	if health.DurationMS != 250 {
		t.Errorf("healthcheck duration = %v, want exactly 250 ms", health.DurationMS)
	}
	// A statement written inside a wrapped function lands before that function's own entry, which
	// Logwrap writes in a finally block. A person reading a failure wants it with its case.
	if len(health.Steps) != 1 || health.Steps[0].Desc != "healthcheck" {
		t.Errorf("the log line that ran inside the case was not attached to it: %+v", health.Steps)
	}

	if purchase.Name != "test_purchase" || !purchase.Failed {
		t.Fatalf("second case = %+v, want a failing test_purchase", purchase)
	}
	if purchase.DurationMS != 1900 {
		t.Errorf("purchase duration = %v, want exactly 1900 ms", purchase.DurationMS)
	}
	if purchase.FailedStep != "buy_pack" {
		t.Errorf("failed step = %q, want buy_pack: the innermost failing step is the one that explains the case",
			purchase.FailedStep)
	}
	if !strings.Contains(purchase.Error, "AssertionError: balance did not grow by 500") {
		t.Errorf("error = %q, want the last line of the traceback", purchase.Error)
	}
	if len(purchase.Steps) != 5 {
		t.Errorf("steps = %d, want 5 (two logs, two nested functions, one touch)", len(purchase.Steps))
	}
	for _, s := range purchase.Steps {
		if s.Name == "buy_pack" && s.DurationMS != 750 {
			t.Errorf("the failing step took %v ms, want exactly 750", s.DurationMS)
		}
	}
	if run.WallClockMS != 2800 {
		t.Errorf("wall clock = %v ms, want 2800: the run starts when the first line was written and ends "+
			"when the last one was", run.WallClockMS)
	}
}

// The two verdicts can differ, and a reader should be shown that instead of being handed one of them.
func TestAnalyzeKeepsAirtestsVerdictApartFromAnyFailure(t *testing.T) {
	entries := []Entry{
		{Tag: "function", Depth: 1, Time: 100, Data: Data{Name: "test_a", Traceback: "Traceback\nValueError: nope\n",
			StartTime: 100, EndTime: 100.5}},
		{Tag: "log", Depth: 1, Time: 101, Data: Data{Msg: "cleanup finished"}},
	}
	run := Analyze(entries)

	if !run.Failed {
		t.Error("a step with a traceback must make the run red here")
	}
	if !run.AirtestResult {
		t.Error("airtest report decides on the last row, and the last row is clean - the difference is the point")
	}
	if run.Disagreement() == "" {
		t.Fatal("the difference was not explained")
	}
	if !strings.Contains(run.Disagreement(), "green") {
		t.Errorf("disagreement = %q, want it to say what airtest report would call this", run.Disagreement())
	}
	if run.Verdict() != "failed" {
		t.Errorf("verdict = %q, want failed", run.Verdict())
	}
}

func TestAnalyzeHandlesLogsWithNoTestCase(t *testing.T) {
	run := Analyze([]Entry{
		{Tag: "log", Depth: 1, Time: 10, Data: Data{Msg: "connecting"}},
		{Tag: "touch", Depth: 1, Time: 11, Data: Data{Pos: []float64{1, 2}, Desc: "tap"}},
	})
	if len(run.Cases) != 0 || len(run.Loose) != 2 {
		t.Fatalf("cases=%d loose=%d, want 0 and 2", len(run.Cases), len(run.Loose))
	}
	if run.Failed || run.Verdict() != "passed" {
		t.Errorf("verdict = %q failed=%v, want passed: nothing here failed", run.Verdict(), run.Failed)
	}

	empty := Analyze(nil)
	if len(empty.Rows) != 0 || empty.Verdict() != "empty" || !empty.AirtestResult {
		t.Errorf("empty log = %+v, want the empty verdict and airtest's default result", empty)
	}
	if empty.Disagreement() != "" {
		t.Error("an empty log agrees with airtest, so there is nothing to explain")
	}
}

func TestReadFileSetsThePath(t *testing.T) {
	run, err := ReadFile(filepath.Join("testdata", "airtest-log-fixed.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.HasSuffix(run.Path, "airtest-log-fixed.txt") {
		t.Errorf("path = %q, want the file it read", run.Path)
	}
	if _, err := ReadFile(filepath.Join("testdata", "does-not-exist.txt")); err == nil {
		t.Error("a missing log should be an error")
	}
}

func TestReadFileOnAFileThatIsNotALog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-log.txt")
	if err := os.WriteFile(path, []byte("plain text, no JSON\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil {
		t.Fatal("a file that is not a log was accepted")
	}
}

func TestEntryTimePrefersStartTime(t *testing.T) {
	e := Entry{Tag: "function", Depth: 1, Time: 110, Data: Data{StartTime: 100, EndTime: 109}}
	if got, want := entryTime(e), time.Unix(100, 0); !got.Equal(want) {
		t.Errorf("entryTime = %v, want %v", got, want)
	}
	if got, want := lineTime(e), time.Unix(110, 0); !got.Equal(want) {
		t.Errorf("lineTime = %v, want %v", got, want)
	}
	if !lineTime(Entry{}).IsZero() || !entryTime(Entry{}).IsZero() {
		t.Error("an entry with no time should not invent one")
	}
	long := fmt.Sprintf(`{"tag":"log","depth":1,"time":1,"data":{"msg":%q}}`, strings.Repeat("a", 10))
	var e2 Entry
	if err := json.Unmarshal([]byte(long), &e2); err != nil {
		t.Fatalf("a plain entry stopped parsing: %v", err)
	}
}
