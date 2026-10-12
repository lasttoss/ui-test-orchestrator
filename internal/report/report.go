// Package report writes the two things a person needs afterwards: a file they can open, and a file a
// build system can read.
package report

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/flake"
	"github.com/lasttoss/ui-test-orchestrator/internal/runner"
)

// Data is everything the report shows.
type Data struct {
	Generated   time.Time
	BaseURL     string
	Summary     runner.Summary
	State       *flake.State
	Policy      flake.Policy
	Browser     string
	Scenarios   int
	FlakeRate   float64
	Flakes      int
	ScenarioRun int
}

type row struct {
	ID         string
	Name       string
	Status     string
	StatusNote string
	Attempts   int
	DurationMS float64
	FailedStep int
	Error      string
	ImageNote  string
	Images     []string // data URIs
	Tags       []string
	Steps      []runner.StepResult
}

// Write renders report/index.html and report/summary.json.
func Write(dir string, data Data) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rows := buildRows(data)
	html := renderHTML(rows, data)
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o644); err != nil {
		return err
	}
	jsonRaw, err := json.MarshalIndent(struct {
		runner.Summary
		Generated    time.Time    `json:"generated"`
		BaseURL      string       `json:"base_url"`
		Browser      string       `json:"browser"`
		FlakeRate    float64      `json:"flake_rate_percent"`
		Flakes       int          `json:"flakes"`
		ScenarioRuns int          `json:"scenario_runs"`
		Quarantined  []string     `json:"quarantined"`
		Policy       flake.Policy `json:"policy"`
	}{Summary: data.Summary, Generated: data.Generated.UTC(), BaseURL: data.BaseURL, Browser: data.Browser,
		FlakeRate: data.FlakeRate, Flakes: data.Flakes, ScenarioRuns: data.ScenarioRun,
		Quarantined: data.State.Quarantined(), Policy: data.Policy}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.json"), append(jsonRaw, '\n'), 0o644)
}

func buildRows(data Data) []row {
	rows := make([]row, 0, len(data.Summary.Outcomes))
	for _, o := range data.Summary.Outcomes {
		entry := data.State.Scenarios[o.ID]
		status, note := "pass", ""
		switch {
		case !o.Passed && entry.Status == flake.Quarantined:
			status, note = "quarantined", "failing, and quarantined: it does not fail the build"
		case !o.Passed:
			status, note = "fail", ""
		case o.Flaked:
			status, note = "flake", fmt.Sprintf("passed on attempt %d", o.Attempts)
		}
		r := row{
			ID: o.ID, Name: o.Name, Status: status, StatusNote: note, Attempts: o.Attempts,
			DurationMS: o.DurationMS, FailedStep: o.FailedStep, Error: o.Error,
			ImageNote: o.ImageNote, Tags: o.Tags, Steps: o.Steps,
		}
		for _, path := range o.Embeds {
			if uri, err := dataURI(path); err == nil {
				r.Images = append(r.Images, uri)
			}
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return statusRank(rows[i].Status) < statusRank(rows[j].Status) })
	return rows
}

func statusRank(status string) int {
	switch status {
	case "fail":
		return 0
	case "quarantined":
		return 1
	case "flake":
		return 2
	default:
		return 3
	}
}

// dataURI inlines the screenshot. The report is one file you can attach to a message, and a report that
// breaks when it is moved is a report nobody sends.
func dataURI(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), nil
}

var page = template.Must(template.New("report").Funcs(template.FuncMap{
	"ms": func(v float64) string {
		if v >= 1000 {
			return fmt.Sprintf("%.2f s", v/1000)
		}
		return fmt.Sprintf("%.0f ms", v)
	},
	"lower": strings.ToLower,
	// safeURL exists because html/template refuses a data: URI in src and rewrites it to #ZgotmplZ - so
	// the report rendered, listed every failure, and silently showed no screenshots at all. The URLs here
	// are built by this package from a base64 of a PNG it wrote itself, never from a scenario.
	"safeURL": func(u string) template.URL { return template.URL(u) },
}).Parse(reportHTML))

func renderHTML(rows []row, data Data) string {
	var sb strings.Builder
	if err := page.Execute(&sb, map[string]any{
		"Rows": rows, "Data": data, "Quarantined": data.State.Quarantined(),
		"Passed": data.Summary.Passed, "Failed": data.Summary.Failed, "Flaked": data.Summary.Flaked,
		"Total": len(rows), "Policy": data.Policy, "FlakeRate": data.FlakeRate,
	}); err != nil {
		return "<!doctype html><p>report rendering failed: " + template.HTMLEscapeString(err.Error())
	}
	return sb.String()
}

const reportHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>UI test report</title>
<style>
  :root { color-scheme: light; }
  body { font: 14px/1.5 system-ui, sans-serif; margin: 0; background: #f6f7f9; color: #1d2129; }
  header { background: #14213d; color: #fff; padding: 18px 24px; }
  header h1 { margin: 0 0 6px; font-size: 20px; }
  header a { color: #9dc0ff; }
  .cards { display: flex; gap: 12px; padding: 18px 24px; flex-wrap: wrap; }
  .card { background: #fff; border: 1px solid #d5d9e0; border-radius: 8px; padding: 12px 16px; min-width: 120px; }
  .card b { display: block; font-size: 20px; }
  .card span { color: #5b6472; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; }
  main { padding: 0 24px 40px; }
  table { border-collapse: collapse; width: 100%; background: #fff; border: 1px solid #d5d9e0; border-radius: 8px; overflow: hidden; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #eceff3; vertical-align: top; }
  th { background: #eef1f6; font-size: 12px; text-transform: uppercase; letter-spacing: .04em; color: #5b6472; }
  .status { font-weight: 700; }
  .pass { color: #0f7b3f; } .fail { color: #b42318; } .flake { color: #b54708; } .quarantined { color: #6b7280; }
  code, pre { font: 12px/1.45 ui-monospace, monospace; }
  pre { background: #f2f4f8; border-radius: 6px; padding: 8px; margin: 6px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; }
  img { max-width: min(460px, 100%); border: 1px solid #d5d9e0; border-radius: 6px; display: block; margin-top: 6px; }
  .note { color: #5b6472; font-size: 12px; }
  .tags span { display: inline-block; background: #eef1f6; border-radius: 999px; padding: 1px 8px; font-size: 12px; margin-right: 4px; }
  h2 { font-size: 16px; margin: 26px 0 8px; }
  header a { overflow-wrap: anywhere; }
  /* The table scrolls inside its own wrapper; the page never scrolls sideways. */
  .table-wrap { overflow-x: auto; -webkit-overflow-scrolling: touch; }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px;
             overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0; }
  /* A five-column table does not fit a phone, so each row becomes a labelled card. */
  @media (max-width: 600px) {
    header, main, .cards { padding-left: 12px; padding-right: 12px; }
    .cards { gap: 8px; }
    .card { flex: 1 1 44%; min-width: 0; }
    header a { display: inline-flex; min-height: 44px; align-items: center; }
    .table-wrap { border: 1px solid #d5d9e0; border-radius: 8px; background: #fff; padding: 0 12px; }
    .table-wrap table { border: 0; background: none; min-width: 0; }
    .table-wrap thead { display: none; }
    .table-wrap tbody tr { display: grid; padding: 10px 0; border-bottom: 1px solid #eceff3; }
    .table-wrap tbody tr:last-child { border-bottom: 0; }
    .table-wrap td { display: grid; grid-template-columns: 84px minmax(0, 1fr); gap: 8px;
                     padding: 2px 0; border: 0; }
    .table-wrap td::before { content: attr(data-label); color: #5b6472; font-size: 12px;
                             text-transform: uppercase; letter-spacing: .04em; }
  }
</style></head>
<body>
<header>
  <h1>UI test report</h1>
  <div class="note">{{.Total}} scenarios · wall clock {{ms .Data.Summary.WallClockMS}} · {{.Data.Summary.Parallel}} in parallel · {{.Data.Browser}} · base URL <a href="{{.Data.BaseURL}}">{{.Data.BaseURL}}</a></div>
  <div class="note">generated {{.Data.Generated.Format "2006-01-02 15:04 MST"}}</div>
</header>
<div class="cards">
  <div class="card"><span>passed</span><b class="pass">{{.Passed}}</b></div>
  <div class="card"><span>failed</span><b class="fail">{{.Failed}}</b></div>
  <div class="card"><span>flakes</span><b class="flake">{{.Flaked}}</b></div>
  <div class="card"><span>quarantined</span><b class="quarantined">{{len .Quarantined}}</b></div>
  <div class="card"><span>flake rate</span><b>{{printf "%.1f%%" .FlakeRate}}</b></div>
  <div class="card"><span>policy</span><b>{{.Policy.QuarantineAfter}} / {{.Policy.UnquarantineAfter}}</b><span class="note">fail runs to quarantine / pass runs to release</span></div>
</div>
<main>
{{if .Quarantined}}<h2>Quarantined</h2>
<p class="note">These run, they are reported, and their failures do not fail the build. They come back on their own after {{.Policy.UnquarantineAfter}} runs in a row pass.</p>
<ul>{{range .Quarantined}}<li><code>{{.}}</code></li>{{end}}</ul>{{end}}
<h2>Scenarios</h2>
<div class="table-wrap"><table>
<caption class="sr-only">Scenario results</caption>
<thead><tr><th scope="col">Scenario</th><th scope="col">Status</th><th scope="col">Attempts</th><th scope="col">Duration</th><th scope="col">Detail</th></tr></thead><tbody>
{{range .Rows}}<tr>
  <td data-label="Scenario"><code>{{.ID}}</code><div class="note">{{.Name}}</div>{{if .Tags}}<div class="tags">{{range .Tags}}<span>{{.}}</span>{{end}}</div>{{end}}</td>
  <td data-label="Status" class="status {{.Status}}">{{.Status}}</td>
  <td data-label="Attempts">{{.Attempts}}</td>
  <td data-label="Duration">{{ms .DurationMS}}</td>
  <td data-label="Detail">
    {{if .StatusNote}}<div class="note">{{.StatusNote}}</div>{{end}}
    {{if .FailedStep}}<div class="note">failed at step {{.FailedStep}}</div>{{end}}
    {{if .Error}}<pre>{{.Error}}</pre>{{end}}
    {{if .ImageNote}}<div class="note">{{.ImageNote}}</div>{{end}}
    {{range .Images}}<img alt="screenshot of the failure" src="{{. | safeURL}}">{{end}}
  </td>
</tr>{{end}}
</tbody></table></div>
</main></body></html>`
