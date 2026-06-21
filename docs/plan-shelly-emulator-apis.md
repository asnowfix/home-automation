# Plan: close Shelly Gen2 scripting API gaps in the goja emulator

> `pkg/shelly/script` emulates a Shelly Gen2 device for script testing (`myhome ctl
> shelly script debug/eval`) and for daemon-hosted workflows (`myhome/scripthost`). It
> currently covers `Shelly`, `Timer`, `MQTT`, `Script.storage`, and the `Shelly.call()`
> method dispatch (KVS, HTTP.GET, schedule.*, switch/input config). This plan closes the
> remaining gaps against the [Shelly Gen2 Script APIs](https://shelly-api-docs.shelly.cloud/gen2/Scripts/APIs/Shelly).

## Decisions (agreed with FiX, 2026-06-21)

- **Hardware-only globals fail loud, not silent**: `HTTPServer`, `BLE` (`Scanner`, `GAP`,
  `AdvBuilder`, `advertiseOnce`), `BTHome`, and `UART` have no meaningful emulation —
  they depend on physical radios/pins the daemon/test host doesn't have. Register them
  so scripts referencing them get a clear "not implemented by the emulator" exception
  instead of either a confusing `ReferenceError` or, worse, silently-wrong behavior.
- **Virtual components and AES get real implementations** — both are pure software and
  scripts can reasonably depend on them.
- **Utility functions** (`btoa`, `atob`, `btoh`) are simple and get real implementations.
- Resource-limit emulation (#250) and its `DeviceTestMode`/`DeviceExtensionMode` split
  is a separate, already-filed concern — not part of this plan.

## Phases

### Phase 1 — stub-and-fail: HTTPServer, BLE, BTHome, UART (done)
Add each global (and sub-objects: `BLE.Scanner`, `BLE.GAP`, `BLE.AdvBuilder`,
`BTHome.DataBuilder`) with every documented method bound to a shared
`notImplemented(global, method)` helper that panics with a descriptive message,
catchable by script `try/catch` like a real exception. Test: one table-driven test
per global confirming the panic message and that it's catchable.

### Phase 2 — utilities: btoa, atob, btoh (done)
Top-level functions next to `print`/`console`. `btoa`/`atob` = standard base64;
`btoh` = lowercase hex of the input bytes. Tests cover round-trip + known vectors.

### Phase 3 — Virtual components (done)
`Virtual.getHandle(key)` returns an instance backed by a new `DeviceState.Virtual
map[string]*VirtualComponent` field (mirrors the `ComponentStatus` pattern). Instance
methods: `setValue`/`getValue`/`getStatus`/`getConfig`/`setConfig`/`on`/`off`. Support
component kinds Number/Text/Boolean/Enum/Button/Group per the docs; `on`/`off` use the
existing event-handler-list pattern already used for `Shelly.addEventHandler`. Test:
get/set round-trip, `change` event firing, `Button` push events.

### Phase 4 — AES
`AES.encrypt(plainText, key, mode)` / `AES.decrypt(cypherText, key, mode)` operating on
`goja.ArrayBuffer` (`vm.NewArrayBuffer`/`.Export().(goja.ArrayBuffer).Bytes()`). Modes:
CBC, CFB, CTR, OFB (stdlib `crypto/cipher`), ECB (manual block-by-block, stdlib has no
ECB — needed since Shelly docs list it). Key sizes 128/192/256 bit. Tests: encrypt then
decrypt round-trip per mode, known-answer test vector for at least CBC.

## Notes

- File: `pkg/shelly/script/run.go` (globals are bound in `createShellyRuntime`).
- Keep each phase to its own commit; update this file marking phases done as completed.
