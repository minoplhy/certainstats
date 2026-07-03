package ws

import (
	"errors"
	"os"
	"strings"
)

// allowedOrigins is parsed once at startup from the ALLOWED_ORIGINS env var.
// An empty slice means all origins are permitted (dev mode).
var allowedOrigins []string

func init() {
	if raw := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			if trimmed := strings.TrimSpace(o); trimmed != "" {
				allowedOrigins = append(allowedOrigins, trimmed)
			}
		}
	}
}

// checkOrigin returns nil if the request origin is permitted, or an error otherwise.
func checkOrigin(origin string) error {
	if len(allowedOrigins) == 0 {
		return nil
	}
	for _, o := range allowedOrigins {
		if o == origin {
			return nil
		}
	}
	return errors.New("unauthorized origin")
}
