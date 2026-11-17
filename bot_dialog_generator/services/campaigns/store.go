package main

import (
	"context"
	"sync"
)

// Reasons an address is on the do-not-contact list.
const (
	ReasonUnsubscribe = "unsubscribe"
	ReasonErasure     = "erasure"
	ReasonBounce      = "bounce"
	ReasonComplaint   = "complaint"
)

// Outcomes recorded for every recipient a campaign considered.
const (
	StatusSent              = "sent"
	StatusSkippedSuppressed = "skipped_suppressed"
	StatusSkippedNoConsent  = "skipped_no_consent"
	StatusFailed            = "failed"
)

// Store keeps the do-not-contact list and the delivery log. Both are keyed by a
// one-way hash of the address (privacy.EmailHash), never by the address itself, and
// both belong to one workspace.
type Store interface {
	// IsSuppressed reports whether the address may no longer be contacted.
	IsSuppressed(ctx context.Context, workspaceID, emailHash string) (bool, error)
	// Suppress adds the address to the do-not-contact list. Adding it twice is fine;
	// nothing in this service removes an entry.
	Suppress(ctx context.Context, workspaceID, emailHash, reason string) error
	RecordSend(ctx context.Context, workspaceID, campaignID, emailHash, status string) error
}

// MemoryStore is for local development and tests: it forgets every unsubscribe when
// the process stops, so main refuses to use it outside local environments.
type MemoryStore struct {
	mu           sync.RWMutex
	suppressions map[string]string // workspaceID + "\x00" + emailHash -> reason
	sends        []SendRecord
}

// SendRecord is one line of the delivery log.
type SendRecord struct {
	WorkspaceID, CampaignID, EmailHash, Status string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{suppressions: make(map[string]string)}
}

func suppressionKey(workspaceID, emailHash string) string { return workspaceID + "\x00" + emailHash }

func (m *MemoryStore) IsSuppressed(_ context.Context, workspaceID, emailHash string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.suppressions[suppressionKey(workspaceID, emailHash)]
	return ok, nil
}

func (m *MemoryStore) Suppress(_ context.Context, workspaceID, emailHash, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := suppressionKey(workspaceID, emailHash)
	if _, ok := m.suppressions[key]; !ok {
		m.suppressions[key] = reason
	}
	return nil
}

func (m *MemoryStore) RecordSend(_ context.Context, workspaceID, campaignID, emailHash, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, SendRecord{WorkspaceID: workspaceID, CampaignID: campaignID, EmailHash: emailHash, Status: status})
	return nil
}

// Sends returns a copy of the delivery log.
func (m *MemoryStore) Sends() []SendRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]SendRecord(nil), m.sends...)
}
