# Mater France research progress

Task: identify Mater France (materfrance.fr, French PV installer/provider) as a solar production data provider; check Linky TIC viability. Started 2026-11-01.

## Step 0 (local)
- Read docs/solar-router-research.md. Its "Matter France" entry treated it as the Matter protocol, and it was never identified. Local repo: pkg/beem is the only solar provider; no Linky/TIC/Enphase/Modbus code.

## Step 1 (materfrance.fr identity)
- materfrance.fr home and /kit-solaire-autoconsommation return HTTP 403 to WebFetch. Not readable directly; retry later with other paths.
- Que Choisir product page (fetched 2026-11-01) https://www.quechoisir.org/comparatif-kits-solaires-n131170/materfrance-kit-2pv-1030-w-p279892/ : Materfrance kit "2PV 1030 W": 2 x 515 W bifacial mono panels, micro-onduleur Deye SUN-M100G4-EU-Q0 (1000 VA); "Le suivi de la production est assuré par l'application Solarman Smart"; Wi-Fi on the micro-inverter, no extension. Full test paywalled.
- Conclusion so far: OEM for this kit = Deye (microinverter), cloud app = Solarman Smart. Need primary confirmation from Deye datasheet and Solarman.
- Reputation (secondary, unverified): Signal-Arnaques alerts (URLs seen in search, not fetched yet); Trustpilot 4.9 claim from monkitsolaire (secondary).

## Step 2 (Deye datasheet, local access)
- Deye datasheet SUN-M60/80/100G4-EU-Q0 (fr.deyeinverter.com, 2024-10-21, PDF text extracted with pdftotext): "Wi-Fi communication"; rated AC 1000 W for M100G4; "module level monitoring"; "25 years design lifetime and 15 years warranty"; Communication: Wi-Fi. Datasheet does NOT name Solarman or any local API.
- Hypfer/deye-microinverter-cloud-free (gh api 2026-11-01): license null (none), pushed 2025-05-08, 192 stars, JS. README (fetched via gh api readme): known-working list = SUN600G3-EU-230, SUN-M80G3-EU-Q0, SUN-M80G4-EU-Q0; "it should also be the same for other inverters of the same series including rebrands such as Bosswerk"; "Local polling ... modbusTCP interface that uses the inverter logger serial number for authentication"; port 8899 in docker example; port 48899 AT commands; 2024-08-20 update: Deye firmware "block both ModbusTCP access and prevent the mock cloud ... due to the introduction of TLS"; minimum logger firmware MW3_16U_5406_1.53.
- M100G4 specifically: NOT in Hypfer's verified list. Inference only that it probably behaves like M80G4.
- Note: raw.githubusercontent and blob URLs gave 404 for Hypfer; use gh api for READMEs.

## Step 3 (cloud app, Solarman, Materfrance product mix)
- Trustpilot materfrance.fr (fetched 2026-11-01): 4.9/5, 3,860 reviews per page; company reply (2024-05-03) about a "Hoymiles interface" -> Materfrance also sells Hoymiles-based kits (single source, verify). Company reply: "Il vous suffit dans un premier temps de demander l'ouverture de votre compte".
- Dealabs deal (search snippet, expired 2025-09-21): Materfrance sold "Solarman Smart Meter DDZY422-D2" at 79 EUR (was 129), "Suivi en temps réel ... via l'application", Wi-Fi. Need to fetch dealabs page and check Solarman meter local access.
- Solarman Smart app (apps.apple.com id1469487897): available iOS/Android; sources summarise "updated every 30 minutes" (App Store review, unverified).
- Solarman Open API v1.1.0 (2019-2020) PDF mirrored at github.com/evcc-io/evcc/files/10950986/SolarmanOpenAPI.v1.1.0.-EN.pdf (attachment in an evcc issue); requires developer registration via sales; base api.solarmanpv.com; /device/v1.0/currentData. Current portal unverified.
- Materfrance product page and monitoring pages still 403 to WebFetch.

## Step 4 (Hoymiles, Solarman meter, correction)
- CORRECTION: the Dealabs page (fetched directly) does NOT name Materfrance as seller; it shows "Compteur intelligent monophasé Solarman Smart Meter Suivi Consommation" at 59,60 EUR and says the deal is no longer active. The earlier search-snippet claim (79 EUR, Materfrance seller) is NOT verified by the page. Treat as unverified.
- Hoymiles: Trustpilot company reply mentions "Hoymiles interface" (single source). No materfrance Hoymiles product page found in search. Hoymiles has no own-brand kits per PromptQuorum (weak source).
- Hoymiles local options (community, unofficial): suaveolent/ha-hoymiles-wifi (HMS-xxxW models; warns polling <32s may disable cloud); osrtos dtuGateway (ESP32, HMS-xxxW-2T built-in DTU); Homey app TCP 10081 protobuf. Not relevant unless the box is Hoymiles.
- Solarman Smart Meter DDZY422-D2 / DTSD422-D3: Wi-Fi, CT clamps; datasheet mentions GPRS/NB-IoT/WiFi/4G; "real-time data transmitting ... by using SOLARMAN Cloud Platform" (solarmanpv install guide). No local API found. Unverified RS485/Modbus exposure.

