#!/usr/bin/env python3
"""myhome-local-daemon.py - run a LOCAL read-only myhome daemon (instance "local") seeded from the main one.

Portable (Linux, macOS, Windows; Python 3.12+, stdlib only). Run it as
`python3 -I scripts/myhome-local-daemon.py` (on Windows `py -3 -I scripts\\myhome-local-daemon.py`).
For the thin ctl/MCP wrapper toward the MAIN instance, see myhome-ctl.py.

In order:
  1. build the binary into the instance directory (after the missing `go generate` steps);
  2. delete the side instance's own myhome.db (+ -wal/-shm), inside INSTANCE_DIR only;
  3. `ctl --instance myhome db export` from the MAIN daemon over MQTT (read-only);
  4. start the local daemon with the #700 safe flags, in INSTANCE_DIR;
  5. probe until it answers, run `ctl --instance local db import <file>` exactly once (a partial
     import is reported and the daemon keeps running), then keep running until Ctrl-C.
The local daemon runs with a scrubbed environment (no MYHOME_* variables, no .env).

Broker: the local daemon has no broker of its own; it uses the home broker for the export, the
import and device traffic (owner decision on #746), with the #700 read-only flags.

MYHOME_MQTT_BROKER overrides the home broker (default tcp://192.168.1.2:1883).
"""

from __future__ import annotations

import importlib.util
import os
import platform
import subprocess
import sys
import time
from pathlib import Path

_spec = importlib.util.spec_from_file_location("_myhome_common", Path(__file__).resolve().with_name("_myhome_common.py"))
common = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(common)

MAIN_INSTANCE = common.MAIN_INSTANCE
SIDE_INSTANCE = "local"

# Flags that make the side daemon read-only toward devices and the network (#700).
SAFE_DAEMON_FLAGS = [
    "--disable-auto-setup",
    "--reconcile-interval", "0",
    "--disable-occupancy-service",
    "--disable-temperature-service",
    "--no-mdns-publish",
    "--disable-weather-publisher",
]

DB_FILES = ("myhome.db", "myhome.db-wal", "myhome.db-shm")
EXPORT_NAME = "main-export.json"
IMPORT_ATTEMPTS = 20
IMPORT_RETRY_SECONDS = 3


def instance_dir(home: Path, system: str) -> Path:
    """The side instance's working directory, derived from the home directory only.

    Nothing here comes from an environment variable that could hold an absolute path
    (XDG_STATE_HOME, LOCALAPPDATA...), so the deletion target cannot be redirected.
    """
    if system == "Windows":
        base = home / "AppData" / "Local"
    elif system == "Darwin":
        base = home / "Library" / "Application Support"
    else:
        base = home / ".local" / "state"
    return base / "myhome" / "local-side-instance"


def daemon_args(broker: str) -> list[str]:
    return ["daemon", "run", "--instance", SIDE_INSTANCE, "--mqtt-broker", broker, *SAFE_DAEMON_FLAGS]


def export_args(broker: str, out: Path) -> list[str]:
    """Read-only export from the main daemon's instance."""
    return ["ctl", "--instance", MAIN_INSTANCE, "--mqtt-broker", broker, "db", "export", "--pretty", "--output", str(out)]


def probe_args(broker: str) -> list[str]:
    """Read-only readiness probe of the side daemon: a devices-only export to stdout.

    It exits non-zero while the daemon does not answer (device lookup fails) and zero once it does,
    which an import cannot tell us: since #738 an import also exits non-zero on a *partial* import.
    """
    return ["ctl", "--instance", SIDE_INSTANCE, "--mqtt-broker", broker, "db", "export", "--devices-only"]


def import_args(broker: str, src: Path) -> list[str]:
    return ["ctl", "--instance", SIDE_INSTANCE, "--mqtt-broker", broker, "db", "import", str(src)]


def db_targets(directory: Path) -> list[Path]:
    return [directory / name for name in DB_FILES]


