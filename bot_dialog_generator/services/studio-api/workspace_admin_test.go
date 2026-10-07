package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// legalEditors stands in for an Entra ID security group: tokens carry the group's
// object ID, not its name.
const legalEditors = "6f1c2a9e-4b7d-4e21-9a3c-1d2e3f405162"

// doAs sends a request as subject with an explicit list of token groups.
func (e *testEnv) doAs(method, path, subject string, groups []string, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	token, err := e.issuer.Mint(subject, groups)
	if err != nil {
		e.t.Fatal(err)
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) audited(action string) bool {
	for _, entry := range e.store.ListAudit() {
		if entry.Action == action {
			return true
		}
	}
	return false
}

type meResponse struct {
	Workspaces []workspaceAccess `json:"workspaces"`
}

// The milestone 1 story: an admin creates "Legal", makes Ana its owner and assigns an
// Entra group; Ana adds Luis; Luis builds a bot nobody outside Legal can see.
func TestAdminCreatesAreaAndOwnerStaffsIt(t *testing.T) {
	e := newTestEnv(t)

	rec := e.do(http.MethodPost, "/admin/workspaces", "dana", `{
		"name": "Legal",
		"members": [{"subject": "ana", "role": "owner"}],
		"groups": [{"group_id": "`+legalEditors+`", "display_name": "Legal editors", "role": "editor"}]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d: %s", rec.Code, rec.Body.String())
	}
	legal := decode[Workspace](t, rec)
	base := "/workspaces/" + legal.ID

	ana := decode[meResponse](t, e.do(http.MethodGet, "/me", "ana", ""))
	if len(ana.Workspaces) != 1 || ana.Workspaces[0].Name != "Legal" || ana.Workspaces[0].Role != RoleOwner {
		t.Fatalf("ana = %+v", ana)
	}

	// A member of the Entra group gets the group's role without being added by name.
	gina := decode[meResponse](t, e.doAs(http.MethodGet, "/me", "gina", []string{legalEditors}, ""))
	if len(gina.Workspaces) != 1 || gina.Workspaces[0].Role != RoleEditor {
		t.Fatalf("group member = %+v", gina)
	}

	if rec := e.do(http.MethodPut, base+"/members/luis", "ana", `{"role":"editor"}`); rec.Code != http.StatusOK {
		t.Fatalf("ana adds luis: status = %d: %s", rec.Code, rec.Body.String())
	}
	rec = e.do(http.MethodPost, base+"/bots", "luis", `{"name":"Contracts helper"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("luis creates a bot: status = %d: %s", rec.Code, rec.Body.String())
	}
	bot := decode[Bot](t, rec)

	for _, outsider := range []string{"alice", "bob", "frank"} {
		if rec := e.do(http.MethodGet, base+"/bots/"+bot.ID, outsider, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s reads Legal's bot: status = %d, want 404", outsider, rec.Code)
		}
	}

	access := decode[Access](t, e.do(http.MethodGet, base+"/members", "luis", ""))
	if len(access.Members) != 2 || len(access.Groups) != 1 || access.Groups[0].DisplayName != "Legal editors" {
		t.Errorf("access = %+v", access)
	}
	for _, action := range []string{"workspace.created Legal", "member.set luis=editor", "bot.created"} {
		if !e.audited(action) {
			t.Errorf("not audited: %s", action)
		}
	}
}

func TestOwnersManageGroupsInTheirOwnArea(t *testing.T) {
	e := newTestEnv(t)
	put := `{"display_name":"Customer service agents","role":"analyst"}`
	if rec := e.do(http.MethodPut, "/workspaces/ws-customer-service/groups/"+legalEditors, "alice", put); rec.Code != http.StatusOK {
		t.Fatalf("grant: status = %d: %s", rec.Code, rec.Body.String())
	}
	member := decode[meResponse](t, e.doAs(http.MethodGet, "/me", "gina", []string{legalEditors}, ""))
	if len(member.Workspaces) != 1 || member.Workspaces[0].ID != "ws-customer-service" || member.Workspaces[0].Role != RoleAnalyst {
		t.Fatalf("after grant = %+v", member)
	}
	if rec := e.do(http.MethodDelete, "/workspaces/ws-customer-service/groups/"+legalEditors, "alice", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d", rec.Code)
	}
	member = decode[meResponse](t, e.doAs(http.MethodGet, "/me", "gina", []string{legalEditors}, ""))
	if len(member.Workspaces) != 0 {
		t.Fatalf("after revoke = %+v", member)
	}
	if !e.audited("group.set "+legalEditors+"=analyst") || !e.audited("group.removed "+legalEditors) {
		t.Errorf("group changes not audited: %+v", e.store.ListAudit())
	}
}

func TestAreaKeepsAtLeastOneOwner(t *testing.T) {
	e := newTestEnv(t)
	// bob is the only owner of Human resources.
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"remove":  e.do(http.MethodDelete, "/workspaces/ws-hr/members/bob", "bob", ""),
		"demote":  e.do(http.MethodPut, "/workspaces/ws-hr/members/bob", "bob", `{"role":"editor"}`),
		"create":  e.do(http.MethodPost, "/admin/workspaces", "dana", `{"name":"Orphan","members":[{"subject":"ana","role":"editor"}]}`),
		"noowner": e.do(http.MethodPost, "/admin/workspaces", "dana", `{"name":"Orphan"}`),
	} {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	// The message is shown to people as is: it says what to do, without a generic prefix.
	problem := decode[ProblemDetails](t, e.do(http.MethodDelete, "/workspaces/ws-hr/members/bob", "bob", ""))
	if want := "This is the area's last owner. Make someone else an owner first, then change or remove this one"; problem.Detail != want {
		t.Errorf("detail = %q, want %q", problem.Detail, want)
	}
	// With a second owner, bob can step down.
	if rec := e.do(http.MethodPut, "/workspaces/ws-hr/members/maria", "bob", `{"role":"owner"}`); rec.Code != http.StatusOK {
		t.Fatalf("add owner: status = %d", rec.Code)
	}
	if rec := e.do(http.MethodDelete, "/workspaces/ws-hr/members/bob", "bob", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("bob steps down: status = %d", rec.Code)
	}
}

func TestMembershipInputIsValidated(t *testing.T) {
	e := newTestEnv(t)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"unknown role":   e.do(http.MethodPut, "/workspaces/ws-customer-service/members/zoe", "alice", `{"role":"platform_admin"}`),
		"missing role":   e.do(http.MethodPut, "/workspaces/ws-customer-service/members/zoe", "alice", `{}`),
		"group role":     e.do(http.MethodPut, "/workspaces/ws-customer-service/groups/g1", "alice", `{"role":"admin"}`),
		"empty name":     e.do(http.MethodPost, "/admin/workspaces", "dana", `{"name":"  ","members":[{"subject":"ana","role":"owner"}]}`),
		"blank subject":  e.do(http.MethodPost, "/admin/workspaces", "dana", `{"name":"Legal","members":[{"subject":" ","role":"owner"}]}`),
		"name too long":  e.do(http.MethodPatch, "/admin/workspaces/ws-hr", "dana", `{"name":"`+strings.Repeat("x", 101)+`"}`),
		"admin escalate": e.do(http.MethodPut, "/workspaces/ws-customer-service/members/zoe", "alice", `{"role":"owner","platform_admin":true}`),
	} {
		want := http.StatusBadRequest
		if name == "admin escalate" {
			// Unknown fields are ignored: zoe becomes an owner of this area only.
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("%s: status = %d, want %d", name, rec.Code, want)
		}
	}
	if rec := e.do(http.MethodPatch, "/admin/workspaces/ws-nope", "dana", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown workspace: status = %d, want 404", rec.Code)
	}
	if me := decode[struct {
		PlatformAdmin bool `json:"platform_admin"`
	}](t, e.do(http.MethodGet, "/me", "zoe", "")); me.PlatformAdmin {
		t.Error("an owner made zoe a platform admin")
	}
}

func TestArchivedAreaIsHiddenFromMembers(t *testing.T) {
	e := newTestEnv(t)
	if rec := e.do(http.MethodPatch, "/admin/workspaces/ws-hr", "dana", `{"archived":true}`); rec.Code != http.StatusOK {
		t.Fatalf("archive: status = %d", rec.Code)
	}
	for _, subject := range []string{"bob", "frank"} {
		if me := decode[meResponse](t, e.do(http.MethodGet, "/me", subject, "")); len(me.Workspaces) != 0 {
			t.Errorf("%s still sees %+v", subject, me.Workspaces)
		}
	}
	if rec := e.do(http.MethodGet, "/workspaces/ws-hr/bots", "bob", ""); rec.Code != http.StatusNotFound {
		t.Errorf("bob lists archived bots: status = %d, want 404", rec.Code)
	}
	archived := false
	for _, ws := range decode[[]Workspace](t, e.do(http.MethodGet, "/admin/workspaces", "dana", "")) {
		archived = archived || (ws.ID == "ws-hr" && ws.ArchivedAt != nil)
	}
	if !archived {
		t.Error("admin list does not show ws-hr as archived")
	}

	if rec := e.do(http.MethodPatch, "/admin/workspaces/ws-hr", "dana", `{"archived":false,"name":"People"}`); rec.Code != http.StatusOK {
		t.Fatalf("restore: status = %d", rec.Code)
	}
	bob := decode[meResponse](t, e.do(http.MethodGet, "/me", "bob", ""))
	if len(bob.Workspaces) != 1 || bob.Workspaces[0].Name != "People" {
		t.Errorf("after restore bob = %+v", bob)
	}
	for _, action := range []string{"workspace.archived", "workspace.restored", "workspace.renamed People"} {
		if !e.audited(action) {
			t.Errorf("not audited: %s", action)
		}
	}
}

func TestAdminWriteRoutesRequirePlatformAdmin(t *testing.T) {
	e := newTestEnv(t)
	for _, rt := range adminWriteRoutes {
		method, pattern, _ := strings.Cut(rt.pattern, " ")
		path := strings.ReplaceAll(pattern, "{workspaceID}", "ws-customer-service")
		body := `{"name":"Takeover","members":[{"subject":"alice","role":"owner"}]}`
		if rec := e.do(method, path, "alice", body); rec.Code != http.StatusForbidden {
			t.Errorf("%s as workspace owner: status = %d, want 403", rt.pattern, rec.Code)
		}
		if rec := e.do(method, path, "", body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token: status = %d, want 401", rt.pattern, rec.Code)
		}
	}
	if len(e.store.ListWorkspaces()) != 2 {
		t.Errorf("non-admin calls changed workspaces: %+v", e.store.ListWorkspaces())
	}
}

// TestOwnerOnlyRoutes pins the required role of sensitive routes. The isolation suite
// reads each route's role from workspaceRoutes itself, so it cannot notice that role
// being lowered by mistake; this list is the independent check.
func TestOwnerOnlyRoutes(t *testing.T) {
	ownerOnly := []string{
		"PUT /workspaces/{workspaceID}/bots/{botID}/active-version",
		"POST /workspaces/{workspaceID}/bots/{botID}/versions/{versionID}/publish",
		"PUT /workspaces/{workspaceID}/members/{subject}",
		"DELETE /workspaces/{workspaceID}/members/{subject}",
		"PUT /workspaces/{workspaceID}/groups/{groupID}",
		"DELETE /workspaces/{workspaceID}/groups/{groupID}",
	}
	need := map[string]Role{}
	for _, rt := range workspaceRoutes {
		need[rt.pattern] = rt.need
	}
	for _, pattern := range ownerOnly {
		if got, ok := need[pattern]; !ok || got != RoleOwner {
			t.Errorf("%s: requires %q, want owner", pattern, got)
		}
	}

	e := newTestEnv(t)
	for _, subject := range []string{"carol", "erin"} {
		if rec := e.do(http.MethodPut, "/workspaces/ws-customer-service/members/"+subject, subject, `{"role":"owner"}`); rec.Code != http.StatusForbidden {
			t.Errorf("%s promotes themselves: status = %d, want 403", subject, rec.Code)
		}
	}
}

// The compiled definition names its workspace, so the runtime can refuse to run it
// for another area's conversations.
func TestPublishedDefinitionCarriesWorkspace(t *testing.T) {
	e := newTestEnv(t)
	artifact, _, err := e.store.PublishVersion("ws-customer-service", "retail-assistant", "v18")
	if err != nil {
		t.Fatal(err)
	}
	if got := artifact.Definition.GetWorkspaceId(); got != "ws-customer-service" {
		t.Fatalf("definition workspace_id = %q, want ws-customer-service", got)
	}
}
