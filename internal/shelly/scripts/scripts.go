package scripts

import (
	"embed"
	"io/fs"
)

//go:embed *.js
var content embed.FS

// GetFS returns the embedded filesystem containing all Shelly scripts
func GetFS() fs.FS {
	return content
}
