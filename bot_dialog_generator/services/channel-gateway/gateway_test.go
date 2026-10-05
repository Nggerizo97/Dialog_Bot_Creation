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
