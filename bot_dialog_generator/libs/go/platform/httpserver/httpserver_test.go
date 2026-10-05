package httpserver_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
)

func TestLivezAlwaysReturns204(t *testing.T) {
	handler := httpserver.New("test-service")
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
}

func TestReadyzDefaultWithoutChecksReturns204(t *testing.T) {
	handler := httpserver.New("test-service")
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
}

func TestReadyzWithPassingChecksReturns204(t *testing.T) {
	handler := httpserver.New(
		"test-service",
		httpserver.WithReadyCheck("database", func(ctx context.Context) error {
			return nil
		}),
		httpserver.WithReadyCheck("valkey", func(ctx context.Context) error {
			return nil
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
}

func TestReadyzWithFailingCheckReturns503(t *testing.T) {
	handler := httpserver.New(
		"test-service",
		httpserver.WithReadyCheck("database", func(ctx context.Context) error {
			return errors.New("connection refused")
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/problem+json" {
		t.Fatalf("expected application/problem+json, got %s", contentType)
	}
}

func TestReadyzTimeoutReturns503(t *testing.T) {
	handler := httpserver.New(
		"test-service",
		httpserver.WithReadyTimeout(50*time.Millisecond),
		httpserver.WithReadyCheck("slow-dep", func(ctx context.Context) error {
			select {
			case <-time.After(200 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}
}

func TestServiceRoot(t *testing.T) {
	handler := httpserver.New("conversation-engine")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
