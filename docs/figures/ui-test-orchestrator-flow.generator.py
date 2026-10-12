#!/usr/bin/env python3
"""Generate the figure beside this generator: it writes <own name>.html, measured not positioned by eye.

Measured, not positioned by eye: every box is sized from its own text, so editing a line below
cannot overlap anything. Same shape as the generators in this portfolio, for the same reason.

    python3 docs/figures/ui-test-orchestrator-flow.generator.py
    python3 <diagram-design>/scripts/self_check.py docs/figures/ui-test-orchestrator-flow.html
    make diagram
"""
import html
import pathlib

PAPER, INK, MUTED, ACCENT, LINE, TINT, BAD = "#f5f5f5", "#2d3142", "#4f5d75", "#b3541e", "#c9ccd1", "#fdf4ef", "#8c2f1f"
FONT = {"sans": "'Geist', system-ui, sans-serif",
        "mono": "'Geist Mono', ui-monospace, monospace",
        "serif": "'Instrument Serif', serif"}
ADVANCE = {"mono": 0.605, "sans": 0.525, "sans-bold": 0.565}
PAD, GAP = 16, 110


def esc(s):
    return html.escape(str(s))


def width(s, size, family="sans", weight="400"):
    key = "mono" if family == "mono" else ("sans-bold" if weight == "600" else "sans")
    return len(s) * ADVANCE[key] * size


def text(x, y, s, size=12.5, weight="400", fill=INK, family="sans", anchor="start"):
    return (f'<text x="{x:.0f}" y="{y:.0f}" font-family="{FONT[family]}" font-size="{size}" '
            f'font-weight="{weight}" fill="{fill}" text-anchor="{anchor}">{esc(s)}</text>')


def rect(x, y, w, h, fill="#ffffff", stroke=LINE, rx=10):
    return f'<rect x="{x:.0f}" y="{y:.0f}" width="{w:.0f}" height="{h:.0f}" rx="{rx}" fill="{fill}" stroke="{stroke}"/>'


def arrow(x1, y1, x2, y2, label="", dashed=False):
    dash = ' stroke-dasharray="5 4"' if dashed else ""
    out = [f'<line x1="{x1:.0f}" y1="{y1:.0f}" x2="{x2:.0f}" y2="{y2:.0f}" stroke="{MUTED}" stroke-width="1.6" '
           f'marker-end="url(#arrowhead)"{dash}/>']
    if label:
        mx, my = (x1 + x2) / 2, (y1 + y2) / 2
        lw = width(label, 11, "mono") + 12
        out.append(rect(mx - lw / 2, my - 12, lw, 20, fill=PAPER, stroke="none", rx=5))
        out.append(text(mx, my + 2, label, size=11, fill=MUTED, family="mono", anchor="middle"))
    return "".join(out)


class Block:
    def __init__(self, title, subtitle, lines, tint=None):
        self.title, self.subtitle, self.lines, self.tint = title, subtitle, lines, tint
        widths = [width(title, 15, weight="600")]
        if subtitle:
            widths.append(width(subtitle, 11.5, "mono"))
        for line, mono in lines:
            widths.append(width(line, 11.5, "mono" if mono else "sans"))
        self.w = max(widths) + PAD * 2
        self.h = PAD * 2 + 22 + (16 if subtitle else 0) + len(lines) * 17

    def render(self, x, y):
        out = [rect(x, y, self.w, self.h, fill=self.tint or "#ffffff")]
        out.append(text(x + PAD, y + PAD + 12, self.title, size=15, weight="600"))
        yy = y + PAD + 34
        if self.subtitle:
            out.append(text(x + PAD, yy + 4, self.subtitle, size=11.5, fill=MUTED, family="mono"))
            yy += 16
        for line, mono in self.lines:
            out.append(text(x + PAD, yy + 9, line, size=11.5, family="mono" if mono else "sans"))
            yy += 17
        return "".join(out)


