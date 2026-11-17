package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
	"github.com/jackc/pgx/v5"
)

// The PostgreSQL store needs a server where TEST_DATABASE_URL can create databases and
// roles (infra/local/compose.yml, or the service in CI). The schema is studio-api's:
// its migrations are applied to a throwaway database.

const campaignsTestRole = "bdg_test_campaigns"

const ensureAppRole = `DO $$ BEGIN
	CREATE ROLE bdg_app NOLOGIN;
EXCEPTION WHEN duplicate_object OR unique_violation THEN NULL;
END $$`

func newPostgresTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL is not set; start infra/local/compose.yml to run Postgres tests")
	}
	ctx := context.Background()
	name := fmt.Sprintf("bdg_campaigns_test_%d", os.Getpid())
	exec := func(target, sql string) {
		t.Helper()
		conn, err := pgx.Connect(ctx, target)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		if _, err := conn.PgConn().Exec(ctx, sql).ReadAll(); err != nil {
			t.Fatalf("%.60s…: %v", sql, err)
		}
	}
	withDatabase := func(user string) string {
		u, err := url.Parse(admin)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name
		if user != "" {
			u.User = url.UserPassword(user, user)
		}
		return u.String()
	}

	exec(admin, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	exec(admin, "CREATE DATABASE "+name)
	t.Cleanup(func() { exec(admin, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })

	// Roles belong to the whole server and studio-api's tests may be creating bdg_app at
	// the same moment, so create it first in a way that tolerates losing that race.
	exec(admin, ensureAppRole)

	migrations, err := filepath.Glob("../studio-api/migrations/*.up.sql")
	if err != nil || len(migrations) == 0 {
		t.Fatalf("studio-api migrations not found: %v", err)
	}
	slices.Sort(migrations)
	for _, file := range migrations {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		exec(withDatabase(""), string(sql))
	}
	exec(withDatabase(""), fmt.Sprintf(`DO $$ BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%[1]s') THEN
			CREATE ROLE %[1]s LOGIN PASSWORD '%[1]s' IN ROLE bdg_app;
		END IF; END $$;
		INSERT INTO workspaces (id, name) VALUES ('ws-customer-service', 'Customer service'), ('ws-hr', 'Human resources');`, campaignsTestRole))

	store, err := NewPostgresStore(ctx, withDatabase(campaignsTestRole))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

func TestPostgresStoreKeepsSuppressionsPerWorkspace(t *testing.T) {
	store := newPostgresTestStore(t)
	ctx := context.Background()
	hash := privacy.EmailHash(testWorkspace, "ana@example.com")

	if suppressed, err := store.IsSuppressed(ctx, testWorkspace, hash); err != nil || suppressed {
		t.Fatalf("before: suppressed = %v, err = %v", suppressed, err)
	}
	for range 2 { // unsubscribing twice is not an error
		if err := store.Suppress(ctx, testWorkspace, hash, ReasonUnsubscribe); err != nil {
			t.Fatal(err)
		}
	}
	if suppressed, err := store.IsSuppressed(ctx, testWorkspace, hash); err != nil || !suppressed {
		t.Errorf("after: suppressed = %v, err = %v", suppressed, err)
	}
	if suppressed, _ := store.IsSuppressed(ctx, "ws-hr", hash); suppressed {
		t.Error("the opt-out is visible from another workspace")
	}

	// The whole flow against the database: send, unsubscribe, send again.
	mailer := &recordingMailer{}
	service := newTestService(t, store, mailer)
	results, err := service.Send(ctx, testCampaign, []Recipient{recipient("ana@example.com"), recipient("luis@example.com")})
	if err != nil {
		t.Fatal(err)
	}
	if got := statuses(results); got[0] != StatusSkippedSuppressed || got[1] != StatusSent {
		t.Errorf("statuses = %v", got)
	}

	var logged int
	err = store.inWorkspace(ctx, testWorkspace, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM campaign_sends WHERE campaign_id = $1", testCampaign.ID).Scan(&logged)
	})
	if err != nil || logged != 2 {
		t.Errorf("delivery log has %d rows (err %v), want 2", logged, err)
	}

	// The application role cannot lift an opt-out, even inside the right workspace.
	err = store.inWorkspace(ctx, testWorkspace, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM campaign_suppressions")
		return err
	})
	if err == nil {
		t.Error("the application role deleted from the do-not-contact list")
	}
}
