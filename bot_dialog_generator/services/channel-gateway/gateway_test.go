package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
)

func TestChannelGatewayWebchatIngressWelcome(t *testing.T) {
	client := &LocalEngineClient{}
	handler := NewGatewayHandler(client, testBinding)

	payload := WebchatInbound{
		Tenant:  "demo",
		Channel: "webchat",
		UserID:  "usr-test",
		Text:    "Hello",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/channels/webchat/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var outbound WebchatOutbound
	if err := json.Unmarshal(rec.Body.Bytes(), &outbound); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(outbound.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(outbound.Messages))
	}
	if outbound.Messages[0].Kind != "text" || outbound.Messages[0].Text != "Welcome to the Bot_Dialog_Generator demo!" {
		t.Errorf("unexpected first message: %+v", outbound.Messages[0])
	}
	if outbound.Messages[1].Kind != "menu" || len(outbound.Messages[1].Options) != 2 {
		t.Errorf("unexpected menu message: %+v", outbound.Messages[1])
	}
}

func TestChannelGatewayWebchatChoice(t *testing.T) {
	client := &LocalEngineClient{}
	handler := NewGatewayHandler(client, testBinding)

	payload := WebchatInbound{
		Tenant:  "demo",
		Channel: "webchat",
		UserID:  "usr-test",
		Choice:  "balance",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/channels/webchat/messages", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var outbound WebchatOutbound
	if err := json.Unmarshal(rec.Body.Bytes(), &outbound); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(outbound.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(outbound.Messages))
	}
	expected := demoBalanceReply
	if outbound.Messages[0].Text != expected {
		t.Errorf("expected '%s', got '%s'", expected, outbound.Messages[0].Text)
	}
}

func TestChannelGatewayAdvisorOffersAIOrContactCenter(t *testing.T) {
	handler := NewGatewayHandler(&LocalEngineClient{}, testBinding)
	send := func(choice string) WebchatOutbound {
		t.Helper()
		body, _ := json.Marshal(WebchatInbound{Tenant: "demo", Channel: "webchat", UserID: "usr-test", Choice: choice})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/channels/webchat/messages", bytes.NewReader(body)))
		if rec.Code != http.StatusOK {
			t.Fatalf("choice %q: expected status 200, got %d: %s", choice, rec.Code, rec.Body.String())
		}
		var out WebchatOutbound
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("choice %q: failed to decode response: %v", choice, err)
		}
		return out
	}
	optionIDs := func(m WebchatMessage) []string {
		var ids []string
		for _, o := range m.Options {
			ids = append(ids, o.ID)
		}
		return ids
	}

	// Asking for an advisor does not transfer straight away; the user chooses AI or a person.
	out := send("handoff")
	if len(out.Messages) != 1 || out.Messages[0].Kind != "menu" {
		t.Fatalf("handoff: expected one menu, got %+v", out.Messages)
	}
	if got := optionIDs(out.Messages[0]); len(got) != 2 || got[0] != "ai_assistant" || got[1] != "contact_center" {
		t.Errorf("handoff: expected options [ai_assistant contact_center], got %v", got)
	}

	// The AI assistant always keeps a way to reach a person.
	out = send("ai_assistant")
	if len(out.Messages) != 2 || out.Messages[1].Kind != "menu" {
		t.Fatalf("ai_assistant: expected text and menu, got %+v", out.Messages)
	}
	if got := optionIDs(out.Messages[1]); len(got) == 0 || got[0] != "contact_center" {
		t.Errorf("ai_assistant: expected a contact_center option first, got %v", got)
	}

	out = send("contact_center")
	if len(out.Messages) != 1 || out.Messages[0].Text != demoHandoffReply {
		t.Errorf("contact_center: unexpected reply %+v", out.Messages)
	}
}

var testBinding = WebchatBinding{WorkspaceID: "ws-customer-service"}

// recordingEngine keeps the last message the gateway forwarded.
type recordingEngine struct {
	LocalEngineClient
	last *botdialoggeneratorv1.InboundMessage
}

func (r *recordingEngine) ProcessMessage(in *botdialoggeneratorv1.InboundMessage) (*botdialoggeneratorv1.OutboundBatch, error) {
	r.last = in
	return r.LocalEngineClient.ProcessMessage(in)
}

