# ui-test-orchestrator

[![ci](https://github.com/lasttoss/ui-test-orchestrator/actions/workflows/ci.yml/badge.svg)](https://github.com/lasttoss/ui-test-orchestrator/actions/workflows/ci.yml)

Scenarios in YAML, a browser driven over the DevTools protocol, assertions that can look at pixels, and a
flake policy kept in a file the repository can see. Written because the two things that make a UI suite
useless are both measurable: an assertion that reads the DOM and calls a page with an unusable button
fine, and a "flaky" test that everybody has learned to re-run.

Both of those turned out to be measurable here, and neither is what it looks like.

```
make demo        # serve a fixture app, run 21 scenarios, write report/index.html
make visual      # what a screenshot assertion catches that a text assertion cannot
make flake       # the same four checks written two ways, with the flake rate of each
make policy      # quarantine and release, in transitions you can reproduce
make test        # unit tests: no browser, no cluster, no credentials
make readme-check # every command named in this file resolves, and the offline ones are run
```

## Two ways to run the suite

`make demo` runs on the host against the browser on the machine, and it is where the numbers in this
README come from. `make up` and `make compose-suite` run the same suite as containers, which is one
command on a machine that has Docker and nothing else - and, more usefully, they run it the way the
chart does: this image, attaching to a browser over the DevTools protocol rather than carrying one.

```
make up            # the browser sidecar and the fixture app, waiting until both answer
make compose-suite # the whole suite against them; the exit code is the suite's
make down
make image         # build the image, then check it runs as nonroot and serves the fixture app
```

Three things about that arrangement were learned by running it, and each was a failure first:

* **The browser is reached by IP, not by name.** Chrome's DevTools endpoint answers `500 Host header
  is specified and is not an IP address or localhost` to a request whose `Host` is a container name,
  so `http://browser:9222` cannot work however the network is arranged. Every service has a static
  address on the lab's own subnet, which also keeps the lab off the host's ports - an earlier version
  published 8080 and the suite spent one run testing somebody else's API.
* **Every step gets the same canvas.** The visual scenarios declare a region, not a viewport, so the
  viewport used to be whatever the browser defaulted to; against an attached browser with an 800x600
  window, a region at y=150 with height 480 does not fit, and one scenario failed with "region is
  outside the 800x600 screenshot". `runner.DefaultViewport` is 1280x900 for every run now, and a
  scenario can still declare its own.
* **The pixel threshold is calibrated per browser build, and the lab says so.** In the container the
  same badge matched at 0.73 against a 0.90 threshold, because the committed templates were captured
  with the browser `make demo` launches and the sidecar is Chrome 156. The lab sets
  `UITO_IMAGE_THRESHOLD=0.6`; the run prints a warning and every note carries
  "run threshold 0.60 overrides the scenario's 0.90", so a relaxed number cannot be read as a strict
  pass. The seventeen DOM scenarios are unaffected.


## What this demonstrates

| capability | how it is done here | where |
|---|---|---|
| **a driver that is not a browser's shape** | the same interface drives headless Chromium and a phone over adb; the browser driver is written against the DevTools protocol directly, and clicks with a real mouse event so a framework's handlers fire | `internal/driver` |
| **assertions that can see** | template matching over colour planes, coarse-to-fine, with a threshold and a region; a failing assertion reports the score, the position and the mean colour difference | `internal/vision` |
| **a schema that refuses to lie** | unknown keys and fields belonging to another action are errors, because the failure mode of an open schema is a scenario that passes while asserting nothing | `internal/scenario` |
| **a flake policy with consequences** | retry, quarantine after two whole runs failing, release after three whole runs passing, all kept in a committed file; a quarantined failure does not fail the build but is still printed and still run | `internal/flake`, `state/quarantine.json` |
| **a report you can send** | one HTML file with its screenshots inlined, plus a JSON summary a build system reads | `internal/report` |
| **reading somebody else's log format** | a device-driven Airtest run leaves a log, and a log nobody reads is an attachment rather than a gate: the format is read back with Airtest's own rules, grouped into test cases, written as the same report, and returned as the same exit code | `internal/airtestlog` |
| **commands that work, checked** | every command this file names is resolved, and the ones that need no browser and no container are run: a documented command that cannot run is a bug a reader would have found, so CI finds it first | `make readme-check` |
| **measurements, not opinions** | what a visual assertion caught that text assertions did not, and how much of a "flake rate" was really a race | `scripts/measure-visual.sh`, `scripts/measure-flake.sh` |

## The measurements

**A broken page, and which assertions noticed.** The fixture's visual fault turns one status badge from
green to red and changes no words at all.

| suite | result |
|---|---|
| 21 scenarios against a healthy page | **21 passed**, 0 flakes, about 5.5 s wall clock at 3 in parallel |
| the same 21 against the page with the red badge | **18 passed, 3 failed** |

Eighteen scenarios — every text and DOM check in the suite — passed on a page whose status badge had
turned red. The three that failed are the three that looked:

```
visual-badge              best match 0.999 at (77,176), mean colour difference 51.7,
                          threshold 0.90, max delta 40
```

**Correlation called it a perfect match.** 0.999 is not a near miss; a two-colour badge whose colour
changes is still an affine transformation of itself in each channel, so correlation is blind to it. What
failed the check was the second number: a mean colour difference of 51.7 against a bound of 40. That is
why this tool requires both numbers to pass, and why the vision package has a test asserting that a
dimmed page keeps its score.

**One honest weakness in the same table.** A fourth visual scenario, whose template covers the whole
panel rather than the badge, passed at a score of 0.947 with a colour difference of 0.8: a badge is a
small part of a panel, and a mean difference over a large region dilutes it. That is why `region` exists
and why the assertion prints the numbers instead of a verdict.

**A "flake" that was a race.** Four timing-sensitive checks, written two ways, against a fixture whose
events endpoint takes 900 ms every third request.

| how the check is written | attempts | scenario runs | failed | flaked |
|---|---|---|---|---|
| assert immediately after the click | 1 | 24 | **100%** | 0% |
| wait with `assert_eventually` | 1 | 126 | 0% | 0% |
| assert immediately, one retry | 2 | 24 | **100%** | 0% |
| wait, one retry | 2 | 126 | 0% | 0% |

The naive check does not fail sometimes, it fails **always** — 100% of runs, and 100% with a retry,
because the browser has not had a turn yet. A retry does not fix a race, it only makes it cheaper to
ignore. What is actually flaky is a *timeout that is too short*:

| waiting for a 900 ms endpoint | scenario runs | failed | flaked |
|---|---|---|---|
| 250 ms | 16 | 0% | **43.8%** |
| 8 s | 168 | 0% | 0% |

**The policy, in transitions.** `make policy` runs six times and prints the exit code of each:

```
run 1 against the broken server    failing scenarios: 1   exit code: 1
run 2 against the broken server    failing scenarios: 1   quarantined now: [smoke-title]   exit code: 0
run 3 against the broken server    failing scenarios: 1   exit code: 0
run 4-6 against a healthy server   released after three passes in a row
```

`state/quarantine.json` is committed, so which scenarios are trusted and why is a review comment rather
than a memory.

## A log somebody else wrote

`make demo` drives a browser. A device-driven run — a real phone, the Airtest path this repository has an
adb driver for — leaves a `log.txt` in a format of its own, and a run whose only output is an HTML file
somebody attaches to a pipeline is not a gate. So the log is read back:

```bash
make build
./bin/orchestrator airtest --log internal/airtestlog/testdata/airtest-log-fixed.txt --out report-airtest
```

```
log internal/airtestlog/testdata/airtest-log-fixed.txt: failed · 2 cases (1 passed, 1 failed) · 8 entries
  PASS test_healthcheck                   250 ms
  FAIL test_purchase                     1.9 s   first failure: buy_pack — AssertionError: balance did not grow by 500
```

Exit code 1, because one case failed: the same red a browser run gives, without asking Airtest to write
anything differently.

**The format is one JSON object per line**, and the rules are Airtest's own, taken from its report
(`airtest/report/report.py`) rather than invented here:

| what the log says | what it means |
|---|---|
| `{"tag": "function", "depth": 1, "time": …, "data": {"name": "test_purchase", "start_time": …, "end_time": …}}` | a wrapped function: how a test case is written in Airtest. `start_time`/`end_time` are its duration |
| `"data": {"traceback": …}` instead of `"ret"` | that function raised. This is where `airtest report` gets its red step, and it is the only failure signal a log carries |
| `"depth": 2` | something that happened inside a row. Deeper lines come *before* the row they belong to, because `Logwrap` writes a function's own entry in a `finally` block |
| the last row | `airtest report` decides the whole run on it, and on nothing else |

That last row is worth a sentence: **this repository and Airtest can disagree**, and when they do the
report says so instead of picking a winner. A run that failed and then logged a clean line is red here
and green there, and a reader should see that rather than find out later.

What the fixtures pin down, exactly, because a frozen clock makes it possible: a passing case at 250 ms, a
nested call at 500 ms, the failing one at 750 ms, the case that contains it at 1900 ms, and a wall clock
of 2800 ms. `internal/airtestlog/testdata/README.md` says how both logs were produced — by Airtest 1.4.3's
own writer, not typed by hand, because a hand-written log only proves that a reader can read what its
author already believed.

## How it works

**Figure 1.** A scenario file is run by a runner that executes several at once and gives every attempt a
fresh browser. The driver speaks the DevTools protocol directly, and the same interface drives a phone
over adb. A visual assertion reports two numbers because correlation alone called a red badge a pass.
Outcomes go to the flake policy and to a report with its screenshots inlined. Every run gets the same
canvas, and a pixel threshold that was relaxed for a different browser build says so in the note it
appears in rather than passing quietly. The three measurements at the bottom are the ones this
repository is built around.

![ui-test-orchestrator: scenario files are run in parallel, each attempt getting a fresh browser; a hand-written DevTools protocol driver clicks with real mouse events and captures pixels; a visual assertion reports both a correlation of 0.999 and a mean colour difference of 51.7 because correlation is invariant to brightness; outcomes feed a flake policy in a committed file and a self-contained report; and three measurements are quoted, that an immediately-asserting check failed 24 of 24 runs even with a retry, that waiting 250 milliseconds for a 900 millisecond endpoint flaked 43.8 percent of runs against none of 168 when waiting 8 seconds, and that on a page whose badge turned red 18 of 21 scenarios still passed](docs/figures/ui-test-orchestrator-flow.png)

A scenario is a list of steps and nothing else:

```yaml
name: the badge is the colour it should be
url: /
tags: [visual]
steps:
  - action: assert_image
    template: badge-healthy.png
    threshold: 0.9
    region: { x: 0, y: 85, w: 800, h: 240 }
```

Deep notes — the schema's rules, what the vision search guarantees and what it does not, how a template
is captured, and what to do when a visual assertion fails — are in
**[`docs/how-it-works.md`](docs/how-it-works.md)**.

## What this does not claim

- **Not a device farm.** One browser per worker on one machine. No grid, no remote devices, no Android
  emulator in CI.
- **The adb driver was never run against a phone.** No device was attached. It is exercised against a
  fake adb that records every command, and the tests assert the commands — a text selector becomes a tap
  in the middle of the node's bounds, `fill` taps and then types, `screencap` goes through `exec-out` and
  not `shell`. That is a real test of the driver and it is not the same as a test on a device; the README
  says which one it is.
- **The vision search is neither scale nor rotation invariant.** A template match finds the same pixels,
  not the same element after a redesign. A test asserts that limit rather than leaving it to be
  discovered.
- **No embedding-based comparison.** The spec this repository was built from suggested an optional
  embedding path; it is not here, because a threshold nobody can explain is what turns a visual suite
  into a coin toss.
- **The numbers come from one laptop.** A 21-scenario suite at 3 in parallel is not a capacity claim.

## Repository standard

| item | here | note |
|---|---|---|
| `docker-compose.yml` | n/a | the app under test is a fixture served in-process by `make demo`; a compose file would add a moving part to a demo that has none |
| `Dockerfile` | ✅ | distroless nonroot, two stages: the orchestrator, and the fixture app, so the demo runs without a browser on the host |
| Helm chart | ✅ | `charts/ui-test-orchestrator`: a Job and an optional CronJob, a NetworkPolicy that only allows the app under test, and the report kept in a volume; every branch rendered and validated in CI |
| `docs/figures/` + freshness gate | ✅ | `ui-test-orchestrator-flow` with `diagram-meta`; `make diagram` fails when the export is older than the sources it names; the .png export is checked against the .html it came from, and the checker lives in `scripts/`, so CI runs it too |
| tests + coverage in CI | ✅ | unit tests need no browser, no network and no credentials; the browser and adb drivers are tested against a real browser and a fake adb respectively |
| documented commands | ✅ | `make readme-check` resolves every command this file names and runs the offline ones; a `~/` path in a recipe is a failure, because a target that needs one machine is not a target |
| README with what it demonstrates | ✅ | this file, with the numbers and the commands that produced them |

## Licence

MIT, see `LICENSE`. What was written here and what was only inspired by something else is in
`PROVENANCE.md`.
