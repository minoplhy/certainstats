// Command certainstats-collector runs on a host, gathers data from software
// there (each kind is a type, switched on with COLLECT_<TYPE>=true) and pushes it
// to CertainStats over one outbound WebSocket. It keeps no local state. The work
// is done by internal/collectoragent; this is only the entrypoint.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"certainstats/internal/collectoragent"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	check := flag.Bool("check", false, "test every prerequisite, report the first failure and exit")
	once := flag.Bool("once", false, "run one collection pass and exit")
	flag.Parse()
	// internal/logger writes through the standard logger, so this prefix applies to it too.
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("certainstats-collector: ")
	if *showVersion {
		fmt.Println("certainstats-collector", version)
		return
	}

	cfg, mods, err := collectoragent.LoadConfig(os.Getenv, version)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch {
	case *check:
		if err := collectoragent.Check(ctx, cfg, mods); err != nil {
			fmt.Fprintln(os.Stderr, "FAIL:", err)
			os.Exit(1)
		}
		fmt.Println("All checks passed.")
	case *once:
		if err := collectoragent.Once(ctx, cfg, mods); err != nil {
			log.Fatal(err)
		}
	default:
		collectoragent.Run(ctx, cfg, mods)
	}
}
