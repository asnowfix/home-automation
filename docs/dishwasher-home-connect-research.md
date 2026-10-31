# Bosch dishwasher (BSH Home Connect) over the LAN: protocol, state, start, completion, key extraction

## What was asked

Establish, from primary sources, how a Bosch/Siemens Home Connect dishwasher can be controlled on the
LAN without the Home Connect cloud at runtime, so that MyHome can (1) see online/offline and, if
possible, "ready to start / loaded", (2) start a program, and (3) learn when a wash finishes. The
solar-router subsystem is out of scope. This file is the reference for that device-control layer.

## When and what I looked at

- Research date: **2026-10-31** (UTC, roughly 16:47-17:10). Local repo HEAD when started: `ccc92232` (branch `main`).
- Clones were shallow (`--depth 1`) into the agent scratchpad. Versions read:

| Upstream | Commit / version read | Date of that revision | License (read from file, and GitHub API where noted) |
|---|---|---|---|
| osresearch/hcpy | `c8570590` | 2025-06-03 | MIT (LICENSE file) |
| hcpy2-0/hcpy (fork of the osresearch lineage, active) | `590f1e71` | 2026-10-01 | MIT per LICENSE file ("2022-2025 Trammell Hudson (osresearch)"); GitHub API `license` field reads `NOASSERTION` (mismatch, reported as-is) |
| Skons/hcpy | `b7dae0a7` | 2023-09-12 (last push 2023-09-23) | **none**: no LICENSE file; GitHub API `license: null` |
| chris-mc1/homeconnect_local_hass (Home Assistant integration) | `f91fd4b5` | 2026-10-22 | MIT (LICENSE file) |
| chris-mc1/homeconnect_websocket (library; repo not cloned, its PyPI wheel was read) | PyPI `homeconnect-websocket` **1.5.5**, uploaded 2026-10-19 | 2026-10-19 | **none**: GitHub API `license: null`; PyPI metadata has no License field |
| bruestel/homeconnect-profile-downloader | `cfe110c4` | 2026-09-03 | MIT (LICENSE file, package.json) |
| bruestel/homeconnect-appliance-proxy (Java) | `108621f7` | 2025-05-31 | Apache-2.0 (LICENSE file) |
| SukramJ/go-homeconnect2mqtt (pure-Go bridge, active) | `392b42b8` | 2026-10-30 | MIT (LICENSE file, Copyright 2026 SukramJ) |
| raff/tls-psk (Go fork of crypto/tls with PSK) | not cloned; README via WebFetch | not shown | not shown (UNKNOWN) |

Evidence labels used below:

- **[V]** VERIFIED: I read it in the source file or in the official doc text, and the URL/line is given.
- **[R]** REPORTED: from a secondary source (issue, forum, README claim, or a WebFetch summary of an
  official page). Treat as a lead to confirm, not as a fact. Where I quote an official page through a
  summarising fetch, the quote is marked [R] and must be checked verbatim before anyone relies on it.
- **[U]** UNVERIFIED / not established: no source found, or I could not read it.

Line numbers refer to the revisions in the table above. GitHub URLs are given with the commit so they
stay stable.

## Short answers

1. **Protocol.** Two transports, chosen per appliance. Older/less capable models speak AES-CBC+HMAC over
   plain `ws://…:80/homeconnect` (binary frames). Others speak TLS 1.2 with a pre-shared key
   (`wss://…:443/homeconnect`, ECDHE-PSK-CHACHA20-POLY1305). The choice is in the profile (`tls` vs `aes`
   key) and in mDNS (TXT `tls=true|false`) [V]. Go's standard library has no TLS-PSK [R, issue 6379 open,
   Unplanned]. The only working Go route found is cgo + OpenSSL (`go-homeconnect2mqtt`) [V]. The AES
   transport is fully implementable with the Go standard library [V].
2. **State.** Everything is a resource with a numeric `uid`; the profile maps uid to name and enum.
   `BSH.Common.Status.OperationState` (uid 552 in one fixture), `DoorState` (527), and
   `RemoteControlStartAllowed` (517) are the relevant ones [R, fixture values]. **No signal for "loaded"
   was found** [U]. Best available proxies are `OperationState=Ready` with `RemoteControlStartAllowed=true`
   and door transitions, which are inferred [U].
3. **Start.** `POST /ro/activeProgram {program, options[]}` (optionally `POST /ro/selectedProgram` first)
   [V]. Remote start must be enabled on the appliance by a user, once (official doc, [R]). Error codes 537
   `RemoteControlNotActive`, 538 `RemoteStartNotActive`, 539 `LockedByLocalControl`, 540/541 "not compliant"
   are defined in the library [V]. Whether a sleeping dishwasher must be woken first is [U].
4. **Completion.** The appliance *pushes* value changes (`NOTIFY /ro/values`, `NOTIFY /ro/descriptionChange`)
   on a single long-lived WebSocket [V]. `BSH.Common.Event.ProgramFinished` exists [V, name in mapping]. Whether
   it fires as a NOTIFY needing acknowledgement, and whether the appliance accepts a second local client, are
   [U]. Silent staleness is a documented failure mode (HA issue 559, 14 days) [R].
5. **Key extraction.** The Bosch profile downloader logs in through an **interactive Electron browser window**
   and then calls the cloud for the PSK/IV and XML [V]. It is not headless. Upstream reports a CAPTCHA step
   that forces a manual browser flow in the older hc-login [R]. A Go tool does not do the cloud step; the
   Go bridge consumes the downloader's output [V]. Key stability across re-pairing or firmware is [U].
6. **Cloud alternative.** Official OAuth2 API with rate limits of 1000 requests/day and 5 program starts/min [R].
   It is internet-dependent, so it cannot meet the internet-optional rule for runtime [V, CLAUDE.md].
7. **Recommendation** (section 8): decide by the dishwasher's connection type first. AES: a native Go client
   is realistic. TLS-PSK: do not build a TLS-PSK stack in Go; use a supervised bridge process behind MQTT,
   with a cgo-built Go bridge (MIT) or a Python 3.14 bridge as candidates.

