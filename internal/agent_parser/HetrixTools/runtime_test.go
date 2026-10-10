package hetrixtools

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"net/url"
	"testing"
)

func TestOpaqueSoftwareVersion(t *testing.T) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(`{"SID":"token","version":"build-2026.10","agent":"hetrixtools","time":"1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw := []byte(url.Values{"j": {base64.StdEncoding.EncodeToString(b.Bytes())}}.Encode())
	p, e := (&HTStats{}).Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	if p.Runtime == nil || p.Runtime.ProtocolVersion != nil || p.Runtime.AgentVersion == nil || *p.Runtime.AgentVersion != "build-2026.10" {
		t.Fatal(p.Runtime)
	}
}
