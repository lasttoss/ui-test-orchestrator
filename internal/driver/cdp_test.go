package driver_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/driver"
)

// The page is written here rather than checked in as a fixture so that the assertions and the markup
// they depend on stay in one file.
const page = `<!doctype html><html><head><title>fixture</title></head><body>
<h1 id="title">Live operations</h1>
<button id="count" data-testid="count">Clicked 0 times</button>
<input id="filter" placeholder="filter events">
<p id="echo">nothing typed yet</p>
<div id="hidden-rows" data-testid="hidden-rows" style="display:none"><table><tr><td>lazy</td></tr></table></div>
<script>
  let n = 0;
  // A real handler, not an inline onclick: an attribute would keep working even if the driver
  // dispatched nothing but a scripted call.
  document.getElementById('count').addEventListener('click', () => {
    n += 1;
    document.getElementById('count').textContent = 'Clicked ' + n + ' times';
  });
  // The input event is what a framework listens to. A driver that assigns .value without dispatching
  // it fills the field and leaves the app believing nothing happened.
  document.getElementById('filter').addEventListener('input', (e) => {
    document.getElementById('echo').textContent = 'typed: ' + e.target.value;
  });
</script></body></html>`

func start(t *testing.T) (*driver.Chromium, string) {
	t.Helper()
	if os.Getenv("UITO_SKIP_BROWSER") != "" {
		t.Skip("UITO_SKIP_BROWSER is set")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	d := &driver.Chromium{Timeout: 20 * time.Second}
	if err := d.Start(ctx); err != nil {
		t.Skipf("no browser available: %v", err)
	}
	t.Cleanup(func() { _ = d.Stop(context.Background()) })
	return d, server.URL
}

func TestBrowserDrivesAPage(t *testing.T) {
	d, url := start(t)
	ctx := context.Background()

	if err := d.SetViewport(ctx, 1024, 768); err != nil {
		t.Fatal(err)
	}
	if err := d.Open(ctx, url); err != nil {
		t.Fatal(err)
	}

	if got, err := d.Text(ctx, "#title"); err != nil || got != "Live operations" {
		t.Fatalf("text = %q, %v", got, err)
	}

	// A click has to reach a listener, so the assertion is on the page's own reaction.
	for i := 1; i <= 2; i++ {
		if err := d.Click(ctx, "#count"); err != nil {
			t.Fatalf("click %d: %v", i, err)
		}
		want := fmt.Sprintf("Clicked %d times", i)
		if got, _ := d.Text(ctx, "#count"); got != want {
			t.Fatalf("after click %d: %q, want %q", i, got, want)
		}
	}

	if err := d.Fill(ctx, "#filter", "deploy"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Text(ctx, "#echo"); got != "typed: deploy" {
		t.Errorf("the input event never reached the page: %q", got)
	}

	// The same click by label, which is how most of a scenario suite is written.
	if err := d.Click(ctx, "text=Clicked 2 times"); err != nil {
		t.Errorf("text selector: %v", err)
	}
	if got, _ := d.Text(ctx, "#count"); got != "Clicked 3 times" {
		t.Errorf("after a text click: %q", got)
	}

	if ok, err := d.Exist(ctx, "#count"); err != nil || !ok {
		t.Errorf("exist = %v, %v", ok, err)
	}
	if ok, _ := d.Exist(ctx, "#nope"); ok {
		t.Error("a missing selector reported present")
	}

	// The difference the assertion names depend on: present and hidden is not present and visible.
	if ok, err := d.Exist(ctx, "#hidden-rows"); err != nil || !ok {
		t.Errorf("exist on a hidden element = %v, %v", ok, err)
	}
	if ok, err := d.Visible(ctx, "#hidden-rows"); err != nil || ok {
		t.Errorf("visible on a display:none element = %v, %v (want false)", ok, err)
	}
	if ok, err := d.Visible(ctx, "#title"); err != nil || !ok {
		t.Errorf("visible on a heading = %v, %v", ok, err)
	}

	shot, err := d.Screenshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bounds := shot.Bounds()
	if bounds.Dx() != 1024 || bounds.Dy() != 768 {
		t.Errorf("screenshot is %dx%d, want the viewport 1024x768", bounds.Dx(), bounds.Dy())
	}
}

// Failing loudly is part of the contract: a scenario that clicks something absent must say which
// selector was absent, or the person reading the report has to guess.
func TestErrorsNameTheSelector(t *testing.T) {
	d, url := start(t)
	ctx := context.Background()
	if err := d.Open(ctx, url); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"click": d.Click(ctx, "#absent"),
		"fill":  d.Fill(ctx, "#absent", "x"),
	} {
		if err == nil {
			t.Fatalf("%s on a missing element succeeded", name)
		}
		if !strings.Contains(err.Error(), "#absent") {
			t.Errorf("%s error does not name the selector: %v", name, err)
		}
	}
	if _, err := d.Text(ctx, "#absent"); err == nil {
		t.Error("text on a missing element succeeded")
	}
}

func TestOpenWithoutABrowserIsAnError(t *testing.T) {
	d := &driver.Chromium{Binary: "/nonexistent/chrome", Timeout: 2 * time.Second}
	if err := d.Start(context.Background()); err == nil {
		t.Fatal("a missing browser was not reported")
	}
}
