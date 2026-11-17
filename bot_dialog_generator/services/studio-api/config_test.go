package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoadConfigDefaultsToDevLocally(t *testing.T) {
	cfg, mode, err := loadConfig(context.Background(), env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if mode != "dev" || cfg.DevIssuer == nil || cfg.Verifier == nil {
		t.Fatalf("mode = %s, dev issuer set = %v", mode, cfg.DevIssuer != nil)
	}
	if cfg.PlatformAdminGroup != "bdg-platform-admins" {
		t.Errorf("admin group = %q", cfg.PlatformAdminGroup)
	}
	if !slices.Equal(cfg.AllowedOrigins, []string{"http://localhost:5173", "http://localhost:5174"}) {
		t.Errorf("origins = %v", cfg.AllowedOrigins)
	}
}

func TestLoadConfigRefusesDevAuthOutsideLocal(t *testing.T) {
	for _, appEnv := range []string{"production", "staging", "qa"} {
		if _, _, err := loadConfig(context.Background(), env(map[string]string{"APP_ENV": appEnv, "AUTH_MODE": "dev"})); err == nil {
			t.Errorf("APP_ENV=%s: dev auth was allowed", appEnv)
		}
	}
}

func TestLoadConfigRequiresOIDCSettingsOutsideLocal(t *testing.T) {
	if _, _, err := loadConfig(context.Background(), env(map[string]string{"APP_ENV": "production"})); err == nil {
		t.Fatal("production without OIDC_ISSUER/OIDC_AUDIENCE must fail")
	}
}

func TestLoadConfigRejectsUnknownMode(t *testing.T) {
	if _, _, err := loadConfig(context.Background(), env(map[string]string{"AUTH_MODE": "header"})); err == nil {
		t.Fatal("unknown AUTH_MODE accepted")
	}
}

func TestLoadConfigParsesOrigins(t *testing.T) {
	cfg, _, err := loadConfig(context.Background(), env(map[string]string{"CORS_ALLOWED_ORIGINS": " https://studio.example.com , ,http://localhost:3000"}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.AllowedOrigins, []string{"https://studio.example.com", "http://localhost:3000"}) {
		t.Errorf("origins = %v", cfg.AllowedOrigins)
	}
}

func TestOpenStore(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	store, name, err := openStore(context.Background(), env(nil))
	if err != nil || store == nil || !strings.HasPrefix(name, "memory") {
		t.Fatalf("without DATABASE_URL: store=%v name=%q err=%v", store, name, err)
	}
	// Demo data must never be loaded into a production database. The guard runs before
	// connecting, so the unreachable address is never dialed.
	_, _, err = openStore(context.Background(), env(map[string]string{
		"DATABASE_URL": "postgres://nobody@127.0.0.1:1/none?connect_timeout=1", "SEED_DEMO": "true", "APP_ENV": "production",
	}))
	if err == nil || !strings.Contains(err.Error(), "SEED_DEMO") {
		t.Fatalf("SEED_DEMO in production: err = %v, want the SEED_DEMO guard", err)
	}
}

// fakeCognitoIssuer serves a user pool's discovery document over HTTPS with its own
// certificate, like the AWS Emulator, and returns the issuer and the CA file to trust.
func fakeCognitoIssuer(t *testing.T) (issuer, caFile string) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL + "/us-east-1_LOCAL"
	mux.HandleFunc("/us-east-1_LOCAL/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/.well-known/jwks.json"})
	})
	caFile = filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return issuer, caFile
}

func TestLoadConfigCognito(t *testing.T) {
	issuer, caFile := fakeCognitoIssuer(t)
	vars := map[string]string{
		"APP_ENV": "production", "AUTH_MODE": "cognito", "COGNITO_ISSUER": issuer,
		"COGNITO_CLIENT_IDS": "studio-client", "OIDC_CA_FILE": caFile, "OIDC_SUBJECT_CLAIM": "username",
	}
	cfg, mode, err := loadConfig(context.Background(), env(vars))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if mode != "cognito" || cfg.Verifier == nil || cfg.DevIssuer != nil {
		t.Fatalf("mode = %s, verifier = %v, dev issuer = %v", mode, cfg.Verifier != nil, cfg.DevIssuer != nil)
	}

	// AWS_CA_BUNDLE, which the AWS SDKs read, works too.
	delete(vars, "OIDC_CA_FILE")
	vars["AWS_CA_BUNDLE"] = caFile
	if _, _, err := loadConfig(context.Background(), env(vars)); err != nil {
		t.Errorf("with AWS_CA_BUNDLE: %v", err)
	}
}

func TestLoadConfigCognitoNeedsItsSettings(t *testing.T) {
	issuer, caFile := fakeCognitoIssuer(t)
	base := map[string]string{"AUTH_MODE": "cognito", "COGNITO_ISSUER": issuer, "COGNITO_CLIENT_IDS": "studio-client", "OIDC_CA_FILE": caFile}
	for name, change := range map[string]map[string]string{
		"no app client":         {"COGNITO_CLIENT_IDS": ""},
		"no pool or issuer":     {"COGNITO_ISSUER": "", "COGNITO_USER_POOL_ID": ""},
		"pool without a region": {"COGNITO_ISSUER": "", "COGNITO_USER_POOL_ID": "us-east-1_abc"},
		"untrusted certificate": {"OIDC_CA_FILE": ""},
		"missing CA file":       {"OIDC_CA_FILE": filepath.Join(t.TempDir(), "nope.pem")},
		"email as the subject":  {"OIDC_SUBJECT_CLAIM": "email"},
		"unreachable issuer":    {"COGNITO_ISSUER": "https://127.0.0.1:1/us-east-1_abc"},
	} {
		vars := map[string]string{}
		for k, v := range base {
			vars[k] = v
		}
		for k, v := range change {
			vars[k] = v
		}
		if _, _, err := loadConfig(context.Background(), env(vars)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
