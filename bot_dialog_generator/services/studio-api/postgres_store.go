package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore keeps studio-api data in PostgreSQL. Each area's bots, versions and
// outbox events are protected by row-level security (migration 000001): every
// workspace-scoped transaction declares its workspace, and the database refuses to
// read or write another one, even if a query forgets its WHERE clause.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects with databaseURL, which must be the application role
// (a member of bdg_app), not the schema owner.
func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

// inWorkspace runs fn in a transaction that row-level security limits to one workspace.
func (s *PostgresStore) inWorkspace(ctx context.Context, workspaceID string, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.workspace_id', $1, true)", workspaceID); err != nil {
			return err
		}
		return fn(tx)
	})
}

// acrossWorkspaces runs fn in a read transaction that sees every workspace. Only
// platform-admin reads use it; writes are always limited to one workspace.
func (s *PostgresStore) acrossWorkspaces(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.all_workspaces', 'on', true)"); err != nil {
			return err
		}
		return fn(tx)
	})
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func workspaceExists(ctx context.Context, q queryRower, workspaceID string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM workspaces WHERE id = $1)", workspaceID).Scan(&exists)
	return exists, err
}

// requireWorkspace returns ErrNotFound when the workspace does not exist.
func requireWorkspace(ctx context.Context, q queryRower, workspaceID string) error {
	exists, err := workspaceExists(ctx, q, workspaceID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// lockWorkspace makes concurrent membership changes in one workspace run one after
// the other, so two owners cannot remove each other at the same time.
func lockWorkspace(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	var id string
	err := tx.QueryRow(ctx, "SELECT id FROM workspaces WHERE id = $1 FOR UPDATE", workspaceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *PostgresStore) RolesFor(ctx context.Context, subject string, groups []string) (map[string]Role, error) {
	if groups == nil {
		groups = []string{}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.workspace_id, m.role FROM workspace_members m
		JOIN workspaces w ON w.id = m.workspace_id
		WHERE w.archived_at IS NULL AND m.subject = $1
		UNION ALL
		SELECT g.workspace_id, g.role FROM workspace_group_grants g
		JOIN workspaces w ON w.id = g.workspace_id
		WHERE w.archived_at IS NULL AND g.group_id = ANY($2)`, subject, groups)
	if err != nil {
		return nil, err
	}
	roles := make(map[string]Role)
	var workspaceID string
	var role Role
	_, err = pgx.ForEachRow(rows, []any{&workspaceID, &role}, func() error {
		roles[workspaceID] = higher(roles[workspaceID], role)
		return nil
	})
	return roles, err
}

const workspaceColumns = "id, name, created_at, archived_at"

func scanWorkspace(row pgx.Row) (*Workspace, error) {
	var ws Workspace
	if err := row.Scan(&ws.ID, &ws.Name, &ws.CreatedAt, &ws.ArchivedAt); err != nil {
		return nil, err
	}
	return &ws, nil
}

func (s *PostgresStore) ListWorkspaces(ctx context.Context) ([]*Workspace, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+workspaceColumns+` FROM workspaces ORDER BY name COLLATE "C"`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Workspace, error) { return scanWorkspace(r) })
}

func (s *PostgresStore) WorkspaceExists(ctx context.Context, workspaceID string) (bool, error) {
	return workspaceExists(ctx, s.pool, workspaceID)
}

func (s *PostgresStore) CreateWorkspace(ctx context.Context, name string, initial Access) (*Workspace, error) {
	name, initial, err := validateNewWorkspace(name, initial)
	if err != nil {
		return nil, err
	}
	var ws *Workspace
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		ws, err = scanWorkspace(tx.QueryRow(ctx,
			"INSERT INTO workspaces (id, name) VALUES ($1, $2) RETURNING "+workspaceColumns, newID(), name))
		if err != nil {
			return err
		}
		for _, m := range initial.Members {
			if _, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, subject, role) VALUES ($1, $2, $3)
				ON CONFLICT (workspace_id, subject) DO UPDATE SET role = EXCLUDED.role`, ws.ID, m.Subject, m.Role); err != nil {
				return err
			}
		}
		for _, g := range initial.Groups {
			if _, err := tx.Exec(ctx, `INSERT INTO workspace_group_grants (workspace_id, group_id, display_name, role) VALUES ($1, $2, $3, $4)
				ON CONFLICT (workspace_id, group_id) DO UPDATE SET display_name = EXCLUDED.display_name, role = EXCLUDED.role`,
				ws.ID, g.GroupID, g.DisplayName, g.Role); err != nil {
				return err
			}
		}
		return nil
	})
	return ws, err
}

