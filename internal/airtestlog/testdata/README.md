# Where these logs come from

Both were written by Airtest 1.4.3 itself — `airtest.utils.logwraper.AirtestLogger`, the same writer the
framework uses — not typed by hand. Airtest writes `log.txt` as one JSON object per line, and the field
names here are the ones `Logwrap` puts into its own dict (`name`, `call_args`, `start_time`, `end_time`,
`ret`, and `traceback` instead of `ret` when the wrapped function raises).

| file | how it was produced |
|---|---|
| `airtest-log.txt` | a script using the real `@logwrap` path (`Logwrap`), so the timestamps are live and the tracebacks are the ones Python produced |
| `airtest-log-fixed.txt` | the same writer with a frozen clock, so the durations are exact (250 ms, 500 ms, 750 ms, 1900 ms) and a test can assert them |
| `airtest-log-malformed.txt` | hand-written on purpose: one good line, one line that is not JSON, one good line after it |

Regenerate with Airtest installed in a throwaway directory:

```bash
pip install --target /tmp/at-libs airtest
PYTHONPATH=/tmp/at-libs python3 generate-fixtures.py
```