func TestChannelGatewayTakesWorkspaceFromBindingNotRequest(t *testing.T) {
	engine := &recordingEngine{}
	handler := NewGatewayHandler(engine, testBinding)
	// A client trying to reach another area's bot by naming its workspace.
	body := `{"tenant":"demo","channel":"webchat","user_id":"usr-test","text":"Hello","workspace_id":"ws-hr"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/channels/webchat/messages", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := engine.last.GetWorkspaceId(); got != "ws-customer-service" {
		t.Fatalf("forwarded workspace_id = %q, want the binding's ws-customer-service", got)
	}
}

func post(handler http.Handler, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	return rec
}

func TestChannelGatewayRefusesMissingOrMeaningfulUserIDs(t *testing.T) {
	handler := NewGatewayHandler(&LocalEngineClient{}, testBinding)
	// No default ID: a shared one would put every anonymous visitor in one conversation.
	for name, body := range map[string]string{
		"missing":   `{"text":"Hello"}`,
		"an email":  `{"user_id":"ana.gomez@example.com","text":"Hello"}`,
		"too short": `{"user_id":"ana","text":"Hello"}`,
	} {
		if rec := post(handler, "/channels/webchat/messages", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	long := `{"user_id":"usr-test","text":"` + strings.Repeat("a", maxTextLen+1) + `"}`
	if rec := post(handler, "/channels/webchat/messages", long); rec.Code != http.StatusBadRequest {
		t.Errorf("oversized text: status = %d, want 400", rec.Code)
	}
}

func TestChannelGatewayForwardsCampaignContextOnlyWithOptIn(t *testing.T) {
	engine := &recordingEngine{}
	handler := NewGatewayHandler(engine, testBinding)
	context := `"context":{"utm_campaign":"spring","utm_source":"newsletter","email":"ana@example.com","referrer":"https://example.com/account/123"}`

	if rec := post(handler, "/channels/webchat/messages", `{"user_id":"usr-test","text":"Hello",`+context+`}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := engine.last.GetContext(); len(got) != 0 {
		t.Errorf("without the campaigns opt-in the engine received %v", got)
	}

	if rec := post(handler, "/channels/webchat/messages", `{"user_id":"usr-test","text":"Hello","consent":{"campaigns":true},`+context+`}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := engine.last.GetContext()
	if len(got) != 2 || got["utm_campaign"] != "spring" || got["utm_source"] != "newsletter" {
		t.Errorf("with the opt-in the engine received %v, want only the two campaign keys", got)
	}
}

type failingEngine struct{}

func (failingEngine) ProcessMessage(in *botdialoggeneratorv1.InboundMessage) (*botdialoggeneratorv1.OutboundBatch, error) {
	return nil, errors.New("cannot route message: " + in.GetText().GetValue())
}

func TestChannelGatewayDoesNotEchoWhatThePersonTypedInErrors(t *testing.T) {
	handler := NewGatewayHandler(failingEngine{}, testBinding)
	rec := post(handler, "/channels/webchat/messages", `{"user_id":"usr-test","text":"my card is 4111 1111 1111 1111"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "4111") {
		t.Errorf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
}

func TestChannelGatewayConsentAndErasureUseTheBindingWorkspace(t *testing.T) {
	local := NewLocalPrivacyClient()
	handler := NewGatewayHandler(&LocalEngineClient{}, testBinding, WithPrivacyClient(local))

	// Nothing is held yet: the receipt says so.
	rec := post(handler, "/channels/webchat/data-deletion", `{"user_id":"usr-test"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var receipt ErasureReceipt
	_ = json.Unmarshal(rec.Body.Bytes(), &receipt)
	if receipt.Status != "completed" || receipt.ServerCopy || receipt.Reference == "" {
		t.Errorf("receipt = %+v", receipt)
	}

	// A workspace_id in the request is ignored, like on the messages route.
	consent := `{"user_id":"usr-test","workspace_id":"ws-hr","purposes":{"processing":true,"analytics":false},"policy_version":"2026-11","age_confirmed":true}`
	if rec := post(handler, "/channels/webchat/consent", consent); rec.Code != http.StatusNoContent {
		t.Fatalf("consent: status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(local.consents["ws-customer-service:usr-test"]) != 1 || len(local.consents["ws-hr:usr-test"]) != 0 {
		t.Errorf("consent was not filed under the binding's workspace: %v", local.consents)
	}
	if stored := local.consents["ws-customer-service:usr-test"][0]; stored.Purposes["analytics"] || !stored.Purposes["processing"] || !stored.AgeConfirmed {
		t.Errorf("stored consent = %+v", stored)
	}

	rec = post(handler, "/channels/webchat/data-deletion", `{"user_id":"usr-test"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &receipt)
	if !receipt.ServerCopy || len(local.consents) != 0 {
		t.Errorf("after erasure: receipt = %+v, consents = %v", receipt, local.consents)
	}

	for _, path := range []string{"/channels/webchat/consent", "/channels/webchat/data-deletion"} {
		if rec := post(handler, path, `{"user_id":"x"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s with a bad user_id: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestStudioPrivacyClientCallsTheInternalRoutes(t *testing.T) {
	var calls []string
	studio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer shared-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, r.Method+" "+r.URL.Path+" "+body["subject_id"].(string))
		switch {
		case body["subject_id"] == "usr-rejected":
			w.WriteHeader(http.StatusBadRequest)
		case strings.HasSuffix(r.URL.Path, "/consents"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"req-1","status":"completed","result":[{"category":"consent_records","action":"deleted","count":2},{"category":"conversation_sessions","action":"queued","count":0}]}`))
		}
	}))
	defer studio.Close()

	client, err := NewStudioPrivacyClient(studio.URL+"/", "shared-secret")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.RecordConsent(ctx, "ws-customer-service", ConsentRequest{UserID: "usr-test"}); err != nil {
		t.Fatalf("RecordConsent: %v", err)
	}
	receipt, err := client.RequestErasure(ctx, "ws-customer-service", "usr-test")
	if err != nil || receipt.Reference != "req-1" || receipt.Status != "completed" || !receipt.ServerCopy {
		t.Errorf("receipt = %+v, err = %v", receipt, err)
	}
	want := []string{
		"POST /internal/workspaces/ws-customer-service/consents usr-test",
		"POST /internal/workspaces/ws-customer-service/data-deletion-requests usr-test",
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	if _, err := client.RequestErasure(ctx, "ws-customer-service", "usr-rejected"); !errors.Is(err, errPrivacyRejected) {
		t.Errorf("a 400 from studio-api: err = %v, want errPrivacyRejected", err)
	}

	wrongToken, _ := NewStudioPrivacyClient(studio.URL, "other-secret")
	if err := wrongToken.RecordConsent(ctx, "ws-customer-service", ConsentRequest{UserID: "usr-test"}); err == nil {
		t.Error("a refused token was reported as success")
	}
	if _, err := NewStudioPrivacyClient("studio-api:8080", "x"); err == nil {
		t.Error("an address without a scheme was accepted")
	}
	if _, err := NewStudioPrivacyClient(studio.URL, ""); err == nil {
		t.Error("a missing token was accepted")
	}
}
