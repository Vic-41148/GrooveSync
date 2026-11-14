#!/usr/bin/env python3
"""Tests for the UX-4 benchmark gate."""

import unittest

from check_benchmarks import check, percentile


def report(runs, frames, emulator=False):
    fingerprint = "google/sdk_gphone64_x86_64/emu64xa:15" if emulator else "vendor/device/phone:15"
    model = "sdk_gphone64_x86_64" if emulator else "Pixel 8"
    return {
        "context": {"build": {"fingerprint": fingerprint, "model": model}},
        "benchmarks": [
            {
                "name": "coldStartup",
                "metrics": {"timeToInitialDisplayMs": {"runs": runs}},
            },
            {
                "name": "libraryScroll",
                "sampledMetrics": {"frameOverrunMs": {"runs": [frames]}},
            },
        ],
    }


class CheckBenchmarksTest(unittest.TestCase):
    def test_passes_within_budget(self):
        data = report([1000.0, 1100.0, 1150.0, 1000.0, 1050.0], [0.0, 0.0, -1.0])

        self.assertEqual(0, check(data, allow_emulator=False))

    def test_fails_latency_budget(self):
        data = report([1300.0, 1500.0, 1600.0, 1400.0, 3500.0], [0.0])

        self.assertEqual(1, check(data, allow_emulator=False))

    def test_fails_jank_budget(self):
        frames = [0.0] * 90 + [10.0] * 10
        data = report([1000.0], frames)

        self.assertEqual(1, check(data, allow_emulator=False))

    def test_refuses_an_emulator(self):
        data = report([1000.0], [0.0], emulator=True)

        self.assertEqual(3, check(data, allow_emulator=False))
        self.assertEqual(0, check(data, allow_emulator=True))

    def test_percentiles_use_nearest_rank(self):
        runs = [10.0, 20.0, 30.0, 40.0, 50.0]

        self.assertEqual(30.0, percentile(runs, 50))
        self.assertEqual(50.0, percentile(runs, 90))


if __name__ == "__main__":
    unittest.main()
