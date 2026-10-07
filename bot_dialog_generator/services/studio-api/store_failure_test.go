package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

var errStoreDown = errors.New("store unavailable")

// failingStore wraps a working store and fails the operations under test.
type failingStore struct {
	Store
	rolesFail bool
	auditFail bool
}

func (f *failingStore) RolesFor(ctx context.Context, subject string, groups []string) (map[string]Role, error) {
	if f.rolesFail {
		return nil, errStoreDown
	}
	return f.Store.RolesFor(ctx, subject, groups)
}

func (f *failingStore) AppendAudit(ctx context.Context, entry AuditEntry) error {
	if f.auditFail {
		return errStoreDown
	}
	return f.Store.AppendAudit(ctx, entry)
}

func envWithStore(t *testing.T, store Store) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	e.handler = NewStudioHandler(store, Config{Verifier: e.issuer, PlatformAdminGroup: adminGroup})
	return e
}

// A storage failure must never turn into "no access" (an empty workspace list) or into access.
func TestRolesLookupFailureIsAnError(t *testing.T) {
	e := envWithStore(t, &failingStore{Store: NewMemoryStore(), rolesFail: true})
	for _, path := range []string{"/me", "/workspaces", "/workspaces/ws-customer-service/bots"} {
		if rec := e.do(http.MethodGet, path, "alice", ""); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", path, rec.Code)
		}
	}
}

// Admins only get access when that access can be recorded.
func TestAdminAccessNeedsAnAuditRecord(t *testing.T) {
	e := envWithStore(t, &failingStore{Store: NewMemoryStore(), auditFail: true})
	for _, path := range []string{"/workspaces/ws-hr/bots", "/admin/bots"} {
		if rec := e.do(http.MethodGet, path, "dana", ""); rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500 (no audit, no admin access)", path, rec.Code)
		}
	}
	// Members are not affected: their access is not an exception that needs a record.
	if rec := e.do(http.MethodGet, "/workspaces/ws-hr/bots", "bob", ""); rec.Code != http.StatusOK {
		t.Errorf("member: status = %d, want 200", rec.Code)
	}
}

func TestNewDraftFollowsTheHighestVersion(t *testing.T) {
	e := newTestEnv(t)
	// retail-assistant has v17 and v18; counting versions would give "v3".
	rec := e.do(http.MethodPost, "/workspaces/ws-customer-service/bots/retail-assistant/versions", "alice", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode[Version](t, rec).Version; got != "v19" {
		t.Errorf("new draft = %s, want v19", got)
	}
}