CLIENT = Block("scenarios/*.yaml, 21 files", "a person writes this part", [
    ("open · click · fill · assert_text", True),
    ("assert_visible  present AND visible", True),
    ("assert_image    template, threshold, region", True),
    ("assert_eventually  wait inside the step", True),
], tint=TINT)
RUNNER = Block("runner: parallel, attempts", "a fresh driver for every attempt", [
    ("21 scenarios, 3 at a time, about 5.5 s", True),
    ("a retry opens a new page, never the failed one", True),
], tint=TINT)
DRIVER = Block("the browser driver, written here", "the DevTools protocol, no SDK", [
    ("click  -> a real mouse event at the centre", True),
    ("fill   -> value plus input event, so React sees it", True),
    ("shot   -> the pixels an assertion can read", True),
    ("visible-> display:none is not visible", True),
], tint=TINT)
VISION = Block("a visual assertion, two numbers", "correlation alone calls a broken page a pass", [
    ("badge turns green -> red: score 0.999", True),
    ("...and mean colour difference 51.7", True),
    ("correlation is invariant to brightness", True),
    ("so a match needs both: score and delta", True),
    ("same fault on the CI runner: delta 71.9", True),
    ("a threshold is a statement about a browser", True),
    ("build, so CI asserts \"delta > 20\", not 51", True),
], tint="#fdf4ef")
CANVAS = Block("the canvas every run shares", "1280x900, not the browser's default", [
    ("a region without a viewport depended on", True),
    ("the window the browser happened to have", True),
    ("attach to an 800x600 shell and it fails:", True),
    ("\"region is outside the 800x600 screenshot\"", True),
    ("-image-threshold / UITO_IMAGE_THRESHOLD", True),
    ("warns, and every note says whose number", True),
    ("it is: \"run threshold 0.60 overrides the", True),
    ("scenario's 0.90\"   <- declared, not quiet", True),
], tint="#fdf4ef")
FLAKE = Block("the flake policy, in a committed file", "state/quarantine.json", [
    ("2 whole runs failing  ->  quarantine", True),
    ("3 whole runs passing  ->  release", True),
    ("a quarantined failure does not fail the build", True),
    ("it still runs, and it is still reported", True),
], tint=TINT)
REPORT = Block("report/index.html", "one file, screenshots inlined", [
    ("pass · fail · flake · quarantined", True),
    ("what a visual failure actually saw", True),
])
# Short, because the strip gives each claim a third of the figure's width and the guard below fails
# the build rather than clipping one: the numbers are the point, and they are the part that stays.
CLAIMS = [
    "asserting at once is a race: 24 of 24 failed",
    "250 ms for a 900 ms endpoint: 43.8% flaked",
    "a red badge: 18 of 21 still passed",
]

col1 = max(CLIENT.w, RUNNER.w)
col2 = max(DRIVER.w, VISION.w, CANVAS.w)
col3 = max(FLAKE.w, REPORT.w)
MARGIN, TOP = 28, 74
x1 = MARGIN
x2 = x1 + col1 + GAP
x3 = x2 + col2 + GAP
total_w = x3 + col3 + MARGIN

client_y, runner_y = TOP, TOP + CLIENT.h + 40
driver_y, vision_y = TOP, TOP + DRIVER.h + 40
canvas_y = vision_y + VISION.h + 40
flake_y, report_y = TOP, TOP + FLAKE.h + 40
strip_y = max(runner_y + RUNNER.h, canvas_y + CANVAS.h, report_y + REPORT.h) + 42
strip_h = 84
H = strip_y + strip_h + 46

svg = [f'<svg viewBox="0 0 {total_w:.0f} {H:.0f}" role="img" aria-labelledby="ui-test-orchestrator-flow-title ui-test-orchestrator-flow-desc" xmlns="http://www.w3.org/2000/svg">']
svg.append('<title id="ui-test-orchestrator-flow-title">ui-test-orchestrator: what a scenario run does, and what each part measured</title>')
svg.append('<desc id="ui-test-orchestrator-flow-desc">A scenario file is executed by a runner that runs several at once and gives '
           'every attempt a fresh browser, because retrying on the page that failed re-runs the test in the state that broke '
           'it. The driver is written against the DevTools protocol directly: it clicks with a real mouse event at the element '
           'centre, fills by setting the value and dispatching the input event so a framework notices, answers whether an '
           'element is visible rather than merely present, and captures the pixels an assertion can read. A visual assertion '
           'reports two numbers, because correlation alone called a badge that turned red a pass at 0.999: the mean colour '
           'difference of 51.7 is what failed it. Outcomes go to a flake policy kept in a committed file, where two whole runs '
           'failing quarantine a scenario and three passing release it, and to a report that inlines its screenshots. The three '
           'measurements at the bottom are the ones this repository is built around: a check that asserts immediately is a race '
           'rather than a flake and fails 24 of 24 runs even with a retry; waiting 250 milliseconds for a 900 millisecond '
           'endpoint flakes 43.8 percent of runs while waiting 8 seconds flakes none of 168; and on a page whose status badge '
           'turned red, 18 of 21 scenarios still passed, because only the three visual ones looked.</desc>')

svg.append(f'<defs><marker id="arrowhead" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" '
           f'orient="auto-start-reverse"><path d="M 0 0 L 10 5 L 0 10 z" fill="{MUTED}"/></marker></defs>')

svg.append(CLIENT.render(x1, client_y))
svg.append(RUNNER.render(x1, runner_y))
svg.append(DRIVER.render(x2, driver_y))
svg.append(VISION.render(x2, vision_y))
svg.append(CANVAS.render(x2, canvas_y))
svg.append(FLAKE.render(x3, flake_y))
svg.append(REPORT.render(x3, report_y))

