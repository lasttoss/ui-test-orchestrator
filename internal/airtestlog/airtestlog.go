// Package airtestlog reads the log a device-driven Airtest run leaves behind.
//
// Airtest writes log.txt as one JSON object per line:
//
//	{"tag": "function", "depth": 2, "time": 1792380916.75, "data": {"name": "buy_pack", ...}}
//
// and it builds its own report by walking those lines in order (airtest/report/report.py): a line at
// depth 1 becomes a row, the deeper lines that came before it become that row's children, and a step
// whose data carries a traceback is a failure - that same traceback is how `airtest report` decides a
// run failed, by looking at the last row it translated. This package reads the same file and keeps
// those rules, then adds the grouping a person needs on a red run: which test case failed, which step
// inside it, and how long each one took.
//
// Why read somebody else's log at all: a device run is evidence, and evidence that only exists as an
// HTML file attached to a pipeline is not a gate. Reading the log turns an Airtest run into the same
// red or green a browser run gets here, without asking Airtest to write anything differently.
package airtestlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

// TagFunction is the tag a @logwrap'd function gets. It is the only tag that carries a name, both
// timestamps and - when the function raised - the traceback instead of a return value, which is why
// the grouping below hangs off it.
const TagFunction = "function"

// maxLineBytes is generous on purpose. A Python traceback is not short, and bufio.Scanner's 64 KiB
// default truncates one into a parse error: failing to read the log of a failure is the one moment a
// reader must not have.
const maxLineBytes = 4 << 20

// Data is the payload of one entry. Airtest's tags do not share a schema - a function carries a name
// and both timestamps, a log carries a message, an assertion carries the message it failed on - so
// this is the union of what the writer emits, with every field optional. Unknown keys are ignored on
// purpose: a later Airtest adding a key must not break a reader that predates it.
type Data struct {
	Name      string          `json:"name,omitempty"`
	Msg       string          `json:"msg,omitempty"`
	Desc      string          `json:"desc,omitempty"`
	Traceback string          `json:"traceback,omitempty"`
	StartTime float64         `json:"start_time,omitempty"`
	EndTime   float64         `json:"end_time,omitempty"`
	Pos       []float64       `json:"pos,omitempty"`
	CallArgs  json.RawMessage `json:"call_args,omitempty"`
	Ret       json.RawMessage `json:"ret,omitempty"`
}

// Entry is one line of log.txt.
type Entry struct {
	Tag   string  `json:"tag"`
	Depth int     `json:"depth"`
	Time  float64 `json:"time"`
	Data  Data    `json:"data"`
}

// Row is one thing that happened, with the deeper entries that preceded it as children. It mirrors
// what Airtest's own report calls a step.
type Row struct {
	Tag        string    `json:"tag"`
	Name       string    `json:"name"`
	Desc       string    `json:"desc,omitempty"`
	Depth      int       `json:"depth"`
	At         time.Time `json:"at"`
	DurationMS float64   `json:"duration_ms,omitempty"`
	Failed     bool      `json:"failed"`
	Error      string    `json:"error,omitempty"`
	Traceback  string    `json:"traceback,omitempty"`
	Pos        []float64 `json:"pos,omitempty"`
	Children   []Row     `json:"children,omitempty"`
}

// Case is a test case: a @logwrap'd function, which is how a test case is written in Airtest.
type Case struct {
	Name       string    `json:"name"`
	At         time.Time `json:"at"`
	DurationMS float64   `json:"duration_ms,omitempty"`
	Failed     bool      `json:"failed"`
	FailedStep string    `json:"failed_step,omitempty"`
	Error      string    `json:"error,omitempty"`
	Traceback  string    `json:"traceback,omitempty"`
	Steps      []Row     `json:"steps,omitempty"`
}