func (s *PostgresStore) UpdateWorkspace(ctx context.Context, workspaceID string, name *string, archived *bool) (*Workspace, error) {
	if name != nil {
		clean, err := cleanName(*name)
		if err != nil {
			return nil, err
		}
		name = &clean
	}
	ws, err := scanWorkspace(s.pool.QueryRow(ctx, `
		UPDATE workspaces SET
			name = COALESCE($2, name),
			archived_at = CASE WHEN $3::boolean IS NULL THEN archived_at
			                   WHEN $3 THEN COALESCE(archived_at, now())
			                   ELSE NULL END
		WHERE id = $1
		RETURNING `+workspaceColumns, workspaceID, name, archived))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return ws, err
}

func (s *PostgresStore) ListAccess(ctx context.Context, workspaceID string) (Access, error) {
	access := Access{Members: []Member{}, Groups: []GroupGrant{}}
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT subject, role FROM workspace_members WHERE workspace_id = $1 ORDER BY subject COLLATE "C"`, workspaceID)
		if err != nil {
			return err
		}
		if access.Members, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Member]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT group_id, display_name, role FROM workspace_group_grants WHERE workspace_id = $1 ORDER BY display_name COLLATE "C"`, workspaceID)
		if err != nil {
			return err
		}
		access.Groups, err = pgx.CollectRows(rows, pgx.RowToStructByPos[GroupGrant])
		return err
	})
	return access, err
}

// ownerCheck refuses a change from current to next role (empty when removing) that
// would leave the workspace without an owner. The caller holds lockWorkspace.
func ownerCheck(ctx context.Context, tx pgx.Tx, workspaceID string, current, next Role) error {
	if current != RoleOwner || next == RoleOwner {
		return nil
	}
	var owners int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner')
		     + (SELECT count(*) FROM workspace_group_grants WHERE workspace_id = $1 AND role = 'owner')`,
		workspaceID).Scan(&owners)
	if err != nil {
		return err
	}
	if owners == 1 {
		return errLastOwner
	}
	return nil
}

func currentRole(ctx context.Context, tx pgx.Tx, query, workspaceID, key string) (Role, error) {
	var role Role
	err := tx.QueryRow(ctx, query, workspaceID, key).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

const (
	memberRoleQuery = "SELECT role FROM workspace_members WHERE workspace_id = $1 AND subject = $2"
	groupRoleQuery  = "SELECT role FROM workspace_group_grants WHERE workspace_id = $1 AND group_id = $2"
)

// changeAccess runs one membership or group change under the workspace lock, after
// checking it keeps an owner.
func (s *PostgresStore) changeAccess(ctx context.Context, workspaceID, roleQuery, key string, next Role, change func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		current, err := currentRole(ctx, tx, roleQuery, workspaceID, key)
		if err != nil {
			return err
		}
		if err := ownerCheck(ctx, tx, workspaceID, current, next); err != nil {
			return err
		}
		return change(tx)
	})
}

func (s *PostgresStore) SetMember(ctx context.Context, workspaceID, subject string, role Role) error {
	subject, err := cleanID("subject", subject)
	if err != nil {
		return err
	}
	if err := checkRole(role); err != nil {
		return err
	}
	return s.changeAccess(ctx, workspaceID, memberRoleQuery, subject, role, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO workspace_members (workspace_id, subject, role) VALUES ($1, $2, $3)
			ON CONFLICT (workspace_id, subject) DO UPDATE SET role = EXCLUDED.role`, workspaceID, subject, role)
		return err
	})
}

