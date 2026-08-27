package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"stageclearance/internal/domain"
)

func (s *Store) Get(ctx context.Context, id string) (domain.Snapshot, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT snapshot_json FROM productions WHERE id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Snapshot{}, domain.NotFound("production", id)
	}
	if err != nil {
		return domain.Snapshot{}, err
	}
	var snapshot domain.Snapshot
	err = json.Unmarshal(raw, &snapshot)
	return snapshot, err
}

func (s *Store) List(ctx context.Context) ([]domain.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT snapshot_json FROM productions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Snapshot
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var item domain.Snapshot
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) FindCredential(ctx context.Context, code string) (domain.Snapshot, *domain.ReleaseCredential, error) {
	var productionID string
	err := s.db.QueryRowContext(ctx, `SELECT production_id FROM release_credentials WHERE code=?`, code).Scan(&productionID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Snapshot{}, nil, domain.NotFound("credential", code)
	}
	if err != nil {
		return domain.Snapshot{}, nil, err
	}
	snapshot, err := s.Get(ctx, productionID)
	if err != nil {
		return domain.Snapshot{}, nil, err
	}
	for i := range snapshot.Credentials {
		if snapshot.Credentials[i].VerificationCode == code {
			return snapshot, &snapshot.Credentials[i], nil
		}
	}
	return domain.Snapshot{}, nil, domain.NotFound("credential", code)
}

func (s *Store) Audit(ctx context.Context, id string) ([]AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,production_id,revision,actor_id,action,detail,created_at FROM audit_events WHERE production_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var created string
		if err = rows.Scan(&event.ID, &event.ProductionID, &event.Revision, &event.ActorID, &event.Action, &event.Detail, &created); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, event)
	}
	return out, rows.Err()
}
