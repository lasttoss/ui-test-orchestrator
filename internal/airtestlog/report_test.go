package airtestlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixedReport(t *testing.T, run Run) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "report-airtest")
	at := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
	if err := WriteReportAt(dir, run, at); err != nil {
		t.Fatalf("WriteReportAt: %v", err)
	}
	return dir
}

func TestWriteReportWritesWhatAPersonAndABuildSystemNeed(t *testing.T) {
	run := Analyze(fixed(t))
	run.Path = filepath.Join("testdata", "airtest-log-fixed.txt")
	dir := writeFixedReport(t, run)

	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	html := string(page)
	for _, want := range []string{
		"test_purchase", // the case that failed
		"buy_pack",      // the step inside it that failed
		"AssertionError: balance did not grow by 500", // the line a person needs
		"250 ms",    // a duration, from the frozen clock
		"1.9 s",     // the same durations above a second
		"traceback", // the full text is available, not just the summary
		"test_healthcheck",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the report does not contain %q", want)
		}
	}
	if strings.Contains(html, "<script") {
		t.Error("the report carries a script: it must render in a pipeline with no network and no JS")
	}
	if strings.Contains(html, "http://") || strings.Contains(html, "https://") {
		t.Error("the report refers to something outside itself")
	}

	raw, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatalf("summary.json: %v", err)
	}
	var got Summary
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("summary.json is not JSON: %v", err)
	}
	if !got.Failed || got.CasesFailed != 1 || got.CasesPassed != 1 || got.Entries != 8 {
		t.Errorf("summary = %+v, want one failing case out of eight entries", got)
	}
	if got.Cases[1].FailedStep != "buy_pack" {
		t.Errorf("the summary lost the failing step: %+v", got.Cases[1])
	}
	if want := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC); !got.Generated.Equal(want) {
		t.Errorf("generated = %v, want the clock it was given", got.Generated)
	}
}

// The log is somebody else's file, and a test name can be anything. html/template escapes text; this
// test is here so that a later change to the template cannot quietly turn that off.
func TestWriteReportEscapesWhatTheLogSays(t *testing.T) {
	run := Analyze([]Entry{{
		Tag: TagFunction, Depth: 1, Time: 1792380914, Data: Data{
			Name:      "<script>alert(1)</script>",
			Traceback: "Traceback\nValueError: a < b & c\n",
			StartTime: 1792380914, EndTime: 1792380915,
		},
	}})
	dir := writeFixedReport(t, run)
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("a name from the log was rendered as markup")
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the name was not escaped, it was dropped")
	}
	if !strings.Contains(html, "a &lt; b &amp; c") {
		t.Error("the traceback was not escaped")
	}
}

func TestWriteReportSaysWhenTheTwoVerdictsDiffer(t *testing.T) {
	run := Analyze([]Entry{
		{Tag: TagFunction, Depth: 1, Time: 100, Data: Data{Name: "test_a", StartTime: 100, EndTime: 100.4,
			Traceback: "Traceback\nValueError: nope\n"}},
		{Tag: "log", Depth: 1, Time: 101, Data: Data{Msg: "cleanup ran"}},
	})
	page, err := os.ReadFile(filepath.Join(writeFixedReport(t, run), "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "airtest report would call this run green") {
		t.Error("the page did not explain that airtest's own verdict differs")
	}
}

func TestWriteReportOnALogWithNothingInIt(t *testing.T) {
	dir := writeFixedReport(t, Analyze(nil))
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	if !strings.Contains(html, "verdict empty") {
		t.Error("an empty log should say so, not look like a pass")
	}
	if strings.Contains(html, "airtest report would call this run red") {
		t.Error("an empty log is not a red run")
	}
}

func TestWriteReportMakesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	if err := WriteReport(dir, Analyze(fixed(t))); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "summary.json")); err != nil {
		t.Errorf("summary.json: %v", err)
	}
}
