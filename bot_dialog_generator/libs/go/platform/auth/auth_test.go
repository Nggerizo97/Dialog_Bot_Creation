package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func newTestIssuer(t *testing.T) *DevIssuer {
	t.Helper()
	iss, err := NewDevIssuer("studio-api")
	if err != nil {
		t.Fatalf("NewDevIssuer: %v", err)
	}
	return iss
}

func TestDevIssuerRoundTrip(t *testing.T) {
	iss := newTestIssuer(t)
	token, err := iss.Mint("alice", []string{"cs-team", "bdg-platform-admins"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	claims, err := iss.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("subject = %q, want alice", claims.Subject)
	}
	if !slices.Equal(claims.Groups, []string{"cs-team", "bdg-platform-admins"}) {
		t.Errorf("groups = %v", claims.Groups)
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	iss := newTestIssuer(t)
	now := time.Now()
	valid := func(overrides map[string]any) map[string]any {
		c := map[string]any{"iss": DevIssuerURL, "sub": "alice", "aud": "studio-api", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
		for k, v := range overrides {
			c[k] = v
		}
		return c
	}
	mint := func(claims map[string]any) string {
		token, err := iss.sign(claims)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return token
	}
	other := newTestIssuer(t)
	foreign, _ := other.Mint("alice", nil)
	good := mint(valid(nil))
	parts := strings.Split(good, ".")
	tampered := parts[0] + "." + strings.TrimRight(parts[1], "A") + "B." + parts[2]

	cases := map[string]string{
		"expired":        mint(valid(map[string]any{"exp": now.Add(-time.Minute).Unix()})),
		"wrong audience": mint(valid(map[string]any{"aud": "another-api"})),
		"wrong issuer":   mint(valid(map[string]any{"iss": "https://evil.example.com"})),
		"no subject":     mint(valid(map[string]any{"sub": ""})),
		"other key":      foreign,
		"tampered":       tampered,
		"garbage":        "not-a-jwt",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := iss.Verify(context.Background(), token); err == nil {
				t.Fatal("expected verification to fail")
			}
		})
	}
}

func TestMiddleware(t *testing.T) {
	iss := newTestIssuer(t)
	token, _ := iss.Mint("alice", []string{"cs-team"})
	var seen *Claims
	handler := Middleware(iss, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	for name, header := range map[string]string{
		"missing": "",
		"basic":   "Basic YWxpY2U6c2VjcmV0",
		"empty":   "Bearer ",
		"garbage": "Bearer abc.def.ghi",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/bots", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate header")
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/bots", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid token: status = %d, want 204", rec.Code)
	}
	if seen == nil || seen.Subject != "alice" {
		t.Fatalf("handler did not receive claims: %+v", seen)
	}
}

// TestOIDCVerifierDiscovery exercises the production path: issuer discovery, remote JWKS,
// key IDs and a provider-specific groups claim.
func TestOIDCVerifierDiscovery(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})

	v, err := NewOIDCVerifier(context.Background(), srv.URL, "studio-api", "cognito:groups")
	if err != nil {
		t.Fatalf("NewOIDCVerifier: %v", err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token, err := jwt.Signed(signer).Claims(map[string]any{
		"iss": srv.URL, "sub": "bob", "aud": "studio-api", "email": "bob@example.com",
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(), "cognito:groups": []string{"hr-team"},
	}).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	claims, err := v.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "bob" || claims.Email != "bob@example.com" || !slices.Equal(claims.Groups, []string{"hr-team"}) {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestNewOIDCVerifierRequiresConfig(t *testing.T) {
	if _, err := NewOIDCVerifier(context.Background(), "", "studio-api", ""); err == nil {
		t.Error("expected error without issuer")
	}
	if _, err := NewOIDCVerifier(context.Background(), "https://issuer.example.com", "", ""); err == nil {
		t.Error("expected error without audience")
	}
}