def delete_side_db(directory: Path, expected: Path) -> list[Path]:
    """Delete the side instance's database files and return the ones removed.

    Refuses unless `directory` is exactly `expected` (the instance directory), and
    unless every target is a plain file located directly inside it (no symlink, no
    path outside). Nothing else is ever touched.
    """
    if directory.resolve() != expected.resolve():
        raise ValueError(f"refusing to delete in {directory}: not the side instance directory {expected}")
    removed = []
    for target in db_targets(directory):
        if target.parent.resolve() != expected.resolve():
            raise ValueError(f"refusing to delete {target}: outside {expected}")
        if target.is_symlink():
            raise ValueError(f"refusing to delete {target}: it is a symbolic link")
        if target.exists():
            if not target.is_file():
                raise ValueError(f"refusing to delete {target}: not a regular file")
            target.unlink()
            removed.append(target)
    return removed


def wait_and_import(daemon, probe: list[str], import_cmd: list[str], cwd: Path, env: dict[str, str],
                    sleep=time.sleep) -> int | None:
    """Retry only the readiness probe; run the import exactly once, after the daemon answered.

    Returns None when the side daemon should keep running (import complete, or completed but
    partial: warnings and the summary line were printed by the import itself), or an exit status
    when the launcher must stop (daemon died, or never answered).
    """
    for attempt in range(1, IMPORT_ATTEMPTS + 1):
        if daemon.poll() is not None:
            print("side daemon exited before the import", file=sys.stderr)
            return daemon.returncode or 1
        ready = subprocess.run(probe, cwd=cwd, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
        if ready.returncode == 0:
            break
        print(f"      side daemon not answering yet ({attempt}/{IMPORT_ATTEMPTS}), retrying in {IMPORT_RETRY_SECONDS}s")
        sleep(IMPORT_RETRY_SECONDS)
    else:
        print("side daemon never answered; stopping it", file=sys.stderr)
        return 1
    done = subprocess.run(import_cmd, cwd=cwd, env=env, check=False)
    if done.returncode == 0:
        print("import complete")
    else:
        print(f"WARNING: import PARTIAL (exit {done.returncode}); see the warnings above. "
              "The side daemon keeps running with what was imported.", file=sys.stderr)
    return None


def run_daemon_mode(root: Path, go: str, broker: str) -> int:
    inst = instance_dir(Path(os.path.expanduser("~")), platform.system())
    inst.mkdir(parents=True, exist_ok=True)
    env = common.scrubbed_env(dict(os.environ))

    binary = inst / ("myhome-side.exe" if platform.system() == "Windows" else "myhome-side")
    print(f"[1/5] building {binary}")
    subprocess.run([go, "build", "-o", str(binary), "./myhome"], cwd=root, env=env, check=True)

    print(f"[2/5] deleting the side instance database in {inst}")
    for path in delete_side_db(inst, inst):
        print(f"      removed {path}")

    export = inst / EXPORT_NAME
    print(f"[3/5] exporting from the main daemon (instance {MAIN_INSTANCE}, read-only) to {export}")
    subprocess.run([str(binary), *export_args(broker, export)], cwd=inst, env=env, check=True)

    print("[4/5] starting the read-only side daemon (instance local)")
    daemon = subprocess.Popen([str(binary), *daemon_args(broker)], cwd=inst, env=env)
    try:
        print("[5/5] waiting for the side daemon to answer, then importing the export")
        status = wait_and_import(daemon, [str(binary), *probe_args(broker)], [str(binary), *import_args(broker, export)], inst, env)
        if status is not None:
            return status
        print("side daemon running (Ctrl-C to stop)")
        return daemon.wait()
    except KeyboardInterrupt:
        return 0
    finally:
        if daemon.poll() is None:
            daemon.terminate()
            try:
                daemon.wait(timeout=15)
            except subprocess.TimeoutExpired:
                daemon.kill()


def main(argv: list[str]) -> int:
    if argv:
        sys.exit("myhome-local-daemon: takes no arguments (MYHOME_MQTT_BROKER overrides the home broker)")
    root = common.repo_root()
    go = common.need_go()
    common.ensure_assets(root, go)
    return run_daemon_mode(root, go, common.broker_from(dict(os.environ)))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