func (s *PostgresStore) RemoveMember(ctx context.Context, workspaceID, subject string) error {
	return s.changeAccess(ctx, workspaceID, memberRoleQuery, subject, "", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM workspace_members WHERE workspace_id = $1 AND subject = $2", workspaceID, subject)
		return err
	})
}

func (s *PostgresStore) SetGroupGrant(ctx context.Context, workspaceID string, grant GroupGrant) error {
	grant, err := validGrant(grant)
	if err != nil {
		return err
	}
	return s.changeAccess(ctx, workspaceID, groupRoleQuery, grant.GroupID, grant.Role, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO workspace_group_grants (workspace_id, group_id, display_name, role) VALUES ($1, $2, $3, $4)
			ON CONFLICT (workspace_id, group_id) DO UPDATE SET display_name = EXCLUDED.display_name, role = EXCLUDED.role`,
			workspaceID, grant.GroupID, grant.DisplayName, grant.Role)
		return err
	})
}

func (s *PostgresStore) RemoveGroupGrant(ctx context.Context, workspaceID, groupID string) error {
	return s.changeAccess(ctx, workspaceID, groupRoleQuery, groupID, "", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "DELETE FROM workspace_group_grants WHERE workspace_id = $1 AND group_id = $2", workspaceID, groupID)
		return err
	})
}

const botColumns = "id, workspace_id, tenant, name, description, COALESCE(active_version, ''), draft_version, created_at, updated_at"

func scanBot(row pgx.Row) (*Bot, error) {
	var b Bot
	err := row.Scan(&b.ID, &b.WorkspaceID, &b.Tenant, &b.Name, &b.Description, &b.ActiveVersion, &b.DraftVersion, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func collectBots(rows pgx.Rows, err error) ([]*Bot, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Bot, error) { return scanBot(r) })
}

func getBot(ctx context.Context, tx pgx.Tx, workspaceID, botID string) (*Bot, error) {
	return scanBot(tx.QueryRow(ctx, "SELECT "+botColumns+" FROM bots WHERE id = $1 AND workspace_id = $2", botID, workspaceID))
}

func (s *PostgresStore) ListBots(ctx context.Context, workspaceID string) ([]*Bot, error) {
	var bots []*Bot
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		var err error
		bots, err = collectBots(tx.Query(ctx, "SELECT "+botColumns+` FROM bots WHERE workspace_id = $1 ORDER BY name COLLATE "C"`, workspaceID))
		return err
	})
	return bots, err
}

func (s *PostgresStore) ListAllBots(ctx context.Context) ([]*Bot, error) {
	var bots []*Bot
	err := s.acrossWorkspaces(ctx, func(tx pgx.Tx) error {
		var err error
		bots, err = collectBots(tx.Query(ctx, "SELECT "+botColumns+` FROM bots ORDER BY name COLLATE "C"`))
		return err
	})
	return bots, err
}

func (s *PostgresStore) GetBot(ctx context.Context, workspaceID, botID string) (*Bot, error) {
	var bot *Bot
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		bot, err = getBot(ctx, tx, workspaceID, botID)
		return err
	})
	return bot, err
}

// lockBot returns the bot's tenant, locking its row for the rest of the transaction.
func lockBot(ctx context.Context, tx pgx.Tx, workspaceID, botID string) (string, error) {
	var tenant string
	err := tx.QueryRow(ctx, "SELECT tenant FROM bots WHERE id = $1 AND workspace_id = $2 FOR UPDATE", botID, workspaceID).Scan(&tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return tenant, err
}

func (s *PostgresStore) CreateBot(ctx context.Context, workspaceID, name, description string) (*Bot, error) {
	var bot *Bot
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		var err error
		bot, err = scanBot(tx.QueryRow(ctx, `INSERT INTO bots (id, workspace_id, tenant, name, description, draft_version)
			VALUES ($1, $2, 'demo', $3, $4, 'v1') RETURNING `+botColumns, newID(), workspaceID, name, description))
		if err != nil {
			return err
		}
		return insertVersion(ctx, tx, workspaceID, starterDraft(bot.ID, "v1", time.Now().UTC()))
	})
	return bot, err
}

func insertVersion(ctx context.Context, tx pgx.Tx, workspaceID string, v *Version) error {
	nodes, edges, err := encodeFlow(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO bot_versions
		(workspace_id, bot_id, version, status, entry_node_id, nodes, edges, artifact_uri, published_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		workspaceID, v.BotID, v.Version, v.Status, v.EntryNodeID, nodes, edges, v.ArtifactURI, v.PublishedAt, v.CreatedAt, v.UpdatedAt)
	return err
}

func encodeFlow(v *Version) (nodes, edges []byte, err error) {
	if nodes, err = json.Marshal(nonNil(v.Nodes)); err != nil {
		return nil, nil, err
	}
	edges, err = json.Marshal(nonNil(v.Edges))
	return nodes, edges, err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

const versionColumns = "bot_id, version, status, entry_node_id, nodes, edges, artifact_uri, published_at, created_at, updated_at"

func scanVersion(row pgx.Row) (*Version, error) {
	var v Version
	var nodes, edges []byte
	err := row.Scan(&v.BotID, &v.Version, &v.Status, &v.EntryNodeID, &nodes, &edges, &v.ArtifactURI, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.ID = v.Version
	if err := json.Unmarshal(nodes, &v.Nodes); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(edges, &v.Edges); err != nil {
		return nil, err
	}
	return &v, nil
}

func getVersion(ctx context.Context, tx pgx.Tx, workspaceID, botID, versionID string, forUpdate bool) (*Version, error) {
	query := "SELECT " + versionColumns + " FROM bot_versions WHERE workspace_id = $1 AND bot_id = $2 AND version = $3"
	if forUpdate {
		query += " FOR UPDATE"
	}
	return scanVersion(tx.QueryRow(ctx, query, workspaceID, botID, versionID))
}

func (s *PostgresStore) GetVersion(ctx context.Context, workspaceID, botID, versionID string) (*Version, error) {
	var ver *Version
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		ver, err = getVersion(ctx, tx, workspaceID, botID, versionID, false)
		return err
	})
	return ver, err
}

func (s *PostgresStore) ListVersions(ctx context.Context, workspaceID, botID string) ([]*Version, error) {
	var list []*Version
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if _, err := getBot(ctx, tx, workspaceID, botID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+versionColumns+" FROM bot_versions WHERE workspace_id = $1 AND bot_id = $2 ORDER BY created_at", workspaceID, botID)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Version, error) { return scanVersion(r) })
		return err
	})
	return list, err
}

func (s *PostgresStore) CreateDraft(ctx context.Context, workspaceID, botID, baseVersion string) (*Version, error) {
	var draft *Version
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if _, err := lockBot(ctx, tx, workspaceID, botID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT version FROM bot_versions WHERE workspace_id = $1 AND bot_id = $2", workspaceID, botID)
		if err != nil {
			return err
		}
		existing, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		id := nextVersion(slices.Values(existing))
		draft = &Version{ID: id, BotID: botID, Version: id, Status: "draft", EntryNodeID: "welcome", CreatedAt: now, UpdatedAt: now}
		if baseVersion != "" {
			base, err := getVersion(ctx, tx, workspaceID, botID, baseVersion, false)
			if errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: base version %s does not exist", ErrInvalid, baseVersion)
			}
			if err != nil {
				return err
			}
			draft.Nodes, draft.Edges, draft.EntryNodeID = base.Nodes, base.Edges, base.EntryNodeID
		}
		if err := insertVersion(ctx, tx, workspaceID, draft); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE bots SET draft_version = $3, updated_at = $4 WHERE id = $1 AND workspace_id = $2", botID, workspaceID, id, now)
		return err
	})
	return draft, err
}

func (s *PostgresStore) SaveDraft(ctx context.Context, workspaceID string, version *Version) error {
	return s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		existing, err := getVersion(ctx, tx, workspaceID, version.BotID, version.Version, true)
		if err != nil {
			return err
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
		nodes, edges, err := encodeFlow(&saved)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE bot_versions SET entry_node_id = $4, nodes = $5, edges = $6, artifact_uri = '', published_at = NULL, updated_at = $7
			WHERE workspace_id = $1 AND bot_id = $2 AND version = $3`,
			workspaceID, saved.BotID, saved.Version, saved.EntryNodeID, nodes, edges, saved.UpdatedAt)
		if err == nil {
			*version = saved
		}
		return err
	})
}

