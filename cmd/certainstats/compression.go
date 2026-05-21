package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

var (
	zstdEncoderPool sync.Pool
	gzipEncoderPool sync.Pool

	// Limit concurrent compression routines globally to prevent CPU exhaustion under DoS attacks
	activeCompressors = make(chan struct{}, 512) // Max 512 concurrent active compressions
)

func init() {
	// Initialize ZStandard encoder pool
	zstdEncoderPool = sync.Pool{
		New: func() interface{} {
			zw, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
			return zw
		},
	}

	// Initialize GZIP encoder pool
	gzipEncoderPool = sync.Pool{
		New: func() interface{} {
			gw, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
			return gw
		},
	}
}

// compressionResponseWriter lazily decides whether to compress based on response size
type compressionResponseWriter struct {
	http.ResponseWriter
	ae          string
	buf         *bytes.Buffer
	writer      io.WriteCloser
	wroteHeader bool
	status      int
	bypassed    bool
	release     func()
}

func (w *compressionResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *compressionResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status

	// If Content-Encoding is already set, bypass compression completely and write header immediately
	if w.Header().Get("Content-Encoding") != "" {
		w.bypassed = true
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *compressionResponseWriter) Write(b []byte) (int, error) {
	// If compression has already started, stream directly to the compressor
	if w.writer != nil {
		return w.writer.Write(b)
	}

	// If compression was bypassed (due to CPU load shedding or already compressed content), stream directly to the uncompressed socket
	if w.bypassed {
		return w.ResponseWriter.Write(b)
	}

	// If Content-Encoding is already set, bypass compression completely and stream directly
	if w.Header().Get("Content-Encoding") != "" {
		w.bypassed = true
		w.wroteHeader = true
		if w.status != 0 {
			w.ResponseWriter.WriteHeader(w.status)
		}
		if w.buf.Len() > 0 {
			if _, err := w.ResponseWriter.Write(w.buf.Bytes()); err != nil {
				return 0, err
			}
			w.buf.Reset()
		}
		return w.ResponseWriter.Write(b)
	}

	// Buffer the output up to 1400 bytes (standard MTU packet size)
	n, err := w.buf.Write(b)
	if err != nil {
		return n, err
	}

	// If the buffered size exceeds our threshold, initialize dynamic compression
	if w.buf.Len() > 1400 {
		w.startCompression()
	}

	return n, nil
}

func (w *compressionResponseWriter) startCompression() {
	w.wroteHeader = true

	// Check if we can acquire a compression slot to prevent CPU saturation under attack
	select {
	case activeCompressors <- struct{}{}:
		// Slot acquired! Proceed with compression.
		w.release = func() { <-activeCompressors }
	default:
		// Under extreme CPU load / DoS attack: bypass compression and stream uncompressed
		w.bypassed = true
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(w.buf.Bytes()))
		}
		if w.status != 0 {
			w.ResponseWriter.WriteHeader(w.status)
		} else {
			w.ResponseWriter.WriteHeader(http.StatusOK)
		}
		_, _ = w.ResponseWriter.Write(w.buf.Bytes())
		w.buf.Reset()
		return
	}

	// Dynamically detect Content-Type if not set by the handler
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", http.DetectContentType(w.buf.Bytes()))
	}

	// Negotiate compression
	ae := w.ae
	if strings.Contains(ae, "zstd") {
		w.Header().Set("Content-Encoding", "zstd")
		w.Header().Add("Vary", "Accept-Encoding")

		zw := zstdEncoderPool.Get().(*zstd.Encoder)
		zw.Reset(w.ResponseWriter)

		w.writer = &closerWrapper{
			Writer: zw,
			close: func() error {
				err := zw.Close()
				zstdEncoderPool.Put(zw)
				return err
			},
		}
	} else if strings.Contains(ae, "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")

		gw := gzipEncoderPool.Get().(*gzip.Writer)
		gw.Reset(w.ResponseWriter)

		w.writer = &closerWrapper{
			Writer: gw,
			close: func() error {
				err := gw.Close()
				gzipEncoderPool.Put(gw)
				return err
			},
		}
	}

	// Write HTTP Status Code
	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	} else {
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}

	// Flush already buffered bytes into the compressor
	if w.writer != nil {
		_, _ = w.writer.Write(w.buf.Bytes())
		w.buf.Reset()
	}
}

func (w *compressionResponseWriter) Close() error {
	w.wroteHeader = true
	defer func() {
		if w.release != nil {
			w.release()
		}
	}()

	// If compression was initialized, close and recycle the compressor
	if w.writer != nil {
		return w.writer.Close()
	}

	// Otherwise, serve uncompressed (either response size < 1400 bytes, or bypassed due to load shedding)
	if w.bypassed {
		return nil
	}

	// Check one last time if Content-Encoding was set before writing remaining buffered data
	if w.Header().Get("Content-Encoding") != "" {
		w.bypassed = true
		if w.status != 0 {
			w.ResponseWriter.WriteHeader(w.status)
		}
		_, err := w.ResponseWriter.Write(w.buf.Bytes())
		return err
	}

	if w.Header().Get("Content-Type") == "" && w.buf.Len() > 0 {
		w.Header().Set("Content-Type", http.DetectContentType(w.buf.Bytes()))
	}

	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}

	_, err := w.ResponseWriter.Write(w.buf.Bytes())
	return err
}

type closerWrapper struct {
	io.Writer
	close func() error
}

func (cw *closerWrapper) Close() error {
	return cw.close()
}

// CompressionMiddleware negotiates dynamic response compression (Zstd or Gzip)
func CompressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. WebSocket / Upgrade safety: Skip standard compression to avoid breaking hijacked TCP streams
		if strings.ToLower(r.Header.Get("Upgrade")) == "websocket" ||
			strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
			next.ServeHTTP(w, r)
			return
		}

		ae := r.Header.Get("Accept-Encoding")
		if !strings.Contains(ae, "zstd") && !strings.Contains(ae, "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// 2. Wrap writer with lazy, threshold-aware compression response writer
		cw := &compressionResponseWriter{
			ResponseWriter: w,
			ae:             ae,
			buf:            bytes.NewBuffer(make([]byte, 0, 1400)),
		}
		defer cw.Close()

		next.ServeHTTP(cw, r)
	})
}
