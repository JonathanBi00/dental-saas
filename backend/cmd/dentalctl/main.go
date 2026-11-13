// Command dentalctl runs the API server and operator tasks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dentalsaas/internal/access"
	"dentalsaas/internal/api"
	"dentalsaas/internal/auditlog"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/config"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/mail"
	"dentalsaas/internal/seed"
	"dentalsaas/internal/tenancy"
	"dentalsaas/migrations"
)

const usage = `dentalctl - Dental SaaS server and operator tool

Usage:
  dentalctl serve                                   Run the HTTP API
  dentalctl migrate [up|down|reset|status]          Run database migrations (default: up)
  dentalctl seed                                    Create demo data (idempotent)
  dentalctl org create --name NAME --owner-email EMAIL [--abn ABN]
                                                    Create an organisation and invite its owner

Configuration comes from environment variables (see .env.example).
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(usage)
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "serve":
		return serve(ctx, cfg)
	case "migrate":
		cmd := "up"
		if len(args) > 1 {
			cmd = args[1]
		}
		return migrations.Run(ctx, cfg.MigrateDatabaseURL, cmd)
	case "seed":
		pool, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		return seed.Run(ctx, pool, os.Stdout)
	case "org":
		if len(args) < 2 || args[1] != "create" {
			return errors.New("usage: dentalctl org create --name NAME --owner-email EMAIL [--abn ABN]")
		}
		return orgCreate(ctx, cfg, args[2:])
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(ctx context.Context, cfg config.Config) error {
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	sender := mail.SMTP{Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.MailFrom}
	handler := api.NewHandler(api.Deps{
		DB:             pool,
		Identity:       identity.New(pool, sender, cfg.AppOrigin),
		Tenancy:        tenancy.New(pool),
		Members:        &access.MemberService{DB: pool},
		Audit:          &auditlog.Service{DB: pool},
		AllowedOrigins: allowedOrigins(cfg),
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("api listening", "addr", cfg.HTTPAddr, "app_origin", cfg.AppOrigin)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// allowedOrigins: the web app, plus the API's own origin for direct calls (e.g. API docs tools).
func allowedOrigins(cfg config.Config) []string {
	out := []string{cfg.AppOrigin}
	if _, port, err := net.SplitHostPort(cfg.HTTPAddr); err == nil {
		out = append(out, "http://localhost:"+port, "http://127.0.0.1:"+port)
	}
	return out
}

func orgCreate(ctx context.Context, cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("org create", flag.ContinueOnError)
	name := fs.String("name", "", "organisation name (required)")
	owner := fs.String("owner-email", "", "email of the first owner (required)")
	abn := fs.String("abn", "", "Australian Business Number")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*name) == "" || strings.TrimSpace(*owner) == "" {
		fs.Usage()
		return errors.New("--name and --owner-email are required")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	org, err := tenancy.New(pool).CreateOrganisation(ctx, *name, *abn)
	if err != nil {
		return err
	}
	ids := identity.New(pool, mail.SMTP{Host: cfg.SMTPHost, Port: cfg.SMTPPort, From: cfg.MailFrom}, cfg.AppOrigin)
	_, token, err := ids.CreateOwnerInvitation(ctx, org.ID, *owner)
	if err != nil {
		return fmt.Errorf("organisation %s created, but inviting the owner failed: %w", org.ID, err)
	}
	fmt.Printf("Created organisation %q (%s)\n", org.Name, org.ID)
	fmt.Printf("Owner invitation emailed to %s\n", *owner)
	fmt.Printf("Invitation link: %s\n", ids.InvitationLink(token))
	return nil
}
