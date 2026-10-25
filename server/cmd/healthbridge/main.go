// Command healthbridge is the Health Bridge server CLI. S03a/S03b implement the
// owner surface: check-config, migrate, and serve (which now exposes the
// shortcuts-1 plan/observations/failure/status endpoints plus anonymous
// /healthz). Query endpoints arrive in S06.
package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/atongrun/health-bridge/server/internal/config"
	"github.com/atongrun/health-bridge/server/internal/httpapi"
	"github.com/atongrun/health-bridge/server/internal/store"
)

const version = "0.1.0-s03b"

const usage = `usage: healthbridge <command> [-config PATH]

commands:
  check-config   load and validate the config JSON
  migrate        apply versioned schema migrations (idempotent)
  serve          run the shortcuts-1 upload server
  version        print the build version`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "healthbridge:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	configPath := fs.String("config", "/etc/healthbridge/config.json", "config JSON path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "check-config":
		if _, err := config.Load(*configPath); err != nil {
			return err
		}
		fmt.Println("config ok")
		return nil
	case "migrate":
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		db, err := store.Open(cfg.DatabasePath, false)
		if err != nil {
			return err
		}
		defer db.Close()
		v, err := store.Migrate(db)
		if err != nil {
			return err
		}
		fmt.Printf("schema version %d applied\n", v)
		return nil
	case "serve":
		cfg, err := config.Load(*configPath)
		if err != nil {
			return err
		}
		db, err := store.Open(cfg.DatabasePath, false)
		if err != nil {
			return err
		}
		defer db.Close()
		if err := store.RequireCurrentSchema(db); err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/", httpapi.NewHandler(db))
		ln, err := net.Listen("tcp", cfg.ListenAddress)
		if err != nil {
			return err
		}
		fmt.Printf("serving on %s\n", cfg.ListenAddress)
		return http.Serve(ln, mux)
	default:
		return fmt.Errorf("unknown command %q\n%s", args[0], usage)
	}
}
