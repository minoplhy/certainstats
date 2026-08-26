package minify

// Minifier defines the contract for asset content minification.
type Minifier interface {
	Minify(mediaType string, src []byte) ([]byte, error)
	MinifyCSS(src []byte) []byte
	MinifyJS(src []byte) []byte
	MinifyHTML(src []byte) []byte
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
}
