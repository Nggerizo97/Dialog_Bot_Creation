package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/auth"
)

const devAudience = "studio-api"

func main() {
	cfg, mode, err := loadConfig(context.Background(), os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	if mode == "dev" {
		log.Println("WARNING: AUTH_MODE=dev. POST /dev/token mints a token for anyone. Local development only.")
	}
	handler := NewStudioHandler(NewMemoryStore(), cfg)
	log.Printf("bot_dialog_generator studio-api listening on :8080 (auth: %s)", mode)
	log.Fatal(http.ListenAndServe(":8080", handler))
}

// loadConfig reads configuration from the environment:
//
//	APP_ENV               local (default), dev, staging, production, ...
//	AUTH_MODE             dev or oidc; defaults to dev only when APP_ENV is local or dev
//	OIDC_ISSUER           issuer URL (oidc mode)
//	OIDC_AUDIENCE         expected token audience (oidc mode)
//	OIDC_GROUPS_CLAIM     claim listing the caller's groups (default "groups")
//	PLATFORM_ADMIN_GROUP  group whose members are platform admins (default "bdg-platform-admins")
//	CORS_ALLOWED_ORIGINS  comma-separated browser origins (default the local Vite ports)
func loadConfig(ctx context.Context, getenv func(string) string) (Config, string, error) {
	appEnv := strings.ToLower(getenv("APP_ENV"))
	local := appEnv == "" || appEnv == "local" || appEnv == "dev"
	mode := strings.ToLower(getenv("AUTH_MODE"))
	if mode == "" {
		mode = "oidc"
		if local {
			mode = "dev"
		}
	}
	cfg := Config{
		PlatformAdminGroup: valueOr(getenv("PLATFORM_ADMIN_GROUP"), "bdg-platform-admins"),
		AllowedOrigins:     splitList(valueOr(getenv("CORS_ALLOWED_ORIGINS"), "http://localhost:5173,http://localhost:5174")),
	}
	switch mode {
	case "dev":
		if !local {
			return Config{}, "", fmt.Errorf("AUTH_MODE=dev is only allowed when APP_ENV is local or dev (APP_ENV=%s)", appEnv)
		}
		issuer, err := auth.NewDevIssuer(devAudience)
		if err != nil {
			return Config{}, "", err
		}
		cfg.Verifier, cfg.DevIssuer = issuer, issuer
	case "oidc":
		v, err := auth.NewOIDCVerifier(ctx, getenv("OIDC_ISSUER"), getenv("OIDC_AUDIENCE"), getenv("OIDC_GROUPS_CLAIM"))
		if err != nil {
			return Config{}, "", err
		}
		cfg.Verifier = v
	default:
		return Config{}, "", fmt.Errorf("unknown AUTH_MODE %q (use dev or oidc)", mode)
	}
	return cfg, mode, nil
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
