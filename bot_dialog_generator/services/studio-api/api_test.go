package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/auth"
)

const adminGroup = "bdg-platform-admins"

// testEnv is a Studio API backed by the seeded demo store, with a dev issuer that
// mints tokens for the demo users.
type testEnv struct {
	t       *testing.T
	store   Store
	issuer  *auth.DevIssuer
	handler http.Handler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	iss, err := auth.NewDevIssuer("studio-api")
	if err != nil {
		t.Fatal(err)
	}
	store := newStoreUnderTest(t)
	return &testEnv{t: t, store: store, issuer: iss, handler: NewStudioHandler(store, Config{
		Verifier:           iss,
		DevIssuer:          iss,
		PlatformAdminGroup: adminGroup,
		AllowedOrigins:     []string{"http://localhost:5174"},
	})}
}

// token mints a token for one of the demo users:
// alice (owner), erin (editor) and carol (analyst) in Customer service; bob (owner)
// in Human resources; frank (hr-team group, editor in Human resources); dana (platform admin).
func (e *testEnv) token(subject string) string {
	e.t.Helper()
	var groups []string
	switch subject {
	case "dana":
		groups = []string{adminGroup}
	case "frank":
		groups = []string{"hr-team"}
	}
	token, err := e.issuer.Mint(subject, groups)
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

func (e *testEnv) do(method, path, subject, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if subject != "" {
		req.Header.Set("Authorization", "Bearer "+e.token(subject))
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) auditLog() []AuditEntry {
	e.t.Helper()
	list, err := e.store.ListAudit(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

func (e *testEnv) workspaces() []*Workspace {
	e.t.Helper()
	list, err := e.store.ListWorkspaces(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

func (e *testEnv) outbox() []*OutboxEvent {
	e.t.Helper()
	list, err := e.store.GetOutboxEvents(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestListBotsShowsOnlyOwnWorkspace(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(http.MethodGet, "/workspaces/ws-customer-service/bots", "alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	bots := decode[[]Bot](t, rec)
	if len(bots) != 1 || bots[0].ID != "retail-assistant" || bots[0].WorkspaceID != "ws-customer-service" {
		t.Fatalf("bots = %+v", bots)
	}
}

func TestGetDraftVersion(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(http.MethodGet, "/workspaces/ws-customer-service/bots/retail-assistant/versions/v18", "carol", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	ver := decode[Version](t, rec)
	if ver.Version != "v18" || len(ver.Nodes) != 4 {
		t.Fatalf("version = %s with %d nodes", ver.Version, len(ver.Nodes))
	}
}

func TestPublishDraft(t *testing.T) {
	e := newTestEnv(t)
	base := "/workspaces/ws-customer-service/bots/retail-assistant"

	rec := e.do(http.MethodPost, base+"/versions/v18/publish", "alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: status = %d: %s", rec.Code, rec.Body.String())
	}
	result := decode[map[string]any](t, rec)
	if result["status"] != "published" {
		t.Errorf("status = %v", result["status"])
	}
	wantURI := "s3://bot-dialog-generator-definitions/workspaces/ws-customer-service/bots/retail-assistant/v18.pb"
	if result["artifact_uri"] != wantURI {
		t.Errorf("artifact_uri = %v, want %s", result["artifact_uri"], wantURI)
	}

	active, err := e.store.GetActiveVersion(context.Background(), "ws-customer-service", "retail-assistant")
	if err != nil || active != "v18" {
		t.Errorf("active version = %q (%v), want v18", active, err)
	}
	outbox := e.outbox()
	if len(outbox) != 1 || outbox[0].EventType != "bot.published" {
		t.Fatalf("outbox = %+v", outbox)
	}
	if payload := outbox[0].Payload.(map[string]any); payload["workspace_id"] != "ws-customer-service" {
		t.Errorf("event workspace_id = %v", payload["workspace_id"])
	}

	put := e.do(http.MethodPut, base+"/versions/v18", "alice", `{"nodes":[]}`)
	if put.Code != http.StatusBadRequest {
		t.Fatalf("modify published: status = %d, want 400", put.Code)
	}
	if ct := put.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content type = %s", ct)
	}
}

func TestRollbackToPublishedVersion(t *testing.T) {
	e := newTestEnv(t)
	base := "/workspaces/ws-customer-service/bots/retail-assistant"
	if rec := e.do(http.MethodPost, base+"/versions/v18/publish", "alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d", rec.Code)
	}
	if rec := e.do(http.MethodPut, base+"/active-version", "alice", `{"version":"v17"}`); rec.Code != http.StatusOK {
		t.Fatalf("rollback: status = %d: %s", rec.Code, rec.Body.String())
	}
	if active, _ := e.store.GetActiveVersion(context.Background(), "ws-customer-service", "retail-assistant"); active != "v17" {
		t.Errorf("active = %s, want v17", active)
	}
	draft := e.do(http.MethodPost, base+"/versions", "alice", "")
	if draft.Code != http.StatusCreated {
		t.Fatalf("create draft: %d", draft.Code)
	}
	newDraft := decode[Version](t, draft).Version
	if rec := e.do(http.MethodPut, base+"/active-version", "alice", `{"version":"`+newDraft+`"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("activating a draft: status = %d, want 400", rec.Code)
	}
}

func TestCreateBotUsesUnguessableID(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(http.MethodPost, "/workspaces/ws-hr/bots", "frank", `{"name":"Onboarding guide"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	bot := decode[Bot](t, rec)
	if bot.WorkspaceID != "ws-hr" || len(bot.ID) != 36 || bot.ID[14] != '7' {
		t.Fatalf("bot = %+v, want a UUIDv7 id in ws-hr", bot)
	}
	if rec := e.do(http.MethodPost, "/workspaces/ws-hr/bots/"+bot.ID+"/versions/v1/publish", "bob", ""); rec.Code != http.StatusOK {
		t.Errorf("starter draft should publish: status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodPost, "/workspaces/ws-hr/bots", "frank", `{"name":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty name: status = %d, want 400", rec.Code)
	}
}

func TestMeListsOnlyMemberships(t *testing.T) {
	e := newTestEnv(t)
	type me struct {
		Subject       string            `json:"subject"`
		PlatformAdmin bool              `json:"platform_admin"`
		Workspaces    []workspaceAccess `json:"workspaces"`
	}
	bob := decode[me](t, e.do(http.MethodGet, "/me", "bob", ""))
	if len(bob.Workspaces) != 1 || bob.Workspaces[0].ID != "ws-hr" || bob.Workspaces[0].Role != RoleOwner || bob.PlatformAdmin {
		t.Errorf("bob = %+v", bob)
	}
	frank := decode[me](t, e.do(http.MethodGet, "/me", "frank", ""))
	if len(frank.Workspaces) != 1 || frank.Workspaces[0].Role != RoleEditor {
		t.Errorf("frank (group grant) = %+v", frank)
	}
	dana := decode[me](t, e.do(http.MethodGet, "/me", "dana", ""))
	if !dana.PlatformAdmin || len(dana.Workspaces) != 0 {
		t.Errorf("dana = %+v", dana)
	}
}

func TestAdminRoutes(t *testing.T) {
	e := newTestEnv(t)
	for _, rt := range adminRoutes {
		path := strings.TrimPrefix(rt.pattern, "GET ")
		if rec := e.do(http.MethodGet, path, "alice", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s as workspace owner: status = %d, want 403", path, rec.Code)
		}
		if rec := e.do(http.MethodGet, path, "dana", ""); rec.Code != http.StatusOK {
			t.Errorf("%s as admin: status = %d, want 200", path, rec.Code)
		}
	}
	bots := decode[[]Bot](t, e.do(http.MethodGet, "/admin/bots", "dana", ""))
	if len(bots) != 2 {
		t.Errorf("admin sees %d bots, want both workspaces' 2", len(bots))
	}
	audit := e.auditLog()
	if len(audit) < len(adminRoutes) || audit[0].Subject != "dana" || !strings.HasPrefix(audit[0].Action, "admin.read") {
		t.Errorf("admin reads not audited: %+v", audit)
	}
}

func TestCORS(t *testing.T) {
	e := newTestEnv(t)
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/workspaces/ws-hr/bots", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		rec := httptest.NewRecorder()
		e.handler.ServeHTTP(rec, req)
		return rec
	}
	ok := preflight("http://localhost:5174")
	if ok.Code != http.StatusNoContent || ok.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5174" {
		t.Errorf("allowed origin: status %d, allow-origin %q", ok.Code, ok.Header().Get("Access-Control-Allow-Origin"))
	}
	if got := preflight("https://evil.example.com").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Access-Control-Allow-Origin %q", got)
	}
}

func TestDevTokenEndpoint(t *testing.T) {
	e := newTestEnv(t)
	req := httptest.NewRequest(http.MethodPost, "/dev/token", strings.NewReader(`{"subject":"bob"}`))
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	token := decode[map[string]string](t, rec)["access_token"]
	me := httptest.NewRequest(http.MethodGet, "/me", nil)
	me.Header.Set("Authorization", "Bearer "+token)
	meRec := httptest.NewRecorder()
	e.handler.ServeHTTP(meRec, me)
	if meRec.Code != http.StatusOK {
		t.Fatalf("minted token rejected: %d", meRec.Code)
	}

	withoutDev := NewStudioHandler(e.store, Config{Verifier: e.issuer, PlatformAdminGroup: adminGroup})
	rec = httptest.NewRecorder()
	withoutDev.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/dev/token", strings.NewReader(`{"subject":"bob"}`)))
	if rec.Code == http.StatusOK {
		t.Fatal("/dev/token must not exist when DevIssuer is not configured")
	}
}

func TestHealthIsPublic(t *testing.T) {
	e := newTestEnv(t)
	for _, path := range []string{"/livez", "/readyz"} {
		if rec := e.do(http.MethodGet, path, "", ""); rec.Code != http.StatusNoContent {
			t.Errorf("%s: status = %d, want 204", path, rec.Code)
		}
	}
}
