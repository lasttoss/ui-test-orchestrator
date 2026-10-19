# Provenance

Clean-room: written here, from public documentation, against no code from any employer.

| what | where it came from |
|---|---|
| the DevTools protocol client | the protocol as published at `chromadb.net/devtools-protocol`-style DevTools documentation; written against `Target.attachToTarget`, `Runtime.evaluate`, `Input.dispatchMouseEvent` and `Page.captureScreenshot` |
| the Chrome launch flags | Chromium's own `--help` and the well-documented container requirements (`--no-sandbox`, `--disable-dev-shm-usage`) |
| template matching | normalised cross-correlation, a textbook method; the pyramid, the top-k refinement and the sampled-SAD fallback were arrived at here, after a test showed a coarse search choosing the wrong peak on a textured page |
| scenario schema | this repository's own; YAML parsing by `gopkg.in/yaml.v3` |
| adb interaction | the documented `adb shell uiautomator dump`, `input tap`, `input text` and `exec-out screencap -p` interfaces |
| the fixture application | written here; no company page, no production data, no real names |
| the app under test | a fixture served by this repository. The same suites can be pointed at any URL, and were run against a personal public project of the author's to check that the driver works on a page nobody wrote for it |

| the Airtest log format | Airtest is Apache-2.0 and its log format is public: one JSON object per line, `{"tag", "depth", "time", "data"}`, and its failure rule is in `airtest/report/report.py` (a step whose data carries a traceback failed; the run's own verdict comes from the last row). Both fixtures under `internal/airtestlog/testdata/` were written by Airtest 1.4.3's own writer, installed from PyPI into a throwaway directory; `testdata/generate-fixtures.py` reproduces them |
| the airtest report's design | this repository's own: the summary counts, the case grouping, and the note printed when this reader and `airtest report` would disagree. No Airtest template or CSS was copied; `internal/airtestlog` renders from its own HTML |

Nothing was copied from an internal repository, an internal chart, or a company test suite.
