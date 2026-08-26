package minify

import (
	"bytes"
)

// MinifyJS minifies JavaScript content by stripping comments and compressing whitespace.
func (p *AssetPipeline) MinifyJS(src []byte) []byte {
	if len(src) == 0 {
		return src
	}

	var buf bytes.Buffer
	buf.Grow(len(src))

	n := len(src)
	i := 0

	for i < n {
		// Single-line comment // ...
		if i+1 < n && src[i] == '/' && src[i+1] == '/' {
			i += 2
			for i < n && src[i] != '\n' && src[i] != '\r' {
				i++
			}
			continue
		}

		// Multi-line comment /* ... */
		if i+1 < n && src[i] == '/' && src[i+1] == '*' {
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}

		// String literals: '...', "...", `...`
		if src[i] == '"' || src[i] == '\'' || src[i] == '`' {
			quote := src[i]
			buf.WriteByte(quote)
			i++
			for i < n {
				c := src[i]
				if c == '\\' && i+1 < n {
					buf.WriteByte(c)
					buf.WriteByte(src[i+1])
					i += 2
					continue
				}
				buf.WriteByte(c)
				i++
				if c == quote {
					break
				}
			}
			continue
		}

		// Regular expressions: /pattern/flags (distinguish from division operator)
		if src[i] == '/' && isRegexStart(buf.Bytes()) {
			buf.WriteByte(src[i])
			i++
			for i < n {
				c := src[i]
				if c == '\\' && i+1 < n {
					buf.WriteByte(c)
					buf.WriteByte(src[i+1])
					i += 2
					continue
				}
				buf.WriteByte(c)
				i++
				if c == '/' {
					// Read trailing flags: g, i, m, s, u, y, d
					for i < n && isIdentifierChar(src[i]) {
						buf.WriteByte(src[i])
						i++
					}
					break
				}
			}
			continue
		}

		// Collapse whitespace
		if isWhitespace(src[i]) {
			// Find next non-whitespace char
			j := i
			for j < n && isWhitespace(src[j]) {
				j++
			}

			if buf.Len() > 0 && j < n {
				prev := buf.Bytes()[buf.Len()-1]
				next := src[j]

				// If between two identifier/keyword characters, maintain a single space
				if (isIdentifierChar(prev) || isKeywordChar(prev)) && (isIdentifierChar(next) || isKeywordChar(next)) {
					buf.WriteByte(' ')
				} else if (prev == '+' && next == '+') || (prev == '-' && next == '-') {
					buf.WriteByte(' ')
				}
			}

			i = j
			continue
		}

		buf.WriteByte(src[i])
		i++
	}

	return bytes.TrimSpace(buf.Bytes())
}

func isIdentifierChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}

func isKeywordChar(c byte) bool {
	return isIdentifierChar(c)
}

func isRegexStart(prevBytes []byte) bool {
	if len(prevBytes) == 0 {
		return true
	}
	// Look at the last non-space character
	for i := len(prevBytes) - 1; i >= 0; i-- {
		c := prevBytes[i]
		if isWhitespace(c) {
			continue
		}
		// Characters that precede a regex literal
		return c == '(' || c == ',' || c == '=' || c == ':' || c == '[' || c == '!' || c == '&' || c == '|' || c == '?' || c == '{' || c == ';' || c == '~' || c == '+' || c == '-' || c == '*' || c == '/'
	}
	return true
}
