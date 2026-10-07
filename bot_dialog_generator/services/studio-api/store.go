package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
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

// Workspace is one area's private space. An archived workspace keeps its data but
// is hidden from its members; platform admins can still see and restore it.
type Workspace struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// Member is a person with a direct role in a workspace.
type Member struct {
	Subject string `json:"subject"`
	Role    Role   `json:"role"`
}

// GroupGrant gives every member of an identity-provider group a role in a workspace.
// With Microsoft Entra ID, GroupID is the group's object ID, which is what the
// token's groups claim contains; DisplayName is only for people reading the studio.
type GroupGrant struct {
	GroupID     string `json:"group_id"`
	DisplayName string `json:"display_name"`
	Role        Role   `json:"role"`
}

// Access lists who can use a workspace.
type Access struct {
	Members []Member     `json:"members"`
	Groups  []GroupGrant `json:"groups"`
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
	RolesFor(ctx context.Context, subject string, groups []string) (map[string]Role, error)
	ListWorkspaces(ctx context.Context) ([]*Workspace, error)
	WorkspaceExists(ctx context.Context, workspaceID string) (bool, error)
	CreateWorkspace(ctx context.Context, name string, initial Access) (*Workspace, error)
	UpdateWorkspace(ctx context.Context, workspaceID string, name *string, archived *bool) (*Workspace, error)

	ListAccess(ctx context.Context, workspaceID string) (Access, error)
	SetMember(ctx context.Context, workspaceID, subject string, role Role) error
	RemoveMember(ctx context.Context, workspaceID, subject string) error
	SetGroupGrant(ctx context.Context, workspaceID string, grant GroupGrant) error
	RemoveGroupGrant(ctx context.Context, workspaceID, groupID string) error

	ListBots(ctx context.Context, workspaceID string) ([]*Bot, error)
	ListAllBots(ctx context.Context) ([]*Bot, error)
	GetBot(ctx context.Context, workspaceID, botID string) (*Bot, error)
	CreateBot(ctx context.Context, workspaceID, name, description string) (*Bot, error)
	GetVersion(ctx context.Context, workspaceID, botID, versionID string) (*Version, error)
	ListVersions(ctx context.Context, workspaceID, botID string) ([]*Version, error)
	CreateDraft(ctx context.Context, workspaceID, botID, baseVersion string) (*Version, error)
	SaveDraft(ctx context.Context, workspaceID string, version *Version) error
	PublishVersion(ctx context.Context, workspaceID, botID, versionID string) (*botdef.CompiledArtifact, *Version, error)
	GetActiveVersion(ctx context.Context, workspaceID, botID string) (string, error)
	SetActiveVersion(ctx context.Context, workspaceID, botID, version string) error

	AppendAudit(ctx context.Context, entry AuditEntry) error
	ListAudit(ctx context.Context) ([]AuditEntry, error)
	GetOutboxEvents(ctx context.Context) ([]*OutboxEvent, error)
}

// MemoryStore provides thread-safe in-memory storage.
type MemoryStore struct {
	mu           sync.RWMutex
	workspaces   map[string]*Workspace
	members      map[string]map[string]Role       // workspaceID -> subject -> role
	groupGrants  map[string]map[string]GroupGrant // workspaceID -> IdP group ID -> grant
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
		groupGrants: make(map[string]map[string]GroupGrant),
		bots:        make(map[string]*Bot),
		versions:    make(map[string]map[string]*Version),
		activePtrs:  make(map[string]string),
	}
}

// NewMemoryStore returns a store seeded with the demo organization (see newDemoSeed).
func NewMemoryStore() *MemoryStore {
	s := NewEmptyStore()
	seed := newDemoSeed(time.Now().UTC())
	for _, ws := range seed.Workspaces {
		s.AddWorkspace(ws.ID, ws.Name)
	}
	for _, m := range seed.Members {
		s.AddMember(m.WorkspaceID, m.Subject, m.Role)
	}
	for _, g := range seed.Groups {
		s.putGroupGrant(g.WorkspaceID, g.GroupGrant)
	}
	for _, b := range seed.Bots {
		bot := b.Bot
		versions := make([]*Version, len(b.Versions))
		for i := range b.Versions {
			v := b.Versions[i]
			versions[i] = &v
		}
		s.seedBot(&bot, versions...)
	}
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
	m.putMember(workspaceID, subject, role)
}

