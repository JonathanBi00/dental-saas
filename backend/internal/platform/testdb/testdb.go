// Package testdb starts a real Postgres (via testcontainers) with the same roles as local
// development, runs migrations as the owner, and hands tests a pool connected as the
// runtime app role, so row-level security is exercised exactly as in the running system.
//
// One database is shared per test binary (i.e. per package). Tests isolate themselves by
// creating their own organisations and unique emails.
//
// If TESTDB_ADMIN_URL is set (a superuser URL for a Postgres created from db/init, such as
// the postgres-test service in docker-compose.yml), each package creates its own database
// there and drops it afterwards. Otherwise a container is started with testcontainers.
package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"dentalsaas/internal/access"
	"dentalsaas/internal/identity"
	"dentalsaas/internal/platform/db"
	"dentalsaas/internal/platform/ids"
	"dentalsaas/internal/store"
	"dentalsaas/migrations"
)

type Env struct {
	AppURL   string
	OwnerURL string
	Pool     *db.Pool // connected as dental_app
}

var (
	once      sync.Once
	shared    *Env
	startErr  error
	container testcontainers.Container
	dropDB    func() // set when using TESTDB_ADMIN_URL
)

func init() {
	// Ryuk (testcontainers' reaper) does not start reliably on Docker Desktop for Windows.
	// Containers are terminated by Main instead.
	if _, set := os.LookupEnv("TESTCONTAINERS_RYUK_DISABLED"); !set {
		os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}
	// testcontainers probes for the Docker socket path on startup. On Windows that probe can
	// fail with "rootless Docker is not supported" when several test packages start at once
	// (as `go test ./...` does). Docker Desktop's in-VM socket path is fixed, so set it directly.
	if runtime.GOOS == "windows" {
		if _, set := os.LookupEnv("TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE"); !set {
			os.Setenv("TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE", "//var/run/docker.sock")
		}
	}
}

// Main runs a package's tests and then removes the database container.
// Use it from TestMain in every package that calls Start:
//
//	func TestMain(m *testing.M) { testdb.Main(m) }
func Main(m *testing.M) {
	code := m.Run()
	if shared != nil && shared.Pool != nil {
		shared.Pool.Close()
	}
	if dropDB != nil {
		dropDB()
	}
	if container != nil {
		_ = container.Terminate(context.Background())
	}
	os.Exit(code)
}

// Start returns the shared environment, starting the container on first use.
func Start(t testing.TB) *Env {
	t.Helper()
	once.Do(func() { shared, startErr = start(context.Background()) })
	if startErr != nil {
		t.Fatalf("start test database (is Docker running?): %v", startErr)
	}
	return shared
}

func start(ctx context.Context) (*Env, error) {
	if admin := os.Getenv("TESTDB_ADMIN_URL"); admin != "" {
		return startShared(ctx, admin)
	}
	return startContainer(ctx)
}

// startShared creates a throwaway database on an existing Postgres. It is copied from the
// "dental" database that db/init creates, so it has the same grants and extensions.
func startShared(ctx context.Context, adminURL string) (*Env, error) {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return nil, fmt.Errorf("connect TESTDB_ADMIN_URL: %w", err)
	}
	defer conn.Close(ctx)
	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name+" TEMPLATE dental OWNER dental_owner"); err != nil {
		return nil, fmt.Errorf("create test database: %w", err)
	}
	dropDB = func() {
		ctx := context.Background()
		if c, err := pgx.Connect(ctx, adminURL); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = c.Close(ctx)
		}
	}
	cfg := conn.Config()
	return finish(ctx, cfg.Host, fmt.Sprint(cfg.Port), name)
}

func startContainer(ctx context.Context) (*Env, error) {
	_, file, _, _ := runtime.Caller(0)
	initScript := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "db", "init", "01-roles.sql")

	c, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithInitScripts(initScript),
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, err
	}
	container = c
	host, err := c.Host(ctx)
	if err != nil {
		return nil, err
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, err
	}
	return finish(ctx, host, port.Port(), "dental")
}

// finish migrates the database as the owner and opens the app-role pool.
func finish(ctx context.Context, host, port, dbname string) (*Env, error) {
	url := func(role string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, role, host, port, dbname)
	}
	env := &Env{AppURL: url("dental_app"), OwnerURL: url("dental_owner")}
	if err := migrations.Run(ctx, env.OwnerURL, "up"); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	pool, err := db.Open(ctx, env.AppURL)
	if err != nil {
		return nil, err
	}
	env.Pool = pool
	return env, nil
}

// SuperuserURL connects as postgres (bypasses RLS). Only for test assertions on rows no
// tenant can see, such as pre-tenant audit events.
func (e *Env) SuperuserURL() string {
	return strings.Replace(e.OwnerURL, "dental_owner:dental_owner", "postgres:postgres", 1)
}

