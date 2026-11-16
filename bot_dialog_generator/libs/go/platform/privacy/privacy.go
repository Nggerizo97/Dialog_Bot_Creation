// Package privacy holds the small helpers every service uses to refer to a person
// without keeping more about them than it needs.
//
// The hashes here are pseudonyms, not anonymous data: emails and chat IDs can be
// guessed and re-hashed by anyone who knows the workspace ID. They exist so that
// suppression lists, request ledgers and logs never hold the readable value.
package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// NormalizeEmail lower-cases and trims an address so one mailbox always hashes the
// same way. It reports false when the value cannot be an email address.
func NormalizeEmail(email string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || !strings.Contains(domain, ".") || len(email) > 254 || strings.ContainsAny(email, " \t\r\n<>,;") {
		return "", false
	}
	return email, true
}

// EmailHash is the one-way reference to an email address inside one workspace. The
// workspace ID is part of the hash, so the same person cannot be matched across areas.
func EmailHash(workspaceID, normalizedEmail string) string {
	return hash("email", workspaceID, normalizedEmail)
}

// SubjectHash is the one-way reference to any data subject (a chat conversation ID, an
// email address, a staff account) kept in request ledgers and audit entries.
func SubjectHash(workspaceID, kind, id string) string {
	return hash(kind, workspaceID, id)
}

func hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Short returns the first 12 characters of a hash: enough to find a ledger entry,
// too little to be used as the identifier itself.
func Short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
