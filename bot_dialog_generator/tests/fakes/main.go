package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Mock Channel delivery endpoint (e.g. outbound webhooks)
	mux.HandleFunc("/mock/channel/delivery", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "delivered",
			"timestamp": "2026-10-04T00:00:00Z",
		})
	})

	// Mock Accounts API (for service nodes)
	mux.HandleFunc("/mock/accounts/balance", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"account_id": "acc-12345",
			"balance":    1250.50,
			"currency":   "USD",
		})
	})

	// Fallback catch-all echo
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"fake":    "bot-dialog-generator-fakes",
			"path":    r.URL.Path,
			"method":  r.Method,
			"message": "Bot_Dialog_Generator local fake service ready",
		})
	})

	addr := fmt.Sprintf(":%s", port)
	log.Printf("bot-dialog-generator-fakes listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
