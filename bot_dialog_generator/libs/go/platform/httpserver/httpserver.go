package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// CheckFunc defines a dependency health check with context.
type CheckFunc func(ctx context.Context) error

type dependencyCheck struct {
	name  string
	check CheckFunc
}

// ServerConfig configures the HTTP server instance.
type ServerConfig struct {
	name         string
	readyTimeout time.Duration
	readyChecks  []dependencyCheck
}

// Option configures the ServerConfig.
type Option func(*ServerConfig)

// WithReadyCheck adds a dependency readiness probe.
func WithReadyCheck(name string, check CheckFunc) Option {
	return func(cfg *ServerConfig) {
		cfg.readyChecks = append(cfg.readyChecks, dependencyCheck{name: name, check: check})
	}
}

// WithReadyTimeout sets the max duration for all readiness checks to complete.
func WithReadyTimeout(timeout time.Duration) Option {
	return func(cfg *ServerConfig) {
		if timeout > 0 {
			cfg.readyTimeout = timeout
		}
	}
}

// New creates an http.Handler with /livez, /readyz, and service root.
// /livez is process-only and returns 204 when the HTTP process is alive.
// /readyz executes registered dependency checks with bounded timeouts.
func New(name string, opts ...Option) http.Handler {
	cfg := &ServerConfig{
		name:         name,
		readyTimeout: 3 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	mux := http.NewServeMux()

	// /livez: process-level liveness probe
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// /readyz: dependency readiness probe
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if len(cfg.readyChecks) == 0 {
			// No external dependencies configured yet; acknowledge live process
			w.WriteHeader(http.StatusNoContent)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), cfg.readyTimeout)
		defer cancel()

		var wg sync.WaitGroup
		failures := make(map[string]string)
		var mu sync.Mutex

		for _, check := range cfg.readyChecks {
			wg.Add(1)
			go func(c dependencyCheck) {
				defer wg.Done()
				if err := c.check(ctx); err != nil {
					mu.Lock()
					failures[c.name] = err.Error()
					mu.Unlock()
				}
			}(check)
		}
		wg.Wait()

		if len(failures) > 0 {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":     "https://bot-dialog-generator.example.com/errors/dependency-unhealthy",
				"title":    "Dependency Readiness Check Failed",
				"status":   http.StatusServiceUnavailable,
				"detail":   fmt.Sprintf("%d readiness checks failed", len(failures)),
				"failures": failures,
			})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})

	// Service root info
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"service": cfg.name})
	})

	return mux
}
