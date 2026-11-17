package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/auth"
	"github.com/jackc/pgx/v5"
)

const devAudience = "studio-api"

// minInternalTokenLen keeps a guessable service token from being configured by mistake.
const minInternalTokenLen = 32

func main() {
	ctx := context.Background()
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrations(ctx, os.Getenv("DATABASE_ADMIN_URL")); err != nil {
			log.Fatal(err)
		}
		return
	}
	cfg, mode, err := loadConfig(ctx, os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	if mode == "dev" {
		log.Println("WARNING: AUTH_MODE=dev. POST /dev/token mints a token for anyone. Local development only.")
	}
	store, storeName, err := openStore(ctx, os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	handler := NewStudioHandler(store, cfg)
	log.Printf("bot_dialog_generator studio-api listening on :8080 (auth: %s, store: %s)", mode, storeName)
	log.Fatal(http.ListenAndServe(":8080", handler))
}

// runMigrations applies the schema. DATABASE_ADMIN_URL must be the schema owner, not
// the application role that studio-api runs as.
func runMigrations(ctx context.Context, adminURL string) error {
	if adminURL == "" {
		return fmt.Errorf("migrate: set DATABASE_ADMIN_URL to the schema owner's connection string")
	}
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	defer conn.Close(ctx)
	if err := Migrate(ctx, conn); err != nil {
		return err
	}
	log.Println("migrations applied")
	return nil
}

// openStore uses PostgreSQL when DATABASE_URL or DB_SECRET_ID is set, and the in-memory
// demo store otherwise. SEED_DEMO=true loads the demo organization into an empty database;
// it is refused outside local development, like development sign-in.
func openStore(ctx context.Context, getenv func(string) string) (Store, string, error) {
	databaseURL, err := resolveDatabaseURLFromSecret(ctx, getenv, nil)
	if err != nil {
		return nil, "", err
	}
	if databaseURL == "" {
		return NewMemoryStore(), "memory (data is lost on restart)", nil
	}
	seed := strings.EqualFold(getenv("SEED_DEMO"), "true")
	if appEnv := strings.ToLower(getenv("APP_ENV")); seed && appEnv != "" && appEnv != "local" && appEnv != "dev" {
		return nil, "", fmt.Errorf("SEED_DEMO is only allowed when APP_ENV is local or dev (APP_ENV=%s)", appEnv)
	}
	store, err := NewPostgresStore(ctx, databaseURL)
	if err != nil {
		return nil, "", err
	}
	if seed {
		seeded, err := store.SeedDemoIfEmpty(ctx)
		if err != nil {
			store.Close()
			return nil, "", fmt.Errorf("seed demo data: %w", err)
		}
		if seeded {
			log.Println("loaded the demo organization into the empty database")
		}
	}
	return store, "postgres", nil
}

// loadConfig reads configuration from the environment:
//
//	APP_ENV               local (default), dev, staging, production, ...
//	AUTH_MODE             dev, oidc or cognito; defaults to dev only when APP_ENV is local or dev
//	OIDC_ISSUER           issuer URL (oidc mode)
//	OIDC_AUDIENCE         expected token audience (oidc mode)
//	OIDC_GROUPS_CLAIM     claim listing the caller's groups (default "groups")
//	OIDC_SUBJECT_CLAIM    claim identifying the caller (default "sub"; "oid" for Entra ID)
//	COGNITO_USER_POOL_ID  user pool (cognito mode)
//	COGNITO_REGION        pool's region (cognito mode; default AWS_REGION)
//	COGNITO_CLIENT_IDS    comma-separated app clients whose access tokens are accepted (cognito mode)
//	COGNITO_ISSUER        issuer override, for the AWS Emulator: https://127.0.0.1:9297/<pool> (cognito mode)
//	OIDC_CA_FILE          extra CA certificate to trust for discovery, e.g. the emulator's ca.pem (default AWS_CA_BUNDLE)
//	PLATFORM_ADMIN_GROUP  group whose members are platform admins (default "bdg-platform-admins")
//	CORS_ALLOWED_ORIGINS  comma-separated browser origins (default the local Vite ports)
//	INTERNAL_API_TOKEN    shared secret for the channel gateway's /internal calls; unset disables them
//	DATABASE_URL          PostgreSQL as the application role; unset uses the in-memory store
//	SEED_DEMO             "true" loads the demo organization into an empty database (local only)
//	DATABASE_ADMIN_URL    schema owner, used only by "studio-api migrate"
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
		InternalToken:      getenv("INTERNAL_API_TOKEN"),
	}
	if cfg.InternalToken != "" && len(cfg.InternalToken) < minInternalTokenLen {
		return Config{}, "", fmt.Errorf("INTERNAL_API_TOKEN must be at least %d characters; generate a random one", minInternalTokenLen)
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
		v, err := auth.NewOIDCVerifier(ctx, auth.OIDCConfig{
			Issuer:       getenv("OIDC_ISSUER"),
			Audience:     getenv("OIDC_AUDIENCE"),
			GroupsClaim:  getenv("OIDC_GROUPS_CLAIM"),
			SubjectClaim: getenv("OIDC_SUBJECT_CLAIM"),
		})
		if err != nil {
			return Config{}, "", err
		}
		cfg.Verifier = v
	case "cognito":
		v, err := cognitoVerifier(ctx, getenv)
		if err != nil {
			return Config{}, "", err
		}
		cfg.Verifier = v
	default:
		return Config{}, "", fmt.Errorf("unknown AUTH_MODE %q (use dev, oidc or cognito)", mode)
	}
	artifacts, err := openArtifactPublisherFromEnv(ctx, getenv)
	if err != nil {
		return Config{}, "", err
	}
	cfg.Artifacts = artifacts
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

// cognitoVerifier verifies access tokens from an Amazon Cognito user pool, or from a
// user pool in the AWS Emulator when COGNITO_ISSUER and OIDC_CA_FILE point at it.
// Groups come from cognito:groups.
func cognitoVerifier(ctx context.Context, getenv func(string) string) (auth.Verifier, error) {
	cfg := auth.CognitoConfig{
		Region:       valueOr(getenv("COGNITO_REGION"), getenv("AWS_REGION")),
		UserPoolID:   getenv("COGNITO_USER_POOL_ID"),
		Issuer:       getenv("COGNITO_ISSUER"),
		ClientIDs:    splitList(getenv("COGNITO_CLIENT_IDS")),
		SubjectClaim: getenv("OIDC_SUBJECT_CLAIM"),
	}
	if caFile := valueOr(getenv("OIDC_CA_FILE"), getenv("AWS_CA_BUNDLE")); caFile != "" {
		client, err := auth.HTTPClientWithCA(caFile)
		if err != nil {
			return nil, err
		}
		cfg.HTTPClient = client
	}
	return auth.NewCognitoVerifier(ctx, cfg)
}
