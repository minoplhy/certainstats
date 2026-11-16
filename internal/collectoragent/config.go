package collectoragent

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"certainstats/internal/collectoragent/module"
	"certainstats/internal/collectoragent/type/crowdsec"
	"certainstats/internal/collectoragent/type/knot"
	log "certainstats/internal/logger"
)

// Config is the collector's own settings, read from its environment.
type Config struct {
	HubURL  string
	Token   string
	Version string
}

// Available lists every type this binary can collect.
func Available(getenv func(string) string, version string) []module.Module {
	return []module.Module{crowdsec.New(getenv, version), knot.New(getenv, version)}
}

func enabledModules(getenv func(string) string, version string) []module.Module {
	var on []module.Module
	for _, m := range Available(getenv, version) {
		if strings.EqualFold(strings.TrimSpace(getenv(module.SwitchOf(m))), "true") {
			on = append(on, m)
		}
	}
	return on
}

// LoadConfig reads HUB_URL, TOKEN and each type's switch, COLLECT_<TYPE>.
func LoadConfig(getenv func(string) string, version string) (Config, []module.Module, error) {
	cfg := Config{HubURL: strings.TrimSpace(getenv("HUB_URL")), Token: strings.TrimSpace(getenv("TOKEN")), Version: version}
	if cfg.HubURL == "" {
		return cfg, nil, errors.New("HUB_URL is not set; set it to the CertainStats panel URL")
	}
	if _, _, err := connectURL(cfg.HubURL); err != nil {
		return cfg, nil, err
	}
	if cfg.Token == "" {
		return cfg, nil, errors.New("TOKEN is not set; copy it from the collector in CertainStats")
	}
	mods := enabledModules(getenv, version)
	if len(mods) == 0 {
		var names []string
		for _, m := range Available(getenv, version) {
			names = append(names, module.SwitchOf(m)+"=true")
		}
		return cfg, nil, fmt.Errorf("no type is switched on; set %s", strings.Join(names, " or "))
	}
	return cfg, mods, nil
}

// Run keeps a connection open, reconnecting with jittered backoff up to five minutes.
func Run(ctx context.Context, cfg Config, mods []module.Module) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		_, err := Session(ctx, cfg, mods, false)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > time.Minute {
			backoff = 2 * time.Second
		}
		wait := backoff/2 + rand.N(backoff/2+1)
		log.Printf("%v; reconnecting in %s", err, wait.Round(time.Second))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

// Once runs a single collection pass.
func Once(ctx context.Context, cfg Config, mods []module.Module) error {
	_, err := Session(ctx, cfg, mods, true)
	return err
}

// Check tests every prerequisite and returns the first failure.
func Check(ctx context.Context, cfg Config, mods []module.Module) error {
	fmt.Println("ok   configuration: HUB_URL and TOKEN are set")
	l, err := dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer l.close()
	h, err := l.hello()
	if err != nil {
		return err
	}
	fmt.Printf("ok   CertainStats: connected as %q\n", h.Name)
	enabled := map[string]bool{}
	for _, t := range h.Types {
		enabled[t] = true
	}
	for _, m := range mods {
		if !enabled[m.Name()] {
			return fmt.Errorf("%s is switched on here but not enabled for this collector in CertainStats", m.Name())
		}
		fmt.Printf("ok   %s: enabled in CertainStats\n", m.Name())
	}
	for _, m := range mods {
		if err := m.Check(ctx); err != nil {
			return fmt.Errorf("%s: %w", m.Name(), err)
		}
		fmt.Printf("ok   %s: local prerequisites\n", m.Name())
	}
	return nil
}