// GrantGroup grants every member of an identity-provider group a role in a workspace.
func (m *MemoryStore) GrantGroup(workspaceID, group string, role Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putGroupGrant(workspaceID, GroupGrant{GroupID: group, DisplayName: group, Role: role})
}

// putGroupGrant stores a grant. Callers must hold the lock.
func (m *MemoryStore) putGroupGrant(workspaceID string, grant GroupGrant) {
	if m.groupGrants[workspaceID] == nil {
		m.groupGrants[workspaceID] = make(map[string]GroupGrant)
	}
	m.groupGrants[workspaceID][grant.GroupID] = grant
}

// putMember stores a direct membership. Callers must hold the lock.
func (m *MemoryStore) putMember(workspaceID, subject string, role Role) {
	if m.members[workspaceID] == nil {
		m.members[workspaceID] = make(map[string]Role)
	}
	m.members[workspaceID][subject] = role
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

// RolesFor returns the caller's highest role in each active workspace, combining
// direct memberships and group grants. Archived workspaces grant no roles.
func (m *MemoryStore) RolesFor(_ context.Context, subject string, groups []string) (map[string]Role, error) {
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
			if grant, ok := grants[g]; ok {
				roles[ws] = higher(roles[ws], grant.Role)
			}
		}
	}
	for ws := range roles {
		if w, ok := m.workspaces[ws]; !ok || w.ArchivedAt != nil {
			delete(roles, ws)
		}
	}
	return roles, nil
}

func (m *MemoryStore) ListWorkspaces(_ context.Context) ([]*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		copyWS := *ws
		list = append(list, &copyWS)
	}
	slices.SortFunc(list, func(a, b *Workspace) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

func (m *MemoryStore) WorkspaceExists(_ context.Context, workspaceID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.workspaces[workspaceID]
	return ok, nil
}

const (
	maxNameLen = 100
	maxIDLen   = 256
)

var errLastOwner = fmt.Errorf("%w: This is the area's last owner. Make someone else an owner first, then change or remove this one", ErrInvalid)

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLen {
		return "", fmt.Errorf("%w: name must be 1-%d characters", ErrInvalid, maxNameLen)
	}
	return name, nil
}

func cleanID(kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > maxIDLen || strings.ContainsAny(id, " \t\r\n/") {
		return "", fmt.Errorf("%w: %s must be 1-%d characters without spaces or slashes", ErrInvalid, kind, maxIDLen)
	}
	return id, nil
}

func checkRole(role Role) error {
	if roleRank[role] == 0 {
		return fmt.Errorf("%w: role must be owner, editor or analyst", ErrInvalid)
	}
	return nil
}

func validGrant(g GroupGrant) (GroupGrant, error) {
	id, err := cleanID("group_id", g.GroupID)
	if err != nil {
		return g, err
	}
	g.GroupID = id
	g.DisplayName = strings.TrimSpace(g.DisplayName)
	if g.DisplayName == "" {
		g.DisplayName = id
	}
	if len(g.DisplayName) > maxNameLen {
		return g, fmt.Errorf("%w: display_name must be at most %d characters", ErrInvalid, maxNameLen)
	}
	return g, checkRole(g.Role)
}

