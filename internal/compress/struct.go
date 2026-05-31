package compress

import (
	"bytes"
	"io"
	"net/http"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	zstdEncoderPool sync.Pool
	gzipEncoderPool sync.Pool

	// Limit concurrent compression routines globally to prevent CPU exhaustion under DoS attacks
	activeCompressors = make(chan struct{}, 512) // Max 512 concurrent active compressions

	zstdEncoder *zstd.Encoder
)

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

type closerWrapper struct {
	io.Writer
	close func() error
}
