package minify

// Minifier defines the contract for asset content minification by media type.
type Minifier interface {
	Minify(mediaType string, src []byte) ([]byte, error)
}

// ContentMinifier defines the generic contract for a format-agnostic content minifier.
type ContentMinifier interface {
	Minify(src []byte) ([]byte, error)
}

// MinifierFunc is an adapter to allow the use of ordinary functions as ContentMinifiers.
type MinifierFunc func(src []byte) ([]byte, error)

// Minify calls f(src).
func (f MinifierFunc) Minify(src []byte) ([]byte, error) {
	return f(src)
}

// Fingerprinter defines the contract for content-hash asset versioning and SRI.
type Fingerprinter interface {
	Fingerprint(relPath string, data []byte) (fingerprintedPath string, hash string)
	Integrity(data []byte) string
}

// Pipeline combines minification, fingerprinting, and manifest management.
type Pipeline interface {
	Minifier
	Fingerprinter
	Process(relPath string, data []byte) (processedPath string, processedData []byte, err error)
	AssetPath(relPath string) string
	AssetIntegrity(relPath string) string
	RegisterAsset(canonicalPath string, fingerprintedPath string, integrity string)
	RegisterMinifier(mediaType string, minifier ContentMinifier)
}

