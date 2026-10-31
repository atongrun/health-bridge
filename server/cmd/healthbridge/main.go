// Command healthbridge is the Health Bridge server CLI. S03a/S03b implement the
// owner surface: check-config, migrate, and serve (which now exposes the
// shortcuts-1 plan/observations/failure/status endpoints plus anonymous
// /healthz). Query endpoints arrive in S06.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/atongrun/health-bridge/server/internal/config"
	"github.com/atongrun/health-bridge/server/internal/httpapi"
	"github.com/atongrun/health-bridge/server/internal/oauth"
	"github.com/atongrun/health-bridge/server/internal/store"
)

const version = "0.1.0-s07a"

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
		// Query surface runs on its own read-only connection; writes on it must
		// fail at the SQLite level, not by convention.
		roDB, err := store.Open(cfg.DatabasePath, true)
		if err != nil {
			return err
		}
		defer roDB.Close()
		// OAuth stays off unless explicitly enabled; enabling requires the
		// 0600 secret file from config.
		var oauthHandler *oauth.Handler
		if cfg.OAuth.Enabled {
			secret, err := oauth.LoadSecret(cfg.OAuth.SecretFile)
			if err != nil {
				return fmt.Errorf("oauth secret: %w", err)
			}
			oauthStore := &oauth.Store{DB: db}
			oauthHandler = &oauth.Handler{
				Store: oauthStore,
				Provider: oauth.NewProvider(oauthStore, secret, cfg.PublicOrigin),
				DB:      db,
				Issuer:  cfg.PublicOrigin,
				Enabled: true,
			}
		}
		mux := http.NewServeMux()
		mux.Handle("/", httpapi.NewHandler(db))
		mux.Handle("/v1/query/", httpapi.NewQueryHandler(roDB, cursorSecret(cfg)))
		mux.Handle("/mcp", httpapi.NewMCPHandler(roDB, cursorSecret(cfg), oauthHandler))
		mux.Handle("/mcp/", httpapi.NewMCPHandler(roDB, cursorSecret(cfg), oauthHandler))
		if oauthHandler != nil {
			mux.HandleFunc("/oauth/authorize", oauthHandler.Authorize)
			mux.HandleFunc("/oauth/token", oauthHandler.Token)
			mux.HandleFunc("/oauth/revoke", oauthHandler.Revoke)
			mux.HandleFunc("/oauth/pending/", func(w http.ResponseWriter, r *http.Request) {
				oauthHandler.PendingStatus(w, r, strings.TrimPrefix(r.URL.Path, "/oauth/pending/"))
			})
			mux.HandleFunc("/oauth/continue", oauthHandler.Continue)
			mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
				oauthHandler.Discovery(w, r, "protected-resource")
			})
			mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
				oauthHandler.Discovery(w, r, "authorization-server")
			})
		}
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

// cursorSecret derives the query cursor HMAC key from the instance generation,
// rotated implicitly on restore (new generation). A dedicated secret file is
// S08 installer work.
func cursorSecret(cfg *config.Config) string {
	sum := sha256.Sum256([]byte("healthbridge-cursor:" + cfg.DatabasePath))
	return hex.EncodeToString(sum[:])
}
