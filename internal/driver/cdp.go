package driver

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Chromium drives a headless browser over the DevTools protocol, written out rather than pulled in.
//
// The dependency was the easy part; the reason this is hand-written is that a driver is the thing a
// flake gets blamed on. When a click lands on the wrong element or an input never reaches React, the
// call stack has to be readable, and here it is nine hundred lines of this repository.
type Chromium struct {
	// Binary is the browser to run. Empty means: look for the ones CI and a developer machine have.
	Binary string
	// UserDataDir is created if empty. Kept around when KeepProfile is set, which is how a session that
	// failed can be opened by hand afterwards.
	UserDataDir string
	KeepProfile bool
	// Headful is for watching a scenario fail with your own eyes. Headless is the default because CI
	// has no eyes.
	Headful bool
	// CDPURL, when set, means: do not launch a browser, connect to this one. It is how the orchestrator
	// runs from a distroless image, with Chromium as a sidecar rather than inside the image, and how two
	// suites share one browser process. Stop() then leaves it running, for the same reason the adb driver
	// leaves the device alone.
	CDPURL string
	// Timeout caps a single protocol call, not the whole scenario.
	Timeout time.Duration

	cmd     *exec.Cmd
	profile string
	conn    *websocket.Conn
	session string

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan callResult
	events  map[string]chan json.RawMessage
	closed  bool

	// logPath keeps the browser's own stderr. When Chromium refuses to start the reason is in there and
	// nowhere else.
	logPath string
}

type callResult struct {
	result json.RawMessage
	err    error
}

func (c *Chromium) Name() string { return "playwright" }

func (c *Chromium) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 30 * time.Second
}

// FindBinary returns the first browser that exists, preferring an explicit path.
func FindBinary(explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("browser %s: %w", explicit, err)
		}
		return explicit, nil
	}
	if env := os.Getenv("UITO_BROWSER"); env != "" {
		return FindBinary(env)
	}
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"}
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	// The browsers a playwright installation already downloaded. Reusing them is the difference between
	// a lab that runs and a lab that starts with a two hundred megabyte download.
	home, err := os.UserHomeDir()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux*", "chrome"))
		if len(matches) > 0 {
			return matches[len(matches)-1], nil
		}
	}
	return "", fmt.Errorf("no chromium found: pass --browser, set UITO_BROWSER, or install one of %v", candidates)
}

func (c *Chromium) Start(ctx context.Context) error {
	if c.CDPURL != "" {
		return c.connectTo(ctx, c.CDPURL)
	}
	binary, err := FindBinary(c.Binary)
	if err != nil {
		return err
	}
	c.Binary = binary

	c.profile = c.UserDataDir
	if c.profile == "" {
		dir, err := os.MkdirTemp("", "uito-profile-")
		if err != nil {
			return err
		}
		c.profile = dir
	}

	c.logPath = filepath.Join(c.profile, "browser.log")
	logFile, err := os.Create(c.logPath)
	if err != nil {
		return err
	}

	args := []string{
		"--remote-debugging-port=0", // the port is written into DevToolsActivePort: no guessing, no race
		"--user-data-dir=" + c.profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-extensions",
		"--disable-dev-shm-usage", // /dev/shm is 64 MB in a container, and the default here is to crash
		"--no-sandbox",            // CI runs as a user without the namespaces Chromium wants
		"--window-size=1280,800",
		"about:blank",
	}
	if !c.Headful {
		args = append([]string{"--headless=new"}, args...)
	}

	c.cmd = exec.Command(binary, args...)
	c.cmd.Stdout = logFile
	c.cmd.Stderr = logFile
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", binary, err)
	}

	port, err := c.waitForPort()
	if err != nil {
		c.dumpLog()
		return err
	}
	wsURL, err := browserWebSocket(ctx, port)
	if err != nil {
		c.dumpLog()
		return err
	}
	if err := c.connect(ctx, wsURL); err != nil {
		return err
	}
	return c.attach(ctx)
}

