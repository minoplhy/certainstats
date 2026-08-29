package minify

import (
	"bytes"
)

// HTMLMinifier minifies HTML content.
type HTMLMinifier struct{}

// Minify strips excess whitespace from HTML content.
func (m *HTMLMinifier) Minify(src []byte) ([]byte, error) {
	return bytes.TrimSpace(src), nil
}
