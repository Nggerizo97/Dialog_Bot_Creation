package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
)

// The in-memory side of privacy.go. It behaves like privacy_postgres.go so one test
// suite covers both.

func (m *MemoryStore) UpdateBotSettings(_ context.Context, workspaceID, botID string, settings BotSettings) (*Bot, error) {
	if err := settings.validate(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bot, ok := m.botIn(workspaceID, botID)
	if !ok {
		return nil, ErrNotFound
	}
	bot.Audience, bot.MinAge = settings.Audience, settings.MinAge
	bot.UpdatedAt = time.Now().UTC()
	return m.botCopy(bot), nil
}

func (m *MemoryStore) RecordConsent(_ context.Context, workspaceID string, consent ConsentRecord) error {
	if err := consent.normalize(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return ErrNotFound
	}
	m.consents[workspaceID] = append(m.consents[workspaceID], consent)
	return nil
}

// EraseSubject deletes what the workspace holds about one person, queues the erasure
// event for the stores outside this service, and writes the ledger entry.
func (m *MemoryStore) EraseSubject(_ context.Context, workspaceID string, in ErasureInput) (*SubjectRequest, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	req := newSubjectRequest(workspaceID, in, now)

	var consents, sends int
	switch in.Kind {
	case SubjectChatUser:
		before := len(m.consents[workspaceID])
		m.consents[workspaceID] = slices.DeleteFunc(m.consents[workspaceID], func(c ConsentRecord) bool {
			return c.Channel == in.Channel && c.SubjectID == in.SubjectID
		})
		consents = before - len(m.consents[workspaceID])
	case SubjectEmail:
		hash := privacy.EmailHash(workspaceID, in.SubjectID)
		before := len(m.campaignSends[workspaceID])
		m.campaignSends[workspaceID] = slices.DeleteFunc(m.campaignSends[workspaceID], func(s campaignSend) bool {
			return s.EmailHash == hash
		})
		sends = before - len(m.campaignSends[workspaceID])
		if m.suppressions[workspaceID] == nil {
			m.suppressions[workspaceID] = make(map[string]string)
		}
		if _, suppressed := m.suppressions[workspaceID][hash]; !suppressed {
			m.suppressions[workspaceID][hash] = "erasure"
		}
	}
	req.Result = erasureResult(in, consents, sends)

	m.outboxEvents = append(m.outboxEvents, &OutboxEvent{
		ID: fmt.Sprintf("evt-%d", len(m.outboxEvents)+1), EventType: erasureEventType,
		Payload: erasureEvent(req, in), Status: "pending", CreatedAt: now,
	})
	m.subjectRequests = append(m.subjectRequests, *req)
	return req, nil
}

func (m *MemoryStore) ListSubjectRequests(_ context.Context, workspaceID string) ([]SubjectRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return nil, ErrNotFound
	}
	list := []SubjectRequest{}
	for _, req := range m.subjectRequests {
		if req.WorkspaceID == workspaceID {
			list = append(list, req)
		}
	}
	slices.Reverse(list) // newest first
	return list, nil
}

// CreateStaffRequest records a member of staff asking for their data to be deleted.
// Asking again while a request is open returns the open request.
func (m *MemoryStore) CreateStaffRequest(_ context.Context, subject, note string) (*StaffRequest, error) {
	note, err := cleanNote(note)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.staffRequests {
		if existing.Subject == subject && existing.Status != "completed" {
			copyReq := *existing
			return &copyReq, nil
		}
	}
	req := &StaffRequest{ID: newID(), Subject: subject, Note: note, Status: "pending", Result: []ErasureCategory{}, CreatedAt: time.Now().UTC()}
	m.staffRequests = append(m.staffRequests, req)
	copyReq := *req
	return &copyReq, nil
}

// ListStaffRequests returns one person's requests, or everyone's when subject is empty.
func (m *MemoryStore) ListStaffRequests(_ context.Context, subject string) ([]StaffRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := []StaffRequest{}
	for _, req := range m.staffRequests {
		if subject == "" || req.Subject == subject {
			list = append(list, *req)
		}
	}
	slices.Reverse(list)
	return list, nil
}

// ExecuteStaffRequest removes the person's direct access to every area, except where
// they are the last owner. Their requests that are already completed cannot run again.
func (m *MemoryStore) ExecuteStaffRequest(_ context.Context, requestID, executedBy string) (*StaffRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := slices.IndexFunc(m.staffRequests, func(r *StaffRequest) bool { return r.ID == requestID })
	if idx < 0 {
		return nil, ErrNotFound
	}
	req := m.staffRequests[idx]
	if req.Status == "completed" {
		return nil, errRequestCompleted
	}
	removed := 0
	var kept []string
	for workspaceID, members := range m.members {
		role, ok := members[req.Subject]
		if !ok {
			continue
		}
		if m.losesLastOwner(workspaceID, role, "") {
			kept = append(kept, m.workspaces[workspaceID].Name)
			continue
		}
		delete(members, req.Subject)
		removed++
	}
	slices.SortFunc(kept, strings.Compare)
	now := time.Now().UTC()
	req.Result, req.Status = staffResult(removed, kept)
	req.ResolvedAt, req.ResolvedBy = &now, executedBy
	copyReq := *req
	return &copyReq, nil
}

var errRequestCompleted = fmt.Errorf("%w: this request is already completed", ErrInvalid)
