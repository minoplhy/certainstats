package compress

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompressionMiddleware(t *testing.T) {
	// A helper string that is larger than the 256 bytes threshold to trigger dynamic compression
	largePayload := make([]byte, 1000)
	for i := range largePayload {
		largePayload[i] = 'A'
	}

	tests := []struct {
		name                 string
		acceptEncoding       string
		handlerSetEncoding   string
		payload              []byte
		expectContentEncoding string
		expectDynamicCompress bool
	}{
		{
			name:                  "Compress large response when Accept-Encoding includes zstd",
			acceptEncoding:        "zstd, gzip",
			handlerSetEncoding:    "",
			payload:               largePayload,
			expectContentEncoding: "zstd",
			expectDynamicCompress: true,
		},
		{
			name:                  "Compress large response when Accept-Encoding includes gzip only",
			acceptEncoding:        "gzip",
			handlerSetEncoding:    "",
			payload:               largePayload,
			expectContentEncoding: "gzip",
			expectDynamicCompress: true,
		},
		{
			name:                  "Bypass compression if Content-Encoding is already set by handler (large payload)",
			acceptEncoding:        "zstd, gzip",
			handlerSetEncoding:    "gzip",
			payload:               largePayload,
			expectContentEncoding: "gzip",
			expectDynamicCompress: false,
		},
		{
			name:                  "Bypass compression if Content-Encoding is already set by handler (small payload)",
			acceptEncoding:        "zstd, gzip",
			handlerSetEncoding:    "gzip",
			payload:               []byte("small payload"),
			expectContentEncoding: "gzip",
			expectDynamicCompress: false,
		},
		{
			name:                  "Do not compress if Accept-Encoding is unsupported",
			acceptEncoding:        "deflate",
			handlerSetEncoding:    "",
			payload:               largePayload,
			expectContentEncoding: "",
			expectDynamicCompress: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.handlerSetEncoding != "" {
					w.Header().Set("Content-Encoding", tc.handlerSetEncoding)
				}
				w.Write(tc.payload)
			})

			req := httptest.NewRequest("GET", "http://localhost/", nil)
			if tc.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tc.acceptEncoding)
			}

			rec := httptest.NewRecorder()
			middleware := CompressionMiddleware(handler)
			middleware.ServeHTTP(rec, req)

			respHeader := rec.Header().Get("Content-Encoding")
			if respHeader != tc.expectContentEncoding {
				t.Errorf("Expected Content-Encoding %q, got %q", tc.expectContentEncoding, respHeader)
			}

			if !tc.expectDynamicCompress {
				// If dynamic compression is bypassed, the response body should be EXACTLY the payload
				if !bytes.Equal(rec.Body.Bytes(), tc.payload) {
					t.Errorf("Expected exact payload since compression should be bypassed, but body was modified")
				}
			} else {
				// If dynamic compression occurred, the body should be compressed and thus different (and smaller)
				if bytes.Equal(rec.Body.Bytes(), tc.payload) {
					t.Errorf("Expected body to be compressed, but it matched the uncompressed payload")
				}
			}
		})
	}
}
