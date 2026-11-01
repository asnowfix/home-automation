#!/usr/bin/env python3
"""Tests for the credential prompts of postinst.sh (#629).

Run: python3 -I linux/debian/postinst_test.py   (also run by `make test`)

The script is driven on a real pty (it checks `[ -t 0 ]`), against a temporary
root via the MYHOME_POSTINST_ROOT seam, with dummy values only. Assertions
compare values and report key names, never the values themselves.
"""
import os
import pty
import select
import stat
import subprocess
import tempfile
import time
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPT = os.path.join(HERE, "postinst.sh")

SECRETS = [
    "MYHOME_BEEM_PASSWORD",
    "MYHOME_SFR_PASSWORD",
    "MYHOME_SMTP_PASSWORD",
]
SEED = {
    "MYHOME_BEEM_EMAIL": "beem@example.invalid",
    "MYHOME_BEEM_PASSWORD": "dummy-beem-old",
    "MYHOME_SFR_USERNAME": "admin",
    "MYHOME_SFR_PASSWORD": "dummy-sfr-old",
    "MYHOME_SMTP_USERNAME": "smtp@example.invalid",
    "MYHOME_SMTP_PASSWORD": "dummy-smtp-old",
    "MYHOME_SMTP_FROM": "from@example.invalid",
    "MYHOME_SMTP_TO": "to@example.invalid",
}
# Prompt labels, in the order the script asks them.
PROMPTS = [
    ("Beem email", None),
    ("Beem password", "MYHOME_BEEM_PASSWORD"),
    ("SFR username", None),
    ("SFR password", "MYHOME_SFR_PASSWORD"),
    ("SMTP From", None),
    ("SMTP To", None),
    ("SMTP username", None),
    ("SMTP password", "MYHOME_SMTP_PASSWORD"),
]


def parse_env(path):
    out = {}
    with open(path) as f:
        for line in f:
            if line.startswith("#") or "=" not in line:
                continue
            k, v = line.rstrip("\n").split("=", 1)
            out[k] = v
    return out


def run_interactive(root, steps, timeout=20):
    """steps: list of (expected prompt substring, answer). Returns the exit code."""
    master, slave = pty.openpty()
    env = dict(os.environ, MYHOME_POSTINST_ROOT=root, MYHOME_POSTINST_ENV_ONLY="1")
    env.pop("DEBIAN_FRONTEND", None)
    p = subprocess.Popen(["bash", SCRIPT], stdin=slave,
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env)
    os.close(slave)
    buf, pos, deadline = b"", 0, time.time() + timeout
    try:
        for expect, answer in steps:
            while expect.encode() not in buf[pos:]:
                if time.time() > deadline or p.poll() is not None and not select.select([p.stdout], [], [], 0)[0]:
                    raise AssertionError("prompt %r never appeared" % expect)
                if select.select([p.stdout], [], [], 0.2)[0]:
                    chunk = os.read(p.stdout.fileno(), 4096)
                    if not chunk:
                        raise AssertionError("script exited before prompt %r" % expect)
                    buf += chunk
            pos = buf.index(expect.encode(), pos) + len(expect)
            os.write(master, (answer + "\n").encode())
        return p.wait(timeout=timeout)
    finally:
        if p.poll() is None:
            p.kill()
        p.stdout.close()
        os.close(master)


class PostinstPrompts(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.root = self._tmp.name
        self.state = os.path.join(self.root, "var/lib/myhome")
        os.makedirs(self.state)
        self.env_path = os.path.join(self.state, ".env")

    def tearDown(self):
        self._tmp.cleanup()

    def seed(self):
        with open(self.env_path, "w") as f:
            for k, v in SEED.items():
                f.write("%s=%s\n" % (k, v))
        os.chmod(self.env_path, 0o600)

    def assertSame(self, got, want, keys):
        for k in keys:
            self.assertTrue(got.get(k) == want.get(k), "value differs for %s" % k)

    def test_all_enter_keeps_every_secret(self):
        self.seed()
        rc = run_interactive(self.root, [(p, "") for p, _ in PROMPTS])
        self.assertEqual(rc, 0)
        got = parse_env(self.env_path)
        self.assertSame(got, SEED, SEED.keys())
        self.assertEqual(stat.S_IMODE(os.stat(self.env_path).st_mode), 0o600)

    def test_new_value_replaces_only_that_secret(self):
        self.seed()
        steps = [(p, "dummy-sfr-new" if k == "MYHOME_SFR_PASSWORD" else "") for p, k in PROMPTS]
        self.assertEqual(run_interactive(self.root, steps), 0)
        got = parse_env(self.env_path)
        self.assertTrue(got["MYHOME_SFR_PASSWORD"] == "dummy-sfr-new", "SFR not replaced")
        self.assertSame(got, SEED, [k for k in SEED if k != "MYHOME_SFR_PASSWORD"])

    def test_first_install_insists_on_a_password(self):
        # No .env yet: identifiers are typed, and each blank password is refused.
        ids = {"Beem email": "beem@example.invalid", "SFR username": "admin",
               "SMTP From": "from@example.invalid", "SMTP To": "to@example.invalid",
               "SMTP username": ""}
        steps = []
        for prompt, key in PROMPTS:
            if key is None:
                steps.append((prompt, ids[prompt]))
            else:
                if key == "MYHOME_SMTP_PASSWORD":
                    steps.append((prompt, ""))      # optional: blank accepted
                    continue
                steps.append((prompt, ""))          # blank: must be refused
                steps.append(("Try again", "dummy-new-" + key))
        self.assertEqual(run_interactive(self.root, steps), 0)
        got = parse_env(self.env_path)
        for k in SECRETS:
            want = "" if k == "MYHOME_SMTP_PASSWORD" else "dummy-new-" + k
            self.assertTrue(got[k] == want, "secret wrong: %s" % k)

    def test_no_identifier_means_no_secret_prompt(self):
        # Without an SFR username the SFR password is not asked and ends up empty:
        # clearing the identifier is the way to clear a secret.
        env = {k: v for k, v in SEED.items() if k != "MYHOME_SFR_USERNAME"}
        with open(self.env_path, "w") as f:
            for k, v in env.items():
                f.write("%s=%s\n" % (k, v))
        steps = [(p, "") for p, k in PROMPTS if k != "MYHOME_SFR_PASSWORD"]
        self.assertEqual(run_interactive(self.root, steps), 0)
        self.assertTrue(parse_env(self.env_path)["MYHOME_SFR_PASSWORD"] == "", "SFR password not cleared")

    def test_noninteractive_leaves_existing_env_untouched(self):
        self.seed()
        env = dict(os.environ, MYHOME_POSTINST_ROOT=self.root, DEBIAN_FRONTEND="noninteractive")
        r = subprocess.run(["bash", SCRIPT], stdin=subprocess.DEVNULL, env=env,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.assertEqual(r.returncode, 0)
        self.assertSame(parse_env(self.env_path), SEED, SEED.keys())


if __name__ == "__main__":
    unittest.main(verbosity=1)
