#!/usr/bin/env python3
"""Regenerate the log fixtures beside this file, using Airtest's own log writer.

    pip install --target /tmp/at-libs airtest
    PYTHONPATH=/tmp/at-libs python3 internal/airtestlog/testdata/generate-fixtures.py

Both files are written by airtest.utils.logwraper.AirtestLogger. airtest-log.txt goes through the real
Logwrap path, so its timestamps are live; airtest-log-fixed.txt uses the same writer with a frozen
clock, so the Go tests can assert exact durations. Field names come from Logwrap's own dict updates.
"""
import pathlib
import sys

try:
    from airtest.utils.logwraper import AirtestLogger, Logwrap
    from airtest.core.helper import G
except ImportError:
    sys.exit("airtest is not importable; see the docstring for the two commands this needs")

HERE = pathlib.Path(__file__).resolve().parent

# ---------------------------------------------------------------- the real code path
logger = AirtestLogger(str(HERE / "airtest-log.txt"))
G.LOGGER = logger


def wrap(f):
    return Logwrap(f, logger)


@wrap
def open_shop(**kwargs):
    logger.log("log", {"msg": "tap the shop icon", "desc": "open the shop"})


@wrap
def buy_pack(**kwargs):
    logger.log("log", {"msg": "tap the first pack", "desc": "select the 4.99 pack"})
    raise AssertionError("balance did not grow by 500")


@wrap
def test_healthcheck(**kwargs):
    logger.log("log", {"msg": "server answers 200", "desc": "healthcheck"})


@wrap
def test_purchase(**kwargs):
    open_shop()
    buy_pack()


for case in (test_healthcheck, test_purchase):
    try:
        case()
    except AssertionError:
        pass
logger.handle_stacked_log()

# ------------------------------------------------- the same shapes with a frozen clock
frozen = AirtestLogger(str(HERE / "airtest-log-fixed.txt"))
T0 = 1792380914.0


def entry(tag, data, depth, seconds):
    frozen.log(tag, data, depth=depth, timestamp=seconds)


entry("log", {"msg": "server answers 200", "desc": "healthcheck"}, 1, T0 + 0.10)
entry("function", {"name": "test_healthcheck", "call_args": {"kwargs": {}},
                   "start_time": T0, "end_time": T0 + 0.25, "ret": None}, 1, T0 + 0.25)
entry("log", {"msg": "tap the shop icon", "desc": "open the shop"}, 2, T0 + 1.10)
entry("function", {"name": "open_shop", "call_args": {"kwargs": {}},
                   "start_time": T0 + 1.0, "end_time": T0 + 1.5, "ret": None}, 2, T0 + 1.50)
entry("log", {"msg": "tap the first pack", "desc": "select the 4.99 pack"}, 2, T0 + 2.10)
entry("function", {"name": "buy_pack", "call_args": {"kwargs": {}}, "start_time": T0 + 2.0, "end_time": T0 + 2.75,
                   "traceback": "Traceback (most recent call last):\n"
                                "  File \"tc.air\", line 12, in buy_pack\n"
                                "AssertionError: balance did not grow by 500\n"}, 2, T0 + 2.75)
entry("touch", {"pos": [640, 1200], "desc": "tap the buy button"}, 2, T0 + 2.80)
entry("function", {"name": "test_purchase", "call_args": {"kwargs": {}}, "start_time": T0 + 1.0, "end_time": T0 + 2.9,
                   "traceback": "Traceback (most recent call last):\n"
                                "  File \"tc.air\", line 30, in test_purchase\n"
                                "  File \"tc.air\", line 12, in buy_pack\n"
                                "AssertionError: balance did not grow by 500\n"}, 1, T0 + 2.9)
frozen.handle_stacked_log()
print("wrote airtest-log.txt and airtest-log-fixed.txt into", HERE)
