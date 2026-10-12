package driver

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ADB drives an Android device over adb.
//
// It is here because a driver interface that only one driver can implement is not an interface, it is a
// browser with extra steps. Everything this file cannot do is a method that returns an honest answer
// rather than a fake one: a phone has no viewport to set, and there is no DOM to ask, so selectors are
// either coordinates or text found in the accessibility tree.
//
// No device was available when this was written. The driver is exercised against a fake adb that records
// every command it is given, and the README says exactly that. A driver claiming to be tested against a
// phone nobody had is the kind of claim this portfolio exists to avoid.
type ADB struct {
	// Serial selects a device when several are attached.
	Serial string
	// Binary is the adb to run. Empty means: whatever is on PATH.
	Binary string
	// Timeout caps one adb call.
	Timeout time.Duration
}

func (a *ADB) Name() string { return "adb" }

func (a *ADB) binary() string {
	if a.Binary != "" {
		return a.Binary
	}
	if env := os.Getenv("UITO_ADB"); env != "" {
		return env
	}
	return "adb"
}

func (a *ADB) timeout() time.Duration {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return 30 * time.Second
}

// adb runs one command against the chosen device.
func (a *ADB) adb(ctx context.Context, args ...string) (string, error) {
	wait, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	full := args
	if a.Serial != "" {
		full = append([]string{"-s", a.Serial}, args...)
	}
	cmd := exec.CommandContext(wait, a.binary(), full...)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("adb %s: %w: %s", strings.Join(full, " "), err, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

func (a *ADB) Start(ctx context.Context) error {
	out, err := a.adb(ctx, "devices")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "device" {
			if a.Serial == "" || fields[0] == a.Serial {
				return nil
			}
		}
	}
	if a.Serial != "" {
		return fmt.Errorf("no device with serial %s is attached", a.Serial)
	}
	return fmt.Errorf("no adb device is attached: %s", strings.TrimSpace(out))
}

// Stop does nothing on purpose. Closing a shared adb server would pull the device out from under every
// other run on the machine, and a test framework that disconnects your phone is not one you leave
// installed.
func (a *ADB) Stop(ctx context.Context) error { return nil }

// SetViewport returns nil without doing anything: a phone's viewport is its screen. It says so here
// rather than in a comment in the runner, because this is where a reader looks.
func (a *ADB) SetViewport(ctx context.Context, width, height int) error { return nil }

// Open starts an activity for a URL, which is how a deep link reaches a mobile app.
func (a *ADB) Open(ctx context.Context, url string) error {
	_, err := a.adb(ctx, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	return err
}

// Click taps. A selector is either "x,y" or a text selector resolved through the accessibility tree.
func (a *ADB) Click(ctx context.Context, selector string) error {
	x, y, err := a.target(ctx, selector)
	if err != nil {
		return err
	}
	_, err = a.adb(ctx, "shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	return err
}

func (a *ADB) Fill(ctx context.Context, selector, value string) error {
	// Mobile has no way to set a field's value: the field has to have focus, and then the characters are
	// typed. So a fill is a tap and a type, and the scenario author is the one who knows that the field
	// is reachable.
	if err := a.Click(ctx, selector); err != nil {
		return err
	}
	_, err := a.adb(ctx, "shell", "input", "text", strings.ReplaceAll(value, " ", "%s"))
	return err
}

func (a *ADB) Text(ctx context.Context, selector string) (string, error) {
	nodes, err := a.hierarchy(ctx)
	if err != nil {
		return "", err
	}
	node := findNode(nodes, selector)
	if node == nil {
		return "", fmt.Errorf("text: nothing on screen matches %q", selector)
	}
	return strings.TrimSpace(node.Text), nil
}

func (a *ADB) Exist(ctx context.Context, selector string) (bool, error) {
	nodes, err := a.hierarchy(ctx)
	if err != nil {
		return false, err
	}
	return findNode(nodes, selector) != nil, nil
}

func (a *ADB) Visible(ctx context.Context, selector string) (bool, error) {
	// The accessibility tree only contains what is on screen, so existing here already means visible.
	node, err := a.nodeFor(ctx, selector)
	if err != nil {
		return false, err
	}
	return node != nil, nil
}

func (a *ADB) Box(ctx context.Context, selector string) (Box, error) {
	node, err := a.nodeFor(ctx, selector)
	if err != nil {
		return Box{}, err
	}
	if node == nil {
		return Box{}, fmt.Errorf("nothing on screen matches %q", selector)
	}
	return node.box()
}

// Screenshot asks the device for its framebuffer. exec-out is the only path that does not corrupt the
// PNG with a pty's newline translation, which is a mistake worth never making twice.
func (a *ADB) Screenshot(ctx context.Context) (image.Image, error) {
	wait, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	args := []string{"exec-out", "screencap", "-p"}
	if a.Serial != "" {
		args = append([]string{"-s", a.Serial}, args...)
	}
	cmd := exec.CommandContext(wait, a.binary(), args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("adb screencap: %w: %s", err, strings.TrimSpace(errBuf.String()))
	}
	img, _, err := image.Decode(bytes.NewReader(out.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("decode screencap: %w", err)
	}
	return img, nil
}

func (a *ADB) target(ctx context.Context, selector string) (int, int, error) {
	if x, y, ok := parsePoint(selector); ok {
		return x, y, nil
	}
	node, err := a.nodeFor(ctx, selector)
	if err != nil {
		return 0, 0, err
	}
	if node == nil {
		return 0, 0, fmt.Errorf("nothing on screen matches %q", selector)
	}
	box, err := node.box()
	if err != nil {
		return 0, 0, err
	}
	return box.X + box.W/2, box.Y + box.H/2, nil
}

func (a *ADB) nodeFor(ctx context.Context, selector string) (*node, error) {
	nodes, err := a.hierarchy(ctx)
	if err != nil {
		return nil, err
	}
	return findNode(nodes, selector), nil
}

// hierarchy dumps the accessibility tree. uiautomator writes to a file and prints where, which is a
// detail every first version of this gets wrong.
func (a *ADB) hierarchy(ctx context.Context) ([]node, error) {
	const remote = "/sdcard/uito-dump.xml"
	if _, err := a.adb(ctx, "shell", "uiautomator", "dump", remote); err != nil {
		return nil, err
	}
	raw, err := a.adb(ctx, "exec-out", "cat", remote)
	if err != nil {
		return nil, err
	}
	// Flat scan, not a struct with a slice of direct children: uiautomator nests its nodes, and the
	// unmarshal that looks right succeeds while returning only the outermost one - so a text selector
	// finds nothing and the failure looks like the app is missing a button that is plainly on screen.
	nodes, err := scanNodes(raw)
	if err != nil {
		return nil, fmt.Errorf("parse ui dump: %w", err)
	}
	return nodes, nil
}

type node struct {
	Text        string `xml:"text,attr"`
	ResourceID  string `xml:"resource-id,attr"`
	Bounds      string `xml:"bounds,attr"`
	Description string `xml:"content-desc,attr"`
}

func (n node) box() (Box, error) {
	// bounds="[x1,y1][x2,y2]"
	trimmed := strings.NewReplacer("[", " ", "]", " ", ",", " ").Replace(n.Bounds)
	fields := strings.Fields(trimmed)
	if len(fields) != 4 {
		return Box{}, fmt.Errorf("cannot read bounds %q", n.Bounds)
	}
	var v [4]int
	for i, f := range fields {
		parsed, err := strconv.Atoi(f)
		if err != nil {
			return Box{}, fmt.Errorf("bounds %q: %w", n.Bounds, err)
		}
		v[i] = parsed
	}
	return Box{X: v[0], Y: v[1], W: v[2] - v[0], H: v[3] - v[1]}, nil
}

// findNode matches a text selector against visible text, content description or resource id.
func findNode(nodes []node, selector string) *node {
	kind, value := ParseSelector(selector)
	for i := range nodes {
		n := &nodes[i]
		if kind == SelectorText {
			if strings.Contains(n.Text, value) || strings.Contains(n.Description, value) {
				return n
			}
			continue
		}
		if n.ResourceID == value || strings.Contains(n.ResourceID, strings.TrimPrefix(value, "#")) {
			return n
		}
	}
	return nil
}

func parsePoint(selector string) (int, int, bool) {
	parts := strings.Split(selector, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errX != nil || errY != nil {
		return 0, 0, false
	}
	return x, y, true
}

// scanNodes reads every node out of a nested dump.
//
// It walks tokens and reads attributes itself, rather than decoding an element: decoding an element
// consumes its whole subtree, so a dump of a screen with three controls yielded exactly one node - the
// outermost frame, which has no text - and every text selector then reported that the button was not on
// screen while it plainly was.
func scanNodes(raw string) ([]node, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	var out []node
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return out, nil
			}
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "node" {
			continue
		}
		var n node
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "text":
				n.Text = attr.Value
			case "resource-id":
				n.ResourceID = attr.Value
			case "bounds":
				n.Bounds = attr.Value
			case "content-desc":
				n.Description = attr.Value
			}
		}
		out = append(out, n)
	}
}
