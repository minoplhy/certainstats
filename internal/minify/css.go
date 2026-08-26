package minify

import (
	"bytes"
)

// MinifyCSS minifies stylesheet content by stripping comment blocks and collapsing whitespace.
func (p *AssetPipeline) MinifyCSS(src []byte) []byte {
	if len(src) == 0 {
		return src
	}

	var buf bytes.Buffer
	buf.Grow(len(src))

	n := len(src)
	i := 0

	for i < n {
		// Strip comments /* ... */
		if i+1 < n && src[i] == '/' && src[i+1] == '*' {
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}

		// Handle string literals inside url(...) or quotes
		if src[i] == '"' || src[i] == '\'' {
			quote := src[i]
			buf.WriteByte(quote)
			i++
			for i < n && src[i] != quote {
				if src[i] == '\\' && i+1 < n {
					buf.WriteByte(src[i])
					buf.WriteByte(src[i+1])
					i += 2
					continue
				}
				buf.WriteByte(src[i])
				i++
			}
			if i < n {
				buf.WriteByte(quote)
				i++
			}
			continue
		}

		// Collapse whitespace
		if isWhitespace(src[i]) {
			// Skip whitespace if preceding character is a delimiter
			if buf.Len() > 0 && isCSSDelimiter(buf.Bytes()[buf.Len()-1]) {
				i++
				continue
			}

			// Lookahead: skip whitespace if next non-whitespace char is a delimiter
			j := i
			for j < n && isWhitespace(src[j]) {
				j++
			}
			if j < n && isCSSDelimiter(src[j]) {
				i = j
				continue
			}

			// Otherwise write a single space
			buf.WriteByte(' ')
			i = j
			continue
		}

		// Remove redundant semicolon before closing brace '}'
		if src[i] == ';' {
			j := i + 1
			for j < n && isWhitespace(src[j]) {
				j++
			}
			if j < n && src[j] == '}' {
				i = j
				continue
			}
		}

		buf.WriteByte(src[i])
		i++
	}

	return bytes.TrimSpace(buf.Bytes())
}

func isWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isCSSDelimiter(c byte) bool {
	return c == '{' || c == '}' || c == ':' || c == ';' || c == ',' || c == '>' || c == '~' || c == '(' || c == ')'
}
