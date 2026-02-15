package static

import _ "embed"

//go:generate -command fetch go run ../../../../cmd/fetchasset
//go:generate fetch -url=https://cdn.jsdelivr.net/npm/bulma@0.9.4/css/bulma.min.css -out=bulma.min.css -sha256=ad3a5d3b41d7042369ade00772eead0763e9839d79568fb91ad612b2734bcfef

//go:embed bulma.min.css
var bulmaCSS []byte

// GetBulmaCSS returns the Bulma CSS code
func GetBulmaCSS() []byte { return bulmaCSS }
