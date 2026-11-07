# Design: kiosk shell: left navigation rail, offline-daemon banner, touch rules (#683)

Status: draft for owner review. Part of the kiosk campaign #681. Depends on
`682-retained-state.md` for the SSE heartbeat (its PR 2) and for device-free fragments.

Reference: `docs/architecture/ARCHITECTURE.md` (2026-11-04, judged current). **(verified)** means
read in code on branch `docs/681-kiosk-design`. **(inferred)** means not traced or not testable
from files.

## The ask

Replace the daemon's single long dashboard page with a touch-only kiosk shell for a 1024x600
landscape screen in Chromium kiosk mode. The shell has a left navigation rail with five
sections, its own back and close controls, an offline banner over greyed last-known data, a
project design-token sheet with the bundled Inter font, a device-row component, touch rules, and
a test plan.

## Goals and non-goals

Goals

- G1. A shell with an ~80 px left rail (icon above label): **Home, Rooms, Pool, Vacuum, Events**.
  Content uses the remaining ~944 px.
- G2. Navigation that needs no browser chrome. Every overlay and sub-view has its own close or
  back control. Nothing opens by default.
- G3. When the daemon is unreachable: an icon plus "Penates (My Home Daemon) is offline", with
  last-known data still visible, greyed, and labelled "stale since <time>". The data must be cached
  client-side before the daemon goes away.
- G4. Project design tokens as CSS custom properties applied over Bulma, plus the Inter font,
  served from the binary.
- G5. One device-row component: avatar, name, pill tags, round state button.
- G6. Touch rules that any later section PR can be checked against.
- G7. Fold in the defects that sit on the shell's path: the broken toggle response read, the unused
  toggle route, the unused stylesheet, the duplicated card templates.

Non-goals

- An offline-capable shell, meaning one that loads with the daemon down. See Scope question 3 for
  why it is not proposed.
- An on-screen keyboard. Forms that need typing (room create, device setup, Wi-Fi) stay usable
  from a desktop browser. On the kiosk they need a keyboard.
- New section *content*: live room temperatures, a full pool dashboard, vacuum redesign. The shell
  hosts the existing fragments, restyled. Richer content is for per-section issues.
- Layouts other than 1024x600 landscape. Larger screens should work (the content is fluid), but
  only 1024x600 is verified.
- Kiosk OS provisioning: Chromium flags, autostart, waiting for the daemon at boot. These belong
  in the campaign's launcher work. Requirements on it are listed under Open questions.
- A light theme, authentication, i18n.

## Quality drivers

The architecture reference states two quality attributes that apply here: local-first /
internet-optional, and operability (6-hourly restarts). This feature adds:

1. **Touch operability.** At 1024x600 with touch emulation, every interactive element in every
   section measures at least 48x48 CSS px. A DOM audit snippet (see Confirmation) reports zero
   failures.
2. **Offline behaviour.** When the daemon stops, the banner appears within 2 s and the data stays.
   When it returns, the banner clears within 5 s and the visible section re-renders. This happens
   on every 6-hourly restart, so it must never strand the screen.
3. **Internet-down.** With the WAN unplugged, the shell loads and renders fully. The network panel
   shows zero cross-origin requests. Today the page links to two external sites (verified,
   `index.html:69-70`).
4. **Upgrade without hands.** After the nightly package update, the kiosk runs the new JS and CSS
   within one SSE reconnect. Nobody touches the screen. Today `/static/*` is served
   `immutable, max-age=31536000` with unversioned URLs (verified, `server.go:91`, `index.html:26-34`),
   so a kiosk would keep stale assets.

## Fit