// validateNewWorkspace cleans a new workspace's name and first members and group
// grants. At least one of them must be an owner, so the workspace is never orphaned.
func validateNewWorkspace(name string, initial Access) (string, Access, error) {
	name, err := cleanName(name)
	if err != nil {
		return "", initial, err
	}
	clean := Access{Members: slices.Clone(initial.Members), Groups: slices.Clone(initial.Groups)}
	hasOwner := false
	for i, mem := range clean.Members {
		if clean.Members[i].Subject, err = cleanID("subject", mem.Subject); err != nil {
			return "", initial, err
		}
		if err := checkRole(mem.Role); err != nil {
			return "", initial, err
		}
		hasOwner = hasOwner || mem.Role == RoleOwner
	}
	for i, g := range clean.Groups {
		if clean.Groups[i], err = validGrant(g); err != nil {
			return "", initial, err
		}
		hasOwner = hasOwner || g.Role == RoleOwner
	}
	if !hasOwner {
		return "", initial, fmt.Errorf("%w: a new workspace needs an owner (a person or a group)", ErrInvalid)
	}
	return name, clean, nil
}

// CreateWorkspace creates a workspace with its first members and group grants.
func (m *MemoryStore) CreateWorkspace(_ context.Context, name string, initial Access) (*Workspace, error) {
	name, initial, err := validateNewWorkspace(name, initial)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	ws := &Workspace{ID: newID(), Name: name, CreatedAt: time.Now().UTC()}
	m.workspaces[ws.ID] = ws
	for _, mem := range initial.Members {
		m.putMember(ws.ID, mem.Subject, mem.Role)
	}
	for _, g := range initial.Groups {
		m.putGroupGrant(ws.ID, g)
	}
	copyWS := *ws
	return &copyWS, nil
}

// UpdateWorkspace renames, archives or restores a workspace. Nil fields are unchanged.
func (m *MemoryStore) UpdateWorkspace(_ context.Context, workspaceID string, name *string, archived *bool) (*Workspace, error) {
	var newName string
	if name != nil {
		var err error
		if newName, err = cleanName(*name); err != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, ErrNotFound
	}
	if name != nil {
		ws.Name = newName
	}
	if archived != nil {
		switch {
		case *archived && ws.ArchivedAt == nil:
			now := time.Now().UTC()
			ws.ArchivedAt = &now
		case !*archived:
			ws.ArchivedAt = nil
		}
	}
	copyWS := *ws
	return &copyWS, nil
}

func (m *MemoryStore) ListAccess(_ context.Context, workspaceID string) (Access, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return Access{}, ErrNotFound
	}
	access := Access{Members: []Member{}, Groups: []GroupGrant{}}
	for subject, role := range m.members[workspaceID] {
		access.Members = append(access.Members, Member{Subject: subject, Role: role})
	}
	for _, g := range m.groupGrants[workspaceID] {
		access.Groups = append(access.Groups, g)
	}
	slices.SortFunc(access.Members, func(a, b Member) int { return strings.Compare(a.Subject, b.Subject) })
	slices.SortFunc(access.Groups, func(a, b GroupGrant) int { return strings.Compare(a.DisplayName, b.DisplayName) })
	return access, nil
}

// losesLastOwner reports whether changing a grant from current to next (empty when
// removing) would leave the workspace without an owner. Callers must hold the lock.
func (m *MemoryStore) losesLastOwner(workspaceID string, current, next Role) bool {
	if current != RoleOwner || next == RoleOwner {
		return false
	}
	owners := 0
	for _, role := range m.members[workspaceID] {
		if role == RoleOwner {
			owners++
		}
	}
	for _, g := range m.groupGrants[workspaceID] {
		if g.Role == RoleOwner {
			owners++
		}
	}
	return owners == 1
}

func (m *MemoryStore) SetMember(_ context.Context, workspaceID, subject string, role Role) error {
	subject, err := cleanID("subject", subject)
	if err != nil {
		return err
	}
	if err := checkRole(role); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return ErrNotFound
	}
	if m.losesLastOwner(workspaceID, m.members[workspaceID][subject], role) {
		return errLastOwner
	}
	m.putMember(workspaceID, subject, role)
	return nil
}