// waitForPort reads the port Chromium chose. Polling for a file is more honest than picking a port and
// hoping nothing else in CI took it.
func (c *Chromium) waitForPort() (int, error) {
	path := filepath.Join(c.profile, "DevToolsActivePort")
	deadline := time.Now().Add(c.timeout())
	for time.Now().Before(deadline) {
		if c.cmd.ProcessState != nil && c.cmd.ProcessState.Exited() {
			return 0, fmt.Errorf("the browser exited before it listened (see %s)", c.logPath)
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			line, _, _ := strings.Cut(string(raw), "\n")
			if port, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && port > 0 {
				return port, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, fmt.Errorf("the browser never wrote %s", path)
}

// connectTo attaches to a browser that is already running.
func (c *Chromium) connectTo(ctx context.Context, base string) error {
	ws, err := webSocketAt(ctx, strings.TrimSuffix(base, "/")+"/json/version")
	if err != nil {
		return err
	}
	if err := c.connect(ctx, ws); err != nil {
		return err
	}
	return c.attach(ctx)
}

// webSocketAt reads the debugger URL a running browser advertises.
func webSocketAt(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("no browser at %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("%s did not advertise a debugger URL", url)
	}
	return body.WebSocketDebuggerURL, nil
}

func browserWebSocket(ctx context.Context, port int) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var body struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			_ = resp.Body.Close()
			if decodeErr == nil && body.WebSocketDebuggerURL != "" {
				return body.WebSocketDebuggerURL, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (c *Chromium) connect(ctx context.Context, url string) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("cdp dial: %w", err)
	}
	c.conn = conn
	c.pending = map[int64]chan callResult{}
	c.events = map[string]chan json.RawMessage{}
	go c.readLoop()
	return nil
}

type envelope struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string { return fmt.Sprintf("cdp %d: %s", e.Code, e.Message) }

func (c *Chromium) readLoop() {
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.mu.Lock()
			c.closed = true
			for _, ch := range c.pending {
				ch <- callResult{err: fmt.Errorf("cdp connection closed: %w", err)}
			}
			c.pending = map[int64]chan callResult{}
			c.mu.Unlock()
			return
		}
		var msg envelope
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.ID != 0 {
			c.mu.Lock()
			ch, ok := c.pending[msg.ID]
			delete(c.pending, msg.ID)
			c.mu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- callResult{err: msg.Error}
				} else {
					ch <- callResult{result: msg.Result}
				}
			}
			continue
		}
		if msg.Method != "" {
			key := msg.Method + "|" + msg.SessionID
			c.mu.Lock()
			ch, ok := c.events[key]
			if ok {
				delete(c.events, key)
			}
			c.mu.Unlock()
			if ok {
				ch <- msg.Params
			}
		}
	}
}

// send issues a call and waits for its reply. Session-scoped calls carry the session id; browser-scoped
// ones do not, which is how attaching a target and driving it are told apart.
func (c *Chromium) send(ctx context.Context, method string, params any, session string) (json.RawMessage, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("cdp: driver not started")
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp: connection closed")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan callResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := envelope{ID: id, Method: method, SessionID: session}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		msg.Params = raw
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	writeErr := c.conn.WriteMessage(websocket.TextMessage, payload)
	c.mu.Unlock()
	if writeErr != nil {
		return nil, fmt.Errorf("cdp write %s: %w", method, writeErr)
	}

	wait, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	select {
	case res := <-ch:
		return res.result, res.err
	case <-wait.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("cdp %s: no reply within %s", method, c.timeout())
	}
}

// attach opens a page and attaches to it with flattening on, so page commands and page events travel on
// the one browser connection.
func (c *Chromium) attach(ctx context.Context) error {
	raw, err := c.send(ctx, "Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		return err
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return err
	}
	raw, err = c.send(ctx, "Target.attachToTarget", map[string]any{"targetId": created.TargetID, "flatten": true}, "")
	if err != nil {
		return err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &attached); err != nil {
		return err
	}
	c.session = attached.SessionID
	for _, method := range []string{"Page.enable", "Runtime.enable", "DOM.enable"} {
		if _, err := c.send(ctx, method, nil, c.session); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
	}
	return nil
}

