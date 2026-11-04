# Fix: myhome .deb conflicts with prometheus-mqtt-exporter (#261)

## Root cause

`myhome` ships `/etc/prometheus/mqtt-exporter.yaml`, a path already owned (as a
conffile) by the upstream Debian package `prometheus-mqtt-exporter`
(`hikhvar/mqtt2prometheus` upstream). dpkg refuses to install when both
packages claim the same path.

## Findings

- `prometheus-mqtt-exporter` (confirmed on the NAS, v0.1.7-1+b4) only accepts
  a single config file via `-config <path>` (default
  `/etc/prometheus/mqtt-exporter.yaml`). It has no conf.d/include mechanism,
  so we cannot "drop in" a fragment at the exporter-config level.
- systemd unit drop-ins (`<unit>.service.d/*.conf`) are a package-agnostic
  override mechanism that works regardless of what the exporter itself
  supports. We can use this to repoint `-config` at a myhome-owned file
  without ever touching anything `prometheus-mqtt-exporter` owns.

## Approach

Ship our config under our own namespace and override the exporter's
`ExecStart` via a drop-in, instead of claiming the upstream's path:

1. `linux/prometheus/mqtt-exporter.yaml` → renamed to
   `mqtt-exporter.yaml.sample`, installed to
   `/usr/share/myhome/prometheus-mqtt-exporter.yaml.sample`.
2. `postinst.sh` copies the sample to `/etc/myhome/prometheus-mqtt-exporter.yaml`
   if missing — same pattern already used for `myhome.yaml`.
3. New `linux/prometheus/myhome.conf` systemd drop-in, installed to
   `/lib/systemd/system/prometheus-mqtt-exporter.service.d/myhome.conf`,
   overriding `ExecStart` to pass `-config /etc/myhome/prometheus-mqtt-exporter.yaml`.
4. `postinst.sh` already does `systemctl daemon-reload` + conditional restart
   of `prometheus-mqtt-exporter` if installed and active — this now also
   picks up the new drop-in.
5. `postrm.sh` gets a matching conditional restart on remove/purge so the
   exporter reverts to its own default config once our drop-in is gone
   (dpkg removes `/lib/systemd/system/.../myhome.conf` automatically since
   it's a plain package file, not a conffile).

No `Conflicts:`/`Replaces:` needed, no `--force-overwrite` workaround. myhome
never owns a path belonging to `prometheus-mqtt-exporter`, so future upstream
changes to that package can't reintroduce the conflict.

## Phases

- [x] Phase 1 — plan (this file)
- [x] Phase 2 — rename yaml to `.sample`, add systemd drop-in unit file
- [x] Phase 3 — update `Makefile` `debpkg` target
- [x] Phase 4 — update `.github/workflows/package-release.yml`
- [x] Phase 5 — update `postinst.sh`
- [x] Phase 6 — update `postrm.sh`
- [x] Phase 7 — update `docs/prometheus-mqtt-exporter.md`
- [x] Phase 8 — validate (shell syntax, yaml lint, `make test`)
