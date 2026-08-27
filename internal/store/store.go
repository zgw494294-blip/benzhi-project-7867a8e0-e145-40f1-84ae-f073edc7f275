package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite"
	"stageclearance/internal/domain"
)

type Store struct{ db *sql.DB }

type AuditEvent struct {
	ID           int64     `json:"id"`
	ProductionID string    `json:"productionID"`
	Revision     int64     `json:"revision"`
	ActorID      string    `json:"actorID"`
	Action       string    `json:"action"`
	Detail       string    `json:"detail"`
	CreatedAt    time.Time `json:"createdAt"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err = store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Create(ctx context.Context, snapshot domain.Snapshot, idempotencyKey, actor, action string) (domain.Snapshot, error) {
	if idempotencyKey == "" {
		return domain.Snapshot{}, domain.Invalid("idempotencyKey", "幂等键不能为空")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Snapshot{}, err
	}
	defer tx.Rollback()
	if cached, ok, err := loadIdempotent(ctx, tx, snapshot.Production.ID, idempotencyKey, action); err != nil {
		return domain.Snapshot{}, err
	} else if ok {
		return cached, nil
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return domain.Snapshot{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO productions(id,revision,state,snapshot_json,created_at,updated_at) VALUES(?,?,?,?,?,?)`, snapshot.Production.ID, snapshot.Production.Revision, snapshot.Production.State, raw, snapshot.Production.CreatedAt.Format(time.RFC3339Nano), snapshot.Production.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return domain.Snapshot{}, mapSQLError(err)
	}
	if err = syncTables(ctx, tx, snapshot); err != nil {
		return domain.Snapshot{}, err
	}
	if err = saveMetadata(ctx, tx, snapshot, idempotencyKey, actor, action, "创建制作档案"); err != nil {
		return domain.Snapshot{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

// CloneCreate 在读取来源、构造新聚合、写入所有子实体及幂等结果的同一个事务中完成复制。
func (s *Store) CloneCreate(ctx context.Context, sourceID, idempotencyKey, actor string, build func(domain.Snapshot) (domain.Snapshot, error)) (domain.Snapshot, error) {
	const action = "production.clone"
	if idempotencyKey == "" {
		return domain.Snapshot{}, domain.Invalid("idempotencyKey", "幂等键不能为空")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Snapshot{}, err
	}
	defer tx.Rollback()
	if cached, ok, err := loadIdempotent(ctx, tx, sourceID, idempotencyKey, action); err != nil {
		return domain.Snapshot{}, err
	} else if ok {
		return cached, nil
	}
	var sourceRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT snapshot_json FROM productions WHERE id=?`, sourceID).Scan(&sourceRaw); errors.Is(err, sql.ErrNoRows) {
		failure := domain.NotFound("production", sourceID)
		var domainErr *domain.Error
		errors.As(failure, &domainErr)
		return domain.Snapshot{}, saveIdempotentFailure(ctx, tx, sourceID, idempotencyKey, action, domainErr)
	} else if err != nil {
		return domain.Snapshot{}, err
	}
	var source domain.Snapshot
	if err = json.Unmarshal(sourceRaw, &source); err != nil {
		return domain.Snapshot{}, err
	}
	created, err := build(source)
	if err != nil {
		var domainErr *domain.Error
		if errors.As(err, &domainErr) {
			return domain.Snapshot{}, saveIdempotentFailure(ctx, tx, sourceID, idempotencyKey, action, domainErr)
		}
		return domain.Snapshot{}, err
	}
	raw, err := json.Marshal(created)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO productions(id,revision,state,snapshot_json,created_at,updated_at) VALUES(?,?,?,?,?,?)`, created.Production.ID, created.Production.Revision, created.Production.State, raw, created.Production.CreatedAt.Format(time.RFC3339Nano), created.Production.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
		return domain.Snapshot{}, mapSQLError(err)
	}
	if err = syncTables(ctx, tx, created); err != nil {
		return domain.Snapshot{}, err
	}
	// 复制命令以来源制作作为幂等作用域，结果则完整保存新制作快照。
	if err = saveMetadataScoped(ctx, tx, sourceID, created, idempotencyKey, actor, action, "复制既有方案为独立草拟档案"); err != nil {
		return domain.Snapshot{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Snapshot{}, err
	}
	return created, nil
}

func (s *Store) Update(ctx context.Context, id string, expected int64, idempotencyKey, actor, action string, mutate func(*domain.Snapshot) error) (domain.Snapshot, error) {
	return s.UpdateDetailed(ctx, id, expected, idempotencyKey, actor, action, action, mutate)
}

func (s *Store) UpdateDetailed(ctx context.Context, id string, expected int64, idempotencyKey, actor, action, detail string, mutate func(*domain.Snapshot) error) (domain.Snapshot, error) {
	if idempotencyKey == "" {
		return domain.Snapshot{}, domain.Invalid("idempotencyKey", "幂等键不能为空")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Snapshot{}, err
	}
	defer tx.Rollback()
	if cached, ok, err := loadIdempotent(ctx, tx, id, idempotencyKey, action); err != nil {
		return domain.Snapshot{}, err
	} else if ok {
		return cached, nil
	}
	var raw []byte
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT revision,snapshot_json FROM productions WHERE id=?`, id).Scan(&current, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Snapshot{}, domain.NotFound("production", id)
	}
	if err != nil {
		return domain.Snapshot{}, err
	}
	if current != expected {
		return domain.Snapshot{}, domain.Conflict("制作修订已变化，请刷新后重试", current)
	}
	var snapshot domain.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return domain.Snapshot{}, err
	}
	if err = mutate(&snapshot); err != nil {
		return domain.Snapshot{}, err
	}
	snapshot.Production.Revision = current + 1
	snapshot.Production.UpdatedAt = time.Now().UTC()
	raw, err = json.Marshal(snapshot)
	if err != nil {
		return domain.Snapshot{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE productions SET revision=?,state=?,snapshot_json=?,updated_at=? WHERE id=? AND revision=?`, snapshot.Production.Revision, snapshot.Production.State, raw, snapshot.Production.UpdatedAt.Format(time.RFC3339Nano), id, current)
	if err != nil {
		return domain.Snapshot{}, err
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return domain.Snapshot{}, domain.Conflict("制作修订已变化，请刷新后重试", current)
	}
	if err = syncTables(ctx, tx, snapshot); err != nil {
		return domain.Snapshot{}, err
	}
	if err = saveMetadata(ctx, tx, snapshot, idempotencyKey, actor, action, detail); err != nil {
		return domain.Snapshot{}, err
	}
	if err = tx.Commit(); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

func mapSQLError(err error) error {
	if err == nil {
		return nil
	}
	if len(err.Error()) > 0 {
		return &domain.Error{Code: "storage_conflict", Message: "数据标识已存在"}
	}
	return err
}
