package proxy

import _ "embed"

//go:embed assets/ws-patch.js
var wsPatchJS []byte

func getWsPatch() []byte { return wsPatchJS }
