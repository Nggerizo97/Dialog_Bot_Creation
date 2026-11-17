package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func send(handler http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body)))
	return rec
}

func TestEventsNeedTheAnalyticsOptIn(t *testing.T) {
	counter := NewCounter()
	handler := NewHandler(counter)
	for name, body := range map[string]string{
		"no consent field": `{"source":"webchat","event":"message_sent"}`,
		"declined":         `{"source":"webchat","event":"message_sent","consent":{"analytics":false}}`,
		"campaigns only":   `{"source":"webchat","event":"message_sent","consent":{"campaigns":true}}`,
	} {
		if rec := send(handler, body); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, rec.Code)
		}
	}
	if len(counter.Totals()) != 0 {
		t.Errorf("events without opt-in were counted: %v", counter.Totals())
	}
	if rec := send(handler, `{"source":"webchat","event":"message_sent","consent":{"analytics":true}}`); rec.Code != http.StatusAccepted {
		t.Fatalf("opted in: status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := counter.Totals()["webchat/message_sent"]; got != 1 {
		t.Errorf("total = %d, want 1", got)
	}
}

func TestWhatPeopleTypeCannotBeIngested(t *testing.T) {
	counter := NewCounter()
	handler := NewHandler(counter)
	consent := `"consent":{"analytics":true,"campaigns":true}`
	for name, body := range map[string]string{
		"a text field":               `{"source":"webchat","event":"message_sent","text":"my card is 4111 1111 1111 1111",` + consent + `}`,
		"a user id":                  `{"source":"webchat","event":"message_sent","user_id":"usr-4f9a1c7e2b",` + consent + `}`,
		"free text as a property":    `{"source":"webchat","event":"message_sent","props":{"channel":"I need help with my loan"},` + consent + `}`,
		"an email as a property":     `{"source":"webchat","event":"message_sent","props":{"utm_source":"ana@example.com"},` + consent + `}`,
		"a property not on the list": `{"source":"webchat","event":"message_sent","props":{"message":"hello"},` + consent + `}`,
		"an unknown event":           `{"source":"webchat","event":"transcript_uploaded",` + consent + `}`,
		"an unknown source":          `{"source":"crm","event":"message_sent",` + consent + `}`,
		"a sentence as workspace":    `{"source":"studio","event":"tab_viewed","workspace_id":"Customer service team",` + consent + `}`,
	} {
		if rec := send(handler, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	if len(counter.Totals()) != 0 {
		t.Errorf("refused events were counted: %v", counter.Totals())
	}
}

func TestCampaignPropertiesNeedTheirOwnOptIn(t *testing.T) {
	counter := NewCounter()
	handler := NewHandler(counter)
	props := `"props":{"channel":"webchat","utm_campaign":"spring"}`

	if rec := send(handler, `{"source":"webchat","event":"chat_opened","consent":{"analytics":true},`+props+`}`); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	totals := counter.Totals()
	if totals["webchat/chat_opened/channel=webchat"] != 1 || totals["webchat/chat_opened/utm_campaign=spring"] != 0 {
		t.Errorf("without the campaigns opt-in: %v", totals)
	}

	if rec := send(handler, `{"source":"webchat","event":"chat_opened","consent":{"analytics":true,"campaigns":true},`+props+`}`); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := counter.Totals()["webchat/chat_opened/utm_campaign=spring"]; got != 1 {
		t.Errorf("with the campaigns opt-in: utm_campaign total = %d, want 1", got)
	}
}

func TestIngestIsPostOnlyAndHealthStillWorks(t *testing.T) {
	handler := NewHandler(NewCounter())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/events", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /events: status = %d, want 405", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/events", nil))
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight: status = %d, headers = %v", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("/livez: status = %d, want 204", rec.Code)
	}
}
