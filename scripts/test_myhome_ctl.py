#!/usr/bin/env python3
"""Tests for myhome-ctl.py and _myhome_common.py: run with `python3 -I -m unittest discover -s scripts -p 'test_*.py'` (no network, no daemon)."""

import importlib.util
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


ctl = load("myhome_ctl", "myhome-ctl.py")
common = ctl.common

BROKER = "tcp://192.0.2.1:1883"


class Generation(unittest.TestCase):
    def test_steps_follow_the_makefile_and_skip_existing_outputs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self.assertEqual(common.missing_steps(root), [g for _, g in common.ASSETS] + [common.GENERATE_ALL])
            for output, _ in common.ASSETS:
                (root / output).parent.mkdir(parents=True, exist_ok=True)
                (root / output).write_text("x", encoding="utf-8")
            self.assertEqual(common.missing_steps(root), [])
            (root / common.ASSETS[3][0]).unlink()
            self.assertEqual(common.missing_steps(root), [common.ASSETS[3][1], common.GENERATE_ALL])

    def test_makefile_generate_steps_are_all_covered(self):
        makefile = (HERE.parent / "Makefile").read_text(encoding="utf-8")
        block = makefile.split("\ngenerate:\n", 1)[1].split("\n\n", 1)[0]
        wanted = [line.split("$(GO) generate ", 1)[1].strip() for line in block.splitlines() if "$(GO) generate" in line]
        have = [" ".join(g) for _, g in common.ASSETS] + [" ".join(common.GENERATE_ALL)]
        self.assertEqual(have, wanted)


class Arguments(unittest.TestCase):
    def test_defaults_to_mcp_toward_main_instance(self):
        self.assertEqual(ctl.ctl_args(BROKER, []), ["ctl", "--instance", "myhome", "--mqtt-broker", BROKER, "mcp"])

    def test_keeps_arguments(self):
        self.assertEqual(ctl.ctl_args(BROKER, ["shelly", "list"])[-2:], ["shelly", "list"])

    def test_broker_override(self):
        self.assertEqual(common.broker_from({}), "tcp://192.168.1.2:1883")
        self.assertEqual(common.broker_from({"MYHOME_MQTT_BROKER": BROKER}), BROKER)

    def test_environment_is_scrubbed(self):
        env = common.scrubbed_env({"PATH": "/bin", "MYHOME_MQTT_BROKER": "x", "MYHOME_DB": "y", "HOME": "/h", "SECRET": "s"})
        self.assertEqual(env, {"PATH": "/bin", "HOME": "/h"})


if __name__ == "__main__":
    unittest.main()
