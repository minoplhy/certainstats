package minify

import (
	"strings"
	"testing"
)

func TestMinifyCSS(t *testing.T) {
	input := `
		/* Header Navigation Style */
		:root {
			--bg-primary: #0a0a0c;
			--accent-primary: #6366f1;
		}

		.navbar {
			display: flex;
			align-items: center;
			padding: 12px 24px;
			background-color: var(--bg-primary);
		}
		.dropdown {
			top: calc(100% + 8px);
			max-width: calc(100vw - 32px);
		}
	`

	got := string(CSS([]byte(input)))

	if strings.Contains(got, "/*") || strings.Contains(got, "*/") {
		t.Errorf("expected comments to be stripped, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("expected newlines to be collapsed, got %q", got)
	}
	if !strings.Contains(got, ":root{--bg-primary:#0a0a0c;--accent-primary:#6366f1}") {
		t.Errorf("expected collapsed CSS rules, got %q", got)
	}
	if !strings.Contains(got, ".navbar{display:flex;align-items:center;padding:12px 24px;background-color:var(--bg-primary)}") {
		t.Errorf("expected collapsed navbar rules, got %q", got)
	}
	if !strings.Contains(got, "top:calc(100% + 8px)") {
		t.Errorf("expected calc(100%%%% + 8px) with whitespace around + preserved, got %q", got)
	}
	if !strings.Contains(got, "max-width:calc(100vw - 32px)") {
		t.Errorf("expected calc(100vw - 32px) with whitespace around - preserved, got %q", got)
	}
}

func TestMinifyJS(t *testing.T) {
	input := `
		// Telemetry helper
		function formatBytes(bytes) {
			/* multiline comment */
			if (!bytes || bytes === 0) {
				return '0 B';
			}
			const units = ['B', 'KB', 'MB', 'GB'];
			let v = bytes;
			let i = 0;
			const str = "hello /* not a comment */ world";
			const tpl = ` + "`" + `value: ${bytes}` + "`" + `;
			return v + ' ' + units[i];
		}
	`

	got := string(JS([]byte(input)))

	if strings.Contains(got, "Telemetry helper") || strings.Contains(got, "multiline comment") {
		t.Errorf("expected comments to be stripped, got %q", got)
	}
	if !strings.Contains(got, "function formatBytes(bytes)") {
		t.Errorf("expected function declaration preserved, got %q", got)
	}
	if !strings.Contains(got, "hello /* not a comment */ world") {
		t.Errorf("expected string literals preserved, got %q", got)
	}
	if !strings.Contains(got, "return'0 B'") && !strings.Contains(got, "return '0 B'") {
		t.Errorf("expected return statement preserved, got %q", got)
	}
}

func TestFingerprint(t *testing.T) {
	data := []byte("body { background: #000; }")
	fpPath, hash := Fingerprint("css/styles.css", data)

	if len(hash) != 8 {
		t.Errorf("expected 8-char hash, got %q (len %d)", hash, len(hash))
	}
	if fpPath != "css/styles."+hash+".css" {
		t.Errorf("expected css/styles.%s.css, got %q", hash, fpPath)
	}
}

func TestIntegrity(t *testing.T) {
	data := []byte("body { background: #000; }")
	sri := Integrity(data)

	if !strings.HasPrefix(sri, "sha256-") {
		t.Errorf("expected sha256- prefix in SRI, got %q", sri)
	}
	if len(sri) <= 7 {
		t.Errorf("expected base64 encoded hash in SRI, got %q", sri)
	}
}

func TestPipeline(t *testing.T) {
	p := New()

	cssRaw := []byte("/* comments */\n.btn { color: red; }\n")
	fpPath, minified, err := p.Process("css/button.css", cssRaw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(string(minified), "comments") {
		t.Errorf("expected minified data without comments, got %s", minified)
	}

	resolved := p.AssetPath("css/button.css")
	if resolved != fpPath {
		t.Errorf("expected AssetPath to return %q, got %q", fpPath, resolved)
	}

	sri := p.AssetIntegrity("css/button.css")
	if !strings.HasPrefix(sri, "sha256-") {
		t.Errorf("expected valid SRI integrity string, got %q", sri)
	}

	// Non-existent asset falls back to empty string for integrity and input path for AssetPath
	if fallback := p.AssetPath("images/logo.png"); fallback != "images/logo.png" {
		t.Errorf("expected fallback 'images/logo.png', got %q", fallback)
	}
	if fallbackSri := p.AssetIntegrity("images/logo.png"); fallbackSri != "" {
		t.Errorf("expected empty string for non-existent asset integrity, got %q", fallbackSri)
	}
}