// ------------------------------------------------------------------- fixtures

// Email returns an address unique to this test run.
func Email(t testing.TB, prefix string) string {
	return fmt.Sprintf("%s-%s@test.local", prefix, strings.ReplaceAll(uuid.NewString()[:13], "-", ""))
}

func CreateOrg(t testing.TB, pool *db.Pool, name string) uuid.UUID {
	t.Helper()
	id := ids.New()
	must(t, pool.InTenantTx(context.Background(), db.Tenant{OrgID: id}, func(tx *db.Tx) error {
		_, err := tx.CreateOrganisation(context.Background(), store.CreateOrganisationParams{ID: id, Name: name})
		return err
	}))
	return id
}

func CreateLocation(t testing.TB, pool *db.Pool, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := ids.New()
	must(t, pool.InTenantTx(context.Background(), db.Tenant{OrgID: orgID}, func(tx *db.Tx) error {
		_, err := tx.CreateLocation(context.Background(), store.CreateLocationParams{
			ID: id, OrgID: orgID, Name: name, Timezone: "Australia/Sydney",
		})
		return err
	}))
	return id
}

const Password = "correct-horse-battery"

var passwordHash = sync.OnceValue(func() string {
	h, err := identity.HashPassword(Password)
	if err != nil {
		panic(err)
	}
	return h
})

type Member struct {
	UserID       uuid.UUID
	MembershipID uuid.UUID
	Email        string
}

// CreateUser creates an account with password testdb.Password and no memberships.
func CreateUser(t testing.TB, pool *db.Pool, email string) uuid.UUID {
	t.Helper()
	id := ids.New()
	must(t, pool.InTenantTx(context.Background(), db.Tenant{UserID: id}, func(tx *db.Tx) error {
		return tx.CreateUser(context.Background(), store.CreateUserParams{ID: id, Email: email, Name: "Test " + email, PasswordHash: passwordHash()})
	}))
	return id
}

// AddMember adds an existing user to an organisation, optionally restricted to locations.
func AddMember(t testing.TB, pool *db.Pool, orgID, userID uuid.UUID, role access.Role, locations ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var mid uuid.UUID
	must(t, pool.InTenantTx(ctx, db.Tenant{OrgID: orgID, UserID: userID}, func(tx *db.Tx) error {
		m, err := tx.UpsertMembership(ctx, store.UpsertMembershipParams{ID: ids.New(), OrgID: orgID, UserID: userID, Role: string(role)})
		if err != nil {
			return err
		}
		mid = m.ID
		for _, l := range locations {
			if err := tx.AddMembershipLocation(ctx, store.AddMembershipLocationParams{OrgID: orgID, MembershipID: m.ID, LocationID: l}); err != nil {
				return err
			}
		}
		return nil
	}))
	return mid
}

// CreateMember creates a user and adds them to the organisation.
func CreateMember(t testing.TB, pool *db.Pool, orgID uuid.UUID, role access.Role, locations ...uuid.UUID) Member {
	t.Helper()
	email := Email(t, string(role))
	uid := CreateUser(t, pool, email)
	return Member{UserID: uid, MembershipID: AddMember(t, pool, orgID, uid, role, locations...), Email: email}
}

// SetOrgStatus changes an organisation's status (e.g. "suspended").
func SetOrgStatus(t testing.TB, pool *db.Pool, orgID uuid.UUID, status string) {
	t.Helper()
	must(t, pool.InTenantTx(context.Background(), db.Tenant{OrgID: orgID}, func(tx *db.Tx) error {
		_, err := tx.PG.Exec(context.Background(), "UPDATE organisations SET status = $1 WHERE id = $2", status, orgID)
		return err
	}))
}

// AuditEvents returns the organisation's audit entries for an action (newest first),
// optionally filtered to a target.
func AuditEvents(t testing.TB, pool *db.Pool, orgID uuid.UUID, action string, target *uuid.UUID) []store.ListAuditEventsRow {
	t.Helper()
	var rows []store.ListAuditEventsRow
	must(t, pool.InTenantTx(context.Background(), db.Tenant{OrgID: orgID}, func(tx *db.Tx) error {
		var err error
		rows, err = tx.ListAuditEvents(context.Background(), store.ListAuditEventsParams{Action: &action, TargetID: target, RowLimit: 1000})
		return err
	}))
	return rows
}

// AssertAudited fails unless at least one entry exists for action (and target, if given).
func AssertAudited(t testing.TB, pool *db.Pool, orgID uuid.UUID, action string, target *uuid.UUID) store.ListAuditEventsRow {
	t.Helper()
	rows := AuditEvents(t, pool, orgID, action, target)
	if len(rows) == 0 {
		t.Fatalf("expected audit event %q for org %s (target %v), found none", action, orgID, target)
	}
	return rows[0]
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
