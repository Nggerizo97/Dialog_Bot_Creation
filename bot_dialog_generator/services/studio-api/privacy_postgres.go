package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/privacy"
	"github.com/jackc/pgx/v5"
)

// The PostgreSQL side of privacy.go. Everything that belongs to an area runs inside
// inWorkspace, so row-level security limits an erasure to the workspace it was asked
// for even if a WHERE clause is wrong.

func (s *PostgresStore) UpdateBotSettings(ctx context.Context, workspaceID, botID string, settings BotSettings) (*Bot, error) {
	if err := settings.validate(); err != nil {
		return nil, err
	}
	var bot *Bot
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		var err error
		bot, err = scanBot(tx.QueryRow(ctx, `UPDATE bots SET audience = $3, min_age = $4, updated_at = now()
			WHERE id = $1 AND workspace_id = $2 RETURNING `+botColumns, botID, workspaceID, settings.Audience, settings.MinAge))
		return err
	})
	return bot, err
}

func (s *PostgresStore) RecordConsent(ctx context.Context, workspaceID string, consent ConsentRecord) error {
	if err := consent.normalize(); err != nil {
		return err
	}
	purposes, err := json.Marshal(consent.Purposes)
	if err != nil {
		return err
	}
	return s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO subject_consents
			(workspace_id, channel, subject_id, purposes, policy_version, age_confirmed, guardian_consent, guardian_reference, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			workspaceID, consent.Channel, consent.SubjectID, purposes, consent.PolicyVersion,
			consent.AgeConfirmed, consent.GuardianConsent, consent.GuardianReference, consent.At)
		return err
	})
}

