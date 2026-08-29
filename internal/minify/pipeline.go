package minify

import (
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
	manifest  sync.Map
	minifiers sync.Map
}

// New returns a new Pipeline instance configured with standard format minifiers.
func New() Pipeline {
	p := &AssetPipeline{}
	cssMinifier := &CSSMinifier{}
	jsMinifier := &JSMinifier{}
	htmlMinifier := &HTMLMinifier{}

	p.RegisterMinifier("css", cssMinifier)
	p.RegisterMinifier("text/css", cssMinifier)

	p.RegisterMinifier("js", jsMinifier)
	p.RegisterMinifier("javascript", jsMinifier)
	p.RegisterMinifier("text/javascript", jsMinifier)
	p.RegisterMinifier("application/javascript", jsMinifier)
	p.RegisterMinifier("application/x-javascript", jsMinifier)
	p.RegisterMinifier("application/ecmascript", jsMinifier)
	p.RegisterMinifier("text/ecmascript", jsMinifier)

	p.RegisterMinifier("html", htmlMinifier)
	p.RegisterMinifier("htm", htmlMinifier)
	p.RegisterMinifier("text/html", htmlMinifier)

	return p
}

func normalizeMediaType(mediaType string) string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	mt = strings.TrimPrefix(mt, ".")
	if idx := strings.Index(mt, ";"); idx != -1 {
		mt = strings.TrimSpace(mt[:idx])
	}
	return mt
}

// RegisterMinifier registers a ContentMinifier for a given mediaType or format alias.
func (p *AssetPipeline) RegisterMinifier(mediaType string, minifier ContentMinifier) {
	cleanType := normalizeMediaType(mediaType)
	p.minifiers.Store(cleanType, minifier)
}

// Minify minifies source content based on the given mediaType by dispatching to the registered ContentMinifier.
func (p *AssetPipeline) Minify(mediaType string, src []byte) ([]byte, error) {
	norm := normalizeMediaType(mediaType)
	if val, ok := p.minifiers.Load(norm); ok {
		if m, ok := val.(ContentMinifier); ok && m != nil {
			return m.Minify(src)
		}
	}

	var matched ContentMinifier
	p.minifiers.Range(func(key, val any) bool {
		k, ok := key.(string)
		if !ok {
			return true
		}
		if (k != "" && strings.Contains(norm, k)) || (norm != "" && strings.Contains(k, norm)) {
			if m, ok := val.(ContentMinifier); ok && m != nil {
				matched = m
				return false
			}
		}
		return true
	})

	if matched != nil {
		return matched.Minify(src)
	}

	return src, nil
}

// MinifyCSS minifies stylesheet content using the registered CSS minifier.
func (p *AssetPipeline) MinifyCSS(src []byte) []byte {
	res, _ := p.Minify("text/css", src)
	return res
}

// MinifyJS minifies JavaScript content using the registered JS minifier.
func (p *AssetPipeline) MinifyJS(src []byte) []byte {
	res, _ := p.Minify("text/javascript", src)
	return res
}

// MinifyHTML minifies HTML content using the registered HTML minifier.
func (p *AssetPipeline) MinifyHTML(src []byte) []byte {
	res, _ := p.Minify("text/html", src)
	return res
}

// Process minifies the asset, computes its fingerprint and SRI, and stores the mapping in the manifest.
func (p *AssetPipeline) Process(relPath string, data []byte) (string, []byte, error) {
	cleanPath := strings.TrimPrefix(filepath.ToSlash(relPath), "/")
	ext := filepath.Ext(cleanPath)

	mediaType := strings.TrimPrefix(ext, ".")
	minified, err := p.Minify(mediaType, data)
	if err != nil {
		return "", nil, err
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

// Minify minifies source content based on the given mediaType using the default pipeline.
func Minify(mediaType string, src []byte) ([]byte, error) {
	return defaultPipeline.Minify(mediaType, src)
}

// RegisterMinifier registers a content minifier for a media type in the default pipeline.
func RegisterMinifier(mediaType string, minifier ContentMinifier) {
	defaultPipeline.RegisterMinifier(mediaType, minifier)
}

// CSS minifies stylesheet bytes using the default pipeline.
func CSS(src []byte) []byte {
	res, _ := defaultPipeline.Minify("text/css", src)
	return res
}

// JS minifies JavaScript bytes using the default pipeline.
func JS(src []byte) []byte {
	res, _ := defaultPipeline.Minify("text/javascript", src)
	return res
}

// HTML minifies HTML bytes using the default pipeline.
func HTML(src []byte) []byte {
	res, _ := defaultPipeline.Minify("text/html", src)
	return res
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
