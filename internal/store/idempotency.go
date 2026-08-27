package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"stageclearance/internal/domain"
)

type cachedFailure struct {
	Message string `json:"message"`
}

func (e *cachedFailure) Error() string { return e.Message }

func loadIdempotent(ctx context.Context, tx *sql.Tx, id, key, action string) (domain.Snapshot, bool, error) {
	var storedAction string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT action,snapshot_json FROM idempotency_results WHERE production_id=? AND key=?`, id, key).Scan(&storedAction, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		if cachedErr, ok, failureErr := loadIdempotentFailure(ctx, tx, id, key, action); failureErr != nil {
			return domain.Snapshot{}, false, failureErr
		} else if ok {
			return domain.Snapshot{}, false, cachedErr
		}
		return domain.Snapshot{}, false, nil
	}
	if err != nil {
		return domain.Snapshot{}, false, err
	}
	if storedAction != action {
		return domain.Snapshot{}, false, domain.Invalid("idempotencyKey", "该幂等键已用于其他操作")
	}
	var snapshot domain.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return domain.Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func loadIdempotentFailure(ctx context.Context, tx *sql.Tx, id, key, action string) (error, bool, error) {
	var storedAction string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT action,error_json FROM idempotency_failures WHERE production_id=? AND key=?`, id, key).Scan(&storedAction, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedAction != action {
		return nil, false, domain.Invalid("idempotencyKey", "该幂等键已用于其他操作")
	}
	var cached cachedFailure
	if err = json.Unmarshal(raw, &cached); err != nil {
		return nil, false, err
	}
	return &cached, true, nil
}

func saveIdempotentFailure(ctx context.Context, tx *sql.Tx, scopeID, key, action string, failure *domain.Error) error {
	raw, err := json.Marshal(cachedFailure{Message: failure.Message})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_failures(production_id,key,action,error_json,created_at) VALUES(?,?,?,?,?)`, scopeID, key, action, raw, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return failure
}

func saveMetadata(ctx context.Context, tx *sql.Tx, snapshot domain.Snapshot, key, actor, action, detail string) error {
	return saveMetadataScoped(ctx, tx, snapshot.Production.ID, snapshot, key, actor, action, detail)
}

func saveMetadataScoped(ctx context.Context, tx *sql.Tx, scopeID string, snapshot domain.Snapshot, key, actor, action, detail string) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_results(production_id,key,action,revision,snapshot_json,created_at) VALUES(?,?,?,?,?,?)`, scopeID, key, action, snapshot.Production.Revision, raw, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(production_id,revision,actor_id,action,detail,created_at) VALUES(?,?,?,?,?,?)`, snapshot.Production.ID, snapshot.Production.Revision, actor, action, detail, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
