package ui

import _ "embed"

//go:embed assets/ws-patch.js
var wsPatchJS []byte

// GetWsPatch returns the WebSocket patch JavaScript code
func GetWsPatch() []byte { return wsPatchJS }
