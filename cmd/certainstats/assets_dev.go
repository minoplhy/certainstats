//go:build !embed

package main

import (
	"certainstats/web"
	"io/fs"
)

func getStaticFS() fs.FS {
	return web.StaticFS()
}

const isEmbedded = false
