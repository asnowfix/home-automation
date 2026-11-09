#!/usr/bin/env python3
"""Measure the test suite's worst-case wall clock and check TESTTIMEOUT against it.

The scripts packages are wait-bound (emulator timers), not CPU-bound, and Go's
-parallel defaults to GOMAXPROCS, so core count used to decide their duration
(#748). With -parallel pinned explicitly (TESTPARALLEL) the duration is nearly
independent of the machine. This tool pins the run to ONE CPU, the worst case,
runs the packages, and compares the slowest package against TESTTIMEOUT, so the
timeout can be re-derived from a short run instead of a full serial one.

Usage: python3 -I tools/timeout_check.py TESTTIMEOUT TESTPARALLEL [go test flags...] -- [packages...]
"""
import math
import os
import re
import shutil
import subprocess
import sys

OK_RE = re.compile(r"^ok\s+(\S+)\s+([\d.]+)s", re.M)
FAIL_MARGIN = 1.10  # CLAUDE.md rule: measured + 10% is the floor
WARN_MARGIN = 2.0  # below this the timeout leaves little room for a slower runner


def parse_seconds(value: str) -> float:
    m = re.fullmatch(r"([\d.]+)(s|m)?", value.strip())
    if not m:
        raise SystemExit(f"cannot parse TESTTIMEOUT {value!r} (expected e.g. 120s)")
    return float(m.group(1)) * (60 if m.group(2) == "m" else 1)


def main() -> int:
    if len(sys.argv) < 3 or "--" not in sys.argv:
        print(__doc__)
        return 2
    sep = sys.argv.index("--")
    timeout_s = parse_seconds(sys.argv[1])
    parallel = sys.argv[2]
    flags = sys.argv[3:sep]
    packages = sys.argv[sep + 1 :] or ["./internal/shelly/scripts/..."]

    go = os.environ.get("GO", "go")
    # Measure with a generous guard so a slow run is reported, not cut off.
    cmd = [go, "test", *flags, f"-parallel={parallel}", "-count=1", "-timeout=30m", *packages]
    taskset = shutil.which("taskset")
    if taskset:
        cmd = [taskset, "-c", "0", *cmd]
        print("measuring pinned to ONE CPU (taskset -c 0)")
    else:
        print("WARNING: taskset not found; measuring on all CPUs, which understates the worst case")
    print("+", " ".join(cmd), flush=True)
    proc = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8")
    out = proc.stdout + proc.stderr
    if proc.returncode != 0:
        print(out[-4000:])
        print("FAIL: the measured run did not succeed; only a successful run may set the timeout")
        return 1
    times = {pkg: float(sec) for pkg, sec in OK_RE.findall(out)}
    if not times:
        print("FAIL: no 'ok <pkg> <seconds>' lines found")
        return 1
    for pkg, sec in sorted(times.items(), key=lambda kv: -kv[1]):
        print(f"  {sec:8.3f}s  {pkg}")
    slowest_pkg, slowest = max(times.items(), key=lambda kv: kv[1])
    suggested = math.ceil(slowest * FAIL_MARGIN)
    print(f"slowest: {slowest:.3f}s ({slowest_pkg}); measured+10% = {suggested}s; TESTTIMEOUT = {timeout_s:.0f}s")
    if slowest * FAIL_MARGIN > timeout_s:
        print(f"FAIL: TESTTIMEOUT is below measured+10%. Raise it to at least {suggested}s (see CLAUDE.md 'Test timeout')")
        return 1
    if slowest * WARN_MARGIN > timeout_s:
        print(f"WARN: TESTTIMEOUT is under {WARN_MARGIN:.0f}x the one-CPU measurement; consider raising it")
    else:
        print(f"OK: TESTTIMEOUT is {timeout_s / slowest:.1f}x the one-CPU measurement")
    return 0


if __name__ == "__main__":
    sys.exit(main())
