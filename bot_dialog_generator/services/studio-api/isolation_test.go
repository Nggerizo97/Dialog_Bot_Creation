package main

import (
	"net/http"
	"strings"
	"testing"
)

// Workspace isolation suite (ADR 0003). Every route in workspaceRoutes is checked,
// so a new route cannot ship without isolation coverage.

// requestFor turns a route pattern into a concrete request against the seeded store.
func requestFor(rt workspaceRoute, workspaceID, botID string) (method, path, body string) {
	method, pattern, _ := strings.Cut(rt.pattern, " ")
	path = strings.NewReplacer(
		"{workspaceID}", workspaceID,
		"{botID}", botID,
		"{versionID}", "v18",
		"{subject}", "erin",
		"{groupID}", "cs-agents",
	).Replace(pattern)
	switch {
	case strings.HasSuffix(pattern, "/members/{subject}") && method == http.MethodPut:
		body = `{"role":"editor"}`
	case strings.HasSuffix(pattern, "/groups/{groupID}") && method == http.MethodPut:
		body = `{"role":"analyst","display_name":"Customer service agents"}`
	case strings.HasSuffix(pattern, "/active-version") && method == http.MethodPut:
		body = `{"version":"v17"}`
	case strings.HasSuffix(pattern, "/bots") && method == http.MethodPost:
		body = `{"name":"Isolation probe"}`
	case strings.HasSuffix(pattern, "{versionID}") && method == http.MethodPut:
		body = `{"entry_node_id":"welcome","nodes":[{"id":"welcome","type":"Trigger","title":"Welcome"}],"edges":[]}`
	}
	return method, path, body
}

func denied(code int) bool {
	return code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusNotFound
}

func TestWorkspaceIsolation(t *testing.T) {
	if len(workspaceRoutes) == 0 {
		t.Fatal("no workspace routes registered")
	}
	for _, rt := range workspaceRoutes {
		t.Run(rt.pattern, func(t *testing.T) {
			usesBot := strings.Contains(rt.pattern, "{botID}")

			t.Run("no token is 401", func(t *testing.T) {
				e := newTestEnv(t)
				method, path, body := requestFor(rt, "ws-customer-service", "retail-assistant")
				if rec := e.do(method, path, "", body); rec.Code != http.StatusUnauthorized {
					t.Fatalf("status = %d, want 401", rec.Code)
				}
			})

			t.Run("member of another workspace is 404", func(t *testing.T) {
				e := newTestEnv(t)
				method, path, body := requestFor(rt, "ws-customer-service", "retail-assistant")
				for _, outsider := range []string{"bob", "frank"} {
					if rec := e.do(method, path, outsider, body); rec.Code != http.StatusNotFound {
						t.Errorf("%s: status = %d, want 404", outsider, rec.Code)
					}
				}
			})

			t.Run("unknown workspace is 404", func(t *testing.T) {
				e := newTestEnv(t)
				method, path, body := requestFor(rt, "ws-does-not-exist", "retail-assistant")
				for _, subject := range []string{"alice", "dana"} {
					if rec := e.do(method, path, subject, body); rec.Code != http.StatusNotFound {
						t.Errorf("%s: status = %d, want 404", subject, rec.Code)
					}
				}
			})

			if usesBot {
				t.Run("bot from another workspace is 404", func(t *testing.T) {
					e := newTestEnv(t)
					// alice is an owner of ws-customer-service, but hr-helpdesk belongs to ws-hr.
					method, path, body := requestFor(rt, "ws-customer-service", "hr-helpdesk")
					if rec := e.do(method, path, "alice", body); rec.Code != http.StatusNotFound {
						t.Fatalf("status = %d, want 404", rec.Code)
					}
				})
			}

			t.Run("roles", func(t *testing.T) {
				for subject, role := range map[string]Role{"carol": RoleAnalyst, "erin": RoleEditor, "alice": RoleOwner} {
					e := newTestEnv(t)
					method, path, body := requestFor(rt, "ws-customer-service", "retail-assistant")
					rec := e.do(method, path, subject, body)
					if role.Allows(rt.need) {
						if denied(rec.Code) {
							t.Errorf("%s (%s): status = %d, want access", subject, role, rec.Code)
						}
					} else if rec.Code != http.StatusForbidden {
						t.Errorf("%s (%s): status = %d, want 403", subject, role, rec.Code)
					}
				}
			})

			t.Run("platform admin has access and is audited", func(t *testing.T) {
				e := newTestEnv(t)
				method, path, body := requestFor(rt, "ws-customer-service", "retail-assistant")
				if rec := e.do(method, path, "dana", body); denied(rec.Code) {
					t.Fatalf("status = %d, want access", rec.Code)
				}
				audited := false
				for _, entry := range e.auditLog() {
					if entry.Subject == "dana" && entry.WorkspaceID == "ws-customer-service" && entry.Action == "admin.access "+rt.pattern && entry.Resource == path {
						audited = true
					}
				}
				if !audited {
					t.Fatalf("no audit entry for admin access; log = %+v", e.auditLog())
				}
			})
		})
	}
}

func TestMembersDoNotSeeOtherWorkspaces(t *testing.T) {
	e := newTestEnv(t)
	list := decode[[]workspaceAccess](t, e.do(http.MethodGet, "/workspaces", "alice", ""))
	for _, ws := range list {
		if ws.ID == "ws-hr" {
			t.Fatal("alice can see ws-hr in her workspace list")
		}
	}
	if len(list) != 1 {
		t.Fatalf("alice sees %d workspaces, want 1", len(list))
	}
}