func (s *PostgresStore) PublishVersion(ctx context.Context, workspaceID, botID, versionID string) (*botdef.CompiledArtifact, *Version, error) {
	var artifact *botdef.CompiledArtifact
	var published *Version
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		tenant, err := lockBot(ctx, tx, workspaceID, botID)
		if err != nil {
			return err
		}
		ver, err := getVersion(ctx, tx, workspaceID, botID, versionID, true)
		if err != nil {
			return err
		}
		if ver.Status == "published" {
			return ErrImmutable
		}
		artifact, err = botdef.Compile(&botdef.DraftVersion{
			Tenant: tenant, WorkspaceID: workspaceID, AppID: botID, Version: versionID,
			EntryNodeID: ver.EntryNodeID, Nodes: ver.Nodes, Edges: ver.Edges,
		})
		if err != nil {
			return fmt.Errorf("%w: draft validation failed: %v", ErrInvalid, err)
		}
		now := time.Now().UTC()
		ver.Status = "published"
		ver.PublishedAt = &now
		ver.ArtifactURI = artifactURI(workspaceID, botID, versionID)
		ver.UpdatedAt = now
		if _, err := tx.Exec(ctx, `UPDATE bot_versions SET status = 'published', published_at = $4, artifact_uri = $5, updated_at = $4
			WHERE workspace_id = $1 AND bot_id = $2 AND version = $3`, workspaceID, botID, versionID, now, ver.ArtifactURI); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE bots SET active_version = $3, updated_at = $4 WHERE id = $1 AND workspace_id = $2", botID, workspaceID, versionID, now); err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]any{
			"tenant": tenant, "workspace_id": workspaceID, "bot_id": botID, "version": versionID,
			"artifact_uri": ver.ArtifactURI, "sha256": artifact.SHA256, "timestamp": now,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO outbox_events (workspace_id, event_type, payload) VALUES ($1, 'bot.published', $2)", workspaceID, payload); err != nil {
			return err
		}
		published = ver
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return artifact, published, nil
}

