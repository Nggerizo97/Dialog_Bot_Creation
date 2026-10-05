package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
)

var (
	ErrNotFound  = errors.New("resource not found")
	ErrImmutable = errors.New("cannot modify published version")
	ErrInvalid   = errors.New("invalid request")
)

// Role is a member's permission level inside one workspace.
type Role string

const (
	RoleAnalyst Role = "analyst"
	RoleEditor  Role = "editor"
	RoleOwner   Role = "owner"
)

var roleRank = map[Role]int{RoleAnalyst: 1, RoleEditor: 2, RoleOwner: 3}

// Allows reports whether r grants at least the permissions of need.
func (r Role) Allows(need Role) bool {
	return roleRank[r] > 0 && roleRank[r] >= roleRank[need]
}

func higher(a, b Role) Role {
	if roleRank[b] > roleRank[a] {
		return b
	}
	return a
}

// Workspace is one area's private space.
type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Bot struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
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

// AuditEntry records a sensitive action. The log is append-only.
type AuditEntry struct {
	At          time.Time `json:"at"`
	Subject     string    `json:"subject"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Action      string    `json:"action"`
	Resource    string    `json:"resource"`
}

// Store defines persistence operations for Studio API. Every bot and version
// operation is scoped by workspace: a bot that exists in another workspace is
// reported as ErrNotFound.
type Store interface {
	RolesFor(subject string, groups []string) map[string]Role
	ListWorkspaces() []*Workspace
	WorkspaceExists(workspaceID string) bool

	ListBots(workspaceID string) ([]*Bot, error)
	ListAllBots() []*Bot
	GetBot(workspaceID, botID string) (*Bot, error)
	CreateBot(workspaceID, name, description string) (*Bot, error)
	GetVersion(workspaceID, botID, versionID string) (*Version, error)
	ListVersions(workspaceID, botID string) ([]*Version, error)
	CreateDraft(workspaceID, botID, baseVersion string) (*Version, error)
	SaveDraft(workspaceID string, version *Version) error
	PublishVersion(workspaceID, botID, versionID string) (*botdef.CompiledArtifact, *Version, error)
	GetActiveVersion(workspaceID, botID string) (string, error)
	SetActiveVersion(workspaceID, botID, version string) error

	AppendAudit(entry AuditEntry)
	ListAudit() []AuditEntry
	GetOutboxEvents() []*OutboxEvent
}

// MemoryStore provides thread-safe in-memory storage.
type MemoryStore struct {
	mu           sync.RWMutex
	workspaces   map[string]*Workspace
	members      map[string]map[string]Role // workspaceID -> subject -> role
	groupGrants  map[string]map[string]Role // workspaceID -> IdP group -> role
	bots         map[string]*Bot
	versions     map[string]map[string]*Version // botID -> version -> Version
	activePtrs   map[string]string              // botID -> activeVersion
	outboxEvents []*OutboxEvent
	audit        []AuditEntry
}

// NewEmptyStore returns a store with no data.
func NewEmptyStore() *MemoryStore {
	return &MemoryStore{
		workspaces:  make(map[string]*Workspace),
		members:     make(map[string]map[string]Role),
		groupGrants: make(map[string]map[string]Role),
		bots:        make(map[string]*Bot),
		versions:    make(map[string]map[string]*Version),
		activePtrs:  make(map[string]string),
	}
}

// NewMemoryStore returns a store seeded with two demo workspaces whose members are
// listed in the README (alice, carol and erin in Customer service; bob in Human resources).
func NewMemoryStore() *MemoryStore {
	now := time.Now().UTC()
	s := NewEmptyStore()
	s.AddWorkspace("ws-customer-service", "Customer service")
	s.AddWorkspace("ws-hr", "Human resources")
	s.AddMember("ws-customer-service", "alice", RoleOwner)
	s.AddMember("ws-customer-service", "erin", RoleEditor)
	s.AddMember("ws-customer-service", "carol", RoleAnalyst)
	s.AddMember("ws-hr", "bob", RoleOwner)
	s.GrantGroup("ws-hr", "hr-team", RoleEditor)

	menuNodes := []botdef.DraftNode{
		{ID: "welcome", Type: "Trigger", Title: "Welcome", Detail: "New conversation"},
		{ID: "menu", Type: "Menu", Title: "What can we help with?", Detail: "3 routes", Properties: map[string]string{"prompt": "What can we help you with today?"}},
		{ID: "balance", Type: "Service", Title: "Check balance", Detail: "Accounts API", Properties: map[string]string{"endpoint": "/mock/accounts/balance"}},
		{ID: "handoff", Type: "Response", Title: "Connect to advisor", Detail: "Text message", Properties: map[string]string{"message": "Please hold while we connect you to an advisor."}},
	}
	menuEdges := []botdef.DraftEdge{
		{From: "welcome", To: "menu"},
		{From: "menu", To: "balance", Condition: "balance"},
		{From: "menu", To: "handoff", Condition: "handoff"},
	}
	published := now.Add(-26 * time.Hour)
	s.seedBot(&Bot{
		ID: "retail-assistant", WorkspaceID: "ws-customer-service", Tenant: "demo",
		Name: "Retail assistant", Description: "Customer support bot for account services",
		DraftVersion: "v18", ActiveVersion: "v17", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
	}, &Version{
		ID: "v17", BotID: "retail-assistant", Version: "v17", Status: "published", EntryNodeID: "welcome",
		Nodes: menuNodes, Edges: menuEdges, PublishedAt: &published,
		ArtifactURI: artifactURI("ws-customer-service", "retail-assistant", "v17"),
		CreatedAt:   now.Add(-30 * time.Hour), UpdatedAt: published,
	}, &Version{
		ID: "v18", BotID: "retail-assistant", Version: "v18", Status: "draft", EntryNodeID: "welcome",
		Nodes: menuNodes, Edges: menuEdges, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now,
	})
	s.seedBot(&Bot{
		ID: "hr-helpdesk", WorkspaceID: "ws-hr", Tenant: "demo",
		Name: "HR helpdesk", Description: "Answers questions about leave and payroll",
		DraftVersion: "v1", CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
	}, starterDraft("hr-helpdesk", "v1", now))
	return s
}

// AddWorkspace creates a workspace.
func (m *MemoryStore) AddWorkspace(id, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaces[id] = &Workspace{ID: id, Name: name, CreatedAt: time.Now().UTC()}
}

// AddMember grants subject a role in a workspace.
func (m *MemoryStore) AddMember(workspaceID, subject string, role Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.members[workspaceID] == nil {
		m.members[workspaceID] = make(map[string]Role)
	}
	m.members[workspaceID][subject] = role
}

// GrantGroup grants every member of an identity-provider group a role in a workspace.
func (m *MemoryStore) GrantGroup(workspaceID, group string, role Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.groupGrants[workspaceID] == nil {
		m.groupGrants[workspaceID] = make(map[string]Role)
	}
	m.groupGrants[workspaceID][group] = role
}

func (m *MemoryStore) seedBot(bot *Bot, versions ...*Version) {
	m.bots[bot.ID] = bot
	m.versions[bot.ID] = make(map[string]*Version)
	for _, v := range versions {
		m.versions[bot.ID][v.Version] = v
	}
	if bot.ActiveVersion != "" {
		m.activePtrs[bot.ID] = bot.ActiveVersion
	}
}

// RolesFor returns the caller's highest role in each workspace, combining direct
// memberships and group grants.
func (m *MemoryStore) RolesFor(subject string, groups []string) map[string]Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roles := make(map[string]Role)
	for ws, members := range m.members {
		if role, ok := members[subject]; ok {
			roles[ws] = higher(roles[ws], role)
		}
	}
	for ws, grants := range m.groupGrants {
		for _, g := range groups {
			if role, ok := grants[g]; ok {
				roles[ws] = higher(roles[ws], role)
			}
		}
	}
	return roles
}

func (m *MemoryStore) ListWorkspaces() []*Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		copyWS := *ws
		list = append(list, &copyWS)
	}
	slices.SortFunc(list, func(a, b *Workspace) int { return strings.Compare(a.Name, b.Name) })
	return list
}

func (m *MemoryStore) WorkspaceExists(workspaceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.workspaces[workspaceID]
	return ok
}

// botIn returns the bot only if it belongs to workspaceID. Callers must hold the lock.
func (m *MemoryStore) botIn(workspaceID, botID string) (*Bot, bool) {
	b, ok := m.bots[botID]
	if !ok || b.WorkspaceID != workspaceID {
		return nil, false
	}
	return b, true
}

func (m *MemoryStore) botCopy(b *Bot) *Bot {
	copyBot := *b
	copyBot.ActiveVersion = m.activePtrs[b.ID]
	return &copyBot
}

func sortBots(list []*Bot) {
	slices.SortFunc(list, func(a, b *Bot) int { return strings.Compare(a.Name, b.Name) })
}

func (m *MemoryStore) ListBots(workspaceID string) ([]*Bot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return nil, ErrNotFound
	}
	result := []*Bot{}
	for _, b := range m.bots {
		if b.WorkspaceID == workspaceID {
			result = append(result, m.botCopy(b))
		}
	}
	sortBots(result)
	return result, nil
}

func (m *MemoryStore) ListAllBots() []*Bot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Bot, 0, len(m.bots))
	for _, b := range m.bots {
		result = append(result, m.botCopy(b))
	}
	sortBots(result)
	return result
}

func (m *MemoryStore) GetBot(workspaceID, botID string) (*Bot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.botIn(workspaceID, botID)
	if !ok {
		return nil, ErrNotFound
	}
	return m.botCopy(b), nil
}

func (m *MemoryStore) CreateBot(workspaceID, name, description string) (*Bot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	bot := &Bot{
		ID: newID(), WorkspaceID: workspaceID, Tenant: "demo", Name: name, Description: description,
		DraftVersion: "v1", CreatedAt: now, UpdatedAt: now,
	}
	m.seedBot(bot, starterDraft(bot.ID, "v1", now))
	return m.botCopy(bot), nil
}

// starterDraft is the first draft of a new bot: a single entry trigger.
func starterDraft(botID, version string, now time.Time) *Version {
	return &Version{
		ID: version, BotID: botID, Version: version, Status: "draft", EntryNodeID: "start",
		Nodes:     []botdef.DraftNode{{ID: "start", Type: "Trigger", Title: "Start", Detail: "New conversation"}},
		Edges:     []botdef.DraftEdge{},
		CreatedAt: now, UpdatedAt: now,
	}
}

func (m *MemoryStore) GetVersion(workspaceID, botID, versionID string) (*Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.botIn(workspaceID, botID); !ok {
		return nil, ErrNotFound
	}
	ver, ok := m.versions[botID][versionID]
	if !ok {
		return nil, ErrNotFound
	}
	copyVer := *ver
	return &copyVer, nil
}

func (m *MemoryStore) ListVersions(workspaceID, botID string) ([]*Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.botIn(workspaceID, botID); !ok {
		return nil, ErrNotFound
	}
	list := []*Version{}
	for _, v := range m.versions[botID] {
		copyVer := *v
		list = append(list, &copyVer)
	}
	slices.SortFunc(list, func(a, b *Version) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return list, nil
}

func (m *MemoryStore) CreateDraft(workspaceID, botID, baseVersion string) (*Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bot, ok := m.botIn(workspaceID, botID)
	if !ok {
		return nil, ErrNotFound
	}
	botVers := m.versions[botID]
	now := time.Now().UTC()
	newVerID := fmt.Sprintf("v%d", len(botVers)+1)
	newVer := &Version{
		ID: newVerID, BotID: botID, Version: newVerID, Status: "draft", EntryNodeID: "welcome",
		CreatedAt: now, UpdatedAt: now,
	}
	if baseVersion != "" {
		base, ok := botVers[baseVersion]
		if !ok {
			return nil, fmt.Errorf("%w: base version %s does not exist", ErrInvalid, baseVersion)
		}
		newVer.Nodes = append([]botdef.DraftNode{}, base.Nodes...)
		newVer.Edges = append([]botdef.DraftEdge{}, base.Edges...)
		newVer.EntryNodeID = base.EntryNodeID
	}
	botVers[newVerID] = newVer
	bot.DraftVersion = newVerID
	bot.UpdatedAt = now
	copyVer := *newVer
	return &copyVer, nil
}

func (m *MemoryStore) SaveDraft(workspaceID string, version *Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.botIn(workspaceID, version.BotID); !ok {
		return ErrNotFound
	}
	existing, ok := m.versions[version.BotID][version.Version]
	if !ok {
		return ErrNotFound
	}
	if existing.Status == "published" {
		return ErrImmutable
	}
	saved := *version
	saved.ID = existing.ID
	saved.Status = existing.Status
	saved.CreatedAt = existing.CreatedAt
	saved.PublishedAt = nil
	saved.ArtifactURI = ""
	saved.UpdatedAt = time.Now().UTC()
	m.versions[version.BotID][version.Version] = &saved
	*version = saved
	return nil
}

func artifactURI(workspaceID, botID, versionID string) string {
	return fmt.Sprintf("s3://bot-dialog-generator-definitions/workspaces/%s/bots/%s/%s.pb", workspaceID, botID, versionID)
}

func (m *MemoryStore) PublishVersion(workspaceID, botID, versionID string) (*botdef.CompiledArtifact, *Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bot, ok := m.botIn(workspaceID, botID)
	if !ok {
		return nil, nil, ErrNotFound
	}
	ver, ok := m.versions[botID][versionID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	if ver.Status == "published" {
		return nil, nil, ErrImmutable
	}

	artifact, err := botdef.Compile(&botdef.DraftVersion{
		Tenant:      bot.Tenant,
		AppID:       botID,
		Version:     versionID,
		EntryNodeID: ver.EntryNodeID,
		Nodes:       ver.Nodes,
		Edges:       ver.Edges,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: draft validation failed: %v", ErrInvalid, err)
	}

	now := time.Now().UTC()
	published := *ver
	published.Status = "published"
	published.PublishedAt = &now
	published.ArtifactURI = artifactURI(workspaceID, botID, versionID)
	published.UpdatedAt = now
	m.versions[botID][versionID] = &published

	m.activePtrs[botID] = versionID
	bot.ActiveVersion = versionID
	bot.UpdatedAt = now

	m.outboxEvents = append(m.outboxEvents, &OutboxEvent{
		ID:        fmt.Sprintf("evt-%d", len(m.outboxEvents)+1),
		EventType: "bot.published",
		Payload: map[string]any{
			"tenant":       bot.Tenant,
			"workspace_id": workspaceID,
			"bot_id":       botID,
			"version":      versionID,
			"artifact_uri": published.ArtifactURI,
			"sha256":       artifact.SHA256,
			"timestamp":    now,
		},
		Status:    "pending",
		CreatedAt: now,
	})

	result := published
	return artifact, &result, nil
}

func (m *MemoryStore) GetActiveVersion(workspaceID, botID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.botIn(workspaceID, botID); !ok {
		return "", ErrNotFound
	}
	v, ok := m.activePtrs[botID]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// SetActiveVersion points the bot at an already-published version (rollback or roll-forward).
func (m *MemoryStore) SetActiveVersion(workspaceID, botID, version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	bot, ok := m.botIn(workspaceID, botID)
	if !ok {
		return ErrNotFound
	}
	ver, ok := m.versions[botID][version]
	if !ok || ver.Status != "published" {
		return fmt.Errorf("%w: only published versions can be activated", ErrInvalid)
	}
	m.activePtrs[botID] = version
	bot.ActiveVersion = version
	bot.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *MemoryStore) AppendAudit(entry AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	m.audit = append(m.audit, entry)
}

func (m *MemoryStore) ListAudit() []AuditEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.audit)
}

func (m *MemoryStore) GetOutboxEvents() []*OutboxEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.outboxEvents)
}

// newID returns a UUIDv7: time-ordered and random, so IDs cannot be guessed.
func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on supported platforms
	ms := uint64(time.Now().UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
