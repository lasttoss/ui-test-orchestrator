package airtestlog

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Summary is what a build system reads: the run, plus when it was read.
type Summary struct {
	Generated time.Time `json:"generated"`
	Run
}

// WriteReport writes the two files a device run owes the rest of the pipeline: index.html, which a
// person opens, and summary.json, which a build system reads. The HTML carries no script and no
// external asset, because a report that needs a network to render is a report that fails exactly
// where it is needed, in a pipeline that has none.
func WriteReport(dir string, run Run) error {
	return WriteReportAt(dir, run, time.Now().UTC())
}

// WriteReportAt is WriteReport with the clock passed in, so a test can assert the file it wrote.
func WriteReportAt(dir string, run Run, generated time.Time) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	page, err := renderPage(run, generated)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	body, err := json.MarshalIndent(Summary{Generated: generated, Run: run}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "summary.json"), append(body, '\n'), 0o644)
}

// FormatDurationMS prints a duration the way a person reads one, and the CLI and the page use the same
// function so that they cannot disagree about the same number.
//
// Why this is not just a format verb: the durations here come from two float seconds subtracted from
// each other in somebody else's log, and 1792380916.9 - 1792380915.0 is 1.9000000953674316.
func FormatDurationMS(v float64) string {
	switch {
	case v >= 1000:
		return strconv.FormatFloat(v/1000, 'f', -1, 64) + " s"
	case v > 0:
		return strconv.FormatFloat(v, 'f', -1, 64) + " ms"
	}
	return "-"
}

// Verdict is the one-word answer, which is what a reader looks at first and what a caller prints.
func (r Run) Verdict() string {
	switch {
	case r.Entries == 0:
		return "empty"
	case r.Failed:
		return "failed"
	default:
		return "passed"
	}
}

// Disagreement is the sentence a reader needs when this package and `airtest report` would say
// different things about the same log, and empty when they agree. Printing it beats printing a
// verdict and letting somebody discover the difference later.
func (r Run) Disagreement() string {
	if r.AirtestResult == !r.Failed {
		return ""
	}
	if r.Failed {
		return "airtest report would call this run green: it decides on the last row alone, and the last row has no traceback"
	}
	return "airtest report would call this run red: it decides on the last row alone"
}

var page = template.Must(template.New("airtestlog").Funcs(template.FuncMap{
	"ms": FormatDurationMS,
	"clock": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.UTC().Format("2006-01-02 15:04:05") + " UTC"
	},
	"detail": func(s string) string { return strings.TrimRight(s, "\r\n") },
}).Parse(pageHTML))

