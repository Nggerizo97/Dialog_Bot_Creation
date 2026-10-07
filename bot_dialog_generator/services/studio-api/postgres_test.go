package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Postgres tests need a server where TEST_DATABASE_URL can create databases and roles,
// e.g. postgres://bdg_owner:bdg_owner_local@localhost:5433/postgres (infra/local/compose.yml).
// With STUDIO_TEST_STORE=postgres, the whole studio-api suite runs against Postgres.
// Each test gets its own database, cloned from a migrated and seeded template.

const testAppRole, testAppPassword = "bdg_test_app", "bdg_test_app"

var (
	pgTemplateOnce sync.Once
	pgTemplate     string
	pgTemplateErr  error
	pgDatabases    atomic.Int64
)

func TestMain(m *testing.M) {
	code := m.Run()
	if pgTemplate != "" {
		if err := adminExec(context.Background(), os.Getenv("TEST_DATABASE_URL"), "DROP DATABASE IF EXISTS "+pgx.Identifier{pgTemplate}.Sanitize()+" WITH (FORCE)"); err != nil {
			fmt.Fprintln(os.Stderr, "drop template database:", err)
		}
	}
	os.Exit(code)
}

// newStoreUnderTest returns the store the suite runs against.
func newStoreUnderTest(t *testing.T) Store {
	t.Helper()
	if os.Getenv("STUDIO_TEST_STORE") == "postgres" {
		return newPostgresTestStore(t)
	}
	return NewMemoryStore()
}

func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("TEST_DATABASE_URL")
	if u == "" {
		t.Skip("TEST_DATABASE_URL is not set; start infra/local/compose.yml to run Postgres tests")
	}
	return u
}

func adminExec(ctx context.Context, adminURL string, sql string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

// withDatabase returns rawURL pointing at database name, optionally as another user.
func withDatabase(t *testing.T, rawURL, name, user, password string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	return u.String()
}

func buildTemplate(t *testing.T, admin string) (string, error) {
	ctx := context.Background()
	name := fmt.Sprintf("bdg_test_template_%d", os.Getpid())
	if err := adminExec(ctx, admin, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", err
	}
	owner, err := pgx.Connect(ctx, withDatabase(t, admin, name, "", ""))
	if err != nil {
		return name, err
	}
	defer owner.Close(ctx)
	if err := Migrate(ctx, owner); err != nil {
		return name, err
	}
	// Roles are shared by the whole server: create the test login once.
	createRole := fmt.Sprintf(`DO $$ BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
			CREATE ROLE %s LOGIN PASSWORD '%s' IN ROLE bdg_app;
		END IF; END $$`, testAppRole, testAppRole, testAppPassword)
	if _, err := owner.Exec(ctx, createRole); err != nil {
		return name, err
	}
	store, err := NewPostgresStore(ctx, withDatabase(t, admin, name, testAppRole, testAppPassword))
	if err != nil {
		return name, err
	}
	defer store.Close()
	return name, store.Seed(ctx, newDemoSeed(time.Now().UTC()))
}

// newTestDatabase clones the seeded template into a database used by one test.
func newTestDatabase(t *testing.T) (admin, name string) {
	t.Helper()
	admin = adminURL(t)
	pgTemplateOnce.Do(func() { pgTemplate, pgTemplateErr = buildTemplate(t, admin) })
	if pgTemplateErr != nil {
		t.Fatalf("build template database: %v", pgTemplateErr)
	}
	ctx := context.Background()
	name = fmt.Sprintf("bdg_test_%d_%d", os.Getpid(), pgDatabases.Add(1))
	if err := adminExec(ctx, admin, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE "+pgx.Identifier{pgTemplate}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		if err := adminExec(ctx, admin, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database: %v", err)
		}
	})
	return admin, name
}

func newPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	admin, name := newTestDatabase(t)
	store, err := NewPostgresStore(context.Background(), withDatabase(t, admin, name, testAppRole, testAppPassword))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

// appConn connects to a fresh test database as the application role, which is how
// studio-api connects: it owns nothing, so row-level security applies.
func appConn(t *testing.T) *pgx.Conn {
	t.Helper()
	admin, name := newTestDatabase(t)
	conn, err := pgx.Connect(context.Background(), withDatabase(t, admin, name, testAppRole, testAppPassword))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	return conn
}

