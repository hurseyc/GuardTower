// Package guardtower embeds the built web UI and the documentation into the
// binary so a release is a single self-contained file.
package guardtower

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var webDist embed.FS

//go:embed docs
var docs embed.FS

// WebFS returns the built SPA, or nil when the UI was not built.
func WebFS() fs.FS {
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}

// DocsFS returns the markdown documentation tree.
func DocsFS() fs.FS {
	sub, err := fs.Sub(docs, "docs")
	if err != nil {
		return nil
	}
	return sub
}
