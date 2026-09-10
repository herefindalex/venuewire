//go:build !webui

package webassets

import "io/fs"

func FS() (fs.FS, bool) { return nil, false }
