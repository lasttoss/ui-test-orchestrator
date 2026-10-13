# Measurements

The README's claims are about flakiness, and flakiness is a property of a run: the tables below are
the record, with the run counts next to the rates.

## Environment

| | |
|---|---|
| Machine | 12-core x86-64 (AMD64), Ubuntu 24.04, Docker 29.1.3 |
| Toolchain | Go 1.27.2 / Node 24.19.0 / JDK 17 in `maven:3.9-eclipse-temurin-17` |
| Date | 2026-10-13 (UTC) |


## Coverage

```
$ make coverage
total:  (statements)  66.4%
```

## A race, measured two ways

Against a fixture whose events endpoint takes 900 ms every third request:

| how the check is written | attempts | scenario runs | failed | flaked |
|---|---|---|---|---|
| assert immediately after the click | 1 | 24 | **100%** | 0% |
| wait with `assert_eventually` | 1 | 126 | 0% | 0% |
| assert immediately, one retry | 2 | 24 | **100%** | 0% |
| wait, one retry | 2 | 126 | 0% | 0% |

| waiting for a 900 ms endpoint | scenario runs | failed | flaked |
|---|---|---|---|
| 250 ms | 16 | 0% | **43.8%** |
| 8 s | 168 | 0% | 0% |

```
$ make up && make policy       # six runs against a broken server, then a healthy one
run 1 against the broken server    failing scenarios: 1   exit code: 1
run 2 against the broken server    failing scenarios: 1   quarantined now: [smoke-title]   exit code: 0
run 3 against the broken server    failing scenarios: 1   exit code: 0
run 4-6 against a healthy server   released after three passes in a row
```

**Result.** The distinction the tables exist for: the naive check does not fail *sometimes*, it fails
**always** (24 of 24 runs, and 24 of 24 with a retry), because the browser has not had a turn yet. A
retry does not fix a race, it makes it cheaper to ignore. What is actually flaky is a timeout that is
too short - 43.8% of 16 runs - and it goes to 0 of 168 runs when the wait is long enough.

**Reproducing these tables** needs the lab stack (`make up`: a browser sidecar and a fixture app) and
takes about half an hour for the 168-run row. The numbers above are the committed record; the command
next to each table is the one that produced it.
