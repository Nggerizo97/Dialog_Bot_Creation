package main

import (
	"context"
	"slices"
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
