# Static assets

Generated (gitignored, fetched by `go generate`, see `main.go`): `bulma.min.css`,
`htmx.min.js`, `alpine.min.js`.

Committed:

- `InterVariable-4.1.woff2`: Inter variable font (weights 100 to 900), Inter 4.1.
  - Source: <https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip>, member `web/InterVariable.woff2`
  - Release zip sha256: `9883fdd4a49d4fb66bd8177ba6625ef9a64aa45899767dde3d36aa425756b11e`
  - File sha256: `693b77d4f32ee9b8bfc995589b5fad5e99adf2832738661f5402f9978429a8e3` (352240 bytes)
- `Inter-OFL.txt`: the SIL Open Font License 1.1 text (`LICENSE.txt` of the same release),
  sha256 `262481e844521b326f5ecd053e59b98c8b2da78c8ee1bdbb6e8174305e54935a`. Served at
  `/static/Inter-OFL.txt` so the licence ships with the binary.
- `myhome.css`: design tokens and dark base.

To upgrade Inter, rename the font file, and update `main.go` and the `@font-face` url in `myhome.css`.