func (s *PostgresStore) GetActiveVersion(ctx context.Context, workspaceID, botID string) (string, error) {
	var active string
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		bot, err := getBot(ctx, tx, workspaceID, botID)
		if err != nil {
			return err
		}
		if bot.ActiveVersion == "" {
			return ErrNotFound
		}
		active = bot.ActiveVersion
		return nil
	})
	return active, err
}

// SetActiveVersion points the bot at an already-published version (rollback or roll-forward).
func (s *PostgresStore) SetActiveVersion(ctx context.Context, workspaceID, botID, version string) error {
	return s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if _, err := lockBot(ctx, tx, workspaceID, botID); err != nil {
			return err
		}
		ver, err := getVersion(ctx, tx, workspaceID, botID, version, false)
		if errors.Is(err, ErrNotFound) || (err == nil && ver.Status != "published") {
			return fmt.Errorf("%w: only published versions can be activated", ErrInvalid)
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE bots SET active_version = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2", botID, workspaceID, version)
		return err
	})
}

func (s *PostgresStore) AppendAudit(ctx context.Context, entry AuditEntry) error {
	var at *time.Time
	if !entry.At.IsZero() {
		at = &entry.At
	}
	var workspaceID *string
	if entry.WorkspaceID != "" {
		workspaceID = &entry.WorkspaceID
	}
	_, err := s.pool.Exec(ctx, "INSERT INTO audit_log (at, subject, workspace_id, action, resource) VALUES (COALESCE($1, now()), $2, $3, $4, $5)",
		at, entry.Subject, workspaceID, entry.Action, entry.Resource)
	return err
}

