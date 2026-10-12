# How it works, and what to do when it does not

## The scenario schema

A step names an action, and the validator knows which fields that action uses. An unknown key is an
error, and a field belonging to another action is an error:

```
step 2: unknown action "asert_text"
step 1: open does not use selector
```

Both of those are scenarios that would otherwise run and assert nothing, which is worse than one that
fails: a suite of assertions-that-assert-nothing is green forever, and the first person to find out is
the customer.

Actions: `open`, `click`, `fill`, `assert_text`, `assert_visible`, `assert_image`, `assert_eventually`.

| action | means |
|---|---|
| `assert_visible` | the element exists **and** is visible: `display:none` is not visible |
| `assert_text` | the element's text **contains** the given string |
| `assert_image` | the template matches within the region, at or above the threshold, and no larger a mean colour difference than the bound |
| `assert_eventually` | run the nested steps repeatedly until they pass or the timeout expires |

Selectors are CSS, or `text=` to match an element by its label. Most of what a person asserts about a UI
is a word on the screen, and a scenario that spells out a generated class name breaks on the next deploy
for no reason.

## What the vision search guarantees, and what it does not

`vision.Find` reports a score (normalised cross-correlation, averaged over three colour planes), the
position it found, and the mean absolute colour difference there.

- **It searches coarse to fine.** A pyramid with four candidates at the coarsest useful level, refined
  downward. The coarsest level is chosen so the template is at least eight pixels wide: a 34x28 template
  at one eighth is four pixels across, and at four pixels a textured page has dozens of equally good
  peaks. This threshold is in the code because searching there found the wrong one.
- **When it is not confident it looks everywhere.** A score below 0.9 triggers a second pass at half
  resolution and then an exhaustive scan filtered by a sampled sum of absolute differences. An exact
  match gives that filter zero, so a true match is never lost to the filter, and a page with no match
  costs one cheap full scan rather than the answer being invented.
- **Correlation is invariant to brightness.** A page dimmed to a quarter still correlates at 0.9999. The
  mean colour difference is what notices, so `Passes` requires both.
- **A flat template scores zero, not one.** "This area is the same shade of grey" is not a match.
- **Not scale or rotation invariant**, and not a substitute for a selector. A test asserts the limit.

## Capturing a template

```
bin/orchestrator capture --url http://127.0.0.1:PORT/ \
  --selector '[data-testid=status-badge]' --out testdata/templates/badge-healthy.png \
  --padding 4
```

It asks the page where the element is, crops the screenshot to integer pixels and writes a PNG. Hand
cropping a screenshot is how a template ends up half a pixel out and permanently marginal, which reads
as flakiness in the assertion rather than in the template.

## When a visual assertion fails

Read the numbers before reading the image:

| what you see | what it means | what to do |
|---|---|---|
| score low, position moved | the element is gone or has moved | check whether that was intended |
| score high, delta high | the shape is there and the colour is not | look at the screenshot in the report |
| score high, delta low, but it passed before | the template is too small or the threshold too tight | widen the region, or re-capture and review the diff |
| a template covering more than the thing you care about | the change is diluted (this repository has an example: 0.947 and a delta of 0.8 on a badge that turned red) | tighten the region to the element |

If the change was intended, re-capture the template with `capture`, and commit it: a template is part of
the specification, not a cache.

## The flake policy

`state/quarantine.json`:

- two whole runs failing in a row → quarantined;
- three whole runs passing in a row → released;
- a quarantined scenario still runs and is still printed; its failure does not fail the build;
- a failure in the middle resets the pass streak, so release needs consecutive evidence;
- a scenario that fails the first attempt and passes the second counts as a **flake**, not a failure, and
  does not move it towards quarantine.

`bin/orchestrator policy --state state/quarantine.json` prints the file in a form you can read in a
review.

## The adb driver

Same interface, and two methods that would be lies on a phone:

- `SetViewport` returns nil and does nothing: a phone's viewport is its screen.
- `Stop` does not run `adb kill-server`: disconnecting a device because a test suite finished is not a
  thing a test suite should do.

Selectors are `x,y` coordinates, `text=` labels, or resource ids. `fill` taps and then types, because
there is no way to set a field from outside the app. There was no device attached when this was written;
the tests run against `testdata/bin/fake-adb` and assert the commands, which is what can be wrong in a
driver no phone was plugged into.
