package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
)

// ProblemDetails represents an RFC 9457 Problem Details object.
type ProblemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, title, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ProblemDetails{
		Type:     "https://bot-dialog-generator.example.com/errors/studio-api",
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// NewStudioHandler returns the HTTP handler with all Studio routes mounted.
func NewStudioHandler(store Store) http.Handler {
	base := httpserver.New("studio-api")
	mux := http.NewServeMux()

	// Mount health endpoints from base
	mux.Handle("/livez", base)
	mux.Handle("/readyz", base)

	// Bot and version handlers
	mux.HandleFunc("/bots", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			tenant := r.Header.Get("X-Tenant-ID")
			bots, err := store.ListBots(tenant)
			if err != nil {
				writeProblem(w, http.StatusInternalServerError, "Internal Error", err.Error(), r.URL.Path)
				return
			}
			writeJSON(w, http.StatusOK, bots)
			return
		}
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Use GET /bots", r.URL.Path)
	})

	// /bots/...
	mux.HandleFunc("/bots/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// parts: ["bots", "{botId}", ...]
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		botID := parts[1]

		if len(parts) == 2 {
			// /bots/{botId}
			if r.Method == http.MethodGet {
				bot, err := store.GetBot(botID)
				if err != nil {
					writeProblem(w, http.StatusNotFound, "Bot Not Found", err.Error(), r.URL.Path)
					return
				}
				writeJSON(w, http.StatusOK, bot)
				return
			}
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Unsupported method", r.URL.Path)
			return
		}

		// /bots/{botId}/active-version
		if len(parts) == 3 && parts[2] == "active-version" {
			if r.Method == http.MethodGet {
				ver, err := store.GetActiveVersion(botID)
				if err != nil {
					writeProblem(w, http.StatusNotFound, "Not Found", err.Error(), r.URL.Path)
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"bot_id": botID, "active_version": ver})
				return
			}
			writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed", "Unsupported method", r.URL.Path)
			return
		}

		// /bots/{botId}/versions...
		if len(parts) >= 3 && parts[2] == "versions" {
			if len(parts) == 3 {
				// /bots/{botId}/versions
				if r.Method == http.MethodGet {
					list, err := store.ListVersions(botID)
					if err != nil {
						writeProblem(w, http.StatusNotFound, "Bot Not Found", err.Error(), r.URL.Path)
						return
					}
					writeJSON(w, http.StatusOK, list)
					return
				}
				if r.Method == http.MethodPost {
					newDraft, err := store.CreateDraft(botID, "")
					if err != nil {
						writeProblem(w, http.StatusBadRequest, "Create Draft Error", err.Error(), r.URL.Path)
						return
					}
					writeJSON(w, http.StatusCreated, newDraft)
					return
				}
			}

			if len(parts) == 4 {
				// /bots/{botId}/versions/{versionId}
				versionID := parts[3]
				if r.Method == http.MethodGet {
					ver, err := store.GetVersion(botID, versionID)
					if err != nil {
						writeProblem(w, http.StatusNotFound, "Version Not Found", err.Error(), r.URL.Path)
						return
					}
					writeJSON(w, http.StatusOK, ver)
					return
				}
				if r.Method == http.MethodPut {
					var incoming Version
					if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
						writeProblem(w, http.StatusBadRequest, "Invalid JSON", err.Error(), r.URL.Path)
						return
					}
					incoming.BotID = botID
					incoming.Version = versionID
					if err := store.SaveDraft(&incoming); err != nil {
						if err == ErrImmutable {
							writeProblem(w, http.StatusBadRequest, "Immutable Version", err.Error(), r.URL.Path)
							return
						}
						writeProblem(w, http.StatusInternalServerError, "Save Error", err.Error(), r.URL.Path)
						return
					}
					writeJSON(w, http.StatusOK, incoming)
					return
				}
			}

			if len(parts) == 5 && parts[4] == "publish" {
				// POST /bots/{botId}/versions/{versionId}/publish
				versionID := parts[3]
				if r.Method == http.MethodPost {
					artifact, err := store.PublishVersion(botID, versionID)
					if err != nil {
						writeProblem(w, http.StatusBadRequest, "Publish Failed", err.Error(), r.URL.Path)
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{
						"bot_id":       botID,
						"version":      versionID,
						"status":       "published",
						"sha256":       artifact.SHA256,
						"artifact_uri": fmt.Sprintf("s3://bot-dialog-generator-definitions/demo/%s/%s.pb", botID, versionID),
						"is_active":    true,
					})
					return
				}
			}
		}

		http.NotFound(w, r)
	})

	return corsMiddleware(mux)
}
