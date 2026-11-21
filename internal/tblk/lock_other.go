//go:build !unix

package tblk

import "os"

// lockDir is a no-op where flock is missing; one process per store is then
// up to the program.
func lockDir(dir string) (*os.File, error) { return nil, nil }