// Run is one log file, understood.
type Run struct {
	Path        string    `json:"log"`
	Entries     int       `json:"entries"`
	Rows        []Row     `json:"rows"`
	Cases       []Case    `json:"cases"`
	Loose       []Row     `json:"loose,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	EndedAt     time.Time `json:"ended_at"`
	WallClockMS float64   `json:"wall_clock_ms,omitempty"`
	Failed      bool      `json:"failed"`
	CasesPassed int       `json:"cases_passed"`
	CasesFailed int       `json:"cases_failed"`
	// AirtestResult is what `airtest report` would say about this log: report.py sets its test_result
	// to False when the last row it translated carries a traceback, and leaves it True otherwise. It
	// is kept separate from Failed because a run that failed and then logged something else is red
	// here and green there - and a reader should be able to see that, not be told which one is right.
	AirtestResult bool `json:"airtest_result"`
}

// Parse reads log.txt. Blank lines are skipped: a log that ends in a newline is not a malformed log.
func Parse(r io.Reader) ([]Entry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var entries []Entry
	for line := 1; scanner.Scan(); line++ {
		raw := strings.TrimPrefix(scanner.Text(), "\ufeff")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, fmt.Errorf("airtestlog: line %d is not a log entry: %w", line, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("airtestlog: reading the log: %w", err)
	}
	return entries, nil
}

// ReadFile parses and analyses one log file.
func ReadFile(path string) (Run, error) {
	f, err := os.Open(path)
	if err != nil {
		return Run{}, err
	}
	defer func() { _ = f.Close() }()
	entries, err := Parse(f)
	if err != nil {
		return Run{}, err
	}
	run := Analyze(entries)
	run.Path = path
	return run, nil
}

// Analyze applies Airtest's own reading of the log, then groups the rows into test cases.
func Analyze(entries []Entry) Run {
	run := Run{Entries: len(entries)}

	// Rows and children, in Airtest's order: the deeper lines that came before a row belong to it,
	// because Logwrap writes a wrapped function's own entry in a finally block, after everything that
	// happened inside it is already on the file. Airtest prepends its children, so its own report
	// shows them bottom-up; they keep the order the run produced them here, which is the order a
	// person reads a failure in.
	var children []Row
	for _, e := range entries {
		// Airtest's own report takes the run's start from the first entry's start_time when it has one,
		// and its end from the time the last line was written. Both are kept that way here: a function
		// that started at T and was written at T+1.9 must not make the run look shorter than it was.
		if run.StartedAt.IsZero() {
			run.StartedAt = entryTime(e)
		}
		if at := lineTime(e); !at.IsZero() {
			run.EndedAt = at
		}
		row := newRow(e)
		if e.Depth > 1 {
			children = append(children, row)
			continue
		}
		row.Children = children
		children = nil
		run.Rows = append(run.Rows, row)
	}

	// A depth-1 row that is a wrapped function is a test case. The rows before it that are not one are
	// the steps it logged while it ran: a statement inside a wrapped function is written when it
	// happens, and the function's own entry only when it returns, so they land ahead of their case.
	var loose []Row
	for _, row := range run.Rows {
		if row.Tag != TagFunction {
			loose = append(loose, row)
			continue
		}
		c := Case{
			Name: row.Name, At: row.At, DurationMS: row.DurationMS,
			Failed: row.Failed, Error: row.Error, Traceback: row.Traceback,
		}
		c.Steps = append(c.Steps, loose...)
		loose = nil
		c.Steps = append(c.Steps, row.Children...)
		if step, ok := firstFailure(c.Steps); ok {
			c.Failed = true
			if c.Error == "" {
				c.Error = step.Error
			}
			if c.Traceback == "" {
				c.Traceback = step.Traceback
			}
			c.FailedStep = step.Name
		}
		if c.Failed {
			run.CasesFailed++
		} else {
			run.CasesPassed++
		}
		run.Cases = append(run.Cases, c)
	}
	run.Loose = loose

	for _, row := range run.Rows {
		if row.Failed || rowHasFailure(row) {
			run.Failed = true
			break
		}
	}
	// Airtest's own verdict, for comparison: the last row decides.
	run.AirtestResult = len(run.Rows) == 0 || !run.Rows[len(run.Rows)-1].Failed
	if run.EndedAt.After(run.StartedAt) {
		run.WallClockMS = round3(float64(run.EndedAt.Sub(run.StartedAt)) / float64(time.Millisecond))
	}
	return run
}

// entryTime prefers the start timestamp a function carries, the way Airtest's own report does, and
// falls back to the time the line was written.
func entryTime(e Entry) time.Time {
	if e.Data.StartTime > 0 {
		return time.Unix(0, int64(e.Data.StartTime*float64(time.Second)))
	}
	return lineTime(e)
}

// lineTime is when the line was written, which for a function is when it finished.
func lineTime(e Entry) time.Time {
	if e.Time == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(e.Time*float64(time.Second)))
}

func newRow(e Entry) Row {
	row := Row{Tag: e.Tag, Name: e.Data.Name, Desc: e.Data.Desc, Depth: e.Depth, Pos: e.Data.Pos}
	if row.Name == "" {
		row.Name = e.Tag
	}
	if row.Desc == "" {
		row.Desc = e.Data.Msg
	}
	if at := entryTime(e); !at.IsZero() {
		row.At = at
	}
	if e.Data.StartTime > 0 && e.Data.EndTime > e.Data.StartTime {
		row.DurationMS = round3((e.Data.EndTime - e.Data.StartTime) * 1000)
	}
	if strings.TrimSpace(e.Data.Traceback) != "" {
		row.Failed = true
		row.Traceback = e.Data.Traceback
		// The last line of a Python traceback is the exception, which is the line a person reads and
		// the only one worth putting in a table cell.
		row.Error = lastLine(e.Data.Traceback)
	}
	return row
}

// round3 takes the binary tail off a subtraction. Airtest logs two float seconds per function, and
// 1792380916.9 - 1792380915.0 is 1.9000000953674316 in binary floating point: the noise is in the
// float, not in the measurement, and a report that prints it looks broken to the person reading it.
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func lastLine(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\r\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

// firstFailure walks the steps of a case and returns the first one that failed, deepest first: the
// innermost failure is the one that explains the case.
func firstFailure(steps []Row) (Row, bool) {
	for _, s := range steps {
		if inner, ok := firstFailure(s.Children); ok {
			return inner, true
		}
		if s.Failed {
			return s, true
		}
	}
	return Row{}, false
}

func rowHasFailure(row Row) bool {
	_, ok := firstFailure(row.Children)
	return ok
}
