// Package auth verifies OIDC bearer tokens and exposes the caller's identity to HTTP handlers.
//
// Identity comes only from a verified token. No request header other than
// Authorization is ever treated as a source of identity.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Claims is the verified identity of a caller.
type Claims struct {
	Subject string
	Email   string
	Groups  []string
}

// Verifier checks a raw bearer token and returns the identity it carries.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (*Claims, error)
}

type oidcVerifier struct {
	verifier    *oidc.IDTokenVerifier
	groupsClaim string
}

// NewOIDCVerifier discovers the issuer's signing keys and verifies tokens issued for audience.
// groupsClaim names the claim that lists the caller's groups ("groups" for Entra ID,
// "cognito:groups" for Cognito); it defaults to "groups".
func NewOIDCVerifier(ctx context.Context, issuer, audience, groupsClaim string) (Verifier, error) {
	if issuer == "" || audience == "" {
		return nil, errors.New("auth: OIDC issuer and audience are required")
	}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover issuer %s: %w", issuer, err)
	}
	return newVerifier(provider.Verifier(&oidc.Config{ClientID: audience}), groupsClaim), nil
}

func newVerifier(v *oidc.IDTokenVerifier, groupsClaim string) *oidcVerifier {
	if groupsClaim == "" {
		groupsClaim = "groups"
	}
	return &oidcVerifier{verifier: v, groupsClaim: groupsClaim}
}

func (v *oidcVerifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	if token.Subject == "" {
		return nil, errors.New("auth: token has no subject")
	}
	var all map[string]any
	if err := token.Claims(&all); err != nil {
		return nil, fmt.Errorf("auth: read claims: %w", err)
	}
	claims := &Claims{Subject: token.Subject}
	if email, ok := all["email"].(string); ok {
		claims.Email = email
	}
	switch groups := all[v.groupsClaim].(type) {
	case []any:
		for _, g := range groups {
			if s, ok := g.(string); ok {
				claims.Groups = append(claims.Groups, s)
			}
		}
	case string:
		claims.Groups = []string{groups}
	}
	return claims, nil
}

type claimsKey struct{}

// WithClaims returns a copy of ctx carrying claims.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// FromContext returns the verified claims stored by Middleware.
func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*Claims)
	return claims, ok
}

// Middleware rejects requests without a valid bearer token with 401 and stores the
// verified claims in the request context for next.
func Middleware(v Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r)
		if !ok {
			unauthorized(w, r, "Send a bearer token in the Authorization header.")
			return
		}
		claims, err := v.Verify(r.Context(), raw)
		if err != nil {
			unauthorized(w, r, "The bearer token is invalid or expired. Sign in again.")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="bot-dialog-generator"`)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":     "https://bot-dialog-generator.example.com/errors/unauthorized",
		"title":    "Unauthorized",
		"status":   http.StatusUnauthorized,
		"detail":   detail,
		"instance": r.URL.Path,
	})
}
