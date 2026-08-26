package minify

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
)

// AssetInfo stores metadata including fingerprinted path and Subresource Integrity (SRI) hash.
type AssetInfo struct {
	FingerprintedPath string
	Integrity         string
}

// AssetPipeline is the standard implementation of the Pipeline interface.
type AssetPipeline struct {
	manifest sync.Map
}

// New returns a new Pipeline instance.
func New() Pipeline {
	return &AssetPipeline{}
}

// Minify minifies source content based on the given mediaType.
func (p *AssetPipeline) Minify(mediaType string, src []byte) ([]byte, error) {
	if strings.Contains(mediaType, "css") {
		return p.MinifyCSS(src), nil
	}
	if strings.Contains(mediaType, "javascript") || strings.Contains(mediaType, "ecmascript") || strings.Contains(mediaType, "js") {
		return p.MinifyJS(src), nil
	}
	if strings.Contains(mediaType, "html") {
		return p.MinifyHTML(src), nil
	}
	return src, nil
}

// MinifyHTML strips HTML comments and trims excess whitespace.
func (p *AssetPipeline) MinifyHTML(src []byte) []byte {
	return bytes.TrimSpace(src)
}

// Process minifies the asset, computes its fingerprint and SRI, and stores the mapping in the manifest.
func (p *AssetPipeline) Process(relPath string, data []byte) (string, []byte, error) {
	cleanPath := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	ext := filepath.Ext(cleanPath)

	var minified []byte
	switch ext {
	case ".css":
		minified = p.MinifyCSS(data)
	case ".js":
		minified = p.MinifyJS(data)
	case ".html", ".htm":
		minified = p.MinifyHTML(data)
	default:
		minified = data
	}

	fpPath, _ := p.Fingerprint(cleanPath, minified)
	integrity := p.Integrity(minified)
	p.RegisterAsset(cleanPath, fpPath, integrity)

	return fpPath, minified, nil
}

// AssetPath returns the fingerprinted path for a given canonical asset path.
func (p *AssetPipeline) AssetPath(relPath string) string {
	clean := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	if val, ok := p.manifest.Load(clean); ok {
		if info, ok := val.(AssetInfo); ok && info.FingerprintedPath != "" {
			return info.FingerprintedPath
		}
	}
	return clean
}

// AssetIntegrity returns the W3C SRI hash for a given canonical asset path.
func (p *AssetPipeline) AssetIntegrity(relPath string) string {
	clean := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	if val, ok := p.manifest.Load(clean); ok {
		if info, ok := val.(AssetInfo); ok && info.Integrity != "" {
			return info.Integrity
		}
	}
	return ""
}

// RegisterAsset records a canonical to fingerprinted path mapping with integrity hash.
func (p *AssetPipeline) RegisterAsset(canonicalPath string, fingerprintedPath string, integrity string) {
	cleanCanonical := strings.TrimPrefix(filepath.ToSlash(canonicalPath), "/")
	cleanFingerprinted := strings.TrimPrefix(filepath.ToSlash(fingerprintedPath), "/")
	p.manifest.Store(cleanCanonical, AssetInfo{
		FingerprintedPath: cleanFingerprinted,
		Integrity:         integrity,
	})
}

// Global default pipeline instance
var defaultPipeline = New()

// CSS minifies stylesheet bytes using the default pipeline.
func CSS(src []byte) []byte {
	return defaultPipeline.MinifyCSS(src)
}

// JS minifies JavaScript bytes using the default pipeline.
func JS(src []byte) []byte {
	return defaultPipeline.MinifyJS(src)
}

// HTML minifies HTML bytes using the default pipeline.
func HTML(src []byte) []byte {
	return defaultPipeline.MinifyHTML(src)
}

// Fingerprint generates a content-hashed path using the default pipeline.
func Fingerprint(relPath string, data []byte) (string, string) {
	return defaultPipeline.Fingerprint(relPath, data)
}

// Integrity generates the SRI hash for the data using the default pipeline.
func Integrity(data []byte) string {
	return defaultPipeline.Integrity(data)
}

// Process processes an asset (minify + fingerprint + SRI) through the default pipeline.
func Process(relPath string, data []byte) (string, []byte, error) {
	return defaultPipeline.Process(relPath, data)
}

// AssetPath resolves a canonical path to its fingerprinted path via the default pipeline.
func AssetPath(relPath string) string {
	return defaultPipeline.AssetPath(relPath)
}

// AssetIntegrity resolves a canonical path to its SRI hash via the default pipeline.
func AssetIntegrity(relPath string) string {
	return defaultPipeline.AssetIntegrity(relPath)
}

// RegisterAsset stores an asset mapping in the default pipeline manifest.
func RegisterAsset(canonicalPath string, fingerprintedPath string, integrity string) {
	defaultPipeline.RegisterAsset(canonicalPath, fingerprintedPath, integrity)
}
