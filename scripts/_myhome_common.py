"""Helpers shared by myhome-ctl.py and myhome-local-daemon.py (loaded by file path: `python3 -I` drops the script directory from sys.path).

Python 3.12+, stdlib only. Not meant to be run.
"""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path

DEFAULT_BROKER = "tcp://192.168.1.2:1883"
MAIN_INSTANCE = "myhome"

# The root Makefile's `generate` target, step for step: (output that must exist, go
# generate arguments). A step is skipped only when its output exists. The Makefile ends
# with a catch-all `go generate ./...`, run here (GENERATE_ALL) whenever any step ran.
# `make` itself is not assumed to be installed. The old myhome-local.sh had only the first
# three steps, so a fresh worktree failed to build (PoolKVSKeys undefined, jstarget acorn.js).
ASSETS = [
    ("internal/myhome/ui/static/alpine.min.js", ["./internal/myhome/ui/..."]),
    ("myhome/ctl/pool/pool_defaults_generated.go", ["./myhome/ctl/pool"]),
    ("myhome/ctl/garden/garden_defaults_generated.go", ["./myhome/ctl/garden"]),
    ("internal/myhome/shelly/script/pool_kvs_generated.go", ["./internal/myhome/shelly/script"]),
    ("internal/shelly/scripts/jstarget/acorn.js", ["./internal/shelly/scripts/jstarget"]),
]
GENERATE_ALL = ["./..."]

# Environment variables a scrubbed child may see: what Go and the OS need, nothing from MYHOME_*.
ENV_ALLOWLIST = (
    "PATH", "HOME", "USERPROFILE", "SYSTEMROOT", "SystemDrive", "TEMP", "TMP", "TMPDIR",
    "LOCALAPPDATA", "APPDATA", "GOPATH", "GOCACHE", "GOMODCACHE", "GOROOT", "GOFLAGS",
    "GOPROXY", "GOTOOLCHAIN", "GOENV",
)


def broker_from(environ: dict[str, str]) -> str:
    return environ.get("MYHOME_MQTT_BROKER") or DEFAULT_BROKER


def scrubbed_env(environ: dict[str, str]) -> dict[str, str]:
    return {k: v for k, v in environ.items() if k in ENV_ALLOWLIST and not k.startswith("MYHOME_")}


def repo_root() -> Path:
    git = shutil.which("git")
    if not git:
        sys.exit("myhome: git not found in PATH")
    out = subprocess.run([git, "rev-parse", "--show-toplevel"], cwd=Path(__file__).resolve().parent,
                         capture_output=True, text=True, encoding="utf-8", check=False)
    if out.returncode != 0:
        sys.exit("myhome: not inside a git checkout: " + out.stderr.strip())
    return Path(out.stdout.strip())


def need_go() -> str:
    go = shutil.which("go")
    if not go:
        sys.exit("myhome: go not found in PATH")
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
