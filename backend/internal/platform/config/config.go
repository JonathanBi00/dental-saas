// Package config loads runtime configuration from environment variables.
// A .env file in the working directory or a parent directory is read first;
// variables already set in the environment take precedence.
package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL        string // runtime role (dental_app), subject to RLS
	MigrateDatabaseURL string // schema owner (dental_owner)
	HTTPAddr           string
	AppOrigin          string // browser origin of the web app, e.g. http://localhost:5173
	SMTPHost           string
	SMTPPort           int
	MailFrom           string
	LogLevel           slog.Level
}

func Load() (Config, error) {
	loadDotEnv()

	c := Config{
		DatabaseURL:        env("DATABASE_URL", "postgres://dental_app:dental_app@localhost:5433/dental?sslmode=disable"),
		MigrateDatabaseURL: env("MIGRATE_DATABASE_URL", "postgres://dental_owner:dental_owner@localhost:5433/dental?sslmode=disable"),
		HTTPAddr:           env("HTTP_ADDR", ":8080"),
		AppOrigin:          strings.TrimRight(env("APP_ORIGIN", "http://localhost:5173"), "/"),
		SMTPHost:           env("SMTP_HOST", "localhost"),
		MailFrom:           env("MAIL_FROM", "Dental SaaS <no-reply@dental.local>"),
	}

	port, err := strconv.Atoi(env("SMTP_PORT", "1025"))
	if err != nil {
		return Config{}, fmt.Errorf("SMTP_PORT: %w", err)
	}
	c.SMTPPort = port

	if err := c.LogLevel.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

// loadDotEnv reads KEY=VALUE lines from the nearest .env (cwd, then up to two parents).
func loadDotEnv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for range 3 {
		f, err := os.Open(filepath.Join(dir, ".env"))
		if err == nil {
			defer f.Close()
			s := bufio.NewScanner(f)
			for s.Scan() {
				line := strings.TrimSpace(s.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				k, v, ok := strings.Cut(line, "=")
				if !ok {
					continue
				}
				k = strings.TrimSpace(k)
				v = strings.Trim(strings.TrimSpace(v), `"'`)
				if _, set := os.LookupEnv(k); !set {
					os.Setenv(k, v)
				}
			}
			return
		}
		dir = filepath.Dir(dir)
	}
}