// EraseSubject deletes what the workspace holds about one person, queues the erasure
// event for the stores outside PostgreSQL and writes the ledger entry, all in one
// transaction: either the person's data is gone and the ledger says so, or nothing changed.
func (s *PostgresStore) EraseSubject(ctx context.Context, workspaceID string, in ErasureInput) (*SubjectRequest, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	req := newSubjectRequest(workspaceID, in, time.Now().UTC())
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		var consents, sends int64
		switch in.Kind {
		case SubjectChatUser:
			tag, err := tx.Exec(ctx, "DELETE FROM subject_consents WHERE workspace_id = $1 AND channel = $2 AND subject_id = $3",
				workspaceID, in.Channel, in.SubjectID)
			if err != nil {
				return err
			}
			consents = tag.RowsAffected()
		case SubjectEmail:
			hash := privacy.EmailHash(workspaceID, in.SubjectID)
			tag, err := tx.Exec(ctx, "DELETE FROM campaign_sends WHERE workspace_id = $1 AND email_hash = $2", workspaceID, hash)
			if err != nil {
				return err
			}
			sends = tag.RowsAffected()
			if _, err := tx.Exec(ctx, `INSERT INTO campaign_suppressions (workspace_id, email_hash, reason) VALUES ($1, $2, 'erasure')
				ON CONFLICT (workspace_id, email_hash) DO NOTHING`, workspaceID, hash); err != nil {
				return err
			}
		}
		req.Result = erasureResult(in, int(consents), int(sends))

		payload, err := json.Marshal(erasureEvent(req, in))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO outbox_events (workspace_id, event_type, payload) VALUES ($1, $2, $3)",
			workspaceID, erasureEventType, payload); err != nil {
			return err
		}
		result, err := json.Marshal(req.Result)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO data_subject_requests
			(id, workspace_id, subject_kind, subject_hash, source, verification, requested_by, status, result, created_at, completed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			req.ID, workspaceID, req.SubjectKind, req.SubjectHash, req.Source, req.Verification, req.RequestedBy,
			req.Status, result, req.CreatedAt, req.CompletedAt)
		return err
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (s *PostgresStore) ListSubjectRequests(ctx context.Context, workspaceID string) ([]SubjectRequest, error) {
	var list []SubjectRequest
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := requireWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, workspace_id, subject_kind, subject_hash, source, verification, requested_by, status, result, created_at, completed_at
			FROM data_subject_requests WHERE workspace_id = $1 ORDER BY created_at DESC, id DESC`, workspaceID)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SubjectRequest, error) {
			var req SubjectRequest
			var result []byte
			if err := r.Scan(&req.ID, &req.WorkspaceID, &req.SubjectKind, &req.SubjectHash, &req.Source, &req.Verification,
				&req.RequestedBy, &req.Status, &result, &req.CreatedAt, &req.CompletedAt); err != nil {
				return req, err
			}
			return req, json.Unmarshal(result, &req.Result)
		})
		return err
	})
	return list, err
}

const staffRequestColumns = "id, subject, note, status, result, created_at, resolved_at, resolved_by"

func scanStaffRequest(row pgx.Row) (*StaffRequest, error) {
	var req StaffRequest
	var result []byte
	err := row.Scan(&req.ID, &req.Subject, &req.Note, &req.Status, &result, &req.CreatedAt, &req.ResolvedAt, &req.ResolvedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &req, json.Unmarshal(result, &req.Result)
}

func (s *PostgresStore) CreateStaffRequest(ctx context.Context, subject, note string) (*StaffRequest, error) {
	note, err := cleanNote(note)
	if err != nil {
		return nil, err
	}
	var req *StaffRequest
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// One open request per person: concurrent clicks share it.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", "staff-erasure:"+subject); err != nil {
			return err
		}
		var err error
		req, err = scanStaffRequest(tx.QueryRow(ctx, "SELECT "+staffRequestColumns+
			" FROM staff_erasure_requests WHERE subject = $1 AND status <> 'completed' ORDER BY created_at LIMIT 1", subject))
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		req, err = scanStaffRequest(tx.QueryRow(ctx, `INSERT INTO staff_erasure_requests (id, subject, note, status)
			VALUES ($1, $2, $3, 'pending') RETURNING `+staffRequestColumns, newID(), subject, note))
		return err
	})
	return req, err
}

func (s *PostgresStore) ListStaffRequests(ctx context.Context, subject string) ([]StaffRequest, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+staffRequestColumns+
		" FROM staff_erasure_requests WHERE $1 = '' OR subject = $1 ORDER BY created_at DESC, id DESC", subject)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (StaffRequest, error) {
		req, err := scanStaffRequest(r)
		if err != nil {
			return StaffRequest{}, err
		}
		return *req, nil
	})
}

// ExecuteStaffRequest removes the person's direct access to every area, except where
// they are the last owner. Each area is locked like any other membership change.
func (s *PostgresStore) ExecuteStaffRequest(ctx context.Context, requestID, executedBy string) (*StaffRequest, error) {
	var req *StaffRequest
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		req, err = scanStaffRequest(tx.QueryRow(ctx, "SELECT "+staffRequestColumns+" FROM staff_erasure_requests WHERE id = $1 FOR UPDATE", requestID))
		if err != nil {
			return err
		}
		if req.Status == "completed" {
			return errRequestCompleted
		}
		rows, err := tx.Query(ctx, `SELECT m.workspace_id, w.name, m.role FROM workspace_members m
			JOIN workspaces w ON w.id = m.workspace_id WHERE m.subject = $1 ORDER BY w.name COLLATE "C"`, req.Subject)
		if err != nil {
			return err
		}
		type membership struct {
			WorkspaceID, Name string
			Role              Role
		}
		memberships, err := pgx.CollectRows(rows, pgx.RowToStructByPos[membership])
		if err != nil {
			return err
		}
		removed := 0
		var kept []string
		for _, m := range memberships {
			if err := lockWorkspace(ctx, tx, m.WorkspaceID); err != nil {
				return err
			}
			err := ownerCheck(ctx, tx, m.WorkspaceID, m.Role, "")
			if errors.Is(err, errLastOwner) {
				kept = append(kept, m.Name)
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "DELETE FROM workspace_members WHERE workspace_id = $1 AND subject = $2", m.WorkspaceID, req.Subject); err != nil {
				return err
			}
			removed++
		}
		outcome, status := staffResult(removed, kept)
		result, err := json.Marshal(outcome)
		if err != nil {
			return err
		}
		req, err = scanStaffRequest(tx.QueryRow(ctx, `UPDATE staff_erasure_requests
			SET status = $2, result = $3, resolved_at = now(), resolved_by = $4 WHERE id = $1 RETURNING `+staffRequestColumns,
			requestID, status, result, executedBy))
		return err
	})
	if err != nil {
		return nil, err
	}
	return req, nil
}
