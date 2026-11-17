package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

// issuerOf reads the "iss" claim of a token the pool just issued. The token is not
// verified here: it came straight from the pool over TLS, and studio-api verifies
// every token it receives.
func issuerOf(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("the pool returned a token that is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Issuer == "" {
		return "", errors.New("the pool's token has no issuer")
	}
	return claims.Issuer, nil
}
