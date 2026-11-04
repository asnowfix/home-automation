# Mater France as a solar production provider, and Linky TIC as a grid signal

Status: research note, not a design. Facts are cited; inferences are marked "Inferred". Gaps are listed in the last section.
Progress log: `docs/mater-france-research-progress.md`.
Looked at: 2026-11-01. Sources were read on that date. Documentation versions and "last pushed" dates are as reported on that day.

## What was asked

1. What Mater France sells, its monitoring stack, and the underlying hardware vendor (OEM).
2. Public or reverse-engineered APIs (cloud, local), auth, polling, rate limits, discovery, community clients and their licences.
3. Which data is available, its latency, and whether it can be read locally and offline.
4. Terms-of-service or warranty risks of local access.
5. A recommendation for a provider abstraction, and the unknowns the user must resolve.
Also: whether Linky TIC readers (standard mode, SINSTS/SINSTI) are a viable local grid-point signal.

## Correction of the earlier research

`docs/solar-router-research.md` (section "Matter France") treated "Matter France" as the Matter smart-home protocol and found no provider. That was wrong. Mater France (materfrance.fr) is a French online seller of solar kits. Its kits use Deye microinverters on the Solarman cloud platform (see Finding 1). Nothing in this note depends on the Matter protocol. A web search for a Matter-based Mater France product on 2026-11-01 returned nothing relevant (only unrelated French solar monitoring results, none mentioning Matter or Mater France).

## Summary

- **OEM:** The one product page found (a Que Choisir test of a "Materfrance kit 2PV 1030 W") lists a **Deye SUN-M100G4-EU-Q0** microinverter and monitoring in the **Solarman Smart** app. A forum post and a Trustpilot company reply also mention Hoymiles hardware for earlier Materfrance kits. So the current OEM is probably Deye, but this is one product page, and the materfrance.fr site could not be read (HTTP 403).
- **Local access:** The Deye M-series microinverter has built-in Wi-Fi and a Modbus RTU register map. The same registers can be read over a Solarman V5 TCP wrapper on port 8899, using the logger serial number. Two references do this: evcc's Deye template and plugin (Go, MIT) and the Hypfer project (JavaScript, no licence). Hypfer verified the SUN-M80G3 and SUN-M80G4, not the M100G4. Firmware is the main risk: Deye firmware updates from 2024 reportedly block that port. No 2025-26 report for the M-series logger was found.
- **Data:** Output active power (W) and lifetime energy (kWh) are readable locally from documented registers. Grid import/export and per-panel data are not in the micro-inverter register map as far as was checked. Per-panel and grid data are cloud-side (Solarman), and the Solarman Smart Meter has no documented local API.
- **Linky TIC:** Viable as the one grid-point signal, in **standard mode** (9600 baud). The Enedis spec defines SINSTS (consumed apparent power) and SINSTI (injected apparent power), recomputed every second. Readers exist as ESP32/ESPHome devices, Zigbee (ZLinky), and Python. No Go application reads the standard mode. The meter must be in standard mode and configured to report injection.

## Finding 1: what Mater France sells, and the OEM

