# Feature-Script Bundling Plan

> Status: **PROPOSAL — for review.** No code written yet. This document is the
> executable specification: a coding agent should be able to implement it end-to-end by
> following the phases below. Mark each phase done (`[x]`) before starting the next, and
> commit plan updates alongside the implementation (per `CLAUDE.md` "Non-trivial tasks").

## 1. Context & goal

Shelly devices cap the number of simultaneously-enabled scripts (observed: 3 on a Shelly 1
Mini G3; attempting more returns `-108 "Reached the maximum N of enabled scripts"`). We
want more independent capabilities per device than that cap allows.

**Work-around:** assemble several independently-authored "feature" scripts into **one**
uploaded Shelly script on the fly, while each feature stays individually authored,
versioned, testable, and still able to run **standalone** on devices that have not migrated.

The model (reference: `internal/shelly/scripts/prometheus-metrics.js`):

- Each feature is a `var FeatureName = { config, state, init: function(){...} }` object.
- The feature self-registers into a global `features = {}` registry (created on first use).
- A single appended bootstrap iterates the registry and calls every `init()`.

### Decisions (locked)

| Topic | Decision |
|---|---|
| Checksum/version | Whole-bundle SHA1 drives re-upload (reuse existing mechanism) **+** a per-feature manifest in KVS for observability/migration detection. |
| Composition | Named bundles defined in Go (`map[string][]string`). |
| Minification | Upload `--no-minify` for validation. Minified bundling is a **follow-up** (blocked on the #266/#268 minifier-depth crash). |
| Scope | Framework + convert watchdog (split further), blu-listener, blu-publisher, prometheus-metrics. Validate on device `development` (Shelly Pro 1). |

## 2. How things work today (read these before coding)

- **Embedding:** `//go:embed *.js` in `internal/shelly/scripts/scripts.go:17`. Read via
  `pkg/shelly/script.ReadEmbeddedFile(name)` (`pkg/shelly/script/main.go:26`); enumerated
  via `ListAvailable()` (`main.go:30-45`).
- **Version tracking:** `internal/myhome/shelly/script/ops.go`
  - `UploadWithVersion` (`ops.go:38-94`): `version = hex(sha1(code))`; KVS key
    `script/<basename>`; re-upload only if `force || version != kvsVersion`; write new
    version to KVS; always `script.StartStopDelete(..., Start)`. Returns the new script id,
    or `0` when the upload was skipped (the CLI uses `id==0` as the "up-to-date" signal).
  - `DeleteWithVersion` (`ops.go:98-117`): deletes the script + its `script/<basename>` KVS key.
  - `ComputeScriptVersion` / `DeviceStatusWithVersion` (`internal/shelly/scripts/scripts.go:32-78`)
    mirror the read side.
- **Upload mechanics:** `pkg/shelly/script.Upload`→`doUpload` (`pkg/shelly/script/main.go:318-420`):
  if `minify`, run `minifyJS`+`downgradeTemplates`; create-or-stop the script; chunked
  `Script.PutCode` (2048 bytes); `SetConfig{Enable:true}`. `--no-minify`/`--force` already plumbed.
- **`update` command** (`myhome/ctl/shelly/script/update.go`): lists device-loaded scripts,
  matches each by **name** to an embedded file via `ReadEmbeddedFile` (`update.go:89`), then
  `UploadWithVersion`. Unknown names → reported as `error` ("Read failed"). This path must
  learn about bundle names.
- **Standalone upload call sites** that push these scripts today (must keep working):
  `myhome/ctl/blu/publish.go` (blu-publisher.js), `myhome/ctl/blu/follow/blu.go`
  (blu-listener.js), `myhome/ctl/shelly/follow/shelly.go`. Setup installs `watchdog.js` as
  script #1 (`internal/myhome/shelly/setup/setup.go`).
- **CLI command pattern:** see `myhome/ctl/shelly/script/start-stop-delete.go` (cobra `init()`
  registering on `Cmd`, `myhome.Foreach(...)` dispatch, `doX` handler casting to `*shelly.Device`).

### Espruino / minifier rules (load-bearing — `AGENTS.md` "Shelly JS")

No hoisting (define before use). Max 2-3 nested anonymous functions. Never `catch {}` —
always reference `e` (`if (e && false) {}`). Use `"prop" in obj`, not `obj.prop !== undefined`.
`var` only (no `let`/`const`). No `[].shift()/unshift()`, no `slice.call(arguments)`.
**Why this matters here:** `prometheus-metrics.js` was extracted from `watchdog.js` in #268
because the minifier collapsed its function bodies into comma-expression chains deep enough
to overflow Espruino's C evaluator ("Too much recursion"). That is why we ship `--no-minify`
first and treat minified bundling as a separate, gated follow-up.

## 3. Target design

Assembled output = **core preamble** + **feature fragments** (in bundle order) + **bootstrap footer**.

### 3.1 Core preamble — `internal/shelly/scripts/_core.js` (new)

Creates the registry + shared helpers exactly once. Idempotent (safe to evaluate first):

```js
// _core.js — feature-registry preamble (prepended by the assembler; never uploaded alone)
var features = (typeof features === "undefined") ? {} : features;
features._shared = features._shared || {};

// Shared logger factory. Replaces every per-script top-level log()/CONFIG.scriptName logger.
function mkLog(prefix) {
  return function () {
    var s = "";
    for (var i = 0; i < arguments.length; i++) {
      try {
        var a = arguments[i];
        s += (typeof a === "object") ? JSON.stringify(a) : String(a);
      } catch (e) {
        s += String(arguments[i]);
        if (e && false) {}   // keep 'e' referenced; minifier must not emit `catch {}`
      }
      if (i + 1 < arguments.length) s += " ";
    }
    print(prefix + " " + s);
  };
}
```

### 3.2 Bootstrap footer — `internal/shelly/scripts/_bootstrap.js` (new)

Minifier-safe (a `for-in` loop body is not a comma-collapsible statement sequence; the
try/catch blocks statement-merging) and resilient (one feature's init failure must not abort
the others). `features._shared` has no `init` and is skipped.

```js
// _bootstrap.js — runs every registered feature's init() (appended by the assembler)
(function () {
  print("Bundle starting...");
  var name, f;
  for (name in features) {
    f = features[name];
    if (f && typeof f.init === "function") {
      try {
        f.init();
      } catch (e) {
        print("Feature init failed: " + name + ": " + e);
        if (e && false) {}
      }
    }
  }
  print("Bundle startup complete");
})();
```

### 3.3 Feature fragment shape

Each fragment defines its object and self-registers. **No** top-level `CONFIG`/`log()`
globals (they collide across fragments); fold config onto `.config`, use `mkLog(...)`:

```js
// ===== Feature: <Name> (was <file>.js) =====
var <Name> = {
  config: { /* was the file's CONFIG */ },
  state:  { /* per-feature mutable state */ },
  log: mkLog("[<Name>]"),
  init: function () { /* the file's old init/bootstrap body */ }
};
features.<Name> = <Name>;
```

Cross-feature shared state lives on `features._shared` (see watchdog `rebootLock`).

### 3.4 Assembler + bundle registry — `internal/shelly/scripts/bundles.go` (new)

```go
package scripts

import (
	"bytes"
	"fmt"
	"io/fs"
)

// Bundles maps a bundle name to the ordered list of fragment files it contains.
// Membership is intentionally a plain Go map — easy to edit per the device fleet's needs.
var Bundles = map[string][]string{
	// Always-on background services: MQTT watchdog, firmware updates, Prometheus metrics,
	// daily reboot scheduler. 3 static timers — safe headroom.
	"always-on": {"watchdog.js", "prometheus-metrics.js", "daily-reboot.js"},

	// Full utility bundle for devices that also run BLU listener.
	// WARNING: 5 static timers (Watchdog 1 + FirmwareUpdater 1 + PrometheusMetrics 1 +
	// DailyReboot 1 + BluListener queue 1) — AT the 5-timer limit before any BluListener
	// auto-off timer fires. Validate carefully on "development". See §6.
	"utilities": {"watchdog.js", "prometheus-metrics.js", "daily-reboot.js", "blu-listener.js"},
}

const corePreamble = "_core.js"
const bootstrapFooter = "_bootstrap.js"

// AssembleBundle reads _core.js + each fragment (in order) + _bootstrap.js from the embed
// FS and concatenates them with separators. Used for both multi-feature bundles and the
// single-feature standalone path (a "bundle of one").
func AssembleBundle(fragments []string) ([]byte, error) {
	var out bytes.Buffer
	core, err := fs.ReadFile(content, corePreamble)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", corePreamble, err)
	}
	out.Write(core)
	out.WriteString("\n")
	for _, f := range fragments {
		b, err := fs.ReadFile(content, f)
		if err != nil {
			return nil, fmt.Errorf("read fragment %s: %w", f, err)
		}
		out.WriteString("\n// ===== fragment: " + f + " =====\n")
		out.Write(b)
		out.WriteString("\n")
	}
	foot, err := fs.ReadFile(content, bootstrapFooter)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", bootstrapFooter, err)
	}
	out.Write(foot)
	return out.Bytes(), nil
}

// AssembleNamedBundle resolves a bundle name then assembles it.
func AssembleNamedBundle(name string) ([]byte, []string, error) {
	frags, ok := Bundles[name]
	if !ok {
		return nil, nil, fmt.Errorf("unknown bundle %q", name)
	}
	b, err := AssembleBundle(frags)
	return b, frags, err
}
```

### 3.5 Hide preamble/footer from script listing — `pkg/shelly/script/main.go`

`//go:embed *.js` also embeds `_core.js`/`_bootstrap.js`. Filter `_`-prefixed entries from
`ListAvailable()` (and therefore from `update`'s name-matching and `DeviceStatus` "manual"
detection) so they are never treated as uploadable standalone scripts:

```go
for _, entry := range dir {
	if entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
		continue
	}
	scripts = append(scripts, entry.Name())
}
```
(Add `"strings"` to imports.)

### 3.6 Version tracking + manifest — `internal/myhome/shelly/script/ops.go`

Add a bundle-aware upload that reuses the existing compare/skip/start logic and writes a
per-feature manifest. Bundle device-script name and KVS key are `<bundleName>.js`, distinct
from any per-feature `script/<feature>.js`, so a bundle and leftover standalones never
clobber each other's version key.

```go
// UploadBundleWithVersion assembles a named bundle, uploads it as a single script under
// "<bundleName>.js" with whole-bundle SHA1 version tracking, and writes a per-feature
// manifest to KVS for observability. Returns the new script id (0 if skipped/up-to-date).
func UploadBundleWithVersion(ctx context.Context, log logr.Logger, via types.Channel, device types.Device, bundleName string, minify, force bool) (uint32, error) {
	code, fragments, err := scripts.AssembleNamedBundle(bundleName)
	if err != nil {
		return 0, err
	}
	scriptName := bundleName + ".js"

	id, err := UploadWithVersion(ctx, log, via, device, scriptName, code, minify, force)
	if err != nil {
		return 0, err
	}

	// Manifest: per-fragment SHA1 (diagnostics + migration detection). Best-effort.
	manifest := make(map[string]string, len(fragments))
	for _, f := range fragments {
		if v, e := scripts.ComputeScriptVersion(f); e == nil {
			manifest[f] = v
		}
	}
	if mb, e := json.Marshal(manifest); e == nil {
		key := fmt.Sprintf("script/%s/manifest", scriptName)
		if _, e := kvs.SetKeyValue(ctx, log, via, device, key, string(mb)); e != nil {
			log.Error(e, "Unable to write bundle manifest", "key", key)
		}
	}
	return id, nil
}
```
Imports to add: `encoding/json`, and the `scripts` package
(`github.com/asnowfix/home-automation/internal/shelly/scripts`). **Watch for an import
cycle:** `internal/shelly/scripts` imports `pkg/shelly/script` (not the myhome layer), so
`internal/myhome/shelly/script` importing `internal/shelly/scripts` is fine. Verify with
`make build`; if a cycle appears, move `AssembleNamedBundle` resolution into the CLI layer
and pass `code`/`fragments` into `ops.go` instead.

### 3.7 CLI — `myhome/ctl/shelly/script/bundle.go` (new)

`myhome ctl shelly script bundle DEVICE BUNDLE` with `--no-minify`, `--force`, `--no-cleanup`.
Follow the cobra pattern in `start-stop-delete.go`. After a successful install, unless
`--no-cleanup`, delete superseded standalone members still present on the device (migration
step that prevents double timers / duplicate event+MQTT handlers):

```go
func doBundle(ctx context.Context, log logr.Logger, via types.Channel, device devices.Device, args []string) (any, error) {
	sd := device.(*shelly.Device) // (with the usual ok-check)
	bundleName := args[0]

	id, err := mhscript.UploadBundleWithVersion(ctx, log, via, sd, bundleName, !bundleNoMinify, bundleForce)
	if err != nil { return nil, err }
	fmt.Printf("✓ Bundle %s on %s (id: %d)\n", bundleName, sd.Name(), id)

	if !bundleNoCleanup {
		frags := scripts.Bundles[bundleName] // members to remove if standalone
		loaded, _ := pkgscript.ListLoaded(ctx, via, sd)
		for _, ls := range loaded {
			for _, f := range frags {
				if ls.Name == f { // a standalone member is present -> remove it
					if _, e := mhscript.DeleteWithVersion(ctx, log, via, sd, f); e != nil {
						log.Error(e, "cleanup: failed to delete standalone", "script", f)
					} else {
						fmt.Printf("  – removed superseded standalone %s\n", f)
					}
				}
			}
		}
	}
	return id, nil
}
```
Bundle-name arg should validate against `scripts.Bundles` (print available bundles on
unknown name, mirroring `doUpload`'s "Available scripts" UX).

### 3.8 `update` recognizes bundles — `myhome/ctl/shelly/script/update.go`

In the `doUpdate` loop (around `update.go:89`), before the `ReadEmbeddedFile` path, detect a
bundle: a device-loaded script named `<bundle>.js` whose `<bundle>` key exists in
`scripts.Bundles`. For those, call `UploadBundleWithVersion(...)` instead of
`UploadWithVersion`. Keeps `myhome ctl shelly script update <device>` idempotent on migrated
devices.

## 4. Script conversions (the bulk of the work / risk)

> All converted source files: use `git mv` if a file is renamed (preserve `--follow`
> history). Keep edits minifier-safe (§2). After each conversion, the file is a **fragment**:
> no top-level `CONFIG`/`log()`/`STATE`, no trailing IIFE bootstrap — those move to
> `_core.js`/`_bootstrap.js`.

### 4.1 prometheus-metrics.js → feature (smallest; do first as the reference)

Current: top-level `var CONFIG`, `function log()`, `var PrometheusMetrics = {...}`, trailing
IIFE (`prometheus-metrics.js:280-286`).

- Remove top-level `CONFIG` and `log()`.
- Move config onto `PrometheusMetrics.config` (rewrite `CONFIG.prometheus.*` →
  `this.config.prometheus.*`; note callbacks use `self` already — keep that).
- Replace `log: log.bind(this, "[PrometheusMetrics]")` with `log: mkLog("[PrometheusMetrics]")`.
- Delete the trailing IIFE; add `features.PrometheusMetrics = PrometheusMetrics;`.

### 4.2 watchdog.js → two features (split further), shared lock on `_shared`

Current: `var SHARED_STATE`, `var CONFIG`, `function log()`, `var MqttWatchdog`, `var
FirmwareUpdater`, trailing IIFE running both (`watchdog.js:228-236`).

- Remove top-level `CONFIG`, `log()`. Move the MQTT config onto `Watchdog.config`, the
  firmware config onto `FirmwareUpdater.config`.
- Rename `MqttWatchdog` → `Watchdog` (keep `FirmwareUpdater`). Use `mkLog("[Watchdog]")` /
  `mkLog("[FirmwareUpdater]")`.
- Replace `SHARED_STATE` with `features._shared`: `attemptReconnect` reads
  `features._shared.rebootLock`/`rebootLockReason`; `applyUpdate` sets them. Initialise in
  `_core.js`-style? No — set defaults defensively in each `init()` (`if (!("rebootLock" in
  features._shared)) features._shared.rebootLock = false;`).
- Delete the trailing IIFE; add `features.Watchdog = Watchdog;` and
  `features.FirmwareUpdater = FirmwareUpdater;`.
- Keep the minifier-defеnsive comments at `watchdog.js:101-103` (the if-outside-else) intact.

### 4.3 blu-listener.js → `BluListener` feature (largest refactor)

Current: flat top-level functions + `var CONFIG`/`var STATE`/task-queue globals, bare init
block at `blu-listener.js:394-398`.

- Wrap the bare init (`loadFollowsFromKVS(onLoadFollowsComplete); subscribeMqtt();
  subscribeEvent();`) into `BluListener.init()`.
- **Resolve cross-file collisions** with blu-publisher (`STATE`, `getFollows`, `setFollows`,
  `normalizeMac`, `parseSwitchIndex`, `loadFollowsFromKVS`, `onEventData`, `processKvsKey`,
  `onAllKeysProcessed`, `processKeysSequentially`, `onKvsListResponse`, …). Two acceptable
  strategies — pick per-function to stay within the ≤2-3 nested-anonymous-fn rule:
  1. Make them methods on `BluListener` (preferred where the existing `self`/`.bind(null,…)`
     style ports cleanly).
  2. Where deep KVS-callback nesting makes methods risky, keep them as **module-scoped
     top-level functions uniquely prefixed `bl_`** referenced from `init`. This is explicitly
     allowed — the user's model only requires the registration object + `init()`, not that
     every helper be a method.
- Keep `CONFIG.kvsPrefix = "follow/shelly-blu/"` and the script's own config prefix so its
  runtime KVS config is unchanged (portable standalone ↔ bundled). Move `CONFIG`/`STATE`
  onto the object or into uniquely-named module vars (`bl_CONFIG`, `bl_STATE`).
- Keep the 200ms task-queue (rename globals `bl_TASK_*` to avoid colliding with any future
  queue). Register the single `Shelly.addEventHandler` in `init`.
- Add `features.BluListener = BluListener;`.

### 4.4 blu-publisher.js → `BluPublisher` feature

Current: flat functions, `const`/`let`, implicit global `topic`, guarded init at
`blu-publisher.js:751-757`.

- Convert all `const`/`let` → `var`. Remove the implicit global `topic` (declare `var topic`
  locally where used).
- Wrap the guarded init block into `BluPublisher.init()` (keep the `typeof Shelly !==
  "undefined"` guard inside).
- De-collide with blu-listener using the same `bp_`-prefix / method strategy as §4.3. Keep
  its own `SCRIPT_NAME`/`CONFIG_KEY_PREFIX` (config prefix `publish/shelly-blu/`).
- Register its single `Shelly.addEventHandler` and the `BLE.Scanner.Subscribe` in `init`.
- Add `features.BluPublisher = BluPublisher;`.

### 4.5 daily-reboot.js → `DailyReboot` feature

Current: top-level `var CONFIG`, `var STATE`, `var DailyReboot = {...}`, trailing IIFE
(`daily-reboot.js:80-94`) that calls `DailyReboot.init()` and registers a trivial
`script_stop` event handler (just prints; no functional value in bundled form).

**Important:** `daily-reboot.js` has its own `STATE.rebootLock`/`rebootLockReason`, identical
in semantics to watchdog's `SHARED_STATE`. In bundled form all three reboot-aware features
(Watchdog, FirmwareUpdater, DailyReboot) must share the **same lock** via `features._shared`,
otherwise DailyReboot can reboot mid-firmware-update.

Conversion steps:
- Remove top-level `CONFIG`, `STATE`, `log`.
- Move `CONFIG` → `DailyReboot.config`.
- Replace `CONFIG.windowStartHour`/`CONFIG.windowEndHour`/`CONFIG.debug` references inside
  the object with `this.config.*` (callbacks already use closure, so straightforward).
- Replace the inline `log: function(message) { if (CONFIG.debug) { print(...) } }` with
  `log: mkLog("[DailyReboot]")` (mkLog always prints; the `debug` gate is redundant in
  bundled context where logging is always on).
- Replace `STATE.rebootLock`/`STATE.rebootLockReason` with `features._shared.rebootLock`/
  `features._shared.rebootLockReason`. In `scheduleRandomReboot`'s callback:
  `if (features._shared.rebootLock) { ... }`. In `init()` (defensive default, same pattern
  as Watchdog): `if (!("rebootLock" in features._shared)) features._shared.rebootLock = false;`.
- Drop the `script_stop` event handler from the IIFE (trivial; no functional loss).
- Delete the trailing IIFE; add `features.DailyReboot = DailyReboot;`.

Timer: 1 one-shot (rescheduled inside the callback via `self.scheduleRandomReboot()`). At
most 1 DailyReboot timer live at any time. But it is still +1 toward the 5-timer budget
— see §6 for the constraint this creates.

### 4.6 Keep standalone uploads working

`blu-listener.js`/`blu-publisher.js`/`daily-reboot.js` are uploaded standalone by
`myhome/ctl/blu/follow/blu.go`, `myhome/ctl/blu/publish.go`, and any setup command. After
conversion the raw fragment has no bootstrap, so
those call sites must upload the **assembled single-feature** form. Add a tiny bundle entry
or assemble ad-hoc, e.g. `AssembleBundle([]string{"blu-listener.js"})` + `UploadWithVersion`
under name `blu-listener.js` (unchanged device-script name and KVS key → no migration churn
for blu-only devices). Update both call sites (and `myhome/ctl/shelly/follow/shelly.go` if it
uploads these). Search: `grep -rln "blu-listener.js\|blu-publisher.js" myhome --include=*.go`.

## 5. Tests

- **`internal/shelly/scripts/blu_listener_test.go`:** the goja harness runs the raw bytes
  from `readBluListenerScript` (`blu_listener_test.go:19-26`, used at lines 117/149/179/…).
  After conversion the raw fragment won't init. Change `readBluListenerScript` to return the
  **assembled** form: `scripts.AssembleBundle([]string{"blu-listener.js"})` (read via the
  embed FS, or `os.ReadFile` `_core.js` + fragment + `_bootstrap.js`). All existing
  behavioral assertions (motion, illuminance bounds, auto-off, KVS reload) must stay green.
- **`internal/shelly/scripts/pool_pump_test.go`:** unchanged (pool-pump is not converted) —
  must remain green.
- **New `internal/shelly/scripts/bundle_test.go`:**
  - Assemble `"utilities"`; eval in goja with Shelly/MQTT/BLE/Timer mocks (follow the harness
    in `blu_listener_test.go` and the constraint checks in `pkg/shelly/script/compat_test.go`).
  - Assert every feature registers (`features.Watchdog`, `.FirmwareUpdater`,
    `.PrometheusMetrics`, `.DailyReboot`, `.BluListener` all defined) and `init()` runs
    without throwing; assert `_shared` is skipped by the bootstrap; assert no
    duplicate-global crash; assert `features._shared.rebootLock` is initialized once (not
    re-initialized if already set — the defensive `if (!("rebootLock" in features._shared))`
    guard in each feature's `init()` must be present).
  - Assert the assembled source obeys the Espruino constraints checked in `compat_test.go`
    (no hoisting issues, `catch (e)` present, `var`-only).
- **New `ops` test** (`internal/myhome/shelly/script/`): assert the upload version equals
  `sha1(AssembleNamedBundle("utilities"))` and the manifest JSON contains each fragment's
  `ComputeScriptVersion`.
- Run **`make test`** (canonical — never bare `go test ./...`; it skips workspace submodules).

## 6. Known resource-budget risks (validate on-device; do not pre-optimize)

### Timer budget (5-timer hard limit)

| Feature | Timers | Notes |
|---|---|---|
| Watchdog | 1 | one-shot, rescheduled |
| FirmwareUpdater | 1 | recurring (7-day interval) |
| PrometheusMetrics | 1 | recurring (30s interval) |
| DailyReboot | 1 | one-shot, rescheduled |
| BluListener queue | 1 | recurring 200ms, cleared when queue empty |
| BluListener auto-off | N (dynamic) | per followed switch, seconds-range |

**`"utilities"` bundle = 5 static timers — AT the limit before any auto-off timer fires.**
Any BluListener auto-off activation pushes the count to 6+, which is likely to be silently
dropped or cause a runtime error. Mitigation options:

1. **Use `"always-on"` bundle** (watchdog + prometheus + daily-reboot, 3 timers) on devices
   that don't need BLU listening. BluListener then runs as a standalone script.
2. **Share one tick timer** across Watchdog + PrometheusMetrics (follow-up work; see §9).
3. **Disable DailyReboot** in `"utilities"` (remove from bundle or make no-op when
   `features.BluListener` is present).

For the validation phase (§7), use the `"always-on"` bundle first to prove the framework,
then add BluListener (i.e. use `"utilities"`) and monitor for timer-exhaustion errors.

### Other limits

- **RPC in-flight (5):** shared across all features. BluListener serializes via its 200ms
  task-queue; FirmwareUpdater and DailyReboot each fire at most 1-2 concurrent RPCs; these
  should not contend in practice. Watch for "Too many calls in progress".
- **Event handlers (5):** BluListener(1) + BluPublisher(1) = 2 of 5. Safe.
- **Reboot-lock correctness:** Watchdog, FirmwareUpdater, and DailyReboot all read/write
  `features._shared.rebootLock`. Verify that DailyReboot skips its reboot (and reschedules)
  whenever FirmwareUpdater has set the lock, and that the lock is always released after a
  failed update (`applyUpdate` callback).

Document degraded modes in the PR description per `CLAUDE.md` resilience rules.

## 7. Verification (end-to-end on `development`, Shelly Pro 1)

1. `make build` (runs `go generate` first).
2. **Phase A — `always-on` bundle (3 timers, safe):**
   `go run ./myhome ctl shelly script bundle development always-on --no-minify`
   - `Script.List` on `development`: single `always-on.js`, running; standalone members
     (`watchdog.js`, `prometheus-metrics.js`, `daily-reboot.js`) removed.
   - Device logs: `Bundle starting...` → Watchdog/FirmwareUpdater/PrometheusMetrics/DailyReboot
     init messages → `Bundle startup complete`. No timer errors.
   - KVS: `script/always-on.js` (SHA1) + `script/always-on.js/manifest` (per-feature versions).
   - Re-run → "version is the same, skipping upload".
3. **Phase B — `utilities` bundle (5 static timers; watch for exhaustion):**
   `go run ./myhome ctl shelly script bundle development utilities --no-minify`
   - Same checks as above but for `utilities.js`; additionally confirm BluListener is present.
   - Runtime config test: set a `follow/shelly-blu/<mac>` KVS key; confirm the bundled
     BluListener reacts to an injected motion event.
   - Trigger a BluListener auto-off scenario and verify no timer-exhaustion error (`-107` or
     similar) in device logs.
4. `go run ./myhome ctl shelly script update development` → `utilities.js` recognized as a
   bundle, re-checked idempotently.
5. Record timer observations and any `-107`/`-108` errors in the PR description.

## 8. Implementation order (phases)

- [ ] **P1 — Framework.** `_core.js`, `_bootstrap.js`, `bundles.go` (`AssembleBundle`/
  `AssembleNamedBundle`), `ListAvailable` `_`-filter, `UploadBundleWithVersion` + manifest,
  `bundle.go` CLI, `update.go` bundle recognition. `make build` green.
- [ ] **P2 — prometheus-metrics.js → feature.** Convert; unit-assemble a one-feature bundle
  and eval in goja.
- [ ] **P3 — watchdog.js → Watchdog + FirmwareUpdater.** Convert; `_shared.rebootLock`.
- [ ] **P4 — daily-reboot.js → DailyReboot.** Convert; wire `_shared.rebootLock` read.
- [ ] **P5 — blu-listener.js → BluListener.** Convert; update `blu_listener_test.go` to load
  assembled form; keep all assertions green. Fix standalone call sites (§4.6).
- [ ] **P6 — blu-publisher.js → BluPublisher.** Convert; `const`→`var`; fix call sites.
- [ ] **P7 — Tests.** `bundle_test.go`, ops/manifest test; `make test` green.
- [ ] **P8 — On-device validation.** Run §7 on `development` starting with `"always-on"`,
  then `"utilities"`; record results and timer observations in the PR.

## 9. Out of scope (follow-ups)

- Minified bundling (blocked on #266/#268 minifier-depth crash).
- Shared-tick-timer optimization across Watchdog + PrometheusMetrics.
- Additional named bundles / per-device bundle selection beyond the Go map.