func (s *PostgresStore) ListAudit(ctx context.Context) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx, "SELECT at, subject, COALESCE(workspace_id, ''), action, resource FROM audit_log ORDER BY id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (AuditEntry, error) {
		var e AuditEntry
		err := r.Scan(&e.At, &e.Subject, &e.WorkspaceID, &e.Action, &e.Resource)
		return e, err
	})
}

func (s *PostgresStore) GetOutboxEvents(ctx context.Context) ([]*OutboxEvent, error) {
	var events []*OutboxEvent
	err := s.acrossWorkspaces(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT id::text, event_type, payload, status, created_at FROM outbox_events ORDER BY created_at")
		if err != nil {
			return err
		}
		events, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*OutboxEvent, error) {
			var e OutboxEvent
			var payload []byte
			if err := r.Scan(&e.ID, &e.EventType, &payload, &e.Status, &e.CreatedAt); err != nil {
				return nil, err
			}
			var decoded map[string]any
			if err := json.Unmarshal(payload, &decoded); err != nil {
				return nil, err
			}
			e.Payload = decoded
			return &e, nil
		})
		return err
	})
	return events, err
}

// Seed loads the demo organization. It is used for local development and tests.
func (s *PostgresStore) Seed(ctx context.Context, seed demoSeed) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, ws := range seed.Workspaces {
			if _, err := tx.Exec(ctx, "INSERT INTO workspaces (id, name, created_at) VALUES ($1, $2, $3)", ws.ID, ws.Name, ws.CreatedAt); err != nil {
				return err
			}
		}
		for _, m := range seed.Members {
			if _, err := tx.Exec(ctx, "INSERT INTO workspace_members (workspace_id, subject, role) VALUES ($1, $2, $3)", m.WorkspaceID, m.Subject, m.Role); err != nil {
				return err
			}
		}
		for _, g := range seed.Groups {
			if _, err := tx.Exec(ctx, "INSERT INTO workspace_group_grants (workspace_id, group_id, display_name, role) VALUES ($1, $2, $3, $4)", g.WorkspaceID, g.GroupID, g.DisplayName, g.Role); err != nil {
				return err
			}
		}
		for _, b := range seed.Bots {
			// Bots and versions are under row-level security: declare each one's workspace.
			if _, err := tx.Exec(ctx, "SELECT set_config('app.workspace_id', $1, true)", b.Bot.WorkspaceID); err != nil {
				return err
			}
			var active *string
			if b.Bot.ActiveVersion != "" {
				active = &b.Bot.ActiveVersion
			}
			if _, err := tx.Exec(ctx, `INSERT INTO bots (id, workspace_id, tenant, name, description, draft_version, active_version, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
				b.Bot.ID, b.Bot.WorkspaceID, b.Bot.Tenant, b.Bot.Name, b.Bot.Description, b.Bot.DraftVersion, active, b.Bot.CreatedAt, b.Bot.UpdatedAt); err != nil {
				return err
			}
			for i := range b.Versions {
				if err := insertVersion(ctx, tx, b.Bot.WorkspaceID, &b.Versions[i]); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// SeedDemoIfEmpty loads the demo organization into a database that has no workspaces yet.
func (s *PostgresStore) SeedDemoIfEmpty(ctx context.Context) (bool, error) {
	var hasWorkspaces bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM workspaces)").Scan(&hasWorkspaces); err != nil {
		return false, err
	}
	if hasWorkspaces {
		return false, nil
	}
	return true, s.Seed(ctx, newDemoSeed(time.Now().UTC()))
}

var _ Store = (*PostgresStore)(nil)