// inScope runs fn in a transaction scoped like the store scopes it.
func inScope(t *testing.T, conn *pgx.Conn, setting, value string, fn func(tx pgx.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rolled back unless committed
	if setting != "" {
		if _, err := tx.Exec(ctx, "SELECT set_config($1, $2, true)", setting, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func botIDs(t *testing.T, tx pgx.Tx) []string {
	t.Helper()
	rows, err := tx.Query(context.Background(), "SELECT id FROM bots ORDER BY id") // deliberately no WHERE clause
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func isRLSViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501" // insufficient_privilege, also used by row-level security
}

// TestRowLevelSecurity proves the database itself keeps areas apart, independently of
// the Go code's WHERE clauses.
func TestRowLevelSecurity(t *testing.T) {
	conn := appConn(t)
	ctx := context.Background()

	t.Run("a query without a workspace filter sees only its own workspace", func(t *testing.T) {
		_ = inScope(t, conn, "app.workspace_id", "ws-hr", func(tx pgx.Tx) error {
			if ids := botIDs(t, tx); len(ids) != 1 || ids[0] != "hr-helpdesk" {
				t.Errorf("visible bots = %v, want only hr-helpdesk", ids)
			}
			var versions int
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM bot_versions WHERE bot_id = 'retail-assistant'").Scan(&versions); err != nil {
				t.Fatal(err)
			}
			if versions != 0 {
				t.Errorf("ws-hr sees %d of Customer service's versions", versions)
			}
			return nil
		})
	})

	t.Run("no workspace declared sees nothing", func(t *testing.T) {
		_ = inScope(t, conn, "", "", func(tx pgx.Tx) error {
			if ids := botIDs(t, tx); len(ids) != 0 {
				t.Errorf("visible bots = %v, want none", ids)
			}
			return nil
		})
	})

	t.Run("cannot write into another workspace", func(t *testing.T) {
		err := inScope(t, conn, "app.workspace_id", "ws-hr", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "INSERT INTO bots (id, workspace_id, tenant, name, draft_version) VALUES ('sneaky', 'ws-customer-service', 'demo', 'x', 'v1')")
			return err
		})
		if !isRLSViolation(err) {
			t.Errorf("insert into another workspace: err = %v, want a row-level security violation", err)
		}
	})

	t.Run("cannot move a bot to another workspace", func(t *testing.T) {
		err := inScope(t, conn, "app.workspace_id", "ws-hr", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE bots SET workspace_id = 'ws-customer-service' WHERE id = 'hr-helpdesk'")
			return err
		})
		if err == nil {
			t.Error("moving a bot across workspaces succeeded")
		}
	})

	t.Run("cannot change or delete another workspace's rows", func(t *testing.T) {
		_ = inScope(t, conn, "app.workspace_id", "ws-hr", func(tx pgx.Tx) error {
			for _, sql := range []string{
				"UPDATE bots SET name = 'hacked' WHERE id = 'retail-assistant'",
				"DELETE FROM bot_versions WHERE bot_id = 'retail-assistant'",
			} {
				tag, err := tx.Exec(ctx, sql)
				if err != nil {
					t.Fatalf("%s: %v", sql, err)
				}
				if tag.RowsAffected() != 0 {
					t.Errorf("%s affected %d rows of another workspace", sql, tag.RowsAffected())
				}
			}
			return nil
		})
	})

	t.Run("platform-admin reads see every workspace", func(t *testing.T) {
		_ = inScope(t, conn, "app.all_workspaces", "on", func(tx pgx.Tx) error {
			if ids := botIDs(t, tx); len(ids) != 2 {
				t.Errorf("visible bots = %v, want both workspaces' bots", ids)
			}
			return nil
		})
	})

	t.Run("the audit log is append-only", func(t *testing.T) {
		if _, err := conn.Exec(ctx, "INSERT INTO audit_log (subject, action, resource) VALUES ('alice', 'test', '/x')"); err != nil {
			t.Fatalf("append: %v", err)
		}
		for _, sql := range []string{"UPDATE audit_log SET action = 'tampered'", "DELETE FROM audit_log"} {
			if _, err := conn.Exec(ctx, sql); !isRLSViolation(err) {
				t.Errorf("%s: err = %v, want permission denied", sql, err)
			}
		}
	})

	t.Run("the application role owns nothing and cannot change the schema", func(t *testing.T) {
		if _, err := conn.Exec(ctx, "ALTER TABLE bots DISABLE ROW LEVEL SECURITY"); err == nil || !strings.Contains(err.Error(), "owner") {
			t.Errorf("disabling row-level security: err = %v, want must be owner", err)
		}
	})
}

func TestMigrateIsIdempotent(t *testing.T) {
	admin, name := newTestDatabase(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, withDatabase(t, admin, name, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := Migrate(ctx, conn); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var applied int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Errorf("schema_migrations has %d rows, want 1", applied)
	}
}
