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

Nothing was copied from an internal repository, an internal chart, or a company test suite.
