#!/usr/bin/env python3
"""Regression test for claude-quota's window matching (commit 106a940).

The usage endpoint gives a window's reset time a different fraction of a second at each
reading. learn() and refilled() once required equal reset times, skipped nearly every pair of
readings, and left the fitted rates at their starting guesses. These tests feed them readings
whose reset times differ by fractions of a second and need the readings to count.

Hermetic: the script is loaded from its chezmoi source path (the hyphen and the executable_
prefix rule out a plain import) with XDG_STATE_HOME pointed at a scratch dir. Its module level
only defines constants and functions; nothing reads the network, the keychain or the real log.
Run by .githooks/validate; also runnable alone: python3 .githooks/tests/test_claude_quota.py
"""

import importlib.util
import os
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "dot_config/claude-quota/executable_claude-quota.py"

_state = tempfile.TemporaryDirectory()
os.environ["XDG_STATE_HOME"] = _state.name
spec = importlib.util.spec_from_file_location("claude_quota", SCRIPT)
cq = importlib.util.module_from_spec(spec)
sys.modules["claude_quota"] = cq
spec.loader.exec_module(cq)

T0 = 1_793_670_000.0
FIVE_RESET = 1_793_674_200
WEEK_RESET = 1_794_200_000


def snap(i: int, five: int, week: int, jitter: float) -> dict:
    """The i-th reading, an hour after the one before; both reset times carry a fraction of a second."""
    return {"t": T0 + 3600 * i, "kind": "snap", "five": five, "week": week,
            "five_reset": FIVE_RESET + jitter, "week_reset": WEEK_RESET + jitter}


def log_with_agent() -> list[dict]:
    """Eight readings of one window, with a fraction of a second on the reset times as the endpoint
    gives. An Opus agent runs through the first four hours: 3.6M tokens while the 5-hour window
    rose 72 points, so 20 points per Mtok where the starting guess is 14. The window stays put in
    the last three readings, which keeps the fit from explaining the rise as per-hour baseline."""
    run = 4
    jitters = [0.46, 0.79, 0.12, 0.93, 0.31, 0.68, 0.05, 0.84]
    snaps = [snap(i, 18 * min(i, run), 4 * min(i, run), j) for i, j in enumerate(jitters)]
    return [
        {"t": T0, "kind": "start", "id": "a1", "label": "x", "model": "opus", "task": "implement"},
        *snaps,
        {"t": T0 + 3600 * run, "kind": "end", "id": "a1", "tokens": 3_600_000, "outcome": "merged"},
    ]


class SameWindow(unittest.TestCase):
    def test_fraction_of_a_second_is_one_window(self):
        a, b = snap(0, 0, 0, 0.46), snap(1, 8, 1, 0.79)
        self.assertTrue(cq.same_window(a, b, "five"))
        self.assertTrue(cq.same_window(a, b, "week"))

    def test_next_window_is_another(self):
        a = snap(0, 90, 30, 0.46)
        b = {**snap(1, 2, 31, 0.79), "five_reset": FIVE_RESET + 5 * 3600 + 0.79}
        self.assertFalse(cq.same_window(a, b, "five"))
        self.assertTrue(cq.same_window(a, b, "week"))


class Refilled(unittest.TestCase):
    def test_drop_with_jittered_reset_is_a_refill(self):
        self.assertTrue(cq.refilled(snap(0, 50, 20, 0.46), snap(1, 3, 1, 0.79), "five"))

    def test_rise_is_not(self):
        self.assertFalse(cq.refilled(snap(0, 3, 1, 0.46), snap(1, 50, 20, 0.79), "five"))

    def test_drop_into_a_new_window_is_not(self):
        b = {**snap(1, 3, 20, 0.79), "five_reset": FIVE_RESET + 5 * 3600 + 0.79}
        self.assertFalse(cq.refilled(snap(0, 50, 20, 0.46), b, "five"))


class Learn(unittest.TestCase):
    def test_jittered_readings_move_the_rate_off_its_prior(self):
        m = cq.learn(log_with_agent())
        prior = cq.PRIOR["five"]["opus"]
        self.assertGreater(m["rates"]["five"]["opus"], prior + 1,
                           "5h rate stayed near its starting guess: readings were skipped")
        self.assertLess(m["rates"]["five"]["opus"], 20 + 0.5)
        self.assertEqual(m["agents"], 1)

    def test_week_is_derived_from_the_five_hour_rates(self):
        # 40 points of 5h movement clears RATIO_MIN_FIVE only if every jittered pair is counted.
        m = cq.learn(log_with_agent())
        self.assertTrue(m["derived"])


if __name__ == "__main__":
    unittest.main()
