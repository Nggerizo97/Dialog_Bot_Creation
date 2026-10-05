package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// DevIssuerURL is the issuer of tokens minted by DevIssuer.
const DevIssuerURL = "urn:bot-dialog-generator:dev"

// DevIssuer signs real RS256 tokens with a key that lives only in memory, so local
// development exercises the same verification path as production. Anyone who can reach
// the minting endpoint can become anyone: never enable it outside local development.
type DevIssuer struct {
	audience string
	signer   jose.Signer
	verifier *oidcVerifier
	ttl      time.Duration
}

// NewDevIssuer creates an issuer with a fresh signing key for tokens addressed to audience.
func NewDevIssuer(audience string) (*DevIssuer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("auth: generate dev signing key: %w", err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return nil, fmt.Errorf("auth: create dev signer: %w", err)
	}
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}
	return &DevIssuer{
		audience: audience,
		signer:   signer,
		verifier: newVerifier(oidc.NewVerifier(DevIssuerURL, keySet, &oidc.Config{ClientID: audience}), "groups"),
		ttl:      8 * time.Hour,
	}, nil
}

// Mint returns a signed token for subject with the given groups.
func (d *DevIssuer) Mint(subject string, groups []string) (string, error) {
	now := time.Now()
	return d.sign(map[string]any{
		"iss":    DevIssuerURL,
		"sub":    subject,
		"aud":    d.audience,
		"iat":    now.Unix(),
		"exp":    now.Add(d.ttl).Unix(),
		"groups": groups,
	})
}

func (d *DevIssuer) sign(claims map[string]any) (string, error) {
	return jwt.Signed(d.signer).Claims(claims).Serialize()
}

// Verify checks a token minted by this issuer.
func (d *DevIssuer) Verify(ctx context.Context, rawToken string) (*Claims, error) {
	return d.verifier.Verify(ctx, rawToken)
}