func (c *Chromium) Stop(ctx context.Context) error {
	if c.CDPURL != "" {
		// Somebody else's browser: close the connection, not the process.
		if c.conn != nil {
			_ = c.conn.Close()
		}
		return nil
	}
	if c.conn != nil {
		_, _ = c.send(ctx, "Browser.close", nil, "")
		_ = c.conn.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		done := make(chan struct{})
		go func() { _, _ = c.cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = c.cmd.Process.Kill()
		}
	}
	if c.profile != "" && !c.KeepProfile && c.UserDataDir == "" {
		_ = os.RemoveAll(c.profile)
	}
	return nil
}

// dumpLog puts the browser's own words in the error. "Chromium exited with status 1" sends the reader
// through the whole driver; the line that says why is in this file.
func (c *Chromium) dumpLog() {
	if c.logPath == "" {
		return
	}
	file, err := os.Open(c.logPath)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	fmt.Fprintf(os.Stderr, "--- browser log (%s) ---\n%s\n", c.logPath, strings.Join(lines, "\n"))
}

func (c *Chromium) SetViewport(ctx context.Context, width, height int) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	_, err := c.send(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false,
	}, c.session)
	return err
}

// Open navigates and waits for the load event. Waiting matters: a screenshot taken before the page
// settles makes a visual assertion flaky for a reason that has nothing to do with the assertion.
func (c *Chromium) Open(ctx context.Context, url string) error {
	loaded, err := c.expectEvent(ctx, "Page.loadEventFired")
	if err != nil {
		return err
	}
	if _, err := c.send(ctx, "Page.navigate", map[string]any{"url": url}, c.session); err != nil {
		return err
	}
	select {
	case <-loaded:
		return nil
	case <-time.After(c.timeout()):
		// A page that never fires load is still worth asserting against; say so instead of hanging.
		return fmt.Errorf("no load event within %s for %s", c.timeout(), url)
	}
}

// expectEvent registers interest in an event before the call that causes it, so the reply race is
// impossible.
func (c *Chromium) expectEvent(ctx context.Context, method string) (<-chan json.RawMessage, error) {
	ch := make(chan json.RawMessage, 1)
	key := method + "|" + c.session
	c.mu.Lock()
	c.events[key] = ch
	c.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
			c.mu.Lock()
			delete(c.events, key)
			c.mu.Unlock()
		case <-time.After(c.timeout()):
			c.mu.Lock()
			delete(c.events, key)
			c.mu.Unlock()
		}
	}()
	return ch, nil
}

func (c *Chromium) evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	raw, err := c.send(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	}, c.session)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.ExceptionDetails != nil {
		return nil, fmt.Errorf("page error: %s", out.ExceptionDetails.Text)
	}
	return out.Result.Value, nil
}

// resolveJS returns the JavaScript for "the element a selector names".
func resolveJS(selector string) string {
	kind, value := ParseSelector(selector)
	quoted, _ := json.Marshal(value)
	switch kind {
	case SelectorText:
		return `[...document.querySelectorAll('button,a,label,span,div,p,h1,h2,h3,li,td,th')]` +
			`.find(el => (el.textContent || '').trim().includes(` + string(quoted) + `))`
	default:
		return `document.querySelector(` + string(quoted) + `)`
	}
}

func (c *Chromium) Click(ctx context.Context, selector string) error {
	box, err := c.boundingBox(ctx, selector)
	if err != nil {
		return err
	}
	// Real input events, dispatched at the element's centre. Setting .click() from script works on a
	// static page and quietly skips the handlers a real framework installs.
	base := map[string]any{
		"x": box.X + box.Width/2, "y": box.Y + box.Height/2,
		"button": "left", "clickCount": 1,
	}
	if _, err := c.send(ctx, "Input.dispatchMouseEvent", withExtra(base, "mousePressed"), c.session); err != nil {
		return err
	}
	_, err = c.send(ctx, "Input.dispatchMouseEvent", withExtra(base, "mouseReleased"), c.session)
	return err
}

