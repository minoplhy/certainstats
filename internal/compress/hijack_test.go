package compress

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompressionMiddlewareHijack(t *testing.T) {
	srv := httptest.NewServer(CompressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// x/net/websocket asserts http.Hijacker directly
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 3\r\nConnection: close\r\n\r\nraw")
		_ = rw.Flush()
	})))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "raw" || resp.Header.Get("Content-Encoding") != "" {
		t.Fatalf("got %q encoding=%q", body, resp.Header.Get("Content-Encoding"))
	}
}

func TestCompressionWriterHijackAfterWrite(t *testing.T) {
	cw := &compressionResponseWriter{ResponseWriter: httptest.NewRecorder(), ae: "gzip", buf: new(bytes.Buffer)}
	_, _ = cw.Write([]byte("partial"))
	if _, _, err := cw.Hijack(); err == nil {
		t.Fatal("hijack allowed after write")
	}
}

func TestCompressionWriterFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &compressionResponseWriter{ResponseWriter: rec, ae: "gzip", buf: new(bytes.Buffer)}
	_, _ = cw.Write(bytes.Repeat([]byte("A"), 1000))
	if err := http.NewResponseController(cw).Flush(); err != nil {
		t.Fatal(err)
	}
	if !rec.Flushed || rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() == 0 {
		t.Fatalf("flushed=%v encoding=%q len=%d", rec.Flushed, rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	_ = cw.Close()
}

func TestCompressionWriterFlushBeforeWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	cw := &compressionResponseWriter{ResponseWriter: rec, ae: "gzip", buf: new(bytes.Buffer)}
	cw.Flush()
	_, _ = cw.Write([]byte("hi"))
	_, _ = cw.Write([]byte(" there"))
	_ = cw.Close()
	if rec.Body.String() != "hi there" || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("body=%q encoding=%q", rec.Body.String(), rec.Header().Get("Content-Encoding"))
	}
}
