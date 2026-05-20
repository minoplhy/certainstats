//go:build !embed

package main

import (
	"io/fs"
	"os"
)

func getFrontendFS(path string) fs.FS {
	return os.DirFS(path)
}

const isEmbedded = false
