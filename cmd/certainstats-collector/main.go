// Command certainstats-collector runs on a host, gathers data from software
// there (each kind is a type, switched on with COLLECT_<TYPE>=true) and pushes it
// to CertainStats over one outbound WebSocket. It keeps no local state.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"certainstats/cmd/certainstats-collector/crowdsec"
	"certainstats/cmd/certainstats-collector/module"
)

var version = "dev"

type config struct {
	HubURL string
	Token  string
}

// available lists every type this binary can collect.
func available(getenv func(string) string) []module.Module {
	return []module.Module{crowdsec.New(getenv, version)}
}

func enabledModules(getenv func(string) string) []module.Module {
	var on []module.Module
	for _, m := range available(getenv) {
		if strings.EqualFold(strings.TrimSpace(getenv("COLLECT_"+strings.ToUpper(m.Name()))), "true") {
			on = append(on, m)
		}
	}
	return on
}

func loadConfig(getenv func(string) string) (config, []module.Module, error) {
	cfg := config{HubURL: strings.TrimSpace(getenv("HUB_URL")), Token: strings.TrimSpace(getenv("TOKEN"))}
	if cfg.HubURL == "" {
		return cfg, nil, errors.New("HUB_URL is not set; set it to the CertainStats panel URL")
	}
	if _, _, err := connectURL(cfg.HubURL); err != nil {
		return cfg, nil, err
	}
	if cfg.Token == "" {
		return cfg, nil, errors.New("TOKEN is not set; copy it from the collector in CertainStats")
	}
	mods := enabledModules(getenv)
	if len(mods) == 0 {
		var names []string
		for _, m := range available(getenv) {
			names = append(names, "COLLECT_"+strings.ToUpper(m.Name())+"=true")
		}
		return cfg, nil, fmt.Errorf("no type is switched on; set %s", strings.Join(names, " or "))
	}
	return cfg, mods, nil
}

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	check := flag.Bool("check", false, "test every prerequisite, report the first failure and exit")
	once := flag.Bool("once", false, "run one collection pass and exit")
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("certainstats-collector: ")
	if *showVersion {
		fmt.Println("certainstats-collector", version)
		return
	}

	cfg, mods, err := loadConfig(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *check {
		if err := runCheck(ctx, cfg, mods); err != nil {
			fmt.Fprintln(os.Stderr, "FAIL:", err)
			os.Exit(1)
		}
		fmt.Println("All checks passed.")
		return
	}
	if *once {
		if _, err := session(ctx, cfg, mods, true); err != nil {
			log.Fatal(err)
		}
		return
	}
	run(ctx, cfg, mods)
}

// run keeps a connection open, reconnecting with jittered backoff up to five minutes.
func run(ctx context.Context, cfg config, mods []module.Module) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		started := time.Now()
		_, err := session(ctx, cfg, mods, false)
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

func runCheck(ctx context.Context, cfg config, mods []module.Module) error {
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