func withExtra(base map[string]any, eventType string) map[string]any {
	out := map[string]any{"type": eventType}
	for k, v := range base {
		out[k] = v
	}
	return out
}

type box struct{ X, Y, Width, Height float64 }

func (c *Chromium) boundingBox(ctx context.Context, selector string) (box, error) {
	raw, err := c.evaluate(ctx, `(() => { const el = `+resolveJS(selector)+`; if (!el) return null;
		el.scrollIntoView({block:'center'});
		const r = el.getBoundingClientRect();
		return {x: r.x, y: r.y, w: r.width, h: r.height}; })()`)
	if err != nil {
		return box{}, err
	}
	var got *struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
		W float64 `json:"w"`
		H float64 `json:"h"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return box{}, err
	}
	if got == nil {
		return box{}, fmt.Errorf("click: nothing matches %q", selector)
	}
	if got.W == 0 || got.H == 0 {
		return box{}, fmt.Errorf("click: %q has no size, so nothing can be clicked", selector)
	}
	return box{X: got.X, Y: got.Y, Width: got.W, Height: got.H}, nil
}

// Fill sets the value the way a framework notices. Assigning .value directly leaves React's own state
// untouched, so the field looks filled and the app submits empty - a flake a screenshot cannot explain.
func (c *Chromium) Fill(ctx context.Context, selector, value string) error {
	quoted, _ := json.Marshal(value)
	expr := `(() => { const el = ` + resolveJS(selector) + `; if (!el) return 'missing';
		el.scrollIntoView({block:'center'});
		el.focus();
		const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
		const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
		setter.call(el, ` + string(quoted) + `);
		el.dispatchEvent(new Event('input', {bubbles: true}));
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return 'ok'; })()`
	raw, err := c.evaluate(ctx, expr)
	if err != nil {
		return err
	}
	var status string
	if err := json.Unmarshal(raw, &status); err != nil {
		return err
	}
	if status != "ok" {
		return fmt.Errorf("fill: nothing matches %q", selector)
	}
	return nil
}

// Box reports where the element is, using the same resolution path as Click so that what is captured is
// what is clicked.
func (c *Chromium) Box(ctx context.Context, selector string) (Box, error) {
	b, err := c.boundingBox(ctx, selector)
	if err != nil {
		return Box{}, err
	}
	return Box{X: int(b.X), Y: int(b.Y), W: int(b.Width), H: int(b.Height)}, nil
}

func (c *Chromium) Text(ctx context.Context, selector string) (string, error) {
	raw, err := c.evaluate(ctx, `(() => { const el = `+resolveJS(selector)+`;
		return el ? (el.textContent || '').trim() : null; })()`)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "", fmt.Errorf("text: nothing matches %q", selector)
	}
	var got string
	if err := json.Unmarshal(raw, &got); err != nil {
		return "", err
	}
	return got, nil
}

func (c *Chromium) Exist(ctx context.Context, selector string) (bool, error) {
	raw, err := c.evaluate(ctx, `(() => !!`+resolveJS(selector)+`)()`)
	if err != nil {
		return false, err
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		return false, err
	}
	return got, nil
}

// Visible asks the browser the question a person would ask: is there something here with a size, and is
// it not hidden?
func (c *Chromium) Visible(ctx context.Context, selector string) (bool, error) {
	expr := `(() => { const el = ` + resolveJS(selector) + `;
		if (!el) return false;
		const style = getComputedStyle(el);
		if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return false;
		const r = el.getBoundingClientRect();
		return r.width > 0 && r.height > 0; })()`
	raw, err := c.evaluate(ctx, expr)
	if err != nil {
		return false, err
	}
	var got bool
	if err := json.Unmarshal(raw, &got); err != nil {
		return false, err
	}
	return got, nil
}

func (c *Chromium) Screenshot(ctx context.Context) (image.Image, error) {
	raw, err := c.send(ctx, "Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}, c.session)
	if err != nil {
		return nil, err
	}
	var shot struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &shot); err != nil {
		return nil, err
	}
	png, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(strings.NewReader(string(png)))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	return img, nil
}
