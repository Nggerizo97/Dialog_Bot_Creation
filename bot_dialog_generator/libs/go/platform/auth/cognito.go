package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Amazon Cognito user pools issue OIDC tokens, but their access tokens differ from
// what NewOIDCVerifier expects:
//
//   - there is no "aud" claim; the app client is in "client_id";
//   - "token_use" says whether a token is an access token or an ID token, and an ID
//     token must never be accepted where an access token is expected;
//   - groups are in "cognito:groups".
//
// NewCognitoVerifier checks the signature against the pool's JWKS, the issuer and the
// expiry like any OIDC token, then checks token_use and client_id itself.

// CognitoConfig configures verification of access tokens from one user pool.
type CognitoConfig struct {
	// Region and UserPoolID give the issuer https://cognito-idp.<region>.amazonaws.com/<pool>.
	Region     string
	UserPoolID string
	// Issuer overrides the issuer URL. Set it for the AWS Emulator, whose user pools
	// issue tokens as https://127.0.0.1:9297/<pool>.
	Issuer string
	// ClientIDs are the app clients whose tokens are accepted (the studio's client).
	ClientIDs []string
	// SubjectClaim identifies the caller: "sub" (default; a UUID that never changes) or
	// "username" (the sign-in name, readable in the studio's member lists).
	SubjectClaim string
	// HTTPClient fetches discovery and keys. Use HTTPClientWithCA for the emulator.
	HTTPClient *http.Client
}

// CognitoIssuer is the issuer URL of a user pool on AWS.
func CognitoIssuer(region, userPoolID string) string {
	return fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID)
}

// ErrWrongTokenUse means an ID token (or another token) was sent instead of an access token.
var ErrWrongTokenUse = errors.New("auth: send the Cognito access token, not the ID token")

type cognitoVerifier struct {
	verifier     *oidc.IDTokenVerifier
	clientIDs    []string
	subjectClaim string
}

// NewCognitoVerifier discovers the pool's signing keys and verifies its access tokens.
func NewCognitoVerifier(ctx context.Context, cfg CognitoConfig) (Verifier, error) {
	issuer := cfg.Issuer
	if issuer == "" {
		if cfg.Region == "" || cfg.UserPoolID == "" {
			return nil, errors.New("auth: Cognito needs a region and user pool ID, or an issuer URL")
		}
		issuer = CognitoIssuer(cfg.Region, cfg.UserPoolID)
	}
	if len(cfg.ClientIDs) == 0 {
		return nil, errors.New("auth: Cognito needs at least one app client ID")
	}
	if cfg.SubjectClaim == "" {
		cfg.SubjectClaim = "sub"
	}
	if cfg.SubjectClaim != "sub" && cfg.SubjectClaim != "username" {
		return nil, fmt.Errorf("auth: Cognito subject claim must be sub or username, not %q", cfg.SubjectClaim)
	}
	if cfg.HTTPClient != nil {
		ctx = oidc.ClientContext(ctx, cfg.HTTPClient)
	}
	provider, err := oidc.NewProvider(ctx, strings.TrimRight(issuer, "/"))
	if err != nil {
		return nil, fmt.Errorf("auth: discover Cognito issuer %s: %w", issuer, err)
	}
	return &cognitoVerifier{
		// The client is checked against client_id below: access tokens have no "aud".
		verifier:     provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		clientIDs:    cfg.ClientIDs,
		subjectClaim: cfg.SubjectClaim,
	}, nil
}

func (v *cognitoVerifier) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	token, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	var all map[string]any
	if err := token.Claims(&all); err != nil {
		return nil, fmt.Errorf("auth: read claims: %w", err)
	}
	if use, _ := all["token_use"].(string); use != "access" {
		return nil, ErrWrongTokenUse
	}
	if clientID, _ := all["client_id"].(string); !slices.Contains(v.clientIDs, clientID) {
		return nil, fmt.Errorf("auth: token was issued to app client %q, which is not accepted", clientID)
	}
	subject, _ := all[v.subjectClaim].(string)
	if subject == "" {
		return nil, fmt.Errorf("auth: token has no %q claim", v.subjectClaim)
	}
	claims := &Claims{Subject: subject}
	// Access tokens carry no email; the username is what people recognize.
	if email, _ := all["email"].(string); email != "" {
		claims.Email = email
	} else if username, _ := all["username"].(string); username != "" && v.subjectClaim != "username" {
		claims.Email = username
	}
	if groups, ok := all["cognito:groups"].([]any); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				claims.Groups = append(claims.Groups, s)
			}
		}
	}
	return claims, nil
}

// HTTPClientWithCA returns a client that trusts the system certificates plus the ones
// in caFile, for example the AWS Emulator's ca.pem. TLS verification stays on.
func HTTPClientWithCA(caFile string) (*http.Client, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("auth: read CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("auth: %s contains no PEM certificate", caFile)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}
