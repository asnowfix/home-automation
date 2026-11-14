#!/usr/bin/env python3
"""myhome-ctl.py - thin `myhome ctl` wrapper toward the MAIN myhome instance, runnable from any checkout or git worktree.

Portable (Linux, macOS, Windows; Python 3.12+, stdlib only). Run it as
`python3 -I scripts/myhome-ctl.py [ctl-args...]` (on Windows `py -3 -I scripts\\myhome-ctl.py ...`).

It runs the `go generate` steps of the root Makefile `generate` target whose output is missing
(each git worktree has its own untracked generated files), then `go run ./myhome ctl --instance
myhome --mqtt-broker <broker> <ctl-args>` instead of a pre-built binary. With no arguments it
runs `mcp`: this is what .mcp.json calls. It never touches a database and starts no daemon;
for a local daemon see myhome-local-daemon.py.

  python3 -I scripts/myhome-ctl.py --build-only   generate what is missing, compile to a temp file, exit.

MYHOME_MQTT_BROKER overrides the broker (default tcp://192.168.1.2:1883).
"""

from __future__ import annotations

import importlib.util
import os
import platform
import subprocess
import sys
import tempfile
from pathlib import Path

_spec = importlib.util.spec_from_file_location("_myhome_common", Path(__file__).resolve().with_name("_myhome_common.py"))
common = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(common)


def ctl_args(broker: str, args: list[str]) -> list[str]:
    """Argument list after `go run ./myhome`; no arguments means `mcp`."""
    return ["ctl", "--instance", common.MAIN_INSTANCE, "--mqtt-broker", broker, *(args or ["mcp"])]


def passthrough(root: Path, go: str, broker: str, args: list[str]) -> int:
    cmd = [go, "run", "./myhome", *ctl_args(broker, args)]
    if platform.system() == "Windows":
        # exec does not replace the process on Windows; inherit stdio and relay the exit code.
        return subprocess.run(cmd, cwd=root, check=False).returncode
    os.chdir(root)
    os.execvp(cmd[0], cmd)
    return 0  # unreachable


def main(argv: list[str]) -> int:
    root = common.repo_root()
    go = common.need_go()
    common.ensure_assets(root, go)
    if argv == ["--build-only"]:
        with tempfile.TemporaryDirectory() as tmp:
            return subprocess.run([go, "build", "-o", str(Path(tmp) / "myhome-build-check"), "./myhome"],
                                  cwd=root, check=False).returncode
    return passthrough(root, go, common.broker_from(dict(os.environ)), argv)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
