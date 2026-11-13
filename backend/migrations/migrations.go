// Package migrations embeds the SQL migrations and runs them with goose.
// Migrations must run as the schema owner (dental_owner), never as the app role.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var FS embed.FS

// Run executes a goose command ("up", "down", "status", "reset", ...) against ownerURL.
func Run(ctx context.Context, ownerURL, command string) error {
	db, err := sql.Open("pgx", ownerURL)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, FS)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	switch command {
	case "up":
		_, err = provider.Up(ctx)
	case "down":
		_, err = provider.Down(ctx)
	case "reset":
		_, err = provider.DownTo(ctx, 0)
	case "status":
		var results []*goose.MigrationStatus
		results, err = provider.Status(ctx)
		for _, r := range results {
			applied := "pending"
			if r.State == goose.StateApplied {
				applied = "applied " + r.AppliedAt.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-30s %s\n", r.Source.Path, applied)
		}
	default:
		return fmt.Errorf("unknown migrate command %q (use up, down, reset, status)", command)
	}
	return err
}
