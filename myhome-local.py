#!/usr/bin/env python3
"""myhome-local.py - bootstrap and run myhome from a local checkout (main tree or any git worktree).

Portable replacement for myhome-local.sh (Linux, macOS, Windows; Python 3.12+, stdlib only).
Run it as `python3 -I myhome-local.py ...` (on Windows `py -3 -I myhome-local.py ...`).

It first generates the gitignored assets/code that `go build`/`go run` need (UI static
assets, pool/garden defaults), skipping each step whose output already exists. Each git
worktree has its own untracked files, so a fresh worktree needs them regenerated.

Modes:

  python3 -I myhome-local.py [ctl-args...]
      Passthrough (default `mcp`): runs `go run ./myhome ctl --instance local
      --mqtt-broker <broker> <ctl-args>`. This is what .mcp.json calls.

  python3 -I myhome-local.py --build-only
      Generate what is missing, then `go build` into a temporary file and exit (no run).

  python3 -I myhome-local.py daemon
      A read-only side daemon (instance "local") seeded with a copy of the main daemon's
      database. In order:
        1. delete the side instance's own myhome.db (+ -wal/-shm), inside INSTANCE_DIR only;
        2. `ctl --instance myhome db export` from the MAIN daemon over MQTT (read-only);
        3. start the side daemon with the #700 safe flags, in INSTANCE_DIR;
        4. `ctl --instance local db import <file>` once it answers. Import goes over RPC
           to a running daemon, which is why the daemon is started before the import;
        5. keep running until Ctrl-C.
      The side daemon runs with a scrubbed environment (no MYHOME_* variables, no .env).

MYHOME_MQTT_BROKER overrides the broker (default tcp://192.168.1.2:1883).
"""

from __future__ import annotations

import os
import platform
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

DEFAULT_BROKER = "tcp://192.168.1.2:1883"
MAIN_INSTANCE = "myhome"
SIDE_INSTANCE = "local"

# Flags that make the side daemon read-only toward devices and the network (#700).
SAFE_DAEMON_FLAGS = [
    "--disable-auto-setup",
    "--reconcile-interval", "0",
    "--disable-occupancy-service",
    "--disable-temperature-service",
    "--no-mdns-publish",
]

# The root Makefile's `generate` target, step for step: (output that must exist, go
# generate arguments). A step is skipped only when its output exists. The Makefile ends
# with a catch-all `go generate ./...`, run here (GENERATE_ALL) whenever any step ran.
# `make` itself is not assumed to be installed. myhome-local.sh had only the first three
# steps, so a fresh worktree failed to build (PoolKVSKeys undefined, jstarget acorn.js).
ASSETS = [
    ("internal/myhome/ui/static/alpine.min.js", ["./internal/myhome/ui/..."]),
    ("myhome/ctl/pool/pool_defaults_generated.go", ["./myhome/ctl/pool"]),
    ("myhome/ctl/garden/garden_defaults_generated.go", ["./myhome/ctl/garden"]),
    ("internal/myhome/shelly/script/pool_kvs_generated.go", ["./internal/myhome/shelly/script"]),
    ("internal/shelly/scripts/jstarget/acorn.js", ["./internal/shelly/scripts/jstarget"]),
]
GENERATE_ALL = ["./..."]

# Environment variables the side instance may see: what Go and the OS need, nothing
# from MYHOME_*. Everything else is dropped.
ENV_ALLOWLIST = (
    "PATH", "HOME", "USERPROFILE", "SYSTEMROOT", "SystemDrive", "TEMP", "TMP", "TMPDIR",
    "LOCALAPPDATA", "APPDATA", "GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "GOFLAGS",
    "GOPROXY", "GOTOOLCHAIN", "GOENV",
)

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


def broker_from(environ: dict[str, str]) -> str:
    return environ.get("MYHOME_MQTT_BROKER") or DEFAULT_BROKER


def ctl_args(broker: str, args: list[str]) -> list[str]:
    """Argument list of the passthrough mode (after `go run ./myhome`)."""
    return ["ctl", "--instance", SIDE_INSTANCE, "--mqtt-broker", broker, *(args or ["mcp"])]