func renderPage(run Run, generated time.Time) (string, error) {
	var sb strings.Builder
	if err := page.Execute(&sb, map[string]any{"run": run, "generated": generated}); err != nil {
		return "", err
	}
	return sb.String(), nil
}

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Airtest log — {{ .run.Verdict }}</title>
<style>
  :root { --paper:#f5f5f5; --ink:#2d3142; --muted:#4f5d75; --soft:#7a8399; --line:#c9ccd1;
          --bad:#8c2f1f; --good:#2f6b3f; --tint:#fdf4ef; }
  * { box-sizing:border-box; }
  body { margin:0; padding:32px 20px 64px; background:var(--paper); color:var(--ink);
         font:15px/1.5 'Geist', system-ui, -apple-system, 'Segoe UI', sans-serif; }
  main { max-width:1000px; margin:0 auto; }
  h1 { font-size:20px; margin:0 0 4px; }
  h2 { font-size:16px; margin:36px 0 8px; }
  .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:1px;
          background:var(--line); border:1px solid var(--line); border-radius:10px; overflow:hidden;
          margin:20px 0 0; }
  .grid div { background:#fff; padding:10px 14px; }
  .grid dt { color:var(--soft); font-size:11px; letter-spacing:.08em; text-transform:uppercase; }
  .grid dd { margin:2px 0 0; font-size:18px; font-variant-numeric:tabular-nums; }
  .verdict { display:inline-block; padding:2px 10px; border-radius:999px; font-size:13px; font-weight:600; }
  .verdict.failed { background:var(--tint); color:var(--bad); border:1px solid var(--bad); }
  .verdict.passed { background:#f1f7f2; color:var(--good); border:1px solid var(--good); }
  .verdict.empty  { background:#fff; color:var(--muted); border:1px solid var(--line); }
  .note { margin:14px 0 0; padding:10px 14px; background:#fff; border:1px solid var(--line);
          border-left:3px solid var(--muted); border-radius:8px; color:var(--muted); font-size:13px; }
  .case { margin:18px 0 0; background:#fff; border:1px solid var(--line); border-radius:10px;
          overflow:hidden; }
  .case.failed { border-color:var(--bad); }
  .case header { display:flex; flex-wrap:wrap; gap:10px; align-items:baseline;
                 padding:12px 14px; border-bottom:1px solid var(--line); }
  .case.failed header { background:var(--tint); }
  .case h3 { margin:0; font-size:15px; }
  .meta { color:var(--soft); font-size:12px; font-variant-numeric:tabular-nums; }
  .own { padding:10px 14px 12px; border-bottom:1px solid var(--line); }
  .pill { font-size:11px; font-weight:600; letter-spacing:.06em; text-transform:uppercase; }
  .pill.failed { color:var(--bad); } .pill.passed { color:var(--good); }
  table { width:100%; border-collapse:collapse; }
  th, td { text-align:left; padding:7px 14px; border-bottom:1px solid #eceef1; vertical-align:top;
           font-size:13.5px; }
  th { color:var(--soft); font-size:11px; letter-spacing:.08em; text-transform:uppercase;
       font-weight:600; }
  td.num { text-align:right; font-variant-numeric:tabular-nums; color:var(--muted); white-space:nowrap; }
  tr.failed td { background:var(--tint); }
  code, .mono { font-family:'Geist Mono', ui-monospace, SFMono-Regular, monospace; font-size:12.5px; }
  .tag { color:var(--muted); }
  .err { color:var(--bad); }
  details { margin:6px 0 0; }
  summary { cursor:pointer; color:var(--muted); font-size:12.5px; }
  pre { margin:8px 0 0; padding:10px 12px; background:#f7f8f9; border:1px solid #eceef1;
        border-radius:8px; overflow:auto; font-size:12px; line-height:1.45; }
  footer { margin:28px 0 0; color:var(--soft); font-size:12px; }
</style>
</head>
<body>
<main>
  <h1>Airtest log <span class="verdict {{ .run.Verdict }}">{{ .run.Verdict }}</span></h1>
  <p class="meta mono">{{ .run.Path }}</p>

  <div class="grid">
    <div><dt>Cases</dt><dd>{{ len .run.Cases }}</dd></div>
    <div><dt>Passed</dt><dd>{{ .run.CasesPassed }}</dd></div>
    <div><dt>Failed</dt><dd>{{ .run.CasesFailed }}</dd></div>
    <div><dt>Rows</dt><dd>{{ len .run.Rows }}</dd></div>
    <div><dt>Entries</dt><dd>{{ .run.Entries }}</dd></div>
    <div><dt>Wall clock</dt><dd>{{ ms .run.WallClockMS }}</dd></div>
  </div>

  {{ with .run.Disagreement }}<p class="note">{{ . }}</p>{{ end }}
  {{ if not .run.AirtestResult }}<p class="note">airtest report would call this run red too: the last row carries a traceback.</p>{{ end }}

  {{ range .run.Cases }}
  <section class="case {{ if .Failed }}failed{{ end }}">
    <header>
      <h3>{{ .Name }}</h3>
      <span class="pill {{ if .Failed }}failed{{ else }}passed{{ end }}">{{ if .Failed }}failed{{ else }}passed{{ end }}</span>
      <span class="meta">{{ ms .DurationMS }} · {{ clock .At }}</span>
      {{ if .FailedStep }}<span class="meta">first failure: <code>{{ .FailedStep }}</code></span>{{ end }}
    </header>
    {{ if and .Failed .Traceback (not .FailedStep) }}
    <div class="own">
      <div class="err mono">{{ .Error }}</div>
      <details><summary>traceback</summary><pre>{{ detail .Traceback }}</pre></details>
    </div>
    {{ end }}
    {{ if .Steps }}
    <table>
      <thead><tr><th>step</th><th>what</th><th>tag</th><th class="num">took</th></tr></thead>
      <tbody>
      {{ range .Steps }}
        <tr class="{{ if .Failed }}failed{{ end }}">
          <td><code>{{ .Name }}</code></td>
          <td>{{ if .Desc }}{{ .Desc }}{{ else }}<span class="meta">—</span>{{ end }}
            {{ if .Failed }}<div class="err mono">{{ .Error }}</div>
              <details><summary>traceback</summary><pre>{{ detail .Traceback }}</pre></details>{{ end }}
          </td>
          <td class="tag mono">{{ .Tag }}</td>
          <td class="num">{{ ms .DurationMS }}</td>
        </tr>
      {{ end }}
      </tbody>
    </table>
    {{ else }}<p class="meta" style="padding:10px 14px">No steps recorded inside this case.</p>{{ end }}
  </section>
  {{ end }}

  {{ if .run.Loose }}
  <h2>Rows that belong to no case</h2>
  <table>
    <thead><tr><th>step</th><th>what</th><th>tag</th><th class="num">took</th></tr></thead>
    <tbody>
    {{ range .run.Loose }}
      <tr class="{{ if .Failed }}failed{{ end }}">
        <td><code>{{ .Name }}</code></td>
        <td>{{ .Desc }}{{ if .Failed }}<div class="err mono">{{ .Error }}</div>{{ end }}</td>
        <td class="tag mono">{{ .Tag }}</td>
        <td class="num">{{ ms .DurationMS }}</td>
      </tr>
    {{ end }}
    </tbody>
  </table>
  {{ end }}

  <footer>Read {{ clock .generated }} · the rules are Airtest's own: a row is a failure when its data carries a traceback.</footer>
</main>
</body>
</html>
`
