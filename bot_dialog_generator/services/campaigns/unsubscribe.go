package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
)

// Signer makes and checks unsubscribe tokens. A token names a workspace and the hash
// of one address, so the link in an email works without signing in and without the
// address ever appearing in a URL or a server log.
type Signer struct {
	key []byte
}

const minSigningKeyLen = 32

// NewSigner needs a secret of at least 32 characters. Changing it breaks the
// unsubscribe links in emails that were already sent, so rotate it with care.
func NewSigner(key string) (*Signer, error) {
	if len(key) < minSigningKeyLen {
		return nil, errors.New("UNSUBSCRIBE_SIGNING_KEY must be at least 32 characters")
	}
	return &Signer{key: []byte(key)}, nil
}

// UnsubscribeClaims is what a verified token says.
type UnsubscribeClaims struct {
	WorkspaceID, EmailHash, CampaignID string
}

var errBadToken = errors.New("unsubscribe token is not valid")

var tokenEncoding = base64.RawURLEncoding

func (s *Signer) mac(payload string) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(payload))
	return h.Sum(nil)
}

// Token returns the signed token for one recipient of one campaign. It never expires:
// an unsubscribe link has to keep working for as long as the email exists.
func (s *Signer) Token(workspaceID, emailHash, campaignID string) string {
	payload := strings.Join([]string{workspaceID, emailHash, campaignID}, "\x1f")
	return tokenEncoding.EncodeToString([]byte(payload)) + "." + tokenEncoding.EncodeToString(s.mac(payload))
}

// Verify returns the claims of a token this signer issued.
func (s *Signer) Verify(token string) (UnsubscribeClaims, error) {
	encodedPayload, encodedMAC, ok := strings.Cut(token, ".")
	if !ok {
		return UnsubscribeClaims{}, errBadToken
	}
	payload, err := tokenEncoding.DecodeString(encodedPayload)
	if err != nil {
		return UnsubscribeClaims{}, errBadToken
	}
	mac, err := tokenEncoding.DecodeString(encodedMAC)
	if err != nil || !hmac.Equal(mac, s.mac(string(payload))) {
		return UnsubscribeClaims{}, errBadToken
	}
	parts := strings.Split(string(payload), "\x1f")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return UnsubscribeClaims{}, errBadToken
	}
	return UnsubscribeClaims{WorkspaceID: parts[0], EmailHash: parts[1], CampaignID: parts[2]}, nil
}

var unsubscribePage = template.Must(template.New("unsubscribe").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}}</title>
<style>
body { margin: 0; padding: 48px 20px; color: #18201f; background: #edf0eb; font-family: system-ui, -apple-system, "Segoe UI", sans-serif; line-height: 1.5; }
main { max-width: 520px; margin: 0 auto; padding: 28px; background: #fffef9; border: 1px solid #a9b5ad; border-radius: 8px; }
h1 { margin: 0 0 12px; font-size: 22px; }
p { margin: 0 0 12px; }
.contact { color: #46524d; font-size: 14px; }
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p role="status">{{.Message}}</p>
{{if .Sender}}<p class="contact">{{.Sender}}</p>{{end}}
</main>
</body>
</html>
`))

type unsubscribeView struct {
	Title, Message, Sender string
}

// unsubscribe takes an address off every future campaign of its workspace.
//
//	POST /unsubscribe?token=…  is the RFC 8058 one-click request mail apps send for the
//	                           List-Unsubscribe header.
//	GET  /unsubscribe?token=…  is the link in the email body. It unsubscribes at once:
//	                           no sign-in, no form, no "are you sure".
func (s *Service) unsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	claims, err := s.signer.Verify(r.URL.Query().Get("token"))
	if err != nil {
		s.renderUnsubscribe(w, r, http.StatusBadRequest, "This unsubscribe link is not valid",
			"The link is incomplete or was changed. Open the link from the email again, or reply to the email and ask to be removed.")
		return
	}
	if err := s.store.Suppress(r.Context(), claims.WorkspaceID, claims.EmailHash, ReasonUnsubscribe); err != nil {
		log.Printf("unsubscribe: could not record the opt-out for workspace %s: %v", claims.WorkspaceID, err)
		s.renderUnsubscribe(w, r, http.StatusInternalServerError, "We could not unsubscribe you just now",
			"Please open the link again in a few minutes, or reply to the email and ask to be removed.")
		return
	}
	s.renderUnsubscribe(w, r, http.StatusOK, "You are unsubscribed",
		"We will not send campaign emails to this address again. You do not need to do anything else.")
}

func (s *Service) renderUnsubscribe(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	// Mail apps that POST the one-click request do not show a page: plain text is enough.
	if r.Method == http.MethodPost {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(title + ". " + message + "\n"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = unsubscribePage.Execute(w, unsubscribeView{Title: title, Message: message, Sender: s.sender.footerLine()})
}
