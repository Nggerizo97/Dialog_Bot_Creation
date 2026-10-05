package main

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
)

var (
	ErrNotFound  = errors.New("resource not found")
	ErrConflict  = errors.New("resource conflict")
	ErrImmutable = errors.New("cannot modify published version")
)

type Bot struct {
	ID            string    `json:"id"`
	Tenant        string    `json:"tenant"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	ActiveVersion string    `json:"active_version,omitempty"`
	DraftVersion  string    `json:"draft_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Version struct {
	ID          string             `json:"id"`
	BotID       string             `json:"bot_id"`
	Version     string             `json:"version"`
	Status      string             `json:"status"` // draft, published, archived
	EntryNodeID string             `json:"entry_node_id"`
	Nodes       []botdef.DraftNode `json:"nodes"`
	Edges       []botdef.DraftEdge `json:"edges"`
	PublishedAt *time.Time         `json:"published_at,omitempty"`
	ArtifactURI string             `json:"artifact_uri,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

type OutboxEvent struct {
	ID        string    `json:"id"`
	EventType string    `json:"event_type"`
	Payload   any       `json:"payload"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Store defines persistence operations for Studio API.
type Store interface {
	GetBot(botID string) (*Bot, error)
	ListBots(tenant string) ([]*Bot, error)
	GetVersion(botID, versionID string) (*Version, error)
	ListVersions(botID string) ([]*Version, error)
	CreateDraft(botID, baseVersion string) (*Version, error)
	SaveDraft(version *Version) error
	PublishVersion(botID, versionID string) (*botdef.CompiledArtifact, error)
	GetActiveVersion(botID string) (string, error)
	SetActiveVersion(botID, version string) error
	GetOutboxEvents() []*OutboxEvent
}

// MemoryStore provides thread-safe in-memory storage seeded with default Retail Assistant.
type MemoryStore struct {
	mu           sync.RWMutex
	bots         map[string]*Bot
	versions     map[string]map[string]*Version // botID -> version -> Version
	activePtrs   map[string]string              // botID -> activeVersion
	outboxEvents []*OutboxEvent
}

func NewMemoryStore() *MemoryStore {
	now := time.Now().UTC()
	store := &MemoryStore{
		bots:       make(map[string]*Bot),
		versions:   make(map[string]map[string]*Version),
		activePtrs: make(map[string]string),
	}

	// Seed default Retail Assistant bot
	botID := "retail-assistant"
	store.bots[botID] = &Bot{
		ID:            botID,
		Tenant:        "demo",
		Name:          "Retail assistant",
		Description:   "Customer support bot for account services",
		DraftVersion:  "v18",
		ActiveVersion: "v17",
		CreatedAt:     now.Add(-24 * time.Hour),
		UpdatedAt:     now,
	}
	store.activePtrs[botID] = "v17"

	store.versions[botID] = make(map[string]*Version)
	store.versions[botID]["v18"] = &Version{
		ID:          "v18",
		BotID:       botID,
		Version:     "v18",
		Status:      "draft",
		EntryNodeID: "welcome",
		Nodes: []botdef.DraftNode{
			{ID: "welcome", Type: "Trigger", Title: "Welcome", Detail: "New conversation"},
			{ID: "menu", Type: "Menu", Title: "What can we help with?", Detail: "3 routes", Properties: map[string]string{"prompt": "What can we help you with today?"}},
			{ID: "balance", Type: "Service", Title: "Check balance", Detail: "Accounts API", Properties: map[string]string{"endpoint": "/mock/accounts/balance"}},
			{ID: "handoff", Type: "Response", Title: "Connect to advisor", Detail: "Text message", Properties: map[string]string{"message": "Please hold while we connect you to an advisor."}},
		},
		Edges: []botdef.DraftEdge{
			{From: "welcome", To: "menu"},
			{From: "menu", To: "balance", Condition: "balance"},
			{From: "menu", To: "handoff", Condition: "handoff"},
		},
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now,
	}

	return store
}

func (m *MemoryStore) GetBot(botID string) (*Bot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bots[botID]
	if !ok {
		return nil, ErrNotFound
	}
	copyBot := *b
	copyBot.ActiveVersion = m.activePtrs[botID]
	return &copyBot, nil
}

func (m *MemoryStore) ListBots(tenant string) ([]*Bot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Bot
	for _, b := range m.bots {
		if tenant == "" || b.Tenant == tenant {
			copyBot := *b
			copyBot.ActiveVersion = m.activePtrs[b.ID]
			result = append(result, &copyBot)
		}
	}
	return result, nil
}

func (m *MemoryStore) GetVersion(botID, versionID string) (*Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	botVers, ok := m.versions[botID]
	if !ok {
		return nil, ErrNotFound
	}
	ver, ok := botVers[versionID]
	if !ok {
		return nil, ErrNotFound
	}
	return ver, nil
}

func (m *MemoryStore) ListVersions(botID string) ([]*Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	botVers, ok := m.versions[botID]
	if !ok {
		return nil, ErrNotFound
	}
	var list []*Version
	for _, v := range botVers {
		list = append(list, v)
	}
	return list, nil
}

func (m *MemoryStore) CreateDraft(botID, baseVersion string) (*Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	botVers, ok := m.versions[botID]
	if !ok {
		return nil, ErrNotFound
	}

	newVerID := fmt.Sprintf("v%d", len(botVers)+1)
	newVer := &Version{
		ID:          newVerID,
		BotID:       botID,
		Version:     newVerID,
		Status:      "draft",
		EntryNodeID: "welcome",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if base, ok := botVers[baseVersion]; ok {
		newVer.Nodes = append([]botdef.DraftNode{}, base.Nodes...)
		newVer.Edges = append([]botdef.DraftEdge{}, base.Edges...)
		newVer.EntryNodeID = base.EntryNodeID
	}

	botVers[newVerID] = newVer
	if b, ok := m.bots[botID]; ok {
		b.DraftVersion = newVerID
		b.UpdatedAt = time.Now().UTC()
	}
	return newVer, nil
}

func (m *MemoryStore) SaveDraft(version *Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	botVers, ok := m.versions[version.BotID]
	if !ok {
		return ErrNotFound
	}
	existing, ok := botVers[version.Version]
	if !ok {
		return ErrNotFound
	}
	if existing.Status == "published" {
		return ErrImmutable
	}

	version.UpdatedAt = time.Now().UTC()
	botVers[version.Version] = version
	return nil
}

func (m *MemoryStore) PublishVersion(botID, versionID string) (*botdef.CompiledArtifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	bot, ok := m.bots[botID]
	if !ok {
		return nil, ErrNotFound
	}

	botVers, ok := m.versions[botID]
	if !ok {
		return nil, ErrNotFound
	}

	ver, ok := botVers[versionID]
	if !ok {
		return nil, ErrNotFound
	}

	// Compile draft
	draft := &botdef.DraftVersion{
		Tenant:      bot.Tenant,
		AppID:       botID,
		Version:     versionID,
		EntryNodeID: ver.EntryNodeID,
		Nodes:       ver.Nodes,
		Edges:       ver.Edges,
	}

	artifact, err := botdef.Compile(draft)
	if err != nil {
		return nil, fmt.Errorf("draft validation failed: %w", err)
	}

	// Update status
	now := time.Now().UTC()
	ver.Status = "published"
	ver.PublishedAt = &now
	ver.ArtifactURI = fmt.Sprintf("s3://bot-dialog-generator-definitions/%s/%s/%s.pb", bot.Tenant, botID, versionID)

	// Update active version pointer
	m.activePtrs[botID] = versionID
	bot.ActiveVersion = versionID
	bot.UpdatedAt = now

	// Append outbox event
	m.outboxEvents = append(m.outboxEvents, &OutboxEvent{
		ID:        fmt.Sprintf("evt-%d", len(m.outboxEvents)+1),
		EventType: "bot.published",
		Payload: map[string]any{
			"tenant":       bot.Tenant,
			"bot_id":       botID,
			"version":      versionID,
			"artifact_uri": ver.ArtifactURI,
			"sha256":       artifact.SHA256,
			"timestamp":    now,
		},
		Status:    "pending",
		CreatedAt: now,
	})

	return artifact, nil
}

func (m *MemoryStore) GetActiveVersion(botID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.activePtrs[botID]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (m *MemoryStore) SetActiveVersion(botID, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bots[botID]; !ok {
		return ErrNotFound
	}
	m.activePtrs[botID] = version
	if b, ok := m.bots[botID]; ok {
		b.ActiveVersion = version
		b.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MemoryStore) GetOutboxEvents() []*OutboxEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outboxEvents
}