---

## 1. Protocol (transport, framing, handshake)

### 1.1 Choosing the transport

- The profile JSON from the account has either a `tls` object (with `key`) or an `aes` object (with `key`
  and `iv`). hc-login builds the config as `host = brand-type-haId` for TLS and `host = haId` for AES [V,
  https://github.com/osresearch/hcpy/blob/c8570590b439f49efd8fae0a801595bf40d95ef8/hc-login L237-L245].
- The downloader writes the same split: `appliance.tls` vs `appliance.aes` [V,
  https://github.com/bruestel/homeconnect-profile-downloader/blob/cfe110c4d4c1c8097665deedd23e5c2d4d7588db/main.js L245-L246 and L279-L285].
- The library chooses AES if `iv64` is present, otherwise TLS [V,
  https://github.com/chris-mc1/homeconnect_websocket (PyPI 1.5.5 wheel) `homeconnect_websocket/session.py` L96-L104].
- The mDNS service type is `_homeconnect._tcp.local.`; the TXT key `tls` is parsed as a boolean to choose TLS
  vs AES [V, https://github.com/bruestel/homeconnect-appliance-proxy/blob/108621f726d64b46b1797781f7864e544385355e/src/main/java/net/bruestel/homeconnect/haproxy/service/mdns/MDNSService.java L28, L157-L161].
- Model-to-transport examples: the osresearch README says "The dishwasher has a local HTTPS port open" [V, osresearch README L75-L81], and the clothes washer uses AES on HTTP [V, README L173-L177]. Reports: Bosch SMU4HTS01A/84 dishwasher on TLS-PSK port 443 [R, hcpy2-0 issue 283]; Bosch SMV6ZCX01G on TLS [R, go-homeconnect2mqtt docs/05-resilience.md, row #297]; Bosch KIF81HD40 refrigerator on AES [R, HA issue 502]; Bosch WAV28G40 washer on AES [R, osresearch issue 10]. A model name does not determine the transport. Read it from the profile.

### 1.2 TLS-PSK mode (port 443)

- URL `wss://<host>:443/homeconnect`; TLS 1.2 maximum; cipher `ECDHE-PSK-CHACHA20-POLY1305` (OpenSSL form) [V,
  osresearch HCSocket.py L51-L52 and L132-L138; hcpy2-0 HCSocket.py L102 `set_ciphers("PSK")`].
- No certificate verification (`CERT_NONE`, no hostname check) [V, homeconnect_websocket hc_socket.py L157-L158;
  hcpy2-0 HCSocket.py L97-L98].
- PSK value = `base64url_decode(psk64 + "===")` [V, osresearch HCSocket.py L38; lib hc_socket.py L153].
- **Client PSK identity: sources disagree [V, conflict].** hcpy2-0 sends the identity `"HCCOM_Local_App"`
  (HCSocket.py L103-L104). homeconnect_websocket uses `lambda _: (None, psk)` (hc_socket.py L159). The
  go-homeconnect2mqtt spec says "identity=None/empty" (docs/01-protocol.md §4). The Java proxy uses the same
  string, but as a *server-side* hint for the proxy (TlsProxyService.java L43, L216). Not resolved; test on the
  device.
- Inside TLS the frames are plain UTF-8 JSON text, not binary (go-homeconnect2mqtt docs/01-protocol.md §4,
  [R]). The library sends `ws.send_str` in TLS mode (hc_socket.py L172-L175) [V].
- **No HTTP headers** are needed (no Origin, no subprotocol) [R, go docs §1]. The hcpy2-0 issue 283 (see §7) reports
  that a `Origin` header makes some firmware drop the upgrade silently [R].
- Python: from Python 3.13 the stdlib `ssl` has PSK (`set_psk_client_callback`) [V, hcpy2-0 HCSocket.py L91-L106].
  The sslpsk fallback is used on older Python [V, L110-L120].

### 1.3 AES mode (port 80)

- URL `ws://<host>:80/homeconnect`, binary frames [V, osresearch HCSocket.py L47-L48, L153-L154; lib hc_socket.py L252].
- Keys: `enckey = HMAC-SHA256(psk, "ENC")`, `mackey = HMAC-SHA256(psk, "MAC")`; `iv = base64url_decode(iv64+"===")` [V, osresearch HCSocket.py L44-L46; lib hc_socket.py L213-L216].
- **Send** (client→appliance): JSON UTF-8 → pad → AES-256-CBC with a *persistent* cipher object → MAC.
  Padding: `pad_len = 16 - (len % 16)`; if `pad_len == 1` add 16; pad bytes = `0x00 ‖ random(pad_len-2) ‖ pad_len`
  [V, osresearch HCSocket.py L108-L114; lib hc_socket.py L242-L245].
  MAC input = `iv ‖ 0x45 ('E') ‖ last_tx_hmac ‖ ciphertext`; `last_tx_hmac = HMAC-SHA256(mackey, input)[0:16]`; wire = `ciphertext ‖ last_tx_hmac` [V, lib hc_socket.py L249-L252; osresearch L120-L125].
- **Receive** (appliance→client): frame ≥ 32 bytes, length multiple of 16; split last 16 bytes as the HMAC; MAC input
  = `iv ‖ 0x43 ('C') ‖ last_rx_hmac ‖ ciphertext`; compare (constant time); advance `last_rx_hmac` only on success;
  decrypt with the persistent CBC object; strip padding using the last byte [V, lib hc_socket.py L260-L292; osresearch L73-L102].
- **Chaining.** Both the CBC state and the HMAC chain run for the whole connection; they reset on every new connection
  [V, lib hc_socket.py L223-L227; osresearch HCSocket.py L59-L66]. Evidence that a fresh IV per message breaks decryption:
  osresearch issue 10 (closed) [R]. The go-homeconnect2mqtt spec calls one desynchronised frame fatal: close and reconnect
  rather than keep reading [R, docs/01-protocol.md §3.4].

### 1.4 Message format and ids

- Every message is a JSON object: `sID`, `msgID`, `resource`, `version`, `action` (`GET|POST|RESPONSE|NOTIFY`),
  optional `data` (a list), optional `code` (error) [V, lib message.py L11-L31, L64-L83; go docs §5 [R]].
- Serialisation is compact (`separators=(",",":")`) and single quotes are replaced by double quotes [V, osresearch
  HCSocket.py L147-L150; lib message.py L81-L83].
- `sID` comes from the first device message, `/ei/initialValues`. The client's first `msgID` is `edMsgID` from
  that message. Each client message then uses the next integer [V, lib session.py L367-L370, L474-L477; osresearch
  HCDevice.py L156-L160].
- Responses are matched by `msgID`. A `code` field in the response means an error [V, lib session.py L293-L332].
- Default send timeout 20 s; reconnect back-off starts at 5 s and doubles to a maximum of 300 s [V, lib const.py L6-L8; session.py L506-L538].
- Default `version` = the service version from `/ci/services` for the two-letter service in the resource path
  (for example `/ro/...` uses service `ro`) [V, lib session.py L466-L468].

### 1.5 Handshake (exact order, from the library)

1. Device → client: `POST /ei/initialValues` (first message; establishes `sID` and msgIDs) [V, session.py L362-L380].
2. Client → device: `RESPONSE` to it, with `deviceType` (2 for old devices, else `"Application"`), `deviceName`, `deviceID` [V, session.py L386-L394].
3. `GET /ci/services` → list of `{service, version}`. Stored, and used as the version for later calls [V, session.py L396-L399].
4. If the `ci` version is < 3: `GET /ci/authentication` with a random nonce (v2), then `GET /ci/info` [V, session.py L402-L415].
5. If `iz` is in the service list: `GET /iz/info` [V, session.py L417-L420].
6. If `ei` version is 2: `NOTIFY /ei/deviceReady` [V, session.py L422-L425].
7. If `ni` is in the list: `GET /ni/info` (errors tolerated) [V, session.py L427-L430].
8. State CONNECTED, then `GET /ro/allDescriptionChanges` and `GET /ro/allMandatoryValues` [V, appliance.py L271-L286, L288-L290].

Notes: some firmware returns `500` on the two `/ro/all*` requests [R, go docs §6.3, HA issue 128/255 cited there]. The
go-homeconnect2mqtt spec says the `deviceReady`-style payload key order matters on some firmware [R, docs §6.2 step 1].

---

## 2. Resources, features and state mapping

### 2.1 Resources seen in source

| Resource | Action(s) | What it is | Evidence |
|---|---|---|---|
| `/ei/initialValues` | POST (from device) | session start | [V] session.py L362-L380 |
| `/ei/deviceReady` | NOTIFY, v2 | client says it is ready | [V] session.py L422-L425 |
| `/ci/services` | GET | list of services and versions | [V] session.py L396-L399 |
| `/ci/authentication` | GET v2 (nonce) | auth nonce; response is the device's own nonce | [V] session.py L402-L410 |
| `/ci/info`, `/iz/info` | GET | device identity (serial, software versions) | [V] appliance.py L166-L168; hcpy2-0 HCDevice.py L450-L453 |
| `/ni/info` | GET | network info (IP, Wi-Fi) | [V] session.py L427-L430; appliance.py L229-L233 |
| `/ro/allDescriptionChanges`, `/ro/allMandatoryValues` | GET → RESPONSE | full entity dump (uid, value, access, available, min/max) | [V] appliance.py L161-L165, L271-L286 |
| `/ro/values` | NOTIFY (push) | value changes | [V] appliance.py L157-L159 |
| `/ro/descriptionChange` | NOTIFY (push) | access/available/min/max changes | [V] appliance.py L157-L159; hcpy2-0 HCDevice.py L455-L487 |
| `/ro/values` | POST `{uid, value}` | write a setting or a command | [V] entities.py L240-L249, L480-L495 |
| `/ro/selectedProgram`, `/ro/activeProgram` | POST `{program, options[]}` | select / start a program | [V] entities.py L555-L587; hcpy2-0 HCDevice.py L359-L367 |
| `/ci/registeredDevices` | (received) | list of clients; hcpy ignores it | [V] osresearch HCDevice.py L208-L210 |

The downloader's own docs also list `/ro/allMandatoryValues` and similar names for other appliance types [V,
osresearch HCDevice.py L5-L40 comment list].

### 2.2 Dishwasher-relevant state

**Authority for names and enum values is the appliance profile** (the `*_FeatureMapping.xml` and
`*_DeviceDescription.xml` in the downloader's ZIP) [V, HA README L47; library README "Parsing Device Description"].
The values below come from *fixtures and sample dumps*, not from the profile of our device [R].

| Feature (name) | uid in fixture | Values | Source |
|---|---|---|---|
| `BSH.Common.Status.OperationState` | 552 | 0 Inactive, 1 Ready, 2 DelayedStart, 3 Run, 4 Pause, 5 ActionRequired, 6 Finished, 7 Error, 8 Aborting | [R] library testutils.py L143-L153 |
| `BSH.Common.Status.OperationState` (other fixture) | — | 0 Inactive, 3 Run, **5 Finished** | [R, CONFLICT] go-homeconnect2mqtt internal/homeconnect/entities_test.go L16 (5 = Finished here; 5 = ActionRequired in the library fixture) |
| `BSH.Common.Status.DoorState` | 527 | 0 Open, 1 Closed (no `Locked` in this fixture) | [R] library testutils.py L134-L138 |
| `BSH.Common.Status.RemoteControlStartAllowed` | 517 | boolean | [R] testutils.py L120-L121 |
| `BSH.Common.Status.RemoteControlActive` | 523 | boolean | [R] testutils.py L127-L129 |
| `BSH.Common.Status.LocalControlActive` | (not seen) | boolean; true while a person operates the appliance locally | [R] Go mapping.yaml; HCSocket issue hcpy2-0 #44 |
| `BSH.Common.Setting.PowerState` | (not seen) | Go mapping: On / Standby / Off / MainsOff | [R] go docs/04-device-mapping.md L141-L142 |
| `BSH.Common.Root.ActiveProgram` | 256 | program UID | [R] go docs/05-resilience.md FK-5 |
| `BSH.Common.Root.SelectedProgram` | — | program UID | [V] names in library testutils.py L539-L545 |
| `BSH.Common.Option.StartInRelative` | — | delayed start (dishwasher) | [R] go docs/05-resilience.md "Delayed-start rule"; hcpy2-0 discovery.yaml L19 |
| `BSH.Common.Option.FinishInRelative` | 551 | delayed finish (washer/dryer, not dishwasher) | [R] go docs/05-resilience.md |
| `BSH.Common.Event.ProgramFinished` | — | event | [V] name in go-homeconnect2mqtt mapping.yaml L152 |
| `BSH.Common.Event.ProgramAborted` | — | event | [V] name in mapping.yaml L148 |
| `BSH.Common.Status.ProgramProgress`, `RemainingProgramTime` | — | % and seconds | [R] sample dump in osresearch README L116-L119 (a real dishwasher, 2022) |

A real dishwasher state dump (2022, reported by the osresearch README) contains these keys with values:
`OperationState: Inactive`, `DoorState: Open`, `PowerState: Off`, `RemoteControlStartAllowed: False`,
`RemoteControlActive: True`, `RemoteControlLevel: ManualRemoteStart`, `ProgramProgress`, `RemainingProgramTime`,
`ActiveProgram: 0`, `SelectedProgram: 8192` [R, osresearch README L101-L165]. Its own states were
`"state": "Run"`, `"door": "Closed"`, `"remaining": "2:49"` in the MQTT example [R, README L83-L96].

### 2.3 Mapping of the required signals

- **Online/offline:** the WebSocket connection itself plus the library's CONNECTED state [V, session.py L263-L270; appliance.py L288-L290]. A reachable TCP connection is not proof that the appliance is responsive. Silent staleness is documented (section 7) [R].
- **"Ready to start":** `OperationState == Ready` and `RemoteControlStartAllowed == true` [U as a sufficient condition; the enum names are [R], the semantics are an inference].
- **"Loaded":** **no signal found** [U]. Candidate proxies, all inferred and unverified: a `DoorState` Open→Closed transition, followed by `Ready`. A door cycle does not prove that dishes were loaded.
- **Program selected/active:** `SelectedProgram` and `ActiveProgram` (UIDs; names through the profile) [V for the library handling, entities.py L601-L660].
- **Remaining time / progress:** `RemainingProgramTime`, `ProgramProgress` [R, sample dump].
- **Power:** `PowerState` [R, mapping].

---

## 3. Starting a program

### 3.1 Message sequence

1. Optional select: `POST /ro/selectedProgram {program: <uid>, options: [...]}` [V, entities.py L555-L570].
2. Start: `POST /ro/activeProgram {program: <uid>, options: [...]}` [V, entities.py L572-L587; hcpy2-0 HCDevice.py L364].
3. Options: the library appends the *readwrite* options with their last known values when the program's options are "full" (`fullOptionSet`) [V, entities.py L528-L553]. The Go bridge says a blind `activeProgram` POST fails on some devices, and cooktops need `selectedProgram` with `validate=false` [R, go docs/01-protocol.md §7, docs/05-resilience.md FK-4].
4. The response is `RESPONSE` with no `code` (success) or a `code` (failure) [V, session.py L312-L320; hcpy2-0 HCDevice.py L527-L533].
5. The official doc says to check `OperationState` again after sending an activation command, rather than trusting the response alone [R, official doc via WebFetch summary, see section 6].

### 3.2 Preconditions and failure codes

- **Remote start must be enabled on the appliance by a user, once.** [R, official api-docs "Start program" text as summarised: "The _RemoteStart_ flag needs to be set to true. It must be set on the appliance manually by the user."] Verify verbatim.
- **Local control** must be false; the official doc says LocalControl "is true whenever the user operates the home appliance locally" [R, same fetch].
- **Error codes** (library table) [V, const.py L580-L629]: 537 `RemoteControlNotActive`; 538 `RemoteStartNotActive`; 539 `LockedByLocalControl`; 540 `DeviceStateNotCompliant`; 541 `ProcessStateNotCompliant`; 524 `NotAvailable`; 518 `NoAccess`; 530 `NoExecution`; 531 `ValueOutOfRange`; 512 `UnknownUID`; 515 `Busy`. The library's table has a mislabelled key: `412: "414 RequestUriTooLong"` (const.py L588). The table is the library's, not Bosch's; Bosch's own docs were not read.
- **Door closed, power on, consumables (salt, rinse aid), `ActionRequired`:** whether these block a remote start is **[U]**. `ActionRequired` is in the enum [R], and HA issue 510 (open) reports a salt-state problem on a Bosch dishwasher [R], but no source states the rule.
- **Wake from standby:** **[U]**. The Go bridge calls some devices "sleeping" and says "Offline is normal state" [R, docs/05-resilience.md L20-L30]. No wake procedure is documented.
- **Delayed start:** dishwasher uses `BSH.Common.Option.StartInRelative` [R, go docs/05-resilience.md]. Seconds are the value unit in the HA integration's conversion [V, HA integration `__init__.py` `_duration_to_seconds`].

---

## 4. Completion, push vs poll, reconnect, concurrency, keepalive

### 4.1 Push

- After the initial RESPONSE, the device sends `NOTIFY /ro/values` and `NOTIFY /ro/descriptionChange`, and the library updates entities from them [V, appliance.py L157-L159, L170-L178]. The HA integration's manifest says `iot_class: local_push` [V, manifest.json L13]. No polling is needed for state, with the caveat in 4.4.
- **ProgramFinished:** the event name exists [V, mapping.yaml L152]. The library's event entity has an `acknowledge()` that sends the command `BSH.Common.Command.AcknowledgeEvent` [V, entities.py L462-L475, `acknowledge` at L468]. Whether a finished-cycle event needs acknowledging on a dishwasher is **[U]**. The HA issue requesting an AcknowledgeEvent service exists (issue 533, open) [R].
- The cloud API's event stream gained dishwasher events on **2026-05-14** [R, official changelog]. That is the cloud path, not the LAN.

### 4.2 Reconnect behaviour

- The library's reconnect loop retries with back-off (5 s doubling to 300 s) and runs the handshake again [V, session.py L506-L538]. On every CONNECTED it re-fetches all descriptions and mandatory values, so state is rebuilt [V, appliance.py L288-L290].
- hcpy2-0 issue 269 (open): after a reconnect, *all events fire again*, producing duplicates [R]. Event-based completion detection therefore needs de-duplication by a durable key (for example the cycle start time), not by "event arrived".

### 4.3 Concurrency: how many local clients

- **[U]**. No source states how many simultaneous local WebSocket clients an appliance accepts, or whether a second client disconnects the official app. The Java proxy README says the official app must be force-closed and re-opened to connect to the proxy instead of the real appliance [R, appliance-proxy README L65-L70], which suggests the app and a second client may contend, but it does not establish a limit.
- The vacuum (a different device) allows one local client at a time [V, docs/vacuum.md L54-L56]. That does not transfer to Home Connect.

### 4.4 Keepalive and silent failure

- The library sends a WebSocket ping every 20 s [V, hc_socket.py L69 `heartbeat=20`]. hcpy2-0 pings every 120 s with a 10 s timeout and enables TCP keepalive (idle 30, interval 10, count 3) [V, hcpy2-0 HCSocket.py L201-L215, L301].
- hcpy2-0 has a 10 s connect timeout and a 15 s wait for the connection thread [V, HCSocket.py L207, L245].
- **Silent staleness is real.** HA issue 559 (open, 2026-10-30): a Bosch dishwasher's entities froze for 14 days while the connection sensor stayed `on`; the local `operation_state` stayed `ready` while the machine ran 15 programs; only a config-entry reload recovered it [R]. HA issue 528 (open): a hob and a hood stayed disconnected for about 6 days with WS handshake 503, and the retry loop had no delay [R]. hcpy2-0 issue 120 (open): the dishwasher stops answering until the container restarts [R].
- **Implication (inference):** a "connected" flag is not enough. A freshness check (for example the time of the last NOTIFY) is needed.

### 4.5 Off, Wi-Fi sleep, discovery

- The appliance may drop off the LAN or stop answering in standby. Documented as normal "offline" in the Go bridge [R, docs/05-resilience.md]. Power-off recovery timing is **[U]**.
- Discovery: mDNS `_homeconnect._tcp.local.` [V, Java MDNSService.java L28]. For TLS appliances the name is `brand-type-haId` [V, hc-login L239; go docs/03-profile-format.md L62 "BOSCH-Dishwasher-<haId>"]. Whether that name resolves on a given LAN is **[U]**; hcpy README says it works "assuming that your mDNS or DNS server resolves the names" [R]. The HA integration asks for a manual IP when the initial connection fails [V, HA README L28].
- Issue hcpy2-0 #278 (open, 2026-08-19) reports that some dishwashers can no longer register with the *cloud* because their gateway certificate generation no longer resolves; this blocks `hc-login` for those devices [R]. It is a cloud-side problem and does not directly affect LAN use, but it can block the one-time key extraction.

---

## 5. Key and profile extraction

### 5.1 How the tool works now

- **Bosch's profile downloader** (MIT, Electron app; 2.0.0-beta.1 Rust rewrite on the `2.x` branch [V, README L3-L7]) does the following [V, main.js]:
  - Hard-codes the public `client_id` and scope list (main.js L15-L17; the same value appears in hc-login L58) and the redirect URI `hcauth://auth/prod` (L16).
  - Opens an Electron `BrowserWindow` on `…/security/oauth/authorize` (L130-L149; window at L30, `loadURL` at L156). A `webRequest` hook intercepts the redirect (L160). **Login is therefore interactive in a browser window, not headless.**
  - Exchanges the code at `…/security/oauth/token` (L349-L373). Then it calls:
    `/api/account/v2/accounts/{hcId}/paired-appliances` (L222), `/api/appliance/v2/appliances/{haId}/encryption-information` (L242, returns `tls.key` or `aes.key` + `aes.iv`), and the iddf ZIP at `/api/iddf/v1/iddf/{haId}` (L132, L433-L450).
  - Writes either a ZIP with `<haId>_DeviceDescription.xml`, `<haId>_FeatureMapping.xml` and `<haId>.json` (L562-L600), or an hcpy JSON file (main.js L265-L296, writer at L593).
- **hc-login** (osresearch, MIT) does the same OAuth dance by script. It takes the account email and password **as command-line arguments** [V, hc-login L40-L41; README L44]. The scope is narrowed to `ReadOrigApi` only [V, hc-login L59-L60]. The hcpy2-0 fork says the login changed to a manual browser step with a CAPTCHA, and the code is time-sensitive [R, hcpy2-0 README L53-L58; CHANGELOG 0.1.8 "Change hc-login due to hcaptcha"].
- The osresearch issue 9 (closed) reports that only `ReadOrigApi` is needed and `ReadAccount` breaks the flow [R].

### 5.2 Artifacts and what they contain

- Per appliance: `haId`, `brand`, `type`, `vib`, `mac`, and the key material: `tls.key` (PSK, base64url) or `aes.key` + `aes.iv` [V, main.js L245-L246, L279-L285; hc-login L237-L245].
- The XML pair gives the feature dictionary (uid, name, enum, access) [V, library README].
- **Treat the ZIP and JSON as secrets** (they contain the PSK/IV). The Go bridge's redaction module exists for this reason [R, go-homeconnect2mqtt internal/profile/redact.go; not read in depth].

### 5.3 Headless or Go-only?

- The cloud step is **not headless** with the current tools [V, see 5.1]. No Go tool found does the cloud step [V: `go-homeconnect2mqtt` consumes the downloader's output, NOTICE.md L31]. Keep it a manual, one-time action.
- Known breakage: the downloader can miss a device (for example a Bosch WTYH7701/08 dryer, HA issue 444, open, 2026-08-14) [R]. The downloader's own issue 19 (404) is closed [R].

### 5.4 Stability of the keys

- **[U]**. No source says whether the PSK or IV changes on re-pairing, factory reset, or firmware update. HA issue 502 says the PSK/IV from the profile decrypt correctly for that appliance [R] (a single data point). Measure it: see section 9.

### 5.5 Terms of use

- The downloader and hc-login emulate the Android app's OAuth flow; the downloader sets an Android user-agent (main.js L178 [V]). The Java proxy README says misuse "may violate the terms of service of the Home Connect platform" [R, README L80-L82]. Terms of service were **not read**; this is not a legal opinion [U].

---

## 6. Official Home Connect cloud API (comparison only)

- Source: https://api-docs.home-connect.com/ (read via WebFetch, a summarising fetch; numbers are [R], verify verbatim).
- **Auth:** Authorization Code Grant Flow and Device Flow are both offered (section headings verified; the text was not read in full) [R]. A third-party FAQ says the flow is fixed at client creation [R].
- **Rate limits** [R, quoted via summary]: 10 requests/s on average, burst 20, HTTP 429 beyond that; 50 requests per client and user account per minute; 1000 per client and user account per day; 5 program starts per minute; 5 program stops per minute; 10 event monitoring channels at any time; token refreshes 10/min and 100/day; 10 successive error responses in 10 minutes. An SSE stream counts as one request; keep-alives do not count [R].
- **Events:** SSE stream [R]. A homebridge maintainer says the rate limits force reliance on the event stream, so a dropped stream causes state drift [R, homebridge-homeconnect issue 34 (2020)].
- **Start preconditions:** same as the LAN path (RemoteStart enabled on the appliance) [R].
- **Offline:** the cloud shows "disconnected from the Home Connect server" for an appliance without network [R].
- **2025-2026 changes** (official changelog, [R] via summary, https://developer.home-connect.com/changelog): dishwasher events added to home-appliance-events (2026-05-14); client secret rotation (2026-05-15); EU Data Act support (2025-09-12); beta testing and publication request sections edited for individual accounts (2026-09-01). **No entry** about local/LAN access, device flow, or rate limits in 2025-2026 [R].
- **Reports of local access being blocked by Bosch:** none confirmed. Forum reports that entities went unavailable after firmware updates (cloud path) exist [R, HA community posts, anecdotal]. No BSH/Bosch statement was found [U].
- **Verdict for us:** the cloud API cannot be the runtime path, because the internet-optional rule forbids it [V, CLAUDE.md and AGENTS.md "Rule 1"]. It is a fallback only.

---

## 7. Pitfalls from issues and discussions (open and closed)

Sources are GitHub issues read on 2026-10-31 with `gh issue view`. Status is as read.

| Repo / issue | State | Symptom | Relevance |
|---|---|---|---|
| hcpy2-0/hcpy #283 | open, 2026-10-29 | TLS-PSK on a Bosch SMU4HTS01A/84 dishwasher (FW 6.4-3.12.1.2, WebSocket++/0.8.2): the upgrade gets no reply when `Origin: https://<host>` is sent; without Origin it returns 101 and `/ei/initialValues` within milliseconds. websocket-client adds Origin by default for wss. | **Do not send an Origin header.** Wire capture is by the reporter [R]. |
| hcpy2-0/hcpy #120 | open | dishwasher stops answering until the Docker container restarts | silent staleness [R] |
| hcpy2-0/hcpy #269 | open | after reconnect, all events fire again | de-duplicate completion events [R] |
| hcpy2-0/hcpy #44 | closed | `LocalControlActive: true` for ~15 s after a local press; the app and commands get 403 | expect transient refusals [R] |
| hcpy2-0/hcpy #115 | closed | user asks how to start a program when remote start is active; the body mentions `BSH.Common.Command.PauseProgram`; resolution not read | start path questions [R] |
| hcpy2-0/hcpy #278 | open, 2026-08-19 | BSH PKI generations 15/16 no longer resolve; SMV4HVX31E/65 dishwasher gets TLS bad_certificate on the *cloud* gateway; `hc-login` blocked | cloud-side key extraction risk [R] |
| osresearch/hcpy #10 | closed | AES state not reset per message; second message broke | chain, not per-message IV [R] |
| osresearch/hcpy #9 | closed | `ReadAccount` scope rejected; `ReadOrigApi` alone works | downloader/login scope [R] |
| chris-mc1/homeconnect_local_hass #559 | open, 2026-10-30 | dishwasher entities frozen 14 days, connection sensor `on` | silent staleness; need a freshness check [R] |
| chris-mc1/homeconnect_local_hass #528 | open, 2026-10-14 | hob/hood disconnected for days; WS 503 handshake; no back-off | reconnect back-off mandatory [R] |
| chris-mc1/homeconnect_local_hass #502 | closed, 2026-10-06 | AES fridge closes with code 1008 right after the handshake begins | server-side close on handshake; see the issue for its resolution (not read) [R] |
| chris-mc1/homeconnect_local_hass #510, #514, #491, #373, #257 | open | dishwasher salt-state, Gaggenau dishwasher, select_active_program, Bosch start-in not a control entity, start button unavailable until reload | model-specific gaps in entity mapping [R] |
| chris-mc1/homeconnect_local_hass #437, #479 | open | `select()` sends options the appliance rejects (400) when fullOptionSet is false | options handling is per-model [R] |
| go-homeconnect2mqtt docs/05-resilience.md FK-4 | (their doc) | blind POST to activeProgram fails with 400/501/541 on some devices | verify on our model [R] |

Other model-specific notes from the Go bridge's resilience doc: Bosch SMV6ZCX01G (TLS) "Authentication failed" with no IP fallback (their issue #297) [R]; Bosch SMV4ECX30E delayed start only a read-only sensor (their #373) [R].

---

## 8. Local codebase fit and architecture options

### 8.1 Local conventions this must follow

- Three-tier layer rule: CLI in `myhome/ctl/…`, business logic in `internal/myhome/…`, generic protocol in `pkg/…` [V, CLAUDE.md "Architecture"].
- RPC: four steps (verb in `internal/myhome/const.go`, types, `methods.go` signatures, `RegisterMethodHandler`) [V, CLAUDE.md "RPC System"].
- Config option: four files (`options.go`, `run.go`, `docs/configuration.md`, `myhome-example.yaml`); env pattern `MYHOME_<SECTION>_<KEY>` [V, CLAUDE.md "Go"].
- Events: `events.db` records via `eventSvc.Record(ctx, events.Event{…})` [V, internal/myhome/shelly/blu/listener.go L204]. Default severity for a new event is `info` [V, AGENTS.md "Event severity levels" L455-L458]. Retention 90 days, enforced hourly [V, CLAUDE.md "events.db"].
- Internet-optional and daemon-optional rules [V, CLAUDE.md "Resilience"].

### 8.2 The vacuum feature as precedent (commit series `1f1f657f`..`ccc92232`, epic #606)

- Generic protocol in a separate module `pkg/roomba/` (client, discovery, wake, cloud) [V, `git grep` file list; docs/vacuum-plan.md L19-L24].
- Daemon service `myhome/vacuum/` (separate module): one long-lived connection per robot, supervise loop with connect retries, re-resolve the address, bad-credentials back-off 10 min, and a bounded ICMP wake probe [V, myhome/vacuum/service.go L32-L38, L163-L200, L239-L270].
- RPC verbs `vacuum.list|status|command|clean|rooms|roomname|pair` [V, internal/myhome/const.go L54-L56 and the commit message 1f1f657f]. Wiring in `myhome/daemon/daemon.go` L488-L507 (storage, `NewService`, `RegisterHandlers`).
- Config `MYHOME_VACUUM_BLID`, `MYHOME_VACUUM_PASSWORD`, `MYHOME_VACUUM_HOST`, `MYHOME_VACUUM_WAKE` from `.env` [V, docs/vacuum.md L15-L22; myhome/daemon/run_test.go L81-L124].
- Secret handling: the password is read from the environment and not stored in the DB. An earlier build *did* store it in plaintext, and a migration drops the column (`myhome/vacuum/storage.go` L17-L18, L107-L110) [V]. The same must apply to a dishwasher PSK.
- UI card `internal/myhome/ui/htmx_vacuum.go` and CLI `myhome/ctl/vacuum/vacuum.go` [V, `git grep` list].
- **The vacuum service does not write to events.db** (grep of `myhome/vacuum` and `pkg/roomba` finds no `Record` call) [V]. A dishwasher should, per AGENTS.md's event guidance, but that is a design choice [U as a requirement].
- Repo references to Home Assistant appear only in docs and in `pkg/beem` [V, `git grep`]. The daemon has no HA runtime dependency [V].

### 8.3 Three architectures

**(a) Native Go client in the daemon.**
- AES transport: pure Go (stdlib `crypto/aes`, `crypto/hmac`, `crypto/sha256`, plus a WebSocket library). The go-homeconnect2mqtt AES code is MIT but is itself a clean-room reimplementation from its own spec [R, NOTICE.md]. Our own code can be written from the protocol facts in section 1.
- TLS-PSK transport: stdlib `crypto/tls` cannot do it [R, golang/go#6379, Unplanned, comment 2026-10-26]. Options:
  - **cgo + OpenSSL** behind a build tag, as go-homeconnect2mqtt does [V, `socket_tls_psk.go`, `tlspsk_unsupported.go`]. The default build then cannot talk to TLS appliances. It needs OpenSSL dev headers at build time and a platform-specific cgo setup [V]. This affects `.deb` packaging and cross-builds, which we have not checked.
  - **A fork of crypto/tls** (raff/tls-psk). The README mentions a Go 1.2.2 baseline, a Go 1.13.4 `tls13` branch, and only `TLS_PSK_WITH_AES_128_CBC_SHA`; it does not claim the ECDHE-PSK-CHACHA20 suite the appliance uses. License not shown; activity not shown [R, WebFetch]. Likely stale. **Not recommended without a fresh look.**
  - **Hand-written TLS 1.2 PSK** on top of the record layer. Not researched further; out of scope for this note.
- Trade-offs: no extra runtime if AES; a cgo build and OpenSSL dependency if TLS; the protocol is reverse-engineered and its details (identity, Origin, ids) conflict between sources (section 1). Largest maintenance risk falls on us.

**(b) Sidecar bridged over MQTT (Python or Go).**
- **hcpy2-0** (MIT): Python; native PSK on Python ≥3.13 [V]; hc2mqtt publishes state and HA discovery; the README says the output format "is likely to change" [R, osresearch README L65-L69; the same wording is in the hcpy2-0 README L117-L121].
- **homeconnect-websocket 1.5.5** (no license): requires Python ≥3.14.2 [V, PyPI JSON `requires_python`]. Its code **may not be copied** (no license). Use as a dependency only, and only if the project accepts an unlicensed dependency [U].
- **go-homeconnect2mqtt** (MIT, Go): a complete bridge; single binary and no cgo by default, with a `tlspsk` build tag for TLS appliances [V]. It is an MQTT bridge with HA discovery, so the daemon would consume topics from it and would not speak the protocol itself. Whether its MQTT topic schema is stable was **not** checked [U].
- Trade-offs: an extra process to supervise (the daemon's service model would need one more unit); MQTT is already the bus in this project [V, CLAUDE.md]; a bridge keeps the daemon-optional property clean; the bridge's output schema becomes a dependency.

**(c) Home Assistant integration.**
- chris-mc1's integration (MIT) requires Home Assistant `2026.4.0+` [V, hacs.json L5] and `homeconnect-websocket==1.5.5` [V, manifest.json L19-L21].
- Only sensible if HA is already the consumer. This repo has no HA runtime dependency [V, `git grep`]. Not recommended for MyHome.

### 8.4 Recommendation

1. **First, establish the connection type** from the profile (`tls` vs `aes`) or the mDNS TXT `tls` flag, before choosing anything (section 9, experiment 1). This is a file read, no device call.
2. **If AES:** implement the client natively in Go, following the vacuum layout: generic protocol in its own module under `pkg/` (no MyHome code there), service in `myhome/…` with one long-lived connection, verbs and types in `internal/myhome`, CLI in `myhome/ctl/…`. Keep the secret in `.env` (`MYHOME_DISHWASHER_*`), never in `myhome.yaml` or the DB.
3. **If TLS-PSK (the likely case for newer Bosch dishwashers [R]):** do not write a TLS-PSK stack. Prefer running a supervised bridge process and consuming its MQTT state from the daemon. Candidates: go-homeconnect2mqtt built with the `tlspsk` tag (MIT, Go, cgo/OpenSSL at build time) or hcpy2-0 (MIT, Python ≥3.13). Measure the TLS behaviour on the device before committing.
4. Either way: record operation-state changes and program-finished in `events.db` at `info` severity [V, AGENTS.md]; add a freshness watchdog (section 4.4); never send an Origin header (section 7).

Trade-offs in one line each: (a) keeps everything in Go, but TLS-PSK forces cgo/OpenSSL or a stale fork; (b) gets TLS working quickly but adds a process and an unstable bridge schema; (c) rejected.

---

## 9. Unknowns, and read-only experiments to run first

None of these needs a write to the appliance. Items 4, 5 and 6 each touch a live device and should be done only with the user's approval.

1. **Connection type.** From the existing profile ZIP/JSON (already on disk, if extracted), read the key: `tls` or `aes`. Passive mDNS browse for `_homeconnect._tcp` and read the TXT `tls` property (read-only multicast query).
2. **Port reachability.** TCP connect to ports 443 and 80 only (no protocol traffic). Confirms which transport is listening.
3. **Handshake only.** Connect, receive `/ei/initialValues`, answer, then send only GET requests (`/ci/services`, `/ro/allMandatoryValues`, `/ni/info`). Record `OperationState`, `DoorState`, `RemoteControlStartAllowed`, `RemoteControlActive`, `LocalControlActive`, `PowerState`, `SelectedProgram`, `ActiveProgram`. No POST.
4. **PSK identity.** TLS-only: attempt the TLS handshake with identity `""` and with `"HCCOM_Local_App"`; note which succeeds. Handshake only.
5. **Origin header.** Upgrade with and without an `Origin` header; confirm the hcpy2-0 #283 behaviour on our firmware.
6. **Concurrency.** With the official app connected, open a second local client; observe whether the app session drops and whether the second session stays up. **Needs the user's go-ahead**, because it may disrupt their app.
7. **Standby and staleness.** Over 24-72 h, log mDNS presence, TCP reachability, and the time of the last NOTIFY, with the appliance idle. Establishes how long the appliance stays reachable and whether a freshness watchdog is needed.
8. **"Loaded" proxy.** During a real load, log `DoorState` transitions and `OperationState`; check whether `Ready` followed by an Open→Closed sequence is distinguishable from "empty".
9. **Completion event.** During one real cycle, log every NOTIFY and the arrival of `ProgramFinished` (as a value or as an event needing acknowledgement).
10. **Key stability.** Compare a second profile extraction (after a re-login and after any firmware update) against the first, field by field. Requires the one-time cloud step; the user performs it.
11. **Start preconditions.** Only with explicit approval, and only in a safe, unloaded state: one `POST /ro/activeProgram` with a deliberately refused case (remote start disabled) to observe the exact error codes. Do not start a real wash for this.

---

## What I could not establish

- Which transport the user's dishwasher uses (no model or profile supplied).
- The PSK identity the device actually accepts (sources conflict; section 1.2).
- Whether "loaded" is exposed at all.
- Whether a sleeping appliance can be woken, and how.
- How many simultaneous local clients are allowed, and whether a second one drops the official app.
- Whether the PSK/IV change on re-pairing, reset or firmware update.
- Whether the completion event needs acknowledgement on a dishwasher.
- Exact enum values and uids for *our* model (the fixtures disagree on `OperationState` 5 and 6).
- The Home Connect terms of service for the profile path.
- The official doc text beyond the summaries (rate limits, start preconditions, OAuth) was read only through a summarising fetch and must be checked verbatim.
- Whether cgo + OpenSSL builds on our NAS architecture and in our `.deb` pipeline.
- Whether `raff/tls-psk` is maintained and licensed.
- The resolution of HA issue 502 (closed) and of go issue 64370 (not read).
- The contents of hcpy2-0 issue 278 beyond its first sections.

## Related, not asked

- The cloud PKI break (hcpy2-0 #278) can block the one-time extraction for some dishwashers [R].
- Other Bosch appliances in the same upstream issues (ovens, hobs, washers) follow the same protocol and may help if the dishwasher model is unusual.
- Two projects surfaced by search but not examined: `dosordie/ioBroker.homeconnect-local` (AES and experimental TLS-PSK) and `vemboy200/home-disconnect` on PyPI.
- The Gemini list's names: `bruestel/homeconnect-profile-downloader` and `chris-mc1/homeconnect_local_hass` exist and match. The "homeconnect-local" Go implementation is `SukramJ/go-homeconnect2mqtt`. The hcpy lineage as named in the lead (Skons → osresearch → hcpy2-0) is not confirmed beyond the repos' own READMEs.