| Claim | Source | Status |
|---|---|---|
| Kit "2PV 1030 W": 2 bifacial mono panels of 515 W, microinverter **Deye SUN-M100G4-EU-Q0** (1000 VA). | [Que Choisir product page](https://www.quechoisir.org/comparatif-kits-solaires-n131170/materfrance-kit-2pv-1030-w-p279892/) (fetched; test is paywalled; the page shows "Nombre de panneaux 2" and the Deye model) | Verified for that page. Page date not shown. |
| "La connectivité est assurée via le Wi-Fi du micro-onduleur, sans possibilité d'extension." | same | Verified (quoted). |
| "Le suivi de la production est assuré par l'application Solarman Smart, disponible sur Android et iOS." | same | Verified (quoted). |
| SUN-M60/80/100G4-EU-Q0 has "Wi-Fi communication", "module level monitoring", "15 Years warranty", rated AC output 1000 W for the M100G4. The datasheet does not name Solarman or any local interface. | [Deye datasheet, fr.deyeinverter.com, 2024-10-21 (PDF)](https://fr.deyeinverter.com/deyeinverter/2024/10/21/datasheet_sun-m60-80-100g4-eu-q0_241021_en.pdf) (text extracted with pdftotext) | Verified. |
| Earlier Materfrance kits used Hoymiles. A company reply on a 2024-05-03 review refers to a "Hoymiles interface". | [Trustpilot, materfrance.fr](https://www.trustpilot.com/review/materfrance.fr) (page summarised by the fetch tool; the exact reply text was not checked) | Single source, secondhand. |
| "Materfrance switched brands from Howmiles HMS2000 to Deye." (2024-04-26 forum post) | [HACF thread 39857](https://forum.hacf.fr/t/existe-t-il-la-possibilite-de-connecter-a-home-assistant-londuleur-wifi-deye-sun-m200g4-eu-q0-2000w/39857) | One forum user's statement. Not verified. |
| A Deye SUN-M200G4 kit bought from Materfrance was set up in Solarman (2024). | same thread, posts 2024-05-22 to 2024-05-29 | User report. |
| A Dealabs deal page shows a "Solarman Smart Meter Suivi Consommation" at 59,60 EUR. | [Dealabs deal page](https://www.dealabs.com/bons-plans/solarman-smart-meter-suivi-consommation-3125122) | The page **does not name Materfrance** as seller and the deal has expired. A search snippet that named Materfrance (at 79 EUR) was not confirmed. **Whether Materfrance sells this meter: not established.** |
| Materfrance sells "single-phase self-consumption kits of 1 to 3 kWc". | Search snippet of a Vitepromo page (page not fetched) | Unverified. |

Battery and separate gateway: **not established.** The Que Choisir page describes the Wi-Fi as part of the micro-inverter, so no separate gateway box was found for that kit. No Materfrance battery product was found.

Reputation (secondary, not used for technical claims): Trustpilot shows 4.9/5 over about 3,860 reviews, with a 2-star review dated 2026-07-28 titled "Poor support and failed components". The Signal-Arnaques reports that flag materfrance.fr were visible only as search snippets (the pages returned 403), so their content is unverified.

## Finding 2: monitoring stack and APIs

### 2a. Cloud (Solarman)

- Consumer app: **Solarman Smart** (iOS and Android) per the Que Choisir page above. The App Store listing is at [apps.apple.com/app/id1469487897](https://apps.apple.com/app/id1469487897) (search snippet, not fetched).
- Official API: Solarman OpenAPI. Per the [Solarman help centre article "I want to open API"](https://helpcenter.solarmanpv.com/portal/en/kb/articles/i-want-to-open-api-how-can-i-open-api) (fetched; undated):
  - "the API service is free for SOLARMAN Smart user" with a free tier of "API call volume of 200,000 times per year" and "Number of plants within 3". Exceeding either "will incur a fee". If the limit is exceeded, "we will have to temporarily suspend your service."
  - "Before using the API, please read the 'SOLARMAN OpenAPI Developer Agreement'." Acceptance is by email to customerservice@solarmanpv.com. The agreement is an image on the page and was not readable.
- Latest public API document found: Solarman OpenAPI **v1.1.0** (2019-2020 PDF, mirrored in an evcc issue attachment: [SolarmanOpenAPI.v1.1.0.-EN.pdf](https://github.com/evcc-io/evcc/files/10950986/SolarmanOpenAPI.v1.1.0.-EN.pdf); not fetched in this session). Endpoints named in community posts (not checked against the current portal): `POST /account/v1.0/token` and `POST /device/v1.0/currentData` (with bearer token and `deviceSn`). Inferred: these may have changed; the current developer portal was not found.
- Rate limits: the free-tier annual cap above is the only official figure found. The per-second or per-minute limits are **not established**. A Homey forum thread reports a temporary "allowance" block that cleared after about two hours (anecdotal). The help centre has an error article for "2101010 appid insufficient allowance" (URL returned 404 in this session).
- Latency: no official figure. One community report says the API data lags about 6 minutes (per search summary of a Home Assistant thread; not checked). The App Store snippet says the app "is updated every 30 minutes, on average" (unverified).

### 2b. Local (Modbus over Solarman V5 TCP, port 8899)

- **Transport:** Solarman V5 is a TCP framing that wraps Modbus RTU frames, normally on port 8899 of the logger. Source: [pysolarmanv5 (MIT)](https://github.com/jmccrohan/pysolarmanv5) (README, via search snippet), and the Go library described next.
- **Go library:** [404GamerNotFound/solarman-go](https://github.com/404GamerNotFound/solarman-go), MIT, pushed 2026-10-25, not archived, 0 stars. Its README: "a Go client for the local Solarman V5 TCP transport exposed by compatible data loggers, normally on port `8899`. It forwards Modbus RTU read and write requests through the logger to the connected inverter." It needs "The logger's numerical serial number", and it says "verify whether register addresses are zero- or one-based and which byte order, signedness and scaling a value uses." Reads: holding (0x03) and input (0x04) registers. Writes exist (0x06, 0x10). Connection reuse, calls serialised, one retry on transport error.
- **evcc (Go, MIT, pushed 2026-11-01, 7,486 stars):**
  - Docs: [docs.evcc.io Deye micro inverter (Solarman)](https://docs.evcc.io/en/meters/deye-micro-inverter-solarman): parameters `host`, `serial` ("WiFi logger serial number ... shown under device information on the logger status page"), `port` (default 8899), `id` (default 1).
  - Template [templates/definition/meter/deye-mi-solarman.yaml](https://github.com/evcc-io/evcc/blob/master/templates/definition/meter/deye-mi-solarman.yaml): power = holding register **86** ("Output active power"), `uint32s`, scale **0.1** (W); energy = holding register **63** ("Total production"), `uint32s`, scale **0.1** (kWh).
  - Plugin [plugin/solarman.go](https://github.com/evcc-io/evcc/blob/master/plugin/solarman.go) wraps solarman-go.
  - evcc's `uint32s` decoder is `encoding.Uint32LswFirst` ([util/modbus/register.go](https://github.com/evcc-io/evcc/blob/master/util/modbus/register.go)), i.e. low word first. Verified from the code.
- **Register map:** Deye Modbus RTU specification **V118** (Chinese and English), in [Hypfer/deye-microinverter-cloud-free-assets](https://github.com/Hypfer/deye-microinverter-cloud-free-assets) (file `Deye_Modbus.-.-.V118.pdf`; repo has no licence; last push 2023-03-08). I rendered page 12 of 43 and read it visually. In the "String" (non-hybrid) column:
  - 084 / 085: output apparent power, low / high word, 0.1 VA.
  - **086 "Output active power low word", 087 "Output active power high word", 0.1 W.**
  - 063 / 064: "Total_Active_PowerWh" low / high word, 0.1 kWh (text extraction; not visually checked).
  - 060: "DayActive PowerWh" (daily active energy, 0.1 kWh, signed).
  - Inferred: the evcc template (register 86, low word first, scale 0.1) matches this table.
- **Hypfer README** ([Hypfer/deye-microinverter-cloud-free](https://github.com/Hypfer/deye-microinverter-cloud-free), read via `gh api` README; repo has **no licence**, last push 2025-05-08, 192 stars, JavaScript):
  - "So far, the following devices are known to work like this: Deye SUN600G3-EU-230, Deye SUN-M80G3-EU-Q0, Deye SUN-M80G4-EU-Q0", and "it should also be the same for other inverters of the same series including rebrands such as Bosswerk."
  - "Most of the options rely on polling the local modbusTCP interface that uses the inverter logger serial number for authentication."
  - Docker example: `DEYE_LOGGER_PORT=8899`. Also port 48899 with AT commands.
  - Minimum logger firmware recommended: `MW3_16U_5406_1.53`. Older firmware "don't allow reconfiguration of the Wi-Fi AP, which is a serious vulnerability".
  - 2024-08-20 update: "Deye has started rolling out firmware updates for the logger that block both ModbusTCP access and prevent the mock cloud from working due to the introduction of TLS. Fortunately, for now it seems that newly bought inverters still come with older logger firmwares that work offline. ... However, both of these things may change in the future." And: "it seems that downgrading the firmware is possible as well."
  - "They can operate without an internet connection" and "They can operate without an account". Also: "with internet access blocked, the inverter never receives any time information. This breaks the `Yield today` counter".
- **Discovery:** Not established for the M-series. The HACF thread reports that the ha-solarman integration auto-discovered the device once the IP was set to 0.0.0.0 and the serial to 0 (user tip, 2024-05-29). This was not verified.

### 2c. Current firmware status (2025-2026)

- No 2025 or 2026 report for the **M-series** logger was found.
- Reports from 2026 concern the **hybrid / SUN-8K** loggers with "DYDA_WiBLE" firmware, not the M-series. [ha-solarman discussion #1159](https://github.com/davidrapan/ha-solarman/discussions/1159) (reporter, 2026-09-15): TCP 8899, 10000 and 502 "completely blocked or disabled"; UDP 8899 "responsive using standard Solarman V5 UDP frames". No maintainer reply. Later comments (single users, not verified): firmware 1.7.1 "doesn't serve 8899" and 1.7.2 re-enabled Modbus TCP after a Deye support update.
- A Home Assistant thread (2026, [community.home-assistant.io/t/…/997672](https://community.home-assistant.io/t/connecting-to-the-new-deye-logger-to-home-assistant-using-esphome/997672)) reports that a BLE local mode works via the Deye Cloud app and ESPHome, and that "the logger only allows one BLE central at a time" and stops cloud upload while connected. Hybrid loggers; not the M-series.
- Inferred: the M-series logger may follow the same firmware path. The firmware string on the user's unit determines whether TCP 8899 is open. This must be checked on the device.

### 2d. Alternative local paths, not verified for Mater France

- Hypfer's dummycloud (a local replacement for the Solarman cloud, MQTT output): described in the Hypfer README; not examined in detail.
- Bluetooth (BLE) local mode via the Deye Cloud app: described for hybrid loggers only.

## Finding 3: data available, latency, local or offline

| Quantity | Available locally? | Basis |
|---|---|---|
| Output active power, W (AC, from the micro-inverter) | Yes, registers 86/87, 0.1 W | Deye spec V118; evcc template ([links above](#2b-local-modbus-over-solarman-v5-tcp-port-8899)). Verified on paper; not tested on a device. |
| Lifetime energy, kWh | Yes, registers 63/64, 0.1 kWh | Same. |
| Daily energy, kWh | Yes, register 060 (signed), 0.1 kWh, from the spec. Reset depends on the inverter's clock. | Deye spec V118; Hypfer (clock issue). |
| Per-panel power | Not found in the local map. A German forum report (search summary) says local polling gives only current power, daily and total yield, while the cloud gives per-module detail. | Secondary. Not established. |
| Grid import / export, consumption | Not in the micro-inverter register map (checked for the "String" column only). Grid meter data from Solarman is cloud-side. | Inferred from the map; not verified for the M100G4. |
| Solarman Smart Meter (CT, grid) | No local API found. The installation guide describes "real-time data transmitting & data analysis by using SOLARMAN Cloud Platform". | [Solarman install guide](https://www.solarmanpv.com/introduction-and-installation-guide-of-the-solarman-smart-meter) (search summary, not fetched). |

Latency and polling:
- Local polling: the poll interval is whatever the client chooses. No documented limit on logger polling was found. evcc's default was not established. The solarman-go client has a single timeout for each operation.
- Cloud: no official latency figure. Community: about 6 minutes (unverified); app: about 30 minutes (App Store snippet, unverified).

Offline operation: Hypfer states that the inverter can run without internet, but time sync stops. Whether the M100G4 behaves the same is not verified.

## Finding 4: terms of service and warranty risks of local access

- **Warranty:** The Deye datasheet states "15 Years" warranty. The warranty conditions were **not** read. Whether local access, blocking the internet, or firmware changes affect the warranty: **not established**.
- **Firmware changes:** Hypfer recommends a minimum firmware level, and points to a third-party firmware repository ([dasrecht/deye-firmware](https://github.com/dasrecht/deye-firmware)) for downgrades. Flashing non-vendor firmware is a risk that the sources do not assess.
- **Solarman terms:** The "SOLARMAN OpenAPI Developer Agreement" exists and must be accepted to use the API. Its text was an image on the help-centre page and was not readable. The solarmanpv.com home page links to a privacy policy, not to terms of use (the privacy policy was not read). Whether local V5 access breaches any term: **not established**.
- **Security:** The Hypfer project describes the logger serial number as the only authentication for the Modbus TCP interface (per README). This is a property of the device, not a Mater France term.
- **Consumer-protection context:** negative consumer reports about Materfrance exist (see Finding 1), but they do not address local access.

## Finding 5: Linky TIC as a local grid-point signal

### 5a. Specification (primary)

- Document: [Enedis Sorties de télé-information client des appareils de comptage Linky (enedis.fr/media/2035/download)](https://enedis.fr/media/2035/download). The cover is **Enedis-MOP-CPT_002E, version 1, dated 15/09/2025**, which "remplace à l'identique la note Enedis-NOI-CPT_54E". The attached body is **Enedis-NOI-CPT_54E, version 3, dated 01/06/2018**, 38 pages. Cite both.
- Standard mode (verified in the text):
  - Baud rate "9600 bauds ± 1%" (Table in §5.3). Carrier "modulée à 50 kHz ± 3%".
  - Frames "émises les unes après les autres en continu". The text gives gaps between frames of 16.7 to 33.4 ms but **does not state a full-frame period**.
  - SINSTS: "Puissance app. Instantanée soutirée", 5 characters, VA.
  - SINSTI: "Puissance app. Instantanée injectée", 5 characters, VA.
  - §6.2.3.10: apparent powers (drawn and injected) "sont réévaluées toutes les secondes". A single-phase meter computes "total injectée + total soutirée".
  - In the section describing the data groups: "Seuls les groupes correspondants à la configuration du compteur (consommateur/producteur – monophasé/triphasé) sont transmis." So SINSTI is sent only when the meter is configured to measure injection. Whether the user's meter is configured so: **not established** (check on the meter or with the supplier).
  - Version field "02" (§6.2.3.2).
- Historical mode (1200 baud, NOI-CPT_02E): per a summary of [enedis.fr/media/2122/download](https://www.enedis.fr/media/2122/download) (search summary, not fetched). Historical mode has PAPP (apparent power), not SINSTS/SINSTI.
- Switching mode: per the same summary, the supplier requests the change through the Enedis SGE portal (procedure PRO-CF_55E). Not verified in the primary text.

### 5b. Readers

| Option | Licence, activity | Mode | Labels | Output / notes | Source |
|---|---|---|---|---|---|
| **ZLinky_TIC (LiXee)**, Zigbee 3.0 module, powered from the meter | Firmware repo `fairecasoimeme/Zlinky_TIC`: C, licence **none stated**, pushed 2025-11-25, 458 stars | Standard and historical (`linky_mode` auto/historique/standard) | SINSTS as `apparent_power`; SINSTI as `injected_v_a`; CCASN as `active_power`; EAST as `current_summ_delivered` | **Zigbee only.** Needs a Zigbee coordinator (ZHA or Zigbee2MQTT). Report interval "MAX report interval 20 seconds" (changelog v15); some attributes polled, default 600 s. No network output. | [Zigbee2MQTT ZLinky_TIC](https://www.zigbee2mqtt.io/devices/ZLinky_TIC.html), [repo](https://github.com/fairecasoimeme/Zlinky_TIC) |
| **linky_modbus** (ESPHome component, ESP32 or ESP8266) | `tgrauss/linky_modbus`, MIT, pushed 2025-11-03 | Standard | SINSTS, SINST1-3, CCASN (no SINSTI listed) | Modbus **RTU** registers, RS485. No update rate stated. | [repo](https://github.com/tgrauss/linky_modbus) |
| **ESP32_linky_Teleinfo** (f2knpw) | No licence file; pushed 2026-07-08; 1 star | Not stated | Not stated | MQTT, "instantly via MQTT to Home Assistant". | [repo](https://github.com/f2knpw/ESP32_linky_Teleinfo) |
| **linkytic** (Python, Home Assistant integration) | `hekmon/linkytic`, MIT, pushed 2026-09-12, 74 stars | Historical stable; standard mode in beta (README) | PAPP, IINST (historical names); SINSTS/SINSTI not named in the README | Serial, or remote via serialx (TCP, RFC2217, ESPHome). Observed update "semble être de 30 secondes". | [repo](https://github.com/hekmon/linkytic) |
| **goteleinfo** (Go library) | `j-vizcaino/goteleinfo`, **GPL-3.0**, pushed 2024-03-09, 11 stars | Not stated | Not stated (README only mentions OPTARIF/HPHC as examples) | A decoder library with an example HTTP/JSON server. Standard mode not verified. | [repo](https://github.com/j-vizcaino/goteleinfo) |

No Go application that reads the standard mode (SINSTS/SINSTI) was found.

### 5c. Viability

- Standard mode is required for SINSTS/SINSTI. Historical mode gives PAPP instead (per the historical-mode references).
- The readers listed reach SINSTS (and SINSTI for ZLinky) in standard mode. ESP32 and Python options are local; ZLinky needs a Zigbee coordinator.
- Inferred: a local grid-point signal is viable. The user's meter must be in standard mode and must report injection (SINSTI) for a surplus calculation that needs export. The user should check which mode and which configuration the meter has.

## Recommendation (observation for the provider abstraction, not a design)

- Keep **production** and **grid** as different signals. Mater France data (Deye micro-inverter) is production-only locally. The Linky standard mode gives grid import and export. The existing `SolarReading` has no kind or sign field (see `docs/solar-router-research.md`).
- Model Mater France as a **Deye micro-inverter via Solarman V5**: parameters host, logger serial, port (8899), Modbus unit ID (1). Read registers 86/87 (W) and 63/64 (kWh). Treat the firmware and port check as a precondition, and fall back to the cloud (Solarman OpenAPI, 200,000 calls per year free) only if local access is closed. Inferred: the cloud route needs the developer agreement and has its own latency.
- For grid: an optional Linky reader interface, with SINSTS and SINSTI as separate fields. Its implementation depends on the hardware the user chooses.
- Keep the internet-optional and daemon-optional rules in mind: Solarman cloud calls need a timeout and a no-op path, and local polling must not block.

## Unknowns the user must resolve

1. **Box model and firmware.** Read the model label on the micro-inverter (M60, M80, M100 or M200 G4 or G3) and the logger firmware on the logger's status page (Hypfer: "Device Information" on the Status page; minimum `MW3_16U_5406_1.53`). The firmware string decides whether TCP 8899 is open.
2. **Logger serial** (printed on the sticker and on the status page). Needed for every V5 request.
3. **Whether TCP 8899 answers** on the user's logger, and whether registers 86/87 return a sane value at daytime production. This has not been tested on any device.
4. **Whether the kit has a battery, a separate gateway, or a Solarman Smart Meter.** The Que Choisir page covers one kit only.
5. **The Linky meter's mode** (standard or historical) and whether it reports injection (SINSTI). Check the meter's display or ask the supplier.
6. **Warranty conditions** from Materfrance or Deye for local access and firmware changes.
7. **Solarman developer agreement** text (image on the help centre page) if the cloud route is chosen.

## What I could not establish

- The materfrance.fr product catalogue, manuals, FAQ and terms of sale: the site returned HTTP 403 to every page tried (home, kit page, root).
- Mater France's legal entity, SIREN and address.
- Battery, gateway or Solarman Smart Meter sales by Mater France, except as unverified search snippets.
- Current OEM across all Mater France kits. Only one product page (Deye M100G4) was read.
- Whether the M100G4 or any M-series microinverter responds to Solarman V5 on TCP 8899 on current firmware. No 2025-26 report for the M-series was found.
- Current Solarman OpenAPI endpoints, per-second rate limits, and the text of the developer agreement.
- Solarman latency and the exact sample interval of the cloud data.
- Per-panel data path: whether any local route provides it.
- Whether Solarman Smart Meter has any local interface (Modbus, HTTP): not found.
- Warranty and terms: no source found that addresses local access.
- The full-frame repetition period of the Linky standard mode (the spec gives only inter-frame gaps; a frame period was not found in the text searched).
- Whether SINSTI appears on the user's meter (depends on its configuration).
- Whether goteleinfo decodes standard mode.
- Whether any Go project reads the Linky standard mode.

## Related, not asked

- The evcc Go plugin and solarman-go are reusable references for V5 framing (both MIT). Their licences were checked from the GitHub API; the code was read, not run.
- Hypfer's dummycloud (local Solarman-cloud replacement, MQTT) exists; not examined.
- Hoymiles local options exist (community, for HMS-xxxW models): [ha-hoymiles-wifi](https://github.com/suaveolent/ha-hoymiles-wifi), [dtuGateway](https://osrtos.com/projects/dtugateway-for-hoymiles-hms-inverters). Relevant only if the user's box is Hoymiles.
- Matter 1.4 (CSA) defines Solar Power and Water Heater device types, but no Mater France product uses Matter (see the Correction section).

## When I looked, and what I read

- 2026-11-01 (today). Versions read:
  - Enedis-MOP-CPT_002E v1 (15/09/2025) and attached Enedis-NOI-CPT_54E v3 (01/06/2018), text by pdftotext.
  - Deye datasheet SUN-M60/80/100G4-EU-Q0 (2024-10-21).
  - Deye Modbus RTU spec V118 (repo last pushed 2023-03-08).
  - evcc master (pushed 2026-11-01): template `deye-mi-solarman.yaml`, `plugin/solarman.go`, `util/modbus/register.go`.
  - solarman-go (pushed 2026-10-25); Hypfer README (repo pushed 2025-05-08).
  - Solarman help centre "I want to open API" (undated).
- Web pages read: Que Choisir (Deye M100G4), Trustpilot (materfrance.fr, summary), HACF thread 39857 (2024), ha-solarman discussion #1159 (2026), HA community thread 997672 (2026), zigbee2mqtt ZLinky page, GitHub repos listed above.
- Materfrance and Signal-Arnaques pages were not readable (HTTP 403). Those claims come from search summaries and are marked as such.

## Confidence

- OEM for the Que Choisir kit (Deye M100G4, Solarman Smart): **medium-high** (product page read; Deye datasheet confirms Wi-Fi and warranty).
- Current OEM across Mater France: **low** (one product page; older Hoymiles references are secondhand).
- Local register map and evcc template: **high on the documentation**, medium on applicability to the M100G4 (not tested).
- Local access on current firmware: **low** (2024 firmware note; 2026 reports for a different logger family).
- Linky standard mode fields and 9600 baud: **high** (Enedis text read). Frame period and the user's configuration: **not established**.
