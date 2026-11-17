package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
	"github.com/jackc/pgx/v5"
)

const (
	csRequests = "/workspaces/ws-customer-service/data-deletion-requests"
	csInternal = "/internal/workspaces/ws-customer-service"
)

// doInternal calls a service-to-service route the way the channel gateway does.
func (e *testEnv) doInternal(path, token, body string) *httptest.ResponseRecorder {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func (e *testEnv) consent(workspacePath, userID string) {
	e.t.Helper()
	body := `{"channel":"webchat","subject_id":"` + userID + `","purposes":{"processing":true,"analytics":false,"tracking_pixel":true},"policy_version":"2026-11","age_confirmed":true}`
	if rec := e.doInternal(workspacePath+"/consents", testInternalToken, body); rec.Code != http.StatusNoContent {
		e.t.Fatalf("record consent for %s: status = %d: %s", userID, rec.Code, rec.Body.String())
	}
}

func category(t *testing.T, req SubjectRequest, name string) ErasureCategory {
	t.Helper()
	for _, c := range req.Result {
		if c.Category == name {
			return c
		}
	}
	t.Fatalf("result has no %q category: %+v", name, req.Result)
	return ErasureCategory{}
}

// seedCampaignSend adds a campaign delivery record the way the campaigns service does.
func seedCampaignSend(t *testing.T, store Store, workspaceID, email string) {
	t.Helper()
	hash := privacy.EmailHash(workspaceID, email)
	switch s := store.(type) {
	case *MemoryStore:
		s.mu.Lock()
		defer s.mu.Unlock()
		s.campaignSends[workspaceID] = append(s.campaignSends[workspaceID], campaignSend{CampaignID: "welcome", EmailHash: hash, Status: "sent"})
	case *PostgresStore:
		err := s.inWorkspace(context.Background(), workspaceID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), "INSERT INTO campaign_sends (workspace_id, campaign_id, email_hash, status) VALUES ($1, 'welcome', $2, 'sent')", workspaceID, hash)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown store %T", store)
	}
}