def daemon_args(broker: str) -> list[str]:
    return ["daemon", "run", "--instance", SIDE_INSTANCE, "--mqtt-broker", broker, *SAFE_DAEMON_FLAGS]


def export_args(broker: str, out: Path) -> list[str]:
    """Read-only export from the main daemon's instance."""
    return ["ctl", "--instance", MAIN_INSTANCE, "--mqtt-broker", broker, "db", "export", "--pretty", "--output", str(out)]


def import_args(broker: str, src: Path) -> list[str]:
    return ["ctl", "--instance", SIDE_INSTANCE, "--mqtt-broker", broker, "db", "import", str(src)]


def scrubbed_env(environ: dict[str, str]) -> dict[str, str]:
    return {k: v for k, v in environ.items() if k in ENV_ALLOWLIST and not k.startswith("MYHOME_")}


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


def repo_root() -> Path:
    git = shutil.which("git")
    if not git:
        sys.exit("myhome-local: git not found in PATH")
    out = subprocess.run([git, "rev-parse", "--show-toplevel"], cwd=Path(__file__).resolve().parent,
                         capture_output=True, text=True, encoding="utf-8", check=False)
    if out.returncode != 0:
        sys.exit("myhome-local: not inside a git checkout: " + out.stderr.strip())
    return Path(out.stdout.strip())


def need_go() -> str:
    go = shutil.which("go")
    if not go:
        sys.exit("myhome-local: go not found in PATH")
    return go


def missing_steps(root: Path) -> list[list[str]]:
    """The `go generate` argument lists whose output is absent, in Makefile order."""
    steps = [gen_args for output, gen_args in ASSETS if not (root / output).is_file()]
    if steps:
        steps.append(GENERATE_ALL)
    return steps


def ensure_assets(root: Path, go: str) -> None:
    for gen_args in missing_steps(root):
        subprocess.run([go, "generate", *gen_args], cwd=root, check=True)


def passthrough(root: Path, go: str, broker: str, args: list[str]) -> None:
    cmd = [go, "run", "./myhome", *ctl_args(broker, args)]
    if platform.system() == "Windows":
        # exec does not replace the process on Windows; inherit stdio and relay the exit code.
        sys.exit(subprocess.run(cmd, cwd=root, check=False).returncode)
    os.chdir(root)
    os.execvp(cmd[0], cmd)


def run_daemon_mode(root: Path, go: str, broker: str) -> int:
    inst = instance_dir(Path(os.path.expanduser("~")), platform.system())
    inst.mkdir(parents=True, exist_ok=True)
    env = scrubbed_env(dict(os.environ))

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
        print("[5/5] importing the export into the side daemon (retrying until it answers)")
        for attempt in range(1, IMPORT_ATTEMPTS + 1):
            if daemon.poll() is not None:
                print("side daemon exited before the import", file=sys.stderr)
                return daemon.returncode or 1
            done = subprocess.run([str(binary), *import_args(broker, export)], cwd=inst, env=env, check=False)
            if done.returncode == 0:
                break
            print(f"      import attempt {attempt}/{IMPORT_ATTEMPTS} failed, retrying in {IMPORT_RETRY_SECONDS}s")
            time.sleep(IMPORT_RETRY_SECONDS)
        else:
            print("import never succeeded; stopping the side daemon", file=sys.stderr)
            return 1
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
    root = repo_root()
    go = need_go()
    broker = broker_from(dict(os.environ))
    ensure_assets(root, go)
    if argv == ["--build-only"]:
        # Proof that a fresh worktree compiles after generation; runs nothing.
        with tempfile.TemporaryDirectory() as tmp:
            return subprocess.run([go, "build", "-o", str(Path(tmp) / "myhome-build-check"), "./myhome"],
                                  cwd=root, env=scrubbed_env(dict(os.environ)), check=False).returncode
    if argv and argv[0] == "daemon":
        if len(argv) > 1:
            sys.exit("myhome-local: daemon mode takes no further arguments")
        return run_daemon_mode(root, go, broker)
    passthrough(root, go, broker, argv)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
