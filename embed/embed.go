package embed

import (
	"embed"
	"io/fs"
)

//go:embed all:data
var embedFS embed.FS

// FS returns embed filesystem.
func FS() fs.FS {
	sub, _ := fs.Sub(embedFS, "data")
	return sub
}
