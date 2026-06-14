# Code Coverage Gates

Added in this branch (`feature/code-coverage`).

## What was added

Two coverage gates that run on every PR via `.github/workflows/test.yml`:

### Go — aggregate workspace floor

`make cover` collects per-module profiles across all 50 `go.work` modules,
concatenates them into `coverage.txt`, and the CI step
`./scripts/check-coverage.sh "$(cat .coverage-min)"` fails if the aggregate
drops below the floor stored in `.coverage-min` (currently **19%**).

To raise the floor after improving test coverage:

```bash
make cover
make cover-report   # prints the new total
echo 25 > .coverage-min   # update to new baseline
```

### JavaScript (Shelly scripts) — smoke/load gate

`TestSmokeAllScripts` in `internal/shelly/scripts/scripts_smoke_test.go`
iterates every embedded `*.js` file and:

1. **Minifies** it — catching the Espruino `catch (e) {}` → `catch{}` bug class
   and any other syntax that the minifier would break.
2. **Runs the minified source through the goja harness** (`script.RunWithDeviceState`)
   — ensuring the script loads and initialises without a JS-level exception.

The test enumerates the embedded FS automatically, so **adding a new script
automatically requires it to pass** without updating any list.

Scripts that use hardware-only APIs unavailable in the goja harness (currently
only `universal-blu-to-mqtt.js` which uses `BLE.Scanner`) are listed in the
`minifyOnly` map and are checked for minify-safety only.

This gate exercises all 19 embedded Shelly scripts, of which only 2 had
dedicated behavioural tests before (`pool-pump.js`, `blu-listener.js`).

## CI workflow change

`test.yml` previously ran `make test`. It now runs:

```yaml
- name: Run tests with coverage
  run: make cover
- name: Check coverage gate
  run: ./scripts/check-coverage.sh "$(cat .coverage-min)"
```

`make cover` runs the full test suite (including the smoke gate) and produces
`coverage.txt`. The check step gates on the floor. No external services or
tokens are needed.

## New files

| File | Purpose |
|---|---|
| `Makefile` — `cover` target | Collects per-module profiles → `coverage.txt` |
| `Makefile` — `cover-report` | Prints `total:` from `coverage.txt` |
| `Makefile` — `cover-html` | Opens HTML report in browser |
| `scripts/check-coverage.sh` | Threshold gate script |
| `.coverage-min` | Tracked baseline (single number) |
| `internal/shelly/scripts/scripts_smoke_test.go` | JS smoke/load gate |
| `pkg/shelly/script/main.go` — `Minify()` | Exported minify wrapper for tests |
| `pkg/shelly/script/run.go` — `MQTT.isConnected` | Harness stub for real Shelly API |
