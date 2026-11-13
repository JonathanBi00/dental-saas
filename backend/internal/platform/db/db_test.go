package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"dentalsaas/internal/access"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/platform/testdb"
	"dentalsaas/internal/store"
)

var ctx = context.Background()

func TestMain(m *testing.M) { testdb.Main(m) }

func TestSmoke(t *testing.T) {
	env := testdb.Start(t)
	if err := env.Pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

// Task 2.1: tenant settings are transaction-local and never leak to the next user of a connection.
func TestTenantContextDoesNotLeakBetweenTransactions(t *testing.T) {
	env := testdb.Start(t)
	pool, err := db.Open(ctx, env.AppURL+"&pool_max_conns=1") // force connection reuse
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	org := uuid.New()
	err = pool.InTenantTx(ctx, db.Tenant{OrgID: org, UserID: uuid.New()}, func(tx *db.Tx) error {
		var got string
		if err := tx.PG.QueryRow(ctx, "SELECT current_setting('app.org_id', true)").Scan(&got); err != nil {
			return err
		}
		if got != org.String() {
			t.Errorf("inside tx: app.org_id = %q, want %q", got, org)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = pool.InAuthTx(ctx, func(tx *db.Tx) error {
		var orgSetting, userSetting string
		if err := tx.PG.QueryRow(ctx,
			"SELECT coalesce(current_setting('app.org_id', true), ''), coalesce(current_setting('app.user_id', true), '')",
		).Scan(&orgSetting, &userSetting); err != nil {
			return err
		}
		if orgSetting != "" || userSetting != "" {
			t.Errorf("next tx on same connection saw org=%q user=%q; want both empty", orgSetting, userSetting)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// tenancy spec: "Query without tenant context" fails closed.
func TestNoTenantContextFailsClosed(t *testing.T) {
	env := testdb.Start(t)
	org := testdb.CreateOrg(t, env.Pool, "Closed Dental")
	testdb.CreateLocation(t, env.Pool, org, "Somewhere")

	err := env.Pool.InAuthTx(ctx, func(tx *db.Tx) error {
		var n int
		if err := tx.PG.QueryRow(ctx, "SELECT count(*) FROM locations").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("without tenant context saw %d locations, want 0", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = env.Pool.InAuthTx(ctx, func(tx *db.Tx) error {
		_, err := tx.CreateLocation(ctx, store.CreateLocationParams{ID: ids.New(), OrgID: org, Name: "Sneaky", Timezone: "UTC"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Errorf("insert without tenant context: err = %v, want RLS violation", err)
	}
}

// Task 2.4: every table with an org_id column must have FORCE RLS and at least one policy.
const guardQuery = `
SELECT c.relname
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'org_id' AND NOT a.attisdropped
WHERE n.nspname = $1 AND c.relkind IN ('r', 'p')
  AND (NOT c.relrowsecurity OR NOT c.relforcerowsecurity
       OR NOT EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid))
ORDER BY 1`

func unprotectedTables(t *testing.T, conn *pgx.Conn, schema string) []string {
	t.Helper()
	rows, err := conn.Query(ctx, guardQuery, schema)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestRLSGuard(t *testing.T) {
	env := testdb.Start(t)
	conn, err := pgx.Connect(ctx, env.OwnerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	if bad := unprotectedTables(t, conn, "public"); len(bad) > 0 {
		t.Fatalf("tenant tables without FORCE ROW LEVEL SECURITY and a policy: %v", bad)
	}

	// The guard must catch an unprotected table.
	_, err = conn.Exec(ctx, `
		DROP SCHEMA IF EXISTS guard_check CASCADE;
		CREATE SCHEMA guard_check;
		CREATE TABLE guard_check.leaky (id uuid PRIMARY KEY, org_id uuid NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP SCHEMA guard_check CASCADE")
	if bad := unprotectedTables(t, conn, "guard_check"); strings.Join(bad, ",") != "leaky" {
		t.Fatalf("guard did not flag an unprotected table; got %v", bad)
	}
}

// Tasks 3.1 / 4.1 / 6.1: another organisation's rows are invisible and immutable.
func TestCrossTenantIsolation(t *testing.T) {
	env := testdb.Start(t)
	orgA := testdb.CreateOrg(t, env.Pool, "Org A")
	orgB := testdb.CreateOrg(t, env.Pool, "Org B")
	locA := testdb.CreateLocation(t, env.Pool, orgA, "A clinic")
	memberA := testdb.CreateMember(t, env.Pool, orgA, access.RolePractitioner, locA)
	memberB := testdb.CreateMember(t, env.Pool, orgB, access.RoleOwner)

	// Invitation and audit event in org A.
	err := env.Pool.InTenantTx(ctx, db.Tenant{OrgID: orgA}, func(tx *db.Tx) error {
		_, hash := ids.NewToken()
		if _, err := tx.CreateInvitation(ctx, store.CreateInvitationParams{
			ID: ids.New(), OrgID: orgA, Email: testdb.Email(t, "inv"), Role: "front_desk",
			LocationIds: []uuid.UUID{}, TokenHash: hash, ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			return err
		}
		return tx.InsertAuditEvent(ctx, store.InsertAuditEventParams{ID: ids.New(), OrgID: &orgA, Action: "test.event", Details: []byte("{}")})
	})
	if err != nil {
		t.Fatal(err)
	}

	asB := db.Tenant{OrgID: orgB, UserID: memberB.UserID}
	for _, table := range []string{"locations", "memberships", "membership_locations", "invitations", "audit_events"} {
		t.Run(table, func(t *testing.T) {
			err := env.Pool.InTenantTx(ctx, asB, func(tx *db.Tx) error {
				var n int
				if err := tx.PG.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE org_id = $1", orgA).Scan(&n); err != nil {
					return err
				}
				if n != 0 {
					t.Errorf("org B can see %d of org A's %s rows", n, table)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			// Updates by org B touch nothing (or are refused outright for append-only tables).
			err = env.Pool.InTenantTx(ctx, asB, func(tx *db.Tx) error {
				tag, err := tx.PG.Exec(ctx, "UPDATE "+table+" SET org_id = org_id WHERE org_id = $1", orgA)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 0 {
					t.Errorf("org B updated %d of org A's %s rows", tag.RowsAffected(), table)
				}
				return nil
			})
			if err != nil && table != "audit_events" {
				t.Fatal(err)
			}
		})
	}

	t.Run("insert into another org", func(t *testing.T) {
		err := env.Pool.InTenantTx(ctx, asB, func(tx *db.Tx) error {
			_, err := tx.CreateLocation(ctx, store.CreateLocationParams{ID: ids.New(), OrgID: orgA, Name: "Planted", Timezone: "UTC"})
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "row-level security") {
			t.Fatalf("org B inserting an org A location: err = %v, want RLS violation", err)
		}
	})

	t.Run("users are not visible across tenants", func(t *testing.T) {
		err := env.Pool.InTenantTx(ctx, asB, func(tx *db.Tx) error {
			_, err := tx.GetUser(ctx, memberA.UserID)
			if !db.IsNoRows(err) {
				t.Errorf("org B reading an org A user: err = %v, want no rows", err)
			}
			// ...except through the narrow SECURITY DEFINER lookup used for sign-in.
			var id uuid.UUID
			if err := tx.PG.QueryRow(ctx, "SELECT id FROM auth_find_user_by_email($1)", memberA.Email).Scan(&id); err != nil {
				return err
			}
			if id != memberA.UserID {
				t.Errorf("auth lookup returned %s, want %s", id, memberA.UserID)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

// Task 6.1: the app role cannot change or delete audit entries.
func TestAuditLogIsAppendOnly(t *testing.T) {
	env := testdb.Start(t)
	org := testdb.CreateOrg(t, env.Pool, "Audit Dental")
	id := ids.New()
	tenant := db.Tenant{OrgID: org}
	err := env.Pool.InTenantTx(ctx, tenant, func(tx *db.Tx) error {
		return tx.InsertAuditEvent(ctx, store.InsertAuditEventParams{ID: id, OrgID: &org, Action: "test.event", Details: []byte("{}")})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"UPDATE audit_events SET action = 'tampered' WHERE id = $1",
		"DELETE FROM audit_events WHERE id = $1",
	} {
		err := env.Pool.InTenantTx(ctx, tenant, func(tx *db.Tx) error {
			_, err := tx.PG.Exec(ctx, stmt, id)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%q: err = %v, want permission denied", stmt, err)
		}
	}

	// The trigger blocks even roles that bypass RLS (here, the superuser).
	conn, err := pgx.Connect(ctx, strings.Replace(env.OwnerURL, "dental_owner:dental_owner", "postgres:postgres", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "UPDATE audit_events SET action = 'tampered' WHERE id = $1", id); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Errorf("superuser update: err = %v, want append-only trigger error", err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM audit_events WHERE id = $1", id); err == nil ||
		!strings.Contains(err.Error(), "append-only") {
		t.Errorf("superuser delete: err = %v, want append-only trigger error", err)
	}
}
