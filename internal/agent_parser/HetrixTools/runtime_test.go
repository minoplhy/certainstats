package hetrixtools

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
)

func TestOpaqueSoftwareVersion(t *testing.T) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte(`{"SID":"token","version":"build-2026.10","agent":"hetrixtools","time":"1"}`))
	w.Close()
	raw := []byte("j=" + base64.StdEncoding.EncodeToString(b.Bytes()))
	p, e := (&HTStats{}).Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	if p.Runtime == nil || p.Runtime.ProtocolVersion != nil || p.Runtime.AgentVersion == nil || *p.Runtime.AgentVersion != "build-2026.10" {
		t.Fatal(p.Runtime)
	}
}
