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

// ErrGroupsOverage means the identity provider left the caller's groups out of the
// token because there were too many. Microsoft Entra ID does this above 200 groups
// unless the app registration emits only the groups assigned to the application.
var ErrGroupsOverage = errors.New("auth: token omits the caller's groups (group overage)")

// OIDCConfig configures token verification for one identity provider.
type OIDCConfig struct {
	Issuer   string // issuer URL, e.g. https://login.microsoftonline.com/<tenant-id>/v2.0
	Audience string // expected "aud": for Entra ID v2 access tokens, the app's client ID
	// GroupsClaim lists the caller's groups: "groups" (default) for Entra ID,
	// "cognito:groups" for Cognito.
	GroupsClaim string
	// SubjectClaim identifies the caller: "sub" (default). For Entra ID use "oid", the
	// user's object ID, which admins can look up; Entra's "sub" differs per app.
	SubjectClaim string
}

type oidcVerifier struct {
	verifier     *oidc.IDTokenVerifier
	groupsClaim  string
	subjectClaim string
}

// NewOIDCVerifier discovers the issuer's signing keys and verifies tokens issued for
// the configured audience.
func NewOIDCVerifier(ctx context.Context, cfg OIDCConfig) (Verifier, error) {
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, errors.New("auth: OIDC issuer and audience are required")
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover issuer %s: %w", cfg.Issuer, err)
	}
	return newVerifier(provider.Verifier(&oidc.Config{ClientID: cfg.Audience}), cfg), nil
}

func newVerifier(v *oidc.IDTokenVerifier, cfg OIDCConfig) *oidcVerifier {
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	if cfg.SubjectClaim == "" {
		cfg.SubjectClaim = "sub"
	}
	return &oidcVerifier{verifier: v, groupsClaim: cfg.GroupsClaim, subjectClaim: cfg.SubjectClaim}
}

func (v *oidcVerifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	var all map[string]any
	if err := token.Claims(&all); err != nil {
		return nil, fmt.Errorf("auth: read claims: %w", err)
	}
	subject, _ := all[v.subjectClaim].(string)
	if subject == "" {
		return nil, fmt.Errorf("auth: token has no %q claim", v.subjectClaim)
	}
	claims := &Claims{Subject: subject}
	// Entra ID access tokens carry preferred_username (usually the work email) instead of email.
	for _, name := range []string{"email", "preferred_username"} {
		if value, ok := all[name].(string); ok && value != "" {
			claims.Email = value
			break
		}
	}
	if names, ok := all["_claim_names"].(map[string]any); ok && all[v.groupsClaim] == nil {
		if _, overage := names[v.groupsClaim]; overage {
			return nil, ErrGroupsOverage
		}
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
		if errors.Is(err, ErrGroupsOverage) {
			unauthorized(w, r, "Your account is in too many groups for the sign-in token. Ask IT to set the studio app to send only the groups assigned to it.")
			return
		}
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