svg.append(arrow(x1 + col1, client_y + 40, x2, driver_y + 34, "steps"))
svg.append(arrow(x2 + col2, driver_y + 34, x3, flake_y + 34, "outcomes"))
svg.append(arrow(x1 + col1, runner_y + 34, x2, vision_y + 24, "attempts"))
svg.append(arrow(x2 + col2, vision_y + 24, x3, report_y + 24, "numbers"))
svg.append(text(MARGIN, H - 16, "make demo  ·  make compose-suite  ·  scripts/measure-visual.sh  ·  scripts/measure-flake.sh",
                size=11, fill=MUTED, family="mono"))
svg.append(rect(MARGIN, strip_y, total_w - MARGIN * 2, strip_h))
svg.append(text(MARGIN + PAD, strip_y + 26, "three rules this repository keeps, each one because breaking it produced a wrong number",
                size=12.5, weight="600", fill=MUTED))
chip_w = (total_w - MARGIN * 2 - PAD * 2 - 24) / 3
# A generator that clips a claim is a generator that ships a figure nobody can read, and the clipping
# is invisible in the HTML: it only shows in the export, which is the artefact people look at. This
# fails instead, naming the claim that does not fit.
for i, claim in enumerate(CLAIMS):
    need = width(f"{i + 1}. {claim}", 11.5) + 20
    if need > chip_w:
        raise SystemExit(f"claim {i + 1} needs {need:.0f}px, the strip gives {chip_w:.0f}px: shorten it")
for i, claim in enumerate(CLAIMS):
    cx = MARGIN + PAD + i * (chip_w + 12)
    svg.append(rect(cx, strip_y + 38, chip_w, 28, fill=TINT, stroke="#e8cdbd", rx=6))
    svg.append(text(cx + 10, strip_y + 57, f"{i + 1}. {claim}", size=11.5))
svg.append("</svg>")

meta = ('<!-- diagram-meta {"topic":"ui-test-orchestrator-flow","level":"runtime","type":"flowchart",'
        '"question":"What does a scenario run do, and what did each part measure?",'
        '"sources":["cmd/orchestrator/main.go","internal/scenario/scenario.go","internal/runner/runner.go",'
        '"internal/driver/cdp.go","internal/vision/vision.go","internal/flake/flake.go","internal/report/report.go",'
        '"internal/app/app.go","scenarios","scenarios-flaky","scripts/measure-visual.sh","scripts/measure-flake.sh",'
        '"scripts/demo-policy.sh","state/quarantine.json","Dockerfile","docker-compose.yml","Makefile",".github/workflows/ci.yml","README.md"],'
        '"updated":"2026-10-12"} -->')

page = f"""{meta}
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>rtc-media-bench</title>
<link href="https://fonts.googleapis.com/css2?family=Instrument+Serif:ital@0;1&family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  *, *::before, *::after {{ box-sizing: border-box; margin: 0; padding: 0; }}
  :root {{
    --color-paper: {PAPER}; --color-ink: {INK}; --color-muted: {MUTED}; --color-accent: {ACCENT};
    --font-sans: 'Geist', system-ui, sans-serif; --font-serif: 'Instrument Serif', serif;
    --font-mono: 'Geist Mono', ui-monospace, monospace;
  }}
  body {{ font-family: var(--font-sans); background: var(--color-paper); color: var(--color-ink);
         min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 3rem 2rem; }}
  .frame {{ max-width: {total_w + 40:.0f}px; width: 100%; }}
  .eyebrow {{ font-family: var(--font-mono); font-size: 0.66rem; font-weight: 500; letter-spacing: 0.18em;
              text-transform: uppercase; color: var(--color-muted); margin-bottom: 0.5rem; }}
  h1 {{ font-family: var(--font-serif); font-size: clamp(1.5rem, 2.4vw + 0.75rem, 2rem); font-weight: 400;
        letter-spacing: -0.02em; line-height: 1.15; color: var(--color-ink); margin-bottom: 1.5rem; }}
  .diagram-container {{ width: 100%; overflow-x: auto; }}
  svg {{ width: 100%; max-width: {total_w:.0f}px; display: block; }}
  .caption {{ font-family: var(--font-mono); font-size: 0.7rem; color: var(--color-muted); margin-top: 1rem; }}
</style>
</head>
<body>
<div class="frame">
  <p class="eyebrow">rtc-media-bench · runtime + fault lab</p>
  <h1>Where a call breaks, and the four faults this lab reproduces on purpose</h1>
  <div class="diagram-container">
{"".join(svg)}
  </div>
  <p class="caption">Every fault in the catalogue lives in the ICE number, not in the websocket and not in the pod
  status: signalling and media fail separately, and the tools that report "healthy" are the ones that make this class
  of fault expensive. One fault did not reproduce here at all, and the runbook says so with the four configurations
  that were tried.</p>
</div>
</body>
</html>
"""
out = pathlib.Path(__file__).with_suffix(".html").with_name("ui-test-orchestrator-flow.html")
out.write_text(page, encoding="utf-8")
print(f"wrote {out.name}: {total_w:.0f}x{H:.0f} viewBox, {len(page)} bytes")
