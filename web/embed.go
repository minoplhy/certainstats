package web

import (
	"embed"
	"io/fs"
)

//go:embed templates static
var content embed.FS

// TemplatesFS returns the filesystem for HTML templates.
func TemplatesFS() fs.FS {
	sub, err := fs.Sub(content, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// StaticFS returns the filesystem for static assets (CSS, JS, images).
func StaticFS() fs.FS {
	sub, err := fs.Sub(content, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
