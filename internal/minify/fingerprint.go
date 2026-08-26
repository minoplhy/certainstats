package minify

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Fingerprint generates a content-hashed path for an asset using the first 8 characters of its SHA256 hash.
// Example: "css/styles.css" with data -> ("css/styles.8f3b9a12.css", "8f3b9a12")
func (p *AssetPipeline) Fingerprint(relPath string, data []byte) (string, string) {
	hashBytes := sha256.Sum256(data)
	hash := hex.EncodeToString(hashBytes[:4])

	clean := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	ext := filepath.Ext(clean)
	base := strings.TrimSuffix(clean, ext)

	if ext == "" {
		return base + "." + hash, hash
	}

	return base + "." + hash + ext, hash
}

// Integrity generates the W3C Subresource Integrity (SRI) string for the given data using sha256.
// Example: "sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="
func (p *AssetPipeline) Integrity(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256-" + base64.StdEncoding.EncodeToString(h[:])
}
