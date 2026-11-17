package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeCognito serves a user pool's discovery document and JWKS over HTTPS with its own
// certificate, like the AWS Emulator does, and signs tokens shaped like Cognito's.
type fakeCognito struct {
	issuer string
	caFile string
	sign   func(claims map[string]any) string
}

func newFakeCognito(t *testing.T) *fakeCognito {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	issuer := srv.URL + "/us-east-1_TESTPOOL"
	mux.HandleFunc("/us-east-1_TESTPOOL/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "jwks_uri": issuer + "/.well-known/jwks.json", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/us-east-1_TESTPOOL/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "pool-key", Algorithm: "RS256", Use: "sig"}}})
	})
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "pool-key"}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	return &fakeCognito{issuer: issuer, caFile: caFile, sign: func(claims map[string]any) string {
		now := time.Now()
		base := map[string]any{"iss": issuer, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
		for k, v := range claims {
			base[k] = v
		}
		token, err := jwt.Signed(signer).Claims(base).Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return token
	}}
}

// accessToken has the claims a Cognito access token carries (no "aud", no email).
func accessToken(overrides map[string]any) map[string]any {
	claims := map[string]any{
		"sub": "bc8c224f-e23b-4c41-9f1c-d8aefb7b8ab6", "username": "dana", "client_id": "studio-client",
		"token_use": "access", "scope": "aws.cognito.signin.user.admin", "cognito:groups": []string{"bdg-platform-admins", "hr-team"},
	}
	for k, v := range overrides {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	return claims
}

func newCognitoVerifier(t *testing.T, pool *fakeCognito, subjectClaim string) Verifier {
	t.Helper()
	client, err := HTTPClientWithCA(pool.caFile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewCognitoVerifier(context.Background(), CognitoConfig{
		Issuer: pool.issuer, ClientIDs: []string{"studio-client"}, SubjectClaim: subjectClaim, HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("NewCognitoVerifier: %v", err)
	}
	return v
}

func TestCognitoVerifierAcceptsAccessTokens(t *testing.T) {
	pool := newFakeCognito(t)

	claims, err := newCognitoVerifier(t, pool, "").Verify(context.Background(), pool.sign(accessToken(nil)))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "bc8c224f-e23b-4c41-9f1c-d8aefb7b8ab6" || claims.Email != "dana" || !slices.Equal(claims.Groups, []string{"bdg-platform-admins", "hr-team"}) {
		t.Errorf("claims = %+v", claims)
	}

	// With subject claim "username", memberships can name people as they sign in.
	claims, err = newCognitoVerifier(t, pool, "username").Verify(context.Background(), pool.sign(accessToken(nil)))
	if err != nil || claims.Subject != "dana" {
		t.Errorf("username subject: claims = %+v, err = %v", claims, err)
	}
}

func TestCognitoVerifierRejectsWhatItShould(t *testing.T) {
	pool := newFakeCognito(t)
	other := newFakeCognito(t)
	v := newCognitoVerifier(t, pool, "")

	idToken := accessToken(map[string]any{"token_use": "id", "aud": "studio-client", "client_id": nil, "email": "dana@example.com"})
	cases := map[string]string{
		"an ID token instead of the access token": pool.sign(idToken),
		"another app client":                      pool.sign(accessToken(map[string]any{"client_id": "someone-elses-client"})),
		"no client":                               pool.sign(accessToken(map[string]any{"client_id": nil})),
		"no token_use":                            pool.sign(accessToken(map[string]any{"token_use": nil})),
		"expired":                                 pool.sign(accessToken(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})),
		"another pool's issuer":                   pool.sign(accessToken(map[string]any{"iss": pool.issuer + "x"})),
		"signed by another pool":                  other.sign(accessToken(map[string]any{"iss": pool.issuer})),
		"no subject":                              pool.sign(accessToken(map[string]any{"sub": nil})),
	}
	for name, token := range cases {
		if _, err := v.Verify(context.Background(), token); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := v.Verify(context.Background(), pool.sign(idToken)); !errors.Is(err, ErrWrongTokenUse) {
		t.Errorf("ID token: err = %v, want ErrWrongTokenUse", err)
	}
}

func TestCognitoVerifierKeepsTLSVerificationOn(t *testing.T) {
	pool := newFakeCognito(t)
	// Without the pool's certificate, discovery must fail instead of trusting anything.
	_, err := NewCognitoVerifier(context.Background(), CognitoConfig{Issuer: pool.issuer, ClientIDs: []string{"studio-client"}})
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Errorf("err = %v, want a certificate error", err)
	}
	if _, err := HTTPClientWithCA(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("a missing CA file was accepted")
	}
	empty := filepath.Join(t.TempDir(), "empty.pem")
	_ = os.WriteFile(empty, []byte("not a certificate"), 0o600)
	if _, err := HTTPClientWithCA(empty); err == nil {
		t.Error("a file without certificates was accepted")
	}
}

func TestCognitoConfigIsChecked(t *testing.T) {
	ctx := context.Background()
	if got := CognitoIssuer("us-east-1", "us-east-1_AbC"); got != "https://cognito-idp.us-east-1.amazonaws.com/us-east-1_AbC" {
		t.Errorf("CognitoIssuer = %s", got)
	}
	for name, cfg := range map[string]CognitoConfig{
		"no pool":           {Region: "us-east-1", ClientIDs: []string{"c"}},
		"no client":         {Issuer: "https://127.0.0.1:1/pool"},
		"bad subject claim": {Issuer: "https://127.0.0.1:1/pool", ClientIDs: []string{"c"}, SubjectClaim: "email"},
	} {
		if _, err := NewCognitoVerifier(ctx, cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
