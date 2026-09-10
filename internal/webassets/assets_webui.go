//go:build webui

package webassets

import (
	"embed"
	"io/fs"
)

//go:embed dist/*
var content embed.FS

func FS() (fs.FS, bool) {
	assets, err := fs.Sub(content, "dist")
	return assets, err == nil
}