// campaignRecords reports the delivery records and the suppression kept for an address.
func campaignRecords(t *testing.T, store Store, workspaceID, email string) (sends int, suppression string) {
	t.Helper()
	hash := privacy.EmailHash(workspaceID, email)
	switch s := store.(type) {
	case *MemoryStore:
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, send := range s.campaignSends[workspaceID] {
			if send.EmailHash == hash {
				sends++
			}
		}
		return sends, s.suppressions[workspaceID][hash]
	case *PostgresStore:
		err := s.inWorkspace(context.Background(), workspaceID, func(tx pgx.Tx) error {
			ctx := context.Background()
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM campaign_sends WHERE email_hash = $1", hash).Scan(&sends); err != nil {
				return err
			}
			err := tx.QueryRow(ctx, "SELECT reason FROM campaign_suppressions WHERE email_hash = $1", hash).Scan(&suppression)
			if err == pgx.ErrNoRows {
				return nil
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown store %T", store)
	}
	return sends, suppression
}

func TestEraseChatUserDeletesConsentProofAndQueuesTheRest(t *testing.T) {
	e := newTestEnv(t)
	const subject, other = "usr-4f9a1c7e2b", "usr-9d8e7f6a5b"
	e.consent(csInternal, subject)
	e.consent(csInternal, subject)
	e.consent(csInternal, other)

	rec := e.do(http.MethodPost, csRequests, "alice", `{"subject_kind":"chat_user","subject_id":"`+subject+`","verification":"Asked from the open chat session"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	req := decode[SubjectRequest](t, rec)
	if req.Status != "completed" || req.CompletedAt == nil || req.RequestedBy != "alice" || req.Source != "studio" {
		t.Errorf("request = %+v", req)
	}
	if got := category(t, req, "consent_records"); got.Action != "deleted" || got.Count != 2 {
		t.Errorf("consent_records = %+v, want 2 deleted", got)
	}
	// studio-api keeps no conversations, analytics or vectors: their owners are told to delete.
	for _, name := range []string{"conversation_sessions", "conversation_analytics", "knowledge_references"} {
		if got := category(t, req, name); got.Action != "queued" {
			t.Errorf("%s = %+v, want queued", name, got)
		}
	}

	// The ledger and the audit log name the person only by hash.
	ledger := e.do(http.MethodGet, csRequests, "alice", "")
	if ledger.Code != http.StatusOK || strings.Contains(ledger.Body.String(), subject) {
		t.Errorf("ledger (status %d) must not contain the conversation ID: %s", ledger.Code, ledger.Body.String())
	}
	if list := decode[[]SubjectRequest](t, ledger); len(list) != 1 || list[0].ID != req.ID || len(list[0].SubjectHash) != 64 {
		t.Errorf("ledger = %+v", list)
	}
	audited := false
	for _, entry := range e.auditLog() {
		if strings.Contains(entry.Action, subject) {
			t.Errorf("audit entry contains the conversation ID: %q", entry.Action)
		}
		if entry.Subject == "alice" && entry.Action == "data_subject.erased chat_user "+privacy.Short(req.SubjectHash) {
			audited = true
		}
	}
	if !audited {
		t.Errorf("erasure was not audited: %+v", e.auditLog())
	}

	// The engine, analytics and knowledge stores learn about it through the outbox.
	var event *OutboxEvent
	for _, evt := range e.outbox() {
		if evt.EventType == erasureEventType {
			event = evt
		}
	}
	if event == nil {
		t.Fatal("no erasure event in the outbox")
	}
	payload := event.Payload.(map[string]any)
	if payload["subject_id"] != subject || payload["channel"] != "webchat" || payload["workspace_id"] != "ws-customer-service" || payload["request_id"] != req.ID {
		t.Errorf("event payload = %+v", payload)
	}

	// Someone else's consent proof is untouched.
	rec = e.do(http.MethodPost, csRequests, "alice", `{"subject_kind":"chat_user","subject_id":"`+other+`","verification":"second request"}`)
	if got := category(t, decode[SubjectRequest](t, rec), "consent_records"); got.Count != 1 {
		t.Errorf("the other person's consent records = %+v, want 1 deleted", got)
	}
}

func TestEraseEmailPurgesCampaignRecordsInOneWorkspaceOnly(t *testing.T) {
	e := newTestEnv(t)
	const email = "ana.gomez@example.com"
	seedCampaignSend(t, e.store, "ws-customer-service", email)
	seedCampaignSend(t, e.store, "ws-customer-service", email)
	seedCampaignSend(t, e.store, "ws-customer-service", "luis@example.com")
	seedCampaignSend(t, e.store, "ws-hr", email)

	// The address is matched however the person typed it.
	rec := e.do(http.MethodPost, csRequests, "alice", `{"subject_kind":"email","subject_id":"  Ana.Gomez@Example.com ","verification":"Replied from that mailbox"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ana.gomez") {
		t.Errorf("response repeats the address: %s", rec.Body.String())
	}
	req := decode[SubjectRequest](t, rec)
	if got := category(t, req, "campaign_sends"); got.Action != "deleted" || got.Count != 2 {
		t.Errorf("campaign_sends = %+v, want 2 deleted", got)
	}
	if got := category(t, req, "campaign_suppression"); got.Action != "retained" {
		t.Errorf("campaign_suppression = %+v, want retained", got)
	}

	if sends, suppression := campaignRecords(t, e.store, "ws-customer-service", email); sends != 0 || suppression != "erasure" {
		t.Errorf("after erasure: %d sends, suppression %q; want 0 and erasure", sends, suppression)
	}
	if sends, _ := campaignRecords(t, e.store, "ws-customer-service", "luis@example.com"); sends != 1 {
		t.Errorf("another recipient lost %d records", 1-sends)
	}
	// Human resources is another area: its records about the same address are not Customer service's to delete.
	if sends, suppression := campaignRecords(t, e.store, "ws-hr", email); sends != 1 || suppression != "" {
		t.Errorf("ws-hr: %d sends, suppression %q; want its record untouched", sends, suppression)
	}

	for _, evt := range e.outbox() {
		if evt.EventType != erasureEventType {
			continue
		}
		raw, _ := json.Marshal(evt.Payload)
		if strings.Contains(string(raw), "ana.gomez") {
			t.Errorf("outbox event carries the plain address: %s", raw)
		}
		if evt.Payload.(map[string]any)["email_hash"] != privacy.EmailHash("ws-customer-service", email) {
			t.Errorf("outbox event = %s", raw)
		}
	}
}

func TestErasureNeedsVerificationAndAValidSubject(t *testing.T) {
	e := newTestEnv(t)
	for name, body := range map[string]string{
		"no verification": `{"subject_kind":"chat_user","subject_id":"usr-4f9a1c7e2b"}`,
		"blank":           `{"subject_kind":"chat_user","subject_id":"usr-4f9a1c7e2b","verification":"   "}`,
		"not an email":    `{"subject_kind":"email","subject_id":"ana","verification":"phone call"}`,
		"unknown kind":    `{"subject_kind":"employee","subject_id":"ana","verification":"phone call"}`,
		"no subject":      `{"subject_kind":"chat_user","subject_id":"","verification":"phone call"}`,
	} {
		if rec := e.do(http.MethodPost, csRequests, "alice", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	if list := decode[[]SubjectRequest](t, e.do(http.MethodGet, csRequests, "alice", "")); len(list) != 0 {
		t.Errorf("refused requests reached the ledger: %+v", list)
	}
}

func TestErasureStaysInsideTheWorkspace(t *testing.T) {
	e := newTestEnv(t)
	const subject = "usr-4f9a1c7e2b"
	e.consent(csInternal, subject)

	// bob owns Human resources. He cannot erase in Customer service, and erasing the same
	// conversation ID in his own area touches nothing of Customer service's.
	body := `{"subject_kind":"chat_user","subject_id":"` + subject + `","verification":"asked in person"}`
	if rec := e.do(http.MethodPost, csRequests, "bob", body); rec.Code != http.StatusNotFound {
		t.Fatalf("bob in Customer service: status = %d, want 404", rec.Code)
	}
	rec := e.do(http.MethodPost, "/workspaces/ws-hr/data-deletion-requests", "bob", body)
	if got := category(t, decode[SubjectRequest](t, rec), "consent_records"); got.Count != 0 {
		t.Errorf("ws-hr erasure deleted %d records that belong to Customer service", got.Count)
	}
	if list := decode[[]SubjectRequest](t, e.do(http.MethodGet, csRequests, "alice", "")); len(list) != 0 {
		t.Errorf("Customer service sees Human resources' requests: %+v", list)
	}
	rec = e.do(http.MethodPost, csRequests, "alice", body)
	if got := category(t, decode[SubjectRequest](t, rec), "consent_records"); got.Count != 1 {
		t.Errorf("Customer service's record: %+v, want 1 deleted", got)
	}
}

func TestInternalRoutesNeedTheServiceToken(t *testing.T) {
	e := newTestEnv(t)
	body := `{"channel":"webchat","subject_id":"usr-4f9a1c7e2b"}`
	path := csInternal + "/data-deletion-requests"

	for name, token := range map[string]string{"no token": "", "wrong token": "not-the-token", "a user's token": e.token("alice")} {
		if rec := e.doInternal(path, token, body); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
	}
	if rec := e.doInternal("/internal/workspaces/ws-nope/data-deletion-requests", testInternalToken, body); rec.Code != http.StatusNotFound {
		t.Errorf("unknown workspace: status = %d, want 404", rec.Code)
	}

	e.consent(csInternal, "usr-4f9a1c7e2b")
	rec := e.doInternal(path, testInternalToken, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	req := decode[SubjectRequest](t, rec)
	if req.Source != "webchat" || req.RequestedBy != "data-subject" || category(t, req, "consent_records").Count != 1 {
		t.Errorf("request = %+v", req)
	}
	audited := false
	for _, entry := range e.auditLog() {
		audited = audited || (entry.Subject == internalCaller && entry.WorkspaceID == "ws-customer-service" && strings.HasPrefix(entry.Action, "data_subject.erased chat_user "))
	}
	if !audited {
		t.Errorf("the gateway's erasure was not audited: %+v", e.auditLog())
	}

	// Without a configured token the routes do not exist: the request is just an unauthenticated call.
	bare := NewStudioHandler(e.store, Config{Verifier: e.issuer, PlatformAdminGroup: adminGroup})
	httpReq := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+testInternalToken)
	out := httptest.NewRecorder()
	bare.ServeHTTP(out, httpReq)
	if out.Code != http.StatusUnauthorized {
		t.Errorf("unconfigured: status = %d, want 401", out.Code)
	}
}

func TestConsentProofKeepsOnlyKnownPurposes(t *testing.T) {
	e := newTestEnv(t)
	if rec := e.doInternal(csInternal+"/consents", testInternalToken, `{"subject_id":"has spaces"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid subject: status = %d, want 400", rec.Code)
	}
	e.consent(csInternal, "usr-4f9a1c7e2b")
	var stored map[string]bool
	switch s := e.store.(type) {
	case *MemoryStore:
		stored = s.consents["ws-customer-service"][0].Purposes
	case *PostgresStore:
		var raw []byte
		err := s.inWorkspace(context.Background(), "ws-customer-service", func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT purposes FROM subject_consents").Scan(&raw)
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatal(err)
		}
	}
	if len(stored) != 2 || !stored["processing"] || stored["analytics"] {
		t.Errorf("stored purposes = %v, want only processing=true and analytics=false", stored)
	}
}

func TestStaffCanAskForTheirDataToBeDeleted(t *testing.T) {
	e := newTestEnv(t)
	rec := e.do(http.MethodPost, "/me/data-deletion-requests", "erin", `{"note":"Leaving the team"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	req := decode[StaffRequest](t, rec)
	if req.Status != "pending" || req.Subject != "erin" || req.Note != "Leaving the team" {
		t.Errorf("request = %+v", req)
	}
	// Asking twice does not open a second request.
	if again := decode[StaffRequest](t, e.do(http.MethodPost, "/me/data-deletion-requests", "erin", "")); again.ID != req.ID {
		t.Errorf("second request = %s, want the open one %s", again.ID, req.ID)
	}
	if mine := decode[[]StaffRequest](t, e.do(http.MethodGet, "/me/data-deletion-requests", "erin", "")); len(mine) != 1 {
		t.Errorf("erin sees %d requests, want 1", len(mine))
	}
	if others := decode[[]StaffRequest](t, e.do(http.MethodGet, "/me/data-deletion-requests", "carol", "")); len(others) != 0 {
		t.Errorf("carol sees erin's request: %+v", others)
	}

	execute := "/admin/data-deletion-requests/" + req.ID + "/execute"
	if rec := e.do(http.MethodPost, execute, "alice", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a workspace owner executed a staff request: status = %d, want 403", rec.Code)
	}
	if all := decode[[]StaffRequest](t, e.do(http.MethodGet, "/admin/data-deletion-requests", "dana", "")); len(all) != 1 {
		t.Errorf("admin sees %d requests, want 1", len(all))
	}
	rec = e.do(http.MethodPost, execute, "dana", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("execute: status = %d: %s", rec.Code, rec.Body.String())
	}
	done := decode[StaffRequest](t, rec)
	if done.Status != "completed" || done.ResolvedBy != "dana" || done.ResolvedAt == nil || done.Result[0].Category != "workspace_access" || done.Result[0].Count != 1 {
		t.Errorf("executed request = %+v", done)
	}
	if me := decode[struct{ Workspaces []workspaceAccess }](t, e.do(http.MethodGet, "/me", "erin", "")); len(me.Workspaces) != 0 {
		t.Errorf("erin still has access to %+v", me.Workspaces)
	}
	if rec := e.do(http.MethodPost, execute, "dana", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("running a completed request again: status = %d, want 400", rec.Code)
	}
	if rec := e.do(http.MethodPost, "/admin/data-deletion-requests/nope/execute", "dana", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown request: status = %d, want 404", rec.Code)
	}
}

func TestStaffErasureNeverLeavesAnAreaWithoutAnOwner(t *testing.T) {
	e := newTestEnv(t)
	req := decode[StaffRequest](t, e.do(http.MethodPost, "/me/data-deletion-requests", "alice", ""))
	rec := e.do(http.MethodPost, "/admin/data-deletion-requests/"+req.ID+"/execute", "dana", "")
	done := decode[StaffRequest](t, rec)
	if done.Status != "partially_completed" {
		t.Fatalf("status = %s, want partially_completed: %+v", done.Status, done)
	}
	retained := false
	for _, c := range done.Result {
		retained = retained || (c.Category == "workspace_access" && c.Action == "retained" && strings.Contains(c.Detail, "Customer service"))
	}
	if !retained {
		t.Errorf("result does not explain the kept ownership: %+v", done.Result)
	}
	if rec := e.do(http.MethodGet, "/workspaces/ws-customer-service/bots", "alice", ""); rec.Code != http.StatusOK {
		t.Errorf("the last owner lost access: status = %d", rec.Code)
	}

	// Once someone else owns the area, the same request can finish.
	if rec := e.do(http.MethodPut, "/workspaces/ws-customer-service/members/erin", "alice", `{"role":"owner"}`); rec.Code != http.StatusOK {
		t.Fatalf("promote erin: status = %d", rec.Code)
	}
	done = decode[StaffRequest](t, e.do(http.MethodPost, "/admin/data-deletion-requests/"+req.ID+"/execute", "dana", ""))
	if done.Status != "completed" {
		t.Errorf("second run: status = %s, want completed", done.Status)
	}
	if rec := e.do(http.MethodGet, "/workspaces/ws-customer-service/bots", "alice", ""); rec.Code != http.StatusNotFound {
		t.Errorf("alice still has access: status = %d", rec.Code)
	}
}

func TestTermsAcceptanceIsAudited(t *testing.T) {
	e := newTestEnv(t)
	if rec := e.do(http.MethodPost, "/me/terms-acceptance", "carol", `{"version":"2026-11"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodPost, "/me/terms-acceptance", "carol", `{"version":""}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty version: status = %d, want 400", rec.Code)
	}
	found := false
	for _, entry := range e.auditLog() {
		found = found || (entry.Subject == "carol" && entry.Action == "terms.accepted 2026-11")
	}
	if !found {
		t.Errorf("acceptance not in the audit log: %+v", e.auditLog())
	}
}

func TestBotsForMinorsNeedParentalConsentToPublish(t *testing.T) {
	e := newTestEnv(t)
	bot := "/workspaces/ws-customer-service/bots/retail-assistant"

	if got := decode[Bot](t, e.do(http.MethodGet, bot, "carol", "")); got.Audience != AudienceGeneral || got.MinAge != 0 {
		t.Errorf("default audience = %q, min_age = %d", got.Audience, got.MinAge)
	}
	for name, body := range map[string]string{"unknown audience": `{"audience":"everyone"}`, "age out of range": `{"audience":"minors","min_age":40}`} {
		if rec := e.do(http.MethodPatch, bot, "erin", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	rec := e.do(http.MethodPatch, bot, "erin", `{"audience":"minors","min_age":13}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode[Bot](t, rec); got.Audience != AudienceMinors || got.MinAge != 13 {
		t.Errorf("bot = %+v", got)
	}

	publish := bot + "/versions/v18/publish"
	rec = e.do(http.MethodPost, publish, "alice", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "parent or guardian") {
		t.Fatalf("publish without confirmation: status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(http.MethodPost, publish, "alice", `{"parental_consent_confirmed":true}`); rec.Code != http.StatusOK {
		t.Fatalf("publish with confirmation: status = %d: %s", rec.Code, rec.Body.String())
	}
	audited := false
	for _, entry := range e.auditLog() {
		audited = audited || entry.Action == "bot.published v18 audience=minors parental_consent_confirmed"
	}
	if !audited {
		t.Errorf("the confirmation is not in the audit log: %+v", e.auditLog())
	}
	// Channels learn the audience from the publish event, so they can show the age gate.
	for _, evt := range e.outbox() {
		if evt.EventType == "bot.published" {
			payload := evt.Payload.(map[string]any)
			if payload["audience"] != AudienceMinors || payload["min_age"] != float64(13) && payload["min_age"] != 13 {
				t.Errorf("publish event = %+v", payload)
			}
		}
	}
}

// tableDataClasses says, for every table, whether it holds data about a person and how
// an erasure request reaches it. A new table must be added here, which is the moment to
// decide whether EraseSubject or ExecuteStaffRequest has to cover it.
var tableDataClasses = map[string]string{
	"workspaces":             "no personal data",
	"workspace_members":      "staff: removed by ExecuteStaffRequest",
	"workspace_group_grants": "no personal data (groups, not people)",
	"bots":                   "no personal data (authored content)",
	"bot_versions":           "no personal data (authored content; the terms forbid personal data in flows)",
	"outbox_events":          "transient: erasure events carry a conversation ID until the relay publishes and deletes them",
	"audit_log":              "staff: retained, append-only (privacy policy, retention)",
	"schema_migrations":      "no personal data",
	"subject_consents":       "chat user: deleted by EraseSubject",
	"data_subject_requests":  "chat user or recipient: one-way hash only",
	"campaign_suppressions":  "recipient: one-way hash only, retained to honor the opt-out",
	"campaign_sends":         "recipient: deleted by EraseSubject",
	"staff_erasure_requests": "staff: the request itself, kept as proof it was handled",
}

func TestEveryTableIsClassifiedForErasure(t *testing.T) {
	files, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found: %v", err)
	}
	createTable := regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?(\w+)`)
	seen := map[string]bool{"schema_migrations": true}
	for _, name := range files {
		sql, err := migrationFiles.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range createTable.FindAllStringSubmatch(string(sql), -1) {
			table := match[1]
			seen[table] = true
			if tableDataClasses[table] == "" {
				t.Errorf("%s creates table %q, which is not in tableDataClasses: decide whether it holds personal data and how erasure reaches it", name, table)
			}
		}
	}
	for table := range tableDataClasses {
		if !seen[table] {
			t.Errorf("tableDataClasses lists %q, which no migration creates", table)
		}
	}
}
