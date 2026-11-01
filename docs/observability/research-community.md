# Research: observability of home-automation fleets and reusable community code

**What was asked:** State of the art for observability of home-automation fleets (device health, availability, LWT, flap suppression, alerting), community Shelly exporters / MQTT-to-Prometheus bridges that could be reused or ported, exactly what Shelly Gen2/Gen3 publish over MQTT without on-device scripts (and what is pushed periodically vs on change), Shelly-documented Wi-Fi fallback behaviour, known Gen3 firmware 2.0.x Wi-Fi/MQTT issues, and which Wi-Fi failure leading indicators have evidence behind them.

**Constraints on this file:** no code, no implementation plan, no live-system access. Findings are marked VERIFIED (read in a primary source during this session) or UNVERIFIED (seen only in a forum/blog/search snippet, or inferred).

**Context:** Go daemon `myhome` on a Raspberry Pi 3B+, about 40 Shelly devices (mostly Gen2/Gen3) over MQTT, about 7 KB free script heap per device, so no new on-device script is the design goal. Motivating incidents: two Shelly 1 Mini G3 devices silently offline for 12.6 h; frequent short offline/online flaps.

Local repo usage (for reference): `myhome/daemon/watch/mqtt.go` already handles `NotifyFullStatus` / `NotifyStatus` payloads; `pkg/shelly/system/types.go` has `RamFree`; `pkg/shelly/wifi/types.go` has `RSSI`, `RSSIThreshold`.

---

## Topic 3 (part 1): what Shelly Gen2/Gen3 publish over MQTT without scripts

Sources read this session. Doc pages are the Shelly Gen2 API docs at shelly-api-docs.shelly.cloud (no separate Gen3 API page was found; Gen3 is documented on the Gen2 pages). Read 2026-11-01.

### Notification model (VERIFIED)
- Source: https://shelly-api-docs.shelly.cloud/gen2/General/Notifications
- `NotifyStatus`: "This method notifies about change in the status of a component, and carries information about the changes which occurred."
- `NotifyFullStatus`: "This method has the same semantics as `NotifyStatus`, but the payload contains the full status of all components."
- `NotifyEvent`: "This method notifies about an occurred event that is not reflected in the status of a component"
- Channels for NotifyFullStatus: "over outbound websocket RPC channel for all devices" and "over outbound websocket, MQTT and outbound UDP channels on battery-operated (sleeping) devices."
- MQTT subscription: "to receive notifications over MQTT you have to subscribe to the topic `<shelly-id_>/events/rpc`."
- HTTP: "notifications cannot be received over HTTP."
- Events listed on that page (the complete list there): `single_push` (input:0), `sys_btn_push` (sys), `config_changed` (all components). The page does NOT list wifi, mqtt or cloud events.

### Timing: periodic or on change? (VERIFIED: not stated)
- The Notifications page does not say whether NotifyStatus or NotifyFullStatus is sent periodically, on connect, or only on change. NotifyFullStatus is described only by its payload and channels. Do not assume a periodic heartbeat from the docs.
- The Sys page (https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys, read 2026-11-01) states no periodic push interval for Sys status. Its only push statements are: `NotifyStatus` "is emitted when the object appears or its contents change" (provisioning), and a `Sys` status-change notification is emitted when `restart_required` changes.
- Sample payload (UNVERIFIED as to timing): a `NotifyFullStatus` example is quoted in a code comment in the local file `myhome/daemon/watch/mqtt.go` (around line 140). It contains `sys.ram_free`, `sys.ram_size`, `sys.fs_free`, `sys.uptime`, `wifi.rssi`, `wifi.ssid`, `wifi.sta_ip`, `wifi.status`, `mqtt.connected`, `cloud.connected`. It is a code comment, not a timing measurement.

### Sys status fields (VERIFIED)
- Source: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Sys
- `ram_size`, `ram_free` (bytes), `fs_size`, `fs_free` (bytes), `uptime` (seconds since reboot), `mac`, `restart_required`, `time`, `unixtime`, `last_sync_ts`, `cfg_rev`, `kvs_rev`, `schedule_rev`, `webhook_rev`, `available_updates`, `wakeup_reason` (battery devices), `utc_offset`, `backup`, and others.
- Implication (INFERENCE): `ram_free` arrives inside a status push only when a status push happens. The Sys page does not promise it is pushed periodically.

### WiFi status fields (VERIFIED)
- Source: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi (read 2026-11-01)
- `WiFi.GetStatus` fields: `sta_ip` (null if disconnected), `status` ("disconnected, connecting, connected, got ip"), `ssid` (null if disconnected), `bssid` (only when connected), `channel` (only when connected), `rssi` (dBm), `ap_client_count` (only when AP enabled and range-extender functionality present).
- Changelog adds: `BSSID in status` (1.7.0, 2025-07-30) and `Add channel to status` (2.0.0, 2026-07-13). See the changelog section below.
- The WiFi page does not say when status is pushed. It lists five Wi-Fi notification event names as examples: `sta_connect_fail`, `sta_connecting`, `sta_connected`, `sta_ip_acquired`, `sta_disconnected`, without timing or conditions. NOTE: the Notifications page does not list these names. The two pages disagree on whether they are documented as events. Both are reported; neither is resolved here.
- Whether `rssi` is pushed periodically or only on change: NOT ESTABLISHED by docs. Only the changelog hints at suppression of flapping notifications (1.3.0 below).

### MQTT component (VERIFIED)
- Source: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/
- Online topic: "subscribe to the topic `<shelly-id>/online`". Device sends `true` on connect. `false` is sent when the device is "forced to disconnect abruptly" (described as the LWT). On reconnect: "you will receive a message `true` to notify you for the reconnection."
- Retained flag: NOT STATED on the page.
- Status topics: `<topic_prefix>/status/<component:id>` for component status, and `<topic_prefix>/status` for the full device status (returned in response to `status_update`).
- Config fields: `enable`; `ssl_ca` (null = plain TCP, `*` = TLS without validation, `user_ca.pem`, `ca.pem`); `topic_prefix` (default: device ID when null); `rpc_ntf` ("Enables RPC notifications (`NotifyStatus` and `NotifyEvent`)", default `true`); `status_ntf` ("Enables publishing the complete component status on…", "if a signifficant change occurred" [sic], default `false`); `enable_control` (default `true`).
- Reconnect backoff and keepalive: NOT DOCUMENTED on this page.

