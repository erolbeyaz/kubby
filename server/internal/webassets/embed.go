// Package webassets embeds the built frontend so a release is a single binary.
package webassets

import (
	"embed"
	"io/fs"
)

// dist holds the Vite build output. A placeholder index.html is committed so the
// package always compiles; `make build` overwrites it with the real bundle.
//
//go:embed all:dist
var dist embed.FS

// FS returns the embedded site rooted at the directory containing index.html.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
