package driver_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lasttoss/ui-test-orchestrator/internal/driver"
)

// newFakeADB points the driver at testdata/bin/fake-adb and returns the log it writes.
func newFakeADB(t *testing.T) (*driver.ADB, string) {
	t.Helper()
	fake, err := filepath.Abs(filepath.Join("..", "..", "testdata", "bin", "fake-adb"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fake); err != nil {
		t.Skipf("no fake adb: %v", err)
	}
	log := filepath.Join(t.TempDir(), "adb.log")
	t.Setenv("UITO_FAKE_ADB_LOG", log)
	return &driver.ADB{Binary: fake, Timeout: 10 * time.Second}, log
}

func commands(t *testing.T, log string) []string {
	t.Helper()
	raw, err := os.ReadFile(log)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// The device path is the one that cannot be tested on this machine, so what is tested is what the
// driver sends: a text selector has to become a tap in the middle of the node's bounds, or a mobile
// scenario taps the wrong thing and the failure looks like the app's fault.
func TestADBResolvesTextToTheRightTap(t *testing.T) {
	d, log := newFakeADB(t)
	ctx := context.Background()

	if err := d.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := d.Open(ctx, "myapp://events"); err != nil {
		t.Fatal(err)
	}
	if err := d.Click(ctx, "text=Save"); err != nil {
		t.Fatal(err)
	}
	// bounds="[100,200][300,260]" -> centre (200, 230)
	issued := commands(t, log)
	found := false
	for _, c := range issued {
		if c == "shell input tap 200 230" {
			found = true
		}
	}
	if !found {
		t.Errorf("no tap at the node centre; commands were:\n%s", strings.Join(issued, "\n"))
	}

	if text, err := d.Text(ctx, "text=Filter"); err != nil || text != "Filter" {
		t.Errorf("text = %q, %v", text, err)
	}
	if ok, err := d.Exist(ctx, "text=Save"); err != nil || !ok {
		t.Errorf("exist = %v, %v", ok, err)
	}
	if ok, err := d.Visible(ctx, "text=Nope"); err != nil || ok {
		t.Errorf("visible on a missing node = %v, %v", ok, err)
	}
	box, err := d.Box(ctx, "text=Save")
	if err != nil {
		t.Fatal(err)
	}
	if box.W != 200 || box.H != 60 {
		t.Errorf("box = %+v, want 200x60 from the bounds", box)
	}
}

// Filling a field on a phone is a tap and then typing: there is no way to set a value from outside. A
// driver that pretended otherwise would appear to work and leave every form empty.
func TestADBFillsByTappingThenTyping(t *testing.T) {
	d, log := newFakeADB(t)
	ctx := context.Background()
	if err := d.Fill(ctx, "text=Filter", "hello world"); err != nil {
		t.Fatal(err)
	}
	issued := commands(t, log)
	var sawTap, sawText bool
	for _, c := range issued {
		if strings.HasPrefix(c, "shell input tap") {
			sawTap = true
		}
		if c == "shell input text hello%sworld" {
			sawText = true
			// A space has to be encoded, or adb types two words and the second one goes somewhere else.
		}
	}
	if !sawTap || !sawText {
		t.Errorf("fill did not tap then type; commands:\n%s", strings.Join(issued, "\n"))
	}
}

func TestADBScreenshotUsesExecOut(t *testing.T) {
	d, log := newFakeADB(t)
	img, err := d.Screenshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 6 || img.Bounds().Dy() != 4 {
		t.Errorf("screenshot is %v", img.Bounds())
	}
	for _, c := range commands(t, log) {
		// "adb shell screencap -p" translates newlines and corrupts the PNG; exec-out does not.
		if strings.HasPrefix(c, "shell screencap") {
			t.Errorf("screencap went through the shell: %s", c)
		}
	}
}

// Two methods that would be lies on this platform: a phone has no viewport to set, and stopping a run
// must not disconnect a device somebody else is using.
func TestADBIsHonestAboutWhatAPhoneCannotDo(t *testing.T) {
	d, log := newFakeADB(t)
	ctx := context.Background()
	if err := d.SetViewport(ctx, 1280, 800); err != nil {
		t.Errorf("SetViewport returned %v; it should be a no-op", err)
	}
	if err := d.Stop(ctx); err != nil {
		t.Errorf("Stop returned %v", err)
	}
	if issued := commands(t, log); len(issued) != 0 {
		t.Errorf("a no-op talked to the device: %v", issued)
	}
}

func TestADBWithNoDeviceIsAnError(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "adb")
	if err := os.WriteFile(empty, []byte("#!/bin/sh\necho 'List of devices attached'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := &driver.ADB{Binary: empty, Timeout: 5 * time.Second}
	if err := d.Start(context.Background()); err == nil {
		t.Fatal("no attached device was not reported")
	}
}