// RemoveMember removes a direct membership. Removing someone who is not a member succeeds.
func (m *MemoryStore) RemoveMember(_ context.Context, workspaceID, subject string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return ErrNotFound
	}
	if m.losesLastOwner(workspaceID, m.members[workspaceID][subject], "") {
		return errLastOwner
	}
	delete(m.members[workspaceID], subject)
	return nil
}

func (m *MemoryStore) SetGroupGrant(_ context.Context, workspaceID string, grant GroupGrant) error {
	grant, err := validGrant(grant)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return ErrNotFound
	}
	if m.losesLastOwner(workspaceID, m.groupGrants[workspaceID][grant.GroupID].Role, grant.Role) {
		return errLastOwner
	}
	m.putGroupGrant(workspaceID, grant)
	return nil
}

// RemoveGroupGrant removes a group's access. Removing a group without access succeeds.
func (m *MemoryStore) RemoveGroupGrant(_ context.Context, workspaceID, groupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspaceID]; !ok {
		return ErrNotFound
	}
	if m.losesLastOwner(workspaceID, m.groupGrants[workspaceID][groupID].Role, "") {
		return errLastOwner
	}
	delete(m.groupGrants[workspaceID], groupID)
	return nil
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

func (m *MemoryStore) ListBots(_ context.Context, workspaceID string) ([]*Bot, error) {
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

func (m *MemoryStore) ListAllBots(_ context.Context) ([]*Bot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Bot, 0, len(m.bots))
	for _, b := range m.bots {
		result = append(result, m.botCopy(b))
	}
	sortBots(result)
	return result, nil
}

func (m *MemoryStore) GetBot(_ context.Context, workspaceID, botID string) (*Bot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.botIn(workspaceID, botID)
	if !ok {
		return nil, ErrNotFound
	}
	return m.botCopy(b), nil
}

func (m *MemoryStore) CreateBot(_ context.Context, workspaceID, name, description string) (*Bot, error) {
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

func (m *MemoryStore) GetVersion(_ context.Context, workspaceID, botID, versionID string) (*Version, error) {
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

func (m *MemoryStore) ListVersions(_ context.Context, workspaceID, botID string) ([]*Version, error) {
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

func (m *MemoryStore) CreateDraft(_ context.Context, workspaceID, botID, baseVersion string) (*Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bot, ok := m.botIn(workspaceID, botID)
	if !ok {
		return nil, ErrNotFound
	}
	botVers := m.versions[botID]
	now := time.Now().UTC()
	newVerID := nextVersion(maps.Keys(botVers))
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

func (m *MemoryStore) SaveDraft(_ context.Context, workspaceID string, version *Version) error {
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

// nextVersion returns "v<n+1>" for the highest "v<n>" among existing versions.
func nextVersion(existing iter.Seq[string]) string {
	highest := 0
	for v := range existing {
		if n, err := strconv.Atoi(strings.TrimPrefix(v, "v")); err == nil && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("v%d", highest+1)
}

func artifactURI(workspaceID, botID, versionID string) string {
	return fmt.Sprintf("s3://bot-dialog-generator-definitions/workspaces/%s/bots/%s/%s.pb", workspaceID, botID, versionID)
}

func (m *MemoryStore) PublishVersion(_ context.Context, workspaceID, botID, versionID string) (*botdef.CompiledArtifact, *Version, error) {
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
		WorkspaceID: workspaceID,
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

func (m *MemoryStore) GetActiveVersion(_ context.Context, workspaceID, botID string) (string, error) {
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
func (m *MemoryStore) SetActiveVersion(_ context.Context, workspaceID, botID, version string) error {
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

func (m *MemoryStore) AppendAudit(_ context.Context, entry AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	m.audit = append(m.audit, entry)
	return nil
}

func (m *MemoryStore) ListAudit(_ context.Context) ([]AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.audit), nil
}

func (m *MemoryStore) GetOutboxEvents(_ context.Context) ([]*OutboxEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.outboxEvents), nil
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
