package app_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/app"
)

func get(t *testing.T, handler http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// The visual fault is the premise of this repository's headline measurement, so it is asserted exactly:
// the badge changes and nothing a text assertion reads does.
func TestTheVisualFaultChangesColourAndNoWords(t *testing.T) {
	healthy := get(t, app.Handler(app.Options{}), "/")
	broken := get(t, app.Handler(app.Options{Fault: app.FaultVisual}), "/")

	// The class as it is used on an element, not as it is defined in the stylesheet: the first version of
	// this test failed on the healthy page because .badge-bad appears in the inline CSS.
	const applied = `class="badge badge-bad"`
	if strings.Contains(healthy, applied) {
		t.Error("the healthy page already carries the fault")
	}
	if !strings.Contains(broken, applied) {
		t.Fatal("the visual fault did not change the badge")
	}
	// Same words, same structure: only the class differs.
	strip := func(s string) string {
		var out []string
		for _, line := range strings.Split(s, "\n") {
			if strings.Contains(line, "badge badge-") {
				continue
			}
			out = append(out, line)
		}
		return strings.Join(out, "\n")
	}
	if strip(healthy) != strip(broken) {
		t.Error("the visual fault changed something other than the badge: a text check would notice this one")
	}
	if !strings.Contains(broken, "healthy") {
		t.Error("the badge's words changed, so the measurement is not what the README claims")
	}
}

func TestTheOtherFaultsAreWhatTheySayTheyAre(t *testing.T) {
	if got := get(t, app.Handler(app.Options{Fault: app.FaultText}), "/"); !strings.Contains(got, "id=\"heading\">Event<") {
		t.Error("the text fault did not change the heading")
	}
	if got := get(t, app.Handler(app.Options{Fault: app.FaultLayout}), "/"); !strings.Contains(got, "panel-moved") {
		t.Error("the layout fault did not move the panel")
	}
	if got := get(t, app.Handler(app.Options{Fault: "nonsense"}), "/"); !strings.Contains(got, `class="badge badge-ok"`) {
		t.Error("an unknown fault changed the page; the default has to be a healthy page")
	}
}

// Every third request is slow, and the other two are not: the flake measurement depends on the pattern
// being a pattern rather than noise, so it is asserted rather than assumed.
func TestTheSlowFaultIsEveryThirdRequest(t *testing.T) {
	handler := app.Handler(app.Options{Fault: app.FaultSlow, SlowEvery: 3, SlowFor: 120})
	server := httptest.NewServer(handler)
	defer server.Close()

	var slow, fast int
	for i := 1; i <= 6; i++ {
		started := time.Now()
		resp, err := http.Get(server.URL + "/events")
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Count int `json:"count"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if body.Count == 0 {
			t.Error("the events endpoint returned nothing")
		}
		if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
			slow++
		} else {
			fast++
		}
	}
	if slow != 2 || fast != 4 {
		t.Errorf("%d slow and %d fast out of six, want every third one slow", slow, fast)
	}
}

func TestHealthzAnswers(t *testing.T) {
	if got := get(t, app.Handler(app.Options{}), "/healthz"); got != "ok" {
		t.Errorf("healthz = %q", got)
	}
}

var _ = fmt.Sprintf