### Gen3 and `events/rpc` (UNVERIFIED, secondary sources)
- TapHome guide (https://taphome.com/en/compatibility/shelly-ht-gen3-mqtt/) says RPC notifications default `rpc_ntf` true and that the H&T Gen3 relies on `events/rpc` (temperature from `NotifyStatus` `temperature:0.tC`), not individual status topics.
- A forum user with a PM Mini Gen3 reported that `/online` was the only value that updated, and `/events/rpc` carried no usable values (loxforum thread, date not confirmed). Single anecdote; treat as a warning to verify on device, not as a fact.
- No official Gen3-specific MQTT page was found in search. Gen3 behaviour is therefore UNVERIFIED against official docs.

### Changelog: Wi-Fi and MQTT related firmware entries (VERIFIED)
- Source: https://shelly-api-docs.shelly.cloud/gen2/changelog (read 2026-11-01). The page is about 148k characters; the summary covers the first ~100k characters, newest first. Latest listed: `2.2.0-beta2` under "Unreleased"; latest numbered release listed: `2.1.1`.
- 2.0.1 (2026-09-23): [Notifications] "Fix Switch status changes when counters are enabled/disabled"; [Memory] "Index data files in RAM instead of scanning the directory" (ProEM/Pro3EM).
- 2.0.0 (2026-07-13): [WiFi] "Retry on scan failure"; [WiFi] "Add `channel` to status"; [MQTT] "Bump max queue length to 30"; [MQTT] "Fix missing status updated after reboot or power loss"; [MQTT] "Fix crash on MQTT.subscribe with empty topic"; [Notifications] "Fix wrong status change when an error is cleared"; [WiFi, local web] "Fixed WiFi settings clearing the stored password or triggering unnecessary reconnections".
- 1.7.1 (2025-09-24): [WiFi/Disconnect] "Implement connectivity checks with GW pings; force a disconnect if PK seems to be lost".
- 1.7.0 (2025-07-30): [WiFi] "Add BSSID in status"; [WiFi] "Do not save last AP on mains-powered devices"; several MQTT control fixes.
- 1.6.0 (2025-04-30): [MQTT/Notifications] "Fix incorrect `connected` notification (when device tries to connect to host with stopped MQTT broker,...)"; [MQTT/Disconnect] "Fix disconnects when subscribing to a topic above 230 chars".
- 1.5.1 (2025-03-18): [WiFi] "Eco mode: Turn off WiFi idle time by default".
- 1.4.0 (2024-07-31): [MQTT] "Allow more than one outstanding publish request (avoid MQTT queue overflows)"; [WiFi] "Add support for WPA3".
- 1.3.0 (2024-04-25): [WiFi/Disconnect] "Suppress disconnected/connecting flapping notifications"; [WiFi/Disconnect] "System LED: missing response to WiFi disconnects".
- 1.2.1 (2024-02-22): [WiFi/Reconnect] "Troublesome Wi-Fi reconnects".
- 1.2.0 (2024-02-13): [Notifications] "Sys component_added and component_removed event notifications".

Caveat: the changelog does not say which entries apply to Gen3 firmware specifically; it is one Gen2 API changelog. Treating the 2.0.x numbering as the Gen3 firmware line is an ASSUMPTION.

### Known Gen3 firmware 2.0.x Wi-Fi / MQTT reconnect issues (UNVERIFIED)
- No official 2.0.x known-issue entry for Gen3 Wi-Fi or MQTT reconnect was found. The 2.0.0 changelog entries above are fixes, not known issues.
- Community report (Home Assistant forum, date not established in the search snippet): after moving several Gen3 and Gen4 devices to 2.0.0 the user had "communication issues", and switching to the new Shelly Control app "fixed the connection issues" for them. Single anecdote; no root cause.
- Separate: a Home Assistant thread reports BTHome data stopped after updating to a 2.0.0 beta; the workaround was a return to 1.7.5. Not confirmed on stable 2.0.x.

### Wi-Fi fallback behaviour (VERIFIED from docs)
- `sta1`: "Identical to `sta`. It will be used as fallback when the device is unable to connect to the `sta` network." (WiFi page, https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi)
- `roam`: "WiFi roaming configuration." `rssi_thr`: "Rssi threshold - when reached will trigger the access point roaming. Default value: -80". `interval`: "Interval at which to scan for better access points. Enabled if set to positive number, disabled if set to 0. Default value: 60".
- `ap_roaming`: NO field by that name appears in the WiFi page. The closest documented thing is `roam`. Do not treat `ap_roaming` as a documented key.
- Changelog 1.7.0: "Do not save last AP on mains-powered devices". 1.7.1: "force a disconnect if PK seems to be lost" (GW ping checks).
- The docs do not describe a timeout or retry schedule for falling back from `sta` to `sta1`. NOT ESTABLISHED.

---

## Topic 1: how other platforms handle availability, LWT, flap suppression and alerting

Read 2026-11-01 unless stated. Where a primary page was unreachable (404 or no relevant content), this is said explicitly.

### Home Assistant (VERIFIED, primary docs)
- MQTT availability: https://www.home-assistant.io/integrations/mqtt/
  - "Platform specific availability settings are available for `mqtt` entity platforms only."
  - `payload_available` default `online`; `payload_not_available` default `offline`.
  - `availability_mode` default `latest`. `all`: "`payload_available` must be received on all configured availability topics before the entity is marked as online." `any`: "...on at least one configured availability topic..." `latest`: the last `payload_available` or `payload_not_available` received controls availability.
  - LWT: "When the device or service loses connection to the broker, the broker will publish the Will message." The fetch returned a summary, not verbatim text, for the LWT sentence; re-check the page before relying on wording about clean disconnects.
  - Retained caveat: "these messages retain at the broker, even when the device or service stops working." and "Retained messages can create ghost entities that keep coming back."
- Template binary sensor, the closest documented equivalent to flap suppression: https://www.home-assistant.io/integrations/template/
  - `delay_on`: "The amount of time (for example, `0:00:05`) the template state must be ***met*** before this sensor switches to `on`."
  - `delay_off`: "The amount of time the template state must be ***not met*** before this sensor switches to `off`."
  - Common entity option: "If the template returns any other value, the entity is `unavailable`." and "If not configured, the entity is always `available`."
  - The page does not use the words "debounce" or "filter".
- Shelly integration: https://www.home-assistant.io/integrations/shelly/
  - Gen2+ "communicate with Home Assistant" via "the `RPC` protocol"; "Integration is communicating directly with the device; cloud connection is not needed."
  - "Shelly devices push updates to Home Assistant upon changes for all main functions of the device."
  - Gen1: "Home Assistant will display a repair issue for the Shelly device if push updates from this device do not reach the Home Assistant server."
  - Some entities (RSSI, uptime, temperature, cloud, firmware; disabled by default) "poll ... data from the device every 60 seconds".
  - The page does NOT describe how the integration determines a device is offline (no timeout, no heartbeat). NOT ESTABLISHED.
- Community evidence on Shelly flapping (UNVERIFIED, forum): https://community.home-assistant.io/t/shelly-1-gen-3-keeps-going-unavailable-and-then-available-all-is-good-in-shelly-web-ui/757089
  - Aug 2024 to Jan 2025 thread. Symptom: devices "keep going unavailable and then back online without any intervention" while the Shelly web UI looked fine. One poster saw them go unavailable "roughly every 2-3 minutes". Suspected causes offered: an HA polling timeout shorter than the device reporting interval (one poster's guess), and an HA/IPv6 issue after firmware 1.4.0 (GitHub issue #122958 referenced; not checked). Resolutions reported: HA 2024.8.1 for one poster; a faulty ethernet cable to a UniFi access point for another. Two posters ruled out Wi-Fi signal by observation, not measurement.

### Zigbee2MQTT (VERIFIED, primary docs)
- Topics: https://www.zigbee2mqtt.io/guide/usage/mqtt_topics_and_messages.html
  - `zigbee2mqtt/bridge/state`: "Contains the state of the bridge, this message is published as retained." Online `{"state":"online"}` on startup; offline `{"state":"offline"}` "published right before the bridge stops". The page does not mention a last will for the bridge state.
  - `zigbee2mqtt/FRIENDLY_NAME/availability`: "If "Device-Availability" is configured the online/offline status will be published when it changes." The same page does not state whether this topic is retained.
- Device availability: https://www.zigbee2mqtt.io/guide/configuration/device-availability.html
  - Availability is based on check-ins (any Zigbee message). Active devices: if no check-in arrives in time "they will be pinged, if that fails the device will be marked as `offline`." Passive devices cannot be pinged and "will be marked as `offline`" when they miss their check-in window.
  - Defaults: `enabled: false`; `active.timeout` 10 minutes; `active.max_jitter` 30000 ms; `active.backoff: true` (ping backoff x1.5, x3, x6, x12 gives 10, 15, 30, 60, 120 min); `passive.timeout` 1500 minutes (25 h).
  - State published to `zigbee2mqtt/[FRIENDLY_NAME]/availability` as `{"state":"online"}` / `{"state":"offline"}`; this page says "this message is a retained MQTT message".
  - Warning: "The pinging can be heavy on the coordinator, especially if you are using a CC2530 or CC2531 adapter."
  - Groups: "A group is marked as available when at least one device in it is available."
- Known operational issue (UNVERIFIED, older, secondary): bridge/state can stay `offline` after a broker restart even though Z2M reconnected (GitHub issue #4854, https://github.com/Koenkk/zigbee2mqtt/issues/4854). Not checked against current releases.

### Tasmota (VERIFIED, primary docs)
- MQTT page: https://tasmota.github.io/docs/MQTT/
  - LWT topic: "The topic to listen to for the state of this specific client is `tele/<TOPIC>/LWT`." Example output shows `Online` / `Offline`; a boot-log example shows "Online (retained)".
  - Purpose: LWT "instructs the broker to generate a notification about an ungracefully disconnect or timeout."
  - Telemetry: "Tasmota periodically sends telemetry every TelePeriod, which defaults to 300 seconds (5 minutes)."
  - The page does not describe Wi-Fi RSSI in STATE/INFO, and does not describe reconnect behaviour beyond a protocol-downgrade note. NOT ESTABLISHED for Wi-Fi telemetry on this page.

### openHAB (UNVERIFIED: community forum only; official binding docs NOT retrieved)
- Search results (community.openhab.org) say: if no online message is published to the LWT topic "it will default to showing the Thing as OFFLINE"; and "if the messages aren't published as retained, openHAB will assume the device is OFFLINE until it receives a message telling it otherwise." A forum member notes ONLINE "only means that the communications channel to the device is open and working". https://community.openhab.org/t/mqtt-binding-items-value-when-the-associated-thing-goes-offline/128360
- For periodic devices, users suggest the expire binding (e.g. `expire="5m,state=OFFLINE"` item metadata). No official openHAB doc for this was read. Treat as unverified.
- GAP: official openHAB MQTT binding documentation was not fetched.

### Domoticz (PARTIAL, mostly unverified)
- The dzVents wiki page was not readable: www.domoticz.com redirects to wiki.domoticz.com, and wiki.domoticz.com/DzVents:_full_API_reference returned HTTP 404 on fetch. GAP: official timedOut/lastUpdate wording not quoted.
- A Domoticz commit (https://github.com/domoticz/domoticz/commit/596a86acb) is reported in search as documenting `timedOut` as a Boolean device attribute that flags when Domoticz cannot reach the device. Seen only in a search snippet; not read in full.
- Community: a per-hardware "data timeout" restarts a gateway when it sends nothing for a set period (https://forum.mysensors.org/post/82998). Community only.
- Community workaround for alerting on silence: check `lastUpdate.minutesAgo` in a script and email (personal write-up, https://anonimno.codeberg.page/device_monitoring.html). Unverified; personal blog.

### ioBroker (UNVERIFIED; little primary material found)
- No official ioBroker device-offline convention was found. The MQTT client adapter (https://github.com/Pmant/ioBroker.mqtt-client/) has a last-will topic/message setting, which signals the adapter's own connection, not per-device state. Search snippet only; the repository was not read.
- GAP: ioBroker developer or adapter documentation was not retrieved.

### Cross-platform patterns (VERIFIED where cited above; the synthesis is INFERENCE)
- Two-level availability is common: the bridge or hub has its own state topic with LWT (Z2M `bridge/state`, Tasmota `tele/<TOPIC>/LWT`), and per-device availability is separate. A HA forum thread reports that a bridge-offline state can make every device look unavailable (secondary source, https://community.home-assistant.io/t/mqtt-status-problems/381429, not read in full).
- Retained availability: Z2M device availability and Tasmota LWT are documented as retained; HA docs warn retained messages can outlive the device.
- Flap suppression on the consumer side is done with a delay filter (HA `delay_off`) or a check-in timeout (Z2M `active.timeout`, passive `timeout`). No primary doc read describes device-side hysteresis for Shelly Wi-Fi devices.
- Silence-based detection (no message within a window) is the general mechanism for devices that do not send LWT. Z2M passive devices default to a 25 h window, far longer than the 12.6 h incident. (INFERENCE: window sizing is a design question, not a recommendation.)

---

## Topic 2: community Shelly exporters, MQTT-to-Prometheus bridges, and Go client libraries

Repo metadata was taken from the GitHub REST API (`gh api repos/<owner>/<repo>`) on 2026-11-01. "Last push" is `pushed_at`, not last release. "License (API)" is GitHub's detected SPDX id; where the README states a different license, both are shown. README claims were read from the GitHub page via WebFetch on 2026-11-01 and are paraphrased unless quoted. No code was run and no repository was cloned.

### 2a. Shelly-specific exporters

| Repo | Lang | License (API) | Last push | Stars | Gen2 / Gen3 | Collection | Metrics (examples) | On-device script? |
|---|---|---|---|---|---|---|---|---|
| LukeEvansTech/shelly-prometheus-exporter (https://github.com/LukeEvansTech/shelly-prometheus-exporter) | Go | GPL-3.0 | 2026-10-30 | 0 | README: "all Gen2, Gen3, and Gen4 devices work automatically"; Gen1 not mentioned | HTTP RPC polling (`deviceUpdateInterval`); devices listed by IP in config | `shelly_switch_*`, `shelly_cover_*`, `shelly_system_uptime`, `shelly_system_ram`, `shelly_system_fs`, `shelly_system_wifi_rssi`, `shelly_device_wifi_sta`, `shelly_device_cloud` | Not mentioned in README |
| veerendra2/shelly_device_exporter (https://github.com/veerendra2/shelly_device_exporter) | Go | Apache-2.0 | 2026-10-02 | 6 | "Compatible with all Gen 2+ devices utilizing the standard Shelly RPC API"; only Plug M Gen3 marked tested | RPC polling, digest auth; static `prometheus.yml` targets; port 8080 | Metric list deferred to an external wiki page (not read) | Not mentioned |
| jcodybaker/shellyctl (https://github.com/jcodybaker/shellyctl) | Go | MIT | 2026-08-13 | 16 | "Shelly Gen2/3 API" (About text only) | Probes devices on scrape (`--probe-concurrency`); mDNS/BLE/manual discovery; no WebSocket/MQTT | Prefix `shelly_status_*` by default; names not listed | Not mentioned |
| webdevops/shelly-plug-exporter (https://github.com/webdevops/shelly-plug-exporter) | Go | MIT | 2026-10-20 | 10 | "Shelly Plugs and devices (generation 1 and 2)" | Not stated; mDNS or probe list; port 8080 | 25 `shellyplug_*` metrics incl. `shellyplug_wifi_rssi`, `shellyplug_info` | Not mentioned |
| SchumacherFM/prometheus_shelly_exporter (https://github.com/SchumacherFM/prometheus_shelly_exporter) | Go | Apache-2.0 | 2026-10-27 | 0 | Not stated. Tested with Shelly H&T; "WIP (1PM and 3EM)". Default topic `shellies/+/info` is the Gen1 topic scheme | MQTT subscriber, `prom` command forwards MQTT to Prometheus | Not listed | Not mentioned (MQTT-only) |
| Yornik/shelly-bridge (https://github.com/Yornik/shelly-bridge) | Python | MIT | 2026-11-01 | 0 | Gen2 only (per search snippet; not verified in README) | Not checked in README | Relay, power, energy, health (per search snippet) | Not checked |
| lucavb/shelly-power-exporter (https://github.com/lucavb/shelly-power-exporter) | Python (`aioshelly`, `uv`) | None on API; README says "MIT" | 2026-10-28 | 1 | README uses "Gen2+"; "Support is based on component payloads; not every model or firmware has been tested on hardware." | HTTP `Shelly.GetStatus` polling every `SCRAPE_INTERVAL` | `shelly_wifi_rssi_dbm`, `shelly_uptime_seconds`, `shelly_switch_on`, `shelly_power_watts`, `shelly_target_up`, `shelly_up`, `shelly_last_poll_success_timestamp_seconds`, `shelly_poll_failures_total`, others | No; RPC only |
| rvben/shelly-exporter (https://github.com/rvben/shelly-exporter) | Rust | MIT | 2026-10-26 | 6 | README: "supporting both Gen1 and Gen2 devices"; Gen3 not mentioned | HTTP polling every 30 s (`--poll-interval`); mDNS "coming soon" | `shelly_device_up`, `shelly_device_uptime_seconds`, `shelly_wifi_rssi_dbm`, `shelly_system_ram_free_bytes`, `shelly_system_fs_free_bytes`, `shelly_device_update_available` | Not mentioned |
| d1ceward/shellyplug-exporter (https://github.com/d1ceward/shellyplug-exporter) | Crystal | MIT | 2026-10-30 | 13 | Gen2 path; PR #43 "Handle Gen3 and Gen4 plugs as Gen2" (search snippet) | HTTP/RPC API | Plug metrics | Not mentioned |
| easimon/shelly-exporter (https://github.com/easimon/shelly-exporter) | Kotlin | none on API | 2026-10-26 | 10 | Gen1/Gen2 per search snippet (not verified in README) | Not checked | Not checked | Not checked |
| meloos/shellyplugplus-exporter (https://github.com/meloos/shellyplugplus-exporter) | Python | none on API | 2026-03-01 | 1 | Gen3 plugs (per search snippet) | RPC polling | power, voltage, current, relay, energy | Not checked |
| sheffieldhackspace/shellypro3em-mqtt-prometheus-exporter (https://github.com/sheffieldhackspace/shellypro3em-mqtt-prometheus-exporter) | Shell (+ nginx/systemd) | none on API | 2025-11-20 | 0 | Pro 3EM; Gen2 docs linked; generation not explicitly stated | MQTT: consumes `<id>/events/rpc` NotifyStatus (uses `dst` `<id>/events`). Ignores NotifyEvent | Not listed; uses source keys such as `total_act_power`, `a_voltage` | Not stated |

Notes on the table:
- None of the Shelly-specific exporters above, as read, subscribes to the device's MQTT `events/rpc` stream for device health (wifi, sys, online). The sheffieldhackspace project is the only one found that consumes Gen2 MQTT `NotifyStatus` directly, and it is single-device, energy-focused, and its README is cut off in the fetched text.
- Polling exporters (LukeEvansTech, veerendra2, lucavb, rvben, webdevops, shellyctl) match the `Shelly.GetStatus` route the design wants to avoid except as a fallback. Their metric names show what a fleet exporter typically exposes (uptime, RAM, FS, Wi-Fi RSSI, up/down). This is a useful naming reference, not a reuse candidate by itself.
- Conflicting licence data: lucavb README says MIT; GitHub API reports no detected license. Not resolved.

### 2b. Generic MQTT-to-Prometheus bridges (any language)

| Repo | Lang | License | Last push | Stars | Model | Relevance |
|---|---|---|---|---|---|---|
| hikhvar/mqtt2prometheus (https://github.com/hikhvar/mqtt2prometheus) | Go | MIT | 2026-08-20 | 432 | JSON field mapping via gojsonq paths; one topic path per instance; `device_id_regex` extracts device id from topic | Go; mature; relevant ideas: metric expiry. README: "Each received metric will be presented for this time if no update is send via MQTT." Cache timeout default 24 h in sample config; `-1` disables deletion. README: "Currently, the exporter supports only MQTT 3.1." Does NOT mention availability, offline or LWT |
| kpetremann/mqtt-exporter (https://github.com/kpetremann/mqtt-exporter) | Python | MIT | 2026-05-11 | 168 | Expects flat JSON with numeric values; per-topic message counters; `PROMETHEUS_PORT` default 9000 | Python; Shelly NotifyStatus payloads are nested, so flat-JSON assumption does not fit without mapping (INFERENCE from payload shape in the local code comment) |
| Schmidt-DevOps/mqtt2prometheus | (not checked) | | | | Arbitrary JSON on topics | Not read |

### 2c. Go and other client libraries for Shelly Gen2 (for reuse of the RPC/MQTT layer)

| Repo | Lang | License | Last push | Stars | Coverage (README) | Maturity statement |
|---|---|---|---|---|---|---|
| jcodybaker/go-shelly (https://github.com/jcodybaker/go-shelly) | Go | MIT | 2026-07-19 | 4 | "unofficial native go client for the Shelly Gen2 API"; HTTP via `mgrpc.UseHTTPPost()`; README does not claim WebSocket or MQTT support (TODO: "MQTT / WebSocket examples" pending); no Gen3 mention; NotifyStatus not described in README | "This library is currently in active development (as of December 2024)." "It has meaningful gaps in testing and functionality." "At this stage there is no guarantee of backwards compatibility." |
| home-assistant-libs/aioshelly (https://github.com/home-assistant-libs/aioshelly) | Python | Apache-2.0 | 2026-11-01 | 83 | Gen1 (CoAP) and Gen2+ (RPC over WebSocket); no MQTT in README; no device availability text in README | Used by Home Assistant (not verified in this session, from general knowledge) |
| jcodybaker/shellyctl | Go | MIT | 2026-08-13 | 16 | See table above | README: "This library is currently in active development (as of February 2024)." |

### 2d. Reuse, port, or borrow ideas (INFERENCE; this is the classification the task asked for, not an implementation plan)
- Reuse as a library: the only Go Gen2 client with a permissive license is go-shelly, and its own README says it has "meaningful gaps in testing" and no compatibility guarantee. Treat as a reference, not a dependency, unless its notify path is read and tested.
- Port or borrow ideas: LukeEvansTech and rvben show a metric naming set for uptime, RAM, FS, Wi-Fi RSSI and up/down, usable as a naming reference. lucavb shows stale-metric and reconnect-with-backoff wording (see its README quotes: "After `METRICS_MAX_AGE_SECONDS`, measurement and component-error series disappear until the next successful poll."; "Failed reconnects use exponential backoff between attempts.") These are ideas to borrow, not code to port.
- Borrow metric expiry from hikhvar/mqtt2prometheus: a per-series timeout with a "-1 disables" escape hatch.
- No candidate found that reads Gen2/Gen3 MQTT `events/rpc` or `/online` and produces device-health metrics without polling. The closest is sheffieldhackspace (MQTT NotifyStatus, energy only), which shows the topic shape but is a shell project. This gap is the main negative finding of topic 2.


---

## Topic 3 (part 2): on-demand reads, Gen3 firmware line, and Wi-Fi roaming

### Cheapest way to read ram_free, rssi, uptime on demand (VERIFIED from docs; cost and load NOT measured)
- Source: https://shelly-api-docs.shelly.cloud/gen2/HOWTO/MQTTSetup (read 2026-11-01)
  - Publish to `<shelly-id>/rpc` with the quoted example `{"id":123, "src":"mynewtopic", "method":"Shelly.GetStatus"}`.
  - "Then the response to this request will be published on topic `mynewtopic/rpc`." The response carries `id`, `src` (the Shelly ID) and `dst` (the value of `src`).
  - The same page says the device publishes "the entire status payload of components on `<topic_prefix>/status/<component:id>`" and, for `status_ntf`, "The complete status will be published if a signifficant change occurred."
  - The page does not say whether these are retained, and does not state a limit on subscriptions or concurrent requests.
- Source: https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/ (read 2026-11-01)
  - Control section: `status_update` "to trigger the component to publish it's status"; the device status is then published on `<topic_prefix>/status` "as response to `status_update` published on `<...>/command`".
  - Command topics: `shellies/command` (broadcast) and `<topic_prefix>/command`.
  - Errors: `<topic_prefix>/error/<component:id>`.
  - QoS: the component description states "The supported `Quality of service` level is 1".
  - The control section does not mention `Shelly.GetStatus` and does not describe RPC topics; those come from the setup HOWTO above.
- Interpretation (INFERENCE, not a measured cost): a `Shelly.GetStatus` over MQTT is one publish and one response, and `status_update` is one publish to the command topic. Neither is documented as periodic. Measured cost on a 40-device fleet: NOT MEASURED.

### Are ram_free, rssi, ssid, bssid pushed periodically or only on change? (NOT ESTABLISHED by docs)
- The Notifications, Sys, WiFi and MQTT pages do not state a periodic interval for NotifyStatus, NotifyFullStatus or the status topics. The WiFi page does not state when Wi-Fi status is pushed. The only timing statements are: `status_ntf` publishes "if a signifficant change occurred" (MQTT page); NotifyStatus is sent "when the object appears or its contents change" (Sys page, provisioning context).
- The sample NotifyFullStatus payload in the local code comment (`myhome/daemon/watch/mqtt.go`, around line 140) contains `sys.ram_free` and `wifi.rssi`. It is a single example and shows no timing.
- Consequence for design: the official docs do not support expecting a periodic RSSI or ram_free heartbeat from the device's MQTT stream. Whether a device emits these periodically must be measured on hardware (for example by recording a device's `events/rpc` for a fixed window). NOT MEASURED in this session; no live device was touched.

### Gen3 firmware line and the 2.0.x numbering (changelog content VERIFIED; the 2.0.x-equals-Gen3 mapping is partly verified)
- Source: https://shelly-api-docs.shelly.cloud/gen2/changelog (re-read 2026-11-01 for generation-tagged entries)
- The single changelog covers Gen2, Gen3 and Gen4 device entries under one version numbering. Gen3 entries appear inside 2.0.x releases, for example:
  - 2.0.0 (2026-07-13): "Dimmer Gen3: Add Matter support"; "HT Gen3: Add Matter support".
  - 2.0.1 (2026-09-23): "Dimmer Gen3, Dimmer Gen4: Do not reject transition duration of 0".
  - 2.0.2 (2026-10-09): "BLU Gateway Gen3: Fix slow/stalled updates".
  - 2.0.3 (2026-10-26): only "Update CA bundles, drop compromised eMudhra roots."
- Gen3-tagged Wi-Fi or MQTT entries in the 2.0.x releases: NONE found. The Wi-Fi and MQTT entries listed earlier (2.0.0, 2.0.1) carry no device-generation tag, so they read as applying to all generations (INFERENCE from absence of a tag).
- Gen3-tagged entries in earlier releases: 1.4.5 (2024-10-17) "HT Gen3 Fix unresponsive devices" (cause not stated).
- Community corroboration of the same numbering: https://github.com/mongoose-os-apps/shelly-homekit/releases/tag/2.16.1 says its build "matches the partition layout of stock 2.0.0 on the 1 Gen3 and 1PM Gen3" (community project; not an official source).
- Coverage caveat: the changelog is about 148k characters. The Gen-tagged pass read the first ~100k characters, so entries older than about 1.0.8 were not read.

### Wi-Fi roaming and the rssi threshold (VERIFIED from docs; behaviour NOT measured)
- `roam.rssi_thr` default -80: "when reached will trigger the access point roaming." `roam.interval` default 60: "Interval at which to scan for better access points. Enabled if set to positive number, disabled if set to 0." (WiFi page, https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/WiFi)
- So the firmware's own roaming trigger is -80 dBm by default. The docs do not say what happens if roaming fails. NOT ESTABLISHED.

### Shelly's own RSSI guidance (VERIFIED as quoted, but internally inconsistent)
- Source: https://allterco.freshdesk.com/en/support/solutions/articles/103000221396-shelly-devices-and-wi-fi-signals (read 2026-11-01)
- "A signal between -80 RSSI and -90 RSSI is considered as critical and connectivity issues could be faced."
- "Signal between 40-60 is considered as excellent." No unit given. This conflicts with the dBm scale used for the -80/-90 range in the same article (in dBm, -40 to -60 is the good range), so the "40-60" figure is ambiguous. Reported as found; not resolved.
- "A higher number or more bars indicate a stronger signal" (same article, unit-less).
- The article gives no disconnect rate and no measured link between RSSI and disconnects.

---

## Topic 4: leading indicators of Wi-Fi device failure, evidence vs folklore

Evidence grading used below:
- DOC: a vendor or platform document states the behaviour. It does not say the indicator predicts failure.
- FIELD: one or a few observational reports (forum, blog). Not measured.
- NONE: no evidence found in this session. Any predictive claim is folklore until measured.

| Indicator | What the device exposes | Evidence found | Grade |
|---|---|---|---|
| Low RSSI (weaker than about -80 dBm) | wifi.rssi (WiFi.GetStatus) | Shelly KB: "A signal between -80 RSSI and -90 RSSI is considered as critical and connectivity issues could be faced." (allterco freshdesk). Firmware roams at -80 by default (WiFi docs). No measured link to disconnect rate found; a search for an RSSI-as-disconnect-predictor study found only ESP8266/ESP32 RSSI characterisation papers. | Vendor threshold: DOC. Predictive value: NONE. |
| Good RSSI while devices still flap | wifi.rssi, wifi.status | HA community thread (2024-2025, https://community.home-assistant.io/t/shelly-1-gen-3-keeps-going-unavailable-and-then-available-all-is-good-in-shelly-web-ui/757089): Chuck ruled out Wi-Fi signal (devices side by side did not drop together); mirekmal ruled it out for a device beside an access point; Dazman1 saw flapping on many devices while UniFi showed 100% uptime. | FIELD. Suggests RSSI alone can miss the cause. |
| Access point or backhaul fault | Not on device (AP side) | Same thread, one poster: "A bad ethernet feed to a UniFi access point caused meshing, which affected the Shelly devices. Fixing the cable resolved it." No measurement. | FIELD (single). |
| Reboots (uptime reset) | sys.uptime (documented, seconds since reboot) | Shelly community 2.5 thread (https://shelly-forum.com/thread/12308-shelly-2-5-web-ui-status-pings-cause-random-reboots-when-both-channels-are-on): poster says a low uptime value confirms the device rebooted. A reboot is a different event from a Wi-Fi outage. | Field: DOC (the field exists). Link to Wi-Fi outage: NONE. |
| Free RAM trend (ram_free) falling | sys.ram_free (documented, bytes) | No source found tying a falling ram_free to a later Wi-Fi or MQTT failure. The Gen1 thread (https://shelly-forum.com/thread/15465-shelly-1-auto-reboot-problem-in-v1-11-8) reports free memory "staying steady rather than falling" while rebooting. Vendor changelog shows leaks were fixed: 2.0.0 "Fix memleak when component's name is cleared"; 2.0.1 "Fix digest nonce table slot leak on TTL expiry". These show leaks occurred, not that ram_free predicts outages. | Predictive value: NONE. Folklore unless measured. |
| BSSID or channel change (roaming) | wifi.bssid, channel (documented; BSSID added 1.7.0, channel added 2.0.0) | Firmware roams and reports BSSID (changelog 1.7.0). No evidence found that a BSSID change precedes outages. | NONE for prediction. |
| Raw sta_disconnected / sta_connecting events | Event names on the WiFi docs page (not on the Notifications page) | Changelog 1.3.0 (2024-04-25): "Suppress disconnected/connecting flapping notifications". The vendor treated raw transitions as noisy. | DOC that raw transitions flap. Predictive value: NONE. |
| Gateway-ping failure | Device-internal; no documented MQTT field | Changelog 1.7.1 (2025-09-24): "Implement connectivity checks with GW pings; force a disconnect if PK seems to be lost." Shows the vendor saw an associated-but-unreachable failure mode. No frequency given. | DOC for the mechanism. Frequency: NOT ESTABLISHED. |
| online topic = false (LWT or status) | <shelly-id>/online (documented) | Docs: false "in case it is forced to disconnect abruptly" (Mqtt page). HA forum thread (Dec 2025, https://community.home-assistant.io/t/shelly-mqtt-randomly-showing-offline/967379): user reports the online topic went false while "The devices are definitely online when the issue occurs"; restarting from the web UI, or re-running an announcement script, restored them. Unresolved; the thread does not establish which side was wrong. | Payload: DOC. Reliability as sole signal: CONFLICT (FIELD). |
| MQTT reconnect not happening after broker or device restart | Not directly exposed | Changelog 1.6.0: "Fix incorrect connected notification (when device tries to connect to host with stopped MQTT broker,...)"; changelog 2.0.0: "Fix missing status updated after reboot or power loss". A shelly-forum post (EM3) reports a device "stayed disconnected after a reboot" with no connection attempts logged; unresolved, single report. | DOC that these bugs existed and were fixed; single report of a stuck state. Frequency: NONE. |
| Keepalive timeout as detection delay | Broker-side | HA docs: the will is published "When the device or service loses connection to the broker" (broker-side). ioBroker search snippet (secondary): the will is sent only when the keep-alive timeout expires. Shelly docs do not state keepalive values. | Detection latency is set by the broker keepalive, which Shelly does not document. INFERENCE. |
| Espressif Wi-Fi disconnect reason codes (e.g. beacon timeout) | Not exposed by Shelly | The Espressif wifi-driver page was reached but its content was only an index; no reason codes were read. Whether Shelly firmware uses the Espressif Wi-Fi stack was not confirmed. | GAP. Not established. |

### What the evidence does and does not support
- Supported (weakly): a Shelly device can flap or go unavailable while its RSSI is good and while its own web UI looks fine. Several independent posters report this (FIELD). The cause offered most often is the access point or backhaul, which is also one poster's finding.
- Documented but unverified as predictors: RSSI below -80 to -90 (vendor calls it "critical"; no measured disconnect rate); sys.uptime reset (proves a reboot, not a Wi-Fi fault); gateway-ping failure (the vendor implemented it; frequency unknown).
- Folklore unless measured: falling ram_free predicts Wi-Fi or MQTT failure; slow RSSI decline predicts an outage; BSSID or channel changes predict flaps.
- Conflicting: online false versus a device that is "definitely online" (HA thread, Dec 2025). Unresolved.
- Not established: how often each indicator precedes an offline event; whether Shelly pushes RSSI or ram_free periodically; Gen3 2.0.x Wi-Fi or MQTT reconnect bugs (no official known issue found; one forum anecdote only).

### Note on the project's own evidence (NOT accessed)
- The project guidance (CLAUDE.md, "events.db is the long-term record") says failure rates should be answered by querying events.db, and that a mitigation must state its observed rate. The live events.db is on the NAS (documented path /var/lib/myhome/events.db). This session did not access the NAS, because it is a live system. A search of the local `~/.myhome` and the repository found no local copy of events.db or myhome.db. So none of the "predictive" rows above can be checked against this fleet from here.

---

## What could not be established (explicit gaps)

1. Whether Shelly Gen2/Gen3 push `NotifyStatus`, `NotifyFullStatus`, `sys.ram_free` or `wifi.rssi` periodically or only on change. The official Notifications, Sys, WiFi and MQTT pages do not state it. Needs a timed capture on a real device (not done; no live device touched).
2. Whether the `<shelly-id>/online` topic is retained. Not stated in the Shelly Mqtt page or HOWTO.
3. MQTT keepalive, reconnect backoff and retry schedule for Shelly devices. Not documented.
4. Whether Gen3 entries in the Gen2 API changelog reflect the same firmware line as the user's Gen3 devices, and any Gen3-specific Wi-Fi or MQTT reconnect issue in 2.0.x. No official known-issue list found. The changelog was read in two passes; the final ~48k characters (pre-1.0.8) were not read.
5. Whether the `sta1` fallback has a timeout or retry schedule. Docs say only that it "will be used as fallback when the device is unable to connect to the sta network."
6. The `ap_roaming` key the user asked about does not appear in the WiFi docs page. Treated as not documented; the nearest is `roam`.
7. Whether `sta_*` Wi-Fi events are published on MQTT or only over the websocket/RPC notify path. The Notifications page lists only single_push, sys_btn_push, config_changed; the WiFi page lists five sta_* names with no channel stated.
8. Official openHAB MQTT binding docs; official ioBroker device-availability conventions; official Domoticz dzVents `timedOut` wording (page returned 404 on fetch). Gaps stated in topic 1.
9. Zigbee2MQTT `bridge/state` last-will behaviour: the topics page does not mention a will; the troubleshooting claims come from secondary sources and an older issue.
10. Whether the LukeEvansTech exporter (GPL-3.0) actually works with Gen3 hardware; the README marks only one device as verified, and the Shelly 1 Mini Gen3 is listed only as "Supported (upstream)", which is not the same as verified. Not tested here.
11. Exporter licence conflict: lucavb/shelly-power-exporter README says MIT; GitHub API reports no license.
12. Shelly RSSI page: "40-60 excellent" has no unit and conflicts with the dBm scale used for -80/-90. Not resolved.
13. Predictive value of any leading indicator for Wi-Fi failure in this fleet. Requires the fleet's events.db (on the NAS; not accessed) and a query. No published study with disconnect-rate data was found.
14. Espressif Wi-Fi disconnect reason codes: not read (page returned only an index); whether Shelly firmware uses the same stack: not confirmed.
15. Whether the user's 12.6 h silent offline matches any documented failure mode. Not established. Candidates from the docs are only: the device never reconnected after a broker or power event (changelog 2.0.0 "Fix missing status updated after reboot or power loss" suggests this class of bug existed), or LWT never fired because the broker did not detect the drop (keepalive-dependent; INFERENCE).

## When I looked

- Date of reading: 2026-11-01 (system date). All Shelly and platform pages were fetched on this date; the page contents are as served on this date and may change.
- Shelly Gen2 API docs (shelly-api-docs.shelly.cloud/gen2/...): General/Notifications, ComponentsAndServices/Sys, ComponentsAndServices/WiFi, ComponentsAndServices/Mqtt/, HOWTO/MQTTSetup, changelog (Gen-tagged re-read). A Gen3-specific API page (shelly-api-docs.shelly.cloud/gen3/) returned 404.
- Repository metadata: GitHub REST API via `gh api`, 2026-11-01. Last push dates are as reported by the API on that date.
- Local code read: myhome/daemon/watch/mqtt.go (comment with sample NotifyFullStatus payload), pkg/shelly/system/types.go (RamFree), pkg/shelly/wifi/types.go (RSSI, RSSIThreshold). No code was changed; no tests were run.
- No live system was touched. The NAS and the live devices were not contacted.

## Related, not asked (one line each)

- aioshelly (Python, Apache-2.0, last push 2026-11-01) is the library Home Assistant uses for Gen1/Gen2 (CoAP and WebSocket); its README describes no MQTT transport. Reading its notification handling would show how a mature client treats availability.
- The sheffieldhackspace Pro 3EM project (shell, MQTT `events/rpc` NotifyStatus) is the only MQTT-driven Gen2 consumer found, and is worth reading for topic-shape only.
- The Shelly changelog is a single cross-generation list. Future Gen3 behaviour questions should be checked there first.

---

## Summary of answers (for the reader who skips to the end)

- Platforms: HA, Z2M and Tasmota publish a retained or LWT-driven availability state; Z2M adds check-in timeouts with pings; HA offers delay_on/delay_off filters. openHAB, Domoticz and ioBroker: community sources only; official pages were not retrieved or were unreachable.
- Shelly MQTT (documented): NotifyStatus/NotifyFullStatus on `<id>/events/rpc` (rpc_ntf default true); status topics `<prefix>/status/<component>` (status_ntf default false, on significant change); `<id>/online` true on connect, false on abrupt disconnect; on-demand `Shelly.GetStatus` via publish to `<id>/rpc`. Periodic push of rssi or ram_free: not documented, not established.
- Exporters: no reusable Go Gen2 MQTT device-health exporter found. Go polling exporters exist (LukeEvansTech GPL-3.0, veerendra2 Apache-2.0, jcodybaker MIT, webdevops MIT). Go MQTT bridge: hikhvar/mqtt2prometheus (MIT, generic JSON). Go client library with the strongest permissive licence: go-shelly (MIT, self-described as having meaningful gaps).
- Wi-Fi fallback: sta1 is the documented fallback; roam rssi_thr default -80. Gen3 2.0.x: no official Wi-Fi or MQTT reconnect known issue found; the 2.0.x changelog has Gen3 entries but none on Wi-Fi or MQTT.
- Leading indicators: RSSI threshold is a vendor claim with no measured predictive value; several field reports show flaps with good RSSI; uptime reset proves reboots; ram_free trend and BSSID changes are unsupported as predictors. The project's own events.db is the only place that can settle these, and it was not accessed.

Confidence: high on what the Shelly docs say (quoted, dated); medium on the exporter table (README-level, not cloned or run); low on everything marked FIELD or folklore.
