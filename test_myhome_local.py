#!/usr/bin/env python3
"""Tests for myhome-local.py: run with `python3 -I -m unittest test_myhome_local` (no network, no daemon)."""

import importlib.util
import tempfile
import unittest
from pathlib import Path

_spec = importlib.util.spec_from_file_location("myhome_local", Path(__file__).resolve().parent / "myhome-local.py")
ml = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(ml)

BROKER = "tcp://192.0.2.1:1883"


class PathSafety(unittest.TestCase):
    def test_instance_dir_comes_from_home_only(self):
        home = Path("/home/u")
        self.assertEqual(ml.instance_dir(home, "Linux"), home / ".local/state/myhome/local-side-instance")
        self.assertEqual(ml.instance_dir(home, "Darwin"), home / "Library/Application Support/myhome/local-side-instance")
        self.assertEqual(ml.instance_dir(home, "Windows"), home / "AppData/Local/myhome/local-side-instance")

    def test_deletes_only_db_files_in_instance_dir(self):
        with tempfile.TemporaryDirectory() as tmp:
            inst = Path(tmp) / "inst"
            inst.mkdir()
            for name in (*ml.DB_FILES, "keep.json", "events.db"):
                (inst / name).write_text("x", encoding="utf-8")
            removed = ml.delete_side_db(inst, inst)
            self.assertEqual({p.name for p in removed}, set(ml.DB_FILES))
            self.assertTrue((inst / "keep.json").exists())
            self.assertTrue((inst / "events.db").exists())

    def test_refuses_other_directory(self):
        with tempfile.TemporaryDirectory() as tmp:
            inst, other = Path(tmp) / "inst", Path(tmp) / "other"
            inst.mkdir()
            other.mkdir()
            (other / "myhome.db").write_text("prod", encoding="utf-8")
            with self.assertRaises(ValueError):
                ml.delete_side_db(other, inst)
            self.assertTrue((other / "myhome.db").exists())

    def test_refuses_parent_traversal(self):
        with tempfile.TemporaryDirectory() as tmp:
            inst = Path(tmp) / "inst"
            inst.mkdir()
            (Path(tmp) / "myhome.db").write_text("other", encoding="utf-8")
            with self.assertRaises(ValueError):
                ml.delete_side_db(inst / "..", inst)
            self.assertTrue((Path(tmp) / "myhome.db").exists())

    def test_refuses_symlink_and_directory_targets(self):
        with tempfile.TemporaryDirectory() as tmp:
            inst, outside = Path(tmp) / "inst", Path(tmp) / "outside.db"
            inst.mkdir()
            outside.write_text("precious", encoding="utf-8")
            try:
                (inst / "myhome.db").symlink_to(outside)
            except OSError:
                self.skipTest("symlinks unavailable")
            with self.assertRaises(ValueError):
                ml.delete_side_db(inst, inst)
            self.assertTrue(outside.exists())
            (inst / "myhome.db").unlink()
            (inst / "myhome.db").mkdir()
            with self.assertRaises(ValueError):
                ml.delete_side_db(inst, inst)


class Arguments(unittest.TestCase):
    def test_passthrough_defaults_to_mcp(self):
        self.assertEqual(ml.ctl_args(BROKER, []), ["ctl", "--instance", "local", "--mqtt-broker", BROKER, "mcp"])

    def test_passthrough_keeps_arguments(self):
        self.assertEqual(ml.ctl_args(BROKER, ["shelly", "list"])[-2:], ["shelly", "list"])

    def test_broker_override(self):
        self.assertEqual(ml.broker_from({}), "tcp://192.168.1.2:1883")
        self.assertEqual(ml.broker_from({"MYHOME_MQTT_BROKER": BROKER}), BROKER)

    def test_daemon_has_exactly_the_safe_flags(self):
        args = ml.daemon_args(BROKER)
        self.assertEqual(args[:6], ["daemon", "run", "--instance", "local", "--mqtt-broker", BROKER])
        self.assertEqual(args[6:], ["--disable-auto-setup", "--reconcile-interval", "0", "--disable-occupancy-service",
                                    "--disable-temperature-service", "--no-mdns-publish"])

    def test_export_targets_main_instance_and_import_the_side_one(self):
        out = Path("/x/e.json")
        self.assertIn("myhome", ml.export_args(BROKER, out)[2:4])
        self.assertEqual(ml.export_args(BROKER, out)[-2:], ["--output", str(out)])
        self.assertEqual(ml.import_args(BROKER, out)[:3], ["ctl", "--instance", "local"])
        self.assertEqual(ml.import_args(BROKER, out)[-3:], ["db", "import", str(out)])

    def test_environment_is_scrubbed(self):
        env = ml.scrubbed_env({"PATH": "/bin", "MYHOME_MQTT_BROKER": "x", "MYHOME_DB": "y", "HOME": "/h", "SECRET": "s"})
        self.assertEqual(env, {"PATH": "/bin", "HOME": "/h"})


if __name__ == "__main__":
    unittest.main()
