// Package module is what a collector type implements inside certainstats-collector.
package module

import (
	"context"
	"strings"

	"certainstats/internal/collector/wire"
)

// Send writes one frame to CertainStats. Safe for concurrent use.
type Send func(msgType string, body any) error

// Module gathers one type of data. It is switched on with COLLECT_<NAME>=true,
// or with the variable its Switch names.
type Module interface {
	// Name is the type ID shared with the server, e.g. "crowdsec".
	Name() string
	// Prefix starts every message type the module owns, e.g. "cs_".
	Prefix() string
	// Check tests the module's local prerequisites and returns the first failure.
	Check(ctx context.Context) error
	// Run serves one connection. inbox carries frames with the module's prefix.
	// With once set it does a single pass and returns. It must return when ctx ends.
	Run(ctx context.Context, send Send, inbox <-chan wire.Envelope, once bool) error
}

// Switcher is implemented by modules switched on by a variable other than COLLECT_<NAME>.
type Switcher interface {
	Switch() string
}

// SwitchOf returns the environment variable that switches m on.
func SwitchOf(m Module) string {
	if s, ok := m.(Switcher); ok {
		return s.Switch()
	}
	return "COLLECT_" + strings.ToUpper(m.Name())
}
