package registry

import (
	"testing"
)

func TestRegistry_SupportedParsers(t *testing.T) {
	r := NewRegistry()

	supported := []string{"beszel", "hetrixtools", "ltstats"}
	for _, s := range supported {
		if !r.IsSupported(s) {
			t.Errorf("expected agent type %s to be supported", s)
		}
	}

	if r.IsSupported("non_existent_parser") {
		t.Errorf("expected non_existent_parser to be unsupported")
	}
}

func TestRegistry_ParsePayloadInvalid(t *testing.T) {
	r := NewRegistry()

	_, err := r.ParsePayload("invalid_type", []byte("dummy"))
	if err == nil {
		t.Errorf("expected error for invalid_type")
	}

	_, err = r.ParseToken("invalid_type", []byte("dummy"))
	if err == nil {
		t.Errorf("expected error for invalid_type ParseToken")
	}
}

func TestRegistry_DetectInvalid(t *testing.T) {
	r := NewRegistry()

	_, _, err := r.Detect([]byte{})
	if err == nil {
		t.Errorf("expected detect failure on empty data")
	}
}
