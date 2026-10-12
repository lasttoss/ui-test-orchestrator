// Package app serves the application the scenarios run against.
//
// It is deliberately small, and deliberately able to fail on request. The point of this repository is
// not the page; it is what happens to a test suite when the page breaks in a way one kind of assertion
// notices and another does not.
package app

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// Fault selects how the page misbehaves. The zero value is a working page, so a run that forgets to set
// this tests the thing it thinks it is testing.
type Fault string

const (
	FaultNone Fault = ""
	// FaultVisual changes a status badge from green to red and leaves every word on the page alone.
	// Text assertions pass; a screenshot assertion does not. This is the regression the README measures.
	FaultVisual Fault = "visual"
	// FaultLayout moves the badge out of its panel, which is the same story with geometry instead of
	// colour.
	FaultLayout Fault = "layout"
	// FaultSlow makes the events table take a moment to appear, every third request, which is what a
	// flake looks like before anybody writes it down.
	FaultSlow Fault = "slow"
	// FaultText changes a label, which every kind of assertion should catch.
	FaultText Fault = "text"
)

// Options configure the served page.
type Options struct {
	Fault Fault
	// SlowEvery makes every nth request to the events endpoint slow. Zero disables it.
	SlowEvery int
	// SlowFor is how long that request takes.
	SlowFor int // milliseconds
}

type server struct {
	opts    Options
	loaded  atomic.Int64 // every visit to an events endpoint counts, so "every third" is deterministic
	served  int64
	address string
}

// Handler returns the page, with fault injection wired in.
func Handler(opts Options) http.Handler {
	if opts.SlowEvery == 0 {
		opts.SlowEvery = 3
	}
	if opts.SlowFor == 0 {
		opts.SlowFor = 900
	}
	s := &server{opts: opts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.page)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// FaultFromEnv reads the fault the demo wants, so scenarios never mention it.
func FaultFromEnv() Options {
	slow := 0
	if v := os.Getenv("UITO_SLOW_MS"); v != "" {
		slow, _ = strconv.Atoi(v)
	}
	return Options{Fault: Fault(os.Getenv("UITO_FAULT")), SlowEvery: 3, SlowFor: slow}
}

type row struct {
	ID       string
	Kind     string
	Subject  string
	State    string
	BadgeTag string
}

var rows = []row{
	{ID: "ev-1041", Kind: "deploy", Subject: "game-api v1.9.2-dev", State: "succeeded", BadgeTag: "ok"},
	{ID: "ev-1042", Kind: "config", Subject: "match.maxPlayers", State: "applied", BadgeTag: "ok"},
	{ID: "ev-1043", Kind: "scale", Subject: "lobby pool 3 -> 5", State: "in progress", BadgeTag: "busy"},
	{ID: "ev-1044", Kind: "deploy", Subject: "nakama-go kit v0.4.1-dev", State: "failed", BadgeTag: "bad"},
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	s.served++
	data := map[string]any{
		"Title": "Live operations",
		"Fault": string(s.opts.Fault),
		// The badge class is what the fault changes. The words never move for FaultVisual, which is the
		// whole point: a check that reads text cannot see it.
		"BadgeClass": "badge-ok",
		"PanelClass": "panel",
		"Rows":       rows,
		"Heading":    "Events",
	}
	switch s.opts.Fault {
	case FaultVisual:
		data["BadgeClass"] = "badge-bad"
	case FaultLayout:
		data["PanelClass"] = "panel panel-moved"
	case FaultText:
		data["Heading"] = "Event"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// events is what assert_eventually waits for. Every third load is slow, which is not random: a
// deterministic flake in a fixture is the only way to measure a flake policy honestly.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	n := s.loaded.Add(1)
	if s.opts.Fault == FaultSlow && n%int64(s.opts.SlowEvery) == 0 {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Duration(s.opts.SlowFor) * time.Millisecond):
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"count": len(rows), "rows": rows})
}

func (s *server) String() string { return fmt.Sprintf("served %d pages", s.served) }

const pageHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Live operations</title>
<style>
  body { font: 14px/1.5 system-ui, sans-serif; margin: 0; background: #f6f7f9; color: #1d2129; }
  header { background: #14213d; color: #fff; padding: 14px 20px; }
  header h1 { margin: 0; font-size: 18px; }
  nav button { margin-right: 8px; }
  main { padding: 20px; }
  .panel { background: #fff; border: 1px solid #d5d9e0; border-radius: 6px; padding: 16px; max-width: 720px; }
  .panel-moved { margin-left: 320px; }
  .badge { display: inline-block; padding: 2px 10px; border-radius: 999px; font-weight: 600; }
  .badge-ok { background: #0f7b3f; color: #fff; }
  .badge-bad { background: #b42318; color: #fff; }
  table { border-collapse: collapse; width: 100%; margin-top: 12px; }
  th, td { text-align: left; padding: 6px 8px; border-bottom: 1px solid #e6e9ee; }
  #rows { display: none; }
  #rows.ready { display: table; }
</style></head>
<body>
<header><h1>Live operations</h1>
<nav>
  <button data-testid="tab-events" class="tab">Events</button>
  <button data-testid="tab-config" class="tab">Config</button>
  <button data-testid="tab-audit" class="tab">Audit</button>
</nav></header>
<main>
  <section class="{{.PanelClass}}" id="panel">
    <h2 id="heading">{{.Heading}}</h2>
    <p>Status <span id="status-badge" class="badge {{.BadgeClass}}" data-testid="status-badge">healthy</span></p>
    <p><label for="filter">Filter</label>
       <input id="filter" data-testid="filter" placeholder="filter events">
       <button id="load" data-testid="load">Load events</button></p>
    <p id="echo" data-testid="echo">nothing typed yet</p>
    <table id="rows" data-testid="rows"><thead><tr><th>ID</th><th>Kind</th><th>Subject</th><th>State</th></tr></thead>
      <tbody id="rows-body"></tbody></table>
  </section>
</main>
<script>
  const rows = document.getElementById('rows');
  const body = document.getElementById('rows-body');
  document.getElementById('load').addEventListener('click', async () => {
    const res = await fetch('/events');
    const data = await res.json();
    body.innerHTML = data.rows.map(r =>
      '<tr data-testid="event-row"><td>' + r.ID + '</td><td>' + r.Kind + '</td><td>' + r.Subject +
      '</td><td>' + r.State + '</td></tr>').join('');
    rows.classList.add('ready');
  });
  // The filter shows what was typed, so a scenario can tell whether the input event arrived.
  document.getElementById('filter').addEventListener('input', (e) => {
    document.getElementById('echo').textContent = 'typed: ' + e.target.value;
  });
  for (const tab of document.querySelectorAll('.tab')) {
    tab.addEventListener('click', () => {
      document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
      tab.classList.add('active');
      document.getElementById('heading').textContent = tab.textContent + ' (active)';
    });
  }
</script>
</body></html>`

var pageTemplate = template.Must(template.New("page").Parse(pageHTML))
