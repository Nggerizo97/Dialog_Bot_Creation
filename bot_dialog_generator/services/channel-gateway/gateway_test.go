package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChannelGatewayWebchatIngressWelcome(t *testing.T) {
	client := &LocalEngineClient{}
	handler := NewGatewayHandler(client)

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
	handler := NewGatewayHandler(client)

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
	expected := "Your current checking balance is $1,250.50 USD."
	if outbound.Messages[0].Text != expected {
		t.Errorf("expected '%s', got '%s'", expected, outbound.Messages[0].Text)
	}
}

func TestChannelGatewayAdvisorOffersAIOrContactCenter(t *testing.T) {
	handler := NewGatewayHandler(&LocalEngineClient{})
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
	if len(out.Messages) != 1 || out.Messages[0].Text != "Connecting you with an available agent from the contact center now..." {
		t.Errorf("contact_center: unexpected reply %+v", out.Messages)
	}
}
