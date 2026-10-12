// Package driver is the seam between a scenario and whatever is being driven.
//
// The runner knows about steps, not about browsers. A driver that can open a page, click, fill, read
// text and take a screenshot can be a headless Chromium over CDP or a phone over adb; the flake policy
// and the report never learn which one ran.
package driver

import (
	"context"
	"image"
)

// Driver is deliberately small. Every method here has to be implementable for a phone as well as for a
// browser, so anything that only makes sense for one of them belongs in that driver, not in this one.
type Driver interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	SetViewport(ctx context.Context, width, height int) error
	Open(ctx context.Context, url string) error
	Click(ctx context.Context, selector string) error
	Fill(ctx context.Context, selector, value string) error
	Text(ctx context.Context, selector string) (string, error)
	// Box is where an element is on the screen, in integer pixels. Capturing a template needs it, and
	// so does anything that has to reason about layout rather than about text.
	Box(ctx context.Context, selector string) (Box, error)
	Exist(ctx context.Context, selector string) (bool, error)
	// Visible is not Exist. An element with display:none is present in the DOM and unreachable for a
	// person, and a check called "visible" that answered the other question is the reason this method
	// exists: the first version of assert_visible passed on a table that had not loaded.
	Visible(ctx context.Context, selector string) (bool, error)
	Screenshot(ctx context.Context) (image.Image, error)
}

// Box is an element's rectangle on the page, in integer pixels.
type Box struct{ X, Y, W, H int }

// SelectorKind separates the two selector languages a scenario can use. The default is CSS because it
// is what a browser developer writes; "text=" is there because most of what a person asserts about a UI
// is a label, and a scenario that spells out a generated class name is a scenario that breaks on the
// next deploy for no reason.
type SelectorKind int

const (
	SelectorCSS SelectorKind = iota
	SelectorText
)

// ParseSelector splits the optional "text=" prefix off a selector.
func ParseSelector(selector string) (SelectorKind, string) {
	const prefix = "text="
	if len(selector) > len(prefix) && selector[:len(prefix)] == prefix {
		return SelectorText, selector[len(prefix):]
	}
	return SelectorCSS, selector
}
