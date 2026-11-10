# #793 progress: postinst script push retry (v0.13.x line)

## Plan
1. Add default-OFF test seams to linux/debian/postinst.sh (binary path, systemctl command, poll interval, retry pause).
2. Write tests first (postinst_test.py: retry-then-success, persistent failure with name, no devices); run against the UNMODIFIED push logic (seams only) to record failing-before.
3. Implement: after a failing first `update '*'`, pause, run once more, report the second result; best-effort failed names from `timeout waiting for response from <id> (<name>)` lines.
4. Update CLAUDE.md "NAS operational facts" (postinst description + manual follow-up).
5. `make test` in background; report.

## Log
- Branch fix/793-postinst-retry-v0.13.x from origin/v0.13.x. postinst.sh push block matches the issue (wait loop + single `update '*'`). No Go change needed.
- Seams added (MYHOME_POSTINST_BIN/SYSTEMCTL/POLL_INTERVAL/MAX_WAIT, RETRY_PAUSE next), all default-off. Tests written first.
- FAILING BEFORE (unmodified push logic, seams only): `python3 -I linux/debian/postinst_test.py` -> Ran 8 tests, FAILED (failures=2): test_first_pass_fails_second_succeeds and test_persistent_failure_warns_and_names_device (AssertionError: 1 != 2, i.e. update ran once, no second pass). test_no_devices_skips_the_push passes (existing behaviour). A first attempt with POLL_INTERVAL=0 looped forever when no devices (elapsed never advances), hence the MAX_WAIT seam.
- Implemented retry in postinst.sh (RETRY_PAUSE default 40 s, seam MYHOME_POSTINST_RETRY_PAUSE); CLAUDE.md note added after the dpkg/postinst bullet (v0.13.x has no separate manual-follow-up paragraph; the postinst bullet text did not claim a single run, left as is).
- PASSING AFTER: `python3 -I linux/debian/postinst_test.py` -> Ran 8 tests, OK; `bash -n` ok.
