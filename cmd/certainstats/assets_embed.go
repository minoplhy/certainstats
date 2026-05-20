//go:build embed

package main

import (
	frontendadmin "certainstats/frontend-admin"
	frontendpublic "certainstats/frontend-public"
	"io/fs"
)

func getFrontendFS(path string) fs.FS {
	if path == "frontend-admin/out" {
		sub, err := fs.Sub(frontendadmin.FS, "out")
		if err == nil {
			return sub
		}
	} else if path == "frontend-public/out" {
		sub, err := fs.Sub(frontendpublic.FS, "out")
		if err == nil {
			return sub
		}
	}
	return fs.FS(nil)
}

const isEmbedded = true