## Step 5 (Linky TIC, primary spec)
- Enedis spec fetched (enedis.fr/media/2035/download, PDF text via pdftotext): cover is "Enedis-MOP-CPT_002E" v1 dated 15/09/2025 which "remplace à l'identique" Enedis-NOI-CPT_54E; the attached body is NOI-CPT_54E version 3 dated 01/06/2018 (38 pages). Use both references.
- Verified text: standard mode baud 9600 +/-1% (Table in 5.3.x); carrier 50 kHz; frames "émises ... en continu"; SINSTS = "Puissance app. Instantanée soutirée" 5 chars VA; SINSTI = "Puissance app. Instantanée injectée" 5 chars VA; apparent powers "sont réévaluées toutes les secondes" (6.2.3.10); "Seuls les groupes correspondants à la configuration du compteur (consommateur/producteur – monophasé/triphasé) sont transmis" (so SINSTI only when meter configured to report injection - verify per meter); version "02" (6.2.3.2).
- Not established: explicit full-frame period in ms/s (spec says inter-frame gap 16.7-33.4 ms; cycle time not found in grep).
- Historical mode 1200 baud per NOI-CPT_02E (secondary citation of enedis.fr/media/2122).
- Zlinky/ZLinky_TIC: Zigbee2MQTT device page (not fetched yet), dresden deCONZ draft doc, forum says ZHA values update ~5 min (forum post).
- Solarman terms: solarmanpv.com homepage has no terms page link found (only privacy-policy.html, not read). Negative result on terms.

## Step 7 (HA/HACF forum evidence, local access, firmware)
- HACF thread https://forum.hacf.fr/t/existe-t-il-la-possibilite-de-connecter-a-home-assistant-londuleur-wifi-deye-sun-m200g4-eu-q0-2000w/39857 (fetched): posts 2024-04-11..2024-06-14. Post 2024-04-26 (Argonaute): "Materfrance switched brands from Howmiles HMS2000 to Deye" (one user statement, unverified). 2024-05-29: user with WavLink in AP mode got "deye_4mppt.yaml" Solarman integration working (IP 0.0.0.0, serial 0 auto-discovery). 2024-06-14 (Benoit_Dierickx, M100G4 kit): production visible in Solarman app but "rien ne remonte dans HA". Outcome of that user unknown (page 2 not read).
- Search: github.com/davidrapan/ha-solarman discussion #1159 (DYDA_WiBLE_1.6.2 logger: TCP 8899/10000/502 blocked; UDP 8899 still answers V5) - firmware-dependent. HA community thread 997672 (ESPHome to new Deye logger; user switched to Deye Cloud app BLE local mode). Not yet fetched.

## Step 8 (current 2026 firmware reports, different logger family)
- github.com/davidrapan/ha-solarman discussions/1159 (fetched): firmware DYDA_WiBLE_1.6.2 on SUN-8K hybrid (reporter 2026-09-15): TCP 8899/10000/502 blocked; UDP 8899 answers V5. Unanswered by maintainer. Reports: 1.7.1 doesn't serve 8899 (secondhand, 2026-10-14); 1.7.2 reported to re-enable Modbus TCP (2026-10-15). Single-user reports. NOT the M-series logger.
- HA community thread 997672 (fetched): March-Oct 2026 posts on DL1000B / SUN-8K. BLE local mode via Deye Cloud app + ESPHome works per users; BLE blocks Deye cloud sync while connected (one BLE central at a time). Hybrid-logger evidence, not M-series.
- Repo metadata (gh api): davidrapan/ha-solarman MIT, pushed 2026-05-03, 541 stars. StephanJoubert/home_assistant_solarman Apache-2.0, ARCHIVED, pushed 2026-10-20, 740 stars. jmccrohan/pysolarmanv5 MIT, pushed 2026-03-08, 183 stars.

## Step 9 (Solarman API terms, primary helpcenter)
- helpcenter.solarmanpv.com "I want to open API" (fetched, undated): "the API service is free for SOLARMAN Smart user"; free tier limits: "API call volume of 200,000 times per year", "Number of plants within 3"; "Before using the API, please read the 'SOLARMAN OpenAPI Developer Agreement'" (agreement is an image, not read); "If you exceed the limit, we will have to temporarily suspend your service."
- 2101010 'appid insufficient allowance' article: 404 at tested URL.
- Solarman OpenAPI v1.1.0 (2019-20 PDF) remains the only official API doc found; no 2026 currentData schema found.
- Next: Materfrance manuals on mirakl CDN / PDFs; Solarman Smart Meter local; then write note.

## Step 10 (evcc Deye microinverter Solarman template - Go project, MIT)
- docs.evcc.io/en/meters/deye-micro-inverter-solarman (fetched): "Deye Micro inverter (Solarman)", usage pv; params host, port (8899), serial (WiFi logger serial, "shown under device information on the logger status page"), id (1).
- Template file templates/definition/meter/deye-mi-solarman.yaml in evcc-io/evcc (gh api, copy in scratchpad): source "solarman"; power = holding register 86 "Output active power", uint32s, scale 0.1; energy = holding register 63 "Total production", uint32s, scale 0.1. Register addresses are as commented in the template; not checked against Hypfer's Modbus spec.
- evcc repo: MIT, pushed 2026-11-01, 7486 stars.
- Next: find evcc Go implementation of the solarman source and go-solarman (caarlos0) licence.

## Step 12 (final)
- Final note written: docs/mater-france-research.md (2026-11-01). Remaining gaps listed in its "What I could not establish" section.