**The shell fits the existing architecture without a new subsystem.** It stays a server-rendered
page with htmx fragments, Alpine for local state, and SSE for push (the reference's §5 pattern). The
increments, all inside `internal/myhome/ui`:

- `static/index.html` becomes the shell. The separate event-log page, `eventLogPageHTML`, folds into
  it as the Events section. That removes the chrome duplicated across the two pages (debt #8) and
  keeps a single EventSource per page, which the liveness logic needs.
- One new fragment route, `/htmx/pool`. A fragment is needed because `/` falls through to the
  device proxy for unknown paths (verified, `server.go:76`). The shell itself uses `/` and a URL
  hash, so no new page route is needed.
- `Start` is refactored so the mux can be built separately and tested with `httptest`. Today
  routing is untested.
- New static assets: one font file and its licence. `myhome.css` is repurposed (it is embedded and
  served but linked by nothing, verified).

No new Go module, no new JS dependency, and no `go:generate` change.

## Proposed design

### 1. Page structure and routes

```
+--------+-------------------------------------------------------------+
| rail   | [offline banner, 48px, only when offline]                   |
| 80px   +-------------------------------------------------------------+
| Home   | <main class="mh-content">   (scrolls; 944px wide)            |
| Rooms  |   <section id="sec-home"   hx-get="/htmx/devices" ...>       |
| Pool   |   <section id="sec-rooms"  hx-get="/htmx/rooms" ...>         |
| Vacuum |   <section id="sec-pool"   hx-get="/htmx/pool" ...>          |
| Events |   <section id="sec-vacuum" hx-get="/htmx/vacuum" ...>        |
|        |   <section id="sec-events" hx-get="/htmx/events" ...>        |
+--------+-------------------------------------------------------------+
           overlay layer: confirm dialog, sheets, existing modals
```

- **One page, five containers.** All five sections are fetched at shell load
  (`hx-trigger="load, refresh"`). Only the current one is shown (`x-show`). Prefetching is what
  puts every section's data on the client *before* the daemon can disappear (G3). It costs five
  in-memory GETs. They are device-free once `682` PRs 5 and 6 land, and the existing Home fragment
  already loads all devices once today.
- **Routing.** `/` serves the shell. The section is in the hash (`/#rooms`), read at init. Rail
  taps update it with `history.replaceState`, so a reload lands on the same section and no
  history builds up. `/event-log` answers `302 Location: /#events`, so old bookmarks keep working.
  `/htmx/devices`, `/htmx/rooms`, `/htmx/vacuum`, `/htmx/events(/more)` and `/htmx/accounts` keep
  their URLs. `/htmx/pool` is new. It renders the pool device's row(s) and the pool pills from
  `pool.getstatus`, or an empty state "Pool tracking is not enabled" when the verb is not
  registered (the existing "feature absent" convention).
- **Accounts panel.** It moves into the Events section as a "System status" row that opens a
  sheet. It stays closed by default. This is open to the owner (Scope question 5).
- **The rail always shows all five items**, with empty states for absent features. That keeps a
  stable layout and stable muscle memory.
- **Removed from the page:** the "Shelly Control" and "Community Forum" external links, the reload
  button (a reload while the daemon is down strands Chromium on its error page, with no chrome to
  recover), and the footer.

### 2. Section loading and freshness

- **Visible section:** kept fresh by the `682` relay pattern. An SSE invalidation
  (`device-update`, later `<domain>-update`) makes the shell `htmx.ajax` the entity's fragment and
  swap it in place. The existing `device-update` handler changes only its target selector: from
  `.closest('.column')` to the row's own element.
- **Hidden sections:** invalidations for elements that are not visible set `data-dirty` on their
  section. Showing a section while online triggers `refresh` if it is dirty, or if its
  `data-fetched-at` is older than 60 s (a constant). Showing a section while offline shows its
  retained DOM, greyed.
- **Vacuum** keeps its 5 s poll until `682` follow-up F, but filtered:
  `hx-trigger="load, refresh, every 5s [kiosk.visible('vacuum') && kiosk.online]"`. An htmx trigger
  filter is standard htmx 2 syntax. It then stops costing anything while hidden or offline.
- **Fragment failure while online.** htmx does not swap on a network error or on a 5xx by default
  (htmx 2 default response handling; verified in PR 2 for the embedded 2.0.4: `responseHandling` is
  `204: no swap`, `[23]..: swap`, `[45]..: no swap, error`), so the previous DOM stays. A
  `htmx:responseError`/`htmx:sendError` handler adds a muted pill "couldn't refresh, HH:MM" to the
  section header.

### 3. Back and close without browser chrome

- The rail is the global navigation and is always tappable, even over an overlay. Tapping a rail
  item closes any open overlay and returns that section to its root view. Tapping the *active*
  item does the same, which is the conventional "go to top".
- **Overlays** (confirm dialog, device sheet, existing room, setup and heater modals) cover the
  content area only. Each has a 48 px close button (x) in its top-right corner. **The backdrop does
  not dismiss.** Accidental touches are common on a wall screen, and a half-filled form must not
  vanish. This changes today's `@click` on `.modal-background` for the three existing modals.
- **Sub-views inside a section** (none in this issue, the pattern for later ones) render a header
  row with a 48 px "Back" button on the left. No `pushState`; the state lives in Alpine.
- **Nothing opens by default:** no overlay, sheet or expanded panel is open on load, on section
  switch, or after an SSE-driven re-render.
- Outlawed in the shell: `target="_blank"`, `window.open`, external URLs, native
  `alert`/`confirm`/`prompt`. Each one strands a chrome-less browser or is unusable by touch.

### 4. Offline banner and client-side cache

State lives in the root Alpine component: `online`, `lastSeenMono` (`performance.now()` at the
last SSE frame), `lastPingTs` (server `ts` from the last `ping`), `startedAt`, and `version`
(rendered into the page from `{{.Version}}`).

- **Detection** follows the `682` §5 rule: offline when the EventSource is not `OPEN`, or when
  more than 40 s have passed since any frame, checked every 5 s. Using `performance.now()` for the
  timeout makes detection immune to wall-clock jumps on the Pi.
- **"Stale since <time>"** shows `lastPingTs`, the *daemon's* clock, formatted in the browser's
  locale (`HH:MM`, prefixed with the date if it is not today). Reason: a Pi without an RTC can boot
  with a wrong clock, and NTP may be unreachable without internet (inferred), while the daemon's
  clock is the house's reference. Until the heartbeat exists (`682` PR 2), the fallback is the
  browser time of the last frame.
- **Banner:** a 48 px strip at the top of the content area, with an inline SVG "cloud-off" icon,
  the text **"Penates (My Home Daemon) is offline"**, and on the right "stale since 14:32". The
  colour is token surface-4 with body text. The palette has no warning colour (Open question 4).
- **Greyed data:** `main.mh-content.is-stale` gets `filter: grayscale(1); opacity: .55`. Every
  action control (`[hx-post]`, `[data-action]`, form buttons) in the content gets
  `pointer-events: none`. A `htmx:beforeRequest` guard also cancels non-GET requests while
  offline. The rail stays active, so the other cached sections can still be viewed.
- **Recovery:** on the first frame after offline, the banner hides. A changed `started` makes all
  sections `refresh`. A changed `version` makes the shell `location.reload()`. It is safe to reload
  then, because the daemon has just answered.

**What is cached, and where.** The cache is **the DOM of the five prefetched sections plus
`data-fetched-at` per section, in memory.** Nothing is written to localStorage or sessionStorage in
v1. Reasoning: the shell is served by the daemon. When the daemon is down, any reload shows
Chromium's error page whatever is in storage. When the daemon is up, a reload fetches fresh data.
Persistent storage only pays off with a shell that loads offline. That needs a service worker,
which Chromium allows only in a secure context, and `http://<LAN-IP>` is not one (`localhost`
is). Hence Scope question 3. If persistence is added later, every `localStorage` access must be
wrapped in `try/catch`, because quota and disabled-storage errors throw synchronously. The snapshot
would then be `{section: {html, fetchedAt}}` plus `lastPingTs`, under one versioned key.

### 5. Design tokens (CSS custom properties)

These are the project's own tokens. They live in `static/myhome.css`, rewritten in place: it is
already embedded and in `static.Assets`, and unlinked, so no Go wiring changes. It is linked after
`bulma.min.css`.

| Token | Value | Use |
|---|---|---|
| `--mh-bg` | `#151617` | page background |
| `--mh-surface-1` … `--mh-surface-4` | `#1b1d21`, `#242527`, `#303235`, `#5b5f66` | rail, sheets, controls, borders (darkest to lightest) |
| `--mh-card` | `rgba(52,55,58,.3)` | translucent row and card surface |
| `--mh-primary` | `#2491ff` | "on" ring, active rail item, primary button |
| `--mh-primary-tint-1`, `-2` | `#182f45`, `#1a3b5d` | "on" avatar background, active rail pill |
| `--mh-secondary` | `#bbdcf8` | secondary text and pill text on tints |
| `--mh-text` / `--mh-text-muted` | `#eaeaea` / `#a1a2a2` | body / meta text |
| `--mh-radius-pill` | `200px` | pills, round buttons |
| `--mh-radius-card` | `24px` *(proposed; brief says "large")* | rows, cards, sheets |
| `--mh-font` | `"Inter", system-ui, sans-serif` | everything |
| `--mh-font-size` / `--mh-font-weight` | `16px` / `400` | body |
| `--mh-tap` | `48px` | minimum target |
| `--mh-rail-width` | `80px` | rail |

**Overriding Bulma.** Bulma 0.9.4 is compiled Sass with literal colours and exposes no CSS custom
properties. Those arrived in Bulma 1.0. This is not verifiable in the repo, because the file is
generated and gitignored. So tokens are applied by **selector overrides after Bulma**, limited to
the components the fragments actually use: `html/body`, `.title/.subtitle`, `.label/.input/.select`,
`.table`, `.tag`, `.button`, `.modal-card-*`, `.notification`, `.card`. New shell markup uses its
own `mh-` classes (rail, row, round button, banner, sheet) and does not rely on Bulma for them. That
keeps the override surface from growing with every PR.

Page meta follows the tokens: `theme-color` and `manifest.json` `theme_color`/`background_color` →
`#151617`.

### 6. Inter font: embedding and serving

- **Commit the font files. Do not generate them.** Add `static/InterVariable-<ver>.woff2` (one
  variable file covering weights 100 to 900) and `static/Inter-OFL.txt` (the SIL Open Font License
  1.1 text from the same upstream release; check the licence file in the release at commit time).
- **Why commit:** the file never changes for a given version, its licence permits redistribution,
  and committing removes a URL that has to stay alive and the need for internet in a fresh worktree.
  **The four-place `go:generate` rule is not triggered either way.** A new `fetch` line in
  `static/main.go` would run under `./internal/myhome/ui/...`, which every generate path already
  covers (reference §9, §11). Committing simply needs no generate line at all. The cost is a few
  hundred KB of binary in git history, forever. The exact size is to be measured at commit.
- **Serving:** in `static/main.go`, add `//go:embed` vars and two `Assets` entries,
  `/static/InterVariable-<ver>.woff2` (`font/woff2`) and `/static/Inter-OFL.txt`
  (`text/plain; charset=utf-8`). Serving the licence keeps it shipped with the binary, which is
  what OFL redistribution asks for. The `.gitignore` entries for generated assets name only the
  three files (reference §5.5), so `.woff2` is not ignored.
- **`@font-face`** in `myhome.css`: `font-family: Inter; src: url(/static/InterVariable-<ver>.woff2)
  format("woff2"); font-weight: 100 900; font-display: swap;`. The version is in the *filename*
  because `myhome.css` is static and cannot be templated, and the asset handler caches immutably.
- **Embedded twice (inferred).** `template.go`'s `//go:embed static/*` also embeds every file
  under `static/`. It only needs `index.html` (`staticFS` has no other caller, debt #6), so PR 1
  narrows that directive to `static/index.html`. Otherwise the font may be stored in the binary
  twice.
- **Cache-busting for the rest:** the shell links `bulma.min.css`, `htmx.min.js`, `alpine.min.js`,
  `myhome.css`, `myhome.js`, `manifest.json` and `penates.svg` with `?v=<content hash>` (the first 12
  hex digits of the SHA-256 of the embedded bytes, computed once; the `asset` template function).
  The build version cannot do this job: every hand build reports `unknown`, so the URL never
  changed and the browser kept a stale `myhome.css` for the `immutable` year. The asset handler
  looks up `r.URL.Path` only, ignoring the query (verified, `server.go`), so no server change is
  needed. The icon URL inside `manifest.json` stays unversioned: that file is static bytes and
  cannot carry a hash of another asset without generation.
- **No emoji in kiosk chrome.** The current templates use emoji as glyphs. Whether the Pi image
  ships a colour-emoji font is unverified (inferred risk: tofu boxes). Rail, avatars and state
  glyphs use inline SVG, the existing pattern.

### 7. Device row pattern

```
+------------------------------------------------------------------------+
| (O)  Kitchen ceiling                                             ( P ) |
| 48px [Kitchen] [21.4 °C] [Door open]                             48px  |
+------------------------------------------------------------------------+
 avatar  name 16px/500, one line, ellipsis; pills below          state button
```

- **Grid:** two columns at 944 px (`repeat(2, 1fr)`, 8 px gap). A row is at least 72 px tall, with
  12 px/16 px padding, `--mh-card` background and `--mh-radius-card`.
- **Avatar:** a 48 px circle, `--mh-surface-3` background, white inline-SVG glyph by device type
  (switch, thermometer, door, motion, button, pump). When "on", the background becomes
  `--mh-primary-tint-2`. **Tapping the avatar or name opens the device sheet** (id, manufacturer,
  refresh, setup, heater, web UI; see Scope question 2). That gives the four buttons on today's
  card a home without crowding the row.
- **Pills:** `--mh-radius-pill`, `--mh-surface-2` background, 14 px `--mh-secondary` text,
  carrying room, temperature, humidity, door, water supply and turnover. An unknown value shows
  `--`, never a default (matches `682`'s absent-means-unknown rule).
- **State button:** a 48 px circle with a white power glyph.

  | State | Look |
  |---|---|
  | off | `--mh-surface-3` |
  | on | `--mh-primary-tint-1` plus a 2 px `--mh-primary` ring |
  | pending | ring pulses, `disabled` |
  | unknown | muted glyph, `disabled` |

  It is present only for devices with switches.
- **One row per switch** on multi-switch devices ("Device · Switch name"), so every row has
  exactly one control. Sensor-only devices get one row per device.
- **Toggle wiring (folds defects 5, 6 and 8 of the reference's debt table):** the state button is
  `hx-post="/htmx/switch/toggle"` with `hx-vals='{"device_id":"…","switch_id":"…"}'`,
  `hx-swap="outerHTML"` and `hx-disabled-elt="this"`. The handler already exists and returns the
  *logical* state after polarity (verified, `htmx.go:215-271`; `sswitch/service.go:174`). Its
  `switchButtonTemplate` is rewritten as the round button. This **deletes** the JS `toggleSwitch`,
  which read `out.on` from a `{"result":…}` wrapper (verified, `index.html:397-399`), and gives the
  unused route its purpose. `deviceCardsTemplate` and `deviceCardTemplate` (near-identical,
  verified) are replaced by one `{{define "device-row"}}` plus a list template, parsed once at
  `init` instead of on every request.

### 8. Touch rules (normative for every kiosk PR)

1. Every interactive element is at least 48x48 CSS px, including padding. Adjacent targets are
   at least 8 px apart.
2. No information is available only on hover. `title=` is never the sole carrier (it is used
   throughout today's cards). Press feedback uses `:active`, never `:hover`.
3. `html { touch-action: manipulation; overscroll-behavior: none; -webkit-tap-highlight-color:
   transparent; }`. `user-select: none` applies to the rail, rows and buttons. A
   `contextmenu` listener calls `preventDefault()` to kill long-press menus.
4. Only `main.mh-content` scrolls. The body is fixed at `100vh` and the rail never scrolls.
5. **Confirm before destructive or long-running actions** with one shared dialog. htmx elements use
   the standard `hx-confirm="…"` attribute. A global `htmx:confirm` listener calls
   `preventDefault()`, opens the dialog, and calls `evt.detail.issueRequest(true)` on Confirm. This
   is the documented htmx 2 hook, so there is no native `confirm()`. Alpine-driven actions call
   the same dialog through `$store.confirm.ask(text)`. The dialog has a title, one line of text,
   [Cancel] on the left and [Confirm] (primary) on the right, both at least 48 px tall, with no
   backdrop dismiss and no timeout.
   - **Confirm required:** pool pump switch, vacuum start/dock/clean-room, device refresh, device
     setup, room delete, and any `device.*` write.
   - **Not required:** ordinary light and plug toggles, which are reversible, instant and frequent.
6. Text is at least 14 px everywhere, and the body is 16 px.
7. No `target="_blank"`, external links, `window.open` or native dialogs (repeated from §3, because
   reviewers check this list).
8. Disabled controls look disabled (`opacity`) *and* are `disabled`/`pointer-events: none`. Looks
   alone are not enough.

### 9. Shell JavaScript

The shell's Alpine component (navigation, SSE, offline state, confirm store, invalidation
routing) moves from inline `index.html` to `static/myhome.js`, with one `Assets` entry. This makes
it cacheable with `?v=`. It also stops it from passing through `text/template`, which parses
`index.html` (verified, `template.go:127-130`), so `{{` in JS can never collide. The existing
modal logic (rooms, setup, heater) moves with it unchanged. The event-log page's
`eventLogApp()` merges in. Its `EventSource` goes away, and the `eventlog` listener prepends rows
when `#sec-events` holds the table. The existing "ignores active filter" behaviour (debt #10) is
kept, not fixed here.

## Alternatives considered

1. **Separate full pages per section** (`/home`, `/rooms`, … as mux routes, plain links in the
   rail). In its strongest form: simplest mental model, each page independently testable, no hidden
   DOM, and memory resets per page. **It loses on the offline driver.** A rail tap while the daemon
   is down navigates to Chromium's error page, with no browser chrome to come back. Every 6-hourly
   restart would make the screen a trap for a few seconds, and permanently if tapped at the wrong
   moment. It also cannot keep other sections' data cached without persistent storage.
2. **One container, swap the section's HTML on each rail tap** (`hx-get` into a single `<main>`).
   In its strongest form: smaller DOM, and only the current section is live. **It loses because a
   section not visited before the outage has nothing to show**, so G3's "cached before the daemon
   disappears" fails. Keeping the swapped-out HTML in JS memory to compensate re-creates the
   five-container design, with more code.
3. **An offline-capable shell (service worker plus a localStorage snapshot).** In its strongest
   form, the kiosk survives a reload or a reboot while the daemon is down, and shows last-known
   data instead of an error page. **It loses on scope and on platform:** a service worker needs a
   secure context. That means a Chromium launch flag that treats the daemon's origin as secure, or
   TLS on the daemon, plus cache-versioning logic. It is kept as a Scope question, because only the
   owner can decide whether surviving a cold boot during an outage is worth that.
4. **Theme by upgrading to Bulma 1.x** (native CSS variables). In its strongest form, the tokens map
   onto Bulma's own variables and every component follows them. **It loses as a first step:** it is
   a dependency upgrade with class and markup changes across every fragment, inside a campaign that
   is already rewriting the shell. It can be revisited later. The `mh-` tokens survive such an
   upgrade.

## Consequences

- **Better:** the kiosk is usable by touch, and every outage is survivable. Upgrades reach the
  screen by themselves. One SSE connection per page. One device-row template instead of two
  copies. The toggle reads a correct state. Routing gains its first tests. The event-log page's
  duplicated chrome goes.
- **Worse:** all five sections are in the DOM at once, a bigger page than today. On the Pi it is
  tens of rows and 50 event rows, so the cost is small but unmeasured. Desktop users get the
  kiosk layout (Scope question 1). Bulma overrides by selector are brittle if a fragment starts
  using a new Bulma component; the `mh-` classes limit this.
- **Harder to change later:** the hash routing and the single-page assumption. Moving to separate
  pages later would need the offline story redone. The `hx-confirm` interception becomes the one
  confirmation mechanism other sections depend on.

## Cross-cutting concerns

- **Security:** not changed (LAN, no auth; reference §13). Removing `target="_blank"` external links
  slightly reduces the surface. If the device-web-UI iframe (Scope question 2) is adopted, the
  device page becomes same-origin with the shell through the proxy. That is the trust it already
  has today in a tab.
- **Privacy:** not affected.
- **Observability:** the shell logs SSE state changes to the console (kept). The "couldn't refresh"
  pill makes failed fragment loads visible on screen. Server side, nothing changes.
- **Failure modes and degraded modes:**
  - Daemon down: banner, greyed cached sections, actions disabled, rail still works. A cold boot
    during an outage shows Chromium's error page (the launcher's problem; Open question 1).
  - Internet down: no effect. All assets are in the binary, and the external links are removed.
  - Broker down while the daemon is up: data is not marked stale (`682` Open question 3).
  - One fragment failing: previous content kept, plus the pill.
- **Rollout / migration:** PR 1 changes only look and assets. PR 2 replaces the page. `/event-log`
  redirects, and fragment URLs are unchanged, so `ctl` and other users are unaffected. Each PR can be
  reverted on its own. No data migration.

## Impact on existing conventions

- **Extends** the reference's "Add a UI section" recipe: sections are containers in the shell, not
  pages. A new section adds a rail item, a container and a fragment route, and must follow §8.
- **Bends** "fragments parsed per request" (debt #9): new templates are parsed once at `init`.
- **Changes** existing modal behaviour (backdrop no longer closes them), on desktop too.
- **Adds** two committed binary/static assets. That bends the "assets are generated" habit, for
  the reasons in §6. It does not touch the `go:generate` registration rule.
- **Removes** the separate event-log page template. `/event-log` becomes a redirect.

## PR decomposition

Each PR is reviewable in one sitting. Across all of them, these must not change: fragment URLs
other than the new `/htmx/pool`, SSE event names and payloads, RPC verbs, and `/devices/{token}/`
proxying.

1. **Tokens, font, dark base.** Files: `static/myhome.css` (rewritten), `static/InterVariable-<ver>.woff2`
   and `static/Inter-OFL.txt` (new, committed), `static/main.go`, `template.go` (narrow
   `go:embed`), `static/index.html` and `events_template.go` (link `myhome.css`, `?v=` on assets,
   theme-color), `static/manifest.json`. Acceptance: a Go test asserts the new `Assets` entries,
   their content types and non-empty bodies, and that the rendered index contains `?v=<version>`
   links. In Chromium at 1024x600: dark background, Inter listed in devtools "Rendered Fonts", zero
   cross-origin requests. Degraded: none.
2. **Shell, rail and sections.** Files: `static/index.html` (shell), `static/myhome.js` (new),
   `server.go` (`newMux` split, `/htmx/pool`, `/event-log` redirect), `htmx.go` (pool fragment),
   `events_template.go` (page removed, fragments kept), a new `server_test.go`. Acceptance:
   `httptest` shows `GET /` has five rail items and five section containers with the right `hx-get`,
   `GET /event-log` → 302 `/#events`, `GET /htmx/pool` is 200 (not proxied), and there is no
   `target="_blank"` or `http(s)://` in the shell. In Chromium: no overlay open on load, every section
   reachable, a reload on `/#rooms` lands on Rooms, and the vacuum poll stops when hidden (network
   panel). Degraded: an internet-down load shows no failed requests.
3. **Offline banner and stale view.** Needs `682` PR 2 (heartbeat). Files: `myhome.js`,
   `myhome.css`, `index.html`. Acceptance: a Go test asserts the banner markup contains exactly
   "Penates (My Home Daemon) is offline". In Chromium: stopping the daemon shows the banner within
   2 s and greys the content, buttons are inert, and "stale since" equals the last ping time.
   Restarting it clears the banner within 5 s and refreshes sections. A version change triggers one
   reload. DevTools "Offline" throttling (a silent partition) shows the banner within 45 s.
4. **Device row, toggle and sheet.** Files: `htmx.go` (row templates replace both card templates;
   `switchButtonTemplate` rewritten), `myhome.js` (delete `toggleSwitch`, update `device-update`
   target, device sheet), `htmx_pool_test.go` (adapt), a new `htmx_row_test.go`. Acceptance:
   template tests cover on/off/unknown/pending classes, one row per switch, and pills showing `--`
   when unknown. A `SwitchButton` handler test with a fake `switch.toggle` handler (saves and
   restores the methods map in `t.Cleanup`, no `t.Parallel()`) renders the ring when `On`. In
   Chromium: a tap toggles the light and the button shows pending, then the new state.
5. **Confirm dialog and touch hardening.** Files: `myhome.js`, `myhome.css`, the vacuum, rooms and
   events fragments (`htmx_vacuum.go`, `htmx.go`, `events_template.go`: drop `title=`-only info,
   add `hx-confirm`, enlarge targets), the existing modals (close button, no backdrop dismiss).
   Acceptance: the tap-target audit reports zero failures in all five sections and all overlays,
   every action in the §8.5 list shows the dialog, and Cancel sends no request (network panel).

Order: 1 → 2 → (3 once `682` PR 2 is merged) and 4 in either order → 5.

## Confirmation

- **Go tests** (run by `make test`): asset entries, shell structure, routes (`httptest` against
  `newMux`), row and toggle templates, and the banner text.
- **Chromium verification protocol** (each PR's description reports it):
  1. DevTools → Device toolbar → custom device "Kiosk": **1024 x 600, DPR 1, type Mobile (touch)**,
     landscape. Confirm `matchMedia('(hover: none)').matches === true`.
  2. Network panel: "Disable cache" *off* (test the real caching), filter `-domain:<daemon host>`,
     and expect zero rows.
  3. Tap-target audit, pasted in the console:
     `[...document.querySelectorAll('button,a,[hx-post],[hx-get][role=button],input,select')].filter(e=>e.offsetParent&&(e.getBoundingClientRect().width<48||e.getBoundingClientRect().height<48))`
     must return `[]` in each section and overlay.
  4. The offline drill from PR 3.
  5. A final pass on the real Pi screen. The emulator does not prove the touch digitiser, DPR or
     fonts.
- **Review questions:** "Does anything open by default?", "Does every overlay have its own close?",
  "Any `target=_blank`, external URL, `title=`-only info or native dialog?", "Do the new templates
  render unknown as `--`?"

## Scope questions for the owner

1. **One UI for desktop and kiosk, or a separate kiosk mode?** One UI is smaller: a single shell
   and single tests. Desktop users then get the rail layout and the stricter modal behaviour.
   *Recommendation:* one UI.
2. **Device web UI link.** Today it opens in a new tab (`target="_blank"`), which strands a kiosk.
   Options: (a) drop it from the kiosk, keeping it reachable from a desktop browser by URL;
   (b) open `/devices/{token}/` in an in-shell iframe sheet with a close button. Option (b) works
   only if device pages tolerate framing and the proxy does not add frame-blocking headers. That is
   unverified. *Recommendation:* (a) in this issue; (b) as a follow-up after a framing test on one
   device.
3. **Offline-capable shell** (service worker plus a localStorage snapshot, needing a secure-origin
   Chromium flag or TLS) vs **DOM retention only**. *Recommendation:* DOM retention. The only gain
   is surviving a kiosk reboot during a daemon outage, which the launcher can handle by waiting for
   `/_health`.
4. **Inter as one variable file vs two static weights (400, 600).** One file is simpler (one asset,
   any weight). Two static files are probably smaller (unmeasured). *Recommendation:* one variable
   file, unless its measured size exceeds ~400 KB.
5. **Where the Accounts panel goes.** It has no rail slot. *Recommendation:* a "System status" row
   in Events that opens a sheet. The alternative is the bottom of Home.

## Open questions

1. **Kiosk launcher (outside this issue):** does Chromium auto-retry its own network-error page when
   the daemon is down at boot? If not, the launcher must wait for `GET /_health` before starting
   Chromium. The banner only works once the shell has loaded once.
2. **Pi display facts:** the DPR (1 assumed) and whether Chromium reports `hover: none` with only
   the touchscreen attached. An attached mouse flips it to `hover: hover`. The design never depends
   on hover, so this only affects the verification step.
3. **Card radius:** the brief says "large card radii" without a value; 24 px is proposed.
4. **No warning or error colour in the token palette.** The offline banner and pool "water supply
   paused" currently need one. Should a warning token be added, or should these use surface-4 plus
   an icon, as proposed?
5. **Pool section content beyond the pump row and pills:** whose issue is it? This design ships
   only the container and the minimal fragment.
