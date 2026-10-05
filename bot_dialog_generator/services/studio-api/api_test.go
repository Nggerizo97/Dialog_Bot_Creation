package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStudioAPIListBots(t *testing.T) {
	store := NewMemoryStore()
	handler := NewStudioHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/bots", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var bots []Bot
	if err := json.Unmarshal(rec.Body.Bytes(), &bots); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(bots) == 0 {
		t.Fatal("expected at least one bot in store")
	}
	if bots[0].ID != "retail-assistant" {
		t.Errorf("expected retail-assistant bot, got %s", bots[0].ID)
	}
}

func TestStudioAPIGetDraftVersion(t *testing.T) {
	store := NewMemoryStore()
	handler := NewStudioHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/bots/retail-assistant/versions/v18", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var ver Version
	if err := json.Unmarshal(rec.Body.Bytes(), &ver); err != nil {
		t.Fatalf("failed to decode version: %v", err)
	}
	if ver.Version != "v18" {
		t.Errorf("expected version v18, got %s", ver.Version)
	}
	if len(ver.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(ver.Nodes))
	}
}

func TestStudioAPIPublishDraft(t *testing.T) {
	store := NewMemoryStore()
	handler := NewStudioHandler(store)

	// Publish v18
	req := httptest.NewRequest(http.MethodPost, "/bots/retail-assistant/versions/v18/publish", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	if result["status"] != "published" {
		t.Errorf("expected status published, got %v", result["status"])
	}

	// Verify active version updated
	activeVer, err := store.GetActiveVersion("retail-assistant")
	if err != nil || activeVer != "v18" {
		t.Errorf("expected active version v18, got %s", activeVer)
	}

	// Verify outbox record created
	outbox := store.GetOutboxEvents()
	if len(outbox) == 0 {
		t.Fatal("expected at least one outbox event")
	}
	if outbox[0].EventType != "bot.published" {
		t.Errorf("expected bot.published event, got %s", outbox[0].EventType)
	}

	// Attempting to modify a published version should fail with Problem Details
	putReq := httptest.NewRequest(http.MethodPut, "/bots/retail-assistant/versions/v18", strings.NewReader(`{"nodes":[]}`))
	putRec := httptest.NewRecorder()
	handler.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for published version modification, got %d", putRec.Code)
	}
	contentType := putRec.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Errorf("expected application/problem+json, got %s", contentType)
	}
}
